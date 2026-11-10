'use strict';

const { scalarTextOf } = require('./yaml-node');

/**
 * A value of the folded, expanded document, with where it was written. `kind` is
 * a node's kind, or `reference`, `call` or `template`; `value` is what a node of
 * that kind holds, a list of values and of map entries `{ key, keyPosition, value }`
 * included, or the reference, the call, or the template's parts.
 */
class Value {
  constructor(kind, value, position) {
    this.kind = kind;
    this.value = value;
    this.position = position;
  }

  static string(text, position) {
    return new Value('string', text, position);
  }

  /** A node as written, each value with its position: nothing resolved. */
  static fromNode(node) {
    let value;
    if (node.kind === 'list') value = node.value.map((item) => Value.fromNode(item));
    else if (node.kind === 'map') {
      value = node.value.map((entry) => ({
        key: entry.key,
        keyPosition: entry.keyPosition,
        value: Value.fromNode(entry.value),
      }));
    } else value = node.value;
    return new Value(node.kind, value, node.position);
  }

  entries() {
    return this.kind === 'map' ? this.value : null;
  }

  items() {
    return this.kind === 'list' ? this.value : null;
  }

  get(key) {
    const found = (this.entries() || []).find((entry) => entry.key === key);
    return found ? found.value : null;
  }

  asStr() {
    return this.kind === 'string' ? this.value : null;
  }

  /** A scalar's text, for splicing into a longer string. */
  scalarText() {
    if (this.kind === 'reference' && this.value.target.kind === 'local') {
      return this.value.target.value.scalarText();
    }
    return scalarTextOf(this.kind, this.value);
  }

  /** The value with every local reference replaced by the local's value. */
  localValue() {
    if (this.kind === 'reference' && this.value.target.kind === 'local') {
      return this.value.target.value.localValue();
    }
    return this;
  }
}

/**
 * A name read by one of the definition's reference rules: `{ scope, scopeKey,
 * name, path, text, target }`. A target is `{ kind: 'local', value }`, or a
 * `kind` of `local_unknown`, `builtin`, `declaration` with its `position`, or
 * `undeclared`.
 */
function reference(fields) {
  return { scopeKey: null, ...fields };
}

/** A call left for the target to evaluate: `{ function, arguments, expandFinalArgument }`. */
function call(functionName, callArguments) {
  return { function: functionName, arguments: callArguments, expandFinalArgument: false };
}

/**
 * The map values `maps` merged in order by a `merge` macro's `mergeBehavior`, a map
 * at `position`: `deep` merges nested maps key by key, `shallow` replaces a key's
 * value whole, `join` takes each key once. Each entry keeps where it was written.
 */
function mapMergeResult(maps, mergeBehavior, position) {
  const entries = [];
  for (const entry of maps.flatMap((map) => map.value)) {
    const earlierAt = entries.findIndex((earlier) => earlier.key === entry.key);
    if (earlierAt === -1) entries.push(entry);
    else if (mergeBehavior !== 'join') {
      const earlierValue = entries[earlierAt].value.localValue();
      const laterValue = entry.value.localValue();
      const mergesDeep = mergeBehavior === 'deep'
        && earlierValue.kind === 'map'
        && laterValue.kind === 'map';
      const nestedMerge = () => mapMergeResult(
        [earlierValue, laterValue],
        mergeBehavior,
        laterValue.position,
      );
      entries[earlierAt] = mergesDeep ? { ...entry, value: nestedMerge() } : entry;
    }
  }
  return new Value('map', entries, position);
}

module.exports = { Value, reference, call, mapMergeResult };
