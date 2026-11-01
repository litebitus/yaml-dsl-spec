# yaml-dsl-spec

The YAML DSL standard. A YAML DSL is defined by configuration, in a `yaml-dsl.yml`: the files it owns, its layers, scopes, declarations, references, placeholders and calls. This repo holds that format's JSON Schema, and the JSON Schema of a DSL's function table, and publishes them as a package for each language.

The specification is [docs/spec.md](docs/spec.md): what each part of a `yaml-dsl.yml` means, and how the parts work together.

## Examples

[`examples/`](examples) holds mini DSLs, each a definition, a document schema and documents written in it:

| Example | Shows |
|---|---|
| [`layers/`](examples/layers) | a common layer and its overlays, references, locals and placeholders |
| [`macros/`](examples/macros) | a function table, function calls, and `loop` and `merge` macros |
| [`tags/`](examples/tags) | YAML tags that stand for keys, and references written with them |

## Readers

| Reader | Reads the standard through |
|---|---|
| [YAML DSL editor](https://github.com/litebitus/vscode-yaml-dsl-editor) | the JavaScript package |
| [yaml-dsl-compiler](https://github.com/litebitus/yaml-dsl-compiler) | the Rust crate |
| [tfgen](https://github.com/litebitus/yaml-dsl-terraform-generator) | the Rust crate |

## Packages

| Language | Package | Folder | Offers |
|---|---|---|---|
| Rust | [`yaml-dsl-spec`](https://crates.io/crates/yaml-dsl-spec) | [`rust/`](rust) | `read_dsl_definitions`, a `yaml-dsl.yml`'s DSL definitions validated, or every problem at its field; the YAML reader with positions, diagnostics and schema validation it stands on; the schemas |
| JavaScript | [`yaml-dsl-spec`](https://www.npmjs.com/package/yaml-dsl-spec) | [`javascript/`](javascript) | the same as the Rust crate: `readDslDefinitions`, the YAML reader, diagnostics, schema validation and the schemas |

## Conformance

[`conformance/`](conformance) holds the cases every package reads alike. A package's tests run every case, and a problem is compared with its line, column, message, label, rule and help.

| Cases | Each case | Reads to |
|---|---|---|
| `definitions/<case>/` | a `yaml-dsl.yml` | its DSLs, or its problems |
| `yaml/<case>/` | an `input.yml`, and the tag keys of an optional `tag-keys.json` | its node tree with every value's line and column, or its problem |
| `schema/<case>/` | a `document.yml`, its `schema.json`, and the patterns of an optional `child-value-patterns.json` | its problems, or why the schema is not read |
| `patterns/cases.json` | a pattern of the standard's subset, and texts | whether it compiles, its named groups, and each text's captures |

Each case's `expected.json` states what it reads to; `patterns/expected.json` states it for every pattern.

## Schemas

| Schema | Describes |
|---|---|
| `schemas/yaml-dsl-v<version>.schema.json` | `yaml-dsl.yml` |
| `schemas/x-yaml-dsl-functions-v<version>.schema.json` | a DSL's function table, a document schema's `x-yaml-dsl-functions` |

## Versioning

A schema and the packages carry separate versions. A schema's version is the version of the format it describes, and a file states it in its `version` field. A package's version is the release that carries the schemas.

| Change | Schema version | Package version |
|---|---|---|
| a breaking format change | a new major, `v2.0` | a new major |
| an additive format change, such as a new optional field | a new minor, `v1.1` | a new minor |
| a package's code or packaging only | none | a new patch |

### Schemas

- A minor's schema is a file of its own, frozen once released: `yaml-dsl-v1.0.schema.json`, then `yaml-dsl-v1.1.schema.json` beside it.
- A major floats to its newest minor. `version: "1"` in a `yaml-dsl.yml` is read against the newest 1.x its reader carries; `version: "1.0"` against 1.0.
- The schemas are edited here, and only here. Each package's build copies them in.

### Packages

- Every package carries the same version, from one release tag, `v0.1.0`.
- A package reads every schema version it carries, a major as its newest minor; its types describe the newest, and a reader moves an older major's file up to it.
- Each release attaches every minor's schema, and each major's as its newest minor, for a modeline to name.

### Modelines

The newest 1.x, from the latest release, so the modeline never needs editing:

```yaml
# yaml-language-server: $schema=https://github.com/litebitus/yaml-dsl-spec/releases/latest/download/yaml-dsl-v1.schema.json
version: "1"
```

Exactly 1.0, from a fixed release, which never changes, so its copy is kept a year:

```yaml
# yaml-language-server: $schema=https://github.com/litebitus/yaml-dsl-spec/releases/download/v0.1.0/yaml-dsl-v1.0.schema.json?yaml_dsl_max_age_days=365
version: "1.0"
```

## Development

| Target | Does |
|---|---|
| `make test` | the crate's format check, clippy with warnings as errors and tests, then the package's tests |
| `make build` | `make test`, then a release build of the crate |
| `make promote-npm STAGE_ID=<stage id>` | shows a version the `Release` workflow staged on npm, then promotes it, asking for two-factor authentication; without `STAGE_ID`, lists the staged versions |

The `Release` workflow is dispatched by hand: it stamps `CHANGELOG.md` and the version of every package, tags, publishes the crate, stages the npm package, and attaches the schemas to a GitHub release. A maintainer promotes the staged npm version, on npmjs.com or with `make promote-npm`, with two-factor authentication. Every package carries the same version.
