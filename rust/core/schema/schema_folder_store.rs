//! The package's schema store for a caller keeping entries on disk: each entry a
//! file in a folder, the least recently used dropped past the capacity.

use std::fs;
use std::io::ErrorKind;
use std::num::NonZeroUsize;
use std::path::{Path, PathBuf};
use std::time::SystemTime;

use super::schema_cache::SchemaStore;
use crate::problem_wording::code_span;

const ENTRY_EXTENSION: &str = "json";

pub struct SchemaFolderStore {
    folder: PathBuf,
    capacity: NonZeroUsize,
    report_failure: Box<dyn Fn(&str) + Send + Sync>,
}

impl SchemaFolderStore {
    /// A store keeping at most `capacity` entries in `folder`, each entry it
    /// fails to save or drop reported to `report_failure`.
    pub fn new(
        folder: impl Into<PathBuf>,
        capacity: NonZeroUsize,
        report_failure: impl Fn(&str) + Send + Sync + 'static,
    ) -> Self {
        Self {
            folder: folder.into(),
            capacity,
            report_failure: Box::new(report_failure),
        }
    }

    fn entry_path(&self, key: &str) -> PathBuf {
        self.folder.join(format!("{key}.{ENTRY_EXTENSION}"))
    }

    /// Reports a failure on the file or folder at `path`.
    fn report(&self, failed_action: &str, path: &Path, error: &std::io::Error) {
        let named_path = code_span(&path.display().to_string());
        (self.report_failure)(&format!("failed to {failed_action} {named_path}: {error}"));
    }

    /// Drops the least recently used entries beyond the capacity.
    fn drop_least_recently_used(&self) {
        let listing = match fs::read_dir(&self.folder) {
            Ok(listing) => listing,
            Err(error) => {
                self.report("list schema store folder", &self.folder, &error);
                return;
            }
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
            if let Err(error) = fs::remove_file(path)
                && error.kind() != ErrorKind::NotFound
            {
                self.report("drop schema store entry", path, &error);
            }
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
        if let Err(error) = written {
            let _ = fs::remove_file(&partial);
            self.report("save schema store entry", &path, &error);
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
    use std::sync::{Arc, Mutex};
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
            let _ = fs::remove_file(&path);
            Self(path)
        }
    }

    impl Drop for ScratchFolder {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
            let _ = fs::remove_file(&self.0);
        }
    }

    /// A store in `folder` keeping `capacity` entries, and the failures it reports.
    fn reporting_store(
        folder: &Path,
        capacity: usize,
    ) -> (SchemaFolderStore, Arc<Mutex<Vec<String>>>) {
        let reported_failures = Arc::new(Mutex::new(Vec::new()));
        let failure_sink = Arc::clone(&reported_failures);
        let store = SchemaFolderStore::new(
            folder,
            NonZeroUsize::new(capacity).unwrap(),
            move |failure: &str| failure_sink.lock().unwrap().push(failure.to_owned()),
        );
        (store, reported_failures)
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
        let (store, reported_failures) = reporting_store(&scratch.0, 2);
        assert_eq!(store.read("mock-key"), None);
        store.write("mock-key", "{\"mock\": 1}");
        assert_eq!(store.read("mock-key").as_deref(), Some("{\"mock\": 1}"));
        assert!(scratch.0.join("mock-key.json").is_file());
        assert!(reported_failures.lock().unwrap().is_empty());
    }

    #[test]
    fn an_entry_that_cannot_be_saved_is_reported() {
        let scratch = ScratchFolder::new("unsaved");
        fs::write(&scratch.0, "a file where the folder goes").unwrap();
        let (store, reported_failures) = reporting_store(&scratch.0, 2);
        store.write("mock-key", "{\"mock\": 1}");
        assert_eq!(store.read("mock-key"), None);
        let reported_failures = reported_failures.lock().unwrap();
        assert_eq!(reported_failures.len(), 1);
        let entry_path = scratch.0.join("mock-key.json").display().to_string();
        assert!(
            reported_failures[0].starts_with(&format!(
                "failed to save schema store entry `{entry_path}`: "
            )),
            "{reported_failures:?}"
        );
    }

    #[test]
    fn a_write_drops_the_least_recently_used_entries_beyond_the_capacity() {
        let scratch = ScratchFolder::new("capacity");
        let (store, reported_failures) = reporting_store(&scratch.0, 2);
        store.write("oldest", "1");
        store.write("used", "2");
        age(&store, "oldest", 300);
        age(&store, "used", 200);
        store.read("used");
        store.write("newest", "3");
        assert_eq!(store.read("oldest"), None);
        assert_eq!(store.read("used").as_deref(), Some("2"));
        assert_eq!(store.read("newest").as_deref(), Some("3"));
        assert!(reported_failures.lock().unwrap().is_empty());
    }
}
