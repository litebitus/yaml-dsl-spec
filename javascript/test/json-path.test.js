'use strict';

const assert = require('node:assert');
const test = require('node:test');

const { jsonPath, resolved, Position } = require('..');

const { PathPattern } = jsonPath;
const { Value } = resolved;

function path(steps) {
  return steps.map((step) => (/^\d+$/.test(step) ? { index: Number(step) } : { key: step }));
}

test('a name and a wildcard select one level each', () => {
  const entries = PathPattern.parse('$.*.*', ['locals']);
  assert.ok(entries.selects(path(['queue', 'orders'])));
  assert.ok(!entries.selects(path(['locals', 'defaults'])));
  assert.ok(!entries.selects(path(['queue'])));
  assert.ok(PathPattern.parse('$.locals.*').selects(path(['locals', 'defaults'])));
});

test('a descendant step selects at any depth', () => {
  const transform = PathPattern.parse("$..['Fn::Transform']");
  assert.ok(transform.selects(path(['Resources', 'Bucket', 'Fn::Transform'])));
  const below = PathPattern.parse('$.cloud..*');
  assert.ok(below.selects(path(['cloud', 'aws_regions', '0'])));
  assert.ok(!below.selects(path(['cloud'])));
});

test('list items are selected by the bracket wildcard', () => {
  assert.ok(PathPattern.parse('$.cases[*].steps[*].*').selects(path(['cases', '0', 'steps', '2', 'call'])));
});

test('select answers every selected node in document order', () => {
  const position = Position.fileStart('mock.yml');
  const entry = (key, value) => ({ key, keyPosition: position, value });
  const leaf = (text) => Value.string(text, position);
  const map = (entries) => new Value('map', entries, position);
  const document = map([
    entry('queue', map([entry('b', leaf('1')), entry('a', leaf('2'))])),
    entry('locals', map([entry('c', leaf('3'))])),
  ]);
  const selected = PathPattern.parse('$.*.*', ['locals']).select(document).map((found) => found.path);
  assert.deepStrictEqual(selected, [path(['queue', 'b']), path(['queue', 'a'])]);
});

test('a path is read only in the subset the schema states', () => {
  for (const text of ['$.a', '$.*', '$[*]', "$['a.b']", '$..a', "$..['a']", '$.a..*', '$.a*b']) {
    assert.doesNotThrow(() => PathPattern.parse(text), text);
  }
  const refused = [
    '$abc',
    '$*',
    "$.a.['b']",
    '$..[*]',
    '$..*.a',
    '$.*a',
    "$['']",
    "$['a'b']",
    '$["a"]',
    '$.',
  ];
  for (const text of refused) assert.throws(() => PathPattern.parse(text), text);
});

test('the root selects the document', () => {
  assert.ok(PathPattern.parse('$').selects([]));
  assert.ok(PathPattern.parse('$').covers(path(['any', 'thing'])));
});

test('a path beyond the subset is refused with why', () => {
  assert.throws(() => PathPattern.parse('queue.*'), { message: '`queue.*` is not a path of the supported JSONPath subset' });
  assert.throws(() => PathPattern.parse('$..*.queue'), { message: '`$..*.queue` is not a path of the supported JSONPath subset' });
});
