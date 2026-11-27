'use strict';

const NODE_KINDS = ['null', 'bool', 'number', 'string', 'list', 'map'];
const INTEGER_TEXT = /^[+-]?\d+$/;
const DECIMAL_TEXT = /^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/;
const SIGNED_64_RANGE = [-(2n ** 63n), 2n ** 63n - 1n];
const UNSIGNED_64_LIMIT = 2n ** 64n - 1n;

/**
 * A YAML value as written, with where each part was written. `value` is `null`,
 * a boolean, a number's text, a string, a list of nodes, or a list of entries,
 * each `{ key, keyPosition, value }`.
 */
class Node {
  constructor(kind, value, position) {
    this.kind = kind;
    this.value = value;
    this.position = position;
  }

  entries() {
    return this.kind === 'map' ? this.value : null;
  }

  items() {
    return this.kind === 'list' ? this.value : null;
  }

  asStr() {
    return this.kind === 'string' ? this.value : null;
  }

  get(key) {
    const found = (this.entries() || []).find((entry) => entry.key === key);
    return found ? found.value : null;
  }

  isMap() {
    return this.kind === 'map';
  }

  /** The scalar as text, for splicing into a longer string. */
  scalarText() {
    return scalarTextOf(this.kind, this.value);
  }

  toJson() {
    switch (this.kind) {
      case 'null': return null;
      case 'bool': return this.value;
      case 'number': return numberJson(this.value);
      case 'string': return this.value;
      case 'list': return this.value.map((item) => item.toJson());
      default: return Object.fromEntries(this.value.map((entry) => [entry.key, entry.value.toJson()]));
    }
  }
}

function scalarTextOf(kind, value) {
  switch (kind) {
    case 'null': return '';
    case 'bool': return String(value);
    case 'number':
    case 'string': return value;
    default: return null;
  }
}

/** The 64-bit integer a number's text writes, or `null` where it writes none. */
function integerOfText(text) {
  if (!INTEGER_TEXT.test(text)) return null;
  const integer = BigInt(text);
  const signed = integer >= SIGNED_64_RANGE[0] && integer <= SIGNED_64_RANGE[1];
  const unsigned = integer >= 0n && integer <= UNSIGNED_64_LIMIT;
  return signed || unsigned ? integer : null;
}

/** A number's text as JSON: a number where it reads as one, else the text. */
function numberJson(text) {
  const integer = integerOfText(text);
  if (integer !== null) return Number(integer);
  const number = DECIMAL_TEXT.test(text) ? Number(text) : Number.NaN;
  return Number.isFinite(number) ? number : text;
}

/** Whether a number's text reads as an integer rather than a floating-point number. */
function isIntegerText(text) {
  return integerOfText(text) !== null;
}

module.exports = { NODE_KINDS, Node, numberJson, isIntegerText, integerOfText, scalarTextOf };
