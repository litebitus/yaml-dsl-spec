'use strict';

const AjvDraft07 = require('ajv').default;
const AjvDraft201909 = require('ajv/dist/2019').default;
const AjvDraft202012 = require('ajv/dist/2020').default;

const { Diagnostic } = require('./diagnostic-report');
const { FunctionTable } = require('./function-table');
const { numberJson, integerOfText } = require('./yaml-node');

const VALIDATOR_OPTIONS = {
  allErrors: true,
  strict: false,
  unicodeRegExp: true,
  validateFormats: false,
  verbose: true,
  logger: false,
};
const DRAFTS = [
  {
    name: 'draft-07',
    ValidatorClass: AjvDraft07,
    schemaUris: ['http://json-schema.org/draft-07/schema#', 'http://json-schema.org/draft-07/schema'],
  },
  {
    name: 'draft 2019-09',
    ValidatorClass: AjvDraft201909,
    schemaUris: ['https://json-schema.org/draft/2019-09/schema', 'https://json-schema.org/draft/2019-09/schema#'],
  },
  {
    name: 'draft 2020-12',
    ValidatorClass: AjvDraft202012,
    schemaUris: ['https://json-schema.org/draft/2020-12/schema', 'https://json-schema.org/draft/2020-12/schema#'],
  },
];
const [ASSUMED_DRAFT] = DRAFTS;
const OLDER_DRAFT_URIS = ['draft-03', 'draft-04', 'draft-06'].flatMap((name) => [
  `http://json-schema.org/${name}/schema#`,
  `http://json-schema.org/${name}/schema`,
]);
const ALTERNATIVE_KEYWORDS = new Set(['anyOf', 'oneOf']);
const READS_BELOW_KEYWORDS = new Set([
  'anyOf',
  'oneOf',
  'not',
  'contains',
  'unevaluatedItems',
  'unevaluatedProperties',
  'uniqueItems',
]);
const REQUIRED_KEYWORDS = new Set(['required', 'dependentRequired', 'dependencies']);
const LIMIT_PLURALS = {
  maxLength: ['is longer than', 'character', 'characters'],
  minLength: ['is shorter than', 'character', 'characters'],
  maxItems: ['has more than', 'item', 'items'],
  minItems: ['has less than', 'item', 'items'],
  maxProperties: ['has more than', 'property', 'properties'],
  minProperties: ['has less than', 'property', 'properties'],
};
const BOUNDS = {
  maximum: 'is greater than the maximum of',
  minimum: 'is less than the minimum of',
  exclusiveMaximum: 'is greater than or equal to the maximum of',
  exclusiveMinimum: 'is less than or equal to the minimum of',
};
const KEYWORD_NAMES = { 'false schema': 'falseSchema', items: 'additionalItems' };
const FALSE_SCHEMA_STEP = /\/false schema$/;
const SHOWN_ENUM_OPTIONS = 3;

const hasOwn = (object, field) => Object.prototype.hasOwnProperty.call(object, field);
const isObject = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);

/** Orders texts by their UTF-8 bytes, as the crate orders strings. */
function compareText(first, second) {
  return Buffer.compare(Buffer.from(first, 'utf8'), Buffer.from(second, 'utf8'));
}

/** The draft the standard reads a document schema in, by its root `$schema`. */
function draftOfSchema(schema) {
  if (!isObject(schema) || schema.$schema === undefined || OLDER_DRAFT_URIS.includes(schema.$schema)) {
    return ASSUMED_DRAFT;
  }
  const draft = DRAFTS.find((known) => known.schemaUris.includes(schema.$schema));
  if (draft) return draft;
  const readDrafts = DRAFTS.map((known) => known.name).join(', ');
  throw new Error(`declares ${jsonText(schema.$schema)}, a draft this reader does not read; it reads ${readDrafts}`);
}

/** A document's schema, as its caller hands it over: its function table, and the validation against it. */
class LoadedSchema {
  constructor(table, validator, schema, name, pointersBySchemaObject) {
    this.table = table;
    this.validator = validator;
    this.schema = schema;
    this.name = name;
    this.pointersBySchemaObject = pointersBySchemaObject;
  }

  /**
   * Where the schema holds the keyword an error breaks, as a JSON pointer: by the
   * schema object holding it, since Ajv restarts a path inside a reference it
   * does not inline.
   */
  keywordPointer(error) {
    const written = pointerOfSchemaPath(error.schemaPath);
    if (FALSE_SCHEMA_STEP.test(written)) return written.replace(FALSE_SCHEMA_STEP, '');
    const holder = this.pointersBySchemaObject.get(error.parentSchema);
    if (holder === undefined) return written;
    return `${holder}${written.slice(written.lastIndexOf('/'))}`;
  }

  /** The schema `text` holds, named `name` in reports, or throws why it holds none. */
  static parse(name, text) {
    const failed = (error) => new Error(`schema ${name}: ${error.message}`);
    let schema;
    let table;
    let validator;
    try {
      schema = JSON.parse(text);
      table = FunctionTable.fromSchema(schema);
      const draft = draftOfSchema(schema);
      const compiled = isObject(schema) && schema.$schema !== undefined && OLDER_DRAFT_URIS.includes(schema.$schema)
        ? { ...schema, $schema: ASSUMED_DRAFT.schemaUris[0] }
        : schema;
      validator = new draft.ValidatorClass(VALIDATOR_OPTIONS).compile(compiled);
      return new LoadedSchema(table, validator, schema, name, schemaObjectPointers(compiled));
    } catch (error) {
      throw failed(error);
    }
  }

  /**
   * Validates `document`, a resolved value. A value not known when the document
   * compiles, such as a call or a reference to a declaration, is not checked.
   */
  validate(document, childValuePatterns = []) {
    const instance = new InstanceRecord(childValuePatterns);
    const json = instance.json(document, '', document.position);
    this.validator(json);
    const errors = reportedErrors(this.validator.errors || [], this.schema)
      .filter((error) => !instance.readsUnknown(error.instancePath, error.keyword));
    const diagnostics = errors.flatMap((error) => this.errorDiagnostics(error, instance));
    diagnostics.sort(compareDiagnostics);
    return diagnostics.filter((diagnostic, index) =>
      index === 0 || !sameDiagnostic(diagnostic, diagnostics[index - 1]));
  }

  /**
   * A schema error as the report a reader fixes it from: placed at the value or
   * the field that is wrong, labeled with what is wrong there, naming the rule it
   * breaks, and the allowed name closest to a misspelt one.
   */
  errorDiagnostics(error, instance) {
    const pointer = error.instancePath;
    const keyword = keywordName(error);
    const schemaPointer = this.keywordPointer(error);
    const rule = `${keyword}, at ${this.name}#${schemaPointer}`;
    if (error.propertyName !== undefined) {
      return [
        Diagnostic.at(instance.valuePosition(pointer), `${displayPointer(pointer)}: ${errorText(error, instance)}`)
          .withLabel(`breaks \`${keyword}\``)
          .withRule(rule),
      ];
    }
    if (error.keyword === 'items' && error.schema === false) {
      const extra = Array.isArray(error.data) ? error.data.slice(error.params.limit) : [];
      return extra.map((item, index) => {
        const itemPointer = `${pointer}/${error.params.limit + index}`;
        return Diagnostic.at(
          instance.valuePosition(itemPointer),
          `${displayPointer(itemPointer)}: False schema does not allow ${instance.jsonText(item, itemPointer)}`,
        )
          .withLabel('breaks `falseSchema`')
          .withRule(`falseSchema, at ${this.name}#${schemaPointer}`);
      });
    }
    const step = lastStep(pointer);
    const holder = step === null ? 'the document' : `\`${step}\``;
    const suggestion = (name, candidates) => {
      const closest = closestName(name, candidates);
      return closest === null ? null : `did you mean \`${closest}\`?`;
    };
    if (error.keyword === 'additionalProperties' || error.keyword === 'unevaluatedProperties') {
      const name = error.keyword === 'additionalProperties'
        ? error.params.additionalProperty
        : error.params.unevaluatedProperty;
      const allowed = allowedFields(this.schema, schemaPointer);
      const field = `${pointer}/${escapePointer(name)}`;
      return [
        Diagnostic.at(instance.position(field), `field \`${name}\` is not allowed`)
          .withLabel(`${holder} has no field \`${name}\``)
          .withRule(rule)
          .withHelp(suggestion(name, allowed)),
      ];
    }
    if (error.keyword === 'enum') {
      const options = Array.isArray(error.schema) ? error.schema : [];
      const allowed = options.filter((option) => typeof option === 'string');
      const value = error.data;
      return [
        Diagnostic.at(instance.valuePosition(pointer), `\`${shownValue(value, pointer, instance)}\` is not an allowed value`)
          .withLabel(`not one of the ${options.length} values \`enum\` allows`)
          .withRule(rule)
          .withHelp(typeof value === 'string' ? suggestion(value, allowed) : null),
      ];
    }
    if (error.keyword === 'type') {
      const expected = typesOf(error.schema);
      return [
        Diagnostic.at(instance.valuePosition(pointer), `${holder} is not ${withArticle(expected.join(' or '))}`)
          .withLabel(`is ${withArticle(instance.jsonType(pointer, error.data))}`)
          .withRule(rule),
      ];
    }
    if (REQUIRED_KEYWORDS.has(error.keyword)) {
      const name = error.params.missingProperty;
      return [
        Diagnostic.at(instance.position(pointer), `field \`${name}\` is required`)
          .withLabel(`${holder} has no field \`${name}\``)
          .withRule(rule),
      ];
    }
    return [
      Diagnostic.at(instance.valuePosition(pointer), `${displayPointer(pointer)}: ${errorText(error, instance)}`)
        .withLabel(`breaks \`${keyword}\``)
        .withRule(rule),
    ];
  }
}

/** The JSON pointer of every object of a schema, by the object. */
function schemaObjectPointers(schema) {
  const pointers = new Map();
  const pending = [[schema, '']];
  while (pending.length > 0) {
    const [node, pointer] = pending.pop();
    if (node === null || typeof node !== 'object' || pointers.has(node)) continue;
    pointers.set(node, pointer);
    for (const [key, child] of Object.entries(node)) pending.push([child, `${pointer}/${escapePointer(key)}`]);
  }
  return pointers;
}

/** Schemas parsed once and shared by every unit that reads them, by name and text. */
class SchemaCache {
  constructor() {
    this.loaded = new Map();
  }

  /** The schema named `name` whose text is `text`, or throws why it holds none. */
  load(name, text) {
    const key = JSON.stringify([name, text]);
    if (!this.loaded.has(key)) {
      try {
        this.loaded.set(key, { schema: LoadedSchema.parse(name, text) });
      } catch (error) {
        this.loaded.set(key, { error });
      }
    }
    const loaded = this.loaded.get(key);
    if (loaded.error) throw loaded.error;
    return loaded.schema;
  }
}

/** The keyword a report names: the crate's name for what Ajv reports. */
function keywordName(error) {
  if (error.propertyName !== undefined) return 'propertyNames';
  if (error.keyword === 'dependentRequired' || error.keyword === 'dependencies') return 'required';
  return hasOwn(KEYWORD_NAMES, error.keyword) ? KEYWORD_NAMES[error.keyword] : error.keyword;
}

/**
 * The errors a reader reports: those of a check made on the document, never the
 * errors inside a branch of `anyOf`, `oneOf` or `contains` that only explain the
 * check's failure, and never an `if`. A property name's failure is reported by the
 * check the name fails, never again as the `propertyNames` it fails through.
 */
function reportedErrors(errors, schema) {
  const reachable = new Map();
  const reaches = (start, target) => {
    if (!reachable.has(start)) reachable.set(start, reachableSchemaObjects(start, schema));
    return reachable.get(start).has(target);
  };
  const explainers = errors.filter((error) =>
    ALTERNATIVE_KEYWORDS.has(error.keyword) || error.keyword === 'contains');
  const explains = (outer, inner) => {
    if (outer === inner) return false;
    const below = inner.instancePath === outer.instancePath
      || inner.instancePath.startsWith(`${outer.instancePath}/`);
    if (outer.keyword === 'contains' && inner.instancePath === outer.instancePath) return false;
    return below && reaches(outer.schema, inner.parentSchema);
  };
  return errors.filter((error) => error.keyword !== 'if'
    && !(error.keyword === 'propertyNames' && error.propertyName === undefined)
    && !explainers.some((outer) => explains(outer, error)));
}

/** Every schema object reachable from `start`, through local references. */
function reachableSchemaObjects(start, root) {
  const reached = new Set();
  const pending = [start];
  while (pending.length > 0) {
    const node = pending.pop();
    if (node === null || typeof node !== 'object' || reached.has(node)) continue;
    reached.add(node);
    if (!Array.isArray(node) && typeof node.$ref === 'string' && node.$ref.startsWith('#')) {
      pending.push(schemaNodeAt(root, pointerOfSchemaPath(node.$ref)));
    }
    pending.push(...Object.values(node));
  }
  return reached;
}

/** A schema path or a local reference as a JSON pointer, without its `#`. */
function pointerOfSchemaPath(path) {
  const pointer = path.startsWith('#') ? path.slice(1) : path;
  try {
    return decodeURIComponent(pointer);
  } catch {
    return pointer;
  }
}

function schemaNodeAt(root, pointer) {
  if (pointer === '') return root;
  let node = root;
  for (const step of pointer.slice(1).split('/')) {
    const key = step.replaceAll('~1', '/').replaceAll('~0', '~');
    if (node === null || typeof node !== 'object' || !hasOwn(node, key)) return undefined;
    node = node[key];
  }
  return node;
}

/**
 * The fields a schema node allows, by the `properties` of the node holding the
 * keyword at `keywordPointer`, of the schemas it references and of those it
 * combines.
 */
function allowedFields(schema, keywordPointer) {
  const holderPointer = keywordPointer.includes('/')
    ? keywordPointer.slice(0, keywordPointer.lastIndexOf('/'))
    : '';
  const fields = [];
  const holder = schemaNodeAt(schema, holderPointer);
  if (holder !== undefined) collectFields(schema, holder, fields, new Set());
  return fields;
}

function collectFields(schema, node, fields, visited) {
  if (!isObject(node)) return;
  if (isObject(node.properties)) {
    for (const name of Object.keys(node.properties)) if (!fields.includes(name)) fields.push(name);
  }
  if (typeof node.$ref === 'string' && node.$ref.startsWith('#')) {
    const pointer = node.$ref.slice(1);
    const target = visited.has(pointer) ? undefined : schemaNodeAt(schema, pointer);
    visited.add(pointer);
    if (target !== undefined) collectFields(schema, target, fields, visited);
  }
  for (const combinator of ['allOf', 'anyOf', 'oneOf']) {
    for (const branch of Array.isArray(node[combinator]) ? node[combinator] : []) {
      collectFields(schema, branch, fields, visited);
    }
  }
}

/**
 * The candidate closest in spelling to `name`, where one is close enough to be a
 * likely fix and no other is as close; else `null`.
 */
function closestName(name, candidates) {
  const ranked = candidates
    .filter((candidate) => candidate !== name)
    .map((candidate) => [editDistance(name, candidate), candidate])
    .sort(([firstDistance, first], [secondDistance, second]) =>
      firstDistance - secondDistance || compareText(first, second));
  const limit = Math.max(Math.floor(Array.from(name).length / 3), 1);
  if (ranked.length === 0) return null;
  const [[distance, best], next] = ranked;
  return distance <= limit && (next === undefined || next[0] > distance) ? best : null;
}

/** The fewest single-character insertions, deletions and substitutions turning one text into the other. */
function editDistance(first, second) {
  const secondCharacters = Array.from(second);
  let previous = Array.from({ length: secondCharacters.length + 1 }, (_, index) => index);
  Array.from(first).forEach((firstCharacter, row) => {
    const current = [row + 1];
    secondCharacters.forEach((secondCharacter, column) => {
      const substitution = previous[column] + (firstCharacter === secondCharacter ? 0 : 1);
      current.push(Math.min(substitution, previous[column + 1] + 1, current[column] + 1));
    });
    previous = current;
  });
  return previous[secondCharacters.length];
}

/**
 * What a JSON pointer names, as a report shows it: its last key, or for a list
 * item, the list's key and the item's index, such as `dsls[0]`; `null` for the
 * document's root.
 */
function lastStep(pointer) {
  const slash = pointer.lastIndexOf('/');
  if (slash === -1) return null;
  const parent = pointer.slice(0, slash);
  const step = pointer.slice(slash + 1);
  if (/^\d+$/.test(step)) {
    const list = lastStep(parent);
    if (list !== null) return `${list}[${step}]`;
  }
  return step.replaceAll('~1', '/').replaceAll('~0', '~');
}

function escapePointer(key) {
  return key.replaceAll('~', '~0').replaceAll('/', '~1');
}

function displayPointer(pointer) {
  if (pointer === '') return 'the document';
  const steps = pointer.slice(1).split('/').map((step) => step.replaceAll('~1', '/').replaceAll('~0', '~'));
  return `\`${steps.join('.')}\``;
}

function withArticle(noun) {
  return `${/^[aeiou]/.test(noun) ? 'an' : 'a'} ${noun}`;
}

/** A schema's `type` as the crate lists several: in its fixed order of JSON types. */
function typesOf(typeKeyword) {
  if (!Array.isArray(typeKeyword)) return [String(typeKeyword)];
  const order = ['null', 'boolean', 'integer', 'number', 'string', 'array', 'object'];
  return order.filter((type) => typeKeyword.includes(type));
}

/** A string as Rust's `Debug` writes it. */
function debugText(text) {
  let written = '"';
  for (const character of text) {
    const code = character.codePointAt(0);
    if (character === '"') written += '\\"';
    else if (character === '\\') written += '\\\\';
    else if (character === '\n') written += '\\n';
    else if (character === '\r') written += '\\r';
    else if (character === '\t') written += '\\t';
    else if (character === '\0') written += '\\0';
    else if (code < 0x20 || code === 0x7f) written += `\\u{${code.toString(16)}}`;
    else written += character;
  }
  return `${written}"`;
}

/** A floating-point number as `serde_json` writes one. */
function floatText(number) {
  if (Object.is(number, -0)) return '-0.0';
  const exponent = Number(number.toExponential().split('e')[1]);
  if (exponent < -5 || exponent >= 16) return number.toExponential().replace('e+', 'e');
  const decimal = String(number);
  return decimal.includes('.') ? decimal : `${decimal}.0`;
}

/** A JSON value as `serde_json` writes it, compact. */
function jsonText(value, numberTextAt = () => null, pointer = '') {
  if (value === null) return 'null';
  if (typeof value === 'boolean') return String(value);
  if (typeof value === 'string') return JSON.stringify(value);
  if (typeof value === 'number') {
    const written = numberTextAt(pointer);
    if (written !== null) {
      const integer = integerOfText(written);
      if (integer !== null) return integer.toString();
    }
    return Number.isInteger(value) && written === null ? String(value) : floatText(value);
  }
  if (Array.isArray(value)) {
    return `[${value.map((item, index) => jsonText(item, numberTextAt, `${pointer}/${index}`)).join(',')}]`;
  }
  return `{${Object.entries(value)
    .map(([key, item]) => `${JSON.stringify(key)}:${jsonText(item, numberTextAt, `${pointer}/${escapePointer(key)}`)}`)
    .join(',')}}`;
}

/** A node as `serde_json` writes the JSON it reads to, each number as it is written. */
function nodeJsonText(node) {
  const numberTexts = new Map();
  const collect = (current, pointer) => {
    if (current.kind === 'number') numberTexts.set(pointer, current.value);
    if (current.kind === 'list') current.value.forEach((item, index) => collect(item, `${pointer}/${index}`));
    if (current.kind === 'map') {
      for (const entry of current.value) collect(entry.value, `${pointer}/${escapePointer(entry.key)}`);
    }
  };
  collect(node, '');
  return jsonText(node.toJson(), (at) => (numberTexts.has(at) ? numberTexts.get(at) : null));
}

function shownValue(value, pointer, instance) {
  return typeof value === 'string' ? debugText(value) : instance.jsonText(value, pointer);
}

function plural(count, words) {
  return count === 1 ? words[1] : words[2];
}

/** An error's text as the crate's validator words it. */
function errorText(error, instance) {
  const shown = instance.jsonText(error.data, error.instancePath);
  const { keyword, params } = error;
  if (hasOwn(LIMIT_PLURALS, keyword)) {
    const words = LIMIT_PLURALS[keyword];
    return `${shown} ${words[0]} ${params.limit} ${plural(params.limit, words)}`;
  }
  if (hasOwn(BOUNDS, keyword)) return `${shown} ${BOUNDS[keyword]} ${jsonText(params.limit)}`;
  switch (keyword) {
    case 'enum': return `${shown} is not one of ${enumOptionsText(Array.isArray(error.schema) ? error.schema : [])}`;
    case 'type': {
      const types = typesOf(error.schema);
      return types.length === 1
        ? `${shown} is not of type ${JSON.stringify(types[0])}`
        : `${shown} is not of types ${types.map((type) => JSON.stringify(type)).join(', ')}`;
    }
    case 'const': return `${jsonText(error.schema)} was expected`;
    case 'pattern': return `${shown} does not match "${params.pattern}"`;
    case 'multipleOf': return `${shown} is not a multiple of ${params.multipleOf}`;
    case 'uniqueItems': return `${shown} has non-unique elements`;
    case 'not': return `${jsonText(error.schema)} is not allowed for ${shown}`;
    case 'anyOf': return `${shown} is not valid under any of the schemas listed in the 'anyOf' keyword`;
    case 'oneOf':
      return params.passingSchemas
        ? `${shown} is valid under more than one of the schemas listed in the 'oneOf' keyword`
        : `${shown} is not valid under any of the schemas listed in the 'oneOf' keyword`;
    case 'contains': return `None of ${shown} are valid under the given schema`;
    case 'false schema': return `False schema does not allow ${shown}`;
    case 'propertyNames': return `${JSON.stringify(params.propertyName)} is not valid under the property names schema`;
    case 'items':
    case 'additionalItems': {
      const extra = Array.isArray(error.data) ? error.data.slice(params.limit) : [];
      const listed = extra.map((item, index) =>
        instance.jsonText(item, `${error.instancePath}/${params.limit + index}`));
      return `Additional items are not allowed (${listed.join(', ')} ${extra.length === 1 ? 'was' : 'were'} unexpected)`;
    }
    case 'unevaluatedItems': {
      const extra = Array.isArray(error.data) ? error.data.slice(params.len) : [];
      const listed = extra.map((item, index) =>
        `'${instance.jsonText(item, `${error.instancePath}/${params.len + index}`)}'`);
      return `Unevaluated items are not allowed (${listed.join(', ')} ${extra.length === 1 ? 'was' : 'were'} unexpected)`;
    }
    default: return error.message;
  }
}

/** An enum's options as the crate's validator lists them: a few, then how many more. */
function enumOptionsText(options) {
  const written = options.map((option) => jsonText(option));
  if (written.length <= SHOWN_ENUM_OPTIONS) {
    if (written.length <= 1) return written.join('');
    return `${written.slice(0, -1).join(', ')} or ${written.at(-1)}`;
  }
  const shown = written.slice(0, SHOWN_ENUM_OPTIONS - 1);
  return `${shown.join(', ')} or ${written.length - shown.length} other candidates`;
}

function compareDiagnostics(first, second) {
  const firstPlace = first.position ? [first.position.line, first.position.column] : [0, 0];
  const secondPlace = second.position ? [second.position.line, second.position.column] : [0, 0];
  return firstPlace[0] - secondPlace[0] || firstPlace[1] - secondPlace[1]
    || compareText(first.message, second.message);
}

function sameDiagnostic(first, second) {
  return JSON.stringify(first) === JSON.stringify(second);
}

/** The document as JSON, and where each of its values was written. */
class InstanceRecord {
  constructor(childValuePatterns) {
    this.childValuePatterns = childValuePatterns;
    this.positions = new Map();
    this.valuePositions = new Map();
    this.numberTexts = new Map();
    this.unknown = [];
  }

  json(value, pointer, keyPosition) {
    this.positions.set(pointer, keyPosition);
    this.valuePositions.set(pointer, value.position);
    const local = value.localValue();
    switch (local.kind) {
      case 'null': return null;
      case 'bool': return local.value;
      case 'number':
        this.numberTexts.set(pointer, local.value);
        return numberJson(local.value);
      case 'string': return local.value;
      case 'list':
        return local.value.map((item, index) => this.json(item, `${pointer}/${index}`, item.position));
      case 'map': {
        const object = {};
        for (const entry of local.value) {
          const child = `${pointer}/${escapePointer(entry.key)}`;
          const json = this.json(entry.value, child, entry.keyPosition);
          if (isObject(json)) this.setChildValue(entry.key, json);
          object[entry.key] = json;
        }
        return object;
      }
      default:
        this.unknown.push(pointer);
        return null;
    }
  }

  /** A key such as `name mock-orders` sets the field `name` of the entry it keys, unless it writes the field. */
  setChildValue(key, fields) {
    const found = childValue(key, this.childValuePatterns);
    if (found !== null && !hasOwn(fields, found.field)) fields[found.field] = found.value;
  }

  /**
   * Whether an error at `pointer` comes from a value not known when the document
   * compiles: the value itself, or a check that reads a value below it through a
   * subschema, or compares the values below it.
   */
  readsUnknown(pointer, keyword) {
    if (this.isUnknown(pointer)) return true;
    const below = `${pointer}/`;
    return READS_BELOW_KEYWORDS.has(keyword) && this.unknown.some((unknown) => unknown.startsWith(below));
  }

  isUnknown(pointer) {
    return this.unknown.some((unknown) => pointer === unknown || pointer.startsWith(`${unknown}/`));
  }

  valuePosition(pointer) {
    return this.valuePositions.has(pointer) ? this.valuePositions.get(pointer) : this.position(pointer);
  }

  position(pointer) {
    let current = pointer;
    for (;;) {
      if (this.positions.has(current)) return this.positions.get(current);
      current = current.includes('/') ? current.slice(0, current.lastIndexOf('/')) : '';
    }
  }

  /** The JSON type of the value at `pointer`, an integer as written told from a number. */
  jsonType(pointer, value) {
    if (value === null) return 'null';
    if (Array.isArray(value)) return 'array';
    if (typeof value === 'boolean') return 'boolean';
    if (typeof value === 'string') return 'string';
    if (typeof value === 'number') {
      const written = this.numberTexts.get(pointer);
      if (written !== undefined) return integerOfText(written) !== null ? 'integer' : 'number';
      return Number.isInteger(value) ? 'integer' : 'number';
    }
    return 'object';
  }

  jsonText(value, pointer) {
    return jsonText(value, (at) => (this.numberTexts.has(at) ? this.numberTexts.get(at) : null), pointer);
  }
}

/**
 * The field a key such as `name mock-orders` sets on the entry it keys, and the
 * value it sets, `{ field, value }`, by the first of `patterns` with `field` and
 * `value` groups that matches; else `null`.
 */
function childValue(key, patterns) {
  for (const pattern of patterns) {
    const match = pattern.exec(key);
    const groups = match && match.groups;
    if (groups && groups.field !== undefined && groups.value !== undefined) {
      return { field: groups.field, value: groups.value };
    }
  }
  return null;
}

module.exports = {
  LoadedSchema,
  SchemaCache,
  childValue,
  closestName,
  nodeJsonText,
  draftOfSchema,
  compareText,
  debugText,
};
