'use strict';

const assert = require('node:assert');
const test = require('node:test');

const { SchemaCache, storeKey } = require('../lib/schema-cache');

const SCHEMA = '{"type": "object", "properties": {"name": {"type": "string"}}}';
const OTHER_SCHEMA = '{"type": "object", "required": ["name"]}';

/** A store in memory, counting what it is asked to read and write. */
function mockStore() {
  const entries = new Map();
  const counts = { reads: 0, writes: 0 };
  return {
    entries,
    counts,
    read(key) {
      counts.reads += 1;
      return entries.has(key) ? entries.get(key) : null;
    },
    write(key, prepared) {
      counts.writes += 1;
      entries.set(key, prepared);
    },
  };
}

test('a schema is prepared once and read from the store by a later cache', () => {
  const store = mockStore();
  new SchemaCache({ store }).load('mock.schema.json', SCHEMA);
  assert.strictEqual(store.counts.writes, 1);
  const stored = JSON.parse([...store.entries.values()][0]);
  assert.strictEqual(stored.draft, 'draft-07');
  assert.strictEqual(stored.schema.type, 'object');
  assert.strictEqual(typeof stored.marked, 'object');

  new SchemaCache({ store }).load('mock.schema.json', SCHEMA);
  assert.strictEqual(store.counts.writes, 1);
  assert.strictEqual(store.counts.reads, 2);
});

test('a stored entry that does not read is prepared again from the text', () => {
  const store = mockStore();
  store.write(storeKey('mock.schema.json', SCHEMA), 'not prepared');
  assert.ok(new SchemaCache({ store }).load('mock.schema.json', SCHEMA));
  assert.strictEqual(store.counts.writes, 2);
});

test('the least recently asked for schema is dropped past the capacity', () => {
  const cache = new SchemaCache({ capacity: 2 });
  const first = cache.load('first.json', SCHEMA);
  const second = cache.load('second.json', SCHEMA);
  cache.load('first.json', SCHEMA);
  cache.load('third.json', OTHER_SCHEMA);
  assert.strictEqual(cache.load('first.json', SCHEMA), first);
  assert.notStrictEqual(cache.load('second.json', SCHEMA), second);
});

test('a capacity below one is refused', () => {
  assert.throws(() => new SchemaCache({ capacity: 0 }), /capacity is a whole number from 1, not 0/);
});

test('the store key names the schema, its text and the package version', () => {
  const key = storeKey('mock.schema.json', SCHEMA);
  assert.match(key, /^[0-9a-f]{64}$/);
  assert.strictEqual(key, storeKey('mock.schema.json', SCHEMA));
  assert.notStrictEqual(key, storeKey('other.schema.json', SCHEMA));
  assert.notStrictEqual(key, storeKey('mock.schema.json', OTHER_SCHEMA));
});
