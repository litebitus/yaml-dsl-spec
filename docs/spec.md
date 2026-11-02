# The YAML DSL standard

A YAML DSL is defined by configuration: a block of a `yaml-dsl.yml` states which files are the DSL's, its layers, scopes, declarations, references, placeholders and calls, and where its documents' schemas are. This document specifies what a `yaml-dsl.yml` means, so every reader of a DSL reads it the same way. A reader is any tool that reads a DSL's documents, such as the [YAML DSL editor](https://github.com/litebitus/vscode-yaml-dsl-editor) or [yaml-dsl-compiler](https://github.com/litebitus/yaml-dsl-compiler).

The fields are described in the JSON Schema of each version, `schemas/yaml-dsl-v<version>.schema.json`, and versions are covered in the README. The standard deprecates only its own forms, never a feature a DSL may choose.

## Generic by config

Any DSL written this way is a block in the workspace config, `yaml-dsl.yml`. Nothing about a particular DSL is compiled into a reader, and the config states every rule: a reader has no defaults, because every DSL is different. A DSL's block states:

- which files belong;
- its scopes;
- where names are declared, and how they are referenced;
- its placeholders and functions;
- when the DSL has layers, which directories are overlays;
- a fallback schema for a document that names none.

Scopes, references, placeholders and calls are generic: a reader reads them the same way in every DSL. Each DSL's block supplies the syntax, and those syntaxes differ.

## Common layer and overlays

A layered DSL's stack is one common layer and its overlays. The engine deep-merges the common layer under the overlay at the common layer's root, and the folded document is what a plan sees.

`layers.common_layer_discovery` states how an overlay and its common layer are paired:

| Value | The pairing | The common layer's root |
|---|---|---|
| `parent` | an overlay directory holds the overlay itself, its parent the common layer | `$` |
| `ancestor` | the overlay may sit deeper, at the same path below the overlay directory in every overlay | `$` |
| `overlay_includes` | an include in the overlay names the common layer | the map holding the include |

For `parent` and `ancestor`:

- Each overlay is a `mock-dsl.yml` under an overlay directory, one `layers.overlay_folders` names, such as `overlay-1` or `overlay-2`, directly below the common layer's directory.
- A file below no overlay directory is a common layer.
- An overlay directory that holds the overlay's place but no overlay file is an overlay holding no documents: its folded document is the common layer alone.
- The overlay's place is where its file would be: the overlay directory itself for `parent`; for `ancestor`, the path below the overlay directory that the stack's other overlays take, or the concern for a `{concern}` common layer.
- An overlay holding no documents finds its schema as a file at its place with no modeline: by the DSL's search paths from there.
- A common layer with no overlay file in any overlay directory is a document with no overlays, read as if no overlay directory existed: a stack of its own.

| Value | The overlay file | Example |
|---|---|---|
| `parent` | is in the overlay directory itself | `concern-1/overlay-1/mock-dsl.yml` |
| `ancestor` | may sit deeper, at the same path below the overlay directory in every overlay | `concern-group/overlay-1/concern-1/mock-dsl.yml` takes `concern-group/mock-dsl.yml`, the nearest ancestor above an overlay directory |

### Concerns

A file's concern is the nearest directory above it that is not an overlay directory:

| File | Concern | Kind |
|---|---|---|
| `concern-1/overlay-1/mock-dsl.yml` | `concern-1` | a concern above its overlays |
| `concern-group/overlay-1/concern-1/mock-dsl.yml` | `concern-1` | a concern below its overlays |

A concern's overlays and the common layer they take are one stack. `layers.common_layer_files` maps an overlay's file name to the names its common layer may take in the overlay directories' parent:

- The names are tried in order, and the first that exists is the common layer.
- A name holding `{concern}` is a concern's own common layer, tried only for a concern below its overlays.
- A file whose name has no entry takes a common layer of its own name.
- A file a `{concern}` name matches belongs to the DSL as a common layer, whatever `file_includes` says.

```yaml
common_layer_files:
  mock-dsl.yml: ["{concern}.mock-dsl.yml", mock-dsl.yml]
```

Under this entry, `concern-group/overlay-1/concern-1/mock-dsl.yml` takes `concern-group/concern-1.mock-dsl.yml`, else the fallback `concern-group/mock-dsl.yml`.

### Folded documents

- Each overlay's folded document is the common layer merged with that overlay.
- An overlay's `{}` or `[]` at a key the common layer holds is an opt-out: the key is deleted from the fold, at any depth. At a key the common layer does not hold, it is content.
- Shared config lives in the common layer, a local that an entry references included.

### Concern above its overlays

```
concern-1/
├── mock-dsl.yml  # common layer of concern-1
├── overlay-1/mock-dsl.yml  # overlay-1 of concern-1
└── overlay-2/mock-dsl.yml  # overlay-2 of concern-1
concern-2/
├── mock-dsl.yml  # common layer of concern-2
├── overlay-1/mock-dsl.yml  # overlay-1 of concern-2
└── overlay-2/mock-dsl.yml  # overlay-2 of concern-2
```

```yaml
layers:
  overlay_folders: [overlay-1, overlay-2]
  common_layer_discovery: parent
```

### Concern below its overlays

```
concern-group/
├── mock-dsl.yml  # fallback common layer, for a concern without its own
├── concern-1.mock-dsl.yml  # common layer of concern-1
├── overlay-1/
│   ├── concern-1/mock-dsl.yml  # overlay-1 of concern-1
│   └── concern-2/mock-dsl.yml  # overlay-1 of concern-2
└── overlay-2/
    ├── concern-1/mock-dsl.yml  # overlay-2 of concern-1
    └── concern-2/mock-dsl.yml  # overlay-2 of concern-2
```

```yaml
layers:
  overlay_folders: [overlay-1, overlay-2]
  common_layer_discovery: ancestor
  common_layer_files:
    mock-dsl.yml: ["{concern}.mock-dsl.yml", mock-dsl.yml]
```

A DSL with both takes this config, since `ancestor` also reaches a concern above its overlays.

### Overlay includes

With `overlay_includes`, an overlay names its common layer in an include, a key whose value holds the common layer's location. `layers.overlay_includes` lists how includes are written:

| Field | Meaning |
|---|---|
| `key_paths` | the keys whose values are includes, such as `$..['Fn::Transform']`; a value that is a list holds one include per map in it |
| `value_conditions` | the key and value pairs an include's value holds, each with `key` and `value`, such as `Name` and `AWS::Include`; `[]` for none |
| `location_path` | where in the include's value its location is written, a path from the value, such as `$.Parameters.Location` |
| `location_mappings` | each a `remote` location prefix and the `local` path it stands for: a location starting with `remote` names the file at `local` followed by the rest of the location; `local` is relative to the overlay, or to the config's folder when it starts with `{config_root}` |

```yaml
layers:
  common_layer_discovery: overlay_includes
  overlay_includes:
    - key_paths: ["$.Transform", "$..['Fn::Transform']"]
      value_conditions:
        - { key: Name, value: "AWS::Include" }
      location_path: "$.Parameters.Location"
      location_mappings:
        - { local: "{config_root}/snippets/", remote: "s3://mock-bucket/snippets/" }
  duplicate_check: { depth: 2, key_depths: {}, skip_keys: [] }
```

- The common layer's root is the map holding the include, and its entries take the include's place there.
- An overlay with several includes takes several common layers, and is an overlay in each of their stacks.
- A fragment, a file its modeline marks as one under "Finding the schema", is a common layer, merged into the overlays that include it. A full document included by an overlay is a common layer too, and read alone it is validated as a full document.
- A location no mapping covers, or whose mapped file does not exist, declares every name, as a `declares_every_name` key does, and is an info problem.
- A common layer's own includes are not followed, and each is a problem.

## Paths

Every path in the config is a JSONPath query (RFC 9535) in this subset:

| Step | Selects |
|---|---|
| `$` | the document root, the map that holds the top-level keys |
| `.name` | the key `name` |
| `['name']` | the key `name`, for a key `.name` cannot spell, such as `Fn::GetAtt` |
| `.*`, `[*]` | every child of a map or a list |
| `..name`, `..['name']` | the key `name` at any depth below the path before it |
| `..*`, final only | every node below the path before it, not that node itself |

- A node and everything below it are two entries, `P` and `P..*`.
- An entry's `skip_keys` lists keys a `.*`, `[*]` or `..*` step does not select or descend into.
- In a list of path entries, the path names the entry and appears once.

## Patterns

Every pattern in a `yaml-dsl.yml` is a regular expression in this subset of PCRE syntax, which is what a DSL's definition needs. A pattern beyond the subset has undefined behavior: a reader may read it, reject it, or read it otherwise than another reader.

| Construct | Written |
|---|---|
| a character, or a character escaped | `a`, `\.`, `\$`, `\{`, `\\` |
| a newline | `\n` |
| any character | `.` |
| a character class, with ranges and negation | `[a-z0-9_]`, `[^}\n]` |
| the start and the end of the text | `^`, `$` |
| a group, a non-capturing group, a named group | `(…)`, `(?:…)`, `(?<name>…)` |
| alternation | `a\|b` |
| repetition | `*`, `+`, `?` |
| lookahead and lookbehind | `(?=…)`, `(?!…)`, `(?<=…)`, `(?<!…)` |

A rule reads its named groups by name: a placeholder's `body`, a child value's `field` and `value`, and a reference's `name_group` and `scope_group`.

## YAML tags

A DSL whose YAML tags stand for keys lists them in `tag_keys`; a DSL without it reads tags as YAML does:

| Field | Meaning |
|---|---|
| `tag` | the tag as written, such as `!Ref` |
| `key` | the key the tagged node stands for, such as `Ref` |

- A node carrying a listed tag reads as a map holding that key, the node its value: `!Ref Bucket` reads as `Ref: Bucket`, and `!If [IsProd, a, b]` as `Fn::If: [IsProd, a, b]`.
- Paths and validation read the expanded form, so a short and a long form of one value read alike.
- A reference is known by its syntax, and a reference rule reads a scalar with its tag as written: `!Ref Bucket` is the text `!Ref Bucket`. A scalar that is the value of a listed key reads with that key's tag, so `Ref: Bucket` is the text `!Ref Bucket` too.
- The text keeps the tag as written.

## Scopes

A DSL is a language, and its names live in scopes. Every scope a rule names is declared under `scopes`:

| Field | Meaning |
|---|---|
| `regions` | where the scope's names may be referenced: each path's region is the subtree under every node it selects, in any file of the fold; `$` is all of it |
| `later_items_of_declaring_list` | a name is also visible in the later items of the list that declares it, at any depth inside them |
| `named_by_parent_key` | the mapping key enclosing a declaration is the scope its name is in |
| `builtin_names` | the names of a logical scope |

- A scope named in capitals is logical: the config lists its `builtin_names`, and the word never appears in a document.
- Any other scope is literal: its name is the DSL's own word, and the document declares its names.
- Builtins valid only in some regions are a logical scope of their own, apart from the true globals.

### Declaration rules

A declaration rule says where names are declared: each node its `path` selects is a declaration.

| Field | Meaning |
|---|---|
| `skip_keys` | keys a step does not descend into |
| `exclude_candidates` | keys at the last step that are not declarations |
| `name_source` | where the name is read: the `key`, the scalar `value`, or the `meta_argument` named by `meta_argument_name` of a key such as `http(id=config)`, one name per listed id |
| `declares_every_name` | the key declares every name of the scope, for a scope whose names are defined in a document a reader does not read, such as one whose path holds a placeholder; each name resolves to the key that names the document |
| `key_token` | the part of the key that is the name: `all_words`, `first_word` or `last_word`; `null` for a name not read from the key |
| `name_spelling` | `as_written` or `dashes_as_underscores` |
| `scope_name` | the scope declared under `scopes` |

### Reference rules

A reference rule says how names are used. `pattern` is a regular expression with named groups.

| Field | Meaning |
|---|---|
| `positions` | where a reference may stand, below |
| `text_after_name_allowed` | whether text such as a field path may follow the name |
| `scope_name` | the scope it resolves in |
| `scope_group` | the pattern group holding the scope of a `named_by_parent_key` scope; `null` otherwise |
| `name_group` | the group holding the name |

| Position | A reference is |
|---|---|
| `whole_scalar` | the scalar |
| `whole_placeholder` | a placeholder that is the whole scalar |
| `placeholder_in_text` | a placeholder inside longer text |
| `anywhere_in_scalar` | each match inside a scalar |

### Resolution

- A reference resolves to a builtin of its target scope whose region holds the reference, else to a symbol of that scope, with that name, visible from the reference.
- When several rules read the same text, each is tried in order, and the first that resolves is the reading.
- A name that holds a local placeholder is also known by the local's value, spelled by its declaration rule: a key `name ${local.thing}` whose local is `mock-thing` is the name `mock_thing`.
- An overlay's own declaration wins over the common layer's, as it does in the merge, and an overlay sees no other overlay's declarations.
- A reference in the common layer resolves in the fold of each overlay whose merge keeps it, and is an error unless it resolves in every one of them. An overlay that replaces or deletes the value or key holding it is not asked. When no overlay keeps it, it resolves in the common layer alone.
- It names the declaration it reaches in each of those folds, in `layers.overlay_folders` order.

## Locals

A local is a named value. `locals.scope_name` names the scope whose declarations are the DSL's locals; that scope is declared under `scopes` like any other, a declaration rule reads its names from keys, and a reference rule reads them. A DSL without the `locals` block has no locals.

- A local's value is its declaration's value as written, with every local it references replaced by that local's value, resolved recursively in the file's fold.
- An overlay's local wins over the common layer's.
- A local that reaches itself through its references is a problem on its declaration, and its value stays as written.
- A reference to anything other than a local stays as written inside the value.
- A key holding a local placeholder is also known by the local's value.
- `locals.path_separator`, when set, reads the text after a local's name as a path into its value, each step a map key or, where the value is a list, an index counted from 0. The local's reference rule allows text after the name.
- A path steps through the locals a value references and the maps its merges bring in.
- A step the value lacks is an error on the reference: `` `local.defaults` holds no `queues` ``. A step into a value known only when the document is used is no problem.

## Loop variables

A loop declares its loop variables within its body. `loops.scope_name` names the scope whose reference rules read loop variables; that scope is declared under `scopes` like any other. A DSL without the `loops` block reads no loop variables.

| DSL | Rule | A loop variable reads |
|---|---|---|
| CloudFormation | `^(?<name>[A-Za-z0-9]+)` in placeholders, `^!Ref (?<name>…)$` | `${TopicName}`, `&{TopicName}`, `!Ref TopicName` |
| a DSL with a `~` sigil | `^~(?<name>[a-z_][a-z0-9_]*)` as a whole scalar and in placeholders, text after the name allowed | `~item`, `~item.label`, `${~item}` |

- A reference by a rule of the loop scope resolves to the innermost loop around it that declares the variable, before any builtin or symbol.
- A reference by a rule of any other scope never resolves to a loop variable.

## Finding the schema

| Source | Used |
|---|---|
| a `# yaml-language-server: $schema=` modeline | first; its path is relative to the file |
| `schema_search_paths`, a list, `[]` when empty | when the file has no modeline, or the path it names is not on disk; each entry is relative to the file, or to the config's folder when it starts with `{config_root}`, its steps may hold `*`, matching any run of characters within one step, and the first file on disk, in path order, is the schema |

A modeline's schema URI also says what the file is, by its fragment:

| Modeline | The file |
|---|---|
| `$schema=<path or URL>` | a full document, validated against the schema |
| `$schema=<path or URL>#` | a fragment whose root is the schema's root |
| `$schema=<path or URL>#<JSON Pointer>` | a fragment whose root is the schema node a JSON Pointer (RFC 6901) names, such as `#/properties/Resources` |

- A fragment is validated against its root node with no key required at any depth. Every other check holds.
- A file whose schema comes from `schema_search_paths` is a full document.
- A pointer naming no node of the schema is a problem on the modeline's line.

- A modeline points at the schema shipped with the grammar version the stack has initialized: an overlay names `.schema/sample.schema.json`, and a common layer names it through the overlay directory that initialized the stack, such as `one/.schema/sample.schema.json`.
- A URL in a path is fetched. The search does not override a modeline that resolves.
- When neither source resolves, the file's modeline line gets a problem, or its first line when it has no modeline.

### Schema URLs

A schema URL, in a modeline or in `schema_search_paths`, may carry the reader's parameters in its query, each named with the `yaml_dsl_` prefix:

| Parameter | Holds |
|---|---|
| `yaml_dsl_max_age_days` | the whole number of days a downloaded copy is used before it is checked with the server; `0`, or no parameter, fetches the URL each time a stack reading it loads |
| `yaml_dsl_machine_credentials` | `true` reads the URL again with the machine's credentials when the read without them is refused; `false`, or no parameter, reads it without credentials only |

- The reader's parameters are removed before the URL is fetched. Every other parameter reaches the server as written, in its order.
- Any other `yaml_dsl_` parameter, a max age that is not a whole number, or a machine credentials value other than `true` or `false` is a problem on the modeline's line.
- A downloaded copy is kept under the URL without the reader's parameters, with the server's `ETag` and `Last-Modified` and the time the server last sent or confirmed it.
- A copy older than its max age is checked with `If-None-Match`, else `If-Modified-Since`, else a full download. A `304` keeps the copy, and either answer restarts its age.
- Files naming one URL with different max ages share its copy, each judging the copy's age by its own max age.
- When the server does not answer, the kept copy is used.

A schema URL is read without credentials. One carrying `yaml_dsl_machine_credentials=true` is read in this order, stopping at the first answer that is not refused:

| Link | Read with, in order |
|---|---|
| a GitHub link under `https://github.com/<owner>/<repo>/releases/`, or `www.github.com` | no credentials; then `GH_TOKEN`, `GITHUB_TOKEN`, the `gh` command's login (`gh auth token`), then the `.netrc` entry for `api.github.com` |
| any other | no credentials; then the `.netrc` entry for the link's host, sent as Basic auth |

- An answer is refused when it is 401 or 403, or 404 from a GitHub link, which GitHub gives for a private repository.
- A source the machine lacks is skipped: a variable that is not set, `gh` not installed or not logged in, a `.netrc` without an entry for the host.
- `.netrc` is the file `NETRC` names, else the one in the user's home folder.
- With a GitHub token, the link is read through GitHub's API. The release comes from the link's path: `download/<tag>/<asset>` is the release by its tag, and `latest/download/<asset>` is the latest release. The asset is then read by its name, following GitHub's redirect without the token.
- A link no credential reads is reported by what is known:
  - a release or asset the repository lacks is a missing release or asset;
  - a repository no credential can see is not found, naming the repository;
  - a refused credential is reported with where it came from and what renews it.
- Once all credentials are refused, they are read again for the next read, so a renewed login takes effect without a restart.

## JSON Schema drafts

A document schema may declare:

- draft-07
- draft 2019-09
- draft 2020-12

A schema's draft:

- is the one its root `$schema` names;
- is draft-07 when it names none or an older draft;
- is not read when it names any other draft: the modeline line gets an info problem naming the URI and the drafts a reader reads.

## Placeholders

A placeholder splices a value into a scalar or a key, whole or as part of a longer string.

| Field | Meaning |
|---|---|
| `pattern` | the placeholder, whose `body` group is what it holds |
| `escape_prefix` | optional; text written right before a placeholder that keeps it as literal text, such as `$` in `$${HOME}`; the compiled value drops the prefix |
| `unscanned_paths` | the paths whose text belongs to another language, such as shell commands or a Dockerfile, where `${…}` is not the DSL's and is not read; each entry has `path` and `skip_keys` |

- A body is read by the reference rules whose `positions` list a placeholder position: `${local.name}` holds `local.name`, read by a `local` rule, and `${env}` holds `env`, read by a rule in a logical scope such as `GLOBAL`.
- A body may hold placeholders, as far as the `pattern` nests them, each read as a placeholder of its own: `${local.buckets.${region}}` holds `local.buckets.${region}`, read by a `local` rule, and `${region}` inside it.
- Every placeholder is validated. A body no rule reads, text after the name where the rule's `text_after_name_allowed` is `false`, and a position the rule does not list are problems.

## Functions

`function` holds what is generic to calls:

| Field | Meaning |
|---|---|
| `call_result_reference_positions` | the positions from which a name a call declares may be referenced |
| `calls_not_allowed_at` | the paths where a call is a problem, each with `path` and `skip_keys` |
| `marker_function` | the one call shape modeled, below |
| `definitions` | the sources of the function table, merged in order |

`marker_function` is a call written in a key, `<name> <call_marker><function>`:

- `splat_operator` follows the function when the value below is a list of arguments rather than the one argument. `null` states a DSL with no operator, and the function table decides: a function with one argument takes the value whole; one with more takes a list value as its arguments, and any other value as its first.
- `calls_without_name_allowed` says whether a key that is the marker alone is a call with no name, the only key of its map.

| `definitions` source | The table |
|---|---|
| `terraform` | the answer of `terraform metadata functions -json`, run by the reader and ignored when Terraform cannot be invoked |
| `schema` | the table the file's schema publishes at `#/x-yaml-dsl-functions`, none when it publishes none; a malformed one is a problem on the modeline line |

- While a source has not answered the vocabulary is open, and an unknown function is a problem only once every source has.
- A common layer's calls are read with each overlay's function table: a call's problem in an overlay's table is the common layer's, naming the overlays it holds in, as `unknown function only_one (two)`. A common layer whose stack has no overlay files reads its own.
- A key that calls a macro is a macro call, never a function call.
- Every call is checked: its function against the vocabulary, its argument count against the function's arity, and each literal argument against its type. A reference or a call standing as an argument is not typed, a reference written as its tag's key included.
- A call annotation is not part of its key: a document's schema validates the key's field, `message` for `message fn.format*`, and a value a call computes is not checked against the field's type. A macro is expanded before its document is validated. A DSL's schema so describes the document's fields alone, never where a call may stand.

## The function table

A schema publishes a DSL's functions as a mapping of function name to its description, its arguments and what it returns. Its JSON Schema is `schemas/x-yaml-dsl-functions-v<version>.schema.json`.

| Function field | Meaning |
|---|---|
| `description` | what the function does, in markdown |
| `arguments` | its arguments in call order |
| `return` | what it returns: `type`, one of the argument types, and `description`, in markdown; left out, the function returns `void` |
| `macro` | present only on a macro, under Macros; an entry with `macro` takes no `return` |

| Argument field | Meaning |
|---|---|
| `name` | the argument's name |
| `type` | `text`, `number`, `whole_number`, `boolean`, `list`, `map`, `function`, `macro` or `any`; `function` and `macro` hold the name of a table entry of that kind |
| `required` | whether it is required; optional arguments follow the required ones |
| `default` | on an optional argument, the value it takes when a call leaves it out; left out, none |
| `repeated` | whether it takes every remaining value; a repeated argument is last |
| `description` | what the argument holds, in markdown |

Every field but `default`, `return` and `macro` is required.

```json
"x-yaml-dsl-functions": {
  "format": {
    "description": "Fills the layout's placeholders with the operands, in order.",
    "arguments": [
      { "name": "layout", "type": "text", "required": true, "repeated": false, "description": "The text holding the placeholders." },
      { "name": "operands", "type": "any", "required": false, "repeated": true, "description": "The values put in the placeholders, in order." }
    ],
    "return": { "type": "text", "description": "The layout with each placeholder filled." }
  }
}
```

Terraform's signatures are read into the same table:

| Terraform | Table |
|---|---|
| `string` | `text` |
| `bool` | `boolean` |
| a list, set or tuple | `list` |
| a map or object | `map` |
| `dynamic` | `any` |
| a variadic parameter | a repeated optional argument |
| a function's or parameter's `description` | its `description` |
| a function's `return_type` | its `return.type`, with no `description` |

## Macros

A function computes a value when the document is used. A macro is expanded by the DSL's preprocessor before then, as a C macro is, and its expansion is what is read: validated, declared, referenced. A macro is an entry of the function table with a `macro` object, documented and called as a function is.

A reader follows the preprocessing, so it reads the document as it will be: an expansion's entries are validated where they land, the names they spell are declared there, and a reference to such a name resolves to the source that spells it. A `merge` is the one kind whose arguments may be left to when the document is used: a content that references anything but a local is merged then, so a reader merges the locals' values and reads the rest as unknown.

| `macro` field | Holds |
|---|---|
| `kind` | what the preprocessor does with a call, below |
| `name_suffix_pattern` | the regular expression of the text after the macro's name that keeps sibling calls distinct; left out, nothing follows the name |
| `unmarked_key_paths` | the keys that call the macro without the call marker; left out, none |
| `merge_behavior` | on a `merge` macro, and required there: `shallow` replaces a key's value whole, `deep` merges nested maps key by key, `join` takes each key once, so a key that both the holding map and a content set is a problem |
| a role, below | the name of the entry's argument that plays the role |

| `kind` | Roles | Expands to |
|---|---|---|
| `function_call` | `callee`, a `text`, `function` or `macro`; `callee_arguments`, a `map`, left out when the callee takes none | a call of the macro or function the callee names, the callee arguments its arguments by name |
| `merge` | `content`, a `text` location or a `map`, repeated for contents merged in order | the map holding the call, combined with the contents by `merge_behavior` |
| `loop` | `loop_variables`, a `text` or a `list` of one or two loop variables; `collection`, a `list` or a `map`; `body`, a `map` or a `list` | the body once per item of the collection, its loop variables bound: one variable to the item, two to the item's key or index and its value |
| `transform` | none | the map holding the call, rewritten by the macro |

- A role names one of the entry's arguments, so arguments sit in whatever order the DSL writes them. An argument no role names is one a reader does not read.
- A call binds its arguments by name when its value is a map holding argument names, by position when it is a list, and otherwise as the first argument, and its arguments are checked as they bind.
- An entry whose roles are missing, name no argument, or name an argument of the wrong type is a problem on the modeline line. Only a `merge` macro's content may be repeated.

### Calls

- A macro is called as a function is, through `marker_function` or its tag through `tag_keys`, and also by a key at one of its `unmarked_key_paths`.
- A function yields a value, so its call stands where a value does. A macro yields entries, so its call may stand as a whole key among siblings. YAML refuses two equal keys in one map, so the text `name_suffix_pattern` matches keeps sibling calls distinct; a reader reads nothing from it.
- A loop's result lands where its call stands:

  | Call | A map body | A list body |
  |---|---|---|
  | with a name, `<name> <call_marker><function>` | a map under the name | a list under the name |
  | with no name | its entries among the holding map's | the holding map's value, the call its only key |

- A map body yields its entries once per item. A list body holds one element and yields it once per item.
- A map collection's items are its values in key order, and two loop variables bind its keys to the first.
- A call that leaves out an optional argument binds the argument's `default`, so a loop with no loop variables argument binds the default loop variables.
- A `function_call`'s value binds its arguments by name, or is the callee's name alone, or is a list holding one call per item.
- A call whose name only a function or only a macro holds calls that one.
- A function and a macro of one name, from two of a DSL's `definitions`, are both kept, and a call tells them apart by syntax: written in its macro kind's usage pattern, it calls the macro, and otherwise the function. A `merge` macro's pattern is a whole key.
- A callee the table does not hold is one the DSL's users define, such as a CloudFormation macro registered by `AWS::CloudFormation::Macro`, and is not a problem.

### Evaluation

A macro is expanded when every input resolves to a constant: a literal, or a local holding one. An input with no value in the files, such as a CloudFormation parameter, leaves the expansion unknown:

| `kind` | Input unknown |
|---|---|
| `function_call` | the call is checked against the macro's own entry alone |
| `merge` | a location no `location_mappings` entry covers, or whose file does not exist, declares every name, as a `declares_every_name` key does, and is an info problem; a content left to when the document is used supplies every field the holding map requires |
| `loop` | the body is checked once, each loop variable bound to its own name; a key in it that names a loop variable declares every name it can spell, so `Topic${TopicName}` declares `TopicA`, and a reference to such a name is not a problem |
| `transform` | always: the map holding the call is checked as written |

### What an expansion gives

- The holding map validates with the expansion in place.
- A loop's body is read at the position its entries land: their schema is the holding map's.
- A problem in one item's expansion is reported at the body's source, naming the item; one every item shares is reported once.
- A loop variable is a name the loop declares within its body, read by the reference rules of the loop scope (see Loop variables); an inner loop's variable hides an outer one of the same name.
- The names an expansion declares are declared where they land.
- A merge whose content is at a location is an include, its location mapped by `layers.overlay_includes`.
- A merge's contents are merged in order, and the holding map's own keys over them. A content that is a local, or a path into one, is read as that value in the fold, whichever layer declares it, and a name it declares resolves to the key the local writes.
- A problem in what a content supplies is reported at the call's key.
- A key in a local a merge reads is read at the position it lands: its schema is that of the maps it lands in, at the same path under each.
- One fold merging a local into maps whose schemas differ is an error at each of those merge calls, the targets listed by schema: `` `local.defaults` is merged into maps with different schemas: `queue.{orders,payments}` and `topic.alerts` ``. The common layer is checked in each overlay's fold, its targets read as `dev:queue.orders`. The local itself is no problem.
- A loop's collection may be a list a local holds, or a path into a local that holds one.

## What the workspace defines

```yaml
version: "1"
dsls:
  - id: sample
    file_includes: ["**/sample.yml"]
    file_excludes: []
    schema_search_paths:
      - .schema/sample.schema.json
      - dev/.schema/sample.schema.json
    layers:
      overlay_folders: [dev, staging]
      common_layer_discovery: ancestor
      common_layer_files:
        sample.yml: ["{concern}.sample.yml", sample.yml]
      duplicate_check:
        depth: 2
        key_depths: {}
        skip_keys: [schema_version]
    placeholder:
      pattern: "\\$\\{(?<body>[^}\\n]*)\\}"
      unscanned_paths: []
    function:
      definitions: [terraform]
      call_result_reference_positions: [whole_scalar]
      calls_not_allowed_at:
        - { path: "$.*", skip_keys: [] }
        - { path: "$.cloud", skip_keys: [] }
        - { path: "$.cloud..*", skip_keys: [] }
      marker_function:
        call_marker: fn.
        splat_operator: "*"
        calls_without_name_allowed: true
    locals:
      scope_name: local
      path_separator: "."
    loops:
      scope_name: LOOP
    validator:
      child_values_from_parent_key:
        - pattern: '^(?<field>[a-z][a-z0-9_]*) (?<value>[a-z0-9-]+)$'
    formatter:
      indent_spaces: 2
      list_items_indented: true
      flow_list_max_items: 2
      flow_map_max_items: 0
      flow_max_line_length: 80
      inline_comment_spaces: 2
      key_sort_orders:
        - path: "$.locals"
          skip_keys: []
          order: alphabetical
          first_keys: []
          last_keys: []
        - path: "$.*.*"
          skip_keys: [cloud, locals, outputs]
          order: significance
          first_keys: [name]
          last_keys: []
        - path: "$..*"
          skip_keys: [locals]
          order: significance
          first_keys: []
          last_keys: []
        - path: "$"
          skip_keys: []
          order: significance
          first_keys:
            - schema_version
            - env
            - cloud
            - locals
          last_keys: [data_source, outputs]
      list_sort_orders:
        - path: "$..*"
          skip_keys: []
          order: alphabetical
      blank_lines_between_entries:
        - path: "$"
          skip_keys: []
          blank_lines: 1
        - path: "$.*"
          skip_keys: [cloud, data_source, locals, outputs]
          blank_lines: 1
        - path: "$..*"
          skip_keys: []
          blank_lines: 0
    scopes:
      GLOBAL:
        regions: ["$"]
        later_items_of_declaring_list: false
        named_by_parent_key: false
        builtin_names: [env, region]
      ENTRY:
        regions: ["$"]
        later_items_of_declaring_list: false
        named_by_parent_key: true
        builtin_names: []
      local:
        regions: ["$"]
        later_items_of_declaring_list: false
        named_by_parent_key: false
        builtin_names: []
      LOOP:
        regions: ["$"]
        later_items_of_declaring_list: false
        named_by_parent_key: false
        builtin_names: []
    declarations:
      - path: "$.locals.*"
        skip_keys: []
        exclude_candidates: []
        name_source: key
        key_token: first_word
        name_spelling: as_written
        meta_argument_name: null
        declares_every_name: false
        scope_name: local
      - path: "$.*.*"
        skip_keys: [cloud, locals, outputs]
        exclude_candidates: []
        name_source: key
        key_token: last_word
        name_spelling: dashes_as_underscores
        meta_argument_name: null
        declares_every_name: false
        scope_name: ENTRY
    references:
      - pattern: "^ref (?<type>[a-z0-9_]+)\\.(?<name>[a-z0-9_${}]+)"
        positions: [whole_scalar]
        text_after_name_allowed: true
        scope_name: ENTRY
        scope_group: type
        name_group: name
      - pattern: "^local\\.(?<name>[a-z0-9_]+)"
        positions: [whole_scalar, whole_placeholder, placeholder_in_text]
        text_after_name_allowed: true
        scope_name: local
        scope_group: null
        name_group: name
      - pattern: "^~(?<name>[a-z_][a-z0-9_]*)"
        positions: [whole_scalar, whole_placeholder, placeholder_in_text]
        text_after_name_allowed: true
        scope_name: LOOP
        scope_group: null
        name_group: name
      - pattern: "^(?<name>[a-z_]+)$"
        positions: [whole_placeholder, placeholder_in_text]
        text_after_name_allowed: false
        scope_name: GLOBAL
        scope_group: null
        name_group: name
  - id: pipeline
    file_includes: ["**/pipelines.yml"]
    file_excludes: []
    schema_search_paths:
      - .terraform/modules/pipelines/schema.json
    placeholder:
      pattern: "\\$\\{(?<body>[^}\\n]*)\\}"
      unscanned_paths:
        - { path: "$.build", skip_keys: [] }
        - { path: "$.build..*", skip_keys: [] }
    formatter:
      indent_spaces: 2
      list_items_indented: true
      flow_list_max_items: 2
      flow_map_max_items: 0
      flow_max_line_length: 80
      inline_comment_spaces: 2
      key_sort_orders: []
      list_sort_orders: []
      blank_lines_between_entries: []
    scopes:
      BRANCH:
        regions: ["$.branches.deployment_target_env"]
        later_items_of_declaring_list: false
        named_by_parent_key: false
        builtin_names: [branch]
      ENV_VAR_GLOBAL:
        regions: ["$.container.environment"]
        later_items_of_declaring_list: false
        named_by_parent_key: false
        builtin_names: [ENVIRONMENT, VERSION]
      GLOBAL:
        regions: ["$"]
        later_items_of_declaring_list: false
        named_by_parent_key: false
        builtin_names: [env, region]
    declarations: []
    references:
      - pattern: "^(?<name>[a-z_]+)$"
        positions: [whole_placeholder, placeholder_in_text]
        text_after_name_allowed: false
        scope_name: GLOBAL
        scope_group: null
        name_group: name
      - pattern: "^(?<name>[a-z_]+)$"
        positions: [whole_placeholder, placeholder_in_text]
        text_after_name_allowed: false
        scope_name: BRANCH
        scope_group: null
        name_group: name
      - pattern: "^(?<name>[A-Z_]+)$"
        positions: [whole_placeholder, placeholder_in_text]
        text_after_name_allowed: false
        scope_name: ENV_VAR_GLOBAL
        scope_group: null
        name_group: name
  - id: test
    file_includes: ["**/*.yml"]
    file_excludes: ["**/protocols/**"]
    schema_search_paths: []
    placeholder:
      pattern: "\\$\\{(?<body>[^}\\n]*)\\}"
      unscanned_paths: []
    function:
      definitions: [schema]
      call_result_reference_positions: [whole_placeholder, placeholder_in_text]
      calls_not_allowed_at: []
      marker_function:
        call_marker: fn.
        splat_operator: "*"
        calls_without_name_allowed: false
    locals:
      scope_name: local
    formatter:
      indent_spaces: 2
      list_items_indented: true
      flow_list_max_items: 2
      flow_map_max_items: 0
      flow_max_line_length: 80
      inline_comment_spaces: 2
      key_sort_orders: []
      list_sort_orders: []
      blank_lines_between_entries: []
    scopes:
      GLOBAL:
        regions: ["$"]
        later_items_of_declaring_list: false
        named_by_parent_key: false
        builtin_names: [env, region]
      local:
        regions: ["$"]
        later_items_of_declaring_list: false
        named_by_parent_key: false
        builtin_names: []
      protocol:
        regions: ["$"]
        later_items_of_declaring_list: false
        named_by_parent_key: false
        builtin_names: []
      setup:
        regions: ["$.cases", "$.teardown"]
        later_items_of_declaring_list: false
        named_by_parent_key: false
        builtin_names: []
      step:
        regions: []
        later_items_of_declaring_list: true
        named_by_parent_key: false
        builtin_names: []
    declarations:
      - path: "$.cases[*].steps[*].*"
        skip_keys: []
        exclude_candidates: []
        name_source: meta_argument
        key_token: null
        name_spelling: as_written
        meta_argument_name: id
        declares_every_name: false
        scope_name: step
      - path: "$.locals.*"
        skip_keys: []
        exclude_candidates: []
        name_source: key
        key_token: first_word
        name_spelling: as_written
        meta_argument_name: null
        declares_every_name: false
        scope_name: local
      - path: "$.protocols.*.messages"
        skip_keys: []
        exclude_candidates: []
        name_source: key
        key_token: all_words
        name_spelling: as_written
        meta_argument_name: null
        declares_every_name: true
        scope_name: protocol
      - path: "$.setup[*].id"
        skip_keys: []
        exclude_candidates: []
        name_source: value
        key_token: null
        name_spelling: as_written
        meta_argument_name: null
        declares_every_name: false
        scope_name: setup
    references:
      - pattern: "^(?<name>[a-z_]+)$"
        positions: [whole_placeholder, placeholder_in_text]
        text_after_name_allowed: false
        scope_name: GLOBAL
        scope_group: null
        name_group: name
      - pattern: "^local\\.(?<name>[a-z0-9_]+)$"
        positions: [whole_placeholder, placeholder_in_text]
        text_after_name_allowed: false
        scope_name: local
        scope_group: null
        name_group: name
      - pattern: "(?<![A-Za-z0-9_./])protocol\\.(?<name>[a-z0-9_]+)"
        positions: [anywhere_in_scalar]
        text_after_name_allowed: true
        scope_name: protocol
        scope_group: null
        name_group: name
      - pattern: "^setup\\.(?<name>[a-z0-9_]+)"
        positions: [whole_placeholder, placeholder_in_text]
        text_after_name_allowed: true
        scope_name: setup
        scope_group: null
        name_group: name
      - pattern: "^step\\.(?<name>[a-z0-9_]+)"
        positions: [whole_placeholder, placeholder_in_text]
        text_after_name_allowed: true
        scope_name: step
        scope_group: null
        name_group: name
```

- `version` is the config format, read by the reader of that version.
- A block left out is a feature the DSL does not have: `layers` is a DSL's composition of a common layer and its overlays.
- A file matching two DSLs is reported and claimed by neither. Other YAML is untouched.

- A file belongs to the config of the deepest folder holding it, and only that config's DSLs are matched against it. A file none of them claims stays YAML; a shallower config is not consulted.
- A path or glob in a config may start with `{config_root}`, the config's folder. Every other one keeps its meaning: a glob matches a file's path, and a file path is relative to the file being read.
- `{config_root}` anchors a glob at the config's folder, `{config_root}/*.yaml` a YAML file directly in it and `{config_root}/**/*.yaml` one at any depth, and a file path at the same folder, `{config_root}/schemas/sample.schema.json` one file from every depth.
- A config in a folder of its own makes that folder one language: `file_includes` of `{config_root}/**/*.yaml` and `{config_root}/**/*.yml` claims every YAML file under it, whatever its name.
- A `yaml-dsl.yml` is a config, never a DSL file, whatever globs match it.
