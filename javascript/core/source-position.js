'use strict';

/** Where a value was written: a file, a 1-based line and a 1-based column. */
class Position {
  constructor(file, line, column) {
    this.file = file;
    this.line = line;
    this.column = column;
  }

  /** The start of a file, for a value that has no narrower place. */
  static fileStart(file) {
    return new Position(file, 1, 1);
  }

  toString() {
    return `${this.file}:${this.line}:${this.column}`;
  }
}

module.exports = { Position };
