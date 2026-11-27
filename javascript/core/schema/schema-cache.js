'use strict';

/*
 * Schemas loaded once per name and text: kept in memory, the least recently used
 * dropped first past the capacity the caller sets, and the information prepared
 * from each kept across runs in a store the caller supplies; and the readers a
 * schema's URL is read by.
 */

const crypto = require('crypto');
const { LoadedSchema } = require('./schema-validation');
const { FileSchemaReader } = require('./resolution/file-schema-reader');
const { HttpSchemaReader } = require('./resolution/http-schema-reader');
const { documentSchemaFinding, readSchemaFinding } = require('./resolution/schema-finding');
const { version: PACKAGE_VERSION } = require('../../package.json');

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
   * `write(key, prepared)` keeps it. `urlReaders` read the URLs of their prefixes
   * beside the core's; a second reader of a prefix throws.
   */
  constructor({ capacity = null, store = null, urlReaders = [] } = {}) {
    if (capacity !== null && !(Number.isInteger(capacity) && capacity >= 1)) {
      throw new Error(`a schema cache's capacity is a whole number from 1, not ${capacity}`);
    }
    this.capacity = capacity;
    this.store = store;
    this.held = new Map();
    this.urlReaders = [new FileSchemaReader(), new HttpSchemaReader()];
    this.readTurns = new Map();
    for (const reader of urlReaders) {
      for (const prefix of reader.urlPrefixes) {
        const taken = this.urlReaders.some((registered) => registered.urlPrefixes
          .some((registeredPrefix) => registeredPrefix.toLowerCase() === prefix.toLowerCase()));
        if (taken) throw new Error(`a reader of \`${prefix}\` URLs is already registered`);
      }
      this.urlReaders.push(reader);
    }
  }

  /**
   * A promise of the schema finding of the document `file`, whose text is `text`:
   * its modeline's location tried, then each search path, `{ pathText, writtenAt }`,
   * in order, a path resolved against the document's folder, or `configRoot`
   * where it starts with `{config_root}`.
   */
  documentSchema(file, text, searchPaths, configRoot) {
    return documentSchemaFinding(this, file, text, searchPaths, configRoot);
  }

  /**
   * A promise of the schema finding of a location named outside a document: a
   * path resolved against the working directory, a problem placed at `writtenAt`.
   */
  readSchema(location, writtenAt = null) {
    return readSchemaFinding(this, location, writtenAt);
  }

  /** The reader of the longest prefix `url` starts with, `null` where the cache has none. */
  urlReader(url) {
    let longestReader = null;
    let longestLength = -1;
    for (const reader of this.urlReaders) {
      for (const prefix of reader.urlPrefixes) {
        const startsWithPrefix = url.slice(0, prefix.length).toLowerCase() === prefix.toLowerCase();
        if (startsWithPrefix && prefix.length > longestLength) {
          longestReader = reader;
          longestLength = prefix.length;
        }
      }
    }
    return longestReader;
  }

  /** What `work` comes to, run once the reads of `fetchUrl` before it have finished. */
  async readInTurn(fetchUrl, work) {
    const readBefore = this.readTurns.get(fetchUrl) || Promise.resolve();
    let finishTurn;
    const thisTurn = new Promise((resolve) => {
      finishTurn = resolve;
    });
    const turnsQueued = readBefore.then(() => thisTurn);
    this.readTurns.set(fetchUrl, turnsQueued);
    await readBefore;
    try {
      return await work();
    } finally {
      finishTurn();
      if (this.readTurns.get(fetchUrl) === turnsQueued) this.readTurns.delete(fetchUrl);
    }
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
