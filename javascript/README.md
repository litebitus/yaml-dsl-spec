# yaml-dsl-spec

The YAML DSL standard in JavaScript, from [yaml-dsl-spec](https://github.com/litebitus/yaml-dsl-spec).

```js
const { yamlDslSchemas, functionTableSchemas } = require('yaml-dsl-spec');
const schema = yamlDslSchemas['1'];
```

| Export | Is |
|---|---|
| `yamlDslSchemas` | the JSON Schemas of `yaml-dsl.yml`, by version |
| `functionTableSchemas` | the JSON Schemas of a DSL's function table, `x-yaml-dsl-functions`, by version |

A version is a minor, such as `1.0`, or a major, such as `1`, which is its newest minor in the package.
