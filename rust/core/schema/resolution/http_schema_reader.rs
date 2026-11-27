//! The core's reader of `http` and `https` links: what a server answers a link
//! with, read with the `.netrc` login of its host when asked for the machine's
//! credentials.

use super::http_reading::{HttpReply, http_conditional_headers, http_get};
use super::machine_credentials::{CredentialSource, HostLogin, MachineCredentials};
use super::schema_url_reading::{
    ReadRefusal, SchemaUrlAnswer, SchemaUrlOutcome, SchemaUrlReader, SchemaUrlRequest,
};

const STATUS_OK: u16 = 200;
const STATUS_NOT_MODIFIED: u16 = 304;
const STATUS_UNAUTHORIZED: u16 = 401;
const STATUS_FORBIDDEN: u16 = 403;
const STATUS_NOT_FOUND: u16 = 404;

pub struct HttpSchemaReader {
    credentials: Box<dyn CredentialSource>,
}

impl Default for HttpSchemaReader {
    fn default() -> Self {
        Self::with_credentials(Box::new(MachineCredentials::default()))
    }
}

impl SchemaUrlReader for HttpSchemaReader {
    fn url_prefixes(&self) -> &[&str] {
        &["http://", "https://"]
    }

    fn reads_remote_sources(&self) -> bool {
        true
    }

    fn read_schema_url(&self, request: &SchemaUrlRequest) -> SchemaUrlAnswer {
        let host = url_host(&request.fetch_url).unwrap_or_default().to_owned();
        let mut headers = http_conditional_headers(request.kept_copy_version.as_ref());
        let login = if request.machine_credentials {
            let Some(login) = self.credentials.host_login(&host) else {
                return SchemaUrlAnswer::refused(vec![ReadRefusal {
                    refused_by: host.clone(),
                    credential_origin: None,
                    refusal_reason: format!("no `.netrc` entry for {host}"),
                    renewal_fix: Some(format!("add a `.netrc` entry for {host}")),
                }]);
            };
            headers.push(("Authorization", login.authorization()));
            Some(login)
        } else {
            None
        };
        let reply = match http_get(&request.fetch_url, &headers, request.answer_deadline) {
            Ok(reply) => reply,
            Err(error) => return SchemaUrlAnswer::failed(error),
        };
        if matches!(reply.status_code, STATUS_UNAUTHORIZED | STATUS_FORBIDDEN) {
            if login.is_some() {
                self.credentials.forget_credentials();
            }
            return SchemaUrlAnswer::refused(vec![refusal(
                &host,
                login.as_ref(),
                reply.status_code,
            )]);
        }
        sent_answer(reply)
    }
}

impl HttpSchemaReader {
    pub(crate) fn with_credentials(credentials: Box<dyn CredentialSource>) -> Self {
        Self { credentials }
    }
}

/// A server's answer to a request for the schema itself.
fn sent_answer(reply: HttpReply) -> SchemaUrlAnswer {
    match reply.status_code {
        STATUS_OK => SchemaUrlAnswer::of(SchemaUrlOutcome::TextSent {
            schema_text: reply.reply_text,
            copy_version: reply.copy_version,
        }),
        STATUS_NOT_MODIFIED => SchemaUrlAnswer::of(SchemaUrlOutcome::CopyConfirmed),
        STATUS_NOT_FOUND => SchemaUrlAnswer::missing("the server answered 404"),
        status => SchemaUrlAnswer::failed(format!("the server answered {status}")),
    }
}

fn refusal(host: &str, login: Option<&HostLogin>, status: u16) -> ReadRefusal {
    ReadRefusal {
        refused_by: host.to_owned(),
        credential_origin: login.map(|login| login.credential_origin.clone()),
        refusal_reason: status.to_string(),
        renewal_fix: login.map(|login| login.renewal_fix.clone()),
    }
}

fn url_host(url: &str) -> Option<&str> {
    let rest = url.split_once("://")?.1;
    let authority = rest.split(['/', '?', '#']).next()?;
    authority.rsplit('@').next()?.split(':').next()
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::thread::{self, JoinHandle};
    use std::time::{Duration, Instant};

    use serde_json::{Value as JsonValue, json};

    use super::*;

    const SCHEMA: &str = r#"{"type": "object"}"#;
    const ETAG: &str = "\"mock-etag\"";
    const LAST_MODIFIED: &str = "Wed, 01 Jan 2026 00:00:00 GMT";
    const MOCK_LOGIN: &str = "Basic bW9jay11c2VyOm1vY2stc2VjcmV0";

    /// A server answering `/schema.json` with the schema, or `304` to a request
    /// carrying its ETag; `/private.json` only to the mock login; `/broken.json`
    /// with `500`; `/moved.json` by redirecting to `/landing.json`, which answers
    /// only a request without a login; and anything else with `404`. Dropping it,
    /// as a failing test does too, stops and joins its thread.
    struct MockServer {
        address: String,
        server: Arc<tiny_http::Server>,
        serving: Option<JoinHandle<()>>,
    }

    impl MockServer {
        fn start() -> Self {
            let server = Arc::new(tiny_http::Server::http("127.0.0.1:0").unwrap());
            let address = format!("http://{}", server.server_addr());
            let listening = Arc::clone(&server);
            let serving = thread::spawn(move || {
                for request in listening.incoming_requests() {
                    let header = |name: &'static str| {
                        request
                            .headers()
                            .iter()
                            .find(|header| header.field.equiv(name))
                            .map(|header| header.value.as_str().to_owned())
                    };
                    let answer = |status: u16, body: &str| {
                        tiny_http::Response::from_string(body).with_status_code(status)
                    };
                    let response = match request.url() {
                        "/schema.json" if header("If-None-Match").as_deref() == Some(ETAG) => {
                            answer(304, "")
                        }
                        "/schema.json" => answer(200, SCHEMA)
                            .with_header(tiny_http::Header::from_bytes("ETag", ETAG).unwrap())
                            .with_header(
                                tiny_http::Header::from_bytes("Last-Modified", LAST_MODIFIED)
                                    .unwrap(),
                            ),
                        "/private.json"
                            if header("Authorization").as_deref() == Some(MOCK_LOGIN) =>
                        {
                            answer(200, SCHEMA)
                        }
                        "/private.json" => answer(401, ""),
                        "/broken.json" => answer(500, ""),
                        "/moved.json" => answer(302, "").with_header(
                            tiny_http::Header::from_bytes("Location", "/landing.json").unwrap(),
                        ),
                        "/landing.json" if header("Authorization").is_none() => answer(200, SCHEMA),
                        "/landing.json" => answer(400, ""),
                        _ => answer(404, ""),
                    };
                    let _ = request.respond(response);
                }
            });
            Self {
                address,
                server,
                serving: Some(serving),
            }
        }

        fn url(&self, path: &str) -> String {
            format!("{}{path}", self.address)
        }
    }

    impl Drop for MockServer {
        fn drop(&mut self) {
            self.server.unblock();
            if let Some(serving) = self.serving.take() {
                let _ = serving.join();
            }
        }
    }

    /// A login a test names in place of the machine's `.netrc`, counting how often
    /// the credentials are forgotten.
    struct GivenLogin {
        login: Option<HostLogin>,
        forgotten: Arc<AtomicUsize>,
    }

    impl CredentialSource for GivenLogin {
        fn host_login(&self, _host: &str) -> Option<HostLogin> {
            self.login.clone()
        }

        fn forget_credentials(&self) {
            self.forgotten.fetch_add(1, Ordering::SeqCst);
        }
    }

    fn mock_login(password: &str) -> HostLogin {
        HostLogin {
            credential_origin: "the `.netrc` entry for 127.0.0.1".into(),
            login_name: "mock-user".into(),
            login_password: password.into(),
            renewal_fix: "update the `.netrc` entry for 127.0.0.1".into(),
        }
    }

    fn reader_with(login: Option<HostLogin>) -> (HttpSchemaReader, Arc<AtomicUsize>) {
        let forgotten = Arc::new(AtomicUsize::new(0));
        let credentials = GivenLogin {
            login,
            forgotten: Arc::clone(&forgotten),
        };
        (
            HttpSchemaReader::with_credentials(Box::new(credentials)),
            forgotten,
        )
    }

    fn request(url: &str, machine_credentials: bool, kept: Option<JsonValue>) -> SchemaUrlRequest {
        SchemaUrlRequest {
            fetch_url: url.to_owned(),
            machine_credentials,
            kept_copy_version: kept,
            answer_deadline: Instant::now() + Duration::from_secs(5),
        }
    }

    #[test]
    fn a_link_is_sent_with_its_version_and_a_current_copy_is_confirmed() {
        let server = MockServer::start();
        let (reader, _) = reader_with(None);
        let sent = reader.read_schema_url(&request(&server.url("/schema.json"), false, None));
        let SchemaUrlOutcome::TextSent {
            schema_text,
            copy_version,
        } = sent.read_outcome
        else {
            panic!("not sent: {sent:?}");
        };
        assert_eq!(schema_text, SCHEMA);
        assert_eq!(
            copy_version,
            json!({ "etag": ETAG, "last_modified": LAST_MODIFIED })
        );
        let confirmed = reader.read_schema_url(&request(
            &server.url("/schema.json"),
            false,
            Some(copy_version),
        ));
        assert_eq!(confirmed.read_outcome, SchemaUrlOutcome::CopyConfirmed);
        assert_eq!(
            http_conditional_headers(Some(
                &json!({ "etag": null, "last_modified": LAST_MODIFIED })
            )),
            vec![("If-Modified-Since", LAST_MODIFIED.to_owned())]
        );
    }

    #[test]
    fn a_404_is_missing_and_another_status_or_no_answer_fails() {
        let server = MockServer::start();
        let (reader, _) = reader_with(None);
        let missing = reader.read_schema_url(&request(&server.url("/absent.json"), false, None));
        assert_eq!(
            missing.read_outcome,
            SchemaUrlOutcome::SourceMissing {
                missing_reason: "the server answered 404".into()
            }
        );
        let broken = reader.read_schema_url(&request(&server.url("/broken.json"), false, None));
        assert_eq!(
            broken.read_outcome,
            SchemaUrlOutcome::ReadFailed {
                failure_reason: "the server answered 500".into()
            }
        );
        let unanswered =
            reader.read_schema_url(&request("http://127.0.0.1:9/schema.json", false, None));
        assert!(matches!(
            unanswered.read_outcome,
            SchemaUrlOutcome::ReadFailed { .. }
        ));
    }

    #[test]
    fn a_refused_link_is_read_with_the_netrc_login_of_its_host() {
        let server = MockServer::start();
        let (anonymous, _) = reader_with(None);
        let refused =
            anonymous.read_schema_url(&request(&server.url("/private.json"), false, None));
        assert_eq!(refused.read_outcome, SchemaUrlOutcome::ReadRefused);
        assert_eq!(refused.read_refusals[0].refusal_reason, "401");
        assert_eq!(refused.read_refusals[0].credential_origin, None);
        let (logged_in, _) = reader_with(Some(mock_login("mock-secret")));
        let sent = logged_in.read_schema_url(&request(&server.url("/private.json"), true, None));
        assert!(matches!(
            sent.read_outcome,
            SchemaUrlOutcome::TextSent { .. }
        ));
    }

    #[test]
    fn a_redirect_is_followed_without_the_login() {
        let server = MockServer::start();
        let (reader, _) = reader_with(Some(mock_login("mock-secret")));
        let moved = reader.read_schema_url(&request(&server.url("/moved.json"), true, None));
        assert!(
            matches!(moved.read_outcome, SchemaUrlOutcome::TextSent { .. }),
            "{moved:?}"
        );
    }

    #[test]
    fn a_refused_login_is_listed_and_the_credentials_read_again_next_time() {
        let server = MockServer::start();
        let (reader, forgotten) = reader_with(Some(mock_login("mock-stale")));
        let refused = reader.read_schema_url(&request(&server.url("/private.json"), true, None));
        assert_eq!(refused.read_outcome, SchemaUrlOutcome::ReadRefused);
        assert_eq!(
            refused.read_refusals,
            vec![ReadRefusal {
                refused_by: "127.0.0.1".into(),
                credential_origin: Some("the `.netrc` entry for 127.0.0.1".into()),
                refusal_reason: "401".into(),
                renewal_fix: Some("update the `.netrc` entry for 127.0.0.1".into()),
            }]
        );
        assert_eq!(forgotten.load(Ordering::SeqCst), 1);
        let (no_entry, _) = reader_with(None);
        let unset = no_entry.read_schema_url(&request(&server.url("/private.json"), true, None));
        assert_eq!(
            unset.read_refusals[0].renewal_fix.as_deref(),
            Some("add a `.netrc` entry for 127.0.0.1")
        );
    }
}
