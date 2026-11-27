'use strict';

/*
 * A document's schema found and read through a schema cache: its modeline's
 * location, then each search path, each read by the reader of its URL's prefix,
 * a remote source's text kept as a copy in the cache's store.
 */

const crypto = require('crypto');
const path = require('path');

const { Diagnostic } = require('../../diagnostic-report');
const { codeSpan } = require('../../problem-wording');
const { Position } = require('../../source-position');
const {
  FILE_SCHEME,
  fileUrlOf,
  fileUrlPath,
  firstMatchingFile,
  hasPattern,
  documentModeline,
  resolveLocation,
} = require('./schema-locations');

const READ_LIMIT_MILLISECONDS = 60 * 1000;
const DAY_SECONDS = 24 * 60 * 60;
const NO_MATCH_REASON = 'no file matches';
const NEVER_KEPT_REASON = 'the reader confirmed a copy never kept';
const MACHINE_CREDENTIALS_HELP = "add `yaml_dsl_machine_credentials=true` to read it with the machine's credentials";

/**
 * The schema finding of the document `file`, whose text is `text`: its
 * modeline's location tried, then each search path, `{ pathText, writtenAt }`, in
 * order, a path resolved against the document's folder, or `configRoot` where it
 * starts with `{config_root}`.
 */
async function documentSchemaFinding(cache, file, text, searchPaths, configRoot) {
  const shownFile = String(file);
  const documentFolder = path.resolve(path.dirname(shownFile));
  const configFolder = path.resolve(String(configRoot));
  const modeline = documentModeline(text);
  const fallbackPosition = new Position(shownFile, modeline === null ? 1 : modeline.line, 1);
  const namedLocations = [];
  if (modeline !== null) {
    const position = new Position(shownFile, modeline.line, modeline.column);
    namedLocations.push({
      resolved: resolveLocation(modeline.schemaUri, documentFolder, configFolder),
      writtenAt: position,
      namedBy: { namerKind: 'modeline', position },
    });
  }
  for (const [index, searchPath] of searchPaths.entries()) {
    const resolved = resolveLocation(searchPath.pathText, documentFolder, configFolder);
    resolved.schemaFragment = null;
    namedLocations.push({
      resolved,
      writtenAt: searchPath.writtenAt || fallbackPosition,
      namedBy: { namerKind: 'searchPath', index },
    });
  }
  const { finding, missingCandidates } = await findingOf(cache, namedLocations);
  if (missingCandidates !== null) {
    const help = missingCandidates
      .map((missing) => `${codeSpan(missing.writtenLocation)}: ${missing.missingReason}`)
      .join('\n');
    const problem = Diagnostic.at(fallbackPosition, 'no schema found');
    finding.findingProblems.push(problem.withHelp(help === '' ? null : help));
  }
  return finding;
}

/**
 * The schema finding of a location named outside a document: a path resolved
 * against the working directory, a problem placed at `writtenAt`, unplaced without it.
 */
async function readSchemaFinding(cache, location, writtenAt) {
  const workingDirectory = path.resolve('');
  const namedLocation = {
    resolved: resolveLocation(location, workingDirectory, workingDirectory),
    writtenAt: writtenAt || null,
    namedBy: { namerKind: 'caller' },
  };
  const { finding, missingCandidates } = await findingOf(cache, [namedLocation]);
  if (missingCandidates !== null && missingCandidates.length > 0) {
    const message = `${codeSpan(location)} is missing`;
    const problem = writtenAt ? Diagnostic.at(writtenAt, message) : Diagnostic.unplaced(message);
    finding.findingProblems.push(problem.withLabel(missingCandidates[0].missingReason));
  }
  return finding;
}

/**
 * Each location read in turn until one is there, or one's read ends the finding;
 * and, where every one is missing, why each is.
 */
async function findingOf(cache, namedLocations) {
  const missingCandidates = [];
  const finding = { triedCandidates: [], foundSchema: null, findingProblems: [] };
  for (const namedLocation of namedLocations) {
    const candidateRead = await candidateReadOf(cache, namedLocation, finding.findingProblems);
    const candidate = {
      schemaLocation: candidateRead.readProblem
        ? namedLocation.resolved.locationUrl
        : candidateRead.schemaLocation,
      namedBy: namedLocation.namedBy,
    };
    finding.triedCandidates.push(candidate);
    if (candidateRead.readProblem) {
      finding.findingProblems.push(candidateRead.readProblem);
      return { finding, missingCandidates: null };
    }
    if (candidateRead.missingReason !== undefined) {
      missingCandidates.push({
        writtenLocation: namedLocation.resolved.writtenLocation,
        missingReason: candidateRead.missingReason,
      });
      continue;
    }
    finding.foundSchema = {
      schemaName: namedLocation.resolved.urlScheme === FILE_SCHEME
        ? fileNameOf(candidateRead.schemaLocation)
        : namedLocation.resolved.writtenLocation,
      schemaText: candidateRead.schemaText,
      schemaFragment: namedLocation.resolved.schemaFragment,
      foundCandidate: candidate,
    };
    return { finding, missingCandidates: null };
  }
  return { finding, missingCandidates };
}

/**
 * One location read by the reader of its prefix, a remote source through its
 * copy: `{ schemaText, schemaLocation }`, `{ missingReason, schemaLocation }` or
 * `{ readProblem }`.
 */
async function candidateReadOf(cache, namedLocation, warnings) {
  const { resolved, writtenAt } = namedLocation;
  const problemAt = (message) => (writtenAt ? Diagnostic.at(writtenAt, message) : Diagnostic.unplaced(message));
  if (resolved.cacheParameterProblem !== null) return { readProblem: problemAt(resolved.cacheParameterProblem) };
  const reader = cache.urlReader(resolved.locationUrl);
  if (reader === null) {
    return { readProblem: problemAt(`no reader reads ${codeSpan(resolved.writtenLocation)}`) };
  }
  if (!reader.readsRemoteSources) {
    if (resolved.cacheParameterNames.length > 0) {
      const [parameterName] = resolved.cacheParameterNames;
      return {
        readProblem: problemAt(
          `${codeSpan(parameterName)} is not a parameter of a ${codeSpan(resolved.urlScheme)} URL`,
        ),
      };
    }
    return localRead(reader, resolved, problemAt);
  }
  return cache.readInTurn(resolved.fetchUrl, () => remoteRead(cache, reader, resolved, problemAt, warnings));
}

/** What a reader is asked: the URL, whether with the machine's credentials, and the kept copy's version. */
function urlRequest(fetchUrl, machineCredentials, keptCopyVersion) {
  return {
    fetchUrl,
    machineCredentials,
    keptCopyVersion,
    signal: AbortSignal.timeout(READ_LIMIT_MILLISECONDS),
  };
}

/** A source on the machine, read each time; a `file` pattern's first match. */
async function localRead(reader, resolved, problemAt) {
  let { fetchUrl } = resolved;
  let schemaLocation = resolved.locationUrl;
  if (resolved.filePath !== null && hasPattern(resolved.filePath)) {
    const matchedPath = firstMatchingFile(resolved.filePath);
    if (matchedPath === null) return { missingReason: NO_MATCH_REASON, schemaLocation };
    schemaLocation = fileUrlOf(matchedPath);
    const queryStart = fetchUrl.indexOf('?');
    fetchUrl = queryStart === -1 ? schemaLocation : `${schemaLocation}${fetchUrl.slice(queryStart)}`;
  }
  const answer = await reader.readSchemaUrl(urlRequest(fetchUrl, false, null));
  const outcome = answer.readOutcome;
  switch (outcome.outcomeKind) {
    case 'textSent':
      return { schemaText: outcome.schemaText, schemaLocation };
    case 'sourceMissing':
      return { missingReason: outcome.missingReason, schemaLocation };
    case 'readRefused':
      return { readProblem: refusedProblem(problemAt, resolved.writtenLocation, answer.readRefusals, false) };
    case 'readFailed':
      return { readProblem: failedProblem(problemAt, resolved.writtenLocation, outcome.failureReason) };
    default:
      return { readProblem: failedProblem(problemAt, resolved.writtenLocation, NEVER_KEPT_REASON) };
  }
}

/**
 * A remote source: its kept copy while it is younger than its max age, else what
 * the reader answers, read again with the machine's credentials where it is
 * refused and the URL asks for them.
 */
async function remoteRead(cache, reader, resolved, problemAt, warnings) {
  const schemaLocation = resolved.locationUrl;
  const copyKey = copyKeyOf(resolved.fetchUrl);
  const keptCopy = keptCopyOf(cache.store, copyKey, resolved.fetchUrl);
  const secondsNow = Math.floor(Date.now() / 1000);
  if (keptCopy !== null && resolved.maxAgeDays > 0
    && Math.max(0, secondsNow - keptCopy.confirmed_at) < resolved.maxAgeDays * DAY_SECONDS) {
    return { schemaText: keptCopy.body, schemaLocation };
  }
  const keptCopyVersion = keptCopy === null ? null : keptCopy.copy_version;
  let answer = await reader.readSchemaUrl(urlRequest(resolved.fetchUrl, false, keptCopyVersion));
  const credentialsAsked = answer.readOutcome.outcomeKind === 'readRefused' && resolved.machineCredentials;
  if (credentialsAsked) {
    answer = await reader.readSchemaUrl(urlRequest(resolved.fetchUrl, true, keptCopyVersion));
  }
  const { readOutcome, readRefusals } = answer;
  if (readOutcome.outcomeKind !== 'readRefused') {
    for (const refusal of readRefusals) {
      if (isStated(refusal.credentialOrigin)) warnings.push(refusalWarning(problemAt, refusal));
    }
  }
  const writtenLocation = resolved.writtenLocation;
  switch (readOutcome.outcomeKind) {
    case 'textSent':
      keepCopy(cache.store, copyKey, {
        url: resolved.fetchUrl,
        copy_version: readOutcome.copyVersion === undefined ? null : readOutcome.copyVersion,
        confirmed_at: secondsNow,
        body: readOutcome.schemaText,
      });
      return { schemaText: readOutcome.schemaText, schemaLocation };
    case 'copyConfirmed':
      if (keptCopy === null) return { readProblem: failedProblem(problemAt, writtenLocation, NEVER_KEPT_REASON) };
      keepCopy(cache.store, copyKey, { ...keptCopy, confirmed_at: secondsNow });
      return { schemaText: keptCopy.body, schemaLocation };
    case 'sourceMissing':
      return { missingReason: readOutcome.missingReason, schemaLocation };
    case 'readRefused': {
      const refused = refusedProblem(problemAt, writtenLocation, readRefusals, credentialsAsked);
      if (keptCopy === null) return { readProblem: refused };
      warnings.push(warningOf(refused, `${refused.message}, so its kept copy was read`));
      return { schemaText: keptCopy.body, schemaLocation };
    }
    default:
      if (keptCopy === null) {
        return { readProblem: failedProblem(problemAt, writtenLocation, readOutcome.failureReason) };
      }
      return { schemaText: keptCopy.body, schemaLocation };
  }
}

/** The copy kept under `copyKey` for `fetchUrl`, `null` where none reads. */
function keptCopyOf(store, copyKey, fetchUrl) {
  if (!store) return null;
  const stored = store.read(copyKey);
  if (typeof stored !== 'string') return null;
  let keptCopy;
  try {
    keptCopy = JSON.parse(stored);
  } catch {
    return null;
  }
  const readable = keptCopy !== null && typeof keptCopy === 'object'
    && keptCopy.url === fetchUrl
    && Number.isInteger(keptCopy.confirmed_at) && keptCopy.confirmed_at >= 0
    && typeof keptCopy.body === 'string';
  if (!readable) return null;
  return {
    url: keptCopy.url,
    copy_version: keptCopy.copy_version === undefined ? null : keptCopy.copy_version,
    confirmed_at: keptCopy.confirmed_at,
    body: keptCopy.body,
  };
}

function keepCopy(store, copyKey, keptCopy) {
  if (store) store.write(copyKey, JSON.stringify(keptCopy));
}

/** `problem` as a warning reading `message`, its position and notes kept. */
function warningOf(problem, message) {
  const warning = new Diagnostic(problem.position, message);
  warning.severity = 'warning';
  warning.notes = problem.notes;
  return warning;
}

/** Whether a refusal states an optional field. */
function isStated(fieldValue) {
  return fieldValue !== null && fieldValue !== undefined;
}

function refusedProblem(problemAt, writtenLocation, readRefusals, credentialsAsked) {
  const refusedBy = readRefusals.length === 0 ? 'the source' : readRefusals[0].refusedBy;
  const reasons = readRefusals.map((refusal) => (isStated(refusal.credentialOrigin)
    ? `${refusal.credentialOrigin}: ${refusal.refusalReason}`
    : refusal.refusalReason));
  const fixes = [];
  for (const refusal of readRefusals) {
    if (isStated(refusal.renewalFix) && !fixes.includes(refusal.renewalFix)) fixes.push(refusal.renewalFix);
  }
  if (!credentialsAsked) fixes.push(MACHINE_CREDENTIALS_HELP);
  const problem = problemAt(`${refusedBy} refused to read ${codeSpan(writtenLocation)}`);
  if (reasons.length > 0) problem.withLabel(reasons.join(', '));
  return problem.withHelp(fixes.length === 0 ? null : fixes.join('; '));
}

function failedProblem(problemAt, writtenLocation, failureReason) {
  return problemAt(`reading ${codeSpan(writtenLocation)} failed`).withLabel(failureReason);
}

function refusalWarning(problemAt, refusal) {
  const problem = problemAt(`${refusal.refusedBy} refused ${refusal.credentialOrigin}`);
  problem.severity = 'warning';
  return problem.withLabel(refusal.refusalReason).withHelp(refusal.renewalFix);
}

function fileNameOf(fileUrl) {
  const lastStep = fileUrl.slice(fileUrl.lastIndexOf('/') + 1);
  const fileName = path.basename(fileUrlPath(`file:///${lastStep}`));
  return fileName === '' ? fileUrl : fileName;
}

/** The key a remote source's copy is kept under: a SHA-256 of its URL without the cache's parameters. */
function copyKeyOf(fetchUrl) {
  return crypto.createHash('sha256').update(fetchUrl, 'utf8').digest('hex');
}

module.exports = { documentSchemaFinding, readSchemaFinding, copyKeyOf, MACHINE_CREDENTIALS_HELP };
