# yaml-dsl-spec

The YAML DSL standard in JavaScript, from [yaml-dsl-spec](https://github.com/litebitus/yaml-dsl-spec). It reads as the Rust crate does: both pass the repo's conformance cases.

```js
const { readDslDefinitions } = require('yaml-dsl-spec');
const read = readDslDefinitions('yaml-dsl.yml', text);
if (read.problems) for (const problem of read.problems) console.error(String(problem));
else for (const { definition } of read.dsls) console.log(definition.id);
```

| Export | Is |
|---|---|
| `readDslDefinitions(file, text)` | `{ dsls }`, each DSL of a `yaml-dsl.yml`, `{ definition, block }`, validated against its version's schema and the checks the schema cannot state; or `{ problems }`, every problem at the field it is about |
| `Diagnostic`, `Position` | a problem at its place: `message`, `position` and `notes` (`label`, `rule`, `help`) |
| `yamlReader.parseYaml(file, text, tagKeys)` | `{ node }`, YAML read with each value's place, or `{ problem }` |
| `Node`, `numberJson` | a YAML value as written, and a number's text as JSON |
| `dslDefinition.dslDefinitionFromJson(block)` | a DSL block with its defaults, its fields named as the standard names them |
| `jsonPath` | `PathPattern` and `PathSet`, the standard's JSONPath subset |
| `resolved` | `Value`, a resolved document's value with its place |
| `functionTable` | `FunctionTable`, a schema's `x-yaml-dsl-functions` |
| `schemaValidation` | `SchemaCache` and `LoadedSchema`, a document validated against its schema, each problem at its place |
| `parseCallKey`, `closestName` | a call written in a key, and the allowed name closest to a misspelt one |
| `yamlDslSchema(version)`, `yamlDslSchemas` | the JSON Schema of `yaml-dsl.yml`, at a version, or by version |
| `functionTableSchema(version)`, `functionTableSchemas` | the JSON Schema of a DSL's function table, `x-yaml-dsl-functions`, at a version, or by version |
| `YAML_DSL_VERSIONS`, `FUNCTION_TABLE_VERSIONS` | the versions the package reads |

A version is a minor, such as `1.0`, or a major, such as `1`, which is its newest minor in the package.
