'use strict';

const { Diagnostic } = require('./diagnostic-report');

/** One step of a concrete path into a document: `{ key }` or `{ index }`. */
function keyStep(key) {
  return { key };
}

function indexStep(index) {
  return { index };
}

/** A `['name']` step's name and the text after the step, or `null`. */
function quotedName(text) {
  if (!text.startsWith("['")) return null;
  const close = text.indexOf("'", 2);
  if (close <= 2 || text[close + 1] !== ']') return null;
  return { name: text.slice(2, close), remaining: text.slice(close + 2) };
}

/** A dotted step's name, which runs to the next `.` or `[`, and the text after it, or `null`. */
function dottedName(text) {
  if (text.startsWith('*')) return null;
  const stepEnd = text.search(/[.[]/);
  const end = stepEnd === -1 ? text.length : stepEnd;
  if (end === 0) return null;
  return { name: text.slice(0, end), remaining: text.slice(end) };
}

/** A parsed path and the keys its wildcard steps do not select or descend into. */
class PathPattern {
  constructor(selectors, skipKeys) {
    this.selectors = selectors;
    this.skipKeys = skipKeys;
  }

  /** The pattern `text` writes, or throws a `Diagnostic` naming why it is none. */
  static parse(text, skipKeys = []) {
    const invalid = () =>
      Diagnostic.unplaced(`\`${text}\` is not a path of the supported JSONPath subset`);
    if (!text.startsWith('$')) throw invalid();
    let rest = text.slice(1);
    const selectors = [];
    while (rest.length > 0) {
      let step;
      if (rest.startsWith('..')) {
        const after = rest.slice(2);
        if (after === '*') {
          step = { selector: { kind: 'descendant_all' }, remaining: '' };
        } else {
          const named = quotedName(after) || dottedName(after);
          if (!named) throw invalid();
          step = {
            selector: { kind: 'descendant_name', name: named.name },
            remaining: named.remaining,
          };
        }
      } else if (rest.startsWith('.*')) {
        step = { selector: { kind: 'wildcard' }, remaining: rest.slice(2) };
      } else if (rest.startsWith('.')) {
        const named = dottedName(rest.slice(1));
        if (!named) throw invalid();
        step = { selector: { kind: 'name', name: named.name }, remaining: named.remaining };
      } else if (rest.startsWith('[*]')) {
        step = { selector: { kind: 'wildcard' }, remaining: rest.slice(3) };
      } else {
        const named = quotedName(rest);
        if (!named) throw invalid();
        step = { selector: { kind: 'name', name: named.name }, remaining: named.remaining };
      }
      selectors.push(step.selector);
      rest = step.remaining;
    }
    return new PathPattern(selectors, [...skipKeys]);
  }

  /** Every node of `document` the pattern selects, `{ path, value }`, in document order. */
  select(document) {
    const selected = [];
    this.collect(document, [], selected);
    return selected;
  }

  collect(value, path, selected) {
    if (this.selects(path)) selected.push({ path: [...path], value });
    if (value.kind === 'map') {
      for (const entry of value.value) {
        path.push(keyStep(entry.key));
        this.collect(entry.value, path, selected);
        path.pop();
      }
    } else if (value.kind === 'list') {
      value.value.forEach((item, itemIndex) => {
        path.push(indexStep(itemIndex));
        this.collect(item, path, selected);
        path.pop();
      });
    }
  }

  /** Whether the pattern selects the node at `path`. */
  selects(path) {
    return this.matchesFrom(0, path);
  }

  /** Whether `path` is the node a step selects or anywhere below it. */
  covers(path) {
    for (let length = 0; length <= path.length; length += 1) {
      if (this.selects(path.slice(0, length))) return true;
    }
    return false;
  }

  skipped(step) {
    return 'key' in step && this.skipKeys.includes(step.key);
  }

  matchesFrom(selectorIndex, path) {
    const selector = this.selectors[selectorIndex];
    if (!selector) return path.length === 0;
    const [first] = path;
    switch (selector.kind) {
      case 'name':
        return first !== undefined && first.key === selector.name
          && this.matchesFrom(selectorIndex + 1, path.slice(1));
      case 'wildcard':
        return first !== undefined && !this.skipped(first)
          && this.matchesFrom(selectorIndex + 1, path.slice(1));
      case 'descendant_name':
        for (let depth = 0; depth < path.length; depth += 1) {
          if (path.slice(0, depth).every((step) => !this.skipped(step))
            && path[depth].key === selector.name
            && this.matchesFrom(selectorIndex + 1, path.slice(depth + 1))) {
            return true;
          }
        }
        return false;
      default:
        return path.length > 0 && path.every((step) => !this.skipped(step));
    }
  }
}

/** Several path entries; a node is selected when any of them selects it. */
class PathSet {
  constructor(patterns = []) {
    this.patterns = patterns;
  }

  /** The set of `entries`, each `{ path, skipKeys }`, or throws the first path's `Diagnostic`. */
  static parse(entries) {
    return new PathSet(entries.map(({ path, skipKeys }) => PathPattern.parse(path, skipKeys || [])));
  }

  selects(path) {
    return this.patterns.some((pattern) => pattern.selects(path));
  }

  covers(path) {
    return this.patterns.some((pattern) => pattern.covers(path));
  }
}

module.exports = { PathPattern, PathSet, keyStep, indexStep };
