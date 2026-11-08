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

<table>
  <tr><th>Package</th><th>Language</th><th>Name</th><th>Folder</th></tr>
  <tr>
    <td rowspan="3">core</td>
    <td>Rust</td>
    <td><a href="https://crates.io/crates/yaml-dsl-spec"><code>yaml-dsl-spec</code></a></td>
    <td><a href="rust"><code>rust/</code></a></td>
  </tr>
  <tr>
    <td>JavaScript</td>
    <td><a href="https://www.npmjs.com/package/yaml-dsl-spec"><code>yaml-dsl-spec</code></a></td>
    <td><a href="javascript"><code>javascript/</code></a></td>
  </tr>
  <tr>
    <td>Go</td>
    <td><a href="https://pkg.go.dev/github.com/litebitus/yaml-dsl-spec/go"><code>github.com/litebitus/yaml-dsl-spec/go</code></a></td>
    <td><a href="go"><code>go/</code></a></td>
  </tr>
  <tr>
    <td rowspan="2">diagnostic renderer</td>
    <td>Rust</td>
    <td><a href="https://crates.io/crates/yaml-dsl-spec-diagnostic-renderer"><code>yaml-dsl-spec-diagnostic-renderer</code></a></td>
    <td><a href="rust/diagnostic-renderer"><code>rust/diagnostic-renderer/</code></a></td>
  </tr>
  <tr>
    <td>Go</td>
    <td><a href="https://pkg.go.dev/github.com/litebitus/yaml-dsl-spec/go/diagnostic_renderer"><code>github.com/litebitus/yaml-dsl-spec/go/diagnostic_renderer</code></a></td>
    <td><a href="go/diagnostic_renderer"><code>go/diagnostic_renderer/</code></a></td>
  </tr>
</table>

What each package implements is designed in [docs/package-design.md](docs/package-design.md): the core, and the packages beyond it.

## Conformance

[`conformance/`](conformance) holds the cases every package reads alike. A package's tests run every case, and a problem is compared with its line, column, message, label, rule, schema and help.

| Cases | Each case | Reads to |
|---|---|---|
| `definitions/<case>/` | a `yaml-dsl.yml` | its DSLs, or its problems |
| `yaml/<case>/` | an `input.yml`, and the tag keys of an optional `tag-keys.json` | its node tree with every value's line and column, or its problem |
| `schema/<case>/` | a `document.yml`, its `schema.json`, the DSL `validator` block of an optional `validator.json`, and the JSON pointers of an optional `unknown-values.json`, each value read as a reference to a declaration | its problems, or why the schema is not read |
| `patterns/cases.json` | a pattern of the standard's subset, and texts | whether it compiles, its named groups, and each text's captures |
| `diagnostic-rendering/<case>/` | a `diagnostic.json`, the severity, source name and style of a `rendering.json`, and the source text of an optional `source.yml`, all within the subset the package design defines rendering for | the diagnostic renderer's text, its lines in `expected.json` |
| `diagnostic-rendering/subset-corpus.json` | texts drawn from that subset, each an error's message and its source's one line, placed at its start | its text at each style it lists |

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

- A minor's schema is a file of its own: `yaml-dsl-v1.0.schema.json`, then `yaml-dsl-v1.1.schema.json` beside it.
- A released schema is always frozen. Any change to it, a description's wording included, is a new minor beside it.
- A deprecated field stays until the next major, which removes it.
- A major floats to its newest minor. `version: "1"` in a `yaml-dsl.yml` is read against the newest 1.x its reader carries; `version: "1.0"` against 1.0.
- The schemas are edited here, and only here. Each package's build copies them in.

### Packages

- Every package carries the same version, from one release tag, `v0.1.0`.
- A package reads every schema version it carries, a major as its newest minor; its types describe the newest, and a reader moves an older major's file up to it.
- Each release attaches every minor's schema, and each major's as its newest minor, for a modeline to name.

### Modelines

The modeline a `yaml-dsl.yml` takes, and every example's: the newest 1.x, from the latest release, so it never needs editing:

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
| `make test` | the crates' format check, clippy with warnings as errors and tests, then the package's tests, then the Go module's format check, vet and tests; each package's tests fail under 90% line coverage |
| `make build` | `make test`, then builds every package with the schemas of the checkout |

The `Release` workflow is dispatched by hand: it stamps `CHANGELOG.md` and the version of every package, tags, the Go module as `go/<version>` on a commit of its own that carries the schemas, publishes the crates and the npm package, and attaches the schemas to a GitHub release. Every package carries the same version.

The `Snapshot` workflow is dispatched by hand: it publishes every package as a pre-release, such as `0.1.5-snapshot.20261107123456`, tagged on its commit, the Go module as `go/<version>` on a commit of its own that carries the schemas, and the npm package with the `snapshot` tag.

Both publish to npm as its trusted publishers, from GitHub Actions, with no token. No package manager picks a pre-release on its own, so a consumer names a snapshot by its exact version.
