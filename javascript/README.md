# yaml-dsl-spec

The YAML DSL standard in JavaScript, from [yaml-dsl-spec](https://github.com/litebitus/yaml-dsl-spec). It reads as the Rust crate does: both pass the repo's conformance cases.

```js
const { readDslDefinitions } = require('yaml-dsl-spec');
const read = readDslDefinitions('yaml-dsl.yml', text);
if (read.problems) for (const problem of read.problems) console.error(String(problem));
else for (const { definition } of read.dsls) console.log(definition.id);
```

Its API is the core's, designed in the [package design](https://github.com/litebitus/yaml-dsl-spec/blob/main/docs/package-design.md).
