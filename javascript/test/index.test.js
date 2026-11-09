'use strict';

const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const spec = require('..');
const { versionFiles } = require('../schema-versions');

const rootSchema = (file) =>
  JSON.parse(fs.readFileSync(path.join(__dirname, '..', '..', 'schemas', file), 'utf8'));

test('the package exports each schema by version, a major as its newest minor', () => {
  assert.deepStrictEqual(Object.keys(spec.yamlDslSchemas), ['1', '1.0', '1.1', '1.2', '1.3']);
  for (const minor of ['1.0', '1.1', '1.2', '1.3']) {
    assert.deepStrictEqual(spec.yamlDslSchemas[minor], rootSchema(`yaml-dsl-v${minor}.schema.json`));
  }
  assert.strictEqual(spec.yamlDslSchemas['1'], spec.yamlDslSchemas['1.3']);
  assert.deepStrictEqual(Object.keys(spec.functionTableSchemas), ['1', '1.0', '1.1']);
  for (const minor of ['1.0', '1.1']) {
    assert.deepStrictEqual(
      spec.functionTableSchemas[minor],
      rootSchema(`x-yaml-dsl-functions-v${minor}.schema.json`));
  }
  assert.strictEqual(spec.functionTableSchemas['1'], spec.functionTableSchemas['1.1']);
});

test('a major is its newest minor, and other families are left out', () => {
  const fileNames = [
    'mock-v2.0.schema.json',
    'mock-v1.10.schema.json',
    'mock-v1.2.schema.json',
    'mock-other-v1.0.schema.json',
    'mock-v1.schema.json',
    'README.md',
  ];
  assert.deepStrictEqual(versionFiles(fileNames, 'mock'), [
    ['1', 'mock-v1.10.schema.json'],
    ['1.2', 'mock-v1.2.schema.json'],
    ['1.10', 'mock-v1.10.schema.json'],
    ['2', 'mock-v2.0.schema.json'],
    ['2.0', 'mock-v2.0.schema.json'],
  ]);
});

test('a value is fenced in more backticks than it holds in a row', () => {
  assert.strictEqual(spec.codeSpan('on'), '`on`');
  assert.strictEqual(spec.codeSpan('a`b'), '``a`b``');
  assert.strictEqual(spec.codeSpan('a``b`c'), '```a``b`c```');
  assert.strictEqual(spec.codeSpan('`on'), '`` `on ``');
  assert.strictEqual(spec.codeSpan('on`'), '`` on` ``');
});
