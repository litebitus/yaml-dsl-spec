'use strict';

const assert = require('node:assert');
const test = require('node:test');

const { schemaValidation, resolved, Position, closestName } = require('..');

const { LoadedSchema, ValidationRules } = schemaValidation;
const { Value, call } = resolved;
const noRules = new ValidationRules();

const position = Position.fileStart('mock.yml');
const map = (entries) => new Value('map', entries.map(([key, value]) => ({ key, keyPosition: position, value })), position);
const unknownCall = () => new Value('call', call('mock', []), position);

function queueSchema() {
  return LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: 'https://json-schema.org/draft/2020-12/schema',
    type: 'object',
    properties: {
      queue: {
        type: 'object',
        additionalProperties: {
          type: 'object',
          required: ['name', 'visibility'],
          properties: {
            name: { type: 'string' },
            visibility: { type: 'integer' },
            encrypted: { type: 'boolean' },
          },
        },
      },
    },
  }));
}

test('a key sets its entry field before validation', () => {
  const document = map([['queue', map([['name mock-orders', map([['visibility', new Value('number', '30', position)]])]])]]);
  const childValuePatterns = [/^(?<field>[a-z]+) (?<value>[a-z-]+)$/u];
  const rules = new ValidationRules({ childValuePatterns });
  assert.deepStrictEqual(queueSchema().validate(document, rules), []);
});

test('a merge base key supplies its siblings and a quoted value reads as its type', () => {
  const number = (text) => new Value('number', text, position);
  const document = map([['queue', map([
    ['defaults', map([['name', Value.string('mock', position)], ['visibility', number('30')]])],
    ['orders', map([['encrypted', Value.string('true', position)]])],
    ['payments', map([['encrypted', Value.string('yes', position)]])],
  ])]]);
  const rules = new ValidationRules({
    mergeBaseKeys: ['defaults'],
    quotedValueTypes: ['boolean'],
  });
  const messages = queueSchema().validate(document, rules)
    .map((diagnostic) => diagnostic.message);
  assert.deepStrictEqual(messages, ['`encrypted` must be `boolean`']);
});

test('a call is not checked and a wrong type is', () => {
  const document = map([['queue', map([['orders', map([
    ['name', unknownCall()],
    ['visibility', Value.string('thirty', position)],
  ])]])]]);
  const diagnostics = queueSchema().validate(document, noRules);
  assert.strictEqual(diagnostics.length, 1);
  assert.strictEqual(diagnostics[0].message, '`visibility` must be `integer`');
  assert.strictEqual(diagnostics[0].notes.label, 'is a string');
  assert.strictEqual(
    diagnostics[0].notes.rule,
    'type, at mock.schema.json#/properties/queue/additionalProperties/properties/visibility/type',
  );
});

test('a check through a subschema reading an unknown value is not made', () => {
  const schema = LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: 'https://json-schema.org/draft/2020-12/schema',
    type: 'object',
    properties: {
      secrets: { type: 'object', oneOf: [{ additionalProperties: { type: 'string' } }] },
      count: { oneOf: [{ type: 'integer' }] },
    },
  }));
  const document = map([
    ['secrets', map([['token', unknownCall()]])],
    ['count', Value.string('three', position)],
  ]);
  const diagnostics = schema.validate(document, noRules);
  assert.deepStrictEqual(
    diagnostics.map((diagnostic) => diagnostic.message),
    ['`count` matches none of its allowed forms'],
  );
});

test('a local reference is validated as the value it reads', () => {
  const local = Value.string('thirty', position);
  const target = { kind: 'local', value: local };
  const reference = new Value('reference', resolved.reference({ scope: 'local', name: 'n', path: '', text: 'local.n', target }), position);
  const document = map([['queue', map([['orders', map([
    ['name', Value.string('mock-orders', position)],
    ['visibility', reference],
  ])]])]]);
  const diagnostics = queueSchema().validate(document, noRules);
  assert.deepStrictEqual(
    diagnostics.map((diagnostic) => diagnostic.message),
    ['`visibility` must be `integer`'],
  );
});

test('a value holding backticks is fenced in more backticks than it holds in a row', () => {
  const schema = LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: 'https://json-schema.org/draft/2020-12/schema',
    type: 'object',
    properties: {
      quoted: { const: 'mock`on' },
      wrapped: { pattern: '`[a-z]+`' },
    },
  }));
  const document = map([
    ['quoted', Value.string('off', position)],
    ['wrapped', Value.string('plain', position)],
  ]);
  assert.deepStrictEqual(
    schema.validate(document, noRules).map((diagnostic) => diagnostic.message),
    ['`quoted` must be ``"mock`on"``', '`wrapped` must match pattern `` `[a-z]+` ``'],
  );
});

test('a closest name is offered only when it is close and alone', () => {
  assert.strictEqual(closestName('brnch', ['branch', 'tags']), 'branch');
  assert.strictEqual(closestName('xyz', ['branch', 'tags']), null);
  assert.strictEqual(closestName('aa', ['ab', 'ac']), null);
});

test('a schema declaring a draft the reader does not read is refused with the drafts it reads', () => {
  assert.throws(
    () => LoadedSchema.parse('mock.schema.json', '{"$schema": "https://mock.example/schema"}'),
    { message: 'schema mock.schema.json: declares "https://mock.example/schema", a draft this reader does not read; it reads draft-07, draft 2019-09, draft 2020-12' },
  );
});
