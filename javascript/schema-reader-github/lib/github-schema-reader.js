'use strict';

/*
 * The YAML DSL standard's reader of schema URLs on GitHub: a link read as
 * written, and, with the machine's credentials, a release asset read through
 * GitHub's API with each of the machine's tokens in turn.
 */

const { spawn } = require('child_process');

const { schemaResolution } = require('yaml-dsl-spec');

const {
  answerOf, refusedAnswer, failedAnswer, httpGet, httpConditionalHeaders,
} = schemaResolution;

const GITHUB_PREFIXES = ['https://github.com/', 'https://www.github.com/'];
const GITHUB_API = 'https://api.github.com';
const GITHUB_API_HOST = 'api.github.com';
const GITHUB_REFUSER = 'GitHub';
const GITHUB_JSON = 'application/vnd.github+json';
const GH_LOGIN_TIMEOUT_MILLISECONDS = 10 * 1000;
const STATUS_OK = 200;
const STATUS_NOT_MODIFIED = 304;
const STATUS_UNAUTHORIZED = 401;
const STATUS_FORBIDDEN = 403;
const STATUS_NOT_FOUND = 404;
const NO_TOKEN_FIX = 'set `GH_TOKEN`, run `gh auth login`, or add a `.netrc` entry for api.github.com';

/** The machine's tokens, `{ credentialOrigin, authorization, renewalFix }`, read when first needed. */
class MachineTokens {
  constructor() {
    this.tokensRead = null;
  }

  /** A promise of the tokens, in the order they are tried. */
  githubTokens() {
    if (this.tokensRead === null) this.tokensRead = machineTokens();
    return this.tokensRead;
  }

  /** Reads the tokens again when next asked, after every one was refused. */
  forgetTokens() {
    this.tokensRead = null;
  }
}

/** `GH_TOKEN`, `GITHUB_TOKEN`, the `gh` command's login, then the `.netrc` entry for api.github.com. */
async function machineTokens() {
  const tokens = [];
  for (const variableName of ['GH_TOKEN', 'GITHUB_TOKEN']) {
    const variableToken = process.env[variableName];
    if (variableToken) {
      tokens.push({
        credentialOrigin: `\`${variableName}\``,
        authorization: `Bearer ${variableToken}`,
        renewalFix: `replace \`${variableName}\``,
      });
    }
  }
  const loginToken = await ghLoginToken();
  if (loginToken !== null) {
    tokens.push({
      credentialOrigin: '`gh auth token`',
      authorization: `Bearer ${loginToken}`,
      renewalFix: 'run `gh auth login`',
    });
  }
  const login = schemaResolution.netrcLogin(GITHUB_API_HOST);
  if (login !== null) {
    tokens.push({
      credentialOrigin: login.credentialOrigin,
      authorization: login.authorization(),
      renewalFix: login.renewalFix,
    });
  }
  return tokens;
}

/**
 * The token of the account `gh auth login` logged in to github.com, where the
 * `gh` command is installed and answers within its time; `null` otherwise.
 */
function ghLoginToken() {
  return new Promise((resolve) => {
    let ghCommand;
    try {
      ghCommand = spawn('gh', ['auth', 'token', '--hostname', 'github.com'], { stdio: ['ignore', 'pipe', 'ignore'] });
    } catch {
      resolve(null);
      return;
    }
    const outputChunks = [];
    const loginTimer = setTimeout(() => {
      ghCommand.kill();
      resolve(null);
    }, GH_LOGIN_TIMEOUT_MILLISECONDS);
    ghCommand.stdout.on('data', (outputChunk) => outputChunks.push(outputChunk));
    ghCommand.on('error', () => {
      clearTimeout(loginTimer);
      resolve(null);
    });
    ghCommand.on('close', (exitCode) => {
      clearTimeout(loginTimer);
      const loginToken = Buffer.concat(outputChunks).toString('utf8').trim();
      resolve(exitCode === 0 && loginToken !== '' ? loginToken : null);
    });
  });
}

/** The URL reader of GitHub links. */
class GithubSchemaReader {
  constructor() {
    this.urlPrefixes = [...GITHUB_PREFIXES];
    this.readsRemoteSources = true;
    this.tokenSource = new MachineTokens();
    this.githubApi = GITHUB_API;
  }

  async readSchemaUrl(request) {
    if (request.machineCredentials) {
      const release = this.githubRelease(request.fetchUrl);
      if (release !== null && release.releaseAsset !== null) {
        return this.releaseAnswer(release.owner, release.repository, release.releaseAsset, request);
      }
    }
    return linkAnswer(request);
  }

  githubRelease(url) {
    const prefix = this.urlPrefixes.find(
      (linkPrefix) => url.slice(0, linkPrefix.length).toLowerCase() === linkPrefix.toLowerCase(),
    );
    return prefix === undefined ? null : githubReleaseOf(url.slice(prefix.length));
  }

  /**
   * A release asset read through GitHub's API, each token tried in turn until one
   * is let in, each one refused before it listed.
   */
  async releaseAnswer(owner, repository, releaseAsset, request) {
    const tokens = await this.tokenSource.githubTokens();
    if (tokens.length === 0) {
      return refusedAnswer([{
        refusedBy: GITHUB_REFUSER,
        credentialOrigin: null,
        refusalReason: 'no GitHub token is set',
        renewalFix: NO_TOKEN_FIX,
      }]);
    }
    const answerWith = (readOutcome, readRefusals) => ({ readOutcome, readRefusals });
    const failedOutcome = (failureReason) => ({ outcomeKind: 'readFailed', failureReason });
    const repositoryName = `\`${owner}/${repository}\``;
    const repositoryUrl = `${this.githubApi}/repos/${owner}/${repository}`;
    const releaseUrl = `${repositoryUrl}/releases/${releaseApiPath(releaseAsset.release)}`;
    const refusals = [];
    let missingReason = null;
    for (const token of tokens) {
      const apiHeaders = { Authorization: token.authorization, Accept: GITHUB_JSON };
      let releaseReply;
      try {
        releaseReply = await httpGet(releaseUrl, apiHeaders, request.signal);
      } catch (error) {
        return answerWith(failedOutcome(error.message), refusals);
      }
      if (releaseReply.statusCode === STATUS_UNAUTHORIZED || releaseReply.statusCode === STATUS_FORBIDDEN) {
        refusals.push(tokenRefusal(token, releaseReply.statusCode));
        continue;
      }
      if (releaseReply.statusCode !== STATUS_OK) {
        if (await this.repositorySeen(repositoryUrl, apiHeaders, request)) {
          missingReason = `${repositoryName} has no ${releaseShownName(releaseAsset.release)}`;
          break;
        }
        continue;
      }
      let listing;
      try {
        listing = JSON.parse(releaseReply.replyText);
      } catch (error) {
        return answerWith(failedOutcome(error.message), refusals);
      }
      const listedAssets = listing !== null && Array.isArray(listing.assets) ? listing.assets : [];
      const listedAsset = listedAssets.find((listed) => listed !== null && listed.name === releaseAsset.assetName);
      if (listedAsset === undefined || typeof listedAsset.url !== 'string') {
        missingReason = `${releaseShownName(releaseAsset.release)} of ${repositoryName} `
          + `has no asset \`${releaseAsset.assetName}\``;
        break;
      }
      const downloadHeaders = {
        ...httpConditionalHeaders(request.keptCopyVersion),
        Authorization: token.authorization,
        Accept: 'application/octet-stream',
      };
      let download;
      try {
        download = await httpGet(listedAsset.url, downloadHeaders, request.signal);
      } catch (error) {
        return answerWith(failedOutcome(error.message), refusals);
      }
      if (download.statusCode === STATUS_UNAUTHORIZED || download.statusCode === STATUS_FORBIDDEN) {
        refusals.push(tokenRefusal(token, download.statusCode));
        continue;
      }
      return answerWith(sentAnswer(download).readOutcome, refusals);
    }
    if (missingReason !== null) return answerWith({ outcomeKind: 'sourceMissing', missingReason }, refusals);
    if (refusals.length === tokens.length) {
      this.tokenSource.forgetTokens();
      return refusedAnswer(refusals);
    }
    return answerWith(failedOutcome(`no credential can see ${repositoryName}`), refusals);
  }

  /** Whether the repository answers a token's request. */
  async repositorySeen(repositoryUrl, apiHeaders, request) {
    try {
      const repositoryReply = await httpGet(repositoryUrl, apiHeaders, request.signal);
      return repositoryReply.statusCode === STATUS_OK;
    } catch {
      return false;
    }
  }
}

/** The reader of the GitHub at `githubApi`, its links read under `linkPrefixes`, with `tokenSource`'s tokens. */
function githubSchemaReaderAt(tokenSource, githubApi, linkPrefixes) {
  const reader = new GithubSchemaReader();
  reader.tokenSource = tokenSource;
  reader.githubApi = githubApi;
  reader.urlPrefixes = linkPrefixes;
  return reader;
}

/** The link read as written, the machine's credentials never sent. */
async function linkAnswer(request) {
  let reply;
  try {
    reply = await httpGet(request.fetchUrl, httpConditionalHeaders(request.keptCopyVersion), request.signal);
  } catch (error) {
    return failedAnswer(error.message);
  }
  if ([STATUS_UNAUTHORIZED, STATUS_FORBIDDEN, STATUS_NOT_FOUND].includes(reply.statusCode)) {
    return refusedAnswer([{
      refusedBy: GITHUB_REFUSER,
      credentialOrigin: null,
      refusalReason: String(reply.statusCode),
      renewalFix: null,
    }]);
  }
  return sentAnswer(reply);
}

function sentAnswer(reply) {
  if (reply.statusCode === STATUS_OK) {
    return answerOf({ outcomeKind: 'textSent', schemaText: reply.replyText, copyVersion: reply.copyVersion });
  }
  if (reply.statusCode === STATUS_NOT_MODIFIED) return answerOf({ outcomeKind: 'copyConfirmed' });
  return failedAnswer(`GitHub answered ${reply.statusCode}`);
}

function tokenRefusal(token, status) {
  return {
    refusedBy: GITHUB_REFUSER,
    credentialOrigin: token.credentialOrigin,
    refusalReason: status === STATUS_UNAUTHORIZED ? '401 Bad credentials' : String(status),
    renewalFix: token.renewalFix,
  };
}

/** A release by its kind: `{ releaseKind: 'latestRelease' }` or `{ releaseKind: 'taggedRelease', releaseTag }`. */
function releaseApiPath(release) {
  return release.releaseKind === 'latestRelease' ? 'latest' : `tags/${release.releaseTag.replaceAll('/', '%2F')}`;
}

function releaseShownName(release) {
  return release.releaseKind === 'latestRelease' ? 'the latest release' : `release \`${release.releaseTag}\``;
}

/**
 * The repository a link's path, after its host, names under
 * `<owner>/<repo>/releases/`, and the release asset that path reads,
 * `download/<tag>/<asset>` or `latest/download/<asset>`, `null` where it reads
 * none; `null` for a path naming no repository's releases.
 */
function githubReleaseOf(linkPath) {
  const [pathOnly] = linkPath.split(/[?#]/u);
  const steps = pathOnly.split('/');
  const [owner, repository, releasesStep, ...releaseSteps] = steps;
  if (steps.length < 3 || releasesStep !== 'releases' || owner === '' || repository === '') return null;
  let releaseAsset = null;
  if (releaseSteps.length === 3 && releaseSteps[0] === 'latest' && releaseSteps[1] === 'download') {
    releaseAsset = { release: { releaseKind: 'latestRelease' }, assetName: releaseSteps[2] };
  } else if (releaseSteps.length >= 3 && releaseSteps[0] === 'download') {
    releaseAsset = {
      release: { releaseKind: 'taggedRelease', releaseTag: releaseSteps.slice(1, -1).join('/') },
      assetName: releaseSteps[releaseSteps.length - 1],
    };
  }
  return { owner, repository, releaseAsset };
}

module.exports = { GithubSchemaReader, MachineTokens, githubSchemaReaderAt, githubReleaseOf, NO_TOKEN_FIX };
