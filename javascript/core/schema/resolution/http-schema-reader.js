'use strict';

/*
 * The core's reader of `http` and `https` links: what a server answers a link
 * with, read with the `.netrc` login of its host when asked for the machine's
 * credentials.
 */

const { httpGet, httpConditionalHeaders } = require('./http-reading');
const { MachineCredentials } = require('./machine-credentials');
const { answerOf, refusedAnswer, failedAnswer, missingAnswer } = require('./schema-url-reading');

const STATUS_OK = 200;
const STATUS_NOT_MODIFIED = 304;
const STATUS_UNAUTHORIZED = 401;
const STATUS_FORBIDDEN = 403;
const STATUS_NOT_FOUND = 404;

class HttpSchemaReader {
  /** `credentialSource`, `{ hostLogin(host), forgetCredentials() }`, the machine's `.netrc` without one. */
  constructor(credentialSource = new MachineCredentials()) {
    this.urlPrefixes = ['http://', 'https://'];
    this.readsRemoteSources = true;
    this.credentialSource = credentialSource;
  }

  async readSchemaUrl(request) {
    const host = urlHost(request.fetchUrl);
    const headers = httpConditionalHeaders(request.keptCopyVersion);
    let login = null;
    if (request.machineCredentials) {
      login = this.credentialSource.hostLogin(host);
      if (login === null) {
        return refusedAnswer([{
          refusedBy: host,
          credentialOrigin: null,
          refusalReason: `no \`.netrc\` entry for ${host}`,
          renewalFix: `add a \`.netrc\` entry for ${host}`,
        }]);
      }
      headers.Authorization = login.authorization();
    }
    let reply;
    try {
      reply = await httpGet(request.fetchUrl, headers, request.signal);
    } catch (error) {
      return failedAnswer(error.message);
    }
    if (reply.statusCode === STATUS_UNAUTHORIZED || reply.statusCode === STATUS_FORBIDDEN) {
      if (login !== null) this.credentialSource.forgetCredentials();
      return refusedAnswer([{
        refusedBy: host,
        credentialOrigin: login === null ? null : login.credentialOrigin,
        refusalReason: String(reply.statusCode),
        renewalFix: login === null ? null : login.renewalFix,
      }]);
    }
    return sentAnswer(reply);
  }
}

/** A server's answer to a request for the schema itself. */
function sentAnswer(reply) {
  if (reply.statusCode === STATUS_OK) {
    return answerOf({ outcomeKind: 'textSent', schemaText: reply.replyText, copyVersion: reply.copyVersion });
  }
  if (reply.statusCode === STATUS_NOT_MODIFIED) return answerOf({ outcomeKind: 'copyConfirmed' });
  if (reply.statusCode === STATUS_NOT_FOUND) return missingAnswer('the server answered 404');
  return failedAnswer(`the server answered ${reply.statusCode}`);
}

function urlHost(url) {
  const schemeEnd = url.indexOf('://');
  if (schemeEnd === -1) return '';
  const [authority] = url.slice(schemeEnd + 3).split(/[/?#]/u);
  return authority.split('@').pop().split(':')[0];
}

module.exports = { HttpSchemaReader };
