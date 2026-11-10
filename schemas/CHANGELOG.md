# Schema changelog

Each version is the format a file names in its `version`, and the schema file of that name. A version not yet released is headed `### <version> — unreleased`; the release workflow stamps it with the release that ships it and the date.

## `yaml-dsl.yml`

### 1.3 — v0.3.0, 2026-11-10

- `performance.schema_store` replaces `compiled_schema_cache`, deprecated.

### 1.2 — v0.1.2, 2026-11-01

- `merge_base_keys` is a DSL's feature again, no longer deprecated.
- A deprecated field's description says what replaces it.

### 1.1 — v0.1.1, 2026-11-01

- A path takes the `['name']`, `..name` and `..['name']` steps.

### 1.0 — v0.0.1, 2026-11-01

- The first format, moved from the YAML DSL editor.

## Function table

### 1.1 — v0.1.2, 2026-11-01

- A deprecated field's description says what replaces it.

### 1.0 — v0.0.1, 2026-11-01

- The first format, moved from the YAML DSL editor.
