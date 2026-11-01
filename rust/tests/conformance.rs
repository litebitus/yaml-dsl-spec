//! Runs the repo's conformance cases: each `yaml-dsl.yml` under
//! `conformance/definitions/` reads to the DSLs, or the problems, its
//! `expected.json` states.

use std::fs;
use std::path::{Path, PathBuf};

use serde_json::{Map, Value, json};
use yaml_dsl_spec::{Diagnostic, read_dsl_definitions};

/// The cases, beside the crate in a checkout; absent from a published crate.
fn case_folders() -> Vec<PathBuf> {
    let cases = Path::new(env!("CARGO_MANIFEST_DIR")).join("../conformance/definitions");
    let Ok(entries) = fs::read_dir(&cases) else {
        return Vec::new();
    };
    let mut folders: Vec<_> = entries
        .filter_map(|entry| Some(entry.ok()?.path()))
        .filter(|path| path.join("yaml-dsl.yml").is_file())
        .collect();
    folders.sort();
    folders
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
            ("help", &notes.help),
        ] {
            if let Some(note) = note {
                fields.insert(name.to_owned(), json!(note));
            }
        }
    }
    Value::Object(fields)
}

/// What a case reads to, in the form its `expected.json` states it.
fn reading_of(text: &str) -> Value {
    match read_dsl_definitions(Path::new("yaml-dsl.yml"), text) {
        Ok(declared) => json!({
            "dsls": declared.iter().map(|dsl| dsl.definition.id.clone()).collect::<Vec<_>>()
        }),
        Err(problems) => json!({
            "problems": problems.iter().map(problem_json).collect::<Vec<_>>()
        }),
    }
}

#[test]
fn every_conformance_case_reads_as_expected() {
    let folders = case_folders();
    if Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../conformance")
        .is_dir()
    {
        assert!(!folders.is_empty());
    }
    let mut mismatches = Vec::new();
    for folder in folders {
        let text = fs::read_to_string(folder.join("yaml-dsl.yml")).unwrap();
        let actual = reading_of(&text);
        let expected: Option<Value> = fs::read_to_string(folder.join("expected.json"))
            .ok()
            .map(|expected| serde_json::from_str(&expected).unwrap());
        if expected.as_ref() != Some(&actual) {
            mismatches.push(format!(
                "{}: {}",
                folder.file_name().unwrap().to_string_lossy(),
                serde_json::to_string_pretty(&actual).unwrap()
            ));
        }
    }
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}
