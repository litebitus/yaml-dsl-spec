'use strict';

/** A problem found in a file, at the value it is about where there is one. */
class Diagnostic {
  constructor(position, message) {
    this.position = position;
    this.message = message;
    this.notes = null;
  }

  static at(position, message) {
    return new Diagnostic(position, message);
  }

  static unplaced(message) {
    return new Diagnostic(null, message);
  }

  /** What is wrong with the text at the position. */
  withLabel(label) {
    this.writableNotes().label = label;
    return this;
  }

  /** The schema rule the document breaks: its keyword and where the schema holds it. */
  withRule(rule) {
    this.writableNotes().rule = rule;
    return this;
  }

  /** How the problem may be fixed; nothing is noted for `null`. */
  withHelp(help) {
    if (help !== null && help !== undefined) this.writableNotes().help = help;
    return this;
  }

  writableNotes() {
    if (this.notes === null) this.notes = { label: null, rule: null, help: null };
    return this.notes;
  }

  toString() {
    return this.position === null ? this.message : `${this.position}: ${this.message}`;
  }
}

module.exports = { Diagnostic };
