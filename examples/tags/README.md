# Tags

`mock-stack`, a DSL of resources referenced by YAML tags, the way CloudFormation's short forms read. `stack.yml` is the document.

| Where | What it shows | Specification |
|---|---|---|
| `tag_keys` in `yaml-dsl.yml` | `!Ref` stands for the key `Ref`, and `!GetAtt` for `Fn::GetAtt` | YAML tags |
| `!Ref OrdersKey` | reads as `Ref: OrdersKey` for the schema, and as the text `!Ref OrdersKey` for the reference rule | YAML tags, Reference rules |
| `!GetAtt OrdersKey.Arn` | a reference with text after the name, `.Arn`, which the rule allows | Reference rules |
| `OrdersKey` under `Resources` | a declaration of the literal scope `RESOURCE`, read whole from its key | Declaration rules |
| no `layers`, `locals` or `function` | a block left out is a feature the DSL does not have | What the workspace defines |
