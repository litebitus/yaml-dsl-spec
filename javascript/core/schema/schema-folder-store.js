'use strict';

/*
 * The package's schema store for a caller keeping entries on disk: each entry a
 * file in a folder, the least recently used dropped past the capacity.
 */

const fs = require('fs');
const path = require('path');

const ENTRY_EXTENSION = '.json';

class SchemaFolderStore {
  /** A store keeping each entry as a file in `folder`, at most `capacity`, a whole number from 1, of them. */
  constructor(folder, capacity) {
    if (!(Number.isInteger(capacity) && capacity >= 1)) {
      throw new Error(`a schema folder store's capacity is a whole number from 1, not ${capacity}`);
    }
    this.folder = folder;
    this.capacity = capacity;
  }

  entryPath(key) {
    return path.join(this.folder, `${key}${ENTRY_EXTENSION}`);
  }

  /** The entry written under `key`, marked used; `null` where the store holds none. */
  read(key) {
    const entryPath = this.entryPath(key);
    let entryText;
    try {
      entryText = fs.readFileSync(entryPath, 'utf8');
    } catch {
      return null;
    }
    markUsed(entryPath);
    return entryText;
  }

  /** Writes the entry beside its place and moves it there, so a reader never reads half an entry. */
  write(key, prepared) {
    const entryPath = this.entryPath(key);
    const partialPath = path.join(this.folder, `${key}.${process.pid}.partial`);
    try {
      fs.mkdirSync(this.folder, { recursive: true });
      fs.writeFileSync(partialPath, prepared);
      fs.renameSync(partialPath, entryPath);
    } catch {
      removeEntryFile(partialPath);
      return;
    }
    this.dropLeastRecentlyUsed();
  }

  /** Drops the least recently used entries beyond the capacity. */
  dropLeastRecentlyUsed() {
    let entryNames;
    try {
      entryNames = fs.readdirSync(this.folder);
    } catch {
      return;
    }
    const usedEntries = entryNames
      .filter((entryName) => path.extname(entryName) === ENTRY_EXTENSION)
      .map((entryName) => {
        const entryPath = path.join(this.folder, entryName);
        const status = fs.statSync(entryPath, { throwIfNoEntry: false });
        return status === undefined ? null : { usedAt: status.mtimeMs, entryPath };
      })
      .filter((usedEntry) => usedEntry !== null)
      .sort((first, second) => second.usedAt - first.usedAt);
    for (const { entryPath } of usedEntries.slice(this.capacity)) removeEntryFile(entryPath);
  }
}

/** Removes the file at `entryPath`, where one is there to remove. */
function removeEntryFile(entryPath) {
  try {
    fs.rmSync(entryPath, { force: true });
  } catch {
    return;
  }
}

/** Marks the entry at `entryPath` used now, its modified time being when it was last used. */
function markUsed(entryPath) {
  try {
    const now = new Date();
    fs.utimesSync(entryPath, now, now);
  } catch {
    return;
  }
}

module.exports = { SchemaFolderStore };
