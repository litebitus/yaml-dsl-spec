# Changelog

## Unreleased

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
