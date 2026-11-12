//! A document's schema, as its caller hands it over: its function table, and
//! the validation of the resolved document against it.

use std::collections::{BTreeMap, BTreeSet};
use std::sync::OnceLock;

use fancy_regex::Regex;
use jsonschema::error::{TypeKind, ValidationErrorKind};
use jsonschema::{Draft, ValidationError};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value as JsonValue};

use crate::diagnostic::Diagnostic;
use crate::function_table::{FunctionTable, MacroKind};
use crate::node::number_json;
use crate::position::Position;
use crate::problem_wording::{
    alternative_constants, code_span, contains_message, definition_description, dependency_message,
    dependency_of, dependency_value_path, duplicate_items, field_name, number_text, schema_note,
    unescape_pointer,
};
use crate::requiredness_markers::marked_schema;
use crate::resolved::{Call, MapEntry, Value, ValueKind};
pub use crate::schema_cache::{SchemaCache, SchemaStore};
use crate::schema_resources::{SchemaResources, child_schemas};
pub use crate::validation_rules::child_value;
use crate::validation_rules::{ValidationRules, copied_pointers, escape_pointer, merged_over};

pub struct LoadedSchema {
    pub table: FunctionTable,
    validator: jsonschema::Validator,
    schema: JsonValue,
    /// The schema with its requiredness markers read: what a document is validated against.
    marked: JsonValue,
    /// The schema as a report names it, such as its file's name or its URL.
    name: String,
    draft: Draft,
    subschema_validators: OnceLock<Option<jsonschema::ValidatorMap>>,
    resources: SchemaResources,
}

struct SchemaFailure<'v> {
    error: ValidationError<'v>,
    pointer: String,
    schema_path: String,
    failing_members: Vec<String>,
    instance_base: String,
    evaluation_base: String,
}

struct SchemaNode<'s> {
    validator: &'s jsonschema::Validator,
    fragment_pointer: Option<&'s str>,
}

impl SchemaNode<'_> {
    /// The pointer of the schema node the validator starts from.
    fn start_pointer(&self) -> &str {
        self.fragment_pointer.unwrap_or_default()
    }
}

/// What a schema's text prepares before a validator is built from it: the draft
/// it is read in, the schema as written, and the schema its requiredness markers
/// are read into. The package's own information, kept apart from the validator.
#[derive(Serialize, Deserialize)]
pub(crate) struct PreparedSchema {
    draft: String,
    schema: JsonValue,
    marked: JsonValue,
}

impl PreparedSchema {
    pub(crate) fn of(name: &str, text: &str) -> Result<Self, String> {
        let schema: JsonValue =
            serde_json::from_str(text).map_err(|error| format!("schema {name}: {error}"))?;
        let draft = schema_draft(&schema).map_err(|error| format!("schema {name}: {error}"))?;
        let marked = marked_schema(&schema, draft);
        Ok(Self {
            draft: draft_name(draft).to_owned(),
            schema,
            marked,
        })
    }
}

/// A problem, and the document value it is about.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SchemaProblem {
    pub subject: String,
    pub diagnostic: Diagnostic,
}

/// What the problems already reported cover: each dependency's key and each list
/// whose items beyond its tuple are reported once.
#[derive(Default)]
struct ReportedSubjects {
    dependencies: BTreeSet<String>,
    item_lists: BTreeSet<String>,
}

impl LoadedSchema {
    /// The schema built from what its text prepared: its function table read, and
    /// its validator built from the marked schema.
    pub(crate) fn from_prepared(name: &str, prepared: PreparedSchema) -> Result<Self, String> {
        let PreparedSchema {
            draft,
            schema,
            marked,
        } = prepared;
        let draft = draft_of_name(&draft)
            .ok_or_else(|| format!("schema {name}: prepared in an unknown draft {draft}"))?;
        let table = FunctionTable::from_schema(&schema)
            .map_err(|error| format!("schema {name}: {error}"))?;
        let resources = SchemaResources::of(&schema);
        if let Some(failure) = schema_load_failure(&schema, &resources, &schema, "") {
            return Err(format!("schema {name}: {failure}"));
        }
        let validator = jsonschema::options()
            .with_draft(draft)
            .should_validate_formats(false)
            .build(&marked)
            .map_err(|error| format!("schema {name}: {error}"))?;
        Ok(Self {
            table,
            validator,
            schema,
            marked,
            name: name.to_owned(),
            draft,
            subschema_validators: OnceLock::new(),
            resources,
        })
    }

    /// A failure of `error`, found by the validator starting at the schema node at
    /// `start_pointer` on the document value at `instance_base`.
    fn failure_of<'v>(
        &self,
        error: ValidationError<'v>,
        start_pointer: &str,
        instance_base: &str,
    ) -> SchemaFailure<'v> {
        SchemaFailure {
            pointer: format!("{instance_base}{}", error.instance_path().as_str()),
            schema_path: self.keyword_location(start_pointer, error.evaluation_path().as_str()),
            error,
            failing_members: Vec::new(),
            instance_base: instance_base.to_owned(),
            evaluation_base: start_pointer.to_owned(),
        }
    }

    /// The schema as written, with the resources its references are read against.
    fn resolvable_schema(&self) -> ResolvableSchema<'_> {
        ResolvableSchema {
            json: &self.schema,
            resources: &self.resources,
        }
    }

    /// Where the whole schema holds the keyword `evaluation_path` reaches from the
    /// node at `start_pointer`.
    fn keyword_location(&self, start_pointer: &str, evaluation_path: &str) -> String {
        self.resources
            .evaluation_pointer(&self.marked, start_pointer, evaluation_path)
            .unwrap_or_else(|| format!("{start_pointer}{evaluation_path}"))
    }

    /// The schema `text` holds, named `name` in reports, prepared and built at once.
    #[cfg(test)]
    fn parse(name: &str, text: &str) -> Result<Self, String> {
        Self::from_prepared(name, PreparedSchema::of(name, text)?)
    }

    /// Validates `document`, read by `rules`. A value not known when the document
    /// compiles, such as a call or a reference to a declaration, is not checked.
    pub fn validate(&self, document: &Value, rules: &ValidationRules) -> Vec<Diagnostic> {
        self.problems(document, rules)
            .into_iter()
            .map(|problem| problem.diagnostic)
            .collect()
    }

    /// The problems `validate` reports, each with the document value it is about.
    pub fn problems(&self, document: &Value, rules: &ValidationRules) -> Vec<SchemaProblem> {
        let schema_node = SchemaNode {
            validator: &self.validator,
            fragment_pointer: None,
        };
        self.node_problems(&schema_node, document, rules)
    }

    /// The problems of `document` read against the schema node at `fragment_pointer`.
    pub fn fragment_problems(
        &self,
        fragment_pointer: &str,
        document: &Value,
        rules: &ValidationRules,
    ) -> Vec<SchemaProblem> {
        let Some(validator) = self.subschema_validator(fragment_pointer) else {
            let missing_node = code_span(&format!("#{fragment_pointer}"));
            return vec![SchemaProblem {
                subject: String::new(),
                diagnostic: Diagnostic::at(
                    &document.position,
                    format!("the schema has no node at {missing_node}"),
                ),
            }];
        };
        let schema_node = SchemaNode {
            validator,
            fragment_pointer: Some(fragment_pointer),
        };
        self.node_problems(&schema_node, document, rules)
    }

    fn node_problems(
        &self,
        schema_node: &SchemaNode<'_>,
        document: &Value,
        rules: &ValidationRules,
    ) -> Vec<SchemaProblem> {
        let mut instance = Instance {
            rules,
            table: &self.table,
            positions: BTreeMap::new(),
            value_positions: BTreeMap::new(),
            unknown: BTreeSet::new(),
            copied: BTreeSet::new(),
            merge_calls: BTreeSet::new(),
        };
        let mut json = instance.json(document, String::new(), &document.position);
        self.read_quoted_values(schema_node, &mut json, rules);
        let mut reported = ReportedSubjects::default();
        let failures = self.schema_failures(schema_node, &json);
        let requires_no_key = |pointer: &str| {
            schema_node.fragment_pointer.is_some() || instance.requires_no_key(pointer)
        };
        let mut problems: Vec<SchemaProblem> = failures
            .iter()
            .filter(|failure| !instance.reads_unknown(&failure.instance_base, &failure.error))
            .filter(|failure| {
                !requires_no_key(&failure.pointer) || !passes_without_requiredness(&failure.error)
            })
            .flat_map(|failure| self.error_problems(failure, &json, &instance, &mut reported))
            .filter(|problem| !instance.is_copied(&problem.subject))
            .collect();
        problems.sort_by(|first, second| {
            place(&first.diagnostic)
                .cmp(&place(&second.diagnostic))
                .then_with(|| first.diagnostic.message.cmp(&second.diagnostic.message))
        });
        problems.dedup_by(|later, earlier| later.diagnostic == earlier.diagnostic);
        problems
    }

    fn schema_failures<'v>(
        &'v self,
        schema_node: &SchemaNode<'v>,
        json: &'v JsonValue,
    ) -> Vec<SchemaFailure<'v>> {
        let mut failing_units: Option<Vec<(String, String)>> = None;
        let mut failures = Vec::new();
        for error in schema_node.validator.iter_errors(json) {
            let is_unevaluated = matches!(
                error.kind(),
                ValidationErrorKind::UnevaluatedProperties { .. }
                    | ValidationErrorKind::UnevaluatedItems { .. }
            );
            let mut failure = self.failure_of(error, schema_node.start_pointer(), "");
            if !is_unevaluated {
                failures.push(failure);
                continue;
            }
            let units = failing_units.get_or_insert_with(|| self.failing_units(schema_node, json));
            let members = failing_members(units, &failure.pointer, &failure.schema_path);
            let is_open = self
                .marked
                .pointer(&failure.schema_path)
                .is_some_and(|keyword| keyword != &JsonValue::Bool(false));
            match self.subschema_validator(&failure.schema_path) {
                Some(validator) if is_open => {
                    failures.extend(self.member_failures(json, &members, &failure, validator));
                }
                _ => {
                    failure.failing_members = members;
                    failures.push(failure);
                }
            }
        }
        failures
    }

    fn failing_units(
        &self,
        schema_node: &SchemaNode<'_>,
        json: &JsonValue,
    ) -> Vec<(String, String)> {
        let evaluation = schema_node.validator.evaluate(json);
        let listed = serde_json::to_value(evaluation.list()).unwrap_or_default();
        listed["details"]
            .as_array()
            .into_iter()
            .flatten()
            .filter(|unit| unit["valid"] == JsonValue::Bool(false))
            .filter_map(|unit| {
                Some((
                    self.keyword_location(
                        schema_node.start_pointer(),
                        unit["evaluationPath"].as_str()?,
                    ),
                    unit["instanceLocation"].as_str()?.to_owned(),
                ))
            })
            .collect()
    }

    fn subschema_validator(&self, keyword_pointer: &str) -> Option<&jsonschema::Validator> {
        self.subschema_validators
            .get_or_init(|| {
                jsonschema::options()
                    .with_draft(self.draft)
                    .should_validate_formats(false)
                    .build_map(&self.marked)
                    .ok()
            })
            .as_ref()?
            .get(&format!("#{keyword_pointer}"))
    }

    fn member_failures<'v>(
        &self,
        json: &'v JsonValue,
        members: &[String],
        unevaluated: &SchemaFailure<'_>,
        validator: &'v jsonschema::Validator,
    ) -> Vec<SchemaFailure<'v>> {
        let mut failures = Vec::new();
        for member in members {
            let Some(value) = json.pointer(member) else {
                continue;
            };
            for error in validator.iter_errors(value) {
                failures.push(self.failure_of(error, &unevaluated.schema_path, member));
            }
        }
        failures
    }

    /// Reads each quoted value that spells a type the schema expects of it, and
    /// `rules` allow, as that type, until no more are read.
    fn read_quoted_values(
        &self,
        schema_node: &SchemaNode<'_>,
        json: &mut JsonValue,
        rules: &ValidationRules,
    ) {
        if rules.quoted_value_types.is_empty() {
            return;
        }
        loop {
            let mut readings: BTreeMap<String, JsonValue> = BTreeMap::new();
            for error in schema_node.validator.iter_errors(json) {
                self.collect_quoted_readings(schema_node, &error, rules, &mut readings);
            }
            if readings.is_empty() {
                return;
            }
            for (pointer, value) in readings {
                if let Some(slot) = json.pointer_mut(&pointer) {
                    *slot = value;
                }
            }
        }
    }

    fn collect_quoted_readings(
        &self,
        schema_node: &SchemaNode<'_>,
        error: &ValidationError<'_>,
        rules: &ValidationRules,
        readings: &mut BTreeMap<String, JsonValue>,
    ) {
        match error.kind() {
            ValidationErrorKind::Type { .. } => {
                let Some(text) = error.instance().as_str() else {
                    return;
                };
                let keyword_pointer = self.keyword_location(
                    schema_node.start_pointer(),
                    error.evaluation_path().as_str(),
                );
                let expected = self.expected_types(&keyword_pointer, error);
                if let Some(value) = rules.quoted_value(text, &expected) {
                    readings
                        .entry(error.instance_path().as_str().to_owned())
                        .or_insert(value);
                }
            }
            ValidationErrorKind::AnyOf { context }
            | ValidationErrorKind::OneOfNotValid { context } => {
                for inner in context.iter().flatten() {
                    self.collect_quoted_readings(schema_node, inner, rules, readings);
                }
            }
            _ => {}
        }
    }

    /// The types a `type` error's schema lists, in the order it lists them.
    fn expected_types(&self, schema_path: &str, error: &ValidationError<'_>) -> Vec<String> {
        match self.marked.pointer(schema_path) {
            Some(JsonValue::String(single)) => vec![single.clone()],
            Some(JsonValue::Array(listed)) => listed
                .iter()
                .filter_map(|listed_type| listed_type.as_str().map(str::to_owned))
                .collect(),
            _ => match error.kind() {
                ValidationErrorKind::Type {
                    kind: TypeKind::Single(single),
                } => vec![single.as_str().to_owned()],
                ValidationErrorKind::Type {
                    kind: TypeKind::Multiple(several),
                } => several
                    .iter()
                    .map(|listed| listed.as_str().to_owned())
                    .collect(),
                _ => Vec::new(),
            },
        }
    }

    /// A schema error as the problems a reader fixes it from: placed at the value
    /// or the field that is wrong, worded by the standard, labeled with what is
    /// wrong there, naming the rule it breaks, and the allowed name closest to a
    /// misspelt one.
    fn error_problems(
        &self,
        failure: &SchemaFailure<'_>,
        json: &JsonValue,
        instance: &Instance<'_>,
        reported: &mut ReportedSubjects,
    ) -> Vec<SchemaProblem> {
        let error = &failure.error;
        let pointer = failure.pointer.as_str();
        let schema_path = failure.schema_path.as_str();
        let rule_at = |keyword: &str, at: &str| format!("{keyword}, at {}#{at}", self.name);
        let rule = rule_at(error.kind().keyword(), schema_path);
        let field = field_name(json, pointer);
        let holder_value = json.pointer(pointer).unwrap_or(&JsonValue::Null);
        let missing_field = match error.kind() {
            ValidationErrorKind::Required { property } => property.as_str(),
            _ => None,
        };
        if let Some((keyword_at, key)) =
            dependency_of(&self.marked, schema_path, holder_value, missing_field)
        {
            let subject = format!("{pointer}/{}", escape_pointer(&key));
            if !reported.dependencies.insert(subject.clone()) {
                return Vec::new();
            }
            let keyword = keyword_at.rsplit('/').next().unwrap_or_default().to_owned();
            let value_node =
                dependency_value_path(error.evaluation_path().as_str()).and_then(|value_path| {
                    self.resources.evaluation_pointer(
                        &self.marked,
                        &failure.evaluation_base,
                        &value_path,
                    )
                });
            let diagnostic = Diagnostic::at(
                instance.position(&subject),
                dependency_message(&self.marked, &self.resources, value_node.as_deref(), &key),
            )
            .with_label(format!("breaks {}", code_span(&keyword)))
            .with_rule(rule_at(
                &keyword,
                &format!("{keyword_at}/{}", escape_pointer(&key)),
            ));
            return vec![SchemaProblem {
                subject,
                diagnostic,
            }];
        }
        let suggestion = |name: &str, candidates: Vec<String>| {
            closest_name(name, &candidates)
                .map(|candidate| format!("did you mean {}?", code_span(&candidate)))
        };
        let at_value_with_schema = |message: String, schema_note: Option<String>| {
            vec![SchemaProblem {
                subject: pointer.to_owned(),
                diagnostic: Diagnostic::at(instance.value_position(pointer), message)
                    .with_label(format!("breaks {}", code_span(error.kind().keyword())))
                    .with_rule(rule.clone())
                    .with_schema(schema_note),
            }]
        };
        let at_value = |message: String| at_value_with_schema(message, None);
        let listed_value_problems = |listed: Vec<&JsonValue>, schema_note: Option<String>| {
            let allowed: Vec<String> = listed
                .iter()
                .filter_map(|option| option.as_str().map(str::to_owned))
                .collect();
            let shown: Vec<String> = listed
                .iter()
                .map(|option| code_span(&option.to_string()))
                .collect();
            let help = error
                .instance()
                .as_str()
                .and_then(|text| suggestion(text, allowed));
            let diagnostic = Diagnostic::at(
                instance.value_position(pointer),
                format!("{field} must be one of {}", shown.join(", ")),
            )
            .with_label(format!(
                "not one of the {} values {} allows",
                listed.len(),
                code_span(error.kind().keyword())
            ))
            .with_rule(rule.clone())
            .with_schema(schema_note)
            .with_help(help);
            vec![SchemaProblem {
                subject: pointer.to_owned(),
                diagnostic,
            }]
        };
        let list_limit = |limit: usize| {
            format!(
                "{field} must NOT have more than {} items",
                code_span(&limit.to_string())
            )
        };
        let limit_text = |limit: &u64| code_span(&limit.to_string());
        match error.kind() {
            ValidationErrorKind::AdditionalProperties { unexpected }
            | ValidationErrorKind::UnevaluatedProperties { unexpected } => {
                let allowed = allowed_fields(self.resolvable_schema(), schema_path);
                let is_unevaluated = matches!(
                    error.kind(),
                    ValidationErrorKind::UnevaluatedProperties { .. }
                );
                unexpected
                    .iter()
                    .filter(|name| {
                        !is_unevaluated
                            || !instance.member_reads_unknown(pointer, || {
                                field_could_be_accepted(self.resolvable_schema(), schema_path, name)
                            })
                    })
                    .map(|name| {
                        let subject = format!("{pointer}/{}", escape_pointer(name));
                        let diagnostic = Diagnostic::at(
                            instance.position(&subject),
                            format!("{} is not defined in the schema", code_span(name)),
                        )
                        .with_rule(rule.clone())
                        .with_help(suggestion(name, allowed.clone()));
                        SchemaProblem {
                            subject,
                            diagnostic,
                        }
                    })
                    .collect()
            }
            ValidationErrorKind::PropertyNames { error: inner } => {
                let name = inner.instance().as_str().unwrap_or_default();
                let subject = format!("{pointer}/{}", escape_pointer(name));
                let diagnostic = Diagnostic::at(
                    instance.position(&subject),
                    format!("{} is not a valid key of {field}", code_span(name)),
                )
                .with_label("breaks `propertyNames`")
                .with_rule(rule);
                vec![SchemaProblem {
                    subject,
                    diagnostic,
                }]
            }
            ValidationErrorKind::Enum { options } => {
                let listed = options.as_array().map(Vec::as_slice).unwrap_or_default();
                listed_value_problems(listed.iter().collect(), None)
            }
            ValidationErrorKind::Type { .. } => {
                let expected: Vec<String> = self
                    .expected_types(schema_path, error)
                    .iter()
                    .map(|expected_type| code_span(expected_type))
                    .collect();
                let diagnostic = Diagnostic::at(
                    instance.value_position(pointer),
                    format!("{field} must be {}", expected.join(" or ")),
                )
                .with_label(format!("is {}", with_article(json_type(error.instance()))))
                .with_rule(rule);
                vec![SchemaProblem {
                    subject: pointer.to_owned(),
                    diagnostic,
                }]
            }
            ValidationErrorKind::Required { property } => {
                let name = property.as_str().unwrap_or_default();
                let diagnostic = Diagnostic::at(
                    instance.position(pointer),
                    format!("{} is required", code_span(name)),
                )
                .with_label(format!("{field} has no field {}", code_span(name)))
                .with_rule(rule);
                vec![SchemaProblem {
                    subject: pointer.to_owned(),
                    diagnostic,
                }]
            }
            ValidationErrorKind::AnyOf { .. } | ValidationErrorKind::OneOfNotValid { .. } => {
                let holder = schema_path
                    .rsplit_once('/')
                    .map_or("", |(holder, _)| holder);
                let definition_note = definition_description(&self.schema, holder);
                match self
                    .schema
                    .pointer(schema_path)
                    .and_then(alternative_constants)
                {
                    Some(constants) => listed_value_problems(constants, definition_note),
                    None => at_value_with_schema(
                        format!("{field} matches none of its allowed forms"),
                        definition_note,
                    ),
                }
            }
            ValidationErrorKind::OneOfMultipleValid { .. } => at_value(format!(
                "{field} matches more than one of its allowed forms"
            )),
            ValidationErrorKind::Constant { expected_value } => at_value(format!(
                "{field} must be {}",
                code_span(&expected_value.to_string())
            )),
            ValidationErrorKind::Minimum { limit } => at_value(format!(
                "{field} must be >= {}",
                code_span(&number_text(limit))
            )),
            ValidationErrorKind::Maximum { limit } => at_value(format!(
                "{field} must be <= {}",
                code_span(&number_text(limit))
            )),
            ValidationErrorKind::ExclusiveMinimum { limit } => at_value(format!(
                "{field} must be > {}",
                code_span(&number_text(limit))
            )),
            ValidationErrorKind::ExclusiveMaximum { limit } => at_value(format!(
                "{field} must be < {}",
                code_span(&number_text(limit))
            )),
            ValidationErrorKind::MultipleOf { multiple_of } => at_value(format!(
                "{field} must be multiple of {}",
                code_span(&number_text(&JsonValue::from(*multiple_of)))
            )),
            ValidationErrorKind::MinLength { limit } => at_value(format!(
                "{field} must NOT have fewer than {} characters",
                limit_text(limit)
            )),
            ValidationErrorKind::MaxLength { limit } => at_value(format!(
                "{field} must NOT have more than {} characters",
                limit_text(limit)
            )),
            ValidationErrorKind::MinItems { limit } => at_value(format!(
                "{field} must NOT have fewer than {} items",
                limit_text(limit)
            )),
            ValidationErrorKind::MaxItems { limit } => at_value(list_limit(*limit as usize)),
            ValidationErrorKind::MinProperties { limit } => at_value(format!(
                "{field} must NOT have fewer than {} properties",
                limit_text(limit)
            )),
            ValidationErrorKind::MaxProperties { limit } => at_value(format!(
                "{field} must NOT have more than {} properties",
                limit_text(limit)
            )),
            ValidationErrorKind::Pattern { pattern } => {
                at_value(format!("{field} must match pattern {}", code_span(pattern)))
            }
            ValidationErrorKind::UniqueItems => {
                let items = error
                    .instance()
                    .as_array()
                    .map(Vec::as_slice)
                    .unwrap_or_default();
                let (later, earlier) = duplicate_items(items).unwrap_or_default();
                at_value(format!(
                    "{field} must NOT have duplicate items \
                     (items ## {later} and {earlier} are identical)"
                ))
            }
            ValidationErrorKind::Contains => {
                let holder = schema_path
                    .rsplit_once('/')
                    .map_or("", |(holder, _)| holder);
                let message = format!("{field} {}", contains_message(self.marked.pointer(holder)));
                vec![SchemaProblem {
                    subject: pointer.to_owned(),
                    diagnostic: Diagnostic::at(instance.value_position(pointer), message)
                        .with_label("breaks `contains`")
                        .with_rule(rule_at("contains", &format!("{holder}/contains"))),
                }]
            }
            ValidationErrorKind::Not { .. } => at_value_with_schema(
                format!("{field} matches a form that is not allowed"),
                self.schema.pointer(schema_path).and_then(schema_note),
            ),
            ValidationErrorKind::AdditionalItems { limit } => at_value(list_limit(*limit)),
            ValidationErrorKind::UnevaluatedItems { .. } => {
                let length = error.instance().as_array().map_or(0, Vec::len);
                let beyond = failure
                    .failing_members
                    .iter()
                    .filter_map(|member| member.rsplit('/').next()?.parse::<usize>().ok())
                    .filter(|index| {
                        !instance.member_reads_unknown(pointer, || {
                            item_could_be_evaluated(self.resolvable_schema(), schema_path, *index)
                        })
                    })
                    .count();
                if beyond == 0 {
                    return Vec::new();
                }
                at_value(list_limit(length - beyond))
            }
            ValidationErrorKind::FalseSchema
                if schema_path.ends_with("/items") || schema_path.ends_with("/additionalItems") =>
            {
                let list = pointer.rsplit_once('/').map_or("", |(list, _)| list);
                if !reported.item_lists.insert(list.to_owned()) {
                    return Vec::new();
                }
                let (holder, keyword) = schema_path.rsplit_once('/').unwrap_or(("", "items"));
                let tuple_keyword = if keyword == "items" {
                    "prefixItems"
                } else {
                    "items"
                };
                let limit = self
                    .marked
                    .pointer(&format!("{holder}/{tuple_keyword}"))
                    .and_then(JsonValue::as_array)
                    .map_or(0, Vec::len);
                let list_field = field_name(json, list);
                let diagnostic = Diagnostic::at(
                    instance.value_position(list),
                    format!(
                        "{list_field} must NOT have more than {} items",
                        code_span(&limit.to_string())
                    ),
                )
                .with_label(format!("breaks {}", code_span(keyword)))
                .with_rule(rule_at(keyword, schema_path));
                vec![SchemaProblem {
                    subject: list.to_owned(),
                    diagnostic,
                }]
            }
            ValidationErrorKind::FalseSchema => {
                at_value(format!("{field} boolean schema is false"))
            }
            _ => at_value(format!(
                "{field} breaks {}",
                code_span(error.kind().keyword())
            )),
        }
    }
}

/// The draft-07 meta-schema URIs, and those of the drafts read as draft-07.
const DRAFT_07_URIS: [&str; 8] = [
    "http://json-schema.org/draft-07/schema#",
    "http://json-schema.org/draft-07/schema",
    "http://json-schema.org/draft-03/schema#",
    "http://json-schema.org/draft-03/schema",
    "http://json-schema.org/draft-04/schema#",
    "http://json-schema.org/draft-04/schema",
    "http://json-schema.org/draft-06/schema#",
    "http://json-schema.org/draft-06/schema",
];
const DRAFT_2019_09_URIS: [&str; 2] = [
    "https://json-schema.org/draft/2019-09/schema",
    "https://json-schema.org/draft/2019-09/schema#",
];
const DRAFT_2020_12_URIS: [&str; 2] = [
    "https://json-schema.org/draft/2020-12/schema",
    "https://json-schema.org/draft/2020-12/schema#",
];

/// The draft the standard reads a document schema in, by its root `$schema`:
/// draft-07 where it names none or an older draft.
/// A draft's name in prepared information, the same in every language.
fn draft_name(draft: Draft) -> &'static str {
    match draft {
        Draft::Draft201909 => "draft-2019-09",
        Draft::Draft202012 => "draft-2020-12",
        _ => "draft-07",
    }
}

fn draft_of_name(name: &str) -> Option<Draft> {
    match name {
        "draft-07" => Some(Draft::Draft7),
        "draft-2019-09" => Some(Draft::Draft201909),
        "draft-2020-12" => Some(Draft::Draft202012),
        _ => None,
    }
}

fn schema_draft(schema: &JsonValue) -> Result<Draft, String> {
    let Some(declared) = schema.get("$schema") else {
        return Ok(Draft::Draft7);
    };
    let uri = declared.as_str().unwrap_or_default();
    if DRAFT_07_URIS.contains(&uri) {
        Ok(Draft::Draft7)
    } else if DRAFT_2019_09_URIS.contains(&uri) {
        Ok(Draft::Draft201909)
    } else if DRAFT_2020_12_URIS.contains(&uri) {
        Ok(Draft::Draft202012)
    } else {
        Err(format!(
            "declares {declared}, a draft this reader does not read; \
             it reads draft-07, draft 2019-09, draft 2020-12"
        ))
    }
}

/// Why a schema does not load, found before the library compiles it: the first,
/// in the schema's order, of a `$ref` whose JSON pointer names no node of the
/// schema resource holding it, and a `pattern` or `patternProperties` key the
/// standard's pattern dialect does not compile.
fn schema_load_failure(
    schema: &JsonValue,
    resources: &SchemaResources,
    node: &JsonValue,
    pointer: &str,
) -> Option<String> {
    node.as_object()?.iter().find_map(|(keyword, value)| {
        let keyword_failure = match (keyword.as_str(), value) {
            ("$ref", JsonValue::String(reference)) => {
                reference_load_failure(schema, resources, pointer, reference)
            }
            ("pattern", JsonValue::String(pattern)) => pattern_load_failure(pattern),
            _ => None,
        };
        keyword_failure.or_else(|| {
            child_schemas(keyword, value).into_iter().find_map(|child| {
                let name_failure = child
                    .name
                    .filter(|_| keyword == "patternProperties")
                    .and_then(pattern_load_failure);
                name_failure.or_else(|| {
                    let child_pointer = format!("{pointer}{}", child.pointer_suffix);
                    schema_load_failure(schema, resources, child.schema, &child_pointer)
                })
            })
        })
    })
}

fn reference_load_failure(
    schema: &JsonValue,
    resources: &SchemaResources,
    holder_pointer: &str,
    reference: &str,
) -> Option<String> {
    let fragment = reference.strip_prefix('#')?;
    if !fragment.is_empty() && !fragment.starts_with('/') {
        return None;
    }
    let target = resources.reference_target(holder_pointer, reference)?;
    schema
        .pointer(&target)
        .is_none()
        .then(|| format!("{} names no node of the schema", code_span(reference)))
}

fn pattern_load_failure(pattern: &str) -> Option<String> {
    Regex::new(pattern)
        .is_err()
        .then(|| format!("{} is not a pattern", code_span(pattern)))
}

/// Where a diagnostic stands, for reporting a document's problems in its order.
fn place(diagnostic: &Diagnostic) -> Option<(usize, usize)> {
    diagnostic
        .position
        .as_ref()
        .map(|position| (position.line, position.column))
}

fn passes_without_requiredness(error: &ValidationError<'_>) -> bool {
    match error.kind() {
        ValidationErrorKind::Required { .. } => true,
        ValidationErrorKind::AnyOf { context } | ValidationErrorKind::OneOfNotValid { context } => {
            context.iter().any(|branch_errors| {
                !branch_errors.is_empty() && branch_errors.iter().all(passes_without_requiredness)
            })
        }
        _ => false,
    }
}

/// The fields a schema node allows: the `properties` of every schema the node
/// holding the keyword at `keyword_pointer` reaches.
fn allowed_fields(schema: ResolvableSchema<'_>, keyword_pointer: &str) -> Vec<String> {
    let mut fields: Vec<String> = Vec::new();
    let Some(reachable) = reachable_schemas(schema, keyword_pointer) else {
        return fields;
    };
    for (keywords, _) in reachable.schema_nodes {
        for name in keywords
            .get("properties")
            .and_then(JsonValue::as_object)
            .into_iter()
            .flat_map(Map::keys)
        {
            if !fields.contains(name) {
                fields.push(name.clone());
            }
        }
    }
    fields
}

fn field_could_be_accepted(
    schema: ResolvableSchema<'_>,
    keyword_pointer: &str,
    name: &str,
) -> bool {
    schema_could_read(schema, keyword_pointer, &|keywords, is_holder| {
        keywords
            .get("properties")
            .and_then(JsonValue::as_object)
            .is_some_and(|properties| properties.contains_key(name))
            || keywords
                .get("patternProperties")
                .and_then(JsonValue::as_object)
                .is_some_and(|patterns| {
                    patterns.keys().any(|pattern| {
                        Regex::new(pattern)
                            .map_or(true, |compiled| compiled.is_match(name).unwrap_or(true))
                    })
                })
            || is_not_false(keywords, "additionalProperties")
            || (!is_holder && is_not_false(keywords, "unevaluatedProperties"))
    })
}

fn item_could_be_evaluated(
    schema: ResolvableSchema<'_>,
    keyword_pointer: &str,
    index: usize,
) -> bool {
    schema_could_read(schema, keyword_pointer, &|keywords, is_holder| {
        let item_tuple = keywords.get("items").and_then(JsonValue::as_array);
        keywords
            .get("prefixItems")
            .and_then(JsonValue::as_array)
            .is_some_and(|prefix| prefix.len() > index)
            || item_tuple.map_or(is_not_false(keywords, "items"), |tuple| {
                tuple.len() > index || is_not_false(keywords, "additionalItems")
            })
            || keywords.contains_key("contains")
            || (!is_holder && is_not_false(keywords, "unevaluatedItems"))
    })
}

fn failing_members(
    failing_units: &[(String, String)],
    holder_pointer: &str,
    keyword_pointer: &str,
) -> Vec<String> {
    let below_holder = format!("{holder_pointer}/");
    let mut members: Vec<String> = Vec::new();
    for (schema_location, instance_location) in failing_units {
        let is_member = instance_location
            .strip_prefix(&below_holder)
            .is_some_and(|step| !step.is_empty() && !step.contains('/'));
        if schema_location == keyword_pointer && is_member && !members.contains(instance_location) {
            members.push(instance_location.clone());
        }
    }
    members
}

fn is_not_false(keywords: &Map<String, JsonValue>, keyword: &str) -> bool {
    keywords
        .get(keyword)
        .is_some_and(|value| value != &JsonValue::Bool(false))
}

fn schema_could_read(
    schema: ResolvableSchema<'_>,
    keyword_pointer: &str,
    reads_member: &dyn Fn(&Map<String, JsonValue>, bool) -> bool,
) -> bool {
    reachable_schemas(schema, keyword_pointer).is_none_or(|reachable| {
        reachable.reaches_other_document
            || reachable
                .schema_nodes
                .iter()
                .any(|(keywords, is_holder)| reads_member(keywords, *is_holder))
    })
}

#[derive(Default)]
struct ReachableSchemas<'s> {
    schema_nodes: Vec<(&'s Map<String, JsonValue>, bool)>,
    reaches_other_document: bool,
}

/// A schema with the resources its references are read against.
#[derive(Clone, Copy)]
struct ResolvableSchema<'s> {
    json: &'s JsonValue,
    resources: &'s SchemaResources,
}

/// The keywords whose schemas apply to the value of the node holding them.
const REACHING_KEYWORDS: [&str; 7] = [
    "allOf",
    "anyOf",
    "oneOf",
    "if",
    "then",
    "else",
    "dependentSchemas",
];

fn reachable_schemas<'s>(
    schema: ResolvableSchema<'s>,
    keyword_pointer: &str,
) -> Option<ReachableSchemas<'s>> {
    let holder_pointer = keyword_pointer
        .rsplit_once('/')
        .map_or("", |(holder, _)| holder);
    let mut reachable = ReachableSchemas::default();
    let mut visited_references = BTreeSet::new();
    collect_reachable(
        schema,
        holder_pointer.to_owned(),
        true,
        &mut reachable,
        &mut visited_references,
    )?;
    Some(reachable)
}

fn collect_reachable<'s>(
    schema: ResolvableSchema<'s>,
    pointer: String,
    is_holder: bool,
    reachable: &mut ReachableSchemas<'s>,
    visited_references: &mut BTreeSet<String>,
) -> Option<()> {
    let keywords = schema.json.pointer(&pointer)?.as_object()?;
    reachable.schema_nodes.push((keywords, is_holder));
    if let Some(reference) = keywords.get("$ref").and_then(JsonValue::as_str) {
        match schema.resources.reference_target(&pointer, reference) {
            Some(target) => {
                if visited_references.insert(target.clone()) {
                    collect_reachable(schema, target, false, reachable, visited_references);
                }
            }
            None => reachable.reaches_other_document = true,
        }
    }
    for keyword in REACHING_KEYWORDS {
        let Some(value) = keywords.get(keyword) else {
            continue;
        };
        for child in child_schemas(keyword, value) {
            let child_pointer = format!("{pointer}{}", child.pointer_suffix);
            collect_reachable(schema, child_pointer, false, reachable, visited_references);
        }
    }
    Some(())
}

/// The candidate closest in spelling to `name`, where one is close enough to be a
/// likely fix and no other is as close.
pub fn closest_name(name: &str, candidates: &[String]) -> Option<String> {
    let mut ranked: Vec<(usize, &String)> = candidates
        .iter()
        .filter(|candidate| candidate.as_str() != name)
        .map(|candidate| (edit_distance(name, candidate), candidate))
        .collect();
    ranked.sort();
    let limit = (name.chars().count() / 3).max(1);
    match ranked.as_slice() {
        [(distance, best), rest @ ..]
            if *distance <= limit && rest.first().is_none_or(|(next, _)| next > distance) =>
        {
            Some((*best).clone())
        }
        _ => None,
    }
}

/// The fewest single-character insertions, deletions and substitutions turning
/// one text into the other.
fn edit_distance(first: &str, second: &str) -> usize {
    let second: Vec<char> = second.chars().collect();
    let mut previous: Vec<usize> = (0..=second.len()).collect();
    for (row, first_character) in first.chars().enumerate() {
        let mut current = vec![row + 1];
        for (column, second_character) in second.iter().enumerate() {
            let substitution = previous[column] + usize::from(first_character != *second_character);
            current.push(
                substitution
                    .min(previous[column + 1] + 1)
                    .min(current[column] + 1),
            );
        }
        previous = current;
    }
    previous[second.len()]
}

fn json_type(value: &JsonValue) -> &'static str {
    match value {
        JsonValue::Null => "null",
        JsonValue::Bool(_) => "boolean",
        JsonValue::Number(number) if number.is_i64() || number.is_u64() => "integer",
        JsonValue::Number(_) => "number",
        JsonValue::String(_) => "string",
        JsonValue::Array(_) => "array",
        JsonValue::Object(_) => "object",
    }
}

fn with_article(noun: &str) -> String {
    let article = if noun.starts_with(['a', 'e', 'i', 'o', 'u']) {
        "an"
    } else {
        "a"
    };
    format!("{article} {noun}")
}

struct Instance<'r> {
    rules: &'r ValidationRules,
    table: &'r FunctionTable,
    /// The position of the key holding each value, by JSON pointer.
    positions: BTreeMap<String, Position>,
    /// The position of each value itself, by JSON pointer.
    value_positions: BTreeMap<String, Position>,
    unknown: BTreeSet<String>,
    /// The values a merge base key copied into an entry, by JSON pointer.
    copied: BTreeSet<String>,
    /// The `merge` macro calls with an argument not known, by JSON pointer.
    merge_calls: BTreeSet<String>,
}

impl Instance<'_> {
    fn json(&mut self, value: &Value, pointer: String, key_position: &Position) -> JsonValue {
        self.positions.insert(pointer.clone(), key_position.clone());
        self.value_positions
            .insert(pointer.clone(), value.position.clone());
        match &value.local_value().kind {
            ValueKind::Null => JsonValue::Null,
            ValueKind::Bool(flag) => JsonValue::Bool(*flag),
            ValueKind::Number(text) => number_json(text),
            ValueKind::String(text) => JsonValue::String(text.clone()),
            ValueKind::List(items) => JsonValue::Array(
                items
                    .iter()
                    .enumerate()
                    .map(|(index, item)| {
                        self.json(item, format!("{pointer}/{index}"), &item.position)
                    })
                    .collect(),
            ),
            ValueKind::Map(entries) => {
                let mut object = Map::new();
                for entry in entries {
                    let child = format!("{pointer}/{}", escape_pointer(&entry.key));
                    let mut json = self.json(&entry.value, child, &entry.key_position);
                    if let JsonValue::Object(fields) = &mut json {
                        self.set_child_value(&entry.key, fields);
                    }
                    object.insert(entry.key.clone(), json);
                }
                self.merge_base_keys(&mut object, &pointer);
                JsonValue::Object(object)
            }
            ValueKind::Call(call) => match self.merge_call_content(call, &value.position) {
                Some(content) => {
                    let holds_unknown_argument = call.arguments.iter().any(|argument| {
                        matches!(
                            argument.local_value().kind,
                            ValueKind::Reference(_) | ValueKind::Call(_) | ValueKind::Template(_)
                        )
                    });
                    if holds_unknown_argument {
                        self.merge_calls.insert(pointer.clone());
                    }
                    self.json(&content, pointer, key_position)
                }
                None => {
                    self.unknown.insert(pointer);
                    JsonValue::Null
                }
            },
            ValueKind::Reference(_) | ValueKind::Template(_) => {
                self.unknown.insert(pointer);
                JsonValue::Null
            }
        }
    }

    /// A call of a `merge` macro of the schema's function table, as the known maps
    /// among its arguments merged in order by the macro's `merge_behavior`; none
    /// for another call, or one with no known map among its arguments.
    fn merge_call_content(&self, call: &Call, position: &Position) -> Option<Value> {
        let macro_entry = self.table.entry(&call.function)?.macro_entry.as_ref()?;
        if macro_entry.kind != MacroKind::Merge {
            return None;
        }
        let known_maps: Vec<&[MapEntry]> = call
            .arguments
            .iter()
            .filter_map(|argument| argument.local_value().entries())
            .collect();
        if known_maps.is_empty() {
            return None;
        }
        let merged_entries = macro_entry.merge_behavior.merge_contents(&known_maps);
        Some(Value::new(ValueKind::Map(merged_entries), position.clone()))
    }

    /// Whether the value at `pointer` requires no key: the content of a merge base
    /// key, or of a `merge` macro call with an argument not known, or a value
    /// below one.
    fn requires_no_key(&self, pointer: &str) -> bool {
        pointer
            .split('/')
            .skip(1)
            .any(|step| self.rules.merge_base_keys.contains(&unescape_pointer(step)))
            || self.merge_calls.iter().any(|merge_call| {
                pointer == merge_call || pointer.starts_with(&format!("{merge_call}/"))
            })
    }

    /// A key such as `name mock-orders` sets the field `name` of the entry it keys,
    /// unless the entry writes the field itself.
    fn set_child_value(&self, key: &str, fields: &mut Map<String, JsonValue>) {
        if let Some((field, value)) = self.rules.child_value(key) {
            fields.entry(field).or_insert(JsonValue::String(value));
        }
    }

    /// Merges the content of each merge base key `object` holds into each sibling
    /// entry that is a map, the entry over the content.
    fn merge_base_keys(&mut self, object: &mut Map<String, JsonValue>, pointer: &str) {
        let base_keys: Vec<String> = self
            .rules
            .merge_base_keys
            .iter()
            .filter(|base_key| object.contains_key(base_key.as_str()))
            .cloned()
            .collect();
        for base_key in base_keys {
            let base = object[&base_key].clone();
            let keys: Vec<String> = object.keys().cloned().collect();
            for key in keys {
                if self.rules.merge_base_keys.contains(&key) {
                    continue;
                }
                let Some(JsonValue::Object(written)) = object.get(&key) else {
                    continue;
                };
                let entry_pointer = format!("{pointer}/{}", escape_pointer(&key));
                let merged = merged_over(&base, written, &entry_pointer, &|at| self.is_unknown(at));
                let copied =
                    copied_pointers(&merged, &JsonValue::Object(written.clone()), &entry_pointer);
                self.copied.extend(copied);
                object.insert(key, merged);
            }
        }
    }

    /// Whether `pointer` names a value a merge base key copied in, or one below it.
    fn is_copied(&self, pointer: &str) -> bool {
        self.copied
            .iter()
            .any(|copied| pointer == copied || pointer.starts_with(&format!("{copied}/")))
    }

    /// Whether `error`, its instance path read below `instance_base`, comes from a
    /// value not known when the document compiles: the value itself, a check that
    /// reads a value below it through a subschema or compares the values below it,
    /// or alternatives with a form whose every failure comes from one.
    fn reads_unknown(&self, instance_base: &str, error: &ValidationError<'_>) -> bool {
        let pointer = format!("{instance_base}{}", error.instance_path().as_str());
        if self.is_unknown(&pointer) {
            return true;
        }
        match error.kind() {
            ValidationErrorKind::AnyOf { context }
            | ValidationErrorKind::OneOfNotValid { context } => {
                context.iter().any(|form_failures| {
                    !form_failures.is_empty()
                        && form_failures
                            .iter()
                            .all(|form_failure| self.reads_unknown(instance_base, form_failure))
                })
            }
            ValidationErrorKind::OneOfMultipleValid { .. }
            | ValidationErrorKind::Not { .. }
            | ValidationErrorKind::Contains
            | ValidationErrorKind::UniqueItems => self.holds_unknown_below(&pointer),
            _ => false,
        }
    }

    fn member_reads_unknown(&self, pointer: &str, schema_could_read: impl Fn() -> bool) -> bool {
        self.holds_unknown_below(pointer) && schema_could_read()
    }

    fn holds_unknown_below(&self, pointer: &str) -> bool {
        let below = format!("{pointer}/");
        self.unknown
            .iter()
            .any(|unknown| unknown.starts_with(&below))
    }

    fn is_unknown(&self, pointer: &str) -> bool {
        self.unknown
            .iter()
            .any(|unknown| pointer == unknown || pointer.starts_with(&format!("{unknown}/")))
    }

    fn value_position(&self, pointer: &str) -> &Position {
        self.value_positions
            .get(pointer)
            .unwrap_or_else(|| self.position(pointer))
    }

    fn position(&self, pointer: &str) -> &Position {
        let mut current = pointer;
        loop {
            if let Some(position) = self.positions.get(current) {
                return position;
            }
            current = current.rsplit_once('/').map_or("", |(parent, _)| parent);
        }
    }
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use super::*;
    use crate::definition::QuotedValueType;
    use crate::resolved::{Call, MapEntry};
    use fancy_regex::Regex;

    fn position() -> Position {
        Position::file_start(&Arc::from("mock.yml"))
    }

    fn map(entries: Vec<(&str, Value)>) -> Value {
        Value::new(
            ValueKind::Map(
                entries
                    .into_iter()
                    .map(|(key, value)| MapEntry {
                        key: key.to_owned(),
                        key_position: position(),
                        value,
                    })
                    .collect(),
            ),
            position(),
        )
    }

    fn schema() -> LoadedSchema {
        let text = r#"{
            "type": "object",
            "properties": {
                "queue": {
                    "type": "object",
                    "additionalProperties": {
                        "type": "object",
                        "required": ["name", "visibility"],
                        "properties": {
                            "name": {"type": "string"},
                            "visibility": {"type": "integer"},
                            "encrypted": {"type": "boolean"}
                        }
                    }
                }
            }
        }"#;
        LoadedSchema::parse("mock.schema.json", text).unwrap()
    }

    fn messages(diagnostics: &[Diagnostic]) -> Vec<&str> {
        diagnostics
            .iter()
            .map(|diagnostic| diagnostic.message.as_str())
            .collect()
    }

    #[test]
    fn a_key_sets_its_entry_field_before_validation() {
        let document = map(vec![(
            "queue",
            map(vec![(
                "name mock-orders",
                map(vec![(
                    "visibility",
                    Value::new(ValueKind::Number("30".into()), position()),
                )]),
            )]),
        )]);
        let rules = ValidationRules {
            child_value_patterns: vec![Regex::new("^(?<field>[a-z]+) (?<value>[a-z-]+)$").unwrap()],
            ..ValidationRules::default()
        };
        assert!(schema().validate(&document, &rules).is_empty());
    }

    #[test]
    fn a_merge_base_key_supplies_its_siblings_and_a_quoted_value_reads_as_its_type() {
        let number = |text: &str| Value::new(ValueKind::Number(text.into()), position());
        let document = map(vec![(
            "queue",
            map(vec![
                (
                    "defaults",
                    map(vec![
                        ("name", Value::string("mock", position())),
                        ("visibility", number("30")),
                    ]),
                ),
                (
                    "orders",
                    map(vec![("encrypted", Value::string("true", position()))]),
                ),
                (
                    "payments",
                    map(vec![("encrypted", Value::string("yes", position()))]),
                ),
            ]),
        )]);
        let rules = ValidationRules {
            merge_base_keys: vec!["defaults".to_owned()],
            quoted_value_types: vec![QuotedValueType::Boolean],
            ..ValidationRules::default()
        };
        assert_eq!(
            messages(&schema().validate(&document, &rules)),
            ["`encrypted` must be `boolean`"]
        );
    }

    #[test]
    fn a_call_is_not_checked_and_a_wrong_type_is() {
        let call = Value::new(ValueKind::Call(Call::new("mock", Vec::new())), position());
        let document = map(vec![(
            "queue",
            map(vec![(
                "orders",
                map(vec![
                    ("name", call),
                    ("visibility", Value::string("thirty", position())),
                ]),
            )]),
        )]);
        let diagnostics = schema().validate(&document, &ValidationRules::default());
        assert_eq!(messages(&diagnostics), ["`visibility` must be `integer`"]);
        let notes = diagnostics[0].notes.as_deref().unwrap();
        assert_eq!(notes.label.as_deref(), Some("is a string"));
        assert_eq!(
            notes.rule.as_deref(),
            Some(
                "type, at mock.schema.json#/properties/queue/additionalProperties/properties/visibility/type"
            )
        );
    }

    #[test]
    fn each_problem_names_the_document_value_it_is_about() {
        let document = map(vec![(
            "queue",
            map(vec![(
                "orders",
                map(vec![("visibility", Value::string("thirty", position()))]),
            )]),
        )]);
        let problems = schema().problems(&document, &ValidationRules::default());
        let subjects: Vec<&str> = problems
            .iter()
            .map(|problem| problem.subject.as_str())
            .collect();
        assert_eq!(subjects, ["/queue/orders", "/queue/orders/visibility"]);
    }

    #[test]
    fn a_fragment_names_its_rules_in_the_whole_schema_and_requires_no_field() {
        let text = r##"{
            "$schema": "https://json-schema.org/draft/2020-12/schema",
            "$defs": {
                "queue": {
                    "type": "object",
                    "required": ["name"],
                    "properties": {"tags": {"$ref": "#/$defs/tags"}}
                },
                "tags": {"type": "array"}
            }
        }"##;
        let schema = LoadedSchema::parse("mock.schema.json", text).unwrap();
        let document = map(vec![("tags", Value::string("mock", position()))]);
        let problems =
            schema.fragment_problems("/$defs/queue", &document, &ValidationRules::default());
        let reports: Vec<(&str, &str, Option<&str>)> = problems
            .iter()
            .map(|problem| {
                let notes = problem.diagnostic.notes.as_deref().unwrap();
                (
                    problem.subject.as_str(),
                    problem.diagnostic.message.as_str(),
                    notes.rule.as_deref(),
                )
            })
            .collect();
        assert_eq!(
            reports,
            [(
                "/tags",
                "`tags` must be `array`",
                Some("type, at mock.schema.json#/$defs/tags/type")
            )]
        );
    }

    #[test]
    fn a_misspelt_field_or_value_is_reported_at_itself_with_the_closest_allowed_one() {
        let text = r#"{
            "type": "object",
            "properties": {
                "queue": {
                    "type": "object",
                    "properties": {
                        "branch": {"type": "string"},
                        "tier": {"enum": ["gold", "silver", "bronze"]}
                    },
                    "additionalProperties": false
                }
            }
        }"#;
        let schema = LoadedSchema::parse("mock.schema.json", text).unwrap();
        let file: Arc<str> = Arc::from("mock.yml");
        let at = |line: usize| Position::new(&file, line, 3);
        let entry = |key: &str, line: usize, text: &str| MapEntry {
            key: key.to_owned(),
            key_position: at(line),
            value: Value::string(text, Position::new(&file, line, 9)),
        };
        let queue = Value::new(
            ValueKind::Map(vec![entry("brnch", 2, "main"), entry("tier", 3, "gld")]),
            at(1),
        );
        let document = Value::new(
            ValueKind::Map(vec![MapEntry {
                key: "queue".to_owned(),
                key_position: Position::new(&file, 1, 1),
                value: queue,
            }]),
            Position::new(&file, 1, 1),
        );
        let mut diagnostics = schema.validate(&document, &ValidationRules::default());
        diagnostics.sort_by_key(|diagnostic| diagnostic.position.as_ref().map(|at| at.line));
        let reports: Vec<(usize, usize, &str, Option<&str>)> = diagnostics
            .iter()
            .map(|diagnostic| {
                let position = diagnostic.position.as_ref().unwrap();
                let notes = diagnostic.notes.as_deref().unwrap();
                (
                    position.line,
                    position.column,
                    diagnostic.message.as_str(),
                    notes.help.as_deref(),
                )
            })
            .collect();
        assert_eq!(
            reports,
            [
                (
                    2,
                    3,
                    "`brnch` is not defined in the schema",
                    Some("did you mean `branch`?")
                ),
                (
                    3,
                    9,
                    "`tier` must be one of `\"gold\"`, `\"silver\"`, `\"bronze\"`",
                    Some("did you mean `gold`?")
                ),
            ]
        );
    }

    fn number(text: &str) -> Value {
        Value::new(ValueKind::Number(text.into()), position())
    }

    fn unknown_value() -> Value {
        Value::new(ValueKind::Call(Call::new("mock", Vec::new())), position())
    }

    fn subjects(problems: &[SchemaProblem]) -> Vec<(&str, &str)> {
        problems
            .iter()
            .map(|problem| {
                (
                    problem.subject.as_str(),
                    problem.diagnostic.message.as_str(),
                )
            })
            .collect()
    }

    #[test]
    fn a_merge_base_key_content_requires_no_key() {
        let text = r#"{
            "type": "object",
            "properties": {
                "queue": {
                    "type": "object",
                    "additionalProperties": {
                        "type": "object",
                        "required": ["name", "visibility"],
                        "properties": {"visibility": {"type": "integer"}},
                        "anyOf": [{"required": ["fifo"]}, {"required": ["standard"]}]
                    }
                }
            }
        }"#;
        let schema = LoadedSchema::parse("mock.schema.json", text).unwrap();
        let document = map(vec![(
            "queue",
            map(vec![
                (
                    "defaults",
                    map(vec![("visibility", Value::string("thirty", position()))]),
                ),
                (
                    "orders",
                    map(vec![("name", Value::string("mock", position()))]),
                ),
            ]),
        )]);
        let rules = ValidationRules {
            merge_base_keys: vec!["defaults".to_owned()],
            ..ValidationRules::default()
        };
        assert_eq!(
            subjects(&schema.problems(&document, &rules)),
            [
                (
                    "/queue/orders",
                    "`orders` matches none of its allowed forms"
                ),
                (
                    "/queue/defaults/visibility",
                    "`visibility` must be `integer`"
                ),
            ]
        );
    }

    #[test]
    fn a_merge_call_is_validated_as_its_known_maps_merged_requiring_no_key_beside_an_unknown_one() {
        let text = r#"{
            "x-yaml-dsl-functions": {
                "merge": {
                    "arguments": [{"name": "contents", "repeated": true}],
                    "macro": {"kind": "merge", "content": "contents", "merge_behavior": "deep"}
                },
                "mock_join": {
                    "arguments": [{"name": "parts", "repeated": true}],
                    "macro": {"kind": "function_call", "callee": "parts"}
                }
            },
            "type": "object",
            "additionalProperties": {
                "type": "object",
                "required": ["name", "visibility"],
                "properties": {
                    "visibility": {"type": "integer"},
                    "tags": {"type": "object", "required": ["team"]}
                }
            }
        }"#;
        let schema = LoadedSchema::parse("mock.schema.json", text).unwrap();
        let file: Arc<str> = Arc::from("mock.yml");
        let first_map = Value::new(
            ValueKind::Map(vec![MapEntry {
                key: "visibility".to_owned(),
                key_position: Position::new(&file, 2, 3),
                value: Value::string("thirty", Position::new(&file, 2, 15)),
            }]),
            Position::new(&file, 2, 3),
        );
        let second_map = map(vec![("tags", map(vec![("tier", number("1"))]))]);
        let merge_call = |function: &str, arguments: Vec<Value>| {
            Value::new(ValueKind::Call(Call::new(function, arguments)), position())
        };
        let document = map(vec![
            (
                "orders",
                merge_call("merge", vec![unknown_value(), first_map, second_map]),
            ),
            ("payments", merge_call("merge", vec![unknown_value()])),
            (
                "refunds",
                merge_call("mock_join", vec![map(vec![("tags", number("1"))])]),
            ),
            (
                "invoices",
                merge_call(
                    "merge",
                    vec![
                        map(vec![("visibility", number("30"))]),
                        Value::string("mock", position()),
                    ],
                ),
            ),
        ]);
        let problems = schema.problems(&document, &ValidationRules::default());
        assert_eq!(
            subjects(&problems),
            [
                ("/invoices", "`name` is required"),
                ("/orders/visibility", "`visibility` must be `integer`"),
            ]
        );
        let at = problems[1].diagnostic.position.as_ref().unwrap();
        assert_eq!((at.line, at.column), (2, 15));
    }

    #[test]
    fn alternatives_beside_an_unknown_value_are_reported_when_every_form_fails_certainly() {
        let text = r#"{
            "type": "object",
            "additionalProperties": {
                "anyOf": [
                    {"required": ["origins", "methods"], "properties": {"origins": {"type": "array"}}},
                    {"required": ["wildcard"]}
                ]
            }
        }"#;
        let schema = LoadedSchema::parse("mock.schema.json", text).unwrap();
        let document = map(vec![
            ("doors", map(vec![("origins", unknown_value())])),
            (
                "gates",
                map(vec![
                    ("origins", unknown_value()),
                    ("methods", Value::string("GET", position())),
                ]),
            ),
        ]);
        assert_eq!(
            subjects(&schema.problems(&document, &ValidationRules::default())),
            [("/doors", "`doors` matches none of its allowed forms")]
        );
    }

    #[test]
    fn a_dependency_reached_through_a_reference_names_every_dependency_of_its_value() {
        let text = r##"{
            "$schema": "https://json-schema.org/draft/2020-12/schema",
            "$defs": {"mock_form": {"dependentRequired": {"label": ["size"]}}},
            "type": "object",
            "properties": {
                "mock": {
                    "dependentRequired": {"label": ["kind"]},
                    "$ref": "#/$defs/mock_form"
                }
            }
        }"##;
        let schema = LoadedSchema::parse("mock.schema.json", text).unwrap();
        let document = map(vec![(
            "mock",
            map(vec![
                ("label", Value::string("mock-label", position())),
                ("kind", Value::string("mock-kind", position())),
            ]),
        )]);
        assert_eq!(
            subjects(&schema.problems(&document, &ValidationRules::default())),
            [("/mock/label", "`label` needs `kind`; `size`")]
        );
    }

    #[test]
    fn a_schema_whose_reference_or_pattern_does_not_hold_does_not_load() {
        let load = |text: &str| LoadedSchema::parse("mock.schema.json", text).err();
        assert_eq!(
            load(
                r##"{"properties": {
                    "first": {"$ref": "#/$defs/mock-missing"},
                    "second": {"pattern": "(_mock_"}
                }}"##
            )
            .as_deref(),
            Some("schema mock.schema.json: `#/$defs/mock-missing` names no node of the schema")
        );
        assert_eq!(
            load(
                r##"{"patternProperties": {"(_mock_": {}}, "$defs": {"x": {"$ref": "#/nothing"}}}"##
            )
            .as_deref(),
            Some("schema mock.schema.json: `(_mock_` is not a pattern")
        );
        let loads = [
            r##"{"$defs": {"a b": {}}, "properties": {"mock": {"$ref": "#/$defs/a%20b"}}}"##,
            r##"{"$schema": "https://json-schema.org/draft/2020-12/schema",
                "properties": {"mock": {"$id": "https://mock.example/mock.json",
                "$defs": {"inner": {}}, "properties": {"field": {"$ref": "#/$defs/inner"}}}}}"##,
            r##"{"$schema": "https://json-schema.org/draft/2020-12/schema",
                "$defs": {"inner": {"$anchor": "mock-anchor"}},
                "properties": {"mock": {"$ref": "#mock-anchor"}}}"##,
            r##"{"properties": {"default": {"type": "string"}},
                "default": {"$ref": "#/nothing", "pattern": "(_mock_"},
                "examples": [{"$ref": "#/nothing"}]}"##,
        ];
        for text in loads {
            assert_eq!(load(text), None, "{text}");
        }
    }

    #[test]
    fn a_rule_inside_an_embedded_resource_names_where_the_whole_schema_holds_it() {
        let text = r##"{
            "$schema": "https://json-schema.org/draft/2020-12/schema",
            "properties": {"mock": {"$ref": "#/$defs/mock_resource"}},
            "$defs": {
                "mock_resource": {
                    "$id": "https://mock.example/mock-resource.json",
                    "properties": {"size": {"$ref": "#/$defs/mock_size"}},
                    "$defs": {"mock_size": {"type": "integer"}}
                }
            }
        }"##;
        let schema = LoadedSchema::parse("mock.schema.json", text).unwrap();
        let document = map(vec![(
            "mock",
            map(vec![("size", Value::string("mock-size", position()))]),
        )]);
        let diagnostics = schema.validate(&document, &ValidationRules::default());
        let notes = diagnostics[0].notes.as_deref().unwrap();
        assert_eq!(
            notes.rule.as_deref(),
            Some("type, at mock.schema.json#/$defs/mock_resource/$defs/mock_size/type")
        );
    }

    #[test]
    fn a_check_through_a_subschema_reading_an_unknown_value_is_not_made() {
        let text = r#"{
            "type": "object",
            "properties": {
                "secrets": {
                    "type": "object",
                    "oneOf": [{"additionalProperties": {"type": "string"}}]
                },
                "count": {"oneOf": [{"type": "integer"}]}
            }
        }"#;
        let schema = LoadedSchema::parse("mock.schema.json", text).unwrap();
        let call = Value::new(ValueKind::Call(Call::new("mock", Vec::new())), position());
        let document = map(vec![
            ("secrets", map(vec![("token", call)])),
            ("count", Value::string("three", position())),
        ]);
        let diagnostics = schema.validate(&document, &ValidationRules::default());
        assert_eq!(
            messages(&diagnostics),
            ["`count` matches none of its allowed forms"]
        );
    }
}
