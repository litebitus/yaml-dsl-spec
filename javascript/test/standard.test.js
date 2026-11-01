'use strict';

const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const Ajv2020 = require('ajv/dist/2020').default;

const spec = require('..');

const FIXTURES = path.join(__dirname, '..', '..', 'rust', 'tests', 'fixtures');
const EXAMPLES = path.join(__dirname, '..', '..', 'examples');

const fixture = (name) => JSON.parse(fs.readFileSync(path.join(FIXTURES, name), 'utf8'));

function errors(schema, instance) {
  const validate = new Ajv2020({ allErrors: true, strict: false, validateFormats: false }).compile(schema);
  return validate(instance) ? [] : validate.errors.map((error) => `${error.instancePath} ${error.message}`);
}

test('a major stands for its newest minor', () => {
  assert.deepStrictEqual(spec.YAML_DSL_VERSIONS, ['1', '1.0']);
  assert.deepStrictEqual(spec.FUNCTION_TABLE_VERSIONS, ['1', '1.0']);
  assert.strictEqual(spec.yamlDslSchema('1'), spec.yamlDslSchema('1.0'));
  assert.strictEqual(spec.yamlDslSchema('1.0').title, 'yaml-dsl.yml v1.0');
  assert.strictEqual(spec.yamlDslSchema('2'), null);
  assert.strictEqual(spec.functionTableSchema('1'), spec.functionTableSchema('1.0'));
});

test('a minor schema takes a file stating its minor or its major', () => {
  const config = fixture('mock-dsl.json');
  for (const version of ['1', '1.0']) {
    assert.deepStrictEqual(errors(spec.yamlDslSchema('1.0'), { ...config, version }), []);
  }
  assert.notDeepStrictEqual(errors(spec.yamlDslSchema('1.0'), { ...config, version: '1.1' }), []);
});

test('a config the schema accepts reads into its definitions', () => {
  const config = fixture('mock-dsl.json');
  assert.deepStrictEqual(errors(spec.yamlDslSchema(config.version), config), []);
  const definition = spec.dslDefinition.dslDefinitionFromJson(config.dsls[0]);
  assert.strictEqual(definition.id, 'mock-dsl');
  assert.strictEqual(definition.layers.common_layer_discovery, 'ancestor');
  assert.strictEqual(definition.declarations[1].name_spelling, 'dashes_as_underscores');
});

test('the schema reports a misspelt field the definition would ignore', () => {
  const config = fixture('mock-dsl.json');
  const { file_excludes: excludes, ...block } = config.dsls[0];
  const misspelt = { ...config, dsls: [{ ...block, file_exclude: excludes }] };
  assert.ok(errors(spec.yamlDslSchema('1'), misspelt).some((error) => error.includes('additional')));
  assert.doesNotThrow(() => spec.dslDefinition.dslDefinitionFromJson(misspelt.dsls[0]));
});

test('a function table the schema accepts', () => {
  assert.deepStrictEqual(errors(spec.functionTableSchema('1'), fixture('mock-functions.json')), []);
});

function exampleFolders() {
  return fs.readdirSync(EXAMPLES)
    .map((name) => path.join(EXAMPLES, name))
    .filter((folder) => fs.existsSync(path.join(folder, 'yaml-dsl.yml')));
}

test('every function table an example publishes is valid', () => {
  for (const folder of exampleFolders()) {
    for (const name of fs.readdirSync(folder).filter((file) => file.endsWith('.schema.json'))) {
      const schema = JSON.parse(fs.readFileSync(path.join(folder, name), 'utf8'));
      if (schema['x-yaml-dsl-functions'] === undefined) continue;
      assert.deepStrictEqual(errors(spec.functionTableSchema('1'), schema['x-yaml-dsl-functions']), [], name);
    }
  }
});

test('every example reads to validated definitions', () => {
  const folders = exampleFolders();
  assert.ok(folders.length > 0);
  for (const folder of folders) {
    const file = path.join(folder, 'yaml-dsl.yml');
    const read = spec.readDslDefinitions(file, fs.readFileSync(file, 'utf8'));
    assert.ok(read.dsls && read.dsls.length > 0, `${folder}: ${(read.problems || []).join('\n')}`);
  }
});
