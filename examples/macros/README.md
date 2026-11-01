# Macros

`mock-pipeline`, a DSL of jobs whose schema publishes a function table with a function and two macros. `pipeline.yml` is the document; `mock-pipeline.schema.json` holds the table at `x-yaml-dsl-functions`.

| Where | What it shows | Specification |
|---|---|---|
| `function` in `yaml-dsl.yml` | calls are written in keys, `<name> fn.<function>`, and the table comes from the document's schema | Functions |
| `x-yaml-dsl-functions` | `format`, a function, and `merge` and `loop`, macros, each with its arguments in call order | The function table |
| `fn.loop` under `job` | a loop with no name: its map body's entries land among `job`'s, declaring `build-api` and `build-worker` | Macros, Calls |
| `"build-${~service}"` | `~service`, a loop variable, read by the reference rule of the `LOOP` scope | Loop variables |
| `fn.merge: local.defaults` | a merge with no name: the local's fields merge into the job holding the call, its own keys winning | Macros, What an expansion gives |
| `ref job.build_api` | a reference to a job a loop's expansion declares | Macros |
| `message fn.format*` | a function call with a name, its list of arguments after the `*` splat operator; its value is computed when the document is used, so the job's schema admits the call key through `patternProperties` | Functions |
