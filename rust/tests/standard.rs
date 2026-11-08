use serde_json::Value;
use yaml_dsl_spec::definition::{CommonLayerDiscovery, NameSpelling};
use yaml_dsl_spec::{
    DslDefinition, FUNCTION_TABLE_VERSIONS, YAML_DSL_VERSIONS, code_span, function_table_schema,
    yaml_dsl_schema,
};

const MOCK_DSL_CONFIG: &str = include_str!("fixtures/mock-dsl.json");
const MOCK_FUNCTION_TABLE: &str = include_str!("fixtures/mock-functions.json");

fn json(text: &str) -> Value {
    serde_json::from_str(text).unwrap()
}

fn errors(schema: &str, instance: &Value) -> Vec<String> {
    let validator = jsonschema::draft202012::new(&json(schema)).unwrap();
    validator
        .iter_errors(instance)
        .map(|error| format!("{error} at {}", error.instance_path()))
        .collect()
}

#[test]
fn a_major_stands_for_its_newest_minor() {
    assert_eq!(YAML_DSL_VERSIONS, ["1", "1.0", "1.1", "1.2"]);
    assert_eq!(FUNCTION_TABLE_VERSIONS, ["1", "1.0", "1.1"]);
    assert_eq!(yaml_dsl_schema("1"), yaml_dsl_schema("1.2"));
    assert_ne!(yaml_dsl_schema("1.1"), yaml_dsl_schema("1.2"));
    for minor in ["1.0", "1.1", "1.2"] {
        let title = format!("\"title\": \"yaml-dsl.yml v{minor}\"");
        assert!(yaml_dsl_schema(minor).unwrap().contains(&title));
    }
    assert_eq!(yaml_dsl_schema("2"), None);
    assert_eq!(function_table_schema("1"), function_table_schema("1.1"));
    assert_ne!(function_table_schema("1.0"), function_table_schema("1.1"));
}

#[test]
fn a_minor_schema_takes_a_file_stating_its_minor_or_its_major() {
    let mut config = json(MOCK_DSL_CONFIG);
    let schema = yaml_dsl_schema("1.0").unwrap();
    for version in ["1", "1.0"] {
        config["version"] = Value::from(version);
        assert_eq!(errors(schema, &config), Vec::<String>::new());
    }
    config["version"] = Value::from("1.2");
    assert!(!errors(schema, &config).is_empty());
    let newest = yaml_dsl_schema("1.2").unwrap();
    for version in ["1", "1.2"] {
        config["version"] = Value::from(version);
        assert_eq!(errors(newest, &config), Vec::<String>::new());
    }
}

#[test]
fn a_config_the_schema_accepts_reads_into_its_definitions() {
    let config = json(MOCK_DSL_CONFIG);
    let version = config["version"].as_str().unwrap();
    assert_eq!(
        errors(yaml_dsl_schema(version).unwrap(), &config),
        Vec::<String>::new()
    );
    let blocks = config["dsls"].as_array().unwrap();
    let definition = DslDefinition::from_json(&blocks[0]).unwrap();
    assert_eq!(definition.id, "mock-dsl");
    let layers = definition.layers.unwrap();
    assert_eq!(
        layers.common_layer_discovery,
        CommonLayerDiscovery::Ancestor
    );
    assert_eq!(
        definition.declarations[1].name_spelling,
        NameSpelling::DashesAsUnderscores
    );
}

#[test]
fn the_schema_reports_a_misspelt_field_the_types_would_ignore() {
    let mut config = json(MOCK_DSL_CONFIG);
    let block = config["dsls"][0].as_object_mut().unwrap();
    let excludes = block.remove("file_excludes").unwrap();
    block.insert("file_exclude".to_owned(), excludes);
    let reported = errors(yaml_dsl_schema("1").unwrap(), &config);
    assert!(
        reported.iter().any(|error| error.contains("file_exclude")),
        "{reported:?}"
    );
    assert!(DslDefinition::from_json(&config["dsls"][0]).is_ok());
}

#[test]
fn a_function_table_the_schema_accepts() {
    assert_eq!(
        errors(
            function_table_schema("1").unwrap(),
            &json(MOCK_FUNCTION_TABLE)
        ),
        Vec::<String>::new()
    );
}

#[test]
fn a_value_is_fenced_in_more_backticks_than_it_holds_in_a_row() {
    assert_eq!(code_span("on"), "`on`");
    assert_eq!(code_span("a`b"), "``a`b``");
    assert_eq!(code_span("a``b`c"), "```a``b`c```");
    assert_eq!(code_span("`on"), "`` `on ``");
    assert_eq!(code_span("on`"), "`` on` ``");
}
