'use strict';

const FUNCTION_TABLE_POINTER = '/x-yaml-dsl-functions';
const FUNCTION_TABLE_FIELD = 'x-yaml-dsl-functions';
const MACRO_KINDS = new Set(['function_call', 'merge', 'loop', 'transform']);
const MERGE_BEHAVIORS = new Set(['shallow', 'deep', 'join']);
const ROLE_NAMES = ['callee', 'callee_arguments', 'content', 'loop_variables', 'collection', 'body'];
const FUNCTION_NAME_START = /^[A-Za-z_]/;
const FUNCTION_NAME_CHARACTERS = /^[A-Za-z0-9_:-]*$/;

const hasOwn = (object, field) => Object.prototype.hasOwnProperty.call(object, field);
const isObject = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);

/** A macro of the table: its `kind`, `mergeBehavior`, `nameSuffix` and its arguments by role. */
class MacroEntry {
  constructor(kind, mergeBehavior, nameSuffix, roles) {
    this.kind = kind;
    this.mergeBehavior = mergeBehavior;
    this.nameSuffix = nameSuffix;
    this.roles = roles;
  }

  /** The name of the entry's argument that plays `role`. */
  role(role) {
    return hasOwn(this.roles, role) ? this.roles[role] : null;
  }
}

/** The functions and macros a schema publishes at `#/x-yaml-dsl-functions`. */
class FunctionTable {
  constructor(entries = new Map()) {
    this.entries = entries;
  }

  /** The table `schema` publishes, or throws a message naming what is wrong with it. */
  static fromSchema(schema) {
    if (!isObject(schema) || !hasOwn(schema, FUNCTION_TABLE_FIELD)) return new FunctionTable();
    const table = schema[FUNCTION_TABLE_FIELD];
    if (!isObject(table)) {
      throw new Error(`${FUNCTION_TABLE_POINTER} is not a mapping of function names`);
    }
    const entries = new Map(Object.entries(table)
      .sort(([first], [second]) => compareText(first, second))
      .map(([name, entry]) => [name, functionEntry(name, entry)]));
    return new FunctionTable(entries);
  }

  /** The entry a call's function name calls, a suffixed macro name included. */
  entry(functionName) {
    const direct = this.entries.get(functionName);
    if (direct && (!direct.macroEntry || !direct.macroEntry.nameSuffix)) return direct;
    for (const [name, entry] of this.entries) {
      const suffixPattern = entry.macroEntry && entry.macroEntry.nameSuffix;
      if (!suffixPattern || !functionName.startsWith(name)) continue;
      if (suffixPattern.test(functionName.slice(name.length))) return entry;
    }
    return null;
  }
}

/** Orders names by their UTF-8 bytes, as a sorted map of the crate holds them. */
function compareText(first, second) {
  return Buffer.compare(Buffer.from(first, 'utf8'), Buffer.from(second, 'utf8'));
}

function functionEntry(name, entry) {
  const listed = Array.isArray(entry) ? entry : isObject(entry) ? entry.arguments : undefined;
  if (!Array.isArray(listed)) throw new Error(`function \`${name}\` has no arguments list`);
  const functionArguments = listed.map((argument) => {
    const argumentName = isObject(argument) ? argument.name : undefined;
    if (typeof argumentName !== 'string') throw new Error(`an argument of \`${name}\` has no name`);
    return {
      name: argumentName,
      repeated: typeof argument.repeated === 'boolean' ? argument.repeated : false,
      default: hasOwn(argument, 'default') ? argument.default : null,
    };
  });
  const macroEntry = isObject(entry) && hasOwn(entry, 'macro')
    ? macroEntryOf(name, entry.macro)
    : null;
  return { arguments: functionArguments, macroEntry };
}

function macroEntryOf(name, value) {
  const kind = isObject(value) ? value.kind : undefined;
  if (!MACRO_KINDS.has(kind)) {
    const shown = typeof kind === 'string' ? `Some(${JSON.stringify(kind)})` : 'None';
    throw new Error(`macro \`${name}\` has an unknown kind ${shown}`);
  }
  const written = typeof value.merge_behavior === 'string' ? value.merge_behavior : null;
  if (written !== null && !MERGE_BEHAVIORS.has(written)) {
    throw new Error(`macro \`${name}\` has an unknown merge_behavior \`${written}\``);
  }
  let nameSuffix = null;
  if (typeof value.name_suffix_pattern === 'string') {
    try {
      nameSuffix = new RegExp(`^(?:${value.name_suffix_pattern})$`, 'u');
    } catch (error) {
      throw new Error(`macro \`${name}\`: ${error.message}`);
    }
  }
  const roles = {};
  for (const role of ROLE_NAMES) {
    if (typeof value[role] === 'string') roles[role] = value[role];
  }
  if (!hasOwn(roles, 'loop_variables') && typeof value.loop_variable === 'string') {
    roles.loop_variables = value.loop_variable;
  }
  return new MacroEntry(kind, written || 'shallow', nameSuffix, roles);
}

/**
 * A call written in a key, `<name> <marker><function>[splat]` or the marker alone,
 * as `{ name, function, splat }`; `null` for a key that writes none.
 */
function parseCallKey(key, marker) {
  const trimmed = key.trimEnd();
  const space = trimmed.lastIndexOf(' ');
  const callStart = space === -1 ? 0 : space + 1;
  const callText = trimmed.slice(callStart);
  if (!callText.startsWith(marker.call_marker)) return null;
  const rest = callText.slice(marker.call_marker.length);
  const splatOperator = marker.splat_operator;
  const splat = typeof splatOperator === 'string' && rest.endsWith(splatOperator);
  const functionName = splat ? rest.slice(0, rest.length - splatOperator.length) : rest;
  if (!FUNCTION_NAME_START.test(functionName) || !FUNCTION_NAME_CHARACTERS.test(functionName)) return null;
  return {
    name: callStart > 0 ? trimmed.slice(0, callStart).trim() : null,
    function: functionName,
    splat,
  };
}

module.exports = { FUNCTION_TABLE_POINTER, FunctionTable, MacroEntry, parseCallKey };
