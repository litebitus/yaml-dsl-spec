//! A document's schema found and read through a schema cache: its modeline's
//! location, then each search path, each read by the reader of its URL's prefix,
//! a remote source's text kept as a copy in the cache's store.

use std::path::{Component, Path, PathBuf};
use std::sync::{Arc, PoisonError};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};
use serde_json::Value as JsonValue;
use sha2::{Digest, Sha256};

use super::schema_locations::{
    FILE_SCHEME, ResolvedLocation, file_url_of, file_url_path, first_matching_file, has_pattern,
    modeline_of, resolve_location,
};
use super::schema_url_reading::{
    ReadRefusal, SchemaUrlAnswer, SchemaUrlOutcome, SchemaUrlReader, SchemaUrlRequest,
};
use crate::diagnostic::{Diagnostic, Severity};
use crate::position::Position;
use crate::problem_wording::code_span;
use crate::schema::SchemaCache;

const READ_LIMIT: Duration = Duration::from_secs(60);
const DAY_SECONDS: u64 = 24 * 60 * 60;
const NO_MATCH_REASON: &str = "no file matches";
const MACHINE_CREDENTIALS_HELP: &str =
    "add `yaml_dsl_machine_credentials=true` to read it with the machine's credentials";

/// A `schema_search_paths` entry, and the position it is written at.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SchemaSearchPath {
    pub path_text: String,
    pub written_at: Option<Position>,
}

/// Each candidate tried, in order; the schema found, if one is; and the finding's
/// problems, its warnings among them.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SchemaFinding {
    pub tried_candidates: Vec<SchemaCandidate>,
    pub found_schema: Option<FoundSchema>,
    pub finding_problems: Vec<Diagnostic>,
}

/// A location as resolved, and what named it.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SchemaCandidate {
    pub schema_location: String,
    pub named_by: CandidateNamer,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum CandidateNamer {
    Modeline { position: Position },
    SearchPath { index: usize },
    Caller,
}

/// The name the schema loads under, its text, the fragment its location names,
/// and the candidate it was read from.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct FoundSchema {
    pub schema_name: String,
    pub schema_text: String,
    pub schema_fragment: Option<String>,
    pub found_candidate: SchemaCandidate,
}

/// A copy of a remote source's text, in the form every language keeps it in.
#[derive(Clone, Serialize, Deserialize)]
struct KeptCopy {
    url: String,
    copy_version: JsonValue,
    confirmed_at: u64,
    body: String,
}

/// What reading one candidate came to.
enum CandidateRead {
    TextRead {
        schema_text: String,
        schema_location: String,
    },
    SourceMissing {
        missing_reason: String,
        schema_location: String,
    },
    ReadProblem(Diagnostic),
}

/// A candidate whose source is not there, as written, and why.
struct MissingCandidate {
    written_location: String,
    missing_reason: String,
}

/// A candidate before it is read: what it says, where it says it, and what named it.
struct NamedLocation {
    resolved: ResolvedLocation,
    written_at: Option<Position>,
    named_by: CandidateNamer,
}

impl SchemaCache {
    /// The schema of the document `file`, whose text is `text`: its modeline's
    /// location tried, then each search path in order, a path resolved against
    /// the document's folder, or `config_root` where it starts with `{config_root}`.
    pub fn document_schema(
        &self,
        file: &Path,
        text: &str,
        search_paths: &[SchemaSearchPath],
        config_root: &Path,
    ) -> SchemaFinding {
        let shown_file: Arc<str> = Arc::from(file.to_string_lossy().as_ref());
        let folder = absolute(file.parent().unwrap_or(Path::new("")));
        let config_root = absolute(config_root);
        let modeline = modeline_of(text);
        let fallback_position = Position::new(
            &shown_file,
            modeline.as_ref().map_or(1, |modeline| modeline.line),
            1,
        );
        let mut named = Vec::new();
        if let Some(modeline) = &modeline {
            let position = Position::new(&shown_file, modeline.line, modeline.column);
            named.push(NamedLocation {
                resolved: resolve_location(&modeline.schema_uri, &folder, &config_root),
                written_at: Some(position.clone()),
                named_by: CandidateNamer::Modeline { position },
            });
        }
        for (index, search_path) in search_paths.iter().enumerate() {
            let mut resolved = resolve_location(&search_path.path_text, &folder, &config_root);
            resolved.schema_fragment = None;
            named.push(NamedLocation {
                resolved,
                written_at: Some(
                    search_path
                        .written_at
                        .clone()
                        .unwrap_or_else(|| fallback_position.clone()),
                ),
                named_by: CandidateNamer::SearchPath { index },
            });
        }
        let (mut finding, missing_candidates) = self.finding_of(named);
        if let Some(missing_candidates) = missing_candidates {
            let help = missing_candidates
                .iter()
                .map(|missing| {
                    format!(
                        "{}: {}",
                        code_span(&missing.written_location),
                        missing.missing_reason
                    )
                })
                .collect::<Vec<_>>()
                .join("\n");
            finding.finding_problems.push(
                Diagnostic::at(&fallback_position, "no schema found")
                    .with_help((!help.is_empty()).then_some(help)),
            );
        }
        finding
    }

    /// The schema at a location named outside a document: a path resolved against
    /// the working directory, a problem placed at `written_at`.
    pub fn read_schema(&self, location: &str, written_at: Option<&Position>) -> SchemaFinding {
        let working_directory = absolute(Path::new(""));
        let resolved = resolve_location(location, &working_directory, &working_directory);
        let named = NamedLocation {
            resolved,
            written_at: written_at.cloned(),
            named_by: CandidateNamer::Caller,
        };
        let (mut finding, missing_candidates) = self.finding_of(vec![named]);
        if let Some(missing) = missing_candidates.and_then(|missing| missing.into_iter().next()) {
            let message = format!("{} is missing", code_span(location));
            let problem = match written_at {
                Some(position) => Diagnostic::at(position, message),
                None => Diagnostic::unplaced(message),
            };
            finding
                .finding_problems
                .push(problem.with_label(missing.missing_reason));
        }
        finding
    }

    /// Each location read in turn until one is there, or one's read ends the
    /// finding; and, where every one is missing, why each is.
    fn finding_of(
        &self,
        named: Vec<NamedLocation>,
    ) -> (SchemaFinding, Option<Vec<MissingCandidate>>) {
        let mut missing_candidates = Vec::new();
        let mut finding = SchemaFinding {
            tried_candidates: Vec::new(),
            found_schema: None,
            finding_problems: Vec::new(),
        };
        for location in named {
            let read = self.candidate_read(&location, &mut finding.finding_problems);
            let schema_location = match &read {
                CandidateRead::TextRead {
                    schema_location, ..
                }
                | CandidateRead::SourceMissing {
                    schema_location, ..
                } => schema_location.clone(),
                CandidateRead::ReadProblem(_) => location.resolved.location_url.clone(),
            };
            let candidate = SchemaCandidate {
                schema_location,
                named_by: location.named_by.clone(),
            };
            finding.tried_candidates.push(candidate.clone());
            match read {
                CandidateRead::TextRead {
                    schema_text,
                    schema_location,
                } => {
                    let schema_name = if location.resolved.url_scheme == FILE_SCHEME {
                        file_name_of(&schema_location)
                    } else {
                        location.resolved.written_location.clone()
                    };
                    finding.found_schema = Some(FoundSchema {
                        schema_name,
                        schema_text,
                        schema_fragment: location.resolved.schema_fragment.clone(),
                        found_candidate: candidate,
                    });
                    return (finding, None);
                }
                CandidateRead::SourceMissing { missing_reason, .. } => {
                    missing_candidates.push(MissingCandidate {
                        written_location: location.resolved.written_location.clone(),
                        missing_reason,
                    });
                }
                CandidateRead::ReadProblem(problem) => {
                    finding.finding_problems.push(problem);
                    return (finding, None);
                }
            }
        }
        (finding, Some(missing_candidates))
    }

    /// One location read by the reader of its prefix, a remote source through its copy.
    fn candidate_read(
        &self,
        location: &NamedLocation,
        warnings: &mut Vec<Diagnostic>,
    ) -> CandidateRead {
        let resolved = &location.resolved;
        let problem_at = |message: String| match &location.written_at {
            Some(position) => Diagnostic::at(position, message),
            None => Diagnostic::unplaced(message),
        };
        if let Some(problem) = &resolved.cache_parameter_problem {
            return CandidateRead::ReadProblem(problem_at(problem.clone()));
        }
        let Some(reader) = self.url_reader(&resolved.location_url) else {
            return CandidateRead::ReadProblem(problem_at(format!(
                "no reader reads {}",
                code_span(&resolved.written_location)
            )));
        };
        if !reader.reads_remote_sources() {
            if let Some(parameter) = resolved.cache_parameter_names.first() {
                return CandidateRead::ReadProblem(problem_at(format!(
                    "{} is not a parameter of a {} URL",
                    code_span(parameter),
                    code_span(&resolved.url_scheme)
                )));
            }
            return self.local_read(reader.as_ref(), resolved, &problem_at);
        }
        self.remote_read(reader.as_ref(), resolved, &problem_at, warnings)
    }

    /// A source on the machine, read each time; a `file` pattern's first match.
    fn local_read(
        &self,
        reader: &dyn SchemaUrlReader,
        resolved: &ResolvedLocation,
        problem_at: &dyn Fn(String) -> Diagnostic,
    ) -> CandidateRead {
        let mut fetch_url = resolved.fetch_url.clone();
        let mut schema_location = resolved.location_url.clone();
        if let Some(path) = resolved
            .file_path
            .as_deref()
            .filter(|path| has_pattern(path))
        {
            let Some(matched) = first_matching_file(path) else {
                return CandidateRead::SourceMissing {
                    missing_reason: NO_MATCH_REASON.to_owned(),
                    schema_location,
                };
            };
            schema_location = file_url_of(&matched);
            fetch_url = match fetch_url.split_once('?') {
                Some((_, query)) => format!("{schema_location}?{query}"),
                None => schema_location.clone(),
            };
        }
        let answer = reader.read_schema_url(&SchemaUrlRequest {
            fetch_url,
            machine_credentials: false,
            kept_copy_version: None,
            answer_deadline: Instant::now() + READ_LIMIT,
        });
        match answer.read_outcome {
            SchemaUrlOutcome::TextSent { schema_text, .. } => CandidateRead::TextRead {
                schema_text,
                schema_location,
            },
            SchemaUrlOutcome::SourceMissing { missing_reason } => CandidateRead::SourceMissing {
                missing_reason,
                schema_location,
            },
            SchemaUrlOutcome::ReadRefused => CandidateRead::ReadProblem(refused_problem(
                problem_at,
                &resolved.written_location,
                &answer.read_refusals,
                false,
            )),
            SchemaUrlOutcome::ReadFailed { failure_reason } => CandidateRead::ReadProblem(
                failed_problem(problem_at, &resolved.written_location, &failure_reason),
            ),
            SchemaUrlOutcome::CopyConfirmed => CandidateRead::ReadProblem(failed_problem(
                problem_at,
                &resolved.written_location,
                "the reader confirmed a copy never kept",
            )),
        }
    }

    /// A remote source: its kept copy while it is younger than its max age, else
    /// what the reader answers, read again with the machine's credentials where
    /// it is refused and the URL asks for them.
    fn remote_read(
        &self,
        reader: &dyn SchemaUrlReader,
        resolved: &ResolvedLocation,
        problem_at: &dyn Fn(String) -> Diagnostic,
        warnings: &mut Vec<Diagnostic>,
    ) -> CandidateRead {
        let schema_location = resolved.location_url.clone();
        let fetch_url_lock = self.fetch_url_lock(&resolved.fetch_url);
        let _reading_fetch_url = fetch_url_lock
            .lock()
            .unwrap_or_else(PoisonError::into_inner);
        let copy_key = copy_key(&resolved.fetch_url);
        let kept = self.kept_copy(&copy_key, &resolved.fetch_url);
        let now = seconds_now();
        if let Some(kept) = &kept
            && resolved.max_age_days > 0
            && now.saturating_sub(kept.confirmed_at) < resolved.max_age_days * DAY_SECONDS
        {
            return CandidateRead::TextRead {
                schema_text: kept.body.clone(),
                schema_location,
            };
        }
        let ask = |machine_credentials: bool| {
            reader.read_schema_url(&SchemaUrlRequest {
                fetch_url: resolved.fetch_url.clone(),
                machine_credentials,
                kept_copy_version: kept.as_ref().map(|kept| kept.copy_version.clone()),
                answer_deadline: Instant::now() + READ_LIMIT,
            })
        };
        let mut answer = ask(false);
        let credentials_asked =
            answer.read_outcome == SchemaUrlOutcome::ReadRefused && resolved.machine_credentials;
        if credentials_asked {
            answer = ask(true);
        }
        let SchemaUrlAnswer {
            read_outcome,
            read_refusals,
        } = answer;
        if read_outcome != SchemaUrlOutcome::ReadRefused {
            for refusal in read_refusals
                .iter()
                .filter(|refusal| refusal.credential_origin.is_some())
            {
                warnings.push(refusal_warning(problem_at, refusal));
            }
        }
        let written = &resolved.written_location;
        match (read_outcome, kept) {
            (
                SchemaUrlOutcome::TextSent {
                    schema_text,
                    copy_version,
                },
                _,
            ) => {
                self.keep_copy(
                    &copy_key,
                    &KeptCopy {
                        url: resolved.fetch_url.clone(),
                        copy_version,
                        confirmed_at: now,
                        body: schema_text.clone(),
                    },
                );
                CandidateRead::TextRead {
                    schema_text,
                    schema_location,
                }
            }
            (SchemaUrlOutcome::CopyConfirmed, Some(kept)) => {
                let body = kept.body.clone();
                self.keep_copy(
                    &copy_key,
                    &KeptCopy {
                        confirmed_at: now,
                        ..kept
                    },
                );
                CandidateRead::TextRead {
                    schema_text: body,
                    schema_location,
                }
            }
            (SchemaUrlOutcome::CopyConfirmed, None) => CandidateRead::ReadProblem(failed_problem(
                problem_at,
                written,
                "the reader confirmed a copy never kept",
            )),
            (SchemaUrlOutcome::SourceMissing { missing_reason }, _) => {
                CandidateRead::SourceMissing {
                    missing_reason,
                    schema_location,
                }
            }
            (SchemaUrlOutcome::ReadRefused, Some(kept)) => {
                let refused =
                    refused_problem(problem_at, written, &read_refusals, credentials_asked);
                warnings.push(Diagnostic {
                    severity: Severity::Warning,
                    message: format!("{}, so its kept copy was read", refused.message),
                    ..refused
                });
                CandidateRead::TextRead {
                    schema_text: kept.body,
                    schema_location,
                }
            }
            (SchemaUrlOutcome::ReadRefused, None) => CandidateRead::ReadProblem(refused_problem(
                problem_at,
                written,
                &read_refusals,
                credentials_asked,
            )),
            (SchemaUrlOutcome::ReadFailed { .. }, Some(kept)) => CandidateRead::TextRead {
                schema_text: kept.body,
                schema_location,
            },
            (SchemaUrlOutcome::ReadFailed { failure_reason }, None) => {
                CandidateRead::ReadProblem(failed_problem(problem_at, written, &failure_reason))
            }
        }
    }

    fn kept_copy(&self, copy_key: &str, fetch_url: &str) -> Option<KeptCopy> {
        let stored = self.store.as_ref()?.read(copy_key)?;
        serde_json::from_str::<KeptCopy>(&stored)
            .ok()
            .filter(|kept| kept.url == fetch_url)
    }

    fn keep_copy(&self, copy_key: &str, copy: &KeptCopy) {
        if let (Some(store), Ok(written)) = (&self.store, serde_json::to_string(copy)) {
            store.write(copy_key, &written);
        }
    }
}

fn refused_problem(
    problem_at: &dyn Fn(String) -> Diagnostic,
    written: &str,
    refusals: &[ReadRefusal],
    credentials_asked: bool,
) -> Diagnostic {
    let refused_by = refusals
        .first()
        .map_or("the source", |refusal| refusal.refused_by.as_str());
    let reasons: Vec<String> = refusals
        .iter()
        .map(|refusal| match &refusal.credential_origin {
            Some(origin) => format!("{origin}: {}", refusal.refusal_reason),
            None => refusal.refusal_reason.clone(),
        })
        .collect();
    let mut fixes: Vec<&str> = Vec::new();
    for fix in refusals
        .iter()
        .filter_map(|refusal| refusal.renewal_fix.as_deref())
    {
        if !fixes.contains(&fix) {
            fixes.push(fix);
        }
    }
    if !credentials_asked {
        fixes.push(MACHINE_CREDENTIALS_HELP);
    }
    let problem = problem_at(format!(
        "{refused_by} refused to read {}",
        code_span(written)
    ));
    let problem = if reasons.is_empty() {
        problem
    } else {
        problem.with_label(reasons.join(", "))
    };
    problem.with_help((!fixes.is_empty()).then(|| fixes.join("; ")))
}

fn failed_problem(
    problem_at: &dyn Fn(String) -> Diagnostic,
    written: &str,
    failure_reason: &str,
) -> Diagnostic {
    problem_at(format!("reading {} failed", code_span(written))).with_label(failure_reason)
}

fn refusal_warning(problem_at: &dyn Fn(String) -> Diagnostic, refusal: &ReadRefusal) -> Diagnostic {
    let origin = refusal.credential_origin.as_deref().unwrap_or_default();
    let problem = problem_at(format!("{} refused {origin}", refusal.refused_by));
    Diagnostic {
        severity: Severity::Warning,
        ..problem
    }
    .with_label(refusal.refusal_reason.clone())
    .with_help(refusal.renewal_fix.clone())
}

fn file_name_of(file_url: &str) -> String {
    file_url
        .rsplit('/')
        .next()
        .map(|name| file_url_path(&format!("file:///{name}")))
        .and_then(|path| {
            path.file_name()
                .map(|name| name.to_string_lossy().into_owned())
        })
        .unwrap_or_else(|| file_url.to_owned())
}

/// The key a remote source's copy is kept under: a SHA-256 of its URL without
/// the cache's parameters.
fn copy_key(fetch_url: &str) -> String {
    Sha256::digest(fetch_url.as_bytes())
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

/// `path` made absolute, its `.` and `..` steps read out.
fn absolute(path: &Path) -> PathBuf {
    let absolute = if path.as_os_str().is_empty() {
        std::env::current_dir()
    } else {
        std::path::absolute(path)
    }
    .unwrap_or_else(|_| path.to_path_buf());
    let mut normalized = PathBuf::new();
    for component in absolute.components() {
        match component {
            Component::CurDir => {}
            Component::ParentDir => {
                normalized.pop();
            }
            other => normalized.push(other.as_os_str()),
        }
    }
    normalized
}

fn seconds_now() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |elapsed| elapsed.as_secs())
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeMap;
    use std::sync::Mutex;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::thread;

    use serde_json::json;

    use super::*;
    use crate::schema::SchemaStore;

    /// A store in memory.
    #[derive(Default)]
    struct MockStore {
        entries: Mutex<BTreeMap<String, String>>,
    }

    impl SchemaStore for MockStore {
        fn read(&self, key: &str) -> Option<String> {
            self.entries.lock().unwrap().get(key).cloned()
        }

        fn write(&self, key: &str, prepared: &str) {
            self.entries
                .lock()
                .unwrap()
                .insert(key.to_owned(), prepared.to_owned());
        }
    }

    /// A reader of `mock://` URLs answering what a test scripts: one answer for a
    /// read without credentials and one for a read with them, each request kept.
    struct ScriptedReader {
        without_credentials: SchemaUrlAnswer,
        with_credentials: SchemaUrlAnswer,
        requests: Mutex<Vec<SchemaUrlRequest>>,
        reads: AtomicUsize,
    }

    impl ScriptedReader {
        fn answering(
            without_credentials: SchemaUrlAnswer,
            with_credentials: SchemaUrlAnswer,
        ) -> Arc<Self> {
            Arc::new(Self {
                without_credentials,
                with_credentials,
                requests: Mutex::default(),
                reads: AtomicUsize::new(0),
            })
        }

        fn sending(schema_text: &str) -> Arc<Self> {
            let sent = SchemaUrlAnswer::of(SchemaUrlOutcome::TextSent {
                schema_text: schema_text.to_owned(),
                copy_version: json!({ "mock_version": 1 }),
            });
            Self::answering(sent.clone(), sent)
        }
    }

    impl SchemaUrlReader for ScriptedReader {
        fn url_prefixes(&self) -> &[&str] {
            &["mock://"]
        }

        fn reads_remote_sources(&self) -> bool {
            true
        }

        fn read_schema_url(&self, request: &SchemaUrlRequest) -> SchemaUrlAnswer {
            self.reads.fetch_add(1, Ordering::SeqCst);
            self.requests.lock().unwrap().push(request.clone());
            if request.machine_credentials {
                self.with_credentials.clone()
            } else {
                self.without_credentials.clone()
            }
        }
    }

    const MOCK_URL: &str = "mock://mock-host/mock.schema.json";

    fn cache_with(reader: &Arc<ScriptedReader>, store: &Arc<MockStore>) -> SchemaCache {
        SchemaCache::default()
            .with_store(store.clone())
            .with_url_reader(reader.clone())
    }

    fn mock_position() -> Position {
        Position::new(&Arc::from("mock-tfgen.yml"), 4, 7)
    }

    fn refusal(origin: Option<&str>, fix: Option<&str>) -> ReadRefusal {
        ReadRefusal {
            refused_by: "mock-host".to_owned(),
            credential_origin: origin.map(str::to_owned),
            refusal_reason: "403".to_owned(),
            renewal_fix: fix.map(str::to_owned),
        }
    }

    fn keep(store: &MockStore, fetch_url: &str, confirmed_at: u64) {
        let kept = KeptCopy {
            url: fetch_url.to_owned(),
            copy_version: json!({ "mock_version": 0 }),
            confirmed_at,
            body: "{\"title\": \"kept\"}".to_owned(),
        };
        store.write(&copy_key(fetch_url), &serde_json::to_string(&kept).unwrap());
    }

    #[test]
    fn a_remote_text_is_kept_as_a_copy_with_its_version_and_read_back_within_its_max_age() {
        let (reader, store) = (
            ScriptedReader::sending("{}"),
            Arc::new(MockStore::default()),
        );
        let cache = cache_with(&reader, &store);
        let location = format!("{MOCK_URL}?b=1&yaml_dsl_max_age_days=3");
        let first = cache.read_schema(&location, Some(&mock_position()));
        assert_eq!(first.found_schema.unwrap().schema_text, "{}");
        let kept: JsonValue =
            serde_json::from_str(&store.read(&copy_key(&format!("{MOCK_URL}?b=1"))).unwrap())
                .unwrap();
        assert_eq!(kept["copy_version"], json!({ "mock_version": 1 }));
        assert_eq!(kept["url"], format!("{MOCK_URL}?b=1"));
        let second = cache.read_schema(&location, Some(&mock_position()));
        assert_eq!(second.found_schema.unwrap().schema_name, location);
        assert_eq!(reader.reads.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn a_copy_past_its_max_age_is_asked_about_with_its_version_and_a_confirmation_keeps_it() {
        let confirmed = SchemaUrlAnswer::of(SchemaUrlOutcome::CopyConfirmed);
        let reader = ScriptedReader::answering(confirmed.clone(), confirmed);
        let store = Arc::new(MockStore::default());
        keep(&store, MOCK_URL, 0);
        let finding = cache_with(&reader, &store).read_schema(MOCK_URL, None);
        assert_eq!(
            finding.found_schema.unwrap().schema_text,
            "{\"title\": \"kept\"}"
        );
        let requests = reader.requests.lock().unwrap();
        assert_eq!(
            requests[0].kept_copy_version,
            Some(json!({ "mock_version": 0 }))
        );
        let kept: JsonValue =
            serde_json::from_str(&store.read(&copy_key(MOCK_URL)).unwrap()).unwrap();
        assert!(kept["confirmed_at"].as_u64().unwrap() > 0);
    }

    #[test]
    fn a_refused_read_is_asked_again_with_credentials_only_where_the_url_opts_in() {
        let refused = SchemaUrlAnswer::refused(vec![refusal(None, None)]);
        let sent = SchemaUrlAnswer {
            read_outcome: SchemaUrlOutcome::TextSent {
                schema_text: "{}".to_owned(),
                copy_version: JsonValue::Null,
            },
            read_refusals: vec![refusal(Some("`MOCK_TOKEN`"), Some("replace `MOCK_TOKEN`"))],
        };
        let reader = ScriptedReader::answering(refused, sent);
        let store = Arc::new(MockStore::default());
        let cache = cache_with(&reader, &store);
        let without = cache.read_schema(MOCK_URL, Some(&mock_position()));
        assert!(without.found_schema.is_none());
        let problem = &without.finding_problems[0];
        assert_eq!(
            problem.message,
            format!("mock-host refused to read `{MOCK_URL}`")
        );
        let notes = problem.notes.as_ref().unwrap();
        assert_eq!(notes.label.as_deref(), Some("403"));
        assert_eq!(notes.help.as_deref(), Some(MACHINE_CREDENTIALS_HELP));
        let opted_in = format!("{MOCK_URL}?yaml_dsl_machine_credentials=true");
        let with = cache.read_schema(&opted_in, Some(&mock_position()));
        assert_eq!(with.found_schema.unwrap().schema_text, "{}");
        let warning = &with.finding_problems[0];
        assert_eq!(warning.severity, Severity::Warning);
        assert_eq!(warning.message, "mock-host refused `MOCK_TOKEN`");
        assert_eq!(
            warning.notes.as_ref().unwrap().help.as_deref(),
            Some("replace `MOCK_TOKEN`")
        );
        let requests = reader.requests.lock().unwrap();
        let asked: Vec<bool> = requests
            .iter()
            .map(|request| request.machine_credentials)
            .collect();
        assert_eq!(asked, vec![false, false, true]);
    }

    #[test]
    fn every_credential_refused_reads_the_kept_copy_with_a_warning_else_ends_the_finding() {
        let refused = SchemaUrlAnswer::refused(vec![
            refusal(Some("`MOCK_TOKEN`"), Some("replace `MOCK_TOKEN`")),
            refusal(Some("`OTHER_TOKEN`"), Some("replace `MOCK_TOKEN`")),
        ]);
        let reader = ScriptedReader::answering(refused.clone(), refused);
        let opted_in = format!("{MOCK_URL}?yaml_dsl_machine_credentials=true");
        let store = Arc::new(MockStore::default());
        let unkept = cache_with(&reader, &store).read_schema(&opted_in, Some(&mock_position()));
        let notes = unkept.finding_problems[0].notes.clone().unwrap();
        assert_eq!(
            notes.label.as_deref(),
            Some("`MOCK_TOKEN`: 403, `OTHER_TOKEN`: 403")
        );
        assert_eq!(notes.help.as_deref(), Some("replace `MOCK_TOKEN`"));
        keep(&store, MOCK_URL, 0);
        let kept = cache_with(&reader, &store).read_schema(&opted_in, Some(&mock_position()));
        assert_eq!(
            kept.found_schema.unwrap().schema_text,
            "{\"title\": \"kept\"}"
        );
        let warning = &kept.finding_problems[0];
        assert_eq!(warning.severity, Severity::Warning);
        assert_eq!(
            warning.message,
            format!("mock-host refused to read `{opted_in}`, so its kept copy was read")
        );
    }

    #[test]
    fn a_failed_read_gives_the_kept_copy_and_a_missing_source_never_does() {
        let failed = SchemaUrlAnswer::failed("mock failure");
        let failing = ScriptedReader::answering(failed.clone(), failed);
        let store = Arc::new(MockStore::default());
        let unkept = cache_with(&failing, &store).read_schema(MOCK_URL, None);
        assert_eq!(
            unkept.finding_problems[0].message,
            format!("reading `{MOCK_URL}` failed")
        );
        assert_eq!(unkept.finding_problems[0].position, None);
        keep(&store, MOCK_URL, 0);
        let kept = cache_with(&failing, &store).read_schema(MOCK_URL, None);
        assert!(kept.found_schema.is_some());
        assert!(kept.finding_problems.is_empty());
        let missing = SchemaUrlAnswer::missing("mock reason");
        let absent = ScriptedReader::answering(missing.clone(), missing);
        let finding = cache_with(&absent, &store).read_schema(MOCK_URL, Some(&mock_position()));
        assert!(finding.found_schema.is_none());
        let problem = &finding.finding_problems[0];
        assert_eq!(problem.message, format!("`{MOCK_URL}` is missing"));
        assert_eq!(
            problem.notes.as_ref().unwrap().label.as_deref(),
            Some("mock reason")
        );
        assert_eq!(problem.position, Some(mock_position()));
    }

    #[test]
    fn a_relative_path_is_read_against_the_working_directory() {
        let finding = SchemaCache::default().read_schema("Cargo.toml", None);
        let working_directory = std::env::current_dir().unwrap();
        let found = finding.found_schema.unwrap();
        assert_eq!(
            found.found_candidate.schema_location,
            file_url_of(&working_directory.join("Cargo.toml"))
        );
        assert_eq!(found.schema_name, "Cargo.toml");
    }

    #[test]
    fn a_cache_without_a_store_asks_the_reader_on_every_read() {
        let reader = ScriptedReader::sending("{}");
        let cache = SchemaCache::default().with_url_reader(reader.clone());
        let location = format!("{MOCK_URL}?yaml_dsl_max_age_days=3");
        cache.read_schema(&location, None);
        cache.read_schema(&location, None);
        assert_eq!(reader.reads.load(Ordering::SeqCst), 2);
    }

    #[test]
    fn reads_of_one_url_at_once_fetch_it_once_and_the_others_read_its_copy() {
        let (reader, store) = (
            ScriptedReader::sending("{}"),
            Arc::new(MockStore::default()),
        );
        let cache = Arc::new(cache_with(&reader, &store));
        let location = format!("{MOCK_URL}?yaml_dsl_max_age_days=1");
        let readers: Vec<_> = (0..4)
            .map(|_| {
                let (cache, location) = (Arc::clone(&cache), location.clone());
                thread::spawn(move || cache.read_schema(&location, None).found_schema.is_some())
            })
            .collect();
        for reading in readers {
            assert!(reading.join().unwrap());
        }
        assert_eq!(reader.reads.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn a_url_is_read_by_the_reader_of_its_longest_prefix() {
        struct PrefixReader(&'static [&'static str]);
        impl SchemaUrlReader for PrefixReader {
            fn url_prefixes(&self) -> &[&str] {
                self.0
            }
            fn reads_remote_sources(&self) -> bool {
                true
            }
            fn read_schema_url(&self, _request: &SchemaUrlRequest) -> SchemaUrlAnswer {
                SchemaUrlAnswer::missing(self.0[0])
            }
        }
        let cache = SchemaCache::default()
            .with_url_reader(Arc::new(PrefixReader(&["https://mock.example/releases/"])));
        let finding = cache.read_schema("HTTPS://mock.example/releases/a.json", None);
        assert_eq!(
            finding.finding_problems[0]
                .notes
                .as_ref()
                .unwrap()
                .label
                .as_deref(),
            Some("https://mock.example/releases/")
        );
        let taken = std::panic::catch_unwind(|| {
            SchemaCache::default().with_url_reader(Arc::new(PrefixReader(&["HTTPS://"])))
        });
        assert!(taken.is_err());
    }
}
