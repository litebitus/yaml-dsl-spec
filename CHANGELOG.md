# Changelog

## Unreleased

- Packages
  - A function table's deprecated forms are read until the next major: an entry written as its argument list, and `loop_variable`.
  - A path is read only in the subset the schema states, so `$abc`, `$*` and `$.a.['b']` are refused.
  - Schema validation reads a document as the package design states: `merge_base_keys`, `allow_quoted_values` and requirement markers, and each problem worded as the YAML DSL editor words it.
  - `ValidationRules`, a DSL's `validator` block, is what a document is validated by.
  - Formats are not validated, in any draft.
- Documentation
  - The package design: the core every language implements, its API, and what schema validation reads and reports.
- Conformance
  - A schema case reads its DSL `validator` block from `validator.json`; cases of requirement markers, merge base keys, quoted values, described alternatives and `contains` bounds.

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
