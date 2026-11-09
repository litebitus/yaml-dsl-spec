'use strict';

/*
 * Schemas loaded once per name and text: kept in memory, the least recently used
 * dropped first past the capacity the caller sets, and the information prepared
 * from each kept across runs in a store the caller supplies.
 */

const crypto = require('crypto');
const { LoadedSchema } = require('./schema-validation');
const { version: PACKAGE_VERSION } = require('../package.json');

/**
 * The key a schema's prepared information is stored under: its name, its text and
 * the version of the package that prepared it, so another version never reads it.
 */
function storeKey(name, text) {
  const digest = crypto.createHash('sha256');
  for (const part of ['yaml-dsl-spec', PACKAGE_VERSION, name, text]) {
    digest.update(part, 'utf8');
    digest.update(Buffer.from([0]));
  }
  return digest.digest('hex');
}

/** The schema built from information a store kept, or `null` where it does not read. */
function restoredSchema(name, stored) {
  try {
    return LoadedSchema.fromPrepared(name, JSON.parse(stored));
  } catch {
    return null;
  }
}

/** Schemas loaded once and shared by every reader that asks for them, by name and text. */
class SchemaCache {
  /**
   * `capacity`, a whole number from one, holds at most that many schemas, the
   * least recently asked for dropped first; without it, every schema loaded is
   * held. `store`, the caller's, keeps what the cache prepares from each schema
   * across runs: `read(key)` gives the text written under `key`, or `null`, and
   * `write(key, prepared)` keeps it.
   */
  constructor({ capacity = null, store = null } = {}) {
    if (capacity !== null && !(Number.isInteger(capacity) && capacity >= 1)) {
      throw new Error(`a schema cache's capacity is a whole number from 1, not ${capacity}`);
    }
    this.capacity = capacity;
    this.store = store;
    this.held = new Map();
  }

  /** The schema `text` holds, named `name` in reports, or throws why it holds none. */
  load(name, text) {
    const key = JSON.stringify([name, text]);
    let load = this.held.get(key);
    if (load) {
      this.held.delete(key);
    } else {
      try {
        load = { schema: this.preparedLoad(name, text) };
      } catch (error) {
        load = { error };
      }
    }
    this.held.set(key, load);
    if (this.capacity !== null) {
      for (const heldKey of this.held.keys()) {
        if (this.held.size <= this.capacity) break;
        this.held.delete(heldKey);
      }
    }
    if (load.error) throw load.error;
    return load.schema;
  }

  /**
   * The schema built from the information the store holds for it, else from its
   * text, the information prepared then written to the store.
   */
  preparedLoad(name, text) {
    if (!this.store) return LoadedSchema.fromPrepared(name, LoadedSchema.prepared(name, text));
    const key = storeKey(name, text);
    const stored = this.store.read(key);
    const restored = stored === null || stored === undefined ? null : restoredSchema(name, stored);
    if (restored) return restored;
    const prepared = LoadedSchema.prepared(name, text);
    this.store.write(key, JSON.stringify(prepared));
    return LoadedSchema.fromPrepared(name, prepared);
  }
}

module.exports = { SchemaCache, storeKey };
