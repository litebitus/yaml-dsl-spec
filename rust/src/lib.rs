//! The YAML DSL standard in Rust: the JSON Schemas of `yaml-dsl.yml` and of a
//! DSL's function table at each of their versions, a reader that returns a
//! `yaml-dsl.yml`'s DSL definitions fully validated, and the YAML reading,
//! diagnostics and schema validation it and every reader of a DSL stand on.
//!
//! A version is a minor, such as `1.0`, or a major, such as `1`, which stands for
//! the newest minor of that major the crate carries.

pub mod definition;
mod definition_reader;
mod diagnostic;
pub mod function_table;
pub mod json_path;
mod node;
mod position;
mod problem_wording;
mod requirement_markers;
pub mod resolved;
pub mod schema;
pub mod validation_rules;
pub mod yaml_reader;

pub use definition::DslDefinition;
pub use definition_reader::{DeclaredDsl, read_dsl_definitions};
pub use diagnostic::{Diagnostic, DiagnosticNotes};
pub use function_table::{CallKey, parse_call_key};
pub use node::{Entry, Node, NodeKind, number_json};
pub use position::Position;
pub use problem_wording::code_span;
pub use schema::closest_name;
pub use validation_rules::ValidationRules;

include!(concat!(env!("OUT_DIR"), "/schema_versions.rs"));

/// The JSON Schema of `yaml-dsl.yml` at `version`.
pub fn yaml_dsl_schema(version: &str) -> Option<&'static str> {
    schema_at(YAML_DSL_SCHEMAS, version).map(|(_, schema)| schema)
}

/// The JSON Schema of a DSL's function table, a document schema's
/// `x-yaml-dsl-functions`, at `version`.
pub fn function_table_schema(version: &str) -> Option<&'static str> {
    schema_at(FUNCTION_TABLE_SCHEMAS, version).map(|(_, schema)| schema)
}

#[cfg(test)]
#[path = "../schema_versions.rs"]
mod schema_versions;

/// The file name and text of the schema at `version`.
fn schema_at(
    schemas: &[(&str, &'static str, &'static str)],
    version: &str,
) -> Option<(&'static str, &'static str)> {
    schemas
        .iter()
        .find(|(known, _, _)| *known == version)
        .map(|(_, file, schema)| (*file, *schema))
}
