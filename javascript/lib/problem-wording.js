'use strict';

/* How a schema problem is worded: the field it names and the sentence about it. */

const { descriptionText } = require('./requiredness-markers');

const DEPENDENCY_KEYWORDS = ['dependencies', 'dependentRequired', 'dependentSchemas'];
const SCHEMA_MAP_KEYWORDS = [
  '$defs',
  'definitions',
  'dependencies',
  'dependentSchemas',
  'patternProperties',
  'properties',
];
const DEFINITION_KEYWORDS = ['$defs', 'definitions'];
const UNSTATED_CONDITION = 'a condition the schema states';

const hasOwn = (object, field) => Object.prototype.hasOwnProperty.call(object, field);
const isObject = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);

function unescapePointer(step) {
  return step.replaceAll('~1', '/').replaceAll('~0', '~');
}

function escapePointer(key) {
  return key.replaceAll('~', '~0').replaceAll('/', '~1');
}

function schemaNodeAt(root, pointer) {
  if (pointer === '') return root;
  let node = root;
  for (const step of pointer.slice(1).split('/')) {
    const key = unescapePointer(step);
    if (node === null || typeof node !== 'object' || !hasOwn(node, key)) return undefined;
    node = node[key];
  }
  return node;
}

/**
 * The field a problem names: the key of the value at `pointer` in `document`, then
 * `[<index>]` for each list index below it; the root is `the document`.
 */
function fieldName(document, pointer) {
  if (pointer === '') return 'the document';
  const steps = [];
  let node = document;
  for (const step of pointer.slice(1).split('/')) {
    const key = unescapePointer(step);
    const inList = Array.isArray(node);
    if (inList) node = node[Number(key)];
    else node = isObject(node) && hasOwn(node, key) ? node[key] : undefined;
    steps.push({ key, inList });
  }
  let keyAt = steps.length - 1;
  while (keyAt >= 0 && steps[keyAt].inList) keyAt -= 1;
  const key = keyAt >= 0 ? steps[keyAt].key : '';
  const indexes = steps.slice(keyAt + 1).map((step) => `[${step.key}]`).join('');
  return codeSpan(`${key}${indexes}`);
}

/**
 * `text` fenced in the fewest backticks it does not hold, padded with a space where
 * it begins or ends with a backtick.
 */
function codeSpan(text) {
  const longestRun = Math.max(0, ...(text.match(/`+/gu) || []).map((run) => run.length));
  const fence = '`'.repeat(longestRun + 1);
  return text.startsWith('`') || text.endsWith('`')
    ? `${fence} ${text} ${fence}`
    : `${fence}${text}${fence}`;
}

/** A JSON number as a report writes it. */
function numberText(number) {
  return String(number);
}

/**
 * The description of the definition holding alternatives none of whose forms
 * holds, where they are a definition's, `#/$defs/<name>` or `#/definitions/<name>`;
 * else `null`.
 */
function definitionDescription(schema, holderPointer) {
  const steps = holderPointer.split('/').slice(1);
  if (steps.length !== 2 || !DEFINITION_KEYWORDS.includes(steps[0])) return null;
  return descriptionText(schemaNodeAt(schema, holderPointer));
}

/**
 * The dependency keyword's pointer and the key it names, `{ keywordPointer, key }`,
 * where `keywordPointer` lies inside a dependency: the key read from the pointer,
 * or, for a dependency listing fields, from the field `holder` misses; else `null`.
 */
function dependencyOf(schema, keywordPointer, holder, missingField) {
  const steps = keywordPointer.split('/').slice(1).map(unescapePointer);
  let at = steps.length - 1;
  while (at >= 0 && !(DEPENDENCY_KEYWORDS.includes(steps[at])
    && (at === 0 || !SCHEMA_MAP_KEYWORDS.includes(steps[at - 1])))) at -= 1;
  if (at < 0) return null;
  const dependencyPointer = steps.slice(0, at + 1)
    .map((step) => `/${escapePointer(step)}`)
    .join('');
  let key = steps[at + 1];
  if (key === undefined) {
    const listed = schemaNodeAt(schema, dependencyPointer);
    if (typeof missingField !== 'string' || !isObject(listed)) return null;
    key = Object.keys(listed).find((candidate) => isObject(holder) && hasOwn(holder, candidate)
      && Array.isArray(listed[candidate]) && listed[candidate].includes(missingField));
    if (key === undefined) return null;
  }
  if (!isObject(holder) || !hasOwn(holder, key)) return null;
  return { keywordPointer: dependencyPointer, key };
}

/**
 * `` `key` needs <what> ``: what each dependency of the node holding
 * `keywordPointer` states for `key`.
 */
function dependencyMessage(schema, keywordPointer, key) {
  const holder = schemaNodeAt(schema, keywordPointer.slice(0, keywordPointer.lastIndexOf('/')));
  const statesKey = (keyword) => isObject(holder)
    && isObject(holder[keyword])
    && hasOwn(holder[keyword], key);
  const neededTexts = DEPENDENCY_KEYWORDS
    .filter(statesKey)
    .map((keyword) => neededText(holder[keyword][key]));
  return `${codeSpan(key)} needs ${neededTexts.length > 0 ? neededTexts.join('; ') : UNSTATED_CONDITION}`;
}

function neededText(needed) {
  if (Array.isArray(needed)) return keyList(needed);
  return consequenceText(needed) || UNSTATED_CONDITION;
}

function consequenceText(node) {
  if (!isObject(node)) return null;
  const parts = [];
  if (Array.isArray(node.required) && node.required.length > 0) parts.push(keyList(node.required));
  if (isObject(node.not) && Array.isArray(node.not.required) && node.not.required.length > 0) {
    parts.push(`${keyList(node.not.required)} must not be set`);
  }
  const properties = isObject(node.properties) ? node.properties : {};
  for (const [field, fieldSchema] of Object.entries(properties)) {
    if (isObject(fieldSchema) && typeof fieldSchema.$ref === 'string') {
      parts.push(`${codeSpan(field)} is a ${codeSpan(referenceName(fieldSchema.$ref))}`);
    }
  }
  for (const part of Array.isArray(node.allOf) ? node.allOf : []) {
    const partText = consequenceText(part);
    if (partText) parts.push(partText);
  }
  const alternatives = ['anyOf', 'oneOf']
    .flatMap((keyword) => (Array.isArray(node[keyword]) ? node[keyword] : []));
  if (alternatives.length > 0) {
    parts.push(alternatives
      .map((alternative) => consequenceText(alternative) || UNSTATED_CONDITION)
      .join(', or '));
  }
  if (parts.length === 0 && typeof node.$ref === 'string') {
    parts.push(`a ${codeSpan(referenceName(node.$ref))}`);
  }
  return parts.length > 0 ? parts.join('; ') : null;
}

function referenceName(reference) {
  return reference.slice(reference.lastIndexOf('/') + 1).replace(/^#/u, '');
}

function keyList(keys) {
  const spans = keys.map((key) => codeSpan(typeof key === 'string' ? key : JSON.stringify(key)));
  return spans.length > 1 ? `${spans.slice(0, -1).join(', ')} and ${spans.at(-1)}` : spans.join('');
}

/**
 * The indexes of the duplicate items a list reports, later first, `[later, earlier]`:
 * from the last item back, the first one equal to an item after it, and the
 * nearest such item; `null` for a list without duplicates.
 */
function duplicateItems(items) {
  for (let earlier = items.length - 1; earlier >= 0; earlier -= 1) {
    for (let later = earlier + 1; later < items.length; later += 1) {
      if (jsonEqual(items[earlier], items[later])) return [later, earlier];
    }
  }
  return null;
}

function jsonEqual(first, second) {
  if (Array.isArray(first) && Array.isArray(second)) {
    return first.length === second.length
      && first.every((item, index) => jsonEqual(item, second[index]));
  }
  if (isObject(first) && isObject(second)) {
    const keys = Object.keys(first);
    return keys.length === Object.keys(second).length
      && keys.every((key) => hasOwn(second, key) && jsonEqual(first[key], second[key]));
  }
  return first === second;
}

/**
 * `must contain at least <min> valid item(s)`, with `and no more than <max>`
 * where `holder` bounds it.
 */
function containsMessage(holder) {
  const bound = (keyword) => (isObject(holder) && typeof holder[keyword] === 'number'
    ? codeSpan(numberText(holder[keyword]))
    : null);
  const minimum = bound('minContains') || codeSpan('1');
  const maximum = bound('maxContains');
  return maximum === null
    ? `must contain at least ${minimum} valid item(s)`
    : `must contain at least ${minimum} and no more than ${maximum} valid item(s)`;
}

module.exports = {
  fieldName,
  codeSpan,
  numberText,
  definitionDescription,
  dependencyOf,
  dependencyMessage,
  duplicateItems,
  containsMessage,
  schemaNodeAt,
  escapePointer,
};
