'use strict';

const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const { readDefinitions, readDslDefinitions, templateDefinition, codeSpan } = require('..');

const TEMPLATES_CONFIG = path.join(__dirname, '..', '..', 'conformance', 'templates', 'yaml-dsl.yml');

const mockTemplateBlock = () => ({
  id: 'mock-template',
  dsl: 'mock-queues',
  file_includes: ['{config_root}/templates/**'],
  delimiters: { left: '[[', right: ']]' },
  conditional: { if: 'if', else_if: 'else if', else: 'else', end: 'end' },
  loop: { start: '^for (?<variables>\\$[a-z]+) in ', end: 'end', variable: '\\$[a-z]+' },
});

test('a template block reads with its loop patterns compiled, and one that does not compile is none', () => {
  const definition = templateDefinition.templateDefinitionFromJson(mockTemplateBlock());
  assert.ok(definition.loop.start instanceof RegExp);
  assert.deepStrictEqual(definition.file_excludes, []);
  const broken = mockTemplateBlock();
  broken.loop.variable = '(';
  assert.throws(() => templateDefinition.templateDefinitionFromJson(broken), /`loop.variable` is not a regular expression/u);
});

test('a template language repeating an earlier one\'s id is a problem at its id', () => {
  const text = fs.readFileSync(TEMPLATES_CONFIG, 'utf8');
  const second = text.slice(text.indexOf('  - id: mock-queue-templates'));
  const read = readDefinitions('yaml-dsl.yml', `${text}${second}`);
  assert.deepStrictEqual(read.problems.map((problem) => [problem.message, problem.notes.label]), [[
    `template language ${codeSpan('mock-queue-templates')} is declared more than once`,
    'an earlier template language of this file has this `id`',
  ]]);
  assert.deepStrictEqual(readDslDefinitions('yaml-dsl.yml', `${text}${second}`), read);
});

test('readDslDefinitions gives the DSLs alone, and readDefinitions the template languages beside them', () => {
  const text = fs.readFileSync(TEMPLATES_CONFIG, 'utf8');
  assert.deepStrictEqual(Object.keys(readDslDefinitions('yaml-dsl.yml', text)), ['dsls']);
  const read = readDefinitions('yaml-dsl.yml', text);
  assert.deepStrictEqual(read.templates.map((template) => template.definition.id), ['mock-queue-templates']);
});
