use std::fs;
use std::path::Path;

use serde_json::Value;
use yaml_dsl_spec::{function_table_schema, read_dsl_definitions};

/// The repo's examples, beside the crate in a checkout; absent from a published crate.
fn example_folders() -> Vec<std::path::PathBuf> {
    let examples = Path::new(env!("CARGO_MANIFEST_DIR")).join("../examples");
    let Ok(entries) = fs::read_dir(&examples) else {
        return Vec::new();
    };
    let mut folders: Vec<_> = entries
        .filter_map(|entry| Some(entry.ok()?.path()))
        .filter(|path| path.join("yaml-dsl.yml").is_file())
        .collect();
    folders.sort();
    folders
}

fn errors(schema: &str, instance: &Value) -> Vec<String> {
    let validator = jsonschema::draft202012::new(&serde_json::from_str(schema).unwrap()).unwrap();
    validator
        .iter_errors(instance)
        .map(|error| format!("{error} at {}", error.instance_path()))
        .collect()
}

#[test]
fn every_example_reads_to_validated_definitions() {
    let folders = example_folders();
    if Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../examples")
        .is_dir()
    {
        assert!(!folders.is_empty());
    }
    for folder in folders {
        let file = folder.join("yaml-dsl.yml");
        let text = fs::read_to_string(&file).unwrap();
        let declared = read_dsl_definitions(&file, &text)
            .unwrap_or_else(|problems| panic!("{}: {problems:?}", folder.display()));
        assert!(!declared.is_empty(), "{}", folder.display());
    }
}

#[test]
fn every_function_table_an_example_publishes_is_valid() {
    for folder in example_folders() {
        for entry in fs::read_dir(&folder).unwrap() {
            let path = entry.unwrap().path();
            if !path.to_string_lossy().ends_with(".schema.json") {
                continue;
            }
            let schema: Value = serde_json::from_str(&fs::read_to_string(&path).unwrap()).unwrap();
            let Some(table) = schema.get("x-yaml-dsl-functions") else {
                continue;
            };
            assert_eq!(
                errors(function_table_schema("1").unwrap(), table),
                Vec::<String>::new(),
                "{}",
                path.display()
            );
        }
    }
}
