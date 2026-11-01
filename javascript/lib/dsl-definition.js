'use strict';

/**
 * A DSL definition: the shape of a DSL block of `yaml-dsl.yml`, its fields named
 * as the standard names them. The fields read here mean what the specification
 * says they mean; fields that only shape editing are not part of it, and are
 * left out when a block holding them is read.
 */

const isObject = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);

/** Reads one struct of the definition from `source`, by each field's reader. */
function readFields(source, fields) {
  if (!isObject(source)) throw new Error('invalid type: expected a map');
  const read = {};
  for (const [field, readField] of Object.entries(fields)) read[field] = readField(source[field], field);
  return read;
}

const required = (readValue) => (value, field) => {
  if (value === undefined) throw new Error(`missing field \`${field}\``);
  return readValue(value, field);
};
const optional = (readValue) => (value, field) =>
  (value === undefined || value === null ? null : readValue(value, field));
const withDefault = (readValue, fallback) => (value, field) =>
  (value === undefined ? fallback() : readValue(value, field));

function text(value, field) {
  if (typeof value !== 'string') throw new Error(`invalid type for \`${field}\`: expected a string`);
  return value;
}

function flag(value, field) {
  if (typeof value !== 'boolean') throw new Error(`invalid type for \`${field}\`: expected a boolean`);
  return value;
}

const listOf = (readItem) => (value, field) => {
  if (!Array.isArray(value)) throw new Error(`invalid type for \`${field}\`: expected a sequence`);
  return value.map((item) => readItem(item, field));
};

const mapOf = (readItem) => (value, field) => {
  if (!isObject(value)) throw new Error(`invalid type for \`${field}\`: expected a map`);
  const keys = Object.keys(value).sort((first, second) =>
    Buffer.compare(Buffer.from(first, 'utf8'), Buffer.from(second, 'utf8')));
  return Object.fromEntries(keys.map((key) => [key, readItem(value[key], field)]));
};

const oneOf = (variants) => (value, field) => {
  if (!variants.includes(value)) {
    throw new Error(`unknown variant \`${value}\` for \`${field}\`, expected one of ${variants.map((variant) => `\`${variant}\``).join(', ')}`);
  }
  return value;
};

const struct = (fields) => (value) => readFields(value, fields);
const emptyList = () => [];
const emptyMap = () => ({});

const PATH_ENTRY = struct({
  path: required(text),
  skip_keys: withDefault(listOf(text), emptyList),
});

const DSL_FIELDS = {
  id: required(text),
  file_includes: required(listOf(text)),
  file_excludes: withDefault(listOf(text), emptyList),
  schema_search_paths: withDefault(listOf(text), emptyList),
  layers: optional(struct({
    overlay_folders: withDefault(listOf(text), emptyList),
    common_layer_discovery: required(oneOf(['parent', 'ancestor', 'overlay_includes'])),
    common_layer_files: withDefault(mapOf(listOf(text)), emptyMap),
  })),
  placeholder: optional(struct({
    pattern: required(text),
    escape_prefix: withDefault(optional(text), () => null),
    unscanned_paths: withDefault(listOf(PATH_ENTRY), emptyList),
  })),
  function: optional(struct({
    definitions: withDefault(listOf(text), emptyList),
    calls_not_allowed_at: withDefault(listOf(PATH_ENTRY), emptyList),
    marker_function: optional(struct({
      call_marker: required(text),
      splat_operator: optional(text),
      calls_without_name_allowed: required(flag),
    })),
  })),
  locals: optional(struct({
    scope_name: required(text),
    path_separator: optional(text),
  })),
  loops: optional(struct({ scope_name: required(text) })),
  scopes: withDefault(mapOf(struct({
    regions: withDefault(listOf(text), emptyList),
    later_items_of_declaring_list: withDefault(flag, () => false),
    named_by_parent_key: withDefault(flag, () => false),
    builtin_names: withDefault(listOf(text), emptyList),
  })), emptyMap),
  declarations: withDefault(listOf(struct({
    path: required(text),
    skip_keys: withDefault(listOf(text), emptyList),
    exclude_candidates: withDefault(listOf(text), emptyList),
    name_source: required(oneOf(['key', 'value', 'meta_argument'])),
    key_token: optional(oneOf(['all_words', 'first_word', 'last_word'])),
    name_spelling: required(oneOf(['as_written', 'dashes_as_underscores'])),
    meta_argument_name: optional(text),
    declares_every_name: withDefault(flag, () => false),
    scope_name: required(text),
  })), emptyList),
  references: withDefault(listOf(struct({
    pattern: required(text),
    positions: required(listOf(oneOf([
      'whole_scalar',
      'whole_placeholder',
      'placeholder_in_text',
      'anywhere_in_scalar',
    ]))),
    text_after_name_allowed: required(flag),
    scope_name: required(text),
    scope_group: optional(text),
    name_group: required(text),
  })), emptyList),
  tag_keys: withDefault(listOf(struct({ tag: required(text), key: required(text) })), emptyList),
  validator: optional(struct({
    child_values_from_parent_key: withDefault(listOf(struct({ pattern: required(text) })), emptyList),
  })),
};

/** Reads a DSL block, such as one of a parsed `yaml-dsl.yml`, or throws why it is none. */
function dslDefinitionFromJson(block) {
  return readFields(block, DSL_FIELDS);
}

module.exports = { dslDefinitionFromJson };
