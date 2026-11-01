'use strict';

const fs = require('fs');
const path = require('path');

const SCHEMA_FOLDER = path.join(__dirname, 'schemas');

/** The `[major, minor]` of a `<family>-v<major>.<minor>.schema.json` file name. */
function minorVersionOf(fileName, family) {
  const prefix = `${family}-v`;
  const suffix = '.schema.json';
  if (!fileName.startsWith(prefix) || !fileName.endsWith(suffix)) {
    return null;
  }
  const match = /^(\d+)\.(\d+)$/.exec(fileName.slice(prefix.length, -suffix.length));
  return match ? [Number(match[1]), Number(match[2])] : null;
}

/**
 * Each version of `family` among `fileNames`, each major before its minors, with
 * the file it is: a minor is its own file, and a major is its newest minor's.
 */
function versionFiles(fileNames, family) {
  const minors = fileNames
    .map((fileName) => ({ fileName, version: minorVersionOf(fileName, family) }))
    .filter((entry) => entry.version !== null)
    .sort((first, second) =>
      first.version[0] - second.version[0] || first.version[1] - second.version[1]);
  const entries = [];
  minors.forEach(({ fileName, version: [major, minor] }, index) => {
    if (index === 0 || minors[index - 1].version[0] !== major) {
      const newest = minors.filter((entry) => entry.version[0] === major).at(-1);
      entries.push([String(major), newest.fileName]);
    }
    entries.push([`${major}.${minor}`, fileName]);
  });
  return entries;
}

/** Each version of `family` the package carries, with the path of its file. */
function packagedVersionFiles(family) {
  return versionFiles(fs.readdirSync(SCHEMA_FOLDER), family)
    .map(([version, fileName]) => [version, path.join(SCHEMA_FOLDER, fileName)]);
}

module.exports = { versionFiles, packagedVersionFiles };
