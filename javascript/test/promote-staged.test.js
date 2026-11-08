'use strict';

const assert = require('node:assert');
const test = require('node:test');

const {
  abandonedSnapshotsOf,
  fieldValueOf,
  stagedEntriesOf,
  stagedNewestFirstOf,
} = require('../scripts/promote-staged');

const stageListText = [
  'id: mock-stage-old',
  'package name: mock-package',
  'version: 1.2.3-snapshot.20260101000000',
  'status: staged',
  'tag: snapshot',
  'id: mock-stage-older-patch',
  'package name: mock-package',
  'version: 1.2.2-snapshot.20261231000000',
  'status: staged',
  'id: mock-stage-newer',
  'package name: mock-package',
  'version: 1.2.3-snapshot.20260301000000',
  'status: staged',
  'id: mock-stage-release',
  'package name: mock-package',
  'version: 1.2.2',
  'status: staged',
  'id: mock-stage-promoted',
  'package name: mock-package',
  'version: 1.2.3-snapshot.20260201000000',
  'status: staged',
].join('\n');

test('each staged entry is read with its id and version', () => {
  assert.deepEqual(stagedEntriesOf(stageListText).map(({ id }) => id), [
    'mock-stage-old',
    'mock-stage-older-patch',
    'mock-stage-newer',
    'mock-stage-release',
    'mock-stage-promoted',
  ]);
  assert.equal(fieldValueOf('id: mock-stage\nversion: 1.2.3\n', 'version'), '1.2.3');
  assert.equal(fieldValueOf('id: mock-stage\n', 'version'), null);
});

test('promoting a snapshot abandons only the staged snapshots below it, never a release', () => {
  const abandoned = abandonedSnapshotsOf(stageListText, '1.2.3-snapshot.20260201000000');
  assert.deepEqual(abandoned.map(({ id }) => id), ['mock-stage-old', 'mock-stage-older-patch']);
});

test('promoting a release abandons every staged snapshot of it and below', () => {
  const abandoned = abandonedSnapshotsOf(stageListText, '1.2.3');
  assert.deepEqual(abandoned.map(({ id }) => id), [
    'mock-stage-old',
    'mock-stage-older-patch',
    'mock-stage-newer',
    'mock-stage-promoted',
  ]);
});

test('the staged versions list newest first, a release above its snapshots, each with its own lines', () => {
  const listed = stagedNewestFirstOf(`${stageListText}\nid: mock-stage-unversioned\nstatus: staged`);
  assert.deepEqual(listed.map(({ id }) => id), [
    'mock-stage-newer',
    'mock-stage-promoted',
    'mock-stage-old',
    'mock-stage-release',
    'mock-stage-older-patch',
    'mock-stage-unversioned',
  ]);
  assert.deepEqual(listed[0].entryLines, [
    'id: mock-stage-newer',
    'package name: mock-package',
    'version: 1.2.3-snapshot.20260301000000',
    'status: staged',
  ]);
});

test('a version outside the package version form abandons nothing', () => {
  assert.deepEqual(abandonedSnapshotsOf(stageListText, 'mock'), []);
  assert.deepEqual(abandonedSnapshotsOf('id: mock-stage\nversion: mock\n', '1.2.3'), []);
});
