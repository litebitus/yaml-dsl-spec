# Package design

Each language reads the standard through packages: the core, which every language implements alike, and extra packages.

## The core

1. The schemas of `yaml-dsl.yml` and of the function table, at each version.
2. The definition reader: a `yaml-dsl.yml`'s DSL definitions, validated, or every problem at its field.
3. The DSL definition model.
4. The YAML reader: a node tree with positions, YAML tags read as keys.
5. Diagnostics: a problem's severity, position, message, label, rule, schema and help.
6. Paths: the JSONPath subset, parsed, matched and selected.
7. The function table: functions and macros read from a document schema, and call keys.
8. Resolved values: a document value that may hold a reference, a call or a template.
9. Schema validation: a document validated against its schema.
10. The format function: a layout's verbs written from its operands.
11. The template reader: a template's actions masked, its conditionals and loops read, and its problems.
12. Schema resolution: a document's schema found and read, each URL by the reader of its prefix.

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
| read a `yaml-dsl.yml` with its templates | `read_definitions(file, text)` | `readDefinitions(file, text)` | `ReadDefinitions(file, text)` | the DSLs `read_dsl_definitions` gives, and beside them each template language's definition and its block as written; else every problem at its field |

### DSL definition model

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| a DSL block | `DslDefinition::from_json(block)` | `dslDefinition.dslDefinitionFromJson(block)` | `DSLDefinitionFromJSON(block)` | the block's fields as the standard names them, each with its default; else why the block is none |
| a template block | `TemplateDefinition::from_json(block)` | `templateDefinition.templateDefinitionFromJson(block)` | `TemplateDefinitionFromJSON(block)` | the block's fields as the standard names them, its patterns compiled; else why the block is none |

### YAML reader

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| read YAML | `yaml_reader::parse(file, text, tag_keys)` | `yamlReader.parseYaml(file, text, tagKeys)` | `ParseYAML(file, text, tagKeys)` | the node tree, each node with its position and a tag listed in `tag_keys` read as a map holding its key; else the first problem |
| a node | `Node` | `Node` | `Node` | a value as written: its kind, its value and its position |

### Diagnostics

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| a problem | `Diagnostic` | `Diagnostic` | `Diagnostic` | its severity, its position, if it has one, its message, and its notes: label, rule, schema and help |
| the severity | `Severity::Error`, `Severity::Warning` | `'error'`, `'warning'` | `SeverityError`, `SeverityWarning` | whether the problem stops its reader or only warns; the rendered headline's word: `error` or `warning` |
| an error | `Diagnostic::at(position, message)`, `Diagnostic::unplaced(message)` | `Diagnostic.at(position, message)`, `Diagnostic.unplaced(message)` | `DiagnosticAt(position, message)`, `UnplacedDiagnostic(message)` | a problem at a position, or with none |
| a warning | `Diagnostic::warning_at(position, message)` | `Diagnostic.warningAt(position, message)` | `WarningDiagnosticAt(position, message)` | a warning at a position |
| a position | `Position` | `Position` | `Position` | a file, a line and a column, each from 1 |
| a fenced value | `code_span(value)` | `codeSpan(value)` | `CodeSpan(value)` | the value in the fewest backticks it does not hold, padded with a space where it begins or ends with one |

### Template reader

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| read a template | `read_template(file, text, template)` | `templateReader.readTemplate(file, text, template)` | `ReadTemplate(file, text, template)` | the template's reading, its problems among it |
| a reading | `TemplateReading` | `TemplateReading` | `TemplateReading` | the masked text, ready for the YAML reader; each action, with its position, its text and whether it is a directive; the conditionals and loops, nested as written; each loop variable read inside an action, with the loop that binds it; each template variable read inside an action, with its position and name; and every problem at its directive or action |
| a conditional | `TemplateConditional` | `TemplateConditional` | `TemplateConditional` | its branches, each with the action that opens it, its body's conditionals and loops, and the action that closes it |
| a loop | `TemplateLoop` | `TemplateLoop` | `TemplateLoop` | the action that opens it, the loop variables it binds, its body's conditionals and loops, and the action that closes it |

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

### Format function

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| format | `format_layout(layout, operands)` | `formatLayout(layout, operands)` | `FormatLayout(layout, operands)` | the text the specification's format function writes; else its first problem |
| read a layout | `parse_layout(layout)` | `parseLayout(layout)` | `ParseLayout(layout)` | the layout's parts, each its literal text, `%%` read as text, or a verb: its letter, its flags, its width and precision, and its date format, none where left out; a date format is its parts, each its literal text, `%%` read as text, or a date verb with its letter, flags and width, and its offset as written; else the layout's first problem |

- An operand is the language's JSON value: `serde_json::Value` in Rust, and in JavaScript and Go the value its JSON parser reads, Go's integer types also read as whole numbers.

### Resolved values

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| a value | `resolved::Value` | `resolved.Value` | `ResolvedValue` | a document value with its position: a scalar, a list, a map, or a reference, a call or a template |
| from YAML | `Value::from_node(node)` | `Value.fromNode(node)` | `ResolvedValueFromNode(node)` | the value a node reads to |
| a map entry | `resolved::MapEntry` | `{ key, keyPosition, deferredKey, value }` | `MapEntry` | a key as written and its value, with the deferred key read as a value, `deferred_key`, `deferredKey` or `DeferredKey`; none for a resolved key |

### Schema validation

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| a schema cache | `SchemaCache::default()`, `.with_capacity(capacity)`, `.with_store(store)` | `new SchemaCache({ capacity, store })` | `NewSchemaCache(WithCapacity(capacity), WithStore(store))` | a cache holding at most `capacity` schemas, the least recently asked for dropped first, every schema without one; and keeping what it prepares from each schema in `store`, none without one |
| a schema store | `SchemaStore` trait: `read(key)`, `write(key, prepared)` | `{ read(key), write(key, prepared) }` | `SchemaStore` interface: `Read(key)`, `Write(key, prepared)` | the caller's place for prepared schemas and kept copies, by the key the cache names |
| a folder store | `SchemaFolderStore::new(folder, capacity)` | `new SchemaFolderStore(folder, capacity)` | `NewSchemaFolderStore(folder, capacity)` | a schema store keeping each entry as a file in `folder`, at most `capacity` of them, the least recently used dropped first |
| load a schema | `SchemaCache::load(name, text)` | `SchemaCache.load(name, text)` | `SchemaCache.Load(name, text)` | the schema, read once per name and text, in its draft, with its function table; else why it does not load |
| the rules | `ValidationRules::from_validator(validator)` | `ValidationRules.fromValidator(validator)` | `ValidationRulesFromValidator(validator)` | a DSL definition's `validator` block, its patterns compiled; no rules for none |
| validate | `LoadedSchema::validate(document, rules)` | `LoadedSchema.validate(document, rules)` | `LoadedSchema.Validate(document, rules)` | every problem of a resolved document, read by the rules, in document order |
| problems | `LoadedSchema::problems(document, rules)` | `LoadedSchema.problems(document, rules)` | `LoadedSchema.Problems(document, rules)` | the problems `validate` gives, each with its subject: the JSON pointer of the value it is about |
| problems of a fragment | `LoadedSchema::fragment_problems(pointer, document, rules)` | `LoadedSchema.fragmentProblems(pointer, document, rules)` | `LoadedSchema.FragmentProblems(pointer, document, rules)` | the problems of a document read against the schema's node at `pointer`, such as `/$defs/queue`, each with its subject |
| a subject's steps | `json_pointer_steps(pointer)` | `jsonPointerSteps(pointer)` | `JSONPointerSteps(pointer)` | each step of a JSON pointer, `~1` read as `/` and `~0` as `~`, the root's pointer giving none; else why the text is no JSON pointer |
| a child value | `child_value(key, patterns)` | `childValue(key, patterns)` | `ChildValue(key, patterns)` | the field and value a key sets on the entry it keys |
| a closest name | `closest_name(name, candidates)` | `closestName(name, candidates)` | `ClosestName(name, candidates)` | the candidate a misspelt name most likely means |

### Schema resolution

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| find a document's schema | `SchemaCache::document_schema(file, text, search_paths, config_root)` | `SchemaCache.documentSchema(file, text, searchPaths, configRoot)`, a promise | `SchemaCache.DocumentSchema(ctx, file, text, searchPaths, configRoot)` | the document's schema finding: its modeline's location tried, then each search path in order |
| read a schema | `SchemaCache::read_schema(location, written_at)` | `SchemaCache.readSchema(location, writtenAt)`, a promise | `SchemaCache.ReadSchema(ctx, location, writtenAt)` | the schema finding of a location named outside a document, its one candidate |
| a document's modeline | `document_modeline(text)` | `schemaResolution.documentModeline(text)` | `DocumentModeline(text)` | the modeline of a document's text, as a `SchemaModeline`: the schema URI it names, the fragment after its `#`, and the line and column that URI starts at; none without one |
| a search path | `SchemaSearchPath { path_text, written_at }` | `{ pathText, writtenAt }` | `SchemaSearchPath{PathText, WrittenAt}` | a `schema_search_paths` entry, and the position it is written at, if it has one |
| a schema finding | `SchemaFinding { tried_candidates, found_schema, finding_problems }` | `{ triedCandidates, foundSchema, findingProblems }` | `SchemaFinding{TriedCandidates, FoundSchema, FindingProblems}` | each candidate tried, in order; the schema found, if one is; and the finding's problems, its warnings among them |
| a candidate | `SchemaCandidate { schema_location, named_by }` | `{ schemaLocation, namedBy }` | `SchemaCandidate{SchemaLocation, NamedBy}` | a location as resolved, and what named it: the modeline at its position, the search path at its index, or the caller |
| a found schema | `FoundSchema { schema_name, schema_text, schema_fragment, found_candidate }` | `{ schemaName, schemaText, schemaFragment, foundCandidate }` | `FoundSchema{SchemaName, SchemaText, SchemaFragment, FoundCandidate}` | the name the schema loads under, its text, the fragment its location names, if it names one, and the candidate it was read from |
| a URL reader | `SchemaUrlReader` trait: `url_prefixes()`, `reads_remote_sources()`, `read_schema_url(request)` | `{ urlPrefixes, readsRemoteSources, readSchemaUrl(request) }`, its answer a promise | `SchemaURLReader` interface: `URLPrefixes()`, `ReadsRemoteSources()`, `ReadSchemaURL(ctx, request)` | a reader of URLs: the prefixes of the URLs it reads, such as `s3://`, whether its sources are remote, and its answer to a request |
| register a reader | `.with_url_reader(reader)` | `new SchemaCache({ urlReaders })` | `WithURLReader(reader)` | a cache reading the URLs of the reader's prefixes beside the core's |
| a request | `SchemaUrlRequest { fetch_url, machine_credentials, kept_copy_version, answer_deadline }` | `{ fetchUrl, machineCredentials, keptCopyVersion, signal }` | `SchemaURLRequest{FetchURL, MachineCredentials, KeptCopyVersion}`, its deadline `ctx`'s | the URL without the cache's parameters, whether to read it with the machine's credentials, the kept copy's version, if one is kept, and when the answer is due |
| an answer | `SchemaUrlAnswer { read_outcome, read_refusals }` | `{ readOutcome, readRefusals }` | `SchemaURLAnswer{ReadOutcome, ReadRefusals}` | what the read came to, and each refusal the source gave in it |
| an outcome | `SchemaUrlOutcome::TextSent { schema_text, copy_version }`, `CopyConfirmed`, `SourceMissing { missing_reason }`, `ReadRefused`, `ReadFailed { failure_reason }` | `{ outcomeKind, … }`, the kind `'textSent'`, `'copyConfirmed'`, `'sourceMissing'`, `'readRefused'` or `'readFailed'`, with the same fields | `SchemaURLOutcome{OutcomeKind, …}`, the kind `OutcomeTextSent`, `OutcomeCopyConfirmed`, `OutcomeSourceMissing`, `OutcomeReadRefused` or `OutcomeReadFailed`, with the same fields | the text with its version; the kept copy confirmed current; a source that is not there, and why; a read the source refused; or a read that failed, and why |
| an HTTP read | `http_get(url, headers, answer_deadline)` | `httpGet(url, headers, signal)`, a promise | `HTTPGet(ctx, url, headers)` | a server's answer to a GET, as an `HttpReply`: its status, the copy version its `ETag` and `Last-Modified` give, and, for a `200`, its text read as UTF-8; a redirect followed without the `Authorization` header; else why there is none |
| the conditional headers | `http_conditional_headers(kept_copy_version)` | `httpConditionalHeaders(keptCopyVersion)` | `HTTPConditionalHeaders(keptCopyVersion)` | the headers asking a server whether a kept copy is current: `If-None-Match` with its `etag`, else `If-Modified-Since` with its `last_modified` |
| a host's login | `netrc_login(host)` | `netrcLogin(host)` | `NetrcLogin(host)` | the login the machine's `.netrc` gives `host`, its `machine` entry else its `default` entry, as a `HostLogin`: where it came from, its name and password, what renews it, and the `Authorization` header it is sent as; none without one |
| a refusal | `ReadRefusal { refused_by, credential_origin, refusal_reason, renewal_fix }` | `{ refusedBy, credentialOrigin, refusalReason, renewalFix }` | `ReadRefusal{RefusedBy, CredentialOrigin, RefusalReason, RenewalFix}` | who refused, such as `AWS`; where the credential refused came from, such as the `.netrc` entry for a host, none for a read without credentials; what the source answered; and what renews the credential, if it has one |

- A copy version is the language's JSON value, as a format operand is.

## Schema validation

A document is read, then validated against its schema in the draft its `$schema` names.

### A schema that does not load

Why a schema does not load reads `schema <name>: <why>`, `<name>` the name it was loaded with. The package states why in its own words, the same in every language:

| Why | Reads |
|---|---|
| a `$ref` whose JSON pointer names no node of the schema | `` `#/$defs/missing` names no node of the schema `` |
| a `pattern`, or a `patternProperties` key, the standard's pattern dialect does not compile | `` `(_x` is not a pattern `` |
| a draft the package does not read | `declares <draft>, a draft this reader does not read; it reads <drafts>` |

Any other failure is the validator library's text, naming the schema by the name it was loaded with.

### Reading before validation

In order, each by the DSL's `validator` block:

1. A key matching a pattern of `child_values_from_parent_key` sets its entry's `field` to `value`, unless the entry writes the field.
2. In every map, the content of each key `merge_base_keys` lists is merged into each sibling map, as an overlay merges over its common layer: the sibling over the content.
3. A quoted value that spells a type `allow_quoted_values` lists, where the schema expects that type, is validated as that type: `true` and `false` a boolean, `-?(0|[1-9][0-9]*)` an integer, and a number a fraction and an exponent beside. One that spells none stays a string.

- A value not known until the document compiles, a call, a reference or a template, is not checked, nor a check that reads a value below it through a subschema or compares the values below it.
- An `anyOf` or `oneOf` none of whose forms holds is reported when each form fails a check that reads no unknown value.
- A call of a `merge` macro of the schema's function table is validated as the known maps among its arguments, merged in order by the macro's `merge_behavior`. With an argument not known, it requires no key, as a merge base key's content does, since that argument may supply any. A call with no known map among its arguments is not checked.
- An unknown field or item of a map or list holding such a value is reported unless the schema could read it by a keyword below, reached through `$ref`, `allOf`, `anyOf`, `oneOf`, `if`, `then`, `else` or `dependentSchemas`. A field or item the schema could read depends on a branch that reads the unknown value, and is not checked.
- A problem on a value an entry has only from a merge base key is not reported.
- A merge base key's content requires no key: a missing field under it is not reported, nor an `anyOf` or `oneOf` with a form that fails only by fields it requires.
- Formats are not validated.

| Unknown | Read by |
|---|---|
| a field | a `properties` entry, a matching `patternProperties` pattern, an `additionalProperties` other than `false`, or a nested `unevaluatedProperties` other than `false` |
| an item | a `prefixItems` entry at its index, an `items` array entry at its index, an `additionalItems` other than `false` past it, an `items` schema other than `false`, a `contains`, or a nested `unevaluatedItems` other than `false` |

### Requiredness markers

A field's `description` may open with a requiredness marker:

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

A message is the package's own sentence. Every key and value it names is fenced, so a reader tells them from its prose; an item number, a position the package counts, is not. A schema's own words never enter a message: a description the problem reports is its schema note, verbatim, its requiredness marker included, and a reader shows it apart from the message, prefixed `[schema]`.

| Failure | Message | Schema note |
|---|---|---|
| a missing field | `` `name` is required `` | |
| an unknown field, by an `additionalProperties` or `unevaluatedProperties` that is `false` | `` `name` is not defined in the schema ``, with no label | |
| a key `propertyNames` refuses | `` `key` is not a valid key of <field> `` | |
| `type` | `` <field> must be `boolean` ``, the types the schema lists, in its order, joined by `or` | |
| `enum` | `` <field> must be one of `"a"`, `"b"` ``, each value as JSON | |
| `const` | `` <field> must be `"a"` ``, the value as JSON | |
| `anyOf` or `oneOf`, no form holds | `<field> matches none of its allowed forms` | where the alternatives are those of a definition, `#/$defs/<name>` or `#/definitions/<name>`, the definition's description |
| `anyOf` or `oneOf`, no form holds, and every alternative states a `const` | `` <field> must be one of `"a"`, `"b"` ``, each alternative's `const` as JSON, in the schema's order, as an `enum` of them reads | as the row above |
| `oneOf`, several forms hold | `<field> matches more than one of its allowed forms` | |
| a dependency not met | `` `key` needs <what> ``: what each dependency of the key needs, from the value's schema and every `allOf` part and reference of it, in the order the schema writes them, joined by `;`. A dependency needs the fields it lists, joined by `and`, a forbidden one as `` `x` must not be set ``, a definition it names as `` a `name` ``, and its forms joined by `, or` | |
| `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum` | `` <field> must be >= `1` ``, with `<=`, `>` and `<` | |
| `multipleOf` | `` <field> must be multiple of `5` `` | |
| `minLength`, `maxLength` | `` <field> must NOT have fewer than `3` characters ``, or `more than` | |
| `minItems`, `maxItems` | `` <field> must NOT have fewer than `3` items ``, or `more than` | |
| `minProperties`, `maxProperties` | `` <field> must NOT have fewer than `3` properties ``, or `more than` | |
| items beyond a tuple that allows none | `` <field> must NOT have more than `2` items ``, the tuple's length | |
| `unevaluatedItems` that is `false` | `` <field> must NOT have more than `2` items ``, the items evaluated | |
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
- A field or item an `additionalProperties`, `unevaluatedProperties` or `unevaluatedItems` schema other than `false` reads reports that schema's failures as its own, at its value.
- A problem's rule names the keyword and where the schema holds it; its label says what is wrong where it is placed.
- A problem's subject is the JSON pointer of the value it is about in the document validated: a reader that builds the document from another, such as one expanding macros, maps it back to its source.

### Fragments

A document whose schema is a fragment, `schema.json#/$defs/queue`, is validated against that node of the schema, its `$ref`s resolved against the whole schema. A problem's rule names where the whole schema holds the keyword, and its field reads from the document's root. A fragment that names no node of the schema is one problem at the document's root, `` the schema has no node at `#/$defs/missing` ``.

A fragment requires no key: a missing field is not reported, nor an `anyOf` or `oneOf` with a form that fails only by fields it requires.

### Schema cache

A schema is loaded once per name and text: prepared, then built into the validator the package validates with.

- Prepared is the package's own information: the draft the schema is read in, the schema as written, and the schema its requiredness markers are read into. Its form is the same in every language, `{"draft": "draft-2020-12", "schema": …, "marked": …}`, and it holds nothing of the validator library.
- With a capacity, the cache holds at most that many schemas, the least recently asked for dropped first; without one, every schema loaded is held.
- With a store, the cache reads a schema's prepared information from it before preparing the schema, and writes what it prepares. The key is a SHA-256 of the package's name and version, the schema's name and its text, so another version never reads it. An entry that does not read is prepared again from the text.
- The store is the caller's: where it keeps an entry, and whether it keeps one, is the caller's to decide.
- The folder store is the package's store for a caller keeping entries on disk: each entry is a file named by its key, reading or writing an entry marks it used, and a write drops the least recently used entries beyond the capacity.

## Schema resolution

A document's schema is found, then read, through a schema cache: each location by the reader of its URL's prefix. The specification states where a document's schema is looked for and how a schema URL is read; this section states how the package divides that between the cache and its readers.

### Locations

A location is a URL. A path stands for its `file` URL:

| Path | Resolved against |
|---|---|
| a modeline's | the document's folder |
| a search path's | the document's folder, or the config's folder when it starts with `{config_root}` |
| one given to `read_schema` | the working directory |

- A location's fragment is taken off before the read, and given with the schema found.
- The `yaml_dsl_` parameters are the cache's: it reads them and takes them out. Every other parameter is the protocol's, and reaches the reader as written, in its order.
- A `*` step is matched in a `file` location, the first file in path order taken.

### Readers

A reader reads the URLs of its prefixes. A cache holds the core's readers and each reader its consumer registers.

| Prefix | Reader | Sources |
|---|---|---|
| `file://` | the core's | on the machine |
| `http://`, `https://` | the core's, with the `.netrc` entry for the link's host | remote |
| `s3://` | the S3 schema reader, an extra package | remote |
| `https://github.com/`, `https://www.github.com/` | the GitHub schema reader, an extra package | remote |

- A URL is read by the reader of the longest prefix it starts with, prefixes compared without regard to case.
- The core's readers give a source that is not there as `no file is there`, `no file matches` for a `*` pattern, and `the server answered 404`.
- A remote source's text is kept as a copy. A source on the machine is read each time.
- A second reader registered for a prefix faults where the cache is made.

### A read

A remote source is read in this order:

1. The copy kept under the URL is given while it is younger than its max age.
2. The reader is asked for the URL without the machine's credentials, with the kept copy's version.
3. Where it answers `ReadRefused` and the URL carries `yaml_dsl_machine_credentials=true`, it is asked again with them.

A source on the machine takes step 2 alone.

| Outcome | The cache |
|---|---|
| `TextSent` | gives the text, and keeps it as the URL's copy with its version |
| `CopyConfirmed` | gives the kept copy, and restarts its age |
| `SourceMissing` | reads no copy: in a finding, the next candidate is tried |
| `ReadRefused` | gives the kept copy with a warning; a problem where none is kept |
| `ReadFailed` | gives the kept copy; a problem where none is kept |

- The credential rule is the cache's, the same for every protocol: a reader reads with the machine's credentials only when asked to.
- A refusal given before the source let a credential in is a warning.
- Reads of one URL at once run in turn: the first fetches it and keeps its copy, and the others wait for it and read that copy.
- Every request carries the time its answer is due, the cache's limit for one read.

### A reader's part

- It reads the URL as given. A parameter its protocol does not take is `ReadFailed`.
- Asked to read with the machine's credentials, it tries each credential its protocol has, in the protocol's order, and lists each one the source refused.
- It reads the machine's credentials when first asked for them, and again after a read in which every one was refused, so a renewed login takes effect on the next read.
- A copy version is the reader's own: what it needs to ask its source whether the kept copy is current. The cache keeps it unread. A version the reader does not read is read as none.
- It answers several requests at once.
- An answer not ready when it is due is `ReadFailed`.

### Kept copies

- A copy is an entry of the cache's store, its key a SHA-256 of the URL without the cache's parameters. A cache without a store keeps none, and asks the reader on every read.
- Its form is the same in every language: `{"url": …, "copy_version": …, "confirmed_at": …, "body": …}`, `confirmed_at` the time the source last sent or confirmed it, in seconds since the epoch.
- An entry that does not read is fetched again.

### Finding

The candidates are the modeline's location, then each search path, in order.

- The first candidate whose source is there is the schema. A `file` pattern no file matches is a missing source.
- A candidate whose read is refused or fails with no copy kept ends the finding with that problem.
- Every candidate tried is listed with what named it, so a consumer can watch a missing one for its arrival, and tell which entry named the schema found.
- A schema read from a `file` location loads under the file's name, any other under its location as written.

### Problems

A problem is placed where its location is written: a modeline's at the location in the modeline, a search path's at its `written_at`, and one given to `read_schema` at the `written_at` given, with no position without one. A search path without a position, and a finding whose every source is missing, is placed at the document's modeline line, or its first line without a modeline.

| Problem | Message | Label | Help |
|---|---|---|---|
| every candidate's source is missing | `no schema found` | | each candidate, `` `<location>`: <missing reason> ``, one a line |
| a source missing at a location given to `read_schema` | `` `<location>` is missing `` | the missing reason | |
| a read that failed | `` reading `<location>` failed `` | the failure reason | |
| a read refused | `` <refused by> refused to read `<location>` `` | each refusal, `<credential origin>: <reason>`, or its reason alone for a read without credentials, joined by `, ` | each renewal fix, once, joined by `; `; for a URL without `yaml_dsl_machine_credentials=true`, `` add `yaml_dsl_machine_credentials=true` to read it with the machine's credentials `` |
| a parameter of the cache's that does not read | `` `yaml_dsl_mock` is not a parameter of a schema URL ``, `` `yaml_dsl_max_age_days` must be a whole number of days ``, `` `yaml_dsl_machine_credentials` must be `true` or `false` `` | | |
| a URL no reader reads | `` no reader reads `<location>` `` | | |

| Warning | Message | Label | Help |
|---|---|---|---|
| a kept copy given for a read refused | `` <refused by> refused to read `<location>`, so its kept copy was read `` | as a read refused | as a read refused |
| a refusal before a credential was let in | `` <refused by> refused <credential origin> `` | its reason | its renewal fix |

## Extra packages

An extra package is built in the languages its readers use, and carries the core's version.

| Package | Rust | JavaScript | Go |
|---|---|---|---|
| the diagnostic renderer | `yaml-dsl-spec-diagnostic-renderer` | | `github.com/litebitus/yaml-dsl-spec/go/diagnostic_renderer` |
| the S3 schema reader | `yaml-dsl-spec-schema-reader-s3` | `yaml-dsl-spec-schema-reader-s3` | `github.com/litebitus/yaml-dsl-spec/go/schema_reader_s3` |
| the GitHub schema reader | `yaml-dsl-spec-schema-reader-github` | `yaml-dsl-spec-schema-reader-github` | `github.com/litebitus/yaml-dsl-spec/go/schema_reader_github` |

### Diagnostic renderer

A diagnostic written as a compiler writes an error, for a terminal or a log. Every language renders the same text for the same input within the subset "Division to width" states; `conformance/diagnostic-rendering/` holds the cases.

| Entry | Rust | Go | Gives |
|---|---|---|---|
| render a diagnostic | `render_diagnostic(diagnostic, source, style)` | `RenderDiagnostic(diagnostic, source, style)` | the diagnostic's text, headed by its severity's word |
| the source | `DiagnosticSource { name, text }` | `DiagnosticSource{Name, Text}` | the name the `-->` line shows, such as a path or `dashboard:"Mock Service"`, and the text the position counts lines and columns in |
| the style | `RenderStyle { color, width }` | `RenderStyle{Color, Width}` | whether the text is painted for a terminal, and the terminal width its lines divide to: none, `0` in Go, for output that is not a terminal |

```
error: `brnch` is not defined in the schema
  --> services/mock-api/mock-dsl.yml:57:5
   |
52 | workflow:
53 |   publish:
...
55 |     tags: [mock]
56 |     branch: main
57 |     brnch: main
   |     ^^^^^
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
| the carets and the label | the severity's: red, `91`, under an error; yellow, `33`, under a warning |
| `rule:` and `help:` | cyan, `36` |
| `[schema]` | bold purple, `#A6ABF2` |
| the schema note's text | purple, `#A6ABF2` |

### S3 schema reader

The URL reader of `s3://<bucket>/<key>`, read with the AWS SDK of its language.

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| the reader | `S3SchemaReader::new(role_session_name)` | `new S3SchemaReader(roleSessionName)` | `NewS3SchemaReader(roleSessionName)` | a URL reader of `s3://` URLs, a role assumed under `role_session_name`, the consumer's name for itself |

- Without the machine's credentials, an object is read by an unsigned request.
- The machine's credentials are the SDK's default chain: the AWS environment variables, the profile `AWS_PROFILE` names with its SSO login, a container's or an instance's role.
- `assume_role=<role ARN>`, the one parameter the protocol takes, reads the object as that role, assumed with the machine's credentials. A role's credentials are kept, and renewed as they expire.
- An object is read in its bucket's own region, whatever region the machine is set to.
- Where the machine's AWS configuration names an endpoint, such as `AWS_ENDPOINT_URL`, an object is read by its path, `<endpoint>/<bucket>/<key>`, as an S3-compatible server serves it.
- A copy version is the object's `ETag`, and a read asks with it whether the kept copy is current.

| S3 answers | Outcome | Renewal fix |
|---|---|---|
| the object | `TextSent` | |
| the kept `ETag` still current | `CopyConfirmed` | |
| a key or a bucket that is not there | `SourceMissing`, `` no key `<key>` in bucket `<bucket>` `` or `` no bucket `<bucket>` `` | |
| credentials expired, absent or not valid | `ReadRefused` | `` run `aws sso login`, or set `AWS_PROFILE` to a profile that reads it `` |
| access denied, read as a role | `ReadRefused` | `` allow <role> `s3:GetObject` on it `` |
| access denied, read as the machine | `ReadRefused` | `` use a profile allowed `s3:GetObject` on it, such as `AWS_PROFILE=<profile>` `` |
| the role's assumption denied | `ReadRefused` | `` use a profile allowed to assume <role>, such as `AWS_PROFILE=<profile>` `` |
| anything else | `ReadFailed` | |

- A refusal is refused by `AWS`, its reason the error's code and the identity it names, such as `AccessDenied for arn:aws:iam::<account>:user/<name>`.

### GitHub schema reader

The URL reader of GitHub links, `https://github.com/` and `https://www.github.com/`.

| Entry | Rust | JavaScript | Go | Gives |
|---|---|---|---|---|
| the reader | `GithubSchemaReader::new()` | `new GithubSchemaReader()` | `NewGithubSchemaReader()` | a URL reader of GitHub links |

- Without the machine's credentials, a link is read as written.
- With them, a release asset link, `<owner>/<repo>/releases/download/<tag>/<asset>` or `<owner>/<repo>/releases/latest/download/<asset>`, is read through GitHub's API with each token in turn: `GH_TOKEN`, `GITHUB_TOKEN`, the `gh` command's login (`gh auth token`), then the `.netrc` entry for `api.github.com`. A token the machine lacks is skipped: a variable that is not set, `gh` not installed or not logged in, a `.netrc` without the entry.
- The release comes from the link's path, the tag or the latest; the asset is read by its name, following GitHub's redirect without the token.
- Any other GitHub link is read as written, the machine's credentials never sent.
- A copy version is the answer's `ETag` and `Last-Modified`, and a read asks with them whether the kept copy is current.

| GitHub answers | Outcome | Renewal fix |
|---|---|---|
| the link or the asset | `TextSent` | |
| the kept copy still current | `CopyConfirmed` | |
| 401, 403, or 404, which GitHub gives for a private repository, to a read without credentials | `ReadRefused` | |
| a release the repository lacks | `SourceMissing`, `` `<owner>/<repo>` has no release `<tag>` `` | |
| an asset the release lacks | `SourceMissing`, `` release `<tag>` of `<owner>/<repo>` has no asset `<asset>` `` | |
| 401 or 403 to a token | the next token tried, the refusal listed | `` replace `GH_TOKEN` ``, `` replace `GITHUB_TOKEN` ``, `` run `gh auth login` ``, or `` update the `.netrc` entry for api.github.com ``, by the token |
| every token refused | `ReadRefused` | |
| no token set | `ReadRefused` | `` set `GH_TOKEN`, run `gh auth login`, or add a `.netrc` entry for api.github.com `` |
| a repository no token can see | `ReadFailed`, `` no credential can see `<owner>/<repo>` `` | |
| anything else | `ReadFailed` | |

- A refusal is refused by `GitHub`.

## TODO

- Schema resolution is built as "Schema resolution", "S3 schema reader" and "GitHub schema reader" state, in all three languages, and the specification's "Finding the schema" and "Schema URLs" state its rules. [tfgen](https://github.com/litebitus/yaml-dsl-terraform-generator) and the [YAML DSL editor](https://github.com/litebitus/vscode-yaml-dsl-editor) find and read schemas through it in place of their own code, and a consumer reading a problem's subject, such as [zerox](https://github.com/litebitus/zerox) finding the input a problem is about, reads its steps through `json_pointer_steps` in place of its own unescaping.
- Deprecation warnings join the core as an opt-in: a deprecated field or value a document uses is reported as a warning.
- Message bundles, a separate pass and release. Every message the packages write comes from a bundle, one per language, shared by all three languages:
  - A bundle is `messages/<tag>.json`, the tag a BCP 47 language tag such as `en` or `zh-Hant-TW`. Each package's build copies the bundles in, as it copies the schemas.
  - An entry is a sentence with named slots, `{field} needs {needs}`, keyed by what it reports. The words of a language are the bundle's: the list joiners, `the document`, a label's fixed parts. The code fills the slots: fences, JSON pointers, item numbers.
  - Every bundle holds every key; a package's test fails on a bundle missing one or holding an extra one.
  - The covered messages: the definition reader's, schema validation's, a schema that does not load, and the diagnostic renderer's own words.
  - A caller passes a messages value loaded for a tag to the schema cache and the definition reader; without one, messages are `en`. A tag is matched to a bundle by RFC 4647 Lookup, written by the package: the tag loses its last subtag until a bundle matches, and one that matches none is an error. A tag is parsed and canonicalized by `Intl.Locale` in JavaScript, `golang.org/x/text/language` in Go and `icu_locale` in Rust.
  - Conformance runs in `en`, and a case of its own covers Lookup.
