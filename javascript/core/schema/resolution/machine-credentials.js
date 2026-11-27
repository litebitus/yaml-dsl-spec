'use strict';

/* The machine's credentials a schema link is read with: the `.netrc` entry for its host. */

const fs = require('fs');
const path = require('path');

/** A host's login, where it came from, and how its owner renews it. */
class HostLogin {
  constructor({ credentialOrigin, loginName, loginPassword, renewalFix }) {
    this.credentialOrigin = credentialOrigin;
    this.loginName = loginName;
    this.loginPassword = loginPassword;
    this.renewalFix = renewalFix;
  }

  /** The `Authorization` header value the login is sent as. */
  authorization() {
    return `Basic ${Buffer.from(`${this.loginName}:${this.loginPassword}`, 'utf8').toString('base64')}`;
  }
}

/** The machine's `.netrc`, read when first needed, and again after its logins were refused. */
class MachineCredentials {
  constructor() {
    this.netrcTextRead = undefined;
  }

  /** The login `.netrc` gives `host`, `null` without one. */
  hostLogin(host) {
    if (this.netrcTextRead === undefined) this.netrcTextRead = netrcText();
    return this.netrcTextRead === null ? null : hostLoginOf(this.netrcTextRead, host);
  }

  /** Reads the credentials again when next asked, after every one was refused. */
  forgetCredentials() {
    this.netrcTextRead = undefined;
  }
}

/**
 * The login the machine's `.netrc` gives `host`: its `machine` entry, else its
 * `default` entry; the file `NETRC` names, else the one in the user's home folder.
 * `null` without one.
 */
function netrcLogin(host) {
  const text = netrcText();
  return text === null ? null : hostLoginOf(text, host);
}

function netrcText() {
  let netrcPath = process.env.NETRC;
  if (netrcPath === undefined) {
    if (process.env.HOME === undefined) return null;
    netrcPath = path.join(process.env.HOME, '.netrc');
  }
  try {
    return fs.readFileSync(netrcPath, 'utf8');
  } catch {
    return null;
  }
}

function hostLoginOf(text, host) {
  const entry = netrcEntry(text, host);
  if (entry === null) return null;
  return new HostLogin({
    credentialOrigin: `the \`.netrc\` entry for ${host}`,
    loginName: entry.loginName,
    loginPassword: entry.loginPassword,
    renewalFix: `update the \`.netrc\` entry for ${host}`,
  });
}

/**
 * The login and password a `.netrc`'s text gives `host`: its `machine` entry,
 * else its `default` entry; `null` without either.
 */
function netrcEntry(text, host) {
  const words = text.split(/\p{White_Space}+/u).filter((word) => word !== '');
  let hostEntry = null;
  let defaultEntry = null;
  let currentEntry = null;
  let entryFields = { loginName: '', loginPassword: '' };
  const finishEntry = () => {
    if (currentEntry === null) return;
    const taken = entryFields;
    entryFields = { loginName: '', loginPassword: '' };
    if (currentEntry.namesHost && hostEntry === null) hostEntry = { ...taken };
    if (currentEntry.namesDefault && defaultEntry === null) defaultEntry = taken;
  };
  const nextWord = (at) => (at < words.length ? words[at] : null);
  for (let at = 0; at < words.length; at += 1) {
    switch (words[at]) {
      case 'machine':
        finishEntry();
        at += 1;
        currentEntry = { namesHost: nextWord(at) === host, namesDefault: false };
        break;
      case 'default':
        finishEntry();
        currentEntry = { namesHost: false, namesDefault: true };
        break;
      case 'login':
        at += 1;
        entryFields.loginName = nextWord(at) || '';
        break;
      case 'password':
        at += 1;
        entryFields.loginPassword = nextWord(at) || '';
        break;
      case 'account':
        at += 1;
        break;
      case 'macdef':
        finishEntry();
        currentEntry = null;
        at += 1;
        break;
      default:
        break;
    }
  }
  finishEntry();
  return hostEntry || defaultEntry;
}

module.exports = { HostLogin, MachineCredentials, netrcLogin, netrcEntry, hostLoginOf };
