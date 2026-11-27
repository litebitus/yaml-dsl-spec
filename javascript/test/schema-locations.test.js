'use strict';

const assert = require('node:assert');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const {
  modelineOf,
  resolveLocation,
  urlSchemeOf,
  firstMatchingFile,
  stepMatches,
} = require('../core/schema/resolution/schema-locations');
const { FileSchemaReader } = require('../core/schema/resolution/file-schema-reader');

test('a modeline names its uri and where it starts', () => {
  const text = 'name: mock\n  #  yaml-language-server:  $schema=../mock.schema.json#/a rest\n';
  assert.deepStrictEqual(modelineOf(text), { schemaUri: '../mock.schema.json#/a', line: 2, column: 37 });
  assert.strictEqual(modelineOf('# yaml-language-server: $schema=\n'), null);
  assert.strictEqual(modelineOf('# mock: $schema=x\n'), null);
});

test('a path is resolved to its file url by forward slash steps', () => {
  const relative = resolveLocation('../schemas/./mock.schema.json#/a', '/mock/stack', '/mock/config');
  assert.strictEqual(relative.urlScheme, 'file');
  assert.strictEqual(relative.filePath, '/mock/schemas/mock.schema.json');
  assert.strictEqual(relative.schemaFragment, '/a');
  assert.strictEqual(relative.writtenLocation, '../schemas/./mock.schema.json');
  const rooted = resolveLocation('{config_root}/mock dir/m.json', '/mock/stack', '/mock/config');
  assert.strictEqual(rooted.filePath, '/mock/config/mock dir/m.json');
  assert.strictEqual(rooted.locationUrl, 'file:///mock/config/mock%20dir/m.json');
  assert.strictEqual(resolveLocation('/mock/abs.json', '/mock/stack', '/mock/config').filePath, '/mock/abs.json');
  assert.strictEqual(resolveLocation('file:///mock/a%20b.json', '/mock', '/mock').filePath, '/mock/a b.json');
  const climbing = resolveLocation('file:///mock/a/../b/./c.json', '/mock', '/mock');
  assert.strictEqual(climbing.filePath, '/mock/b/c.json');
  assert.strictEqual(climbing.locationUrl, 'file:///mock/b/c.json');
  assert.strictEqual(resolveLocation('file:///mock/a%+Fb.json', '/mock', '/mock').filePath, '/mock/a%+Fb.json');
  assert.strictEqual(resolveLocation('file:///mock/a.json?', '/mock', '/mock').locationUrl, 'file:///mock/a.json?');
});

test('the cache parameters are read out and the others kept in order', () => {
  const location = resolveLocation(
    'https://mock.example/s.json?b=1&yaml_dsl_max_age_days=7&a=2&yaml_dsl_machine_credentials=true',
    '/mock',
    '/mock',
  );
  assert.strictEqual(location.fetchUrl, 'https://mock.example/s.json?b=1&a=2');
  assert.strictEqual(location.urlScheme, 'https');
  assert.deepStrictEqual([location.maxAgeDays, location.machineCredentials], [7, true]);
  assert.strictEqual(location.cacheParameterProblem, null);
  const problem = (query) => resolveLocation(`https://mock.example/s.json?${query}`, '/mock', '/mock')
    .cacheParameterProblem;
  assert.strictEqual(problem('yaml_dsl_mock=1'), '`yaml_dsl_mock` is not a parameter of a schema URL');
  assert.strictEqual(problem('yaml_dsl_max_age_days=07'), '`yaml_dsl_max_age_days` must be a whole number of days');
  assert.strictEqual(
    problem('yaml_dsl_max_age_days=18446744073709551616'),
    '`yaml_dsl_max_age_days` must be a whole number of days',
  );
  assert.strictEqual(
    problem('yaml_dsl_machine_credentials=yes'),
    '`yaml_dsl_machine_credentials` must be `true` or `false`',
  );
});

test('a scheme is read only where a url starts with one', () => {
  assert.strictEqual(urlSchemeOf('S3://mock/key'), 's3');
  assert.strictEqual(urlSchemeOf('gs+x.y-z://mock'), 'gs+x.y-z');
  assert.strictEqual(urlSchemeOf('mock/https://x'), null);
  assert.strictEqual(urlSchemeOf('1x://mock'), null);
});

test('a star step matches any run of characters within it', () => {
  assert.ok(stepMatches('*.json', 'mock.json'));
  assert.ok(stepMatches('a*b*c', 'a-x-b-y-c'));
  assert.ok(!stepMatches('a*b*c', 'a-x-c'));
  assert.ok(!stepMatches('ab*ba', 'aba'));
  assert.ok(stepMatches('*', ''));
});

test.describe('a folder of schemas on the machine', () => {
  let scratchFolder;

  test.beforeEach(() => {
    scratchFolder = fs.mkdtempSync(path.join(os.tmpdir(), 'yaml-dsl-spec-locations-'));
    for (const folderName of ['a', 'a-c']) fs.mkdirSync(path.join(scratchFolder, folderName));
    fs.writeFileSync(path.join(scratchFolder, 'a', 'mock.json'), '{"title": "a"}');
    fs.writeFileSync(path.join(scratchFolder, 'a-c', 'mock.json'), '{"title": "a-c"}');
    fs.writeFileSync(path.join(scratchFolder, 'not-text.json'), Buffer.from([0xff, 0xfe]));
  });

  test.afterEach(() => {
    fs.rmSync(scratchFolder, { recursive: true, force: true });
  });

  test('the first file a pattern matches is taken in path order', () => {
    assert.strictEqual(
      firstMatchingFile(path.join(scratchFolder, '*', 'mock.json')),
      path.join(scratchFolder, 'a', 'mock.json'),
    );
    assert.strictEqual(firstMatchingFile(path.join(scratchFolder, 'absent', '*.json')), null);
  });

  test('the file reader reads a file and refuses a parameter', async () => {
    const reader = new FileSchemaReader();
    const fileUrl = resolveLocation(path.join(scratchFolder, 'a', 'mock.json'), '/', '/').fetchUrl;
    const read = await reader.readSchemaUrl({ fetchUrl: fileUrl, machineCredentials: false, keptCopyVersion: null });
    assert.deepStrictEqual(
      read.readOutcome,
      { outcomeKind: 'textSent', schemaText: '{"title": "a"}', copyVersion: null },
    );
    const withParameter = await reader.readSchemaUrl({ fetchUrl: `${fileUrl}?mock=1`, machineCredentials: false });
    assert.strictEqual(withParameter.readOutcome.failureReason, '`mock` is not a parameter of a `file` URL');
    const folderUrl = resolveLocation(scratchFolder, '/', '/').fetchUrl;
    const folderRead = await reader.readSchemaUrl({ fetchUrl: folderUrl, machineCredentials: false });
    assert.deepStrictEqual(folderRead.readOutcome, { outcomeKind: 'sourceMissing', missingReason: 'no file is there' });
    const notTextUrl = resolveLocation(path.join(scratchFolder, 'not-text.json'), '/', '/').fetchUrl;
    const notText = await reader.readSchemaUrl({ fetchUrl: notTextUrl, machineCredentials: false });
    assert.strictEqual(notText.readOutcome.outcomeKind, 'readFailed');
  });
});
