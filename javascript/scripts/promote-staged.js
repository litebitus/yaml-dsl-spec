'use strict';

const { execFileSync } = require('child_process');
const readline = require('readline');

const PACKAGE_VERSION = /^(\d+)\.(\d+)\.(\d+)(?:-snapshot\.(\d+))?$/;

function fieldValueOf(entryText, fieldName) {
  const prefix = `${fieldName}: `;
  const fieldLine = entryText.split('\n').find((line) => line.startsWith(prefix));
  return fieldLine === undefined ? null : fieldLine.slice(prefix.length).trim();
}

function stagedEntriesOf(stageListText) {
  const stagedEntries = [];
  for (const line of stageListText.split('\n')) {
    if (line.startsWith('id: ')) {
      stagedEntries.push({ id: line.slice('id: '.length).trim(), version: null, entryLines: [] });
    }
    if (stagedEntries.length === 0) continue;
    const currentEntry = stagedEntries[stagedEntries.length - 1];
    currentEntry.entryLines.push(line);
    if (line.startsWith('version: ')) currentEntry.version = line.slice('version: '.length).trim();
  }
  return stagedEntries;
}

function versionRankOf(version) {
  const versionMatch = PACKAGE_VERSION.exec(version || '');
  if (!versionMatch) return null;
  const [, major, minor, patch, snapshotStamp] = versionMatch;
  const snapshotRank = snapshotStamp === undefined ? Infinity : Number(snapshotStamp);
  return [Number(major), Number(minor), Number(patch), snapshotRank];
}

function ranksBelow(versionRank, promotedRank) {
  for (let rankIndex = 0; rankIndex < versionRank.length; rankIndex += 1) {
    if (versionRank[rankIndex] !== promotedRank[rankIndex]) {
      return versionRank[rankIndex] < promotedRank[rankIndex];
    }
  }
  return false;
}

function abandonedSnapshotsOf(stageListText, promotedVersion) {
  const promotedRank = versionRankOf(promotedVersion);
  if (promotedRank === null) return [];
  return stagedEntriesOf(stageListText).filter(({ version }) => {
    const versionRank = versionRankOf(version);
    return versionRank !== null && versionRank[3] !== Infinity && ranksBelow(versionRank, promotedRank);
  });
}

function compareNewestFirst(firstEntry, secondEntry) {
  const firstRank = versionRankOf(firstEntry.version);
  const secondRank = versionRankOf(secondEntry.version);
  if (firstRank === null || secondRank === null) return (firstRank === null) - (secondRank === null);
  if (ranksBelow(secondRank, firstRank)) return -1;
  return ranksBelow(firstRank, secondRank) ? 1 : 0;
}

function stagedNewestFirstOf(stageListText) {
  return stagedEntriesOf(stageListText).sort(compareNewestFirst);
}

function npmOutputOf(npmArguments) {
  return execFileSync('npm', npmArguments, { encoding: 'utf8' });
}

function oneTimePasswordOf() {
  const passwordPrompt = readline.createInterface({ input: process.stdin, output: process.stderr });
  return new Promise((resolve) => {
    passwordPrompt.question('one-time password: ', (oneTimePassword) => {
      passwordPrompt.close();
      resolve(oneTimePassword.trim());
    });
  });
}

async function promoteStaged(packageName, stageId) {
  const promotedVersion = fieldValueOf(npmOutputOf(['stage', 'view', stageId]), 'version');
  const oneTimePassword = await oneTimePasswordOf();
  execFileSync('npm', ['stage', 'approve', stageId, '--otp', oneTimePassword], { stdio: 'inherit' });
  console.log(`promoted ${packageName} ${promotedVersion}, stage ${stageId}`);
  const abandonedSnapshots = abandonedSnapshotsOf(npmOutputOf(['stage', 'list', packageName]), promotedVersion);
  for (const { id, version } of abandonedSnapshots) {
    execFileSync('npm', ['stage', 'reject', id, '--otp', oneTimePassword], { stdio: 'inherit' });
    console.log(`rejected ${packageName} ${version}, stage ${id}, abandoned by ${promotedVersion}`);
  }
}

function listStagedNewestFirst(packageName) {
  const stageListText = npmOutputOf(['stage', 'list', packageName]);
  const stagedEntries = stagedNewestFirstOf(stageListText);
  console.log(stagedEntries.length === 0
    ? stageListText.trim()
    : stagedEntries.map(({ entryLines }) => entryLines.join('\n').trim()).join('\n\n'));
}

if (require.main === module) {
  const [packageName, stageId] = process.argv.slice(2);
  if (!packageName) {
    console.error('usage: promote-staged.js <package> [<stage id>]');
    process.exit(2);
  }
  if (!stageId) {
    listStagedNewestFirst(packageName);
  } else {
    promoteStaged(packageName, stageId).catch((promoteError) => {
      console.error(promoteError.message);
      process.exit(1);
    });
  }
}

module.exports = { abandonedSnapshotsOf, fieldValueOf, stagedEntriesOf, stagedNewestFirstOf };
