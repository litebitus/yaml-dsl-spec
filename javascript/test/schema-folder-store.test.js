'use strict';

const assert = require('node:assert');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const test = require('node:test');

const { schemaValidation } = require('..');

const { SchemaFolderStore } = schemaValidation;

/** Sets when the entry under `key` was last used to `secondsAgo` seconds ago. */
function age(store, key, secondsAgo) {
  const usedAt = new Date(Date.now() - secondsAgo * 1000);
  fs.utimesSync(store.entryPath(key), usedAt, usedAt);
}

test.describe('the folder store', () => {
  let scratchFolder;
  let reportedFailures;

  /** A store in `folder` keeping `capacity` entries, its failures reported to `reportedFailures`. */
  function reportingStore(folder, capacity) {
    return new SchemaFolderStore(folder, capacity, (failureMessage) => reportedFailures.push(failureMessage));
  }

  test.beforeEach(() => {
    scratchFolder = fs.mkdtempSync(path.join(os.tmpdir(), 'yaml-dsl-spec-folder-store-'));
    reportedFailures = [];
  });

  test.afterEach(() => {
    fs.rmSync(scratchFolder, { recursive: true, force: true });
  });

  test('an entry is kept as a file and read back', () => {
    const store = reportingStore(path.join(scratchFolder, 'entries'), 2);
    assert.strictEqual(store.read('mock-key'), null);
    store.write('mock-key', '{"mock": 1}');
    assert.strictEqual(store.read('mock-key'), '{"mock": 1}');
    assert.ok(fs.statSync(path.join(scratchFolder, 'entries', 'mock-key.json')).isFile());
    assert.deepStrictEqual(reportedFailures, []);
  });

  test('a write drops the least recently used entries beyond the capacity', () => {
    const store = reportingStore(scratchFolder, 2);
    store.write('oldest', '1');
    store.write('used', '2');
    age(store, 'oldest', 300);
    age(store, 'used', 200);
    store.read('used');
    store.write('newest', '3');
    assert.strictEqual(store.read('oldest'), null);
    assert.strictEqual(store.read('used'), '2');
    assert.strictEqual(store.read('newest'), '3');
    assert.deepStrictEqual(reportedFailures, []);
  });

  test('a write that cannot land leaves no entry and is reported, and a capacity below one is refused', () => {
    const blockingFile = path.join(scratchFolder, 'blocking');
    fs.writeFileSync(blockingFile, '');
    const store = reportingStore(blockingFile, 1);
    store.write('mock-key', '{}');
    assert.strictEqual(store.read('mock-key'), null);
    assert.strictEqual(reportedFailures.length, 1);
    const savePrefix = `failed to save schema store entry \`${path.join(blockingFile, 'mock-key.json')}\`: `;
    assert.ok(reportedFailures[0].startsWith(savePrefix), reportedFailures[0]);
    assert.throws(() => reportingStore(scratchFolder, 0), /capacity is a whole number from 1, not 0/);
  });
});
