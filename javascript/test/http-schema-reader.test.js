'use strict';

const assert = require('node:assert');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const { schemaResolution } = require('..');
const { HttpSchemaReader } = require('../core/schema/resolution/http-schema-reader');
const {
  HostLogin, MachineCredentials, netrcEntry, hostLoginOf,
} = require('../core/schema/resolution/machine-credentials');

const MOCK_SCHEMA = '{"type": "object"}';
const MOCK_ETAG = '"mock-etag"';
const MOCK_LAST_MODIFIED = 'Wed, 01 Jan 2026 00:00:00 GMT';
const MOCK_LOGIN = 'Basic bW9jay11c2VyOm1vY2stc2VjcmV0';

/**
 * A server answering `/schema.json` with the schema, or `304` to a request
 * carrying its ETag; `/private.json` only to the mock login; `/broken.json` with
 * `500`; `/moved.json` by redirecting to `/landing.json`, which answers only a
 * request without a login; and anything else with `404`.
 */
function mockServer() {
  return http.createServer((request, response) => {
    if (request.url === '/schema.json' && request.headers['if-none-match'] === MOCK_ETAG) {
      response.writeHead(304);
    } else if (request.url === '/schema.json') {
      response.writeHead(200, { ETag: MOCK_ETAG, 'Last-Modified': MOCK_LAST_MODIFIED });
      response.write(MOCK_SCHEMA);
    } else if (request.url === '/private.json' && request.headers.authorization === MOCK_LOGIN) {
      response.writeHead(200);
      response.write(MOCK_SCHEMA);
    } else if (request.url === '/private.json') {
      response.writeHead(401);
    } else if (request.url === '/broken.json') {
      response.writeHead(500);
    } else if (request.url === '/moved.json') {
      response.writeHead(302, { Location: '/landing.json' });
    } else if (request.url === '/landing.json' && request.headers.authorization === undefined) {
      response.writeHead(200);
      response.write(MOCK_SCHEMA);
    } else if (request.url === '/landing.json') {
      response.writeHead(400);
    } else {
      response.writeHead(404);
    }
    response.end();
  });
}

/** A login a test names in place of the machine's `.netrc`, counting how often the credentials are forgotten. */
function givenLogin(login) {
  return {
    forgottenCount: 0,
    hostLogin: () => login,
    forgetCredentials() {
      this.forgottenCount += 1;
    },
  };
}

function mockLogin(loginPassword) {
  return new HostLogin({
    credentialOrigin: 'the `.netrc` entry for 127.0.0.1',
    loginName: 'mock-user',
    loginPassword,
    renewalFix: 'update the `.netrc` entry for 127.0.0.1',
  });
}

function mockRequest(fetchUrl, machineCredentials, keptCopyVersion = null) {
  return { fetchUrl, machineCredentials, keptCopyVersion, signal: AbortSignal.timeout(4000) };
}

test.describe('the http reader', () => {
  let server;
  let serverAddress;

  test.beforeEach(async () => {
    server = mockServer();
    await new Promise((resolve) => {
      server.listen(0, '127.0.0.1', resolve);
    });
    serverAddress = `http://127.0.0.1:${server.address().port}`;
  });

  test.afterEach(() => {
    server.closeAllConnections();
    server.close();
  });

  test('a link is sent with its version and a current copy is confirmed', async () => {
    const reader = new HttpSchemaReader(givenLogin(null));
    assert.deepStrictEqual(reader.urlPrefixes, ['http://', 'https://']);
    const sent = await reader.readSchemaUrl(mockRequest(`${serverAddress}/schema.json`, false));
    assert.strictEqual(sent.readOutcome.schemaText, MOCK_SCHEMA);
    assert.deepStrictEqual(sent.readOutcome.copyVersion, { etag: MOCK_ETAG, last_modified: MOCK_LAST_MODIFIED });
    const confirmed = await reader.readSchemaUrl(
      mockRequest(`${serverAddress}/schema.json`, false, sent.readOutcome.copyVersion),
    );
    assert.deepStrictEqual(confirmed.readOutcome, { outcomeKind: 'copyConfirmed' });
    assert.deepStrictEqual(
      schemaResolution.httpConditionalHeaders({ etag: null, last_modified: MOCK_LAST_MODIFIED }),
      { 'If-Modified-Since': MOCK_LAST_MODIFIED },
    );
    assert.deepStrictEqual(schemaResolution.httpConditionalHeaders('mock-unread-version'), {});
  });

  test('a 404 is missing and another status or no answer fails', async () => {
    const reader = new HttpSchemaReader(givenLogin(null));
    const missing = await reader.readSchemaUrl(mockRequest(`${serverAddress}/absent.json`, false));
    assert.deepStrictEqual(
      missing.readOutcome,
      { outcomeKind: 'sourceMissing', missingReason: 'the server answered 404' },
    );
    const broken = await reader.readSchemaUrl(mockRequest(`${serverAddress}/broken.json`, false));
    assert.deepStrictEqual(broken.readOutcome, { outcomeKind: 'readFailed', failureReason: 'the server answered 500' });
    const unanswered = await reader.readSchemaUrl(mockRequest('http://127.0.0.1:9/schema.json', false));
    assert.strictEqual(unanswered.readOutcome.outcomeKind, 'readFailed');
  });

  test('a refused link is read with the netrc login of its host', async () => {
    const refused = await new HttpSchemaReader(givenLogin(null))
      .readSchemaUrl(mockRequest(`${serverAddress}/private.json`, false));
    assert.strictEqual(refused.readOutcome.outcomeKind, 'readRefused');
    assert.strictEqual(refused.readRefusals[0].refusalReason, '401');
    assert.strictEqual(refused.readRefusals[0].credentialOrigin, null);
    const sent = await new HttpSchemaReader(givenLogin(mockLogin('mock-secret')))
      .readSchemaUrl(mockRequest(`${serverAddress}/private.json`, true));
    assert.strictEqual(sent.readOutcome.outcomeKind, 'textSent');
  });

  test('a redirect is followed without the login', async () => {
    const moved = await new HttpSchemaReader(givenLogin(mockLogin('mock-secret')))
      .readSchemaUrl(mockRequest(`${serverAddress}/moved.json`, true));
    assert.strictEqual(moved.readOutcome.outcomeKind, 'textSent', JSON.stringify(moved));
  });

  test('a refused login is listed and the credentials read again next time', async () => {
    const credentials = givenLogin(mockLogin('mock-stale'));
    const refused = await new HttpSchemaReader(credentials)
      .readSchemaUrl(mockRequest(`${serverAddress}/private.json`, true));
    assert.strictEqual(refused.readOutcome.outcomeKind, 'readRefused');
    assert.deepStrictEqual(refused.readRefusals, [{
      refusedBy: '127.0.0.1',
      credentialOrigin: 'the `.netrc` entry for 127.0.0.1',
      refusalReason: '401',
      renewalFix: 'update the `.netrc` entry for 127.0.0.1',
    }]);
    assert.strictEqual(credentials.forgottenCount, 1);
    const unset = await new HttpSchemaReader(givenLogin(null))
      .readSchemaUrl(mockRequest(`${serverAddress}/private.json`, true));
    assert.strictEqual(unset.readRefusals[0].renewalFix, 'add a `.netrc` entry for 127.0.0.1');
  });
});

test('a netrc gives a host its machine entry else the default', () => {
  const text = [
    'machine mock.example login mock-user password mock-secret',
    'machine api.mock.example\n  login mock-owner\n  password mock-token',
    'macdef init',
    'default login anyone account mock password any-secret',
  ].join('\n');
  assert.deepStrictEqual(
    netrcEntry(text, 'api.mock.example'),
    { loginName: 'mock-owner', loginPassword: 'mock-token' },
  );
  assert.deepStrictEqual(netrcEntry(text, 'other.example'), { loginName: 'anyone', loginPassword: 'any-secret' });
  assert.strictEqual(netrcEntry('machine a.example login x password y', 'b.example'), null);
  const login = hostLoginOf(text, 'api.mock.example');
  assert.strictEqual(login.credentialOrigin, 'the `.netrc` entry for api.mock.example');
  assert.strictEqual(login.renewalFix, 'update the `.netrc` entry for api.mock.example');
});

test('a login is sent base64 encoded', () => {
  assert.strictEqual(mockLogin('mock-secret').authorization(), MOCK_LOGIN);
});

test.describe('the machine netrc', () => {
  let scratchFolder;
  let netrcBefore;

  test.beforeEach(() => {
    scratchFolder = fs.mkdtempSync(path.join(os.tmpdir(), 'yaml-dsl-spec-netrc-'));
    netrcBefore = process.env.NETRC;
    process.env.NETRC = path.join(scratchFolder, '.netrc');
  });

  test.afterEach(() => {
    if (netrcBefore === undefined) delete process.env.NETRC;
    else process.env.NETRC = netrcBefore;
    fs.rmSync(scratchFolder, { recursive: true, force: true });
  });

  test('the file NETRC names is read, and read again once forgotten', () => {
    const credentials = new MachineCredentials();
    assert.strictEqual(credentials.hostLogin('mock.example'), null);
    assert.strictEqual(schemaResolution.netrcLogin('mock.example'), null);
    fs.writeFileSync(process.env.NETRC, 'machine mock.example login mock-user password mock-secret\n');
    assert.strictEqual(credentials.hostLogin('mock.example'), null);
    credentials.forgetCredentials();
    assert.strictEqual(credentials.hostLogin('mock.example').loginName, 'mock-user');
    assert.strictEqual(schemaResolution.netrcLogin('mock.example').authorization(), MOCK_LOGIN);
  });
});
