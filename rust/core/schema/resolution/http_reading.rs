//! An HTTP read for a schema URL reader: the answer to a GET, within the time a
//! request's answer is due, a redirect followed without the `Authorization`
//! header, and the headers asking whether a kept copy is current.

use std::sync::OnceLock;
use std::time::{Duration, Instant};

use serde_json::{Value as JsonValue, json};

const REQUEST_TIMEOUT: Duration = Duration::from_secs(30);
const STATUS_OK: u16 = 200;

/// A server's answer: its status, the copy version its `ETag` and
/// `Last-Modified` give, and, for a `200`, its text.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct HttpReply {
    pub status_code: u16,
    pub copy_version: JsonValue,
    pub reply_text: String,
}

/// The answer to a GET of `url` with `headers`, due by `answer_deadline`; else
/// why there is none.
pub fn http_get(
    url: &str,
    headers: &[(&str, String)],
    answer_deadline: Instant,
) -> Result<HttpReply, String> {
    let remaining = answer_deadline
        .saturating_duration_since(Instant::now())
        .min(REQUEST_TIMEOUT);
    let mut call = shared_agent()
        .get(url)
        .config()
        .timeout_global(Some(remaining))
        .build();
    for (name, value) in headers {
        call = call.header(*name, value);
    }
    let mut response = call.call().map_err(|error| error.to_string())?;
    let header = |name: &str| {
        response
            .headers()
            .get(name)
            .and_then(|value| value.to_str().ok())
            .map(str::to_owned)
    };
    let copy_version = json!({
        "etag": header("etag"),
        "last_modified": header("last-modified"),
    });
    let status_code = response.status().as_u16();
    let reply_text = if status_code == STATUS_OK {
        response
            .body_mut()
            .read_to_string()
            .map_err(|error| error.to_string())?
    } else {
        String::new()
    };
    Ok(HttpReply {
        status_code,
        copy_version,
        reply_text,
    })
}

/// The headers asking a server whether the kept copy is current: `If-None-Match`
/// with its `etag`, else `If-Modified-Since` with its `last_modified`.
pub fn http_conditional_headers(
    kept_copy_version: Option<&JsonValue>,
) -> Vec<(&'static str, String)> {
    let field = |name: &str| {
        kept_copy_version
            .and_then(|version| version[name].as_str())
            .map(str::to_owned)
    };
    match (field("etag"), field("last_modified")) {
        (Some(etag), _) => vec![("If-None-Match", etag)],
        (None, Some(last_modified)) => vec![("If-Modified-Since", last_modified)],
        (None, None) => Vec::new(),
    }
}

fn shared_agent() -> &'static ureq::Agent {
    static AGENT: OnceLock<ureq::Agent> = OnceLock::new();
    AGENT.get_or_init(|| {
        ureq::Agent::config_builder()
            .http_status_as_error(false)
            .build()
            .into()
    })
}
