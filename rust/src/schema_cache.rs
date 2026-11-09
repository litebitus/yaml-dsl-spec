//! Schemas loaded once per name and text: kept in memory, the least recently used
//! dropped first past the capacity the caller sets, and the information prepared
//! from each kept across runs in a store the caller supplies.

use std::collections::BTreeMap;
use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};
use std::num::NonZeroUsize;
use std::sync::{Arc, Mutex, OnceLock};

use sha2::{Digest, Sha256};

use crate::schema::{LoadedSchema, PreparedSchema};

/// Where a schema cache keeps, across runs, the information it prepares from a
/// schema's text, by the key the cache names: the caller's to place.
pub trait SchemaStore: Send + Sync {
    /// The prepared information written under `key`, if the store holds it.
    fn read(&self, key: &str) -> Option<String>;
    fn write(&self, key: &str, prepared: &str);
}

/// A schema loaded once, by the first reader that asks for it, or why it did not load.
type SchemaLoad = Arc<OnceLock<Result<Arc<LoadedSchema>, String>>>;

/// The schemas held in memory, each with the turn it was last asked for.
#[derive(Default)]
struct HeldSchemas {
    by_key: BTreeMap<(String, u64), (SchemaLoad, u64)>,
    turn: u64,
}

/// Schemas loaded once and shared by every reader that asks for them, by name and text.
#[derive(Default)]
pub struct SchemaCache {
    held: Mutex<HeldSchemas>,
    capacity: Option<NonZeroUsize>,
    store: Option<Arc<dyn SchemaStore>>,
}

impl SchemaCache {
    /// The cache holding at most `capacity` schemas, the least recently asked for
    /// dropped first; without it, every schema loaded is held.
    pub fn with_capacity(mut self, capacity: NonZeroUsize) -> Self {
        self.capacity = Some(capacity);
        self
    }

    /// The cache keeping what it prepares from each schema in `store`, and reading
    /// it from there before preparing a schema again.
    pub fn with_store(mut self, store: Arc<dyn SchemaStore>) -> Self {
        self.store = Some(store);
        self
    }

    /// The schema `text` holds, named `name` in reports, or why it holds none.
    pub fn load(&self, name: &str, text: &str) -> Result<Arc<LoadedSchema>, String> {
        let mut hasher = DefaultHasher::new();
        text.hash(&mut hasher);
        let key = (name.to_owned(), hasher.finish());
        let cell = {
            let mut held = self
                .held
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            held.turn += 1;
            let turn = held.turn;
            let cell = {
                let entry = held.by_key.entry(key.clone()).or_default();
                entry.1 = turn;
                Arc::clone(&entry.0)
            };
            if let Some(capacity) = self.capacity {
                while held.by_key.len() > capacity.get() {
                    let Some(oldest) = held
                        .by_key
                        .iter()
                        .filter(|(held_key, _)| **held_key != key)
                        .min_by_key(|(_, (_, last_asked))| *last_asked)
                        .map(|(held_key, _)| held_key.clone())
                    else {
                        break;
                    };
                    held.by_key.remove(&oldest);
                }
            }
            cell
        };
        cell.get_or_init(|| self.prepared_load(name, text).map(Arc::new))
            .clone()
    }

    /// The schema built from the information the store holds for it, else from its
    /// text, the information prepared then written to the store.
    fn prepared_load(&self, name: &str, text: &str) -> Result<LoadedSchema, String> {
        let Some(store) = &self.store else {
            return LoadedSchema::from_prepared(name, PreparedSchema::of(name, text)?);
        };
        let key = store_key(name, text);
        if let Some(stored) = store.read(&key)
            && let Ok(prepared) = serde_json::from_str::<PreparedSchema>(&stored)
            && let Ok(loaded) = LoadedSchema::from_prepared(name, prepared)
        {
            return Ok(loaded);
        }
        let prepared = PreparedSchema::of(name, text)?;
        if let Ok(written) = serde_json::to_string(&prepared) {
            store.write(&key, &written);
        }
        LoadedSchema::from_prepared(name, prepared)
    }
}

/// The key a schema's prepared information is stored under: its name, its text and
/// the version of the package that prepared it, so another version never reads it.
fn store_key(name: &str, text: &str) -> String {
    let mut digest = Sha256::new();
    for part in ["yaml-dsl-spec", env!("CARGO_PKG_VERSION"), name, text] {
        digest.update(part.as_bytes());
        digest.update([0]);
    }
    digest
        .finalize()
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

#[cfg(test)]
mod tests {
    use std::sync::Mutex;

    use super::*;

    const SCHEMA: &str = r#"{"type": "object", "properties": {"name": {"type": "string"}}}"#;
    const OTHER_SCHEMA: &str = r#"{"type": "object", "required": ["name"]}"#;

    /// A store in memory, counting what it is asked to read and write.
    #[derive(Default)]
    struct MockStore {
        entries: Mutex<BTreeMap<String, String>>,
        reads: Mutex<usize>,
        writes: Mutex<usize>,
    }

    impl SchemaStore for MockStore {
        fn read(&self, key: &str) -> Option<String> {
            *self.reads.lock().unwrap() += 1;
            self.entries.lock().unwrap().get(key).cloned()
        }

        fn write(&self, key: &str, prepared: &str) {
            *self.writes.lock().unwrap() += 1;
            self.entries
                .lock()
                .unwrap()
                .insert(key.to_owned(), prepared.to_owned());
        }
    }

    #[test]
    fn a_schema_is_prepared_once_and_read_from_the_store_by_a_later_cache() {
        let store = Arc::new(MockStore::default());
        let first = SchemaCache::default().with_store(store.clone());
        first.load("mock.schema.json", SCHEMA).unwrap();
        assert_eq!(*store.writes.lock().unwrap(), 1);
        let stored: serde_json::Value =
            serde_json::from_str(store.entries.lock().unwrap().values().next().unwrap()).unwrap();
        assert_eq!(stored["draft"], "draft-07");
        assert_eq!(stored["schema"]["type"], "object");
        assert!(stored["marked"].is_object());

        let later = SchemaCache::default().with_store(store.clone());
        later.load("mock.schema.json", SCHEMA).unwrap();
        assert_eq!(*store.writes.lock().unwrap(), 1);
        assert_eq!(*store.reads.lock().unwrap(), 2);
    }

    #[test]
    fn a_stored_entry_that_does_not_read_is_prepared_again_from_the_text() {
        let store = Arc::new(MockStore::default());
        store.write(&store_key("mock.schema.json", SCHEMA), "not prepared");
        let loaded = SchemaCache::default()
            .with_store(store.clone())
            .load("mock.schema.json", SCHEMA);
        assert!(loaded.is_ok());
        assert_eq!(*store.writes.lock().unwrap(), 2);
    }

    #[test]
    fn the_least_recently_asked_for_schema_is_dropped_past_the_capacity() {
        let cache = SchemaCache::default().with_capacity(NonZeroUsize::new(2).unwrap());
        let first = cache.load("first.json", SCHEMA).unwrap();
        let second = cache.load("second.json", SCHEMA).unwrap();
        cache.load("first.json", SCHEMA).unwrap();
        cache.load("third.json", OTHER_SCHEMA).unwrap();
        assert!(Arc::ptr_eq(
            &first,
            &cache.load("first.json", SCHEMA).unwrap()
        ));
        assert!(!Arc::ptr_eq(
            &second,
            &cache.load("second.json", SCHEMA).unwrap()
        ));
    }

    #[test]
    fn the_store_key_names_the_schema_its_text_and_the_package_version() {
        let key = store_key("mock.schema.json", SCHEMA);
        assert_eq!(key.len(), 64);
        assert_eq!(key, store_key("mock.schema.json", SCHEMA));
        assert_ne!(key, store_key("other.schema.json", SCHEMA));
        assert_ne!(key, store_key("mock.schema.json", OTHER_SCHEMA));
    }
}
