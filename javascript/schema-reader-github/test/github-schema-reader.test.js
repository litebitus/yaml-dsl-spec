'use strict';

const assert = require('node:assert');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const { GithubSchemaReader } = require('..');
const {
  MachineTokens, githubSchemaReaderAt, githubReleaseOf, NO_TOKEN_FIX,
} = require('../lib/github-schema-reader');

const MOCK_SCHEMA = '{"type": "object"}';
const GOOD_TOKEN = 'Bearer mock-good-token';
const BLIND_TOKEN = 'Bearer mock-blind-token';
const PRIVATE_RELEASE = 'mock-org/mock-repo/releases/download/mock/v1.0/mock.schema.json';
const PUBLIC_RELEASE = 'mock-org/public-repo/releases/download/v1/mock.schema.json';

/**
 * GitHub for one public and one private repository: its pages answer the public
 * release and 404 for the private one, and its API lets the good token see the
 * private release, by tag and as the latest, whose asset redirects to a file
 * store that refuses any request still carrying a token. The blind token sees nothing.
 */
function mockGithub() {
  const server = http.createServer((request, response) => {
    const { authorization } = request.headers;
    const githubApi = `http://127.0.0.1:${server.address().port}`;
    const answer = (status, body = '', headers = {}) => {
      response.writeHead(status, headers);
      response.end(body);
    };
    const assets = JSON.stringify({ assets: [{ name: 'mock.schema.json', url: `${githubApi}/assets/7` }] });
    const requestPath = request.url;
    if (requestPath.startsWith('/mock-org/public-repo/')) return answer(200, MOCK_SCHEMA);
    if (requestPath.startsWith('/mock-org/')) return answer(404);
    if (requestPath === '/stored/7') return authorization === undefined ? answer(200, MOCK_SCHEMA) : answer(400);
    if (authorization === BLIND_TOKEN) return answer(404);
    if (authorization !== GOOD_TOKEN) return answer(401);
    if (requestPath === '/repos/mock-org/mock-repo') return answer(200, '{}');
    if (['/repos/mock-org/mock-repo/releases/tags/mock%2Fv1.0', '/repos/mock-org/mock-repo/releases/latest']
      .includes(requestPath)) {
      return answer(200, assets);
    }
    if (requestPath === '/assets/7') return answer(302, '', { Location: `${githubApi}/stored/7` });
    return answer(404);
  });
  return server;
}

/** Tokens a test names in place of the machine's, counting how often they are forgotten. */
function givenTokens(tokens) {
  return {
    forgottenCount: 0,
    githubTokens: async () => tokens,
    forgetTokens() {
      this.forgottenCount += 1;
    },
  };
}

function mockToken(variableName, authorization, renewalFix) {
  return { credentialOrigin: `\`${variableName}\``, authorization, renewalFix };
}

function staleToken() {
  return mockToken('GH_TOKEN', 'Bearer mock-stale-token', 'replace `GH_TOKEN`');
}

function goodToken() {
  return mockToken('gh auth token', GOOD_TOKEN, 'run `gh auth login`');
}

function mockRequest(fetchUrl, machineCredentials) {
  return { fetchUrl, machineCredentials, keptCopyVersion: null, signal: AbortSignal.timeout(4000) };
}

test.describe("the machine's tokens", () => {
  const variableNames = ['GH_TOKEN', 'GITHUB_TOKEN', 'NETRC', 'PATH'];
  let variablesBefore;
  let scratchFolder;

  test.beforeEach(() => {
    variablesBefore = Object.fromEntries(
      variableNames.map((variableName) => [variableName, process.env[variableName]]),
    );
    scratchFolder = fs.mkdtempSync(path.join(os.tmpdir(), 'yaml-dsl-spec-github-tokens-'));
    process.env.PATH = scratchFolder;
    process.env.NETRC = path.join(scratchFolder, '.netrc');
    delete process.env.GH_TOKEN;
    delete process.env.GITHUB_TOKEN;
  });

  test.afterEach(() => {
    for (const [variableName, valueBefore] of Object.entries(variablesBefore)) {
      if (valueBefore === undefined) delete process.env[variableName];
      else process.env[variableName] = valueBefore;
    }
    fs.rmSync(scratchFolder, { recursive: true, force: true });
  });

  test('none are read where the machine has none', async () => {
    assert.deepStrictEqual(await new MachineTokens().githubTokens(), []);
  });

  test('each token is read in order, and read again once forgotten', async () => {
    process.env.GH_TOKEN = 'mock-gh-token';
    process.env.GITHUB_TOKEN = 'mock-github-token';
    const ghScript = path.join(scratchFolder, 'gh');
    fs.writeFileSync(ghScript, '#!/bin/sh\necho mock-login-token\n', { mode: 0o755 });
    fs.writeFileSync(process.env.NETRC, 'machine api.github.com login mock-user password mock-secret\n');
    const machineTokens = new MachineTokens();
    const tokens = await machineTokens.githubTokens();
    assert.deepStrictEqual(tokens.map((token) => token.credentialOrigin), [
      '`GH_TOKEN`',
      '`GITHUB_TOKEN`',
      '`gh auth token`',
      'the `.netrc` entry for api.github.com',
    ]);
    assert.deepStrictEqual(tokens.map((token) => token.authorization), [
      'Bearer mock-gh-token',
      'Bearer mock-github-token',
      'Bearer mock-login-token',
      'Basic bW9jay11c2VyOm1vY2stc2VjcmV0',
    ]);
    delete process.env.GH_TOKEN;
    assert.strictEqual((await machineTokens.githubTokens()).length, 4);
    machineTokens.forgetTokens();
    assert.strictEqual((await machineTokens.githubTokens()).length, 3);
  });
});

test('a link names its repository and the release asset its path reads', () => {
  const tagged = githubReleaseOf(PRIVATE_RELEASE);
  assert.deepStrictEqual([tagged.owner, tagged.repository], ['mock-org', 'mock-repo']);
  assert.deepStrictEqual(tagged.releaseAsset, {
    release: { releaseKind: 'taggedRelease', releaseTag: 'mock/v1.0' },
    assetName: 'mock.schema.json',
  });
  assert.deepStrictEqual(githubReleaseOf('mock-org/mock-repo/releases/latest/download/a.json?x=1').releaseAsset, {
    release: { releaseKind: 'latestRelease' },
    assetName: 'a.json',
  });
  assert.strictEqual(githubReleaseOf('mock-org/mock-repo/releases/tag/v1').releaseAsset, null);
  assert.strictEqual(githubReleaseOf('mock-org/mock-repo/blob/main/a.json'), null);
  const reader = new GithubSchemaReader();
  assert.deepStrictEqual(reader.urlPrefixes, ['https://github.com/', 'https://www.github.com/']);
  assert.strictEqual(reader.readsRemoteSources, true);
});

test.describe('a mock github', () => {
  let server;
  let githubApi;

  /** The reader of this GitHub's links, reading with `tokens`. */
  const readerWith = (tokenSource) => githubSchemaReaderAt(tokenSource, githubApi, [`${githubApi}/`]);
  const link = (linkPath) => `${githubApi}/${linkPath}`;

  test.beforeEach(async () => {
    server = mockGithub();
    await new Promise((resolve) => {
      server.listen(0, '127.0.0.1', resolve);
    });
    githubApi = `http://127.0.0.1:${server.address().port}`;
  });

  test.afterEach(() => {
    server.closeAllConnections();
    server.close();
  });

  test('a link without credentials is read as written a private one refused', async () => {
    const reader = readerWith(givenTokens([goodToken()]));
    const publicRead = await reader.readSchemaUrl(mockRequest(link(PUBLIC_RELEASE), false));
    assert.strictEqual(publicRead.readOutcome.outcomeKind, 'textSent');
    const privateRead = await reader.readSchemaUrl(mockRequest(link(PRIVATE_RELEASE), false));
    assert.strictEqual(privateRead.readOutcome.outcomeKind, 'readRefused');
    assert.strictEqual(privateRead.readRefusals[0].refusalReason, '404');
    assert.strictEqual(privateRead.readRefusals[0].refusedBy, 'GitHub');
    const notAnAsset = await reader.readSchemaUrl(mockRequest(link('mock-org/mock-repo/releases/tag/v1'), true));
    assert.strictEqual(notAnAsset.readOutcome.outcomeKind, 'readRefused');
  });

  test('a private release is read with the first token let in each refused one listed', async () => {
    const tokenSource = givenTokens([staleToken(), goodToken()]);
    const reader = readerWith(tokenSource);
    for (const linkPath of [PRIVATE_RELEASE, 'mock-org/mock-repo/releases/latest/download/mock.schema.json']) {
      const read = await reader.readSchemaUrl(mockRequest(link(linkPath), true));
      assert.strictEqual(read.readOutcome.outcomeKind, 'textSent', JSON.stringify(read));
      assert.strictEqual(read.readOutcome.schemaText, MOCK_SCHEMA);
      assert.deepStrictEqual(read.readRefusals, [{
        refusedBy: 'GitHub',
        credentialOrigin: '`GH_TOKEN`',
        refusalReason: '401 Bad credentials',
        renewalFix: 'replace `GH_TOKEN`',
      }]);
    }
    const shoutedLink = link(PRIVATE_RELEASE).replace('http://', 'HTTP://');
    const shouted = await reader.readSchemaUrl(mockRequest(shoutedLink, true));
    assert.strictEqual(shouted.readOutcome.outcomeKind, 'textSent', JSON.stringify(shouted));
    assert.strictEqual(tokenSource.forgottenCount, 0);
  });

  test('a release or an asset the repository lacks is missing', async () => {
    const reader = readerWith(givenTokens([goodToken()]));
    const noRelease = await reader.readSchemaUrl(
      mockRequest(link('mock-org/mock-repo/releases/download/v9/mock.schema.json'), true),
    );
    assert.deepStrictEqual(noRelease.readOutcome, {
      outcomeKind: 'sourceMissing',
      missingReason: '`mock-org/mock-repo` has no release `v9`',
    });
    const noAsset = await reader.readSchemaUrl(
      mockRequest(link('mock-org/mock-repo/releases/latest/download/other.json'), true),
    );
    assert.deepStrictEqual(noAsset.readOutcome, {
      outcomeKind: 'sourceMissing',
      missingReason: 'the latest release of `mock-org/mock-repo` has no asset `other.json`',
    });
  });

  test('every token refused is listed and the tokens read again next time', async () => {
    const tokenSource = givenTokens([staleToken()]);
    const refused = await readerWith(tokenSource).readSchemaUrl(mockRequest(link(PRIVATE_RELEASE), true));
    assert.strictEqual(refused.readOutcome.outcomeKind, 'readRefused');
    assert.strictEqual(refused.readRefusals.length, 1);
    assert.strictEqual(tokenSource.forgottenCount, 1);
    const blind = readerWith(givenTokens([mockToken('GITHUB_TOKEN', BLIND_TOKEN, 'x')]));
    const unseen = await blind.readSchemaUrl(mockRequest(link(PRIVATE_RELEASE), true));
    assert.deepStrictEqual(unseen.readOutcome, {
      outcomeKind: 'readFailed',
      failureReason: 'no credential can see `mock-org/mock-repo`',
    });
    const unset = await readerWith(givenTokens([])).readSchemaUrl(mockRequest(link(PRIVATE_RELEASE), true));
    assert.strictEqual(unset.readRefusals[0].renewalFix, NO_TOKEN_FIX);
  });
});
