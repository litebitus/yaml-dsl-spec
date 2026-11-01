# Changelog

## Unreleased

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
