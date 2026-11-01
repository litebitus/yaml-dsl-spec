# Package design

Each language reads the standard through packages: the core, which every language implements alike, and packages beyond it.

## The core

1. The schemas of `yaml-dsl.yml` and of the function table, at each version.
2. The definition reader: a `yaml-dsl.yml`'s DSL definitions, validated, or every problem at its field.
3. The DSL definition model.
4. The YAML reader: a node tree with positions, YAML tags read as keys.
5. Diagnostics: a problem's position, message, label, rule and help.
6. Paths: the JSONPath subset, parsed, matched and selected.
7. The function table: functions and macros read from a document schema, and call keys.
8. Resolved values: a document value that may hold a reference, a call or a template.
9. Schema validation: a document validated against its schema.

- The core grows as readers need it.

## API

Each entry is named in each language's own case. A version is a minor, such as `1.0`, or a major, such as `1`, which stands for its newest minor.

### Schemas

| Entry | Rust | JavaScript | Gives |
|---|---|---|---|
| a `yaml-dsl.yml` schema | `yaml_dsl_schema(version)` | `yamlDslSchema(version)` | the schema at `version`; none for a version the package does not carry |
| a function table schema | `function_table_schema(version)` | `functionTableSchema(version)` | the schema at `version`; none for a version the package does not carry |
| the versions | `YAML_DSL_VERSIONS`, `FUNCTION_TABLE_VERSIONS` | `YAML_DSL_VERSIONS`, `FUNCTION_TABLE_VERSIONS` | every version the package reads, majors first |

### Definition reader

| Entry | Rust | JavaScript | Gives |
|---|---|---|---|
| read a `yaml-dsl.yml` | `read_dsl_definitions(file, text)` | `readDslDefinitions(file, text)` | each DSL's definition and its block as written, validated against its version's schema and the checks the schema cannot state; else every problem at its field |

### DSL definition model

| Entry | Rust | JavaScript | Gives |
|---|---|---|---|
| a DSL block | `DslDefinition::from_json(block)` | `dslDefinition.dslDefinitionFromJson(block)` | the block's fields as the standard names them, each with its default; else why the block is none |

### YAML reader

| Entry | Rust | JavaScript | Gives |
|---|---|---|---|
| read YAML | `yaml_reader::parse(file, text, tag_keys)` | `yamlReader.parseYaml(file, text, tagKeys)` | the node tree, each node with its position and a tag listed in `tag_keys` read as a map holding its key; else the first problem |
| a node | `Node` | `Node` | a value as written: its kind, its value and its position |

### Diagnostics

| Entry | Rust | JavaScript | Gives |
|---|---|---|---|
| a problem | `Diagnostic` | `Diagnostic` | its position, if it has one, its message, and its notes: label, rule and help |
| a position | `Position` | `Position` | a file, a line and a column, each from 1 |

### Paths

| Entry | Rust | JavaScript | Gives |
|---|---|---|---|
| parse a path | `PathPattern::parse(text, skip_keys)` | `PathPattern.parse(text, skipKeys)` | the pattern; else a problem naming the path outside the subset |
| match a path | `selects(path)`, `covers(path)` | `selects(path)`, `covers(path)` | whether the pattern selects a concrete path, or the path or one above it |
| select | `select(document)` | `select(document)` | every node the pattern selects, with its path, in document order |
| several paths | `PathSet` | `PathSet` | the patterns of a list of paths, matched as one |

### Function table

| Entry | Rust | JavaScript | Gives |
|---|---|---|---|
| read a table | `FunctionTable::from_schema(schema)` | `FunctionTable.fromSchema(schema)` | the table a document schema publishes at `#/x-yaml-dsl-functions`, empty for none; else why it is none |
| an entry | `entry(name)` | `entry(name)` | the function or macro a call names, a suffixed macro name included |
| a macro's role | `MacroEntry::role(role)` | `MacroEntry.role(role)` | the argument that plays `role` |
| a call key | `parse_call_key(key, marker)` | `parseCallKey(key, marker)` | the call a key writes, its name, function and splat; none for a key that writes none |

### Resolved values

| Entry | Rust | JavaScript | Gives |
|---|---|---|---|
| a value | `resolved::Value` | `resolved.Value` | a document value with its position: a scalar, a list, a map, or a reference, a call or a template |
| from YAML | `Value::from_node(node)` | `Value.fromNode(node)` | the value a node reads to |

### Schema validation

| Entry | Rust | JavaScript | Gives |
|---|---|---|---|
| load a schema | `SchemaCache::load(name, text)` | `SchemaCache.load(name, text)` | the schema, read once per name and text, in its draft, with its function table; else why it does not load |
| the rules | `ValidationRules::from_validator(validator)` | `ValidationRules.fromValidator(validator)` | a DSL definition's `validator` block, its patterns compiled; no rules for none |
| validate | `LoadedSchema::validate(document, rules)` | `LoadedSchema.validate(document, rules)` | every problem of a resolved document, read by the rules, in document order |
| a child value | `child_value(key, patterns)` | `childValue(key, patterns)` | the field and value a key sets on the entry it keys |
| a closest name | `closest_name(name, candidates)` | `closestName(name, candidates)` | the candidate a misspelt name most likely means |

## Schema validation

A document is read, then validated against its schema in the draft its `$schema` names.

### Reading before validation

In order, each by the DSL's `validator` block:

1. A key matching a pattern of `child_values_from_parent_key` sets its entry's `field` to `value`, unless the entry writes the field.
2. In every map, the content of each key `merge_base_keys` lists is merged into each sibling map, as an overlay merges over its common layer: the sibling over the content.
3. A quoted value that spells a type `allow_quoted_values` lists, where the schema expects that type, is validated as that type: `true` and `false` a boolean, `-?(0|[1-9][0-9]*)` an integer, and a number a fraction and an exponent beside. One that spells none stays a string.

- A value not known until the document compiles, a call, a reference or a template, is not checked, nor a check that reads a value below it through a subschema or compares the values below it.
- A problem on a value an entry has only from a merge base key is not reported.
- Formats are not validated.

### Requirement markers

A field's `description` may open with a requirement marker:

| Marker | The key |
|---|---|
| `[required]` | is present |
| `[~required]` | is present under a condition the schema does not state |
| `[optional]` | may be absent |

- A marker decides over the parent's `required` array for its key.
- A field without a marker is required when its parent's `required` array lists it.
- A `required` array inside `not` or `if` is a condition, and markers leave it as written.
- A `required` array inside an `anyOf` or `oneOf` branch selects the form the value takes, and markers leave it as written.

### Problems

A problem is placed at the value it is about; a missing field at its map's key, and an unknown field, a key that is not valid, and a key whose dependency is not met at that key. A problem names its field by the field's key, then `[<index>]` for each list index below the key, such as `` `tags[1]` ``; the root is `the document`.

| Failure | Message |
|---|---|
| a missing field | `` `name` is required `` |
| an unknown field | `` `name` is not a field of <field> `` |
| a key `propertyNames` refuses | `` `key` is not a valid key of <field> `` |
| `type` | `<field> must be boolean`, the types the schema lists, in its order, joined by `or` |
| `enum` | `` <field> must be one of `"a"`, `"b"` ``, each value as JSON |
| `const` | `<field> must be equal to constant` |
| `anyOf` or `oneOf`, no form holds | `<field> must be <form>`, where the alternatives are those of a definition, `#/$defs/<name>` or `#/definitions/<name>`, and `<form>` is the first sentence of its description, lowercased unless it opens with an acronym; else `<field> matches none of its allowed forms` |
| `oneOf`, several forms hold | `<field> matches more than one of its allowed forms` |
| a dependency not met | `` `key` needs <what> ``: the fields it needs, joined by `and`, a forbidden one as `` `x` must not be set `` |
| `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum` | `<field> must be >= 1`, with `<=`, `>` and `<` |
| `multipleOf` | `<field> must be multiple of 5` |
| `minLength`, `maxLength` | `<field> must NOT have fewer than 3 characters`, or `more than` |
| `minItems`, `maxItems` | `<field> must NOT have fewer than 3 items`, or `more than` |
| `minProperties`, `maxProperties` | `<field> must NOT have fewer than 3 properties`, or `more than` |
| items beyond a tuple that allows none | `<field> must NOT have more than 2 items`, the tuple's length |
| `unevaluatedItems` | `<field> must NOT have more than 2 items`, the items evaluated |
| `pattern` | `<field> must match pattern "^x"` |
| `uniqueItems` | `<field> must NOT have duplicate items (items ## 2 and 0 are identical)`: from the last item back, the first equal to an item after it, and the nearest such item |
| `contains` | `<field> must contain at least 1 valid item(s)`, with `and no more than 2` where `maxContains` bounds it |
| `not` | `<field> must NOT be valid` |
| a `false` schema | `<field> boolean schema is false` |
| any other keyword | `` <field> breaks `keyword` `` |

- An `anyOf`, `oneOf` or `contains` problem stands for the failures inside it, which are not reported.
- A key `propertyNames` refuses is one problem, whatever checks it fails.
- A dependency is one problem per key.
- A `then` or `else` that fails reports its own failures.
- A problem's rule names the keyword and where the schema holds it; its label says what is wrong where it is placed.
