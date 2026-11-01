'use strict';

/*
 * Reads a `yaml-dsl.yml`: checks its `version`, validates it against that
 * version's schema, checks what the schema cannot state, and returns each DSL's
 * definition, or every problem at the field it is about.
 */

const { Diagnostic } = require('./diagnostic-report');
const { dslDefinitionFromJson } = require('./dsl-definition');
const { PathPattern } = require('./json-path');
const { compilePattern, groupNamesOf } = require('./pattern-dialect');
const { Position } = require('./source-position');
const { Value } = require('./resolved-value');
const { SchemaCache, closestName, nodeJsonText } = require('./schema-validation');
const { ValidationRules } = require('./validation-rules');
const { parseYaml } = require('./yaml-reader');
const { YAML_DSL_VERSIONS, yamlDslSchemaFile } = require('./schema-catalog');

/** The text in a `common_layer_files` name that stands for a concern. */
const CONCERN_TOKEN = '{concern}';
const PATH_ENTRY_FIELDS = [['placeholder', 'unscanned_paths'], ['function', 'calls_not_allowed_at'], ['key_sort_orders']];

/**
 * Each DSL of the `yaml-dsl.yml` at `file`, whose text is `text`, validated, as
 * `{ dsls }`, each `{ definition, block }`; or every problem found, each at the
 * field it is about, as `{ problems }`.
 */
function readDslDefinitions(file, text) {
  const parsed = parseYaml(file, text, []);
  if (parsed.problem) return { problems: [parsed.problem] };
  const root = parsed.node;
  const version = versionSchema(root, file);
  if (version.problems) return version;
  let schema;
  try {
    schema = new SchemaCache().load(version.file, version.text);
  } catch (error) {
    return { problems: [Diagnostic.unplaced(error.message)] };
  }
  const schemaProblems = schema.validate(Value.fromNode(root), new ValidationRules());
  if (schemaProblems.length > 0) return { problems: schemaProblems };
  const blocks = (root.get('dsls') && root.get('dsls').items()) || [];
  const problems = [];
  const dsls = [];
  blocks.forEach((block, index) => {
    const repeated = repeatedId(block, blocks.slice(0, index));
    if (repeated) problems.push(repeated);
    const blockProblems = new BlockChecks(block).problems();
    if (blockProblems.length > 0) {
      problems.push(...blockProblems);
      return;
    }
    try {
      dsls.push({ definition: dslDefinitionFromJson(block.toJson()), block });
    } catch (error) {
      problems.push(Diagnostic.at(block.position, error.message));
    }
  });
  return problems.length === 0 ? { dsls } : { problems };
}

/** The file name and text of the schema the file's `version` names, or its problem. */
function versionSchema(root, file) {
  const listed = YAML_DSL_VERSIONS.map((version) => `\`"${version}"\``).join(', ');
  const versionNode = root.get('version');
  if (versionNode === null) {
    return {
      problems: [
        Diagnostic.at(Position.fileStart(file), '`version` is required')
          .withLabel(`\`version\` is one of ${listed}`),
      ],
    };
  }
  const version = versionNode.asStr() || '';
  const found = yamlDslSchemaFile(version);
  if (found) return found;
  const shown = versionNode.asStr() === null
    ? `\`${nodeJsonText(versionNode)}\``
    : `\`${versionNode.asStr()}\``;
  const close = closestName(version, YAML_DSL_VERSIONS);
  return {
    problems: [
      Diagnostic.at(versionNode.position, `${shown} is not a version this reader reads`)
        .withLabel(`\`version\` is one of ${listed}`)
        .withHelp(close === null ? null : `did you mean \`"${close}"\`?`),
    ],
  };
}

/** A second DSL of the file with the same `id`. */
function repeatedId(block, earlier) {
  const idNode = block.get('id');
  const id = idNode && idNode.asStr();
  if (id === null || id === undefined) return null;
  const repeated = earlier.some((other) => other.get('id') && other.get('id').asStr() === id);
  return repeated
    ? Diagnostic.at(idNode.position, `DSL \`${id}\` is declared more than once`)
      .withLabel('an earlier DSL of this file has this `id`')
    : null;
}

/** Whether a field is written `true`. */
function isTrue(node) {
  return node !== null && node.kind === 'bool' && node.value === true;
}

/** The text of the field `field` of `node`, `null` where it holds none. */
function textAt(node, field) {
  const found = node.get(field);
  return found === null ? null : found.asStr();
}

/** The node at `steps` of map keys below `node`, or `null`. */
function nodeAt(node, steps) {
  let current = node;
  for (const step of steps) {
    current = current.get(step);
    if (current === null) return null;
  }
  return current;
}

/** The items of the list at `steps`, none where there is no list. */
function listAt(node, steps) {
  const found = nodeAt(node, steps);
  return (found && found.items()) || [];
}

/** The values of the map at `steps`, none where there is no map. */
function mapValuesAt(node, steps) {
  const found = nodeAt(node, steps);
  return ((found && found.entries()) || []).map((entry) => entry.value);
}

/**
 * The checks of one DSL block that its schema cannot state. The block has
 * already passed the schema, so every field has its shape.
 */
class BlockChecks {
  constructor(block) {
    this.block = block;
    this.found = [];
  }

  problems() {
    this.patterns();
    this.paths();
    this.repeatedPathEntries();
    this.keySortOrders();
    this.commonLayerFiles();
    this.scopeNames();
    this.localsAndLoops();
    this.tagKeys();
    return this.found;
  }

  report(node, message, label) {
    this.found.push(Diagnostic.at(node.position, message).withLabel(label));
  }

  /** Each regular expression compiles, and holds the named groups its rule reads. */
  patterns() {
    const placeholder = nodeAt(this.block, ['placeholder', 'pattern']);
    if (placeholder) this.pattern(placeholder, [['body', "a placeholder's body"]]);
    for (const rule of listAt(this.block, ['references'])) {
      const pattern = rule.get('pattern');
      if (!pattern) continue;
      const groups = [];
      const name = textAt(rule, 'name_group');
      if (name !== null) groups.push([name, 'the name, by `name_group`']);
      const scope = textAt(rule, 'scope_group');
      if (scope !== null) groups.push([scope, 'the scope, by `scope_group`']);
      this.pattern(pattern, groups);
    }
    for (const rule of listAt(this.block, ['validator', 'child_values_from_parent_key'])) {
      const pattern = rule.get('pattern');
      if (pattern) this.pattern(pattern, [['field', 'the child field'], ['value', 'its value']]);
    }
  }

  pattern(node, groups) {
    const text = node.asStr();
    if (text === null) return;
    const compiled = compilePattern(text);
    if (compiled === null) {
      this.report(node, 'the pattern is not a regular expression', 'a regular expression the reader compiles');
      return;
    }
    const names = groupNamesOf(compiled);
    for (const [group, holds] of groups) {
      if (!names.includes(group)) {
        this.report(node, `the pattern has no \`(?<${group}>...)\` group`, `the group holds ${holds}`);
      }
    }
  }

  /** Each JSONPath is one of the subset the standard reads. */
  paths() {
    const nodes = [];
    for (const rule of listAt(this.block, ['declarations'])) if (rule.get('path')) nodes.push(rule.get('path'));
    for (const scope of mapValuesAt(this.block, ['scopes'])) nodes.push(...listAt(scope, ['regions']));
    for (const field of PATH_ENTRY_FIELDS) {
      for (const entry of listAt(this.block, field)) if (entry.get('path')) nodes.push(entry.get('path'));
    }
    for (const include of listAt(this.block, ['layers', 'overlay_includes'])) {
      nodes.push(...listAt(include, ['key_paths']));
      if (include.get('location_path')) nodes.push(include.get('location_path'));
    }
    for (const node of nodes) {
      const text = node.asStr();
      if (text === null) continue;
      try {
        PathPattern.parse(text, []);
      } catch (error) {
        this.report(node, error.message, "a path of the standard's JSONPath subset");
      }
    }
  }

  /** No list of path entries holds one path twice. */
  repeatedPathEntries() {
    for (const field of PATH_ENTRY_FIELDS) {
      const entries = listAt(this.block, field);
      entries.forEach((entry, index) => {
        const path = entry.get('path');
        if (!path) return;
        const repeated = entries.slice(0, index).some((earlier) =>
          (earlier.get('path') ? earlier.get('path').asStr() : null) === path.asStr());
        if (repeated) {
          this.report(
            path,
            `path \`${path.asStr() || ''}\` is listed more than once`,
            `an earlier entry of \`${field.join('.')}\` has this path`,
          );
        }
      });
    }
  }

  /** An order's first and last keys are distinct, and alphabetical order names none. */
  keySortOrders() {
    for (const entry of listAt(this.block, ['key_sort_orders'])) {
      const alphabetical = entry.get('order') !== null && entry.get('order').asStr() === 'alphabetical';
      for (const field of ['first_keys', 'last_keys']) {
        const keys = listAt(entry, [field]);
        if (alphabetical && keys.length > 0) {
          this.report(entry.get(field) || entry, `\`${field}\` is not read with alphabetical order`, '`[]` under alphabetical order');
        }
        keys.forEach((key, index) => {
          if (keys.slice(0, index).some((earlier) => earlier.asStr() === key.asStr())) {
            this.report(key, `\`${field}\` lists a key more than once`, 'an earlier item is this key');
          }
        });
      }
      const last = listAt(entry, ['last_keys']);
      for (const key of listAt(entry, ['first_keys'])) {
        if (last.some((other) => other.asStr() === key.asStr())) {
          this.report(key, 'a key is in both `first_keys` and `last_keys`', '`last_keys` lists it too');
        }
      }
    }
  }

  /**
   * The names a layer file's common layer may take: each a file name, each once,
   * `{concern}` at most once in a name, one without it, and none with it where
   * only an ancestor reaches a concern below its overlays.
   */
  commonLayerFiles() {
    const discovery = nodeAt(this.block, ['layers', 'common_layer_discovery']);
    const parentDiscovery = discovery !== null && discovery.asStr() === 'parent';
    for (const files of mapValuesAt(this.block, ['layers', 'common_layer_files'])) {
      const names = files.items() || [];
      names.forEach((name, index) => {
        const text = name.asStr() || '';
        if (text === '' || text === '.' || text === '..' || text.includes('/') || text.includes('\\')) {
          this.report(name, `\`${text}\` is not a file name`, 'a file name, with no folder');
        }
        if (names.slice(0, index).some((earlier) => earlier.asStr() === name.asStr())) {
          this.report(name, `\`${text}\` is listed more than once`, 'an earlier item is this name');
        }
        if (text.split(CONCERN_TOKEN).length - 1 > 1) {
          this.report(name, `\`${text}\` holds \`${CONCERN_TOKEN}\` more than once`, `a name holds \`${CONCERN_TOKEN}\` at most once`);
        }
        if (parentDiscovery && text.includes(CONCERN_TOKEN)) {
          this.report(
            name,
            `\`${text}\` names a concern's common layer`,
            'only `common_layer_discovery: ancestor` reaches a concern below its overlays',
          );
        }
      });
      const allConcern = names.length > 0 && names.every((name) => (name.asStr() || '').includes(CONCERN_TOKEN));
      if (allConcern) {
        this.report(
          files,
          `every name holds \`${CONCERN_TOKEN}\``,
          'a name without it is the common layer of a concern above its overlays',
        );
      }
    }
  }

  /**
   * Every scope a rule names is declared, and a reference names its scope by a
   * group exactly when the scope is named by a parent key.
   */
  scopeNames() {
    const scopes = this.scopes();
    const declared = scopes.map(([name]) => name);
    const named = [];
    for (const rule of [...listAt(this.block, ['declarations']), ...listAt(this.block, ['references'])]) {
      if (rule.get('scope_name')) named.push(rule.get('scope_name'));
    }
    for (const holder of ['locals', 'loops']) {
      const scope = nodeAt(this.block, [holder, 'scope_name']);
      if (scope) named.push(scope);
    }
    for (const node of named) {
      const name = node.asStr() || '';
      if (!declared.includes(name)) {
        const close = closestName(name, declared);
        this.found.push(Diagnostic.at(node.position, `scope \`${name}\` is not declared`)
          .withLabel('`scopes` declares no scope of this name')
          .withHelp(close === null ? null : `did you mean \`${close}\`?`));
      }
    }
    for (const rule of listAt(this.block, ['references'])) {
      const name = textAt(rule, 'scope_name');
      if (name === null) continue;
      const scope = scopes.find(([declaredName]) => declaredName === name);
      if (!scope) continue;
      const [, namedByParentKey] = scope;
      const group = rule.get('scope_group');
      const hasGroup = group !== null && group.asStr() !== null;
      if (namedByParentKey && !hasGroup) {
        this.report(group || rule, '`scope_group` is required', `scope \`${name}\` is named by a parent key`);
      }
      if (!namedByParentKey && hasGroup) {
        this.report(group || rule, '`scope_group` must be null', `scope \`${name}\` is not named by a parent key`);
      }
    }
  }

  /** Locals have a rule declaring their names from keys and one reading them; loop variables have a rule reading them. */
  localsAndLoops() {
    const locals = nodeAt(this.block, ['locals', 'scope_name']);
    if (locals) {
      const name = locals.asStr() || '';
      const declaredByKey = listAt(this.block, ['declarations']).some((rule) =>
        rule.get('scope_name') !== null && rule.get('scope_name').asStr() === name
        && rule.get('name_source') !== null && rule.get('name_source').asStr() === 'key'
        && !isTrue(rule.get('declares_every_name')));
      if (!declaredByKey) {
        this.report(locals, `no declaration rule reads \`${name}\` names from keys`, 'a local is declared by its key');
      }
      this.readByReferenceRule(locals, name, 'a local');
    }
    const loops = nodeAt(this.block, ['loops', 'scope_name']);
    if (loops) this.readByReferenceRule(loops, loops.asStr() || '', 'a loop variable');
  }

  readByReferenceRule(scope, name, what) {
    const read = listAt(this.block, ['references']).some((rule) =>
      rule.get('scope_name') !== null && rule.get('scope_name').asStr() === name);
    if (!read) this.report(scope, `no reference rule reads \`${name}\``, `${what} is read by a reference rule of its scope`);
  }

  /** No tag, and no key, is listed twice. */
  tagKeys() {
    const entries = listAt(this.block, ['tag_keys']);
    entries.forEach((entry, index) => {
      for (const field of ['tag', 'key']) {
        const node = entry.get(field);
        if (!node) continue;
        const repeated = entries.slice(0, index).some((earlier) =>
          (earlier.get(field) ? earlier.get(field).asStr() : null) === node.asStr());
        if (repeated) {
          this.report(
            node,
            `${field} \`${node.asStr() || ''}\` is listed more than once`,
            `an earlier entry of \`tag_keys\` has this ${field}`,
          );
        }
      }
    });
  }

  /** Each declared scope's name, and whether it is named by a parent key. */
  scopes() {
    const scopes = this.block.get('scopes');
    return ((scopes && scopes.entries()) || []).map((entry) =>
      [entry.key, isTrue(entry.value.get('named_by_parent_key'))]);
  }
}

module.exports = { readDslDefinitions };
