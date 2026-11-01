# Layers

`mock-queues`, a DSL of queues and the keys that encrypt them, in a common layer and two overlays. `queues/queues.yml` is the common layer, and `queues/dev/queues.yml` and `queues/prod/queues.yml` are the overlays folded over it.

| Where | What it shows | Specification |
|---|---|---|
| `layers` in `yaml-dsl.yml` | `dev` and `prod` are overlay folders, each paired with the nearest common layer above it | Common layer and overlays |
| `retention_seconds: 86400` in `dev` | an overlay's value wins over the common layer's in that overlay's fold; `prod` keeps `345600` | Folded documents |
| `key mock-audit` in `prod` | a declaration only that overlay's fold holds, which `dev` never sees | Resolution |
| `ref key.mock_main.arn` | a reference to a declaration of scope `key`, its name spelt with underscores by `name_spelling` | Scopes, Reference rules |
| `name mock-orders` | a key that sets the entry's `name` field, by the validator's `child_values_from_parent_key` | What the workspace defines |
| `local.team` | a local, declared under `locals` and read by a reference rule of the `local` scope | Locals |
| `"${env}"` | a placeholder holding `env`, a builtin of the logical scope `GLOBAL`, whose value the reader gives | Placeholders, Scopes |
| the modelines | each file names its schema, relative to itself | Finding the schema |
