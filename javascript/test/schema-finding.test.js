'use strict';

const assert = require('node:assert');
const test = require('node:test');

const { schemaValidation, schemaResolution, Position } = require('..');
const { copyKeyOf, MACHINE_CREDENTIALS_HELP } = require('../core/schema/resolution/schema-finding');

const { SchemaCache } = schemaValidation;
const { answerOf, refusedAnswer, failedAnswer, missingAnswer } = schemaResolution;

const MOCK_URL = 'mock://mock-host/mock.schema.json';
const KEPT_BODY = '{"title": "kept"}';

/** A store in memory. */
function mockStore() {
  const entries = new Map();
  return {
    entries,
    read: (key) => (entries.has(key) ? entries.get(key) : null),
    write: (key, prepared) => entries.set(key, prepared),
  };
}

/**
 * A reader of `mock://` URLs answering what a test scripts: one answer for a read
 * without credentials and one for a read with them, each request kept.
 */
function scriptedReader(withoutCredentials, withCredentials) {
  return {
    urlPrefixes: ['mock://'],
    readsRemoteSources: true,
    requests: [],
    async readSchemaUrl(request) {
      this.requests.push(request);
      return request.machineCredentials ? withCredentials : withoutCredentials;
    },
  };
}

function sendingReader(schemaText) {
  const sent = answerOf({ outcomeKind: 'textSent', schemaText, copyVersion: { mock_version: 1 } });
  return scriptedReader(sent, sent);
}

function mockPosition() {
  return new Position('mock-tfgen.yml', 4, 7);
}

function mockRefusal(credentialOrigin, renewalFix) {
  return { refusedBy: 'mock-host', credentialOrigin, refusalReason: '403', renewalFix };
}

function keep(store, fetchUrl, confirmedAt) {
  const keptCopy = { url: fetchUrl, copy_version: { mock_version: 0 }, confirmed_at: confirmedAt, body: KEPT_BODY };
  store.write(copyKeyOf(fetchUrl), JSON.stringify(keptCopy));
}

test('a remote text is kept as a copy with its version and read back within its max age', async () => {
  const reader = sendingReader('{}');
  const store = mockStore();
  const cache = new SchemaCache({ store, urlReaders: [reader] });
  const location = `${MOCK_URL}?b=1&yaml_dsl_max_age_days=3`;
  const first = await cache.readSchema(location, mockPosition());
  assert.strictEqual(first.foundSchema.schemaText, '{}');
  const keptCopy = JSON.parse(store.read(copyKeyOf(`${MOCK_URL}?b=1`)));
  assert.deepStrictEqual(Object.keys(keptCopy), ['url', 'copy_version', 'confirmed_at', 'body']);
  assert.deepStrictEqual(keptCopy.copy_version, { mock_version: 1 });
  assert.strictEqual(keptCopy.url, `${MOCK_URL}?b=1`);
  const second = await cache.readSchema(location, mockPosition());
  assert.strictEqual(second.foundSchema.schemaName, location);
  assert.deepStrictEqual(second.triedCandidates[0].namedBy, { namerKind: 'caller' });
  assert.strictEqual(reader.requests.length, 1);
});

test('a copy past its max age is asked about with its version and a confirmation keeps it', async () => {
  const confirmed = answerOf({ outcomeKind: 'copyConfirmed' });
  const reader = scriptedReader(confirmed, confirmed);
  const store = mockStore();
  keep(store, MOCK_URL, 0);
  const finding = await new SchemaCache({ store, urlReaders: [reader] }).readSchema(MOCK_URL, null);
  assert.strictEqual(finding.foundSchema.schemaText, KEPT_BODY);
  assert.deepStrictEqual(reader.requests[0].keptCopyVersion, { mock_version: 0 });
  assert.ok(reader.requests[0].signal instanceof AbortSignal);
  assert.ok(JSON.parse(store.read(copyKeyOf(MOCK_URL))).confirmed_at > 0);
});

test('a confirmation of a copy never kept fails the read', async () => {
  const confirmed = answerOf({ outcomeKind: 'copyConfirmed' });
  const cache = new SchemaCache({ urlReaders: [scriptedReader(confirmed, confirmed)] });
  const finding = await cache.readSchema(MOCK_URL, null);
  assert.strictEqual(finding.findingProblems[0].message, `reading \`${MOCK_URL}\` failed`);
  assert.strictEqual(finding.findingProblems[0].notes.label, 'the reader confirmed a copy never kept');
});

test('a refused read is asked again with credentials only where the url opts in', async () => {
  const refused = refusedAnswer([mockRefusal(null, null)]);
  const sent = {
    readOutcome: { outcomeKind: 'textSent', schemaText: '{}', copyVersion: null },
    readRefusals: [mockRefusal('`MOCK_TOKEN`', 'replace `MOCK_TOKEN`')],
  };
  const reader = scriptedReader(refused, sent);
  const cache = new SchemaCache({ store: mockStore(), urlReaders: [reader] });
  const without = await cache.readSchema(MOCK_URL, mockPosition());
  assert.strictEqual(without.foundSchema, null);
  const [problem] = without.findingProblems;
  assert.strictEqual(problem.message, `mock-host refused to read \`${MOCK_URL}\``);
  assert.strictEqual(problem.notes.label, '403');
  assert.strictEqual(problem.notes.help, MACHINE_CREDENTIALS_HELP);
  const optedIn = `${MOCK_URL}?yaml_dsl_machine_credentials=true`;
  const withCredentials = await cache.readSchema(optedIn, mockPosition());
  assert.strictEqual(withCredentials.foundSchema.schemaText, '{}');
  const [warning] = withCredentials.findingProblems;
  assert.strictEqual(warning.severity, 'warning');
  assert.strictEqual(warning.message, 'mock-host refused `MOCK_TOKEN`');
  assert.strictEqual(warning.notes.help, 'replace `MOCK_TOKEN`');
  assert.deepStrictEqual(reader.requests.map((request) => request.machineCredentials), [false, false, true]);
});

test('every credential refused reads the kept copy with a warning else ends the finding', async () => {
  const refused = refusedAnswer([
    mockRefusal('`MOCK_TOKEN`', 'replace `MOCK_TOKEN`'),
    mockRefusal('`OTHER_TOKEN`', 'replace `MOCK_TOKEN`'),
  ]);
  const reader = scriptedReader(refused, refused);
  const optedIn = `${MOCK_URL}?yaml_dsl_machine_credentials=true`;
  const store = mockStore();
  const unkept = await new SchemaCache({ store, urlReaders: [reader] }).readSchema(optedIn, mockPosition());
  assert.strictEqual(unkept.findingProblems[0].notes.label, '`MOCK_TOKEN`: 403, `OTHER_TOKEN`: 403');
  assert.strictEqual(unkept.findingProblems[0].notes.help, 'replace `MOCK_TOKEN`');
  keep(store, MOCK_URL, 0);
  const kept = await new SchemaCache({ store, urlReaders: [reader] }).readSchema(optedIn, mockPosition());
  assert.strictEqual(kept.foundSchema.schemaText, KEPT_BODY);
  const [warning] = kept.findingProblems;
  assert.strictEqual(warning.severity, 'warning');
  assert.strictEqual(warning.message, `mock-host refused to read \`${optedIn}\`, so its kept copy was read`);
  assert.deepStrictEqual(warning.position, mockPosition());
});

test('a failed read gives the kept copy and a missing source never does', async () => {
  const failed = failedAnswer('mock failure');
  const failing = scriptedReader(failed, failed);
  const store = mockStore();
  const unkept = await new SchemaCache({ store, urlReaders: [failing] }).readSchema(MOCK_URL);
  assert.strictEqual(unkept.findingProblems[0].message, `reading \`${MOCK_URL}\` failed`);
  assert.strictEqual(unkept.findingProblems[0].position, null);
  keep(store, MOCK_URL, 0);
  const kept = await new SchemaCache({ store, urlReaders: [failing] }).readSchema(MOCK_URL, null);
  assert.ok(kept.foundSchema !== null);
  assert.deepStrictEqual(kept.findingProblems, []);
  const missing = missingAnswer('mock reason');
  const absent = scriptedReader(missing, missing);
  const finding = await new SchemaCache({ store, urlReaders: [absent] }).readSchema(MOCK_URL, mockPosition());
  assert.strictEqual(finding.foundSchema, null);
  const [problem] = finding.findingProblems;
  assert.strictEqual(problem.message, `\`${MOCK_URL}\` is missing`);
  assert.strictEqual(problem.notes.label, 'mock reason');
  assert.deepStrictEqual(problem.position, mockPosition());
});

test('a kept copy that does not read is fetched again', async () => {
  const reader = sendingReader('{}');
  const store = mockStore();
  store.write(copyKeyOf(MOCK_URL), 'not a copy');
  const cache = new SchemaCache({ store, urlReaders: [reader] });
  const finding = await cache.readSchema(`${MOCK_URL}?yaml_dsl_max_age_days=1`);
  assert.strictEqual(finding.foundSchema.schemaText, '{}');
  assert.strictEqual(reader.requests[0].keptCopyVersion, null);
});

test('a cache without a store asks the reader on every read', async () => {
  const reader = sendingReader('{}');
  const cache = new SchemaCache({ urlReaders: [reader] });
  const location = `${MOCK_URL}?yaml_dsl_max_age_days=3`;
  await cache.readSchema(location, null);
  await cache.readSchema(location, null);
  assert.strictEqual(reader.requests.length, 2);
});

test('reads of one url at once fetch it once and the others read its copy', async () => {
  const reader = sendingReader('{}');
  const cache = new SchemaCache({ store: mockStore(), urlReaders: [reader] });
  const location = `${MOCK_URL}?yaml_dsl_max_age_days=1`;
  const findings = await Promise.all([0, 1, 2, 3].map(() => cache.readSchema(location, null)));
  assert.ok(findings.every((finding) => finding.foundSchema !== null));
  assert.strictEqual(reader.requests.length, 1);
  assert.strictEqual(cache.readTurns.size, 0);
});

test('a url is read by the reader of its longest prefix', async () => {
  const prefixReader = (urlPrefixes) => ({
    urlPrefixes,
    readsRemoteSources: true,
    readSchemaUrl: async () => missingAnswer(urlPrefixes[0]),
  });
  const cache = new SchemaCache({ urlReaders: [prefixReader(['https://mock.example/releases/'])] });
  const finding = await cache.readSchema('HTTPS://mock.example/releases/a.json', null);
  assert.strictEqual(finding.findingProblems[0].notes.label, 'https://mock.example/releases/');
  assert.throws(
    () => new SchemaCache({ urlReaders: [prefixReader(['HTTPS://'])] }),
    { message: 'a reader of `HTTPS://` URLs is already registered' },
  );
});

test('a reader on the machine is read each time and its refusal ends the finding', async () => {
  const refused = refusedAnswer([mockRefusal(null, null)]);
  const localReader = { ...scriptedReader(refused, refused), readsRemoteSources: false };
  const cache = new SchemaCache({ store: mockStore(), urlReaders: [localReader] });
  const finding = await cache.readSchema(MOCK_URL, mockPosition());
  assert.strictEqual(finding.findingProblems[0].message, `mock-host refused to read \`${MOCK_URL}\``);
  assert.strictEqual(finding.findingProblems[0].notes.help, MACHINE_CREDENTIALS_HELP);
  const confirmed = answerOf({ outcomeKind: 'copyConfirmed' });
  const confirmingReader = { ...scriptedReader(confirmed, confirmed), readsRemoteSources: false };
  const confirmedFinding = await new SchemaCache({ urlReaders: [confirmingReader] }).readSchema(MOCK_URL);
  assert.strictEqual(confirmedFinding.findingProblems[0].notes.label, 'the reader confirmed a copy never kept');
});
