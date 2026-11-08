# Package design

Each language reads the standard through packages: the core, which every language implements alike, and packages beyond it.

## The core

1. The schemas of `yaml-dsl.yml` and of the function table, at each version.
2. The definition reader: a `yaml-dsl.yml`'s DSL definitions, validated, or every problem at its field.
3. The DSL definition model.
4. The YAML reader: a node tree with positions, YAML tags read as keys.
5. Diagnostics: a problem's position, message, label, rule, schema and help.
6. Paths: the JSONPath subset, parsed, matched and selected.
7. The function table: functions and macros read from a document schema, and call keys.
8. Resolved values: a document value that may hold a reference, a call or a template.
9. Schema validation: a document validated against its schema.

- The core grows as readers need it.

## API

Each entry is named in each language's own case. A version is a minor, such as `1.0`, or a major, such as `1`, which stands for its newest minor.

### Schemas

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| a `yaml-dsl.yml` schema | `yaml_dsl_schema(version)` | `yamlDslSchema(version)` | `YAMLDSLSchema(version)` | the schema at `version`; none for a version the package does not carry |
| a function table schema | `function_table_schema(version)` | `functionTableSchema(version)` | `FunctionTableSchema(version)` | the schema at `version`; none for a version the package does not carry |
| the versions | `YAML_DSL_VERSIONS`, `FUNCTION_TABLE_VERSIONS` | `YAML_DSL_VERSIONS`, `FUNCTION_TABLE_VERSIONS` | `YAMLDSLVersions()`, `FunctionTableVersions()` | every version the package reads, majors first |

### Definition reader

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| read a `yaml-dsl.yml` | `read_dsl_definitions(file, text)` | `readDslDefinitions(file, text)` | `ReadDSLDefinitions(file, text)` | each DSL's definition and its block as written, validated against its version's schema and the checks the schema cannot state; else every problem at its field |

### DSL definition model

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| a DSL block | `DslDefinition::from_json(block)` | `dslDefinition.dslDefinitionFromJson(block)` | `DSLDefinitionFromJSON(block)` | the block's fields as the standard names them, each with its default; else why the block is none |

### YAML reader

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| read YAML | `yaml_reader::parse(file, text, tag_keys)` | `yamlReader.parseYaml(file, text, tagKeys)` | `ParseYAML(file, text, tagKeys)` | the node tree, each node with its position and a tag listed in `tag_keys` read as a map holding its key; else the first problem |
| a node | `Node` | `Node` | `Node` | a value as written: its kind, its value and its position |

### Diagnostics

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| a problem | `Diagnostic` | `Diagnostic` | `Diagnostic` | its position, if it has one, its message, and its notes: label, rule, schema and help |
| a position | `Position` | `Position` | `Position` | a file, a line and a column, each from 1 |
| a fenced value | `code_span(value)` | `codeSpan(value)` | `CodeSpan(value)` | the value in the fewest backticks it does not hold, padded with a space where it begins or ends with one |

### Paths

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| parse a path | `PathPattern::parse(text, skip_keys)` | `PathPattern.parse(text, skipKeys)` | `ParsePathPattern(text, skipKeys)` | the pattern; else a problem naming the path outside the subset |
| match a path | `selects(path)`, `covers(path)` | `selects(path)`, `covers(path)` | `Selects(path)`, `Covers(path)` | whether the pattern selects a concrete path, or the path or one above it |
| select | `select(document)` | `select(document)` | `Select(document)` | every node the pattern selects, with its path, in document order |
| several paths | `PathSet` | `PathSet` | `PathSet` | the patterns of a list of paths, matched as one |

### Function table

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| read a table | `FunctionTable::from_schema(schema)` | `FunctionTable.fromSchema(schema)` | `FunctionTableFromSchema(schema)` | the table a document schema publishes at `#/x-yaml-dsl-functions`, empty for none; else why it is none |
| an entry | `entry(name)` | `entry(name)` | `Entry(name)` | the function or macro a call names, a suffixed macro name included |
| a macro's role | `MacroEntry::role(role)` | `MacroEntry.role(role)` | `MacroEntry.Role(role)` | the argument that plays `role` |
| a call key | `parse_call_key(key, marker)` | `parseCallKey(key, marker)` | `ParseCallKey(key, marker)` | the call a key writes, its name, function and splat; none for a key that writes none |

### Resolved values

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| a value | `resolved::Value` | `resolved.Value` | `ResolvedValue` | a document value with its position: a scalar, a list, a map, or a reference, a call or a template |
| from YAML | `Value::from_node(node)` | `Value.fromNode(node)` | `ResolvedValueFromNode(node)` | the value a node reads to |

### Schema validation

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| load a schema | `SchemaCache::load(name, text)` | `SchemaCache.load(name, text)` | `SchemaCache.Load(name, text)` | the schema, read once per name and text, in its draft, with its function table; else why it does not load |
| the rules | `ValidationRules::from_validator(validator)` | `ValidationRules.fromValidator(validator)` | `ValidationRulesFromValidator(validator)` | a DSL definition's `validator` block, its patterns compiled; no rules for none |
| validate | `LoadedSchema::validate(document, rules)` | `LoadedSchema.validate(document, rules)` | `LoadedSchema.Validate(document, rules)` | every problem of a resolved document, read by the rules, in document order |
| a child value | `child_value(key, patterns)` | `childValue(key, patterns)` | `ChildValue(key, patterns)` | the field and value a key sets on the entry it keys |
| a closest name | `closest_name(name, candidates)` | `closestName(name, candidates)` | `ClosestName(name, candidates)` | the candidate a misspelt name most likely means |

## Schema validation

A document is read, then validated against its schema in the draft its `$schema` names.

### Reading before validation

In order, each by the DSL's `validator` block:

1. A key matching a pattern of `child_values_from_parent_key` sets its entry's `field` to `value`, unless the entry writes the field.
2. In every map, the content of each key `merge_base_keys` lists is merged into each sibling map, as an overlay merges over its common layer: the sibling over the content.
3. A quoted value that spells a type `allow_quoted_values` lists, where the schema expects that type, is validated as that type: `true` and `false` a boolean, `-?(0|[1-9][0-9]*)` an integer, and a number a fraction and an exponent beside. One that spells none stays a string.

- A value not known until the document compiles, a call, a reference or a template, is not checked, nor a check that reads a value below it through a subschema or compares the values below it.
- An unknown field or item of a map or list holding such a value is reported unless the schema could read it by a keyword below, reached through `$ref`, `allOf`, `anyOf`, `oneOf`, `if`, `then`, `else` or `dependentSchemas`. A field or item the schema could read depends on a branch that reads the unknown value, and is not checked.
- A problem on a value an entry has only from a merge base key is not reported.
- Formats are not validated.

| Unknown | Read by |
|---|---|
| a field | a `properties` entry, a matching `patternProperties` pattern, an `additionalProperties` other than `false`, or a nested `unevaluatedProperties` other than `false` |
| an item | a `prefixItems` entry at its index, an `items` other than `false`, a `contains`, or a nested `unevaluatedItems` other than `false` |

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

A message is the package's own sentence. Every key and value it names is fenced, so a reader tells them from its prose; an item number, a position the package counts, is not. A schema's own words never enter a message: a description the problem reports is its schema note, as written, and a reader shows it apart from the message, prefixed `[schema]`.

| Failure | Message | Schema note |
|---|---|---|
| a missing field | `` `name` is required `` | |
| an unknown field | `` `name` is not a field of <field> `` | |
| a key `propertyNames` refuses | `` `key` is not a valid key of <field> `` | |
| `type` | `` <field> must be `boolean` ``, the types the schema lists, in its order, joined by `or` | |
| `enum` | `` <field> must be one of `"a"`, `"b"` ``, each value as JSON | |
| `const` | `` <field> must be `"a"` ``, the value as JSON | |
| `anyOf` or `oneOf`, no form holds | `<field> matches none of its allowed forms` | where the alternatives are those of a definition, `#/$defs/<name>` or `#/definitions/<name>`, the definition's description |
| `oneOf`, several forms hold | `<field> matches more than one of its allowed forms` | |
| a dependency not met | `` `key` needs <what> ``: the fields it needs, joined by `and`, a forbidden one as `` `x` must not be set `` | |
| `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum` | `` <field> must be >= `1` ``, with `<=`, `>` and `<` | |
| `multipleOf` | `` <field> must be multiple of `5` `` | |
| `minLength`, `maxLength` | `` <field> must NOT have fewer than `3` characters ``, or `more than` | |
| `minItems`, `maxItems` | `` <field> must NOT have fewer than `3` items ``, or `more than` | |
| `minProperties`, `maxProperties` | `` <field> must NOT have fewer than `3` properties ``, or `more than` | |
| items beyond a tuple that allows none | `` <field> must NOT have more than `2` items ``, the tuple's length | |
| `unevaluatedItems` | `` <field> must NOT have more than `2` items ``, the items evaluated | |
| `pattern` | `` <field> must match pattern `^x` `` | |
| `uniqueItems` | `<field> must NOT have duplicate items (items ## 2 and 0 are identical)`: from the last item back, the first equal to an item after it, and the nearest such item | |
| `contains` | `` <field> must contain at least `1` valid item(s) ``, with `` and no more than `2` `` where `maxContains` bounds it | |
| `not` | `<field> matches a form that is not allowed` | the description of the schema `not` holds |
| a `false` schema | `<field> boolean schema is false` | |
| any other keyword | `` <field> breaks `keyword` `` | |

A value is fenced in the fewest backticks it does not hold, padded with a space where it begins or ends with one.

- An `anyOf`, `oneOf` or `contains` problem stands for the failures inside it, which are not reported.
- A key `propertyNames` refuses is one problem, whatever checks it fails.
- A dependency is one problem per key.
- A `then` or `else` that fails reports its own failures.
- A problem's rule names the keyword and where the schema holds it; its label says what is wrong where it is placed.

## Beyond the core

A package beyond the core is built in the languages its readers use, and carries the core's version.

| Package | Rust | Go |
|---|---|---|
| the diagnostic renderer | `yaml-dsl-spec-diagnostic-renderer` | `github.com/litebitus/yaml-dsl-spec/go/diagnostic_renderer` |

### Diagnostic renderer

A diagnostic written as a compiler writes an error, for a terminal or a log. Every language renders the same text for the same input within the subset "Division to width" states; `conformance/diagnostic-rendering/` holds the cases.

| Entry | Rust | Go | Gives |
|---|---|---|---|
| render a diagnostic | `render_diagnostic(diagnostic, severity, source, style)` | `RenderDiagnostic(diagnostic, severity, source, style)` | the diagnostic's text |
| the severity | `Severity::Error`, `Severity::Warning` | `SeverityError`, `SeverityWarning` | the headline's word: `error` or `warning` |
| the source | `DiagnosticSource { name, text }` | `DiagnosticSource{Name, Text}` | the name the `-->` line shows, such as a path or `dashboard:"Mock Service"`, and the text the position counts lines and columns in |
| the style | `RenderStyle { color, width }` | `RenderStyle{Color, Width}` | whether the text is painted for a terminal, and the terminal width its lines divide to: none, `0` in Go, for output that is not a terminal |

```
error: `brnch` is not a field of `publish`
  --> services/mock-api/mock-dsl.yml:57:5
   |
52 | workflow:
53 |   publish:
...
55 |     tags: [mock]
56 |     branch: main
57 |     brnch: main
   |     ^^^^^ `publish` has no field `brnch`
58 |     triggers:
59 |       push:
   |
   = rule: unevaluatedProperties, at mock.schema.json#/$defs/workflow/unevaluatedProperties
   = [schema] Where and when a workflow publishes.
   = help: did you mean `branch`?
```

#### Lines shown

- The `-->` line names the source, then the position's line and column; the diagnostic's own file is not shown.
- The source shows two lines on each side of the wrong one, and above them each line it is nested under: each line indented less than the lines below it, blank lines and comments passed over. `...` marks a gap.
- Carets underline the wrong text: a `{{...}}` template, a quoted string, a key up to its colon, or a value up to a comment or the line's end. The label stands beside them.
- A source with empty text, or a position outside it, shows the `-->` line and no source lines.
- A diagnostic with no position is the headline and its notes.

#### Notes

In order, each where the diagnostic carries it:

1. `= rule: <rule>`
2. `= [schema] <schema note>`
3. `= help: <help>`

Each further line of a note's text continues one level in.

#### Division to width

- Nothing is cut. A line divides where the Unicode line breaking rules allow and after a machine string's separator, never inside a comparison operator.
- What wraps continues one level in. Where a row has no room for its next word, its indentation gives first, a level at a time.
- Widths are display columns, East Asian wide characters two.
- A wrapped source line is underlined once, under the row the wrong text ends on. A label that does not fit beside the carets takes its own rows under them.
- Output that is not a terminal is never divided.

Where a line divides, and how wide it is, is defined for a line made of these characters:

| Characters | Code points |
|---|---|
| ASCII | `U+0020`–`U+007E` |
| Latin, Greek and Cyrillic letters | the letters of `U+00C0`–`U+017F`, `U+0391`–`U+03C9` and `U+0410`–`U+044F` |
| CJK unified ideographs | `U+4E00`–`U+9FFF` |
| Hiragana and katakana | `U+3041`–`U+3096`, `U+30A1`–`U+30FA` |
| Hangul syllables | `U+AC00`–`U+D7A3` |
| Emoticons | `U+1F600`–`U+1F64F` |

and holding none of:

1. `$`, `%`, `+` or `\` before `(`, `[` or `{`
2. `"` or `'`, then two or more spaces, then `(`, `[` or `{`
3. a space, then `-`, then a digit
4. `)`, `]` or `}`, then two or more spaces, then a small kana

A line is each line the renderer divides: the headline, a source line, a label, or a note with its prefix. Outside the subset, where a line divides is undefined.

#### Color

| Part | Color |
|---|---|
| `error` | red, `91` |
| `warning` | yellow, `33` |
| the message | bold |
| the gutter, `-->`, `...` and a note's `=` | dim, `90` |
| the carets and the label | red, `91` |
| `rule:` and `help:` | cyan, `36` |
| `[schema]` | bold purple, `#A6ABF2` |
| the schema note's text | purple, `#A6ABF2` |

## TODO

- Schema resolution joins the core: a document's schema found and read as the specification's "Finding the schema" and "Schema URLs" state, through a schema cache, least recently used dropped first, in place of each reader's own copy. The schema cache's capacity is the consumer's to set.
