'use strict';

const { parseAllDocuments, isAlias, isMap, isScalar, isSeq } = require('yaml');

const { Diagnostic } = require('./diagnostic-report');
const { Node } = require('./yaml-node');
const { Position } = require('./source-position');

const CORE_TAG_PREFIX = 'tag:yaml.org,2002:';
const SHORTHAND_CORE_PREFIX = '!!';
const STRING_TAGS = new Set(['!!str', 'tag:yaml.org,2002:str']);
const NULL_TEXTS = new Set(['', '~', 'null', 'Null', 'NULL']);
const TRUE_TEXTS = new Set(['true', 'True', 'TRUE']);
const FALSE_TEXTS = new Set(['false', 'False', 'FALSE']);
const UNSIGNED_INTEGER = /^\d+$/;
const BLOCK_SCALAR_TYPES = new Set(['BLOCK_LITERAL', 'BLOCK_FOLDED']);
const PROPERTY_MARKERS = new Set(['&', '!']);
const FLOW_OPENERS = new Set(['[', '{', ',']);

/** Offsets of a text as 1-based lines and columns, columns counted in characters. */
class LinePlaces {
  constructor(text) {
    this.text = text;
    this.lineStarts = [0];
    for (let offset = 0; offset < text.length; offset += 1) {
      const character = text[offset];
      if (character === '\n') this.lineStarts.push(offset + 1);
      else if (character === '\r' && text[offset + 1] !== '\n') this.lineStarts.push(offset + 1);
    }
  }

  placeOf(offset) {
    let low = 0;
    let high = this.lineStarts.length - 1;
    while (low < high) {
      const middle = Math.ceil((low + high) / 2);
      if (this.lineStarts[middle] <= offset) low = middle;
      else high = middle - 1;
    }
    const lineStart = this.lineStarts[low];
    return { line: low + 1, column: Array.from(this.text.slice(lineStart, offset)).length + 1 };
  }
}

/**
 * Parses the first document of `text` into `{ node }`, or `{ problem }`. A node
 * carrying one of `tagKeys`, each `{ tag, key }`, reads as a map holding that
 * tag's key, the node its value.
 */
function parseYaml(file, text, tagKeys = []) {
  const places = new LinePlaces(text);
  const positionAt = (offset) => {
    const { line, column } = places.placeOf(offset);
    return new Position(file, line, column);
  };
  const documents = parseAllDocuments(text, {
    schema: 'failsafe',
    uniqueKeys: false,
    prettyErrors: false,
    strict: false,
  });
  const document = Array.isArray(documents) ? documents[0] : undefined;
  if (!document || document.contents === null) {
    const errors = document ? document.errors : [];
    if (errors.length > 0) return { problem: Diagnostic.at(positionAt(errors[0].pos[0]), errors[0].message) };
    return { node: new Node('null', null, Position.fileStart(file)) };
  }
  if (document.errors.length > 0) {
    const [error] = document.errors;
    return { problem: Diagnostic.at(positionAt(error.pos[0]), error.message) };
  }
  const builder = new NodeBuilder(document, text, tagKeys, positionAt);
  try {
    return { node: builder.node(document.contents) };
  } catch (problem) {
    if (problem instanceof Diagnostic) return { problem };
    throw problem;
  }
}

class NodeBuilder {
  constructor(document, text, tagKeys, positionAt) {
    this.document = document;
    this.text = text;
    this.tagKeys = tagKeys;
    this.positionAt = positionAt;
    this.anchored = new Map();
  }

  node(source) {
    if (isAlias(source)) {
      const target = source.resolve(this.document);
      if (!target || !this.anchored.has(target)) {
        throw Diagnostic.at(this.positionAt(source.range[0]), 'alias of an anchor not defined before it');
      }
      return this.anchored.get(target);
    }
    const position = this.positionAt(this.startOf(source));
    let node;
    if (isScalar(source)) node = scalarNode(source, position);
    else if (isSeq(source)) node = new Node('list', source.items.map((item) => this.node(item)), position);
    else node = new Node('map', this.entries(source), position);
    const tagKey = this.tagKeyOf(source.tag);
    if (tagKey) {
      node = new Node('map', [{ key: tagKey.key, keyPosition: node.position, value: node }], node.position);
    }
    if (source.anchor) this.anchored.set(source, node);
    return node;
  }

  /**
   * Where a node starts as the standard places it: at its first property, a tag
   * or an anchor, where it has one; a block scalar at its content; an empty
   * value of a map at the `:` before it.
   */
  startOf(source) {
    const [valueStart] = source.range;
    const propertyCount = (source.tag ? 1 : 0) + (source.anchor ? 1 : 0);
    if (propertyCount > 0) return propertiesStart(this.text, valueStart, propertyCount);
    if (isScalar(source) && BLOCK_SCALAR_TYPES.has(source.type)) {
      return blockContentStart(this.text, valueStart);
    }
    if (isScalar(source) && source.type === 'PLAIN' && source.source === '') {
      return mapIndicatorBefore(this.text, valueStart);
    }
    return valueStart;
  }

  entries(source) {
    const entries = [];
    for (const pair of source.items) {
      const keyNode = this.node(pair.key);
      const key = keyNode.kind === 'null' ? null : keyNode.scalarText();
      if (key === null) throw Diagnostic.at(keyNode.position, 'a mapping key must be a scalar');
      if (entries.some((entry) => entry.key === key)) {
        throw Diagnostic.at(keyNode.position, `duplicate key \`${key}\``);
      }
      const value = pair.value === null
        ? new Node('null', null, this.positionAt(nextTokenStart(this.text, pair.key.range[1])))
        : this.node(pair.value);
      entries.push({ key, keyPosition: keyNode.position, value });
    }
    return entries;
  }

  tagKeyOf(tag) {
    if (!tag) return null;
    const written = tag.startsWith(CORE_TAG_PREFIX)
      ? `${SHORTHAND_CORE_PREFIX}${tag.slice(CORE_TAG_PREFIX.length)}`
      : tag;
    return this.tagKeys.find((tagKey) => tagKey.tag === written) || null;
  }
}

function isSpace(character) {
  return character === ' ' || character === '\t' || character === '\n' || character === '\r';
}

/** The offset of the first of `count` properties written before the value at `valueStart`. */
function propertiesStart(text, valueStart, count) {
  let offset = valueStart;
  for (let property = 0; property < count; property += 1) {
    let end = offset;
    while (end > 0 && isSpace(text[end - 1])) end -= 1;
    let start = end;
    while (start > 0 && !isSpace(text[start - 1])) start -= 1;
    if (!PROPERTY_MARKERS.has(text[start])) {
      const token = text.slice(start, end);
      const opener = Math.max(...[...FLOW_OPENERS].map((character) => token.lastIndexOf(character)));
      start += opener + 1;
    }
    if (!PROPERTY_MARKERS.has(text[start])) return offset;
    offset = start;
  }
  return offset;
}

/** The offset of a block scalar's first written character, after its header line. */
function blockContentStart(text, headerStart) {
  let offset = text.indexOf('\n', headerStart);
  if (offset === -1) return headerStart;
  offset += 1;
  while (offset < text.length && isSpace(text[offset])) offset += 1;
  return offset < text.length ? offset : headerStart;
}

/** The offset of the `:` an empty value of a map follows, else the value's own. */
function mapIndicatorBefore(text, valueStart) {
  let offset = valueStart;
  while (offset > 0 && (text[offset - 1] === ' ' || text[offset - 1] === '\t')) offset -= 1;
  return offset > 0 && text[offset - 1] === ':' ? offset - 1 : valueStart;
}

/** The offset of the first token at or after `offset`, past spaces, line breaks and comments. */
function nextTokenStart(text, offset) {
  let next = offset;
  while (next < text.length) {
    if (isSpace(text[next])) next += 1;
    else if (text[next] === '#') while (next < text.length && text[next] !== '\n') next += 1;
    else break;
  }
  return next;
}

function scalarNode(source, position) {
  const text = source.value === null || source.value === undefined ? '' : String(source.value);
  const explicitString = STRING_TAGS.has(source.tag);
  if (source.type !== 'PLAIN' || explicitString) return new Node('string', text, position);
  return plainScalarNode(text, position);
}

/** The YAML 1.2 core schema's reading of a plain scalar. */
function plainScalarNode(text, position) {
  if (NULL_TEXTS.has(text)) return new Node('null', null, position);
  if (TRUE_TEXTS.has(text)) return new Node('bool', true, position);
  if (FALSE_TEXTS.has(text)) return new Node('bool', false, position);
  return new Node(isCoreNumber(text) ? 'number' : 'string', text, position);
}

function isCoreNumber(text) {
  const unsigned = text.startsWith('-') || text.startsWith('+') ? text.slice(1) : text;
  if (unsigned === '') return false;
  if (UNSIGNED_INTEGER.test(unsigned)) return true;
  const exponentAt = unsigned.search(/[eE]/);
  const mantissa = exponentAt === -1 ? unsigned : unsigned.slice(0, exponentAt);
  const exponent = exponentAt === -1 ? null : unsigned.slice(exponentAt + 1);
  const dotAt = mantissa.indexOf('.');
  let mantissaOk;
  if (dotAt === -1) {
    mantissaOk = UNSIGNED_INTEGER.test(mantissa);
  } else {
    const whole = mantissa.slice(0, dotAt);
    const fraction = mantissa.slice(dotAt + 1);
    mantissaOk = (whole !== '' || fraction !== '')
      && (whole === '' || UNSIGNED_INTEGER.test(whole))
      && (fraction === '' || UNSIGNED_INTEGER.test(fraction));
  }
  let exponentOk = true;
  if (exponent !== null) {
    const digits = exponent.startsWith('-') || exponent.startsWith('+') ? exponent.slice(1) : exponent;
    exponentOk = UNSIGNED_INTEGER.test(digits);
  }
  return mantissaOk && exponentOk && (dotAt !== -1 || exponent !== null);
}

module.exports = { parseYaml, LinePlaces };
