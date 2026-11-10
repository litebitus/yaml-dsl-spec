//! Runs the repo's conformance cases, each a folder whose `expected.json` states
//! what its input reads to:
//!
//! - `conformance/definitions/`: a `yaml-dsl.yml`, to its DSLs or its problems;
//! - `conformance/yaml/`: an `input.yml`, read with the tag keys of an optional
//!   `tag-keys.json`, to its node tree or its problem;
//! - `conformance/schema/`: a `document.yml` validated against a `schema.json`, read
//!   by the DSL `validator` block of an optional `validator.json`, each value at a
//!   pointer of an optional `unknown-values.json` read as a reference, each map
//!   at a pointer of an optional `merge-calls.json` read as a call of the macro
//!   it names, its content not known, against the schema node an optional
//!   `fragment.json` points at, to its problems and
//!   the subjects of an optional `subjects.json`, or to why the schema is not read;
//! - `conformance/patterns/cases.json`: each pattern, to its named groups and the
//!   captures of each text, or to not compiling.

use std::collections::BTreeMap;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use fancy_regex::Regex;
use serde_json::{Map, Value, json};
use yaml_dsl_spec::definition::{TagKey, Validator};
use yaml_dsl_spec::resolved::{
    Call, MapEntry, Reference, Target, Value as ResolvedValue, ValueKind,
};
use yaml_dsl_spec::schema::SchemaCache;
use yaml_dsl_spec::{
    Diagnostic, Node, NodeKind, ValidationRules, read_dsl_definitions, yaml_reader,
};

fn conformance_folder() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../conformance")
}

/// The case folders of a category holding `input`, beside the crate in a
/// checkout; absent from a published crate.
fn case_folders(category: &str, input: &str) -> Vec<PathBuf> {
    let Ok(entries) = fs::read_dir(conformance_folder().join(category)) else {
        return Vec::new();
    };
    let mut folders: Vec<_> = entries
        .filter_map(|entry| Some(entry.ok()?.path()))
        .filter(|path| path.join(input).is_file())
        .collect();
    folders.sort();
    folders
}

/// Each case of a category read to what it reads to, and every case whose
/// `expected.json` states otherwise, with what it read to.
fn mismatches_of(category: &str, input: &str, reading: impl Fn(&Path) -> Value) -> Vec<String> {
    let folders = case_folders(category, input);
    if conformance_folder().is_dir() {
        assert!(!folders.is_empty(), "no {category} cases");
    }
    let mut mismatches = Vec::new();
    for folder in folders {
        let actual = reading(&folder);
        let expected: Option<Value> = fs::read_to_string(folder.join("expected.json"))
            .ok()
            .map(|expected| serde_json::from_str(&expected).unwrap());
        if expected.as_ref() != Some(&actual) {
            mismatches.push(format!(
                "{}/{}: {}",
                category,
                folder.file_name().unwrap().to_string_lossy(),
                serde_json::to_string_pretty(&actual).unwrap()
            ));
        }
    }
    mismatches
}

fn problem_json(problem: &Diagnostic) -> Value {
    let mut fields = Map::new();
    if let Some(position) = &problem.position {
        fields.insert("line".to_owned(), json!(position.line));
        fields.insert("column".to_owned(), json!(position.column));
    }
    fields.insert("message".to_owned(), json!(problem.message));
    if let Some(notes) = problem.notes.as_deref() {
        for (name, note) in [
            ("label", &notes.label),
            ("rule", &notes.rule),
            ("schema", &notes.schema),
            ("help", &notes.help),
        ] {
            if let Some(note) = note {
                fields.insert(name.to_owned(), json!(note));
            }
        }
    }
    Value::Object(fields)
}

fn node_json(node: &Node) -> Value {
    let mut fields = Map::new();
    let kind = match &node.kind {
        NodeKind::Null => "null",
        NodeKind::Bool(_) => "bool",
        NodeKind::Number(_) => "number",
        NodeKind::String(_) => "string",
        NodeKind::List(_) => "list",
        NodeKind::Map(_) => "map",
    };
    fields.insert("kind".to_owned(), json!(kind));
    fields.insert("line".to_owned(), json!(node.position.line));
    fields.insert("column".to_owned(), json!(node.position.column));
    match &node.kind {
        NodeKind::Null => {}
        NodeKind::Bool(flag) => {
            fields.insert("value".to_owned(), json!(flag));
        }
        NodeKind::Number(text) | NodeKind::String(text) => {
            fields.insert("value".to_owned(), json!(text));
        }
        NodeKind::List(items) => {
            fields.insert(
                "items".to_owned(),
                Value::Array(items.iter().map(node_json).collect()),
            );
        }
        NodeKind::Map(entries) => {
            let entries = entries
                .iter()
                .map(|entry| {
                    json!({
                        "key": entry.key,
                        "line": entry.key_position.line,
                        "column": entry.key_position.column,
                        "value": node_json(&entry.value),
                    })
                })
                .collect();
            fields.insert("entries".to_owned(), Value::Array(entries));
        }
    }
    Value::Object(fields)
}

fn optional_json(path: &Path) -> Option<Value> {
    fs::read_to_string(path)
        .ok()
        .map(|text| serde_json::from_str(&text).unwrap())
}

fn definitions_reading(folder: &Path) -> Value {
    let text = fs::read_to_string(folder.join("yaml-dsl.yml")).unwrap();
    match read_dsl_definitions(Path::new("yaml-dsl.yml"), &text) {
        Ok(declared) => json!({
            "dsls": declared.iter().map(|dsl| dsl.definition.id.clone()).collect::<Vec<_>>()
        }),
        Err(problems) => json!({
            "problems": problems.iter().map(problem_json).collect::<Vec<_>>()
        }),
    }
}

fn yaml_reading(folder: &Path) -> Value {
    let text = fs::read_to_string(folder.join("input.yml")).unwrap();
    let tag_keys: Vec<TagKey> = optional_json(&folder.join("tag-keys.json"))
        .map(|tag_keys| serde_json::from_value(tag_keys).unwrap())
        .unwrap_or_default();
    match yaml_reader::parse(&Arc::from("input.yml"), &text, &tag_keys) {
        Ok(node) => json!({ "node": node_json(&node) }),
        Err(problem) => json!({ "problem": problem_json(&problem) }),
    }
}

fn schema_reading(folder: &Path) -> Value {
    let schema = fs::read_to_string(folder.join("schema.json")).unwrap();
    let document = fs::read_to_string(folder.join("document.yml")).unwrap();
    let validator: Option<Validator> = optional_json(&folder.join("validator.json"))
        .map(|validator| serde_json::from_value(validator).unwrap());
    let rules = ValidationRules::from_validator(validator.as_ref()).unwrap();
    let root = yaml_reader::parse(&Arc::from("document.yml"), &document, &[]).unwrap();
    let loaded = match SchemaCache::default().load("schema.json", &schema) {
        Ok(loaded) => loaded,
        Err(message) => return json!({ "error": message }),
    };
    let unknown_values = UnknownValues {
        pointers: optional_json(&folder.join("unknown-values.json"))
            .map(|pointers| serde_json::from_value(pointers).unwrap())
            .unwrap_or_default(),
        merge_calls: optional_json(&folder.join("merge-calls.json"))
            .map(|merge_calls| serde_json::from_value(merge_calls).unwrap())
            .unwrap_or_default(),
    };
    let document = with_unknown_values(ResolvedValue::from_node(&root), "", &unknown_values);
    let fragment_pointer = optional_json(&folder.join("fragment.json"))
        .map(|fragment| fragment["pointer"].as_str().unwrap().to_owned());
    let problems = match &fragment_pointer {
        Some(pointer) => loaded.fragment_problems(pointer, &document, &rules),
        None => loaded.problems(&document, &rules),
    };
    if let Some(expected_subjects) = optional_json(&folder.join("subjects.json")) {
        let subjects: Vec<&str> = problems
            .iter()
            .map(|problem| problem.subject.as_str())
            .collect();
        assert_eq!(json!(subjects), expected_subjects, "{}", folder.display());
    }
    let problems: Vec<Value> = problems
        .iter()
        .map(|problem| problem_json(&problem.diagnostic))
        .collect();
    json!({ "problems": problems })
}

/// The values of a case not known when its document compiles: a reference to a
/// declaration at each pointer, and a call of the named `merge` macro at each
/// pointer of `merge_calls`, its arguments a reference and the map as written.
struct UnknownValues {
    pointers: Vec<String>,
    merge_calls: BTreeMap<String, String>,
}

fn declaration_reference(value: &ResolvedValue) -> ResolvedValue {
    let reference = Reference {
        scope: "mock".to_owned(),
        scope_key: None,
        name: "mock".to_owned(),
        path: String::new(),
        text: value.scalar_text().unwrap_or_default(),
        target: Target::Declaration {
            position: value.position.clone(),
        },
    };
    ResolvedValue::new(ValueKind::Reference(reference), value.position.clone())
}

fn with_unknown_values(
    value: ResolvedValue,
    pointer: &str,
    unknown_values: &UnknownValues,
) -> ResolvedValue {
    if unknown_values
        .pointers
        .iter()
        .any(|unknown| unknown == pointer)
    {
        return declaration_reference(&value);
    }
    let value = with_unknown_values_below(value, pointer, unknown_values);
    match unknown_values.merge_calls.get(pointer) {
        Some(macro_name) => {
            let position = value.position.clone();
            let call = Call::new(macro_name, vec![declaration_reference(&value), value]);
            ResolvedValue::new(ValueKind::Call(call), position)
        }
        None => value,
    }
}

fn with_unknown_values_below(
    value: ResolvedValue,
    pointer: &str,
    unknown_values: &UnknownValues,
) -> ResolvedValue {
    let position = value.position.clone();
    let kind = match value.kind {
        ValueKind::Map(entries) => ValueKind::Map(
            entries
                .into_iter()
                .map(|entry| {
                    let child = format!(
                        "{pointer}/{}",
                        entry.key.replace('~', "~0").replace('/', "~1")
                    );
                    MapEntry {
                        value: with_unknown_values(entry.value, &child, unknown_values),
                        ..entry
                    }
                })
                .collect(),
        ),
        ValueKind::List(items) => ValueKind::List(
            items
                .into_iter()
                .enumerate()
                .map(|(index, item)| {
                    with_unknown_values(item, &format!("{pointer}/{index}"), unknown_values)
                })
                .collect(),
        ),
        other => other,
    };
    ResolvedValue::new(kind, position)
}

fn pattern_reading(case: &Value) -> Value {
    let pattern = case["pattern"].as_str().unwrap();
    let Ok(compiled) = Regex::new(pattern) else {
        return json!({ "pattern": pattern, "compiles": false });
    };
    let groups: Vec<&str> = compiled.capture_names().flatten().collect();
    let texts: Vec<Value> = case["texts"]
        .as_array()
        .into_iter()
        .flatten()
        .map(|text| {
            let text = text.as_str().unwrap();
            let captures = compiled.captures(text).unwrap().map(|captures| {
                let mut named = Map::new();
                named.insert(
                    "match".to_owned(),
                    json!(captures.get(0).map(|whole| whole.as_str())),
                );
                for group in &groups {
                    named.insert(
                        (*group).to_owned(),
                        json!(captures.name(group).map(|found| found.as_str())),
                    );
                }
                Value::Object(named)
            });
            json!({ "text": text, "captures": captures })
        })
        .collect();
    json!({ "pattern": pattern, "compiles": true, "groups": groups, "texts": texts })
}

#[test]
fn every_definitions_case_reads_as_expected() {
    let mismatches = mismatches_of("definitions", "yaml-dsl.yml", definitions_reading);
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}

#[test]
fn every_yaml_case_reads_as_expected() {
    let mismatches = mismatches_of("yaml", "input.yml", yaml_reading);
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}

#[test]
fn every_schema_case_validates_as_expected() {
    let mismatches = mismatches_of("schema", "schema.json", schema_reading);
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}

#[test]
fn every_pattern_case_compiles_and_matches_as_expected() {
    let file = conformance_folder().join("patterns/cases.json");
    let Some(cases) = optional_json(&file) else {
        assert!(!conformance_folder().is_dir(), "no pattern cases");
        return;
    };
    let actual: Vec<Value> = cases["cases"]
        .as_array()
        .unwrap()
        .iter()
        .map(pattern_reading)
        .collect();
    let expected = optional_json(&conformance_folder().join("patterns/expected.json"));
    let actual = json!({ "cases": actual });
    assert_eq!(
        expected.as_ref(),
        Some(&actual),
        "{}",
        serde_json::to_string_pretty(&actual).unwrap()
    );
}
