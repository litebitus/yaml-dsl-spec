'use strict';

const { Diagnostic } = require('./diagnostic-report');

/** One step of a concrete path into a document: `{ key }` or `{ index }`. */
function keyStep(key) {
  return { key };
}

function indexStep(index) {
  return { index };
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
    const rest = text.slice(1);
    const selectors = [];
    let index = 0;
    while (index < rest.length) {
      const descendant = rest.startsWith('..', index);
      if (descendant) index += 2;
      else if (rest[index] === '.') index += 1;
      let name;
      let consumed;
      if (rest.startsWith("['", index)) {
        const close = rest.indexOf("']", index + 2);
        if (close === -1) throw invalid();
        name = rest.slice(index + 2, close);
        consumed = close - index + 2;
      } else if (rest.startsWith('[*]', index)) {
        name = null;
        consumed = 3;
      } else if (rest.startsWith('*', index)) {
        name = null;
        consumed = 1;
      } else {
        const ends = [rest.indexOf('.', index), rest.indexOf('[', index)].filter((end) => end !== -1);
        const end = ends.length > 0 ? Math.min(...ends) : rest.length;
        if (end === index) throw invalid();
        name = rest.slice(index, end);
        consumed = end - index;
      }
      index += consumed;
      if (!descendant) selectors.push(name === null ? { kind: 'wildcard' } : { kind: 'name', name });
      else if (name !== null) selectors.push({ kind: 'descendant_name', name });
      else if (index === rest.length) selectors.push({ kind: 'descendant_all' });
      else throw invalid();
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
