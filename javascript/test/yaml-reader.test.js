'use strict';

const assert = require('node:assert');
const test = require('node:test');

const { yamlReader } = require('..');

const parse = (text, tagKeys = []) => yamlReader.parseYaml('mock.yml', text, tagKeys);

test('keeps key order and positions', () => {
  const { node } = parse('zeta: 1\nalpha:\n  nested: true\n');
  const entries = node.entries();
  assert.strictEqual(entries[0].key, 'zeta');
  assert.strictEqual(entries[1].key, 'alpha');
  assert.strictEqual(entries[1].keyPosition.line, 2);
  const nested = entries[1].value.get('nested');
  assert.deepStrictEqual([nested.kind, nested.value], ['bool', true]);
  assert.strictEqual(nested.position.column, 11);
});

test('reads plain scalars by the core schema', () => {
  const { node } = parse('a: 30\nb: 1.5\nc: "30"\nd: ~\ne: mock\n');
  const read = (key) => [node.get(key).kind, node.get(key).value];
  assert.deepStrictEqual(read('a'), ['number', '30']);
  assert.deepStrictEqual(read('b'), ['number', '1.5']);
  assert.deepStrictEqual(read('c'), ['string', '30']);
  assert.deepStrictEqual(read('d'), ['null', null]);
  assert.deepStrictEqual(read('e'), ['string', 'mock']);
});

test('a listed tag reads as a map holding its key', () => {
  const { node } = parse('bucket: !Ref MockBucket\n', [{ tag: '!Ref', key: 'Ref' }]);
  assert.strictEqual(node.get('bucket').get('Ref').value, 'MockBucket');
});

test('an alias reads as its anchor', () => {
  const { node } = parse('a: &shared {x: 1}\nb: *shared\n');
  assert.deepStrictEqual(node.get('b').get('x').value, '1');
});

test('a duplicate key is refused', () => {
  const { problem } = parse('a: 1\na: 2\n');
  assert.strictEqual(problem.message, 'duplicate key `a`');
  assert.strictEqual(problem.position.line, 2);
});
