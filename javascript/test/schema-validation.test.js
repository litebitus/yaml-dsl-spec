'use strict';

const assert = require('node:assert');
const test = require('node:test');

const { schemaValidation, resolved, Position, closestName } = require('..');
const resolvedValue = require('../lib/resolved-value');

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

function fragmentSchema() {
  return LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: 'https://json-schema.org/draft/2020-12/schema',
    type: 'object',
    properties: { queue: { $ref: '#/$defs/queue' } },
    $defs: {
      queue: {
        type: 'object',
        required: ['name'],
        properties: {
          name: { type: 'string' },
          tags: { $ref: '#/$defs/tags' },
          retired: false,
        },
        anyOf: [{ required: ['arn'] }, { required: ['url'] }],
      },
      tags: { type: 'array' },
    },
  }));
}

test('each problem names the document value it is about', () => {
  const document = map([['queue', map([['orders', map([
    ['visibility', Value.string('thirty', position)],
    ['retension', Value.string('7', position)],
  ])]])]]);
  const problems = queueSchema().problems(document, noRules);
  assert.deepStrictEqual(
    problems.map((problem) => [problem.subject, problem.diagnostic.message]),
    [
      ['/queue/orders', '`name` is required'],
      ['/queue/orders/visibility', '`visibility` must be `integer`'],
    ],
  );
  assert.deepStrictEqual(
    queueSchema().validate(document, noRules),
    problems.map((problem) => problem.diagnostic),
  );
});

test('a fragment reads its references in the whole schema and requires no field', () => {
  const schema = fragmentSchema();
  const document = map([
    ['tags', Value.string('mock', position)],
    ['retired', new Value('bool', true, position)],
  ]);
  const problems = schema.fragmentProblems('/$defs/queue', document, noRules);
  assert.deepStrictEqual(
    problems.map(({ subject, diagnostic }) => [subject, diagnostic.message, diagnostic.notes.rule]),
    [
      [
        '/retired',
        '`retired` boolean schema is false',
        'falseSchema, at mock.schema.json#/$defs/queue/properties/retired',
      ],
      ['/tags', '`tags` must be `array`', 'type, at mock.schema.json#/$defs/tags/type'],
    ],
  );
  const firstValidator = schema.fragmentValidator('/$defs/queue');
  assert.strictEqual(schema.fragmentValidator('/$defs/queue'), firstValidator);
});

test('a pointer naming no node of the schema is one problem at the document', () => {
  const problems = fragmentSchema().fragmentProblems('/$defs/missing', map([]), noRules);
  assert.deepStrictEqual(problems.map((problem) => problem.subject), ['']);
  assert.strictEqual(problems[0].diagnostic.message, 'the schema has no node at `#/$defs/missing`');
  assert.strictEqual(problems[0].diagnostic.notes, null);
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

const DRAFT_2020_12 = 'https://json-schema.org/draft/2020-12/schema';
const declarationReference = () => new Value('reference', resolved.reference({
  scope: 'mock',
  name: 'mock',
  path: '',
  text: 'mock',
  target: { kind: 'declaration', position },
}), position);
const placedAt = (line) => new Position('mock.yml', line, 1);
const placedMap = (line, entries) => new Value(
  'map',
  entries.map(([key, value]) => ({ key, keyPosition: placedAt(line), value })),
  placedAt(line),
);
const messagesOf = (problems) => problems
  .map(({ subject, diagnostic }) => [subject, diagnostic.message]);

function strictQueueSchema(functionTable = {}) {
  return LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: DRAFT_2020_12,
    'x-yaml-dsl-functions': functionTable,
    type: 'object',
    properties: {
      queue: {
        type: 'object',
        additionalProperties: {
          type: 'object',
          required: ['visibility', 'tags'],
          additionalProperties: false,
          properties: {
            visibility: { type: 'integer' },
            retention: { type: 'integer' },
            tags: { type: 'object' },
          },
          anyOf: [{ required: ['fifo'] }, { required: ['retention'] }],
        },
      },
    },
  }));
}

test('a merge base key content requires no key while its siblings do', () => {
  const document = map([['queue', map([
    ['defaults', map([['retention', Value.string('forever', position)]])],
    ['orders', map([['visibility', new Value('number', '30', position)]])],
  ])]]);
  const rules = new ValidationRules({ mergeBaseKeys: ['defaults'] });
  assert.deepStrictEqual(messagesOf(strictQueueSchema().problems(document, rules)), [
    ['/queue/defaults/retention', '`retention` must be `integer`'],
    ['/queue/orders', '`tags` is required'],
  ]);
});

function mergeMacroTable(mergeBehavior) {
  const contents = {
    name: 'contents',
    type: 'map',
    required: true,
    repeated: true,
    description: 'The maps merged in.',
  };
  return {
    'mock-merge': {
      description: 'Merges its contents.',
      arguments: [contents],
      macro: { kind: 'merge', content: 'contents', merge_behavior: mergeBehavior },
    },
    'mock-function': { description: 'Returns a map.', arguments: [contents] },
  };
}

test('a merge call is validated as its known maps merged, requiring no key', () => {
  const mergeCall = new Value('call', call('mock-merge', [
    declarationReference(),
    placedMap(3, [['retention', Value.string('forever', placedAt(3))]]),
    placedMap(4, [['extra', Value.string('mock', placedAt(4))]]),
  ]), placedAt(2));
  const document = map([['queue', map([['orders', mergeCall]])]]);
  const problems = strictQueueSchema(mergeMacroTable('deep')).problems(document, noRules);
  assert.deepStrictEqual(
    problems.map(({ subject, diagnostic }) => [
      subject,
      diagnostic.message,
      diagnostic.position.line,
    ]),
    [
      ['/queue/orders/retention', '`retention` must be `integer`', 3],
      ['/queue/orders/extra', '`extra` is not defined in the schema', 4],
    ],
  );
});

test('a merge call whose arguments are all known maps requires its keys', () => {
  const mergeCall = new Value('call', call('mock-merge', [
    placedMap(3, [['retention', new Value('number', '7', placedAt(3))]]),
    placedMap(4, [['visibility', new Value('number', '30', placedAt(4))]]),
  ]), placedAt(2));
  const document = map([['queue', map([['orders', mergeCall]])]]);
  const problems = strictQueueSchema(mergeMacroTable('shallow')).problems(document, noRules);
  assert.deepStrictEqual(messagesOf(problems), [['/queue/orders', '`tags` is required']]);
});

test('a call with no known map, or of a function, is not checked', () => {
  const table = mergeMacroTable('deep');
  const known = placedMap(3, [['retention', Value.string('forever', placedAt(3))]]);
  const unknownMerge = new Value('call', call('mock-merge', [declarationReference()]), position);
  const functionCall = new Value('call', call('mock-function', [known]), position);
  const document = map([['queue', map([['orders', unknownMerge], ['payments', functionCall]])]]);
  assert.deepStrictEqual(strictQueueSchema(table).problems(document, noRules), []);
});

test('merge behaviors merge nested maps, replace values whole, or take each key once', () => {
  const nested = (line, key) => placedMap(line, [[key, Value.string('mock', placedAt(line))]]);
  const first = placedMap(1, [
    ['tags', nested(1, 'team')],
    ['retention', Value.string('one', placedAt(1))],
  ]);
  const second = placedMap(2, [
    ['tags', nested(2, 'owner')],
    ['retention', Value.string('two', placedAt(2))],
  ]);
  const entryLines = (entries) => entries.map((entry) => [entry.key, entry.keyPosition.line]);
  const merged = (mergeBehavior) => {
    const result = resolvedValue.mapMergeResult([first, second], mergeBehavior, placedAt(9));
    return result.value.map(({ key, keyPosition, value }) => [
      key,
      keyPosition.line,
      value.kind === 'map' ? entryLines(value.value) : value.value,
    ]);
  };
  assert.deepStrictEqual(merged('deep'), [
    ['tags', 2, [['team', 1], ['owner', 2]]],
    ['retention', 2, 'two'],
  ]);
  assert.deepStrictEqual(merged('shallow'), [
    ['tags', 2, [['owner', 2]]],
    ['retention', 2, 'two'],
  ]);
  assert.deepStrictEqual(merged('join'), [
    ['tags', 1, [['team', 1]]],
    ['retention', 1, 'one'],
  ]);
});

test('alternatives over an unknown value are reported when every form fails without it', () => {
  const schema = LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: DRAFT_2020_12,
    type: 'object',
    additionalProperties: {
      type: 'object',
      anyOf: [
        { required: ['methods'], properties: { origins: { type: 'array' } } },
        { required: ['wildcard'] },
      ],
    },
  }));
  const document = map([
    ['doors', map([['origins', declarationReference()]])],
    ['gates', map([['origins', declarationReference()], ['methods', map([])]])],
  ]);
  assert.deepStrictEqual(messagesOf(schema.problems(document, noRules)), [
    ['/doors', '`doors` matches none of its allowed forms'],
  ]);
});

test('a dependency names what each dependency of the key needs, each schema once', () => {
  const schema = LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: DRAFT_2020_12,
    $defs: {
      sized: { dependentRequired: { label: ['size'] } },
    },
    type: 'object',
    properties: {
      mock: {
        dependentRequired: { label: ['kind'] },
        allOf: [
          { $ref: '#/$defs/sized' },
          { dependentSchemas: { label: { not: { required: ['color'] } } } },
          { $ref: '#/$defs/sized' },
        ],
      },
      other: { $ref: '#/$defs/sized', dependentRequired: { label: ['shape'] } },
    },
  }));
  const document = map([['mock', map([
    ['label', Value.string('mock-label', position)],
    ['color', Value.string('mock-color', position)],
  ])]]);
  assert.deepStrictEqual(messagesOf(schema.problems(document, noRules)), [
    ['/mock/label', '`label` needs `kind`; `size`; `color` must not be set'],
  ]);
});

test('a schema note is the description verbatim, and a blank one gives none', () => {
  const schema = LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: DRAFT_2020_12,
    $defs: {
      described: { description: '[optional]  A mock form. ', anyOf: [{ type: 'integer' }] },
      blank: { description: '  ', anyOf: [{ type: 'integer' }] },
    },
    type: 'object',
    properties: { described: { $ref: '#/$defs/described' }, blank: { $ref: '#/$defs/blank' } },
  }));
  const document = map([
    ['blank', Value.string('mock', position)],
    ['described', Value.string('mock', position)],
  ]);
  assert.deepStrictEqual(
    schema.validate(document, noRules).map((diagnostic) => diagnostic.notes.schema),
    [null, '[optional]  A mock form. '],
  );
});

test('a schema with a dangling reference or a bad pattern does not load, first one named', () => {
  const refused = (schema) => {
    try {
      LoadedSchema.parse('mock.schema.json', JSON.stringify({ $schema: DRAFT_2020_12, ...schema }));
      return null;
    } catch (error) {
      return error.message;
    }
  };
  assert.strictEqual(
    refused({ properties: { first: { $ref: '#/$defs/mock-missing' }, second: { pattern: '(' } } }),
    'schema mock.schema.json: `#/$defs/mock-missing` names no node of the schema',
  );
  assert.strictEqual(
    refused({ patternProperties: { '(_mock_': {} }, properties: { second: { $ref: '#/mock' } } }),
    'schema mock.schema.json: `(_mock_` is not a pattern',
  );
  assert.strictEqual(
    refused({ properties: { pattern: { type: 'string', pattern: '[' } } }),
    'schema mock.schema.json: `[` is not a pattern',
  );
  assert.strictEqual(
    refused({
      const: { $ref: '#/mock' },
      default: { pattern: '(' },
      properties: { $ref: { $ref: '#' } },
    }),
    null,
  );
  assert.strictEqual(
    refused({ $defs: { inner: { $id: 'https://mock.example/inner.json', $ref: '#/$defs/mock' } } }),
    'schema mock.schema.json: `#/$defs/mock` names no node of the schema',
  );
  assert.strictEqual(
    refused({ $defs: { inner: {
      $id: 'https://mock.example/inner.json',
      $ref: '#/properties/size',
      properties: { size: { type: 'integer' } },
    } } }),
    null,
  );
});

test('any other failure is the library text naming the schema by its loaded name', () => {
  assert.throws(
    () => LoadedSchema.parse('mock.schema.json', JSON.stringify({
      $schema: DRAFT_2020_12,
      properties: { mock: { $ref: 'mock-other.json' } },
    })),
    {
      message: 'schema mock.schema.json: '
        + 'can\'t resolve reference mock-other.json from id mock.schema.json',
    },
  );
});

test('a schema with its own id validates, its fragments included', () => {
  const schema = LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: DRAFT_2020_12,
    $id: 'https://mock.example/mock.schema.json',
    $defs: {
      queue: { type: 'object', properties: { size: { $ref: '#/$defs/size' } } },
      size: { type: 'integer' },
    },
    properties: { queue: { $ref: '#/$defs/queue' } },
  }));
  const queue = map([['size', Value.string('mock', position)]]);
  const problems = schema.problems(map([['queue', queue]]), noRules);
  assert.deepStrictEqual(
    problems.map(({ diagnostic }) => diagnostic.notes.rule),
    ['type, at mock.schema.json#/$defs/size/type'],
  );
  assert.deepStrictEqual(messagesOf(schema.fragmentProblems('/$defs/queue', queue, noRules)), [
    ['/size', '`size` must be `integer`'],
  ]);
});

test('a dependency names what each dependency needs in the order the schema writes them', () => {
  const schema = LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: DRAFT_2020_12,
    $defs: { entry: { dependentSchemas: { label: { required: ['size'] } } } },
    properties: {
      mock: {
        $ref: '#/$defs/entry',
        dependentSchemas: { label: { not: { required: ['legacy'] } } },
        dependentRequired: { label: ['kind'] },
      },
    },
  }));
  const document = map([['mock', map([
    ['label', Value.string('mock-label', position)],
    ['kind', Value.string('mock-kind', position)],
  ])]]);
  assert.deepStrictEqual(messagesOf(schema.problems(document, noRules)), [
    ['/mock/label', '`label` needs `legacy` must not be set; `kind`; `size`'],
  ]);
});

test('a schema validates whole and by fragment under any load name', () => {
  const schemaText = (ownId) => JSON.stringify({
    $schema: DRAFT_2020_12,
    ...(ownId ? { $id: 'https://mock.example/mock.schema.json' } : {}),
    $defs: { queue: { type: 'object', properties: { size: { type: 'integer' } } } },
    properties: { queue: { $ref: '#/$defs/queue' } },
  });
  const queue = map([['size', Value.string('mock', position)]]);
  const loads = [
    ['/mock dir/mock schema.json', false],
    ['C:\\mock\\mock.schema.json', false],
    ['mock.schema.json#', false],
    ['', true],
  ];
  for (const [name, ownId] of loads) {
    const schema = LoadedSchema.parse(name, schemaText(ownId));
    const wholeRules = schema.problems(map([['queue', queue]]), noRules)
      .map(({ diagnostic }) => diagnostic.notes.rule);
    const fragmentRules = schema.fragmentProblems('/$defs/queue', queue, noRules)
      .map(({ diagnostic }) => diagnostic.notes.rule);
    const expected = [`type, at ${name}#/$defs/queue/properties/size/type`];
    assert.deepStrictEqual([wholeRules, fragmentRules], [expected, expected], name);
  }
});

test('a schema that does not compile is named by its load name alone', () => {
  const failure = (name) => {
    try {
      const schemaText = JSON.stringify({ properties: { mock: { $ref: 'mock-other.json' } } });
      LoadedSchema.parse(name, schemaText);
      return null;
    } catch (error) {
      return error.message;
    }
  };
  assert.strictEqual(
    failure('/mock dir/mock schema.json'),
    'schema /mock dir/mock schema.json: '
      + 'can\'t resolve reference mock-other.json from id /mock dir/mock schema.json',
  );
  assert.ok(!failure('mock.schema.json').includes('urn:'));
});

test('a reference inside a schema resource reads that resource for an unknown field', () => {
  const schema = LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: DRAFT_2020_12,
    properties: { mock: { $ref: '#/$defs/resource' } },
    $defs: {
      resource: {
        $id: 'https://mock.example/mock-resource.json',
        properties: { mode: { type: 'string' } },
        $ref: '#/$defs/wide',
        unevaluatedProperties: false,
        $defs: {
          wide: {
            if: { properties: { mode: { const: 'mock-wide' } } },
            then: { properties: { width: { type: 'integer' } } },
          },
        },
      },
    },
  }));
  const document = map([['mock', map([
    ['mode', declarationReference()],
    ['width', new Value('number', '5', position)],
    ['stray', new Value('number', '1', position)],
  ])]]);
  assert.deepStrictEqual(messagesOf(schema.problems(document, noRules)), [
    ['/mock/stray', '`stray` is not defined in the schema'],
  ]);
});

test('an error counts for a form only where that form reads the value it is about', () => {
  const schema = LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: DRAFT_2020_12,
    $defs: { text: { type: 'string' } },
    additionalProperties: {
      oneOf: [
        { anyOf: [{ $ref: '#/$defs/text' }] },
        {
          type: 'object',
          required: ['size', 'kind'],
          properties: { size: { type: 'string' }, kind: { $ref: '#/$defs/text' } },
        },
      ],
    },
  }));
  const document = map([
    ['sized', map([
      ['size', Value.string('mock-size', position)],
      ['kind', declarationReference()],
    ])],
    ['unsized', map([['kind', declarationReference()]])],
  ]);
  assert.deepStrictEqual(messagesOf(schema.problems(document, noRules)), [
    ['/unsized', '`unsized` matches none of its allowed forms'],
  ]);
});

test('errors in the fields and items a form reads are that form’s, not reported apart', () => {
  const tuple = { prefixItems: [{ type: 'string' }], items: { type: 'integer' } };
  const fields = {
    patternProperties: { '^x': { type: 'integer' } },
    additionalProperties: { type: 'boolean' },
  };
  const draft202012 = LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: DRAFT_2020_12,
    properties: {
      list: { anyOf: [tuple, { contains: { const: 'mock' } }] },
      record: { anyOf: [fields, { required: ['mock'] }] },
    },
  }));
  const draft07 = LoadedSchema.parse('mock.schema.json', JSON.stringify({
    $schema: 'http://json-schema.org/draft-07/schema#',
    properties: {
      list: { anyOf: [{ items: [{ type: 'string' }], additionalItems: { type: 'integer' } }] },
    },
  }));
  const document = map([
    ['list', new Value('list', [
      new Value('number', '1', position),
      Value.string('x', position),
    ], position)],
    ['record', map([
      ['xa', Value.string('mock', position)],
      ['other', new Value('number', '1', position)],
    ])],
  ]);
  assert.deepStrictEqual(messagesOf(draft202012.problems(document, noRules)), [
    ['/list', '`list` matches none of its allowed forms'],
    ['/record', '`record` matches none of its allowed forms'],
  ]);
  assert.deepStrictEqual(messagesOf(draft07.problems(document, noRules)), [
    ['/list', '`list` matches none of its allowed forms'],
  ]);
});
