'use strict';

const assert = require('node:assert');
const test = require('node:test');

const { functionTable, parseCallKey } = require('..');

const MARKER = { call_marker: 'fn.', splat_operator: '*', calls_without_name_allowed: true };

test('parses named and nameless calls', () => {
  assert.deepStrictEqual(parseCallKey('tags fn.merge*', MARKER), { name: 'tags', function: 'merge', splat: true });
  assert.deepStrictEqual(parseCallKey('fn.loop', MARKER), { name: null, function: 'loop', splat: false });
  assert.strictEqual(parseCallKey('plain key', MARKER), null);
});

test('reads macro roles from the table', () => {
  const schema = {
    'x-yaml-dsl-functions': {
      loop: {
        arguments: [
          { name: 'in', repeated: false },
          { name: 'body', repeated: false },
          { name: 'for', repeated: false, default: ['it'] },
        ],
        macro: { kind: 'loop', loop_variables: 'for', collection: 'in', body: 'body' },
      },
    },
  };
  const entry = functionTable.FunctionTable.fromSchema(schema).entry('loop');
  assert.strictEqual(entry.macroEntry.kind, 'loop');
  assert.strictEqual(entry.macroEntry.role('collection'), 'in');
  assert.deepStrictEqual(entry.arguments[2].default, ['it']);
});

test('reads the deprecated forms until the next major', () => {
  const schema = {
    'x-yaml-dsl-functions': {
      join: [{ name: 'parts', repeated: true }],
      loop: {
        arguments: [
          { name: 'in', repeated: false },
          { name: 'body', repeated: false },
          { name: 'for', repeated: false },
        ],
        macro: { kind: 'loop', loop_variable: 'for', collection: 'in', body: 'body' },
      },
    },
  };
  const table = functionTable.FunctionTable.fromSchema(schema);
  const joinEntry = table.entry('join');
  assert.deepStrictEqual(joinEntry.arguments, [{ name: 'parts', repeated: true, default: null }]);
  assert.strictEqual(joinEntry.macroEntry, null);
  assert.strictEqual(table.entry('loop').macroEntry.role('loop_variables'), 'for');
});

test('a suffixed macro name calls its entry', () => {
  const schema = {
    'x-yaml-dsl-functions': {
      merge: { arguments: [], macro: { kind: 'merge', name_suffix_pattern: '[0-9]+' } },
    },
  };
  const table = functionTable.FunctionTable.fromSchema(schema);
  assert.strictEqual(table.entry('merge2').macroEntry.kind, 'merge');
  assert.strictEqual(table.entry('merge'), null);
  assert.strictEqual(table.entry('mergex'), null);
});

test('a table naming an unknown macro kind is refused', () => {
  const schema = { 'x-yaml-dsl-functions': { mock: { arguments: [], macro: { kind: 'mock-kind' } } } };
  assert.throws(() => functionTable.FunctionTable.fromSchema(schema), {
    message: 'macro `mock` has an unknown kind Some("mock-kind")',
  });
});
