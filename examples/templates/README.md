# Templates

`mock-queue-templates`, a template language of the `mock-queues` DSL. `templates/queues.yml` is a template a rendering tool fills in with `.team`, `.rotation`, `.queues`, `.retention_seconds` and `.tagged`; until then it is no valid YAML.

| Where | What it shows | Specification |
|---|---|---|
| `templates` in `yaml-dsl.yml` | a template language beside the DSLs, naming the DSL it is a template of with `dsl` | Templates |
| `templates/queues.yml` | a file both the DSL and the template language claim is a template | Templates |
| `[[ .team ]]`, `[[ .rotation ]]` | actions, in a key, in text and as a whole value, read as values known only when the template is rendered | Reading a template |
| `.team`, `.queues` | template variables, matched by `variable`, values the rendering tool supplies | Reading a template |
| `[[ range $queue := .queues ]]` | a loop, binding `$queue` through the `variables` group of `loop.start` | Loops |
| `[[ $queue ]]` | a loop variable, resolved to the loop around it | Loops |
| `[[ if .tagged ]]` | a conditional inside the loop; `end` closes the conditional, then the loop | Conditionals, Reading a template |
| the modeline | names the rendered document's schema and is not read; the template's schema comes from `schema_search_paths` | Templates |
