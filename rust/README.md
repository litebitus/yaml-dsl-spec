# yaml-dsl-spec

The YAML DSL standard in Rust, from [yaml-dsl-spec](https://github.com/litebitus/yaml-dsl-spec).

| Item | Is |
|---|---|
| `read_dsl_definitions(file, text)` | each DSL of a `yaml-dsl.yml`, validated against its version's schema and the checks the schema cannot state, or every problem at the field it is about |
| `DeclaredDsl` | a DSL's definition, and its block as written, for a caller to place its own errors |
| `Diagnostic`, `Position`, `yaml_reader`, `Node` | a problem at its place, and YAML read with each value's place |
| `yaml_dsl_schema(version)` | the JSON Schema of `yaml-dsl.yml` at a version |
| `function_table_schema(version)` | the JSON Schema of a DSL's function table, `x-yaml-dsl-functions`, at a version |
| `YAML_DSL_VERSIONS`, `FUNCTION_TABLE_VERSIONS` | the versions the crate reads |
| `DslDefinition` | a DSL block of `yaml-dsl.yml`, as serde reads it |

A version is a minor, such as `1.0`, or a major, such as `1`, which is its newest minor in the crate.
