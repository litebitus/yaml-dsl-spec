//! A document's schema, as its caller hands it over: its function table, and
//! the validation of the resolved document against it.

use std::collections::hash_map::DefaultHasher;
use std::collections::{BTreeMap, BTreeSet};
use std::hash::{Hash, Hasher};
use std::sync::{Arc, Mutex, OnceLock};

use jsonschema::error::{TypeKind, ValidationErrorKind};
use jsonschema::{Draft, ValidationError};
use serde_json::{Map, Value as JsonValue};

use crate::diagnostic::Diagnostic;
use crate::function_table::FunctionTable;
use crate::node::number_json;
use crate::position::Position;
use crate::problem_wording::{
    contains_message, dependency_message, dependency_of, described_form, duplicate_items,
    field_name, number_text,
};
use crate::requirement_markers::marked_schema;
use crate::resolved::{Value, ValueKind};
pub use crate::validation_rules::child_value;
use crate::validation_rules::{ValidationRules, copied_pointers, escape_pointer, merged_over};

pub struct LoadedSchema {
    pub table: FunctionTable,
    validator: jsonschema::Validator,
    schema: JsonValue,
    /// The schema with its requirement markers read: what a document is validated against.
    marked: JsonValue,
    /// The schema as a report names it, such as its file's name or its URL.
    name: String,
}

/// A schema parsed once, by the first unit that reads it, or why it did not parse.
type SchemaLoad = Arc<OnceLock<Result<Arc<LoadedSchema>, String>>>;

/// Schemas parsed once and shared by every unit that reads them, by name and text.
#[derive(Default)]
pub struct SchemaCache {
    loaded: Mutex<BTreeMap<(String, u64), SchemaLoad>>,
}

impl SchemaCache {
    pub fn load(&self, name: &str, text: &str) -> Result<Arc<LoadedSchema>, String> {
        let mut hasher = DefaultHasher::new();
        text.hash(&mut hasher);
        let key = (name.to_owned(), hasher.finish());
        let cell = {
            let mut loaded = self
                .loaded
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            Arc::clone(loaded.entry(key).or_default())
        };
        cell.get_or_init(|| LoadedSchema::parse(name, text).map(Arc::new))
            .clone()
    }
}

/// A problem, and the document value it is about.
struct SchemaProblem {
    subject: String,
    diagnostic: Diagnostic,
}

/// What the problems already reported cover: each dependency's key and each list
/// whose items beyond its tuple are reported once.
#[derive(Default)]
struct ReportedSubjects {
    dependencies: BTreeSet<String>,
    item_lists: BTreeSet<String>,
}

impl LoadedSchema {
    fn parse(name: &str, text: &str) -> Result<Self, String> {
        let schema: JsonValue =
            serde_json::from_str(text).map_err(|error| format!("schema {name}: {error}"))?;
        let table = FunctionTable::from_schema(&schema)
            .map_err(|error| format!("schema {name}: {error}"))?;
        let draft = schema_draft(&schema).map_err(|error| format!("schema {name}: {error}"))?;
        let marked = marked_schema(&schema, draft);
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
        })
    }

    /// Validates `document`, read by `rules`. A value not known when the document
    /// compiles, such as a call or a reference to a declaration, is not checked.
    pub fn validate(&self, document: &Value, rules: &ValidationRules) -> Vec<Diagnostic> {
        let mut instance = Instance {
            rules,
            positions: BTreeMap::new(),
            value_positions: BTreeMap::new(),
            unknown: BTreeSet::new(),
            copied: BTreeSet::new(),
        };
        let mut json = instance.json(document, String::new(), &document.position);
        self.read_quoted_values(&mut json, rules);
        let mut reported = ReportedSubjects::default();
        let mut diagnostics: Vec<Diagnostic> = self
            .validator
            .iter_errors(&json)
            .filter(|error| !instance.reads_unknown(error.instance_path().as_str(), error.kind()))
            .flat_map(|error| self.error_problems(&error, &json, &instance, &mut reported))
            .filter(|problem| !instance.is_copied(&problem.subject))
            .map(|problem| problem.diagnostic)
            .collect();
        diagnostics.sort_by(|first, second| {
            place(first)
                .cmp(&place(second))
                .then_with(|| first.message.cmp(&second.message))
        });
        diagnostics.dedup();
        diagnostics
    }

    /// Reads each quoted value that spells a type the schema expects of it, and
    /// `rules` allow, as that type, until no more are read.
    fn read_quoted_values(&self, json: &mut JsonValue, rules: &ValidationRules) {
        if rules.quoted_value_types.is_empty() {
            return;
        }
        loop {
            let mut readings: BTreeMap<String, JsonValue> = BTreeMap::new();
            for error in self.validator.iter_errors(json) {
                self.collect_quoted_readings(&error, rules, &mut readings);
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
        error: &ValidationError<'_>,
        rules: &ValidationRules,
        readings: &mut BTreeMap<String, JsonValue>,
    ) {
        match error.kind() {
            ValidationErrorKind::Type { .. } => {
                let Some(text) = error.instance().as_str() else {
                    return;
                };
                let expected = self.expected_types(error);
                if let Some(value) = rules.quoted_value(text, &expected) {
                    readings
                        .entry(error.instance_path().as_str().to_owned())
                        .or_insert(value);
                }
            }
            ValidationErrorKind::AnyOf { context }
            | ValidationErrorKind::OneOfNotValid { context } => {
                for inner in context.iter().flatten() {
                    self.collect_quoted_readings(inner, rules, readings);
                }
            }
            _ => {}
        }
    }

    /// The types a `type` error's schema lists, in the order it lists them.
    fn expected_types(&self, error: &ValidationError<'_>) -> Vec<String> {
        match self.marked.pointer(error.schema_path().as_str()) {
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
        error: &ValidationError<'_>,
        json: &JsonValue,
        instance: &Instance<'_>,
        reported: &mut ReportedSubjects,
    ) -> Vec<SchemaProblem> {
        let pointer = error.instance_path().as_str();
        let schema_path = error.schema_path().as_str();
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
            let diagnostic = Diagnostic::at(
                instance.position(&subject),
                dependency_message(&self.marked, &keyword_at, &key),
            )
            .with_label(format!("breaks `{keyword}`"))
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
            closest_name(name, &candidates).map(|candidate| format!("did you mean `{candidate}`?"))
        };
        let at_value = |message: String| {
            vec![SchemaProblem {
                subject: pointer.to_owned(),
                diagnostic: Diagnostic::at(instance.value_position(pointer), message)
                    .with_label(format!("breaks `{}`", error.kind().keyword()))
                    .with_rule(rule.clone()),
            }]
        };
        let list_limit = |limit: usize| format!("{field} must NOT have more than {limit} items");
        match error.kind() {
            ValidationErrorKind::AdditionalProperties { unexpected }
            | ValidationErrorKind::UnevaluatedProperties { unexpected } => {
                let allowed = allowed_fields(&self.schema, schema_path);
                unexpected
                    .iter()
                    .map(|name| {
                        let subject = format!("{pointer}/{}", escape_pointer(name));
                        let diagnostic = Diagnostic::at(
                            instance.position(&subject),
                            format!("`{name}` is not a field of {field}"),
                        )
                        .with_label(format!("{field} has no field `{name}`"))
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
                    format!("`{name}` is not a valid key of {field}"),
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
                let allowed: Vec<String> = listed
                    .iter()
                    .filter_map(|option| option.as_str().map(str::to_owned))
                    .collect();
                let shown: Vec<String> =
                    listed.iter().map(|option| format!("`{option}`")).collect();
                let help = error
                    .instance()
                    .as_str()
                    .and_then(|text| suggestion(text, allowed));
                let diagnostic = Diagnostic::at(
                    instance.value_position(pointer),
                    format!("{field} must be one of {}", shown.join(", ")),
                )
                .with_label(format!(
                    "not one of the {} values `enum` allows",
                    listed.len()
                ))
                .with_rule(rule)
                .with_help(help);
                vec![SchemaProblem {
                    subject: pointer.to_owned(),
                    diagnostic,
                }]
            }
            ValidationErrorKind::Type { .. } => {
                let expected = self.expected_types(error);
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
                let diagnostic =
                    Diagnostic::at(instance.position(pointer), format!("`{name}` is required"))
                        .with_label(format!("{field} has no field `{name}`"))
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
                at_value(match described_form(&self.schema, holder) {
                    Some(form) => format!("{field} must be {form}"),
                    None => format!("{field} matches none of its allowed forms"),
                })
            }
            ValidationErrorKind::OneOfMultipleValid { .. } => at_value(format!(
                "{field} matches more than one of its allowed forms"
            )),
            ValidationErrorKind::Constant { .. } => {
                at_value(format!("{field} must be equal to constant"))
            }
            ValidationErrorKind::Minimum { limit } => {
                at_value(format!("{field} must be >= {}", number_text(limit)))
            }
            ValidationErrorKind::Maximum { limit } => {
                at_value(format!("{field} must be <= {}", number_text(limit)))
            }
            ValidationErrorKind::ExclusiveMinimum { limit } => {
                at_value(format!("{field} must be > {}", number_text(limit)))
            }
            ValidationErrorKind::ExclusiveMaximum { limit } => {
                at_value(format!("{field} must be < {}", number_text(limit)))
            }
            ValidationErrorKind::MultipleOf { multiple_of } => at_value(format!(
                "{field} must be multiple of {}",
                number_text(&JsonValue::from(*multiple_of))
            )),
            ValidationErrorKind::MinLength { limit } => at_value(format!(
                "{field} must NOT have fewer than {limit} characters"
            )),
            ValidationErrorKind::MaxLength { limit } => at_value(format!(
                "{field} must NOT have more than {limit} characters"
            )),
            ValidationErrorKind::MinItems { limit } => {
                at_value(format!("{field} must NOT have fewer than {limit} items"))
            }
            ValidationErrorKind::MaxItems { limit } => at_value(list_limit(*limit as usize)),
            ValidationErrorKind::MinProperties { limit } => at_value(format!(
                "{field} must NOT have fewer than {limit} properties"
            )),
            ValidationErrorKind::MaxProperties { limit } => at_value(format!(
                "{field} must NOT have more than {limit} properties"
            )),
            ValidationErrorKind::Pattern { pattern } => {
                at_value(format!("{field} must match pattern \"{pattern}\""))
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
            ValidationErrorKind::Not { .. } => at_value(format!("{field} must NOT be valid")),
            ValidationErrorKind::AdditionalItems { limit } => at_value(list_limit(*limit)),
            ValidationErrorKind::UnevaluatedItems { unexpected } => {
                let length = error.instance().as_array().map_or(0, Vec::len);
                at_value(list_limit(length.saturating_sub(unexpected.len())))
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
                    format!("{list_field} must NOT have more than {limit} items"),
                )
                .with_label(format!("breaks `{keyword}`"))
                .with_rule(rule_at(keyword, schema_path));
                vec![SchemaProblem {
                    subject: list.to_owned(),
                    diagnostic,
                }]
            }
            ValidationErrorKind::FalseSchema => {
                at_value(format!("{field} boolean schema is false"))
            }
            _ => at_value(format!("{field} breaks `{}`", error.kind().keyword())),
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

/// Where a diagnostic stands, for reporting a document's problems in its order.
fn place(diagnostic: &Diagnostic) -> Option<(usize, usize)> {
    diagnostic
        .position
        .as_ref()
        .map(|position| (position.line, position.column))
}

/// The fields a schema node allows, by the `properties` of the node holding the
/// keyword at `keyword_pointer`, of the schemas it references and of those it
/// combines.
fn allowed_fields(schema: &JsonValue, keyword_pointer: &str) -> Vec<String> {
    let holder_pointer = keyword_pointer
        .rsplit_once('/')
        .map_or("", |(holder, _)| holder);
    let mut fields = Vec::new();
    let mut visited = BTreeSet::new();
    if let Some(holder) = schema.pointer(holder_pointer) {
        collect_fields(schema, holder, &mut fields, &mut visited);
    }
    fields
}

fn collect_fields(
    schema: &JsonValue,
    node: &JsonValue,
    fields: &mut Vec<String>,
    visited: &mut BTreeSet<String>,
) {
    if let Some(properties) = node.get("properties").and_then(JsonValue::as_object) {
        for name in properties.keys() {
            if !fields.contains(name) {
                fields.push(name.clone());
            }
        }
    }
    if let Some(reference) = node.get("$ref").and_then(JsonValue::as_str)
        && let Some(pointer) = reference.strip_prefix('#')
        && visited.insert(pointer.to_owned())
        && let Some(target) = schema.pointer(pointer)
    {
        collect_fields(schema, target, fields, visited);
    }
    for combinator in ["allOf", "anyOf", "oneOf"] {
        for branch in node
            .get(combinator)
            .and_then(JsonValue::as_array)
            .into_iter()
            .flatten()
        {
            collect_fields(schema, branch, fields, visited);
        }
    }
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
    /// The position of the key holding each value, by JSON pointer.
    positions: BTreeMap<String, Position>,
    /// The position of each value itself, by JSON pointer.
    value_positions: BTreeMap<String, Position>,
    unknown: BTreeSet<String>,
    /// The values a merge base key copied into an entry, by JSON pointer.
    copied: BTreeSet<String>,
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
            ValueKind::Reference(_) | ValueKind::Call(_) | ValueKind::Template(_) => {
                self.unknown.insert(pointer);
                JsonValue::Null
            }
        }
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

    /// Whether an error at `pointer` comes from a value not known when the document
    /// compiles: the value itself, or a check that reads a value below it through a
    /// subschema, or compares the values below it.
    fn reads_unknown(&self, pointer: &str, kind: &ValidationErrorKind) -> bool {
        if self.is_unknown(pointer) {
            return true;
        }
        let reads_below = matches!(
            kind,
            ValidationErrorKind::AnyOf { .. }
                | ValidationErrorKind::OneOfNotValid { .. }
                | ValidationErrorKind::OneOfMultipleValid { .. }
                | ValidationErrorKind::Not { .. }
                | ValidationErrorKind::Contains
                | ValidationErrorKind::UnevaluatedItems { .. }
                | ValidationErrorKind::UnevaluatedProperties { .. }
                | ValidationErrorKind::UniqueItems
        );
        let below = format!("{pointer}/");
        reads_below
            && self
                .unknown
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
            ["`encrypted` must be boolean"]
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
        assert_eq!(messages(&diagnostics), ["`visibility` must be integer"]);
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
                    "`brnch` is not a field of `queue`",
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
