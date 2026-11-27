//! The YAML DSL standard's reader of schema URLs on GitHub: a link read as
//! written, and, with the machine's credentials, a release asset read through
//! GitHub's API with each of the machine's tokens in turn.

use std::io::Read;
use std::process::{Command, Stdio};
use std::sync::Mutex;
use std::thread;
use std::time::{Duration, Instant};

use serde_json::Value as JsonValue;
use yaml_dsl_spec::schema::resolution::{
    HttpReply, ReadRefusal, SchemaUrlAnswer, SchemaUrlOutcome, SchemaUrlReader, SchemaUrlRequest,
    http_conditional_headers, http_get, netrc_login,
};

const GITHUB_PREFIXES: [&str; 2] = ["https://github.com/", "https://www.github.com/"];
const GITHUB_API: &str = "https://api.github.com";
const GITHUB_API_HOST: &str = "api.github.com";
const GITHUB_REFUSER: &str = "GitHub";
const GITHUB_JSON: &str = "application/vnd.github+json";
const GH_LOGIN_TIMEOUT: Duration = Duration::from_secs(10);
const GH_LOGIN_POLL: Duration = Duration::from_millis(20);
const STATUS_OK: u16 = 200;
const STATUS_NOT_MODIFIED: u16 = 304;
const STATUS_UNAUTHORIZED: u16 = 401;
const STATUS_FORBIDDEN: u16 = 403;
const STATUS_NOT_FOUND: u16 = 404;
const NO_TOKEN_FIX: &str =
    "set `GH_TOKEN`, run `gh auth login`, or add a `.netrc` entry for api.github.com";

/// A token, where it came from, and how its owner renews it.
#[derive(Clone, Debug, PartialEq, Eq)]
struct GithubToken {
    credential_origin: String,
    authorization: String,
    renewal_fix: String,
}

/// Where the tokens come from.
trait TokenSource: Send + Sync {
    /// The tokens, in the order they are tried.
    fn github_tokens(&self) -> Vec<GithubToken>;
    /// Reads the tokens again when next asked, after every one was refused.
    fn forget_tokens(&self);
}

/// The machine's tokens: `GH_TOKEN`, `GITHUB_TOKEN`, the `gh` command's login,
/// then the `.netrc` entry for api.github.com, read when first needed.
#[derive(Default)]
struct MachineTokens {
    read_tokens: Mutex<Option<Vec<GithubToken>>>,
}

impl TokenSource for MachineTokens {
    fn github_tokens(&self) -> Vec<GithubToken> {
        let mut read = self
            .read_tokens
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        read.get_or_insert_with(machine_tokens).clone()
    }

    fn forget_tokens(&self) {
        *self
            .read_tokens
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner()) = None;
    }
}

fn machine_tokens() -> Vec<GithubToken> {
    let mut tokens = Vec::new();
    for variable in ["GH_TOKEN", "GITHUB_TOKEN"] {
        if let Some(token) = std::env::var(variable)
            .ok()
            .filter(|token| !token.is_empty())
        {
            tokens.push(GithubToken {
                credential_origin: format!("`{variable}`"),
                authorization: format!("Bearer {token}"),
                renewal_fix: format!("replace `{variable}`"),
            });
        }
    }
    if let Some(token) = gh_login_token() {
        tokens.push(GithubToken {
            credential_origin: "`gh auth token`".to_owned(),
            authorization: format!("Bearer {token}"),
            renewal_fix: "run `gh auth login`".to_owned(),
        });
    }
    if let Some(login) = netrc_login(GITHUB_API_HOST) {
        tokens.push(GithubToken {
            authorization: login.authorization(),
            credential_origin: login.credential_origin,
            renewal_fix: login.renewal_fix,
        });
    }
    tokens
}

/// The token of the account `gh auth login` logged in to github.com, where the
/// `gh` command is installed and answers within its time.
fn gh_login_token() -> Option<String> {
    let mut child = Command::new("gh")
        .args(["auth", "token", "--hostname", "github.com"])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .ok()?;
    let deadline = Instant::now() + GH_LOGIN_TIMEOUT;
    loop {
        match child.try_wait() {
            Ok(Some(status)) if status.success() => break,
            Ok(Some(_)) | Err(_) => return None,
            Ok(None) if Instant::now() >= deadline => {
                let _ = child.kill();
                let _ = child.wait();
                return None;
            }
            Ok(None) => thread::sleep(GH_LOGIN_POLL),
        }
    }
    let mut token = String::new();
    child.stdout.take()?.read_to_string(&mut token).ok()?;
    let token = token.trim().to_owned();
    (!token.is_empty()).then_some(token)
}

/// The URL reader of GitHub links.
pub struct GithubSchemaReader {
    tokens: Box<dyn TokenSource>,
    github_api: String,
    link_prefixes: Vec<&'static str>,
}

impl Default for GithubSchemaReader {
    fn default() -> Self {
        Self::new()
    }
}

impl GithubSchemaReader {
    pub fn new() -> Self {
        Self::with_github(
            Box::new(MachineTokens::default()),
            GITHUB_API,
            GITHUB_PREFIXES.to_vec(),
        )
    }

    fn with_github(
        tokens: Box<dyn TokenSource>,
        github_api: &str,
        link_prefixes: Vec<&'static str>,
    ) -> Self {
        Self {
            tokens,
            github_api: github_api.to_owned(),
            link_prefixes,
        }
    }
}

/// A link under `<owner>/<repo>/releases/`: its repository, and the release and
/// asset its path reads, where it reads one.
#[derive(Debug, PartialEq, Eq)]
struct GithubRelease {
    owner: String,
    repository: String,
    asset: Option<ReleaseAsset>,
}

#[derive(Debug, PartialEq, Eq)]
struct ReleaseAsset {
    release: ReleaseName,
    asset_name: String,
}

#[derive(Debug, PartialEq, Eq)]
enum ReleaseName {
    LatestRelease,
    TaggedRelease(String),
}

impl ReleaseName {
    fn api_path(&self) -> String {
        match self {
            Self::LatestRelease => "latest".to_owned(),
            Self::TaggedRelease(tag) => format!("tags/{}", tag.replace('/', "%2F")),
        }
    }

    fn shown_name(&self) -> String {
        match self {
            Self::LatestRelease => "the latest release".to_owned(),
            Self::TaggedRelease(tag) => format!("release `{tag}`"),
        }
    }
}

impl SchemaUrlReader for GithubSchemaReader {
    fn url_prefixes(&self) -> &[&str] {
        &self.link_prefixes
    }

    fn reads_remote_sources(&self) -> bool {
        true
    }

    fn read_schema_url(&self, request: &SchemaUrlRequest) -> SchemaUrlAnswer {
        if request.machine_credentials
            && let Some(GithubRelease {
                owner,
                repository,
                asset: Some(asset),
            }) = self.github_release(&request.fetch_url)
        {
            return self.release_answer(&owner, &repository, &asset, request);
        }
        self.link_answer(request)
    }
}

impl GithubSchemaReader {
    /// The link read as written, the machine's credentials never sent.
    fn link_answer(&self, request: &SchemaUrlRequest) -> SchemaUrlAnswer {
        let headers = http_conditional_headers(request.kept_copy_version.as_ref());
        let reply = match http_get(&request.fetch_url, &headers, request.answer_deadline) {
            Ok(reply) => reply,
            Err(error) => return SchemaUrlAnswer::failed(error),
        };
        if matches!(
            reply.status_code,
            STATUS_UNAUTHORIZED | STATUS_FORBIDDEN | STATUS_NOT_FOUND
        ) {
            return SchemaUrlAnswer::refused(vec![ReadRefusal {
                refused_by: GITHUB_REFUSER.to_owned(),
                credential_origin: None,
                refusal_reason: reply.status_code.to_string(),
                renewal_fix: None,
            }]);
        }
        sent_answer(reply)
    }

    fn github_release(&self, url: &str) -> Option<GithubRelease> {
        self.link_prefixes
            .iter()
            .find_map(|prefix| {
                url.get(..prefix.len())
                    .filter(|start| start.eq_ignore_ascii_case(prefix))
                    .map(|_| &url[prefix.len()..])
            })
            .and_then(github_release)
    }

    /// A release asset read through GitHub's API, each token tried in turn until
    /// one is let in, each one refused before it listed.
    fn release_answer(
        &self,
        owner: &str,
        repository: &str,
        asset: &ReleaseAsset,
        request: &SchemaUrlRequest,
    ) -> SchemaUrlAnswer {
        let tokens = self.tokens.github_tokens();
        if tokens.is_empty() {
            return SchemaUrlAnswer::refused(vec![ReadRefusal {
                refused_by: GITHUB_REFUSER.to_owned(),
                credential_origin: None,
                refusal_reason: "no GitHub token is set".to_owned(),
                renewal_fix: Some(NO_TOKEN_FIX.to_owned()),
            }]);
        }
        let answer_with =
            |read_outcome: SchemaUrlOutcome, read_refusals: Vec<ReadRefusal>| SchemaUrlAnswer {
                read_outcome,
                read_refusals,
            };
        let failed = |failure_reason: String| SchemaUrlOutcome::ReadFailed { failure_reason };
        let repository_name = format!("`{owner}/{repository}`");
        let repository_url = format!("{}/repos/{owner}/{repository}", self.github_api);
        let release_url = format!("{repository_url}/releases/{}", asset.release.api_path());
        let mut refusals = Vec::new();
        let mut missing_reason = None;
        for token in &tokens {
            let api_headers = [
                ("Authorization", token.authorization.clone()),
                ("Accept", GITHUB_JSON.to_owned()),
            ];
            let release = match http_get(&release_url, &api_headers, request.answer_deadline) {
                Ok(release) => release,
                Err(error) => return answer_with(failed(error), refusals),
            };
            let status = release.status_code;
            if matches!(status, STATUS_UNAUTHORIZED | STATUS_FORBIDDEN) {
                refusals.push(refusal(token, status));
                continue;
            }
            if status != STATUS_OK {
                let seen = http_get(&repository_url, &api_headers, request.answer_deadline)
                    .is_ok_and(|repository| repository.status_code == STATUS_OK);
                if seen {
                    missing_reason = Some(format!(
                        "{repository_name} has no {}",
                        asset.release.shown_name()
                    ));
                    break;
                }
                continue;
            }
            let listing: JsonValue = match serde_json::from_str(&release.reply_text) {
                Ok(listing) => listing,
                Err(error) => return answer_with(failed(error.to_string()), refusals),
            };
            let Some(asset_url) = listing["assets"]
                .as_array()
                .into_iter()
                .flatten()
                .find(|listed| listed["name"] == asset.asset_name.as_str())
                .and_then(|listed| listed["url"].as_str())
            else {
                missing_reason = Some(format!(
                    "{} of {repository_name} has no asset `{}`",
                    asset.release.shown_name(),
                    asset.asset_name
                ));
                break;
            };
            let mut download_headers = http_conditional_headers(request.kept_copy_version.as_ref());
            download_headers.push(("Authorization", token.authorization.clone()));
            download_headers.push(("Accept", "application/octet-stream".to_owned()));
            match http_get(asset_url, &download_headers, request.answer_deadline) {
                Ok(reply)
                    if matches!(reply.status_code, STATUS_UNAUTHORIZED | STATUS_FORBIDDEN) =>
                {
                    refusals.push(refusal(token, reply.status_code));
                }
                Ok(reply) => return answer_with(sent_answer(reply).read_outcome, refusals),
                Err(error) => return answer_with(failed(error), refusals),
            }
        }
        if let Some(missing_reason) = missing_reason {
            return answer_with(SchemaUrlOutcome::SourceMissing { missing_reason }, refusals);
        }
        if refusals.len() == tokens.len() {
            self.tokens.forget_tokens();
            return SchemaUrlAnswer::refused(refusals);
        }
        answer_with(
            failed(format!("no credential can see {repository_name}")),
            refusals,
        )
    }
}

fn sent_answer(reply: HttpReply) -> SchemaUrlAnswer {
    match reply.status_code {
        STATUS_OK => SchemaUrlAnswer::of(SchemaUrlOutcome::TextSent {
            schema_text: reply.reply_text,
            copy_version: reply.copy_version,
        }),
        STATUS_NOT_MODIFIED => SchemaUrlAnswer::of(SchemaUrlOutcome::CopyConfirmed),
        status => SchemaUrlAnswer::failed(format!("GitHub answered {status}")),
    }
}

fn refusal(token: &GithubToken, status: u16) -> ReadRefusal {
    let answered = match status {
        STATUS_UNAUTHORIZED => "401 Bad credentials".to_owned(),
        status => status.to_string(),
    };
    ReadRefusal {
        refused_by: GITHUB_REFUSER.to_owned(),
        credential_origin: Some(token.credential_origin.clone()),
        refusal_reason: answered,
        renewal_fix: Some(token.renewal_fix.clone()),
    }
}

/// The repository a link's path, after its host, names under
/// `<owner>/<repo>/releases/`, and the release asset that path reads:
/// `download/<tag>/<asset>` or `latest/download/<asset>`.
fn github_release(path: &str) -> Option<GithubRelease> {
    let path = path.split(['?', '#']).next()?;
    let steps: Vec<&str> = path.split('/').collect();
    let [owner, repository, "releases", release_steps @ ..] = steps.as_slice() else {
        return None;
    };
    if owner.is_empty() || repository.is_empty() {
        return None;
    }
    let asset = match release_steps {
        ["latest", "download", asset_name] => Some(ReleaseAsset {
            release: ReleaseName::LatestRelease,
            asset_name: (*asset_name).to_owned(),
        }),
        ["download", tag @ .., asset_name] if !tag.is_empty() => Some(ReleaseAsset {
            release: ReleaseName::TaggedRelease(tag.join("/")),
            asset_name: (*asset_name).to_owned(),
        }),
        _ => None,
    };
    Some(GithubRelease {
        owner: (*owner).to_owned(),
        repository: (*repository).to_owned(),
        asset,
    })
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::thread::JoinHandle;

    use super::*;

    const SCHEMA: &str = r#"{"type": "object"}"#;
    const GOOD_TOKEN: &str = "Bearer mock-good-token";
    const BLIND_TOKEN: &str = "Bearer mock-blind-token";
    const PRIVATE_RELEASE: &str = "mock-org/mock-repo/releases/download/mock/v1.0/mock.schema.json";
    const PUBLIC_RELEASE: &str = "mock-org/public-repo/releases/download/v1/mock.schema.json";

    /// GitHub for one public and one private repository: its pages answer the
    /// public release and 404 for the private one, and its API lets the good token
    /// see the private release, by tag and as the latest, whose asset redirects to
    /// a file store that refuses any request still carrying a token. The blind
    /// token sees nothing. Dropping it, as a failing test does too, stops and
    /// joins its thread.
    struct MockGithub {
        api: String,
        server: Arc<tiny_http::Server>,
        serving: Option<JoinHandle<()>>,
    }

    impl MockGithub {
        fn start() -> Self {
            let server = Arc::new(tiny_http::Server::http("127.0.0.1:0").unwrap());
            let api = format!("http://{}", server.server_addr());
            let (listening, base) = (Arc::clone(&server), api.clone());
            let serving = thread::spawn(move || {
                for request in listening.incoming_requests() {
                    let authorization = request
                        .headers()
                        .iter()
                        .find(|header| header.field.equiv("Authorization"))
                        .map(|header| header.value.as_str().to_owned());
                    let answer = |status: u16, body: String| {
                        tiny_http::Response::from_string(body).with_status_code(status)
                    };
                    let assets = format!(
                        r#"{{"assets": [{{"name": "mock.schema.json", "url": "{base}/assets/7"}}]}}"#
                    );
                    let path = request.url().to_owned();
                    let response = match path.as_str() {
                        page if page.starts_with("/mock-org/public-repo/") => {
                            answer(200, SCHEMA.to_owned())
                        }
                        page if page.starts_with("/mock-org/") => answer(404, String::new()),
                        "/stored/7" if authorization.is_none() => answer(200, SCHEMA.to_owned()),
                        "/stored/7" => answer(400, String::new()),
                        _ if authorization.as_deref() == Some(BLIND_TOKEN) => {
                            answer(404, String::new())
                        }
                        _ if authorization.as_deref() != Some(GOOD_TOKEN) => {
                            answer(401, String::new())
                        }
                        "/repos/mock-org/mock-repo" => answer(200, "{}".to_owned()),
                        "/repos/mock-org/mock-repo/releases/tags/mock%2Fv1.0"
                        | "/repos/mock-org/mock-repo/releases/latest" => answer(200, assets),
                        "/assets/7" => answer(302, String::new()).with_header(
                            tiny_http::Header::from_bytes("Location", format!("{base}/stored/7"))
                                .unwrap(),
                        ),
                        _ => answer(404, String::new()),
                    };
                    let _ = request.respond(response);
                }
            });
            Self {
                api,
                server,
                serving: Some(serving),
            }
        }

        fn link(&self, path: &str) -> String {
            format!("{}/{path}", self.api)
        }

        /// The reader of this GitHub's links, reading with `tokens`.
        fn reader(&self, tokens: Vec<GithubToken>) -> (GithubSchemaReader, Arc<AtomicUsize>) {
            let forgotten = Arc::new(AtomicUsize::new(0));
            let given = GivenTokens {
                tokens,
                forgotten: Arc::clone(&forgotten),
            };
            let prefix: &'static str = Box::leak(format!("{}/", self.api).into_boxed_str());
            let reader = GithubSchemaReader::with_github(Box::new(given), &self.api, vec![prefix]);
            (reader, forgotten)
        }
    }

    impl Drop for MockGithub {
        fn drop(&mut self) {
            self.server.unblock();
            if let Some(serving) = self.serving.take() {
                let _ = serving.join();
            }
        }
    }

    /// Tokens a test names in place of the machine's, counting how often they are
    /// forgotten.
    struct GivenTokens {
        tokens: Vec<GithubToken>,
        forgotten: Arc<AtomicUsize>,
    }

    impl TokenSource for GivenTokens {
        fn github_tokens(&self) -> Vec<GithubToken> {
            self.tokens.clone()
        }

        fn forget_tokens(&self) {
            self.forgotten.fetch_add(1, Ordering::SeqCst);
        }
    }

    fn token(origin: &str, authorization: &str, fix: &str) -> GithubToken {
        GithubToken {
            credential_origin: format!("`{origin}`"),
            authorization: authorization.to_owned(),
            renewal_fix: fix.to_owned(),
        }
    }

    fn stale_token() -> GithubToken {
        token("GH_TOKEN", "Bearer mock-stale-token", "replace `GH_TOKEN`")
    }

    fn good_token() -> GithubToken {
        token("gh auth token", GOOD_TOKEN, "run `gh auth login`")
    }

    fn request(url: &str, machine_credentials: bool) -> SchemaUrlRequest {
        SchemaUrlRequest {
            fetch_url: url.to_owned(),
            machine_credentials,
            kept_copy_version: None,
            answer_deadline: Instant::now() + Duration::from_secs(4),
        }
    }

    #[test]
    fn a_link_names_its_repository_and_the_release_asset_its_path_reads() {
        let tagged = github_release(PRIVATE_RELEASE).unwrap();
        assert_eq!(
            (tagged.owner.as_str(), tagged.repository.as_str()),
            ("mock-org", "mock-repo")
        );
        assert_eq!(
            tagged.asset,
            Some(ReleaseAsset {
                release: ReleaseName::TaggedRelease("mock/v1.0".into()),
                asset_name: "mock.schema.json".into(),
            })
        );
        let latest = github_release("mock-org/mock-repo/releases/latest/download/a.json?x=1");
        assert_eq!(
            latest.unwrap().asset,
            Some(ReleaseAsset {
                release: ReleaseName::LatestRelease,
                asset_name: "a.json".into(),
            })
        );
        assert_eq!(
            github_release("mock-org/mock-repo/releases/tag/v1")
                .unwrap()
                .asset,
            None
        );
        assert_eq!(github_release("mock-org/mock-repo/blob/main/a.json"), None);
    }

    #[test]
    fn a_link_without_credentials_is_read_as_written_a_private_one_refused() {
        let github = MockGithub::start();
        let (reader, _) = github.reader(vec![good_token()]);
        let public = reader.read_schema_url(&request(&github.link(PUBLIC_RELEASE), false));
        assert!(matches!(
            public.read_outcome,
            SchemaUrlOutcome::TextSent { .. }
        ));
        let private = reader.read_schema_url(&request(&github.link(PRIVATE_RELEASE), false));
        assert_eq!(private.read_outcome, SchemaUrlOutcome::ReadRefused);
        assert_eq!(private.read_refusals[0].refusal_reason, "404");
        assert_eq!(private.read_refusals[0].refused_by, "GitHub");
        let not_an_asset = reader.read_schema_url(&request(
            &github.link("mock-org/mock-repo/releases/tag/v1"),
            true,
        ));
        assert_eq!(not_an_asset.read_outcome, SchemaUrlOutcome::ReadRefused);
    }

    #[test]
    fn a_private_release_is_read_with_the_first_token_let_in_each_refused_one_listed() {
        let github = MockGithub::start();
        let (reader, forgotten) = github.reader(vec![stale_token(), good_token()]);
        for link in [
            PRIVATE_RELEASE,
            "mock-org/mock-repo/releases/latest/download/mock.schema.json",
        ] {
            let read = reader.read_schema_url(&request(&github.link(link), true));
            assert!(
                matches!(read.read_outcome, SchemaUrlOutcome::TextSent { .. }),
                "{read:?}"
            );
            assert_eq!(
                read.read_refusals,
                vec![ReadRefusal {
                    refused_by: "GitHub".into(),
                    credential_origin: Some("`GH_TOKEN`".into()),
                    refusal_reason: "401 Bad credentials".into(),
                    renewal_fix: Some("replace `GH_TOKEN`".into()),
                }]
            );
        }
        let shouted_link = github
            .link(PRIVATE_RELEASE)
            .replacen("http://", "HTTP://", 1);
        let shouted = reader.read_schema_url(&request(&shouted_link, true));
        assert!(
            matches!(shouted.read_outcome, SchemaUrlOutcome::TextSent { .. }),
            "{shouted:?}"
        );
        assert_eq!(forgotten.load(Ordering::SeqCst), 0);
    }

    #[test]
    fn a_release_or_an_asset_the_repository_lacks_is_missing() {
        let github = MockGithub::start();
        let (reader, _) = github.reader(vec![good_token()]);
        let no_release = reader.read_schema_url(&request(
            &github.link("mock-org/mock-repo/releases/download/v9/mock.schema.json"),
            true,
        ));
        assert_eq!(
            no_release.read_outcome,
            SchemaUrlOutcome::SourceMissing {
                missing_reason: "`mock-org/mock-repo` has no release `v9`".into()
            }
        );
        let no_asset = reader.read_schema_url(&request(
            &github.link("mock-org/mock-repo/releases/latest/download/other.json"),
            true,
        ));
        assert_eq!(
            no_asset.read_outcome,
            SchemaUrlOutcome::SourceMissing {
                missing_reason:
                    "the latest release of `mock-org/mock-repo` has no asset `other.json`".into()
            }
        );
    }

    #[test]
    fn every_token_refused_is_listed_and_the_tokens_read_again_next_time() {
        let github = MockGithub::start();
        let (reader, forgotten) = github.reader(vec![stale_token()]);
        let refused = reader.read_schema_url(&request(&github.link(PRIVATE_RELEASE), true));
        assert_eq!(refused.read_outcome, SchemaUrlOutcome::ReadRefused);
        assert_eq!(refused.read_refusals.len(), 1);
        assert_eq!(forgotten.load(Ordering::SeqCst), 1);
        let (blind, _) = github.reader(vec![token("GITHUB_TOKEN", BLIND_TOKEN, "x")]);
        let unseen = blind.read_schema_url(&request(&github.link(PRIVATE_RELEASE), true));
        assert_eq!(
            unseen.read_outcome,
            SchemaUrlOutcome::ReadFailed {
                failure_reason: "no credential can see `mock-org/mock-repo`".into()
            }
        );
        let (none, _) = github.reader(Vec::new());
        let unset = none.read_schema_url(&request(&github.link(PRIVATE_RELEASE), true));
        assert_eq!(
            unset.read_refusals[0].renewal_fix.as_deref(),
            Some(NO_TOKEN_FIX)
        );
    }
}
