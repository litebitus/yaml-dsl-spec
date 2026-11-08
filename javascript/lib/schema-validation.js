'use strict';

const AjvDraft07 = require('ajv').default;
const AjvDraft201909 = require('ajv/dist/2019').default;
const AjvDraft202012 = require('ajv/dist/2020').default;

const { Diagnostic } = require('./diagnostic-report');
const { FunctionTable } = require('./function-table');
const { numberJson, integerOfText } = require('./yaml-node');
const { descriptionText, markedSchema } = require('./requiredness-markers');
const { compilePattern } = require('./pattern-dialect');
const {
  fieldName,
  codeSpan,
  numberText,
  definitionDescription,
  dependencyOf,
  dependencyMessage,
  duplicateItems,
  containsMessage,
  schemaNodeAt,
} = require('./problem-wording');
const {
  ValidationRules,
  childValue,
  mergedOver,
  copiedPointers,
  escapePointer,
} = require('./validation-rules');

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
  'uniqueItems',
]);
const REQUIRED_KEYWORDS = new Set(['required', 'dependentRequired', 'dependencies']);
const BOUND_COMPARISONS = {
  minimum: '>=',
  maximum: '<=',
  exclusiveMinimum: '>',
  exclusiveMaximum: '<',
};
const LENGTH_LIMITS = {
  minLength: ['fewer', 'characters'],
  maxLength: ['more', 'characters'],
  minItems: ['fewer', 'items'],
  maxItems: ['more', 'items'],
  minProperties: ['fewer', 'properties'],
  maxProperties: ['more', 'properties'],
};
const KEYWORD_NAMES = { 'false schema': 'falseSchema' };
const FALSE_SCHEMA_STEP = /\/false schema$/;

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
  constructor(table, validator, schema, marked, name, pointersBySchemaObject) {
    this.table = table;
    this.validator = validator;
    this.schema = schema;
    this.marked = marked;
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
    try {
      const schema = JSON.parse(text);
      const table = FunctionTable.fromSchema(schema);
      const draft = draftOfSchema(schema);
      const declaresOlderDraft = isObject(schema)
        && schema.$schema !== undefined
        && OLDER_DRAFT_URIS.includes(schema.$schema);
      const declared = declaresOlderDraft
        ? { ...schema, $schema: ASSUMED_DRAFT.schemaUris[0] }
        : schema;
      const marked = markedSchema(declared, draft.name);
      const validator = new draft.ValidatorClass(VALIDATOR_OPTIONS).compile(marked);
      const pointers = schemaObjectPointers(marked);
      return new LoadedSchema(table, validator, schema, marked, name, pointers);
    } catch (error) {
      throw failed(error);
    }
  }

  /**
   * Validates `document`, a resolved value, read by `rules`. A value not known
   * when the document compiles, such as a call or a reference to a declaration,
   * is not checked.
   */
  validate(document, rules) {
    const instance = new InstanceRecord(rules);
    const json = this.withQuotedValuesRead(instance.json(document, '', document.position), rules);
    this.validator(json);
    const errors = reportedErrors(this.validator.errors || [], this.marked)
      .filter((error) => !instance.readsUnknown(error.instancePath, error.keyword));
    const reported = { dependencies: new Set(), itemLists: new Set(), propertyNames: new Set() };
    const diagnostics = errors
      .flatMap((error) => this.errorProblems(error, json, instance, reported))
      .filter((problem) => !instance.isCopied(problem.subject))
      .map((problem) => problem.diagnostic);
    diagnostics.sort(compareDiagnostics);
    return diagnostics.filter((diagnostic, index) =>
      index === 0 || !sameDiagnostic(diagnostic, diagnostics[index - 1]));
  }

  /**
   * `json` with each quoted value that spells a type the schema expects of it, and
   * `rules` allow, read as that type, until no more are read.
   */
  withQuotedValuesRead(json, rules) {
    if (rules.quotedValueTypes.length === 0) return json;
    let read = json;
    for (;;) {
      this.validator(read);
      const readings = new Map();
      for (const error of this.validator.errors || []) {
        if (error.keyword !== 'type' || typeof error.data !== 'string') continue;
        const reading = rules.quotedValue(error.data, [].concat(error.schema));
        if (reading !== null && !readings.has(error.instancePath)) {
          readings.set(error.instancePath, reading.value);
        }
      }
      if (readings.size === 0) return read;
      for (const [pointer, value] of readings) read = withValueAt(read, pointer, value);
    }
  }

  /**
   * A schema error as the problems a reader fixes it from, `{ subject, diagnostic }`:
   * placed at the value or the field that is wrong, worded by the standard,
   * labeled with what is wrong there, naming the rule it breaks, and the allowed
   * name closest to a misspelt one.
   */
  errorProblems(error, json, instance, reported) {
    const pointer = error.instancePath;
    const keyword = keywordName(error);
    const schemaPointer = this.keywordPointer(error);
    const ruleAt = (name, at) => `${name}, at ${this.name}#${at}`;
    const rule = ruleAt(keyword, schemaPointer);
    const field = fieldName(json, pointer);
    const missingField = REQUIRED_KEYWORDS.has(error.keyword)
      ? error.params.missingProperty
      : null;
    const dependency = error.propertyName === undefined
      ? dependencyOf(this.marked, schemaPointer, valueAt(json, pointer), missingField)
      : null;
    if (dependency !== null) {
      const subject = `${pointer}/${escapePointer(dependency.key)}`;
      if (reported.dependencies.has(subject)) return [];
      reported.dependencies.add(subject);
      const { keywordPointer } = dependency;
      const dependencyKeyword = keywordPointer.slice(keywordPointer.lastIndexOf('/') + 1);
      const at = `${dependency.keywordPointer}/${escapePointer(dependency.key)}`;
      return [{
        subject,
        diagnostic: Diagnostic.at(
          instance.position(subject),
          dependencyMessage(this.marked, dependency.keywordPointer, dependency.key),
        )
          .withLabel(`breaks ${codeSpan(dependencyKeyword)}`)
          .withRule(ruleAt(dependencyKeyword, at)),
      }];
    }
    const suggestion = (name, candidates) => {
      const closest = closestName(name, candidates);
      return closest === null ? null : `did you mean ${codeSpan(closest)}?`;
    };
    const atValue = (message, schemaNote = null) => [{
      subject: pointer,
      diagnostic: Diagnostic.at(instance.valuePosition(pointer), message)
        .withLabel(`breaks ${codeSpan(keyword)}`)
        .withRule(rule)
        .withSchema(schemaNote),
    }];
    const limitText = (limit) => codeSpan(String(limit));
    if (error.propertyName !== undefined) {
      const subject = `${pointer}/${escapePointer(error.propertyName)}`;
      if (reported.propertyNames.has(subject)) return [];
      reported.propertyNames.add(subject);
      return [{
        subject,
        diagnostic: Diagnostic.at(
          instance.position(subject),
          `${codeSpan(error.propertyName)} is not a valid key of ${field}`,
        )
          .withLabel('breaks `propertyNames`')
          .withRule(rule),
      }];
    }
    const itemsBeyondTuple = (error.keyword === 'items' && error.schema === false)
      || error.keyword === 'additionalItems';
    if (itemsBeyondTuple) {
      if (reported.itemLists.has(pointer)) return [];
      reported.itemLists.add(pointer);
      const listKeyword = schemaPointer.slice(schemaPointer.lastIndexOf('/') + 1);
      return [{
        subject: pointer,
        diagnostic: Diagnostic.at(
          instance.valuePosition(pointer),
          `${field} must NOT have more than ${limitText(error.params.limit)} items`,
        )
          .withLabel(`breaks ${codeSpan(listKeyword)}`)
          .withRule(ruleAt(listKeyword, schemaPointer)),
      }];
    }
    if (error.keyword === 'additionalProperties' || error.keyword === 'unevaluatedProperties') {
      const name = error.keyword === 'additionalProperties'
        ? error.params.additionalProperty
        : error.params.unevaluatedProperty;
      const fieldPointer = `${pointer}/${escapePointer(name)}`;
      const couldAccept = () => fieldCouldBeAccepted(this.schema, schemaPointer, name);
      const isUnevaluated = error.keyword === 'unevaluatedProperties';
      if (isUnevaluated && instance.memberReadsUnknown(pointer, couldAccept)) return [];
      const allowed = allowedFields(this.schema, schemaPointer);
      return [{
        subject: fieldPointer,
        diagnostic: Diagnostic.at(
          instance.position(fieldPointer),
          `${codeSpan(name)} is not a field of ${field}`,
        )
          .withLabel(`${field} has no field ${codeSpan(name)}`)
          .withRule(rule)
          .withHelp(suggestion(name, allowed)),
      }];
    }
    if (error.keyword === 'enum') {
      const options = Array.isArray(error.schema) ? error.schema : [];
      const allowed = options.filter((option) => typeof option === 'string');
      const shown = options.map((option) => codeSpan(jsonText(option))).join(', ');
      return [{
        subject: pointer,
        diagnostic: Diagnostic.at(
          instance.valuePosition(pointer),
          `${field} must be one of ${shown}`,
        )
          .withLabel(`not one of the ${options.length} values \`enum\` allows`)
          .withRule(rule)
          .withHelp(typeof error.data === 'string' ? suggestion(error.data, allowed) : null),
      }];
    }
    if (error.keyword === 'type') {
      const expected = [].concat(error.schema).map(codeSpan);
      return [{
        subject: pointer,
        diagnostic: Diagnostic.at(
          instance.valuePosition(pointer),
          `${field} must be ${expected.join(' or ')}`,
        )
          .withLabel(`is ${withArticle(instance.jsonType(pointer, error.data))}`)
          .withRule(rule),
      }];
    }
    if (REQUIRED_KEYWORDS.has(error.keyword)) {
      const name = error.params.missingProperty;
      return [{
        subject: pointer,
        diagnostic: Diagnostic.at(instance.position(pointer), `${codeSpan(name)} is required`)
          .withLabel(`${field} has no field ${codeSpan(name)}`)
          .withRule(rule),
      }];
    }
    if (ALTERNATIVE_KEYWORDS.has(error.keyword)) {
      if (Array.isArray(error.params.passingSchemas)) {
        return atValue(`${field} matches more than one of its allowed forms`);
      }
      const holderPointer = schemaPointer.slice(0, schemaPointer.lastIndexOf('/'));
      return atValue(
        `${field} matches none of its allowed forms`,
        definitionDescription(this.schema, holderPointer),
      );
    }
    if (hasOwn(BOUND_COMPARISONS, error.keyword)) {
      const comparison = BOUND_COMPARISONS[error.keyword];
      return atValue(`${field} must be ${comparison} ${codeSpan(numberText(error.schema))}`);
    }
    if (hasOwn(LENGTH_LIMITS, error.keyword)) {
      const [comparison, unit] = LENGTH_LIMITS[error.keyword];
      return atValue(
        `${field} must NOT have ${comparison} than ${limitText(error.params.limit)} ${unit}`,
      );
    }
    switch (error.keyword) {
      case 'const': return atValue(`${field} must be ${codeSpan(jsonText(error.schema))}`);
      case 'multipleOf':
        return atValue(`${field} must be multiple of ${codeSpan(numberText(error.schema))}`);
      case 'pattern':
        return atValue(`${field} must match pattern ${codeSpan(error.params.pattern)}`);
      case 'uniqueItems': {
        const items = Array.isArray(error.data) ? error.data : [];
        const [later, earlier] = duplicateItems(items) || [0, 0];
        return atValue(
          `${field} must NOT have duplicate items (items ## ${later} and ${earlier} are identical)`,
        );
      }
      case 'contains': return atValue(`${field} ${containsMessage(error.parentSchema)}`);
      case 'not':
        return atValue(
          `${field} matches a form that is not allowed`,
          descriptionText(schemaNodeAt(this.schema, schemaPointer)),
        );
      case 'false schema': return atValue(`${field} boolean schema is false`);
      case 'unevaluatedItems': {
        const length = (valueAt(json, pointer) || []).length;
        let beyond = 0;
        for (let index = error.params.limit; index < length; index += 1) {
          const couldEvaluate = () => itemCouldBeEvaluated(this.schema, schemaPointer, index);
          if (!instance.memberReadsUnknown(pointer, couldEvaluate)) beyond += 1;
        }
        if (beyond === 0) return [];
        return atValue(`${field} must NOT have more than ${limitText(length - beyond)} items`);
      }
      default: return atValue(`${field} breaks ${codeSpan(keyword)}`);
    }
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

/**
 * The fields a schema node allows: the `properties` of every schema the node
 * holding the keyword at `keywordPointer` reaches.
 */
function allowedFields(schema, keywordPointer) {
  const fields = [];
  const reachable = reachableSchemas(schema, keywordPointer);
  for (const { node } of reachable === null ? [] : reachable.schemaNodes) {
    if (!isObject(node.properties)) continue;
    for (const name of Object.keys(node.properties)) if (!fields.includes(name)) fields.push(name);
  }
  return fields;
}

function fieldCouldBeAccepted(schema, keywordPointer, name) {
  const patternAccepts = (pattern) => {
    const compiled = compilePattern(pattern);
    return compiled === null || compiled.test(name);
  };
  return schemaCouldRead(schema, keywordPointer, (node, isHolder) => {
    const patterns = isObject(node.patternProperties) ? Object.keys(node.patternProperties) : [];
    return (isObject(node.properties) && Object.hasOwn(node.properties, name))
      || patterns.some(patternAccepts)
      || isNotFalse(node, 'additionalProperties')
      || (!isHolder && isNotFalse(node, 'unevaluatedProperties'));
  });
}

function itemCouldBeEvaluated(schema, keywordPointer, index) {
  return schemaCouldRead(schema, keywordPointer, (node, isHolder) => {
    const itemsEvaluate = Array.isArray(node.items)
      ? node.items.length > index || isNotFalse(node, 'additionalItems')
      : isNotFalse(node, 'items');
    return (Array.isArray(node.prefixItems) && node.prefixItems.length > index)
      || itemsEvaluate
      || node.contains !== undefined
      || (!isHolder && isNotFalse(node, 'unevaluatedItems'));
  });
}

function isNotFalse(node, keyword) {
  return node[keyword] !== undefined && node[keyword] !== false;
}

function schemaCouldRead(schema, keywordPointer, readsMember) {
  const reachable = reachableSchemas(schema, keywordPointer);
  return reachable === null
    || reachable.reachesOtherDocument
    || reachable.schemaNodes.some(({ node, isHolder }) => readsMember(node, isHolder));
}

function reachableSchemas(schema, keywordPointer) {
  const holderPointer = keywordPointer.includes('/')
    ? keywordPointer.slice(0, keywordPointer.lastIndexOf('/'))
    : '';
  const holder = schemaNodeAt(schema, holderPointer);
  if (holder === undefined) return null;
  const reachable = { schemaNodes: [], reachesOtherDocument: false };
  collectReachable(schema, holder, true, reachable, new Set());
  return reachable;
}

function collectReachable(schema, node, isHolder, reachable, visited) {
  if (!isObject(node)) return;
  reachable.schemaNodes.push({ node, isHolder });
  if (typeof node.$ref === 'string') {
    const pointer = node.$ref.startsWith('#') ? node.$ref.slice(1) : null;
    if (pointer === null) reachable.reachesOtherDocument = true;
    else if (!visited.has(pointer)) {
      visited.add(pointer);
      const target = schemaNodeAt(schema, pointer);
      if (target !== undefined) collectReachable(schema, target, false, reachable, visited);
    }
  }
  const branches = [
    ...['allOf', 'anyOf', 'oneOf']
      .flatMap((combinator) => (Array.isArray(node[combinator]) ? node[combinator] : [])),
    ...['if', 'then', 'else']
      .filter((keyword) => node[keyword] !== undefined)
      .map((keyword) => node[keyword]),
    ...(isObject(node.dependentSchemas) ? Object.values(node.dependentSchemas) : []),
  ];
  for (const branch of branches) collectReachable(schema, branch, false, reachable, visited);
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

function withArticle(noun) {
  return `${/^[aeiou]/.test(noun) ? 'an' : 'a'} ${noun}`;
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

function compareDiagnostics(first, second) {
  const firstPlace = first.position ? [first.position.line, first.position.column] : [0, 0];
  const secondPlace = second.position ? [second.position.line, second.position.column] : [0, 0];
  return firstPlace[0] - secondPlace[0] || firstPlace[1] - secondPlace[1]
    || compareText(first.message, second.message);
}

function sameDiagnostic(first, second) {
  return JSON.stringify(first) === JSON.stringify(second);
}

function pointerSteps(pointer) {
  return pointer.slice(1).split('/')
    .map((step) => step.replaceAll('~1', '/').replaceAll('~0', '~'));
}

/** The value at a JSON pointer of `json`, `null` where it holds none. */
function valueAt(json, pointer) {
  if (pointer === '') return json;
  let node = json;
  for (const step of pointerSteps(pointer)) {
    if (node === null || typeof node !== 'object' || !hasOwn(node, step)) return null;
    node = node[step];
  }
  return node;
}

/** `json` with the value at a JSON pointer it holds replaced by `value`. */
function withValueAt(json, pointer, value) {
  if (pointer === '') return value;
  const holder = valueAt(json, pointer.slice(0, pointer.lastIndexOf('/')));
  if (holder !== null && typeof holder === 'object') holder[pointerSteps(pointer).at(-1)] = value;
  return json;
}

/** The document as JSON, and where each of its values was written. */
class InstanceRecord {
  constructor(rules) {
    this.rules = rules;
    this.positions = new Map();
    this.valuePositions = new Map();
    this.numberTexts = new Map();
    this.unknown = [];
    this.copied = [];
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
        this.mergeBaseKeys(object, pointer);
        return object;
      }
      default:
        this.unknown.push(pointer);
        return null;
    }
  }

  /** A key such as `name mock-orders` sets the field `name` of the entry it keys, unless it writes the field. */
  setChildValue(key, fields) {
    const found = this.rules.childValue(key);
    if (found !== null && !hasOwn(fields, found.field)) fields[found.field] = found.value;
  }

  /** Merges the content of each merge base key `object` holds into each sibling map. */
  mergeBaseKeys(object, pointer) {
    const { mergeBaseKeys } = this.rules;
    for (const baseKey of mergeBaseKeys.filter((key) => hasOwn(object, key))) {
      const base = object[baseKey];
      for (const [key, written] of Object.entries(object)) {
        if (mergeBaseKeys.includes(key) || !isObject(written)) continue;
        const entryPointer = `${pointer}/${escapePointer(key)}`;
        const merged = mergedOver(base, written, entryPointer, (at) => this.isUnknown(at));
        this.copied.push(...copiedPointers(merged, written, entryPointer));
        object[key] = merged;
      }
    }
  }

  /** Whether `pointer` names a value a merge base key copied in, or one below it. */
  isCopied(pointer) {
    return this.copied.some((copied) => pointer === copied || pointer.startsWith(`${copied}/`));
  }

  /**
   * Whether an error at `pointer` comes from a value not known when the document
   * compiles: the value itself, or a check that reads a value below it through a
   * subschema, or compares the values below it.
   */
  readsUnknown(pointer, keyword) {
    if (this.isUnknown(pointer)) return true;
    return READS_BELOW_KEYWORDS.has(keyword) && this.holdsUnknownBelow(pointer);
  }

  memberReadsUnknown(pointer, schemaCouldRead) {
    return this.holdsUnknownBelow(pointer) && schemaCouldRead();
  }

  holdsUnknownBelow(pointer) {
    const below = `${pointer}/`;
    return this.unknown.some((unknown) => unknown.startsWith(below));
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
}

module.exports = {
  LoadedSchema,
  SchemaCache,
  ValidationRules,
  childValue,
  closestName,
  nodeJsonText,
  draftOfSchema,
  compareText,
};
