'use strict';

/*
 * Where a schema is named: a document's modeline, and a location as written,
 * resolved to a URL with the cache's parameters read out of it.
 */

const fs = require('fs');
const path = require('path');

const { codeSpan } = require('../../problem-wording');

const MODELINE_KEY = 'yaml-language-server:';
const MODELINE_SCHEMA = '$schema=';
const CONFIG_ROOT = '{config_root}';
const CACHE_PARAMETER_PREFIX = 'yaml_dsl_';
const MAX_AGE_PARAMETER = 'yaml_dsl_max_age_days';
const MACHINE_CREDENTIALS_PARAMETER = 'yaml_dsl_machine_credentials';
const FILE_SCHEME = 'file';
const LARGEST_WHOLE_DAYS = 18446744073709551615n;
const LEADING_WHITE_SPACE = /^\p{White_Space}+/u;
const WHITE_SPACE = /\p{White_Space}/u;
const URL_KEPT_BYTES = new Set(Buffer.from("-._~!$&'()*+,;=:@"));

/** `text` and the part before `separator`'s first place, and after it, `null` without it. */
function splitOnce(text, separator) {
  const at = text.indexOf(separator);
  return at === -1 ? [text, null] : [text.slice(0, at), text.slice(at + separator.length)];
}

/**
 * The first line of `text` reading `# yaml-language-server: $schema=<uri>`: the
 * schema URI, its line and the column it starts at; `null` without one.
 */
function modelineOf(text) {
  const lines = text.split('\n').map((line) => (line.endsWith('\r') ? line.slice(0, -1) : line));
  for (const [lineIndex, line] of lines.entries()) {
    const trimmedLine = line.replace(LEADING_WHITE_SPACE, '');
    if (!trimmedLine.startsWith('#')) continue;
    const afterHash = trimmedLine.slice(1).replace(LEADING_WHITE_SPACE, '');
    if (!afterHash.startsWith(MODELINE_KEY)) continue;
    const afterKey = afterHash.slice(MODELINE_KEY.length).replace(LEADING_WHITE_SPACE, '');
    if (!afterKey.startsWith(MODELINE_SCHEMA)) continue;
    const uriAndRest = afterKey.slice(MODELINE_SCHEMA.length);
    const uriCharacters = [];
    for (const character of uriAndRest) {
      if (WHITE_SPACE.test(character)) break;
      uriCharacters.push(character);
    }
    if (uriCharacters.length === 0) continue;
    const uriStart = line.length - uriAndRest.length;
    return {
      schemaUri: uriCharacters.join(''),
      line: lineIndex + 1,
      column: Array.from(line.slice(0, uriStart)).length + 1,
    };
  }
  return null;
}

/**
 * `written`, a path or a URL, resolved: a path against `base`, or against
 * `configRoot` when it starts with `{config_root}`.
 */
function resolveLocation(written, base, configRoot) {
  const [withoutFragment, schemaFragment] = splitOnce(written, '#');
  const [address, query] = splitOnce(withoutFragment, '?');
  const parameters = cacheParameters(query);
  const scheme = urlSchemeOf(address);
  let urlScheme;
  let locationAddress;
  let filePath = null;
  if (scheme === FILE_SCHEME) {
    filePath = fileUrlPath(address);
    urlScheme = scheme;
    locationAddress = fileUrlOf(filePath);
  } else if (scheme !== null) {
    urlScheme = scheme;
    locationAddress = address;
  } else {
    filePath = writtenPath(address, base, configRoot);
    urlScheme = FILE_SCHEME;
    locationAddress = fileUrlOf(filePath);
  }
  const withQuery = (passedParameters) => (passedParameters.length === 0
    ? locationAddress
    : `${locationAddress}?${passedParameters.join('&')}`);
  const allParameters = query === null ? [] : query.split('&');
  return {
    writtenLocation: withoutFragment,
    schemaFragment,
    locationUrl: withQuery(allParameters),
    urlScheme,
    fetchUrl: withQuery(parameters.passedParameters),
    maxAgeDays: parameters.maxAgeDays,
    machineCredentials: parameters.machineCredentials,
    cacheParameterNames: parameters.parameterNames,
    cacheParameterProblem: parameters.parameterProblem,
    filePath,
  };
}

/** A URL's scheme, lower case, where `location` starts with `<scheme>://`; else `null`. */
function urlSchemeOf(location) {
  const [scheme, rest] = splitOnce(location, '://');
  if (rest === null) return null;
  return /^[A-Za-z][A-Za-z0-9+\-.]*$/u.test(scheme) ? scheme.toLowerCase() : null;
}

/** The cache's parameters a query carries, read out, and every other parameter in order. */
function cacheParameters(query) {
  const parameters = {
    passedParameters: [],
    parameterNames: [],
    maxAgeDays: 0,
    machineCredentials: false,
    parameterProblem: null,
  };
  for (const parameter of query === null ? [] : query.split('&')) {
    const [parameterName, valueText] = splitOnce(parameter, '=');
    const parameterValue = valueText === null ? '' : valueText;
    if (!parameterName.startsWith(CACHE_PARAMETER_PREFIX)) {
      parameters.passedParameters.push(parameter);
      continue;
    }
    parameters.parameterNames.push(parameterName);
    let problem = null;
    if (parameterName === MAX_AGE_PARAMETER) {
      const days = wholeDays(parameterValue);
      if (days === null) problem = `${codeSpan(MAX_AGE_PARAMETER)} must be a whole number of days`;
      else parameters.maxAgeDays = days;
    } else if (parameterName === MACHINE_CREDENTIALS_PARAMETER) {
      if (parameterValue === 'true' || parameterValue === 'false') {
        parameters.machineCredentials = parameterValue === 'true';
      } else {
        problem = `${codeSpan(MACHINE_CREDENTIALS_PARAMETER)} must be ${codeSpan('true')} or ${codeSpan('false')}`;
      }
    } else {
      problem = `${codeSpan(parameterName)} is not a parameter of a schema URL`;
    }
    if (parameters.parameterProblem === null) parameters.parameterProblem = problem;
  }
  return parameters;
}

/** The whole number of days `valueText` writes, `null` where it writes none. */
function wholeDays(valueText) {
  const digitsOnly = /^[0-9]+$/u.test(valueText);
  const leadingZero = valueText.length > 1 && valueText.startsWith('0');
  if (!digitsOnly || leadingZero || BigInt(valueText) > LARGEST_WHOLE_DAYS) return null;
  return Number(valueText);
}

/** `folder` with `step` added, as written. */
function joinedStep(folder, step) {
  return folder.endsWith(path.sep) ? `${folder}${step}` : `${folder}${path.sep}${step}`;
}

/** The machine path a written path names, its steps separated by `/`. */
function writtenPath(written, base, configRoot) {
  let start = base;
  let rest = written;
  if (written.startsWith(CONFIG_ROOT)) {
    start = configRoot;
    rest = written.slice(CONFIG_ROOT.length);
  } else if (written.startsWith('/')) {
    start = '/';
  }
  let machinePath = start;
  for (const step of rest.split('/')) {
    if (step === '' || step === '.') continue;
    machinePath = step === '..' ? path.dirname(machinePath) : joinedStep(machinePath, step);
  }
  return machinePath;
}

/** The machine path a `file` URL names. */
function fileUrlPath(fileUrl) {
  const schemeEnd = fileUrl.indexOf('://');
  const afterScheme = schemeEnd === -1 ? fileUrl : fileUrl.slice(schemeEnd + 3);
  const pathStart = afterScheme.indexOf('/');
  const urlPath = pathStart === -1 ? '' : afterScheme.slice(pathStart);
  const steps = percentDecoded(urlPath).split('/').filter((step) => step !== '');
  const namesDrive = steps.length > 0 && /^[A-Za-z]:$/u.test(steps[0]);
  const machineRoot = namesDrive ? `${steps.shift()}${path.sep}` : path.sep;
  const keptSteps = [];
  for (const step of steps) {
    if (step === '..') keptSteps.pop();
    else if (step !== '.') keptSteps.push(step);
  }
  return keptSteps.reduce(joinedStep, machineRoot);
}

/** The `file` URL of a machine path. */
function fileUrlOf(machinePath) {
  const steps = machinePath.split(path.sep).filter((step) => step !== '' && step !== '.' && step !== '..');
  return `file:///${steps.map(percentEncoded).join('/')}`;
}

function percentEncoded(step) {
  let encoded = '';
  for (const byte of Buffer.from(step, 'utf8')) {
    const kept = (byte >= 0x30 && byte <= 0x39) || (byte >= 0x41 && byte <= 0x5a)
      || (byte >= 0x61 && byte <= 0x7a) || URL_KEPT_BYTES.has(byte);
    encoded += kept
      ? String.fromCharCode(byte)
      : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }
  return encoded;
}

function percentDecoded(text) {
  const textBytes = Buffer.from(text, 'utf8');
  const decodedBytes = [];
  for (let at = 0; at < textBytes.length;) {
    const hexText = textBytes.subarray(at + 1, at + 3).toString('latin1');
    if (textBytes[at] === 0x25 && /^[0-9A-Fa-f]{2}$/u.test(hexText)) {
      decodedBytes.push(parseInt(hexText, 16));
      at += 3;
    } else {
      decodedBytes.push(textBytes[at]);
      at += 1;
    }
  }
  return Buffer.from(decodedBytes).toString('utf8');
}

/** The order of two machine paths, step by step, each step compared by its bytes. */
function comparePathOrder(firstPath, secondPath) {
  const firstSteps = firstPath.split(path.sep);
  const secondSteps = secondPath.split(path.sep);
  for (let at = 0; at < Math.min(firstSteps.length, secondSteps.length); at += 1) {
    const stepOrder = Buffer.compare(Buffer.from(firstSteps[at]), Buffer.from(secondSteps[at]));
    if (stepOrder !== 0) return stepOrder;
  }
  return firstSteps.length - secondSteps.length;
}

function isFile(machinePath) {
  const status = fs.statSync(machinePath, { throwIfNoEntry: false });
  return status !== undefined && status.isFile();
}

/**
 * The first file a path names, each `*` step matching any run of characters
 * within the step, the steps' matches read in path order; `null` where none is.
 */
function firstMatchingFile(patternPath) {
  const pathRoot = path.parse(patternPath).root;
  const steps = patternPath.slice(pathRoot.length).split(path.sep).filter((step) => step !== '' && step !== '.');
  let reachedPaths = [pathRoot];
  for (const step of steps) {
    if (!step.includes('*')) {
      reachedPaths = reachedPaths.map((folder) => joinedStep(folder, step));
      continue;
    }
    const matchedPaths = reachedPaths.flatMap((folder) => {
      let entryNames;
      try {
        entryNames = fs.readdirSync(folder);
      } catch {
        return [];
      }
      return entryNames.filter((entryName) => stepMatches(step, entryName))
        .map((entryName) => joinedStep(folder, entryName));
    });
    reachedPaths = matchedPaths.sort(comparePathOrder);
  }
  return reachedPaths.find(isFile) || null;
}

/** Whether `entryName` matches a step whose `*` matches any run of characters. */
function stepMatches(step, entryName) {
  const parts = step.split('*');
  const firstPart = parts[0];
  const lastPart = parts[parts.length - 1];
  if (!entryName.startsWith(firstPart) || entryName.length < firstPart.length + lastPart.length
    || !entryName.endsWith(lastPart)) {
    return false;
  }
  let middle = entryName.slice(firstPart.length, entryName.length - lastPart.length);
  for (const part of parts.slice(1, -1)) {
    const at = middle.indexOf(part);
    if (at === -1) return false;
    middle = middle.slice(at + part.length);
  }
  return true;
}

function hasPattern(machinePath) {
  return machinePath.includes('*');
}

module.exports = {
  FILE_SCHEME,
  modelineOf,
  resolveLocation,
  urlSchemeOf,
  fileUrlPath,
  fileUrlOf,
  firstMatchingFile,
  stepMatches,
  hasPattern,
};
