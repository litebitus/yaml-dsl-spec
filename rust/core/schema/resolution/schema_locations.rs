//! Where a schema is named: a document's modeline, and a location as written,
//! resolved to a URL with the cache's parameters read out of it.

use std::fs;
use std::path::{Path, PathBuf};

use crate::problem_wording::code_span;

const MODELINE_KEY: &str = "yaml-language-server:";
const MODELINE_SCHEMA: &str = "$schema=";
const CONFIG_ROOT: &str = "{config_root}";
const CACHE_PARAMETER_PREFIX: &str = "yaml_dsl_";
const MAX_AGE_PARAMETER: &str = "yaml_dsl_max_age_days";
const MACHINE_CREDENTIALS_PARAMETER: &str = "yaml_dsl_machine_credentials";
pub const FILE_SCHEME: &str = "file";

/// A document's modeline: the schema URI it names and where that URI starts.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Modeline {
    pub schema_uri: String,
    pub line: usize,
    pub column: usize,
}

/// The first line of `text` reading `# yaml-language-server: $schema=<uri>`.
pub fn modeline_of(text: &str) -> Option<Modeline> {
    text.lines().enumerate().find_map(|(index, line)| {
        let after_hash = line.trim_start().strip_prefix('#')?.trim_start();
        let after_key = after_hash.strip_prefix(MODELINE_KEY)?.trim_start();
        let uri_and_rest = after_key.strip_prefix(MODELINE_SCHEMA)?;
        let schema_uri: String = uri_and_rest
            .chars()
            .take_while(|character| !character.is_whitespace())
            .collect();
        if schema_uri.is_empty() {
            return None;
        }
        let uri_start = line.len() - uri_and_rest.len();
        Some(Modeline {
            schema_uri,
            line: index + 1,
            column: line[..uri_start].chars().count() + 1,
        })
    })
}

/// A location as written, without its fragment, resolved: its URL, the URL a
/// reader is asked for, and the cache's parameters it carries.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ResolvedLocation {
    pub written_location: String,
    pub schema_fragment: Option<String>,
    pub location_url: String,
    pub url_scheme: String,
    pub fetch_url: String,
    pub max_age_days: u64,
    pub machine_credentials: bool,
    /// The cache's parameters written in it, by name, in order.
    pub cache_parameter_names: Vec<String>,
    /// The first of its cache's parameters that does not read, as a problem's message.
    pub cache_parameter_problem: Option<String>,
    /// The file a `file` location names, its `*` steps not yet matched.
    pub file_path: Option<PathBuf>,
}

/// `written`, a path or a URL, resolved: a path against `base`, or against
/// `config_root` when it starts with `{config_root}`.
pub fn resolve_location(written: &str, base: &Path, config_root: &Path) -> ResolvedLocation {
    let (without_fragment, schema_fragment) = match written.split_once('#') {
        Some((location, fragment)) => (location, Some(fragment.to_owned())),
        None => (written, None),
    };
    let (address, query) = match without_fragment.split_once('?') {
        Some((address, query)) => (address, Some(query)),
        None => (without_fragment, None),
    };
    let parameters = cache_parameters(query);
    let (url_scheme, location_address, file_path) = match url_scheme_of(address) {
        Some(scheme) if scheme == FILE_SCHEME => {
            let path = file_url_path(address);
            (scheme, file_url_of(&path), Some(path))
        }
        Some(scheme) => (scheme, address.to_owned(), None),
        None => {
            let path = written_path(address, base, config_root);
            (FILE_SCHEME.to_owned(), file_url_of(&path), Some(path))
        }
    };
    let with_query = |passed: &[&str]| {
        if passed.is_empty() {
            location_address.clone()
        } else {
            format!("{location_address}?{}", passed.join("&"))
        }
    };
    let all_parameters: Vec<&str> = query
        .map(|query| query.split('&').collect())
        .unwrap_or_default();
    ResolvedLocation {
        written_location: without_fragment.to_owned(),
        schema_fragment,
        location_url: with_query(&all_parameters),
        url_scheme,
        fetch_url: with_query(&parameters.passed),
        max_age_days: parameters.max_age_days,
        machine_credentials: parameters.machine_credentials,
        cache_parameter_names: parameters.names,
        cache_parameter_problem: parameters.problem,
        file_path,
    }
}

/// A URL's scheme, lower case, where `location` starts with `<scheme>://`.
pub fn url_scheme_of(location: &str) -> Option<String> {
    let (scheme, _) = location.split_once("://")?;
    let mut characters = scheme.chars();
    let starts_with_letter = characters
        .next()
        .is_some_and(|first| first.is_ascii_alphabetic());
    let rest_allowed =
        characters.all(|character| character.is_ascii_alphanumeric() || "+-.".contains(character));
    (starts_with_letter && rest_allowed).then(|| scheme.to_ascii_lowercase())
}

struct CacheParameters<'q> {
    passed: Vec<&'q str>,
    names: Vec<String>,
    max_age_days: u64,
    machine_credentials: bool,
    problem: Option<String>,
}

fn cache_parameters(query: Option<&str>) -> CacheParameters<'_> {
    let mut parameters = CacheParameters {
        passed: Vec::new(),
        names: Vec::new(),
        max_age_days: 0,
        machine_credentials: false,
        problem: None,
    };
    for parameter in query.into_iter().flat_map(|query| query.split('&')) {
        let (name, value) = parameter.split_once('=').unwrap_or((parameter, ""));
        if !name.starts_with(CACHE_PARAMETER_PREFIX) {
            parameters.passed.push(parameter);
            continue;
        }
        parameters.names.push(name.to_owned());
        let problem = match name {
            MAX_AGE_PARAMETER => match whole_days(value) {
                Some(days) => {
                    parameters.max_age_days = days;
                    None
                }
                None => Some(format!(
                    "{} must be a whole number of days",
                    code_span(MAX_AGE_PARAMETER)
                )),
            },
            MACHINE_CREDENTIALS_PARAMETER => match value {
                "true" | "false" => {
                    parameters.machine_credentials = value == "true";
                    None
                }
                _ => Some(format!(
                    "{} must be {} or {}",
                    code_span(MACHINE_CREDENTIALS_PARAMETER),
                    code_span("true"),
                    code_span("false")
                )),
            },
            _ => Some(format!(
                "{} is not a parameter of a schema URL",
                code_span(name)
            )),
        };
        if parameters.problem.is_none() {
            parameters.problem = problem;
        }
    }
    parameters
}

fn whole_days(value: &str) -> Option<u64> {
    let digits_only = !value.is_empty() && value.bytes().all(|byte| byte.is_ascii_digit());
    let leading_zero = value.len() > 1 && value.starts_with('0');
    (digits_only && !leading_zero)
        .then(|| value.parse().ok())
        .flatten()
}

/// The machine path a written path names, its steps separated by `/`.
fn written_path(written: &str, base: &Path, config_root: &Path) -> PathBuf {
    let (start, rest) = match written.strip_prefix(CONFIG_ROOT) {
        Some(rest) => (config_root, rest),
        None if written.starts_with('/') => (Path::new("/"), written),
        None => (base, written),
    };
    let mut path = start.to_path_buf();
    for step in rest.split('/') {
        match step {
            "" | "." => {}
            ".." => {
                path.pop();
            }
            step => path.push(step),
        }
    }
    path
}

/// The machine path a `file` URL names.
pub fn file_url_path(url: &str) -> PathBuf {
    let after_scheme = &url[url.find("://").map_or(0, |at| at + 3)..];
    let path_start = after_scheme.find('/').unwrap_or(after_scheme.len());
    let decoded = percent_decoded(&after_scheme[path_start..]);
    let mut steps = decoded
        .split('/')
        .filter(|step| !step.is_empty())
        .peekable();
    let names_drive = |step: &str| {
        step.len() == 2
            && step.ends_with(':')
            && step.starts_with(|character: char| character.is_ascii_alphabetic())
    };
    let mut path = match steps.next_if(|step| names_drive(step)) {
        Some(drive) => PathBuf::from(format!("{drive}{}", std::path::MAIN_SEPARATOR)),
        None => PathBuf::from("/"),
    };
    for step in steps {
        match step {
            "." => {}
            ".." => {
                path.pop();
            }
            step => path.push(step),
        }
    }
    path
}

/// The `file` URL of a machine path.
pub fn file_url_of(path: &Path) -> String {
    let steps: Vec<String> = path
        .components()
        .filter_map(|component| match component {
            std::path::Component::Normal(step) => Some(step.to_string_lossy().into_owned()),
            std::path::Component::Prefix(prefix) => {
                Some(prefix.as_os_str().to_string_lossy().into_owned())
            }
            _ => None,
        })
        .collect();
    format!(
        "file:///{}",
        steps
            .iter()
            .map(|step| percent_encoded(step))
            .collect::<Vec<_>>()
            .join("/")
    )
}

fn percent_encoded(step: &str) -> String {
    let mut encoded = String::with_capacity(step.len());
    for byte in step.bytes() {
        let kept = byte.is_ascii_alphanumeric() || b"-._~!$&'()*+,;=:@".contains(&byte);
        if kept {
            encoded.push(byte as char);
        } else {
            encoded.push_str(&format!("%{byte:02X}"));
        }
    }
    encoded
}

fn percent_decoded(text: &str) -> String {
    let bytes = text.as_bytes();
    let mut decoded = Vec::with_capacity(bytes.len());
    let mut at = 0;
    while at < bytes.len() {
        let escaped = (bytes[at] == b'%')
            .then(|| text.get(at + 1..at + 3))
            .flatten()
            .filter(|hex| hex.bytes().all(|digit| digit.is_ascii_hexdigit()))
            .and_then(|hex| u8::from_str_radix(hex, 16).ok());
        match escaped {
            Some(byte) => {
                decoded.push(byte);
                at += 3;
            }
            None => {
                decoded.push(bytes[at]);
                at += 1;
            }
        }
    }
    String::from_utf8_lossy(&decoded).into_owned()
}

/// The first file a path names, each `*` step matching any run of characters
/// within the step, the steps' matches read in path order.
pub fn first_matching_file(path: &Path) -> Option<PathBuf> {
    let mut reached = vec![PathBuf::new()];
    for component in path.components() {
        let step = component.as_os_str().to_string_lossy();
        if !step.contains('*') {
            reached = reached
                .into_iter()
                .map(|folder| folder.join(component.as_os_str()))
                .collect();
            continue;
        }
        let mut matched: Vec<PathBuf> = reached
            .iter()
            .filter_map(|folder| fs::read_dir(folder).ok())
            .flatten()
            .filter_map(|entry| entry.ok().map(|entry| entry.path()))
            .filter(|candidate| {
                candidate
                    .file_name()
                    .is_some_and(|name| step_matches(&step, &name.to_string_lossy()))
            })
            .collect();
        matched.sort();
        reached = matched;
    }
    reached.into_iter().find(|candidate| candidate.is_file())
}

/// Whether `name` matches a step whose `*` matches any run of characters.
fn step_matches(step: &str, name: &str) -> bool {
    let parts: Vec<&str> = step.split('*').collect();
    let (first, last) = (parts[0], parts[parts.len() - 1]);
    if !name.starts_with(first) || name.len() < first.len() + last.len() || !name.ends_with(last) {
        return false;
    }
    let mut middle = &name[first.len()..name.len() - last.len()];
    for part in &parts[1..parts.len() - 1] {
        match middle.find(part) {
            Some(at) => middle = &middle[at + part.len()..],
            None => return false,
        }
    }
    true
}

pub fn has_pattern(path: &Path) -> bool {
    path.to_string_lossy().contains('*')
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_modeline_names_its_uri_and_where_it_starts() {
        let text = "name: mock\n  #  yaml-language-server:  $schema=../mock.schema.json#/a rest\n";
        assert_eq!(
            modeline_of(text),
            Some(Modeline {
                schema_uri: "../mock.schema.json#/a".into(),
                line: 2,
                column: 37,
            })
        );
        assert_eq!(modeline_of("# yaml-language-server: $schema=\n"), None);
        assert_eq!(modeline_of("# mock: $schema=x\n"), None);
    }

    #[test]
    fn a_path_is_resolved_to_its_file_url_by_forward_slash_steps() {
        let base = Path::new("/mock/stack");
        let root = Path::new("/mock/config");
        let relative = resolve_location("../schemas/./mock.schema.json#/a", base, root);
        assert_eq!(relative.url_scheme, "file");
        assert_eq!(
            relative.file_path.as_deref(),
            Some(Path::new("/mock/schemas/mock.schema.json"))
        );
        assert_eq!(relative.schema_fragment.as_deref(), Some("/a"));
        assert_eq!(relative.written_location, "../schemas/./mock.schema.json");
        let rooted = resolve_location("{config_root}/mock dir/m.json", base, root);
        assert_eq!(
            rooted.file_path.as_deref(),
            Some(Path::new("/mock/config/mock dir/m.json"))
        );
        assert_eq!(rooted.location_url, "file:///mock/config/mock%20dir/m.json");
        let absolute = resolve_location("/mock/abs.json", base, root);
        assert_eq!(
            absolute.file_path.as_deref(),
            Some(Path::new("/mock/abs.json"))
        );
        let url = resolve_location("file:///mock/a%20b.json", base, root);
        assert_eq!(url.file_path.as_deref(), Some(Path::new("/mock/a b.json")));
        let climbing = resolve_location("file:///mock/a/../b/./c.json", base, root);
        assert_eq!(
            climbing.file_path.as_deref(),
            Some(Path::new("/mock/b/c.json"))
        );
        assert_eq!(climbing.location_url, "file:///mock/b/c.json");
        let signed = resolve_location("file:///mock/a%+Fb.json", base, root);
        assert_eq!(
            signed.file_path.as_deref(),
            Some(Path::new("/mock/a%+Fb.json"))
        );
    }

    #[test]
    fn the_cache_parameters_are_read_out_and_the_others_kept_in_order() {
        let base = Path::new("/mock");
        let location = resolve_location(
            "https://mock.example/s.json?b=1&yaml_dsl_max_age_days=7&a=2&yaml_dsl_machine_credentials=true",
            base,
            base,
        );
        assert_eq!(location.fetch_url, "https://mock.example/s.json?b=1&a=2");
        assert_eq!(location.url_scheme, "https");
        assert_eq!(
            (location.max_age_days, location.machine_credentials),
            (7, true)
        );
        assert_eq!(location.cache_parameter_problem, None);
        let problem = |query: &str| {
            resolve_location(&format!("https://mock.example/s.json?{query}"), base, base)
                .cache_parameter_problem
        };
        assert_eq!(
            problem("yaml_dsl_mock=1").as_deref(),
            Some("`yaml_dsl_mock` is not a parameter of a schema URL")
        );
        assert_eq!(
            problem("yaml_dsl_max_age_days=07").as_deref(),
            Some("`yaml_dsl_max_age_days` must be a whole number of days")
        );
        assert_eq!(
            problem("yaml_dsl_machine_credentials=yes").as_deref(),
            Some("`yaml_dsl_machine_credentials` must be `true` or `false`")
        );
    }

    #[test]
    fn a_scheme_is_read_only_where_a_url_starts_with_one() {
        assert_eq!(url_scheme_of("S3://mock/key").as_deref(), Some("s3"));
        assert_eq!(
            url_scheme_of("gs+x.y-z://mock").as_deref(),
            Some("gs+x.y-z")
        );
        assert_eq!(url_scheme_of("mock/https://x"), None);
        assert_eq!(url_scheme_of("1x://mock"), None);
    }

    #[test]
    fn a_star_step_matches_any_run_of_characters_within_it() {
        assert!(step_matches("*.json", "mock.json"));
        assert!(step_matches("a*b*c", "a-x-b-y-c"));
        assert!(!step_matches("a*b*c", "a-x-c"));
        assert!(!step_matches("ab*ba", "aba"));
        assert!(step_matches("*", ""));
    }
}
