# Changelog

## Unreleased

- Packages
  - An `anyOf` or `oneOf` whose every alternative states a `const` reads `` `tier` must be one of `"a"`, `"b"` ``, as an `enum` of those constants does.

## v0.3.0 — 2026-11-10

- Schemas
  - `yaml-dsl.yml` 1.3, and a schema changelog, `schemas/CHANGELOG.md`.
- Packages
  - The diagnostic renderer draws a warning's carets and label in yellow, the warning's color, and an error's in red.
  - An unknown field reads `` `name` is not defined in the schema ``, with no label.
  - The schema cache takes a capacity, the least recently asked for schema dropped first, and a store the caller supplies, where it keeps the information it prepares from each schema across runs.
  - `problems` gives each problem with its subject, the JSON pointer of the value it is about, and `fragment_problems` validates a document against a fragment of its schema.
  - A merge base key's content requires no key, as a fragment requires none.
  - A `merge` macro call with a content not known is validated as its known maps, requiring no key.
  - An `anyOf` or `oneOf` is reported when each form fails a check that reads no unknown value.
  - A dependency message names what every `allOf` part and reference of the value's schema needs for the key.
  - A schema note is the description verbatim, its requiredness marker included.
  - A `$ref` naming no node and a pattern that does not compile are worded by the package; no name of the package's own reaches a message.

## v0.2.1 — 2026-11-08

- Packages
  - The `[required]`, `[optional]` and `[~required]` a description opens with are requiredness markers, in the package design and the code; the JavaScript module is `lib/requiredness-markers.js`.

## v0.2.0 — 2026-11-08

- Specification
  - A schema URL is read without credentials; `yaml_dsl_machine_credentials=true` opts in to the machine's credentials, tried only when the read without them is refused.
  - A GitHub link under `<owner>/<repo>/releases/`, `latest` included, is read through GitHub's API with a token, and a link no credential reads is reported by what is known.
  - An overlay directory holding no documents is an overlay of its stack, folding to the common layer alone; a common layer with no overlay files is a stack of its own.
- Packages
  - A problem's notes gain `schema`: the schema's own words it reports, as written.
  - Schema validation fences every key and value a message names, and a schema's description moves from the message to the schema note.
  - An unknown field or item of a map or list holding a reference, a call or a template is reported, unless the schema could read it through a branch that reads that value.
  - A misspelt field's help also names a field the schema accepts through `if`, `then`, `else` or `dependentSchemas`.
  - A wrong value under an `unevaluatedProperties` or `unevaluatedItems` schema is reported as the value's own problem, as under `additionalProperties`.
  - The Go package, `github.com/litebitus/yaml-dsl-spec/go`: the whole core, passing every conformance case.
  - The fence a message puts around a value is public: `code_span`, `codeSpan` and `CodeSpan`.
  - The diagnostic renderer, `yaml-dsl-spec-diagnostic-renderer` and `github.com/litebitus/yaml-dsl-spec/go/diagnostic_renderer`: a diagnostic written as a compiler writes an error, alike in both for the subset of text the package design defines it for, by the cases of `conformance/diagnostic-rendering/`.
- Conformance
  - A problem is compared with its schema note too; a case of `not` with and without a described schema.

## v0.1.4 — 2026-11-01

- Packages
  - A `yaml-dsl.yml` without a `version` reads "`version` is required", worded as every missing field is.

## v0.1.3 — 2026-11-01

- Packages
  - A function table's deprecated forms are read until the next major: an entry written as its argument list, and `loop_variable`.
  - A path is read only in the subset the schema states, so `$abc`, `$*` and `$.a.['b']` are refused.
  - Schema validation reads a document as the package design states: `merge_base_keys`, `allow_quoted_values` and requiredness markers, and each problem worded as the YAML DSL editor words it.
  - `ValidationRules`, a DSL's `validator` block, is what a document is validated by.
  - Formats are not validated, in any draft.
- Documentation
  - The package design: the core every language implements, its API, and what schema validation reads and reports.
- Conformance
  - A schema case reads its DSL `validator` block from `validator.json`; cases of requiredness markers, merge base keys, quoted values, described alternatives and `contains` bounds.

## v0.1.2 — 2026-11-01

- Schemas
  - `yaml-dsl.yml` v1.2: `merge_base_keys` is a DSL's feature again, no longer deprecated.
  - A deprecated field's description says what replaces it, without a "Deprecated:" prefix: `yaml-dsl.yml` v1.2 and the function table's v1.1.

## v0.1.1 — 2026-11-01

- Schemas
  - `yaml-dsl.yml` v1.1: a path takes the `['name']`, `..name` and `..['name']` steps the specification's Paths list; `version: "1"` reads it, and `version: "1.0"` keeps reading v1.0.
- Conformance
  - Cases of descendant-name paths under v1.1 and under v1.0.

## v0.1.0 — 2026-11-01

- Packages
  - The JavaScript package reads as the Rust crate does: `readDslDefinitions`, the YAML reader, diagnostics, the JSONPath subset, the function table and schema validation.
  - A document schema is read in the draft its `$schema` names: draft-07 where it names none or an older one, and an unread draft refused, naming the drafts read.
  - A schema's problems are reported in the document's order.
  - A verbatim `!<tag:yaml.org,2002:str>` tag reads its value as a string.
  - A `version` that is not a string is shown as written, such as `` `1` ``.
- Conformance
  - Cases of YAML reading, of schema validation and of patterns, and more cases of `yaml-dsl.yml`.

## v0.0.1 — 2026-11-01

- Specification
  - What a `yaml-dsl.yml` means, moved from the YAML DSL editor's design doc.
- Schemas
  - The JSON Schemas of `yaml-dsl.yml` and of a DSL's function table at v1.0, moved from the YAML DSL editor; a major floats to its newest minor.
- Examples
  - Mini DSLs, each a definition, a document schema and documents: layers, macros and YAML tags.
- Packages
  - The Rust crate: `read_dsl_definitions`, a `yaml-dsl.yml`'s definitions validated with every problem at its field; the definition's types, YAML reading, diagnostics, the function table and schema validation, moved from yaml-dsl-compiler; the schemas.
- Conformance
  - Cases of valid and invalid `yaml-dsl.yml` files and what each reads to, run by every package.
  - The JavaScript package: the schemas.
