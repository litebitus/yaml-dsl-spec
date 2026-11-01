'use strict';

/*
 * How a document is read before it is validated: the DSL definition's
 * `validator` block, ready to apply.
 */

const { compilePattern } = require('./pattern-dialect');
const { numberJson } = require('./yaml-node');

const INTEGER_TEXT = /^-?(0|[1-9][0-9]*)$/;
const NUMBER_TEXT = /^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$/;
const QUOTED_VALUE_READERS = {
  boolean: (text) => ({ true: true, false: false })[text],
  integer: (text) => (INTEGER_TEXT.test(text) ? numberJson(text) : undefined),
  number: (text) => (NUMBER_TEXT.test(text) ? numberJson(text) : undefined),
};

const hasOwn = (object, field) => Object.prototype.hasOwnProperty.call(object, field);
const isObject = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);

/** A DSL's `validator` block with its patterns compiled. */
class ValidationRules {
  constructor({ mergeBaseKeys = [], childValuePatterns = [], quotedValueTypes = [] } = {}) {
    this.mergeBaseKeys = mergeBaseKeys;
    this.childValuePatterns = childValuePatterns;
    this.quotedValueTypes = quotedValueTypes;
  }

  /** The rules a DSL's `validator` block states, or throws a pattern that does not compile. */
  static fromValidator(validator) {
    if (!validator) return new ValidationRules();
    const childValuePatterns = validator.child_values_from_parent_key.map((rule) => {
      const compiled = compilePattern(rule.pattern);
      if (compiled === null) throw new Error(`\`${rule.pattern}\` is not a pattern`);
      return compiled;
    });
    return new ValidationRules({
      mergeBaseKeys: [...validator.merge_base_keys],
      childValuePatterns,
      quotedValueTypes: [...validator.allow_quoted_values],
    });
  }

  /** The field a key such as `name mock-orders` sets, `{ field, value }`; else `null`. */
  childValue(key) {
    return childValue(key, this.childValuePatterns);
  }

  /**
   * The value a quoted `text` is validated as where the schema expects one of
   * `expectedTypes`, in their order, `{ value }`; `null` where it reads as none.
   */
  quotedValue(text, expectedTypes) {
    for (const expectedType of expectedTypes) {
      const allowed = this.quotedValueTypes.includes(expectedType)
        && hasOwn(QUOTED_VALUE_READERS, expectedType);
      if (!allowed) continue;
      const value = QUOTED_VALUE_READERS[expectedType](text);
      if (value !== undefined) return { value };
    }
    return null;
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

/**
 * `overlay` merged over `base` as an overlay merges over its common layer: maps
 * merge key by key, an empty map or any other value replaces, and a null deletes
 * the key unless `holdsUnknown` says the null stands for a value not yet known.
 */
function mergedOver(base, overlay, overlayPointer, holdsUnknown) {
  const merged = isObject(base) ? structuredClone(base) : {};
  mergeInto(merged, overlay, overlayPointer, holdsUnknown);
  return merged;
}

function mergeInto(target, overlay, overlayPointer, holdsUnknown) {
  for (const [key, value] of Object.entries(overlay)) {
    const childPointer = `${overlayPointer}/${escapePointer(key)}`;
    if (value === null && !holdsUnknown(childPointer)) {
      delete target[key];
      continue;
    }
    if (isObject(target[key]) && isObject(value) && Object.keys(value).length > 0) {
      mergeInto(target[key], value, childPointer, holdsUnknown);
    } else {
      target[key] = structuredClone(value);
    }
  }
}

/** The pointers of `merged` that `written` does not hold: the values a merge copied in. */
function copiedPointers(merged, written, pointer) {
  if (!isObject(merged) || !isObject(written)) return [];
  return Object.entries(merged).flatMap(([key, value]) => {
    const childPointer = `${pointer}/${escapePointer(key)}`;
    return hasOwn(written, key)
      ? copiedPointers(value, written[key], childPointer)
      : [childPointer];
  });
}

function escapePointer(key) {
  return key.replaceAll('~', '~0').replaceAll('/', '~1');
}

module.exports = { ValidationRules, childValue, mergedOver, copiedPointers, escapePointer };
