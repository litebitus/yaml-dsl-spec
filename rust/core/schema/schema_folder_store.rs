//! The package's schema store for a caller keeping entries on disk: each entry a
//! file in a folder, the least recently used dropped past the capacity.

use std::fs;
use std::num::NonZeroUsize;
use std::path::{Path, PathBuf};
use std::time::SystemTime;

use super::schema_cache::SchemaStore;

const ENTRY_EXTENSION: &str = "json";

pub struct SchemaFolderStore {
    folder: PathBuf,
    capacity: NonZeroUsize,
}

impl SchemaFolderStore {
    pub fn new(folder: impl Into<PathBuf>, capacity: NonZeroUsize) -> Self {
        Self {
            folder: folder.into(),
            capacity,
        }
    }

    fn entry_path(&self, key: &str) -> PathBuf {
        self.folder.join(format!("{key}.{ENTRY_EXTENSION}"))
    }

    /// Drops the least recently used entries beyond the capacity.
    fn drop_least_recently_used(&self) {
        let Ok(listing) = fs::read_dir(&self.folder) else {
            return;
        };
        let mut entries: Vec<(SystemTime, PathBuf)> = listing
            .filter_map(Result::ok)
            .map(|entry| entry.path())
            .filter(|path| {
                path.extension()
                    .is_some_and(|extension| extension == ENTRY_EXTENSION)
            })
            .filter_map(|path| Some((fs::metadata(&path).ok()?.modified().ok()?, path)))
            .collect();
        entries.sort_by_key(|(used_at, _)| std::cmp::Reverse(*used_at));
        for (_, path) in entries.iter().skip(self.capacity.get()) {
            let _ = fs::remove_file(path);
        }
    }
}

impl SchemaStore for SchemaFolderStore {
    fn read(&self, key: &str) -> Option<String> {
        let path = self.entry_path(key);
        let text = fs::read_to_string(&path).ok()?;
        mark_used(&path);
        Some(text)
    }

    /// Writes the entry beside its place and moves it there, so a reader never
    /// reads half an entry.
    fn write(&self, key: &str, prepared: &str) {
        let path = self.entry_path(key);
        let partial = path.with_extension(format!("{}.partial", std::process::id()));
        let written = fs::create_dir_all(&self.folder)
            .and_then(|()| fs::write(&partial, prepared))
            .and_then(|()| fs::rename(&partial, &path));
        if written.is_err() {
            let _ = fs::remove_file(&partial);
            return;
        }
        self.drop_least_recently_used();
    }
}

/// Marks the entry at `path` used now, its modified time being when it was last used.
fn mark_used(path: &Path) {
    if let Ok(entry) = fs::File::options().write(true).open(path) {
        let _ = entry.set_modified(SystemTime::now());
    }
}

#[cfg(test)]
mod tests {
    use std::time::Duration;

    use super::*;

    struct ScratchFolder(PathBuf);

    impl ScratchFolder {
        fn new(name: &str) -> Self {
            let path = std::env::temp_dir().join(format!(
                "yaml-dsl-spec-folder-store-{name}-{}",
                std::process::id()
            ));
            let _ = fs::remove_dir_all(&path);
            Self(path)
        }
    }

    impl Drop for ScratchFolder {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    fn age(store: &SchemaFolderStore, key: &str, seconds_ago: u64) {
        let entry = fs::File::options()
            .write(true)
            .open(store.entry_path(key))
            .unwrap();
        entry
            .set_modified(SystemTime::now() - Duration::from_secs(seconds_ago))
            .unwrap();
    }

    #[test]
    fn an_entry_is_kept_as_a_file_and_read_back() {
        let scratch = ScratchFolder::new("read-back");
        let store = SchemaFolderStore::new(&scratch.0, NonZeroUsize::new(2).unwrap());
        assert_eq!(store.read("mock-key"), None);
        store.write("mock-key", "{\"mock\": 1}");
        assert_eq!(store.read("mock-key").as_deref(), Some("{\"mock\": 1}"));
        assert!(scratch.0.join("mock-key.json").is_file());
    }

    #[test]
    fn a_write_drops_the_least_recently_used_entries_beyond_the_capacity() {
        let scratch = ScratchFolder::new("capacity");
        let store = SchemaFolderStore::new(&scratch.0, NonZeroUsize::new(2).unwrap());
        store.write("oldest", "1");
        store.write("used", "2");
        age(&store, "oldest", 300);
        age(&store, "used", 200);
        store.read("used");
        store.write("newest", "3");
        assert_eq!(store.read("oldest"), None);
        assert_eq!(store.read("used").as_deref(), Some("2"));
        assert_eq!(store.read("newest").as_deref(), Some("3"));
    }
}
