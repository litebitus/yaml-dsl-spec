//! The YAML DSL standard's reader of schema URLs on S3, `s3://<bucket>/<key>`,
//! read with the AWS SDK: unsigned without the machine's credentials, else with
//! the SDK's default chain, as the role a URL's `assume_role` names where it
//! names one.

use std::collections::BTreeMap;
use std::sync::{Arc, Mutex, PoisonError, mpsc};
use std::time::Instant;

use aws_config::sts::AssumeRoleProvider;
use aws_config::timeout::TimeoutConfig;
use aws_config::{BehaviorVersion, Region, SdkConfig};
use aws_sdk_s3::Client;
use aws_sdk_s3::config::SharedCredentialsProvider;
use aws_sdk_s3::error::{DisplayErrorContext, ProvideErrorMetadata, SdkError};
use aws_sdk_s3::operation::get_object::{GetObjectError, GetObjectOutput};
use aws_types::service_config::ServiceConfigKey;
use serde_json::{Value as JsonValue, json};
use yaml_dsl_spec::code_span;
use yaml_dsl_spec::schema::resolution::{
    ReadRefusal, SchemaUrlAnswer, SchemaUrlOutcome, SchemaUrlReader, SchemaUrlRequest,
};

const S3_PREFIX: &str = "s3://";
const ASSUME_ROLE_PARAMETER: &str = "assume_role";
const DEFAULT_REGION: &str = "us-east-1";
const S3_SERVICE_ID: &str = "S3";
const ENDPOINT_VARIABLE: &str = "AWS_ENDPOINT_URL";
const ENDPOINT_PROFILE_KEY: &str = "endpoint_url";
const BUCKET_REGION_HEADER: &str = "x-amz-bucket-region";
const AWS_REFUSER: &str = "AWS";
const MACHINE_CREDENTIALS_ORIGIN: &str = "the machine's AWS credentials";
const STATUS_NOT_MODIFIED: u16 = 304;
const STATUS_FORBIDDEN: u16 = 403;
const CREDENTIALS_REFUSED_CODES: [&str; 4] = [
    "ExpiredToken",
    "InvalidAccessKeyId",
    "InvalidClientTokenId",
    "SignatureDoesNotMatch",
];
const REGION_REDIRECT_CODES: [&str; 3] = [
    "PermanentRedirect",
    "AuthorizationHeaderMalformed",
    "IllegalLocationConstraintException",
];
const CREDENTIALS_FIX: &str =
    "run `aws sso login`, or set `AWS_PROFILE` to a profile that reads it";

/// The URL reader of `s3://<bucket>/<key>`.
pub struct S3SchemaReader {
    runtime: tokio::runtime::Runtime,
    shared: Arc<ReaderState>,
}

/// What the reader keeps between reads: the machine's configuration and each
/// role's credentials, read when first needed, and each bucket's region.
struct ReaderState {
    role_session_name: String,
    machine_config: Mutex<Option<SdkConfig>>,
    role_credentials: Mutex<BTreeMap<String, SharedCredentialsProvider>>,
    bucket_regions: Mutex<BTreeMap<String, String>>,
}

/// An `s3://<bucket>/<key>` URL: where its object is, and the role it is read as.
#[derive(Debug, PartialEq, Eq)]
struct S3Object {
    bucket: String,
    key: String,
    assume_role: Option<String>,
}

/// Who a read is made as.
#[derive(Clone)]
enum ReadIdentity {
    Unsigned,
    Machine,
    AssumedRole(String),
}

impl S3SchemaReader {
    /// The reader, a role assumed under `role_session_name`, the consumer's name
    /// for itself.
    ///
    /// # Panics
    ///
    /// When the machine cannot start the reader's runtime's threads.
    pub fn new(role_session_name: &str) -> Self {
        let runtime = tokio::runtime::Builder::new_multi_thread()
            .worker_threads(2)
            .enable_all()
            .build()
            .expect("the S3 schema reader's runtime starts");
        Self {
            runtime,
            shared: Arc::new(ReaderState {
                role_session_name: role_session_name.to_owned(),
                machine_config: Mutex::default(),
                role_credentials: Mutex::default(),
                bucket_regions: Mutex::default(),
            }),
        }
    }
}

impl SchemaUrlReader for S3SchemaReader {
    fn url_prefixes(&self) -> &[&str] {
        &[S3_PREFIX]
    }

    fn reads_remote_sources(&self) -> bool {
        true
    }

    fn read_schema_url(&self, request: &SchemaUrlRequest) -> SchemaUrlAnswer {
        let object = match S3Object::parse(&request.fetch_url) {
            Ok(object) => object,
            Err(reason) => return SchemaUrlAnswer::failed(reason),
        };
        let identity = match (&object.assume_role, request.machine_credentials) {
            (_, false) => ReadIdentity::Unsigned,
            (Some(role), true) => ReadIdentity::AssumedRole(role.clone()),
            (None, true) => ReadIdentity::Machine,
        };
        let kept_etag = request
            .kept_copy_version
            .as_ref()
            .and_then(|version| version["etag"].as_str())
            .map(str::to_owned);
        let remaining = request
            .answer_deadline
            .saturating_duration_since(Instant::now());
        let (sending, answered) = mpsc::channel();
        let shared = Arc::clone(&self.shared);
        self.runtime.spawn(async move {
            let answer = shared
                .answer(&object, &identity, kept_etag, remaining)
                .await;
            let _ = sending.send(answer);
        });
        answered
            .recv_timeout(remaining)
            .unwrap_or_else(|_| SchemaUrlAnswer::failed("S3 did not answer in time"))
    }
}

impl ReaderState {
    async fn answer(
        &self,
        object: &S3Object,
        identity: &ReadIdentity,
        kept_etag: Option<String>,
        remaining: std::time::Duration,
    ) -> SchemaUrlAnswer {
        let config = self.config_of(identity).await;
        let region = self.bucket_region(&object.bucket, &config);
        let mut read = self
            .get_object(&config, &region, object, kept_etag.as_deref(), remaining)
            .await;
        if let Err(error) = &read
            && let Some(bucket_region) = redirected_region(error)
            && bucket_region != region
        {
            self.bucket_regions
                .lock()
                .unwrap_or_else(PoisonError::into_inner)
                .insert(object.bucket.clone(), bucket_region.clone());
            read = self
                .get_object(
                    &config,
                    &bucket_region,
                    object,
                    kept_etag.as_deref(),
                    remaining,
                )
                .await;
        }
        let answer = match read {
            Ok(output) => sent_answer(output).await,
            Err(error) => error_answer(&error, object, identity),
        };
        if answer.read_outcome == SchemaUrlOutcome::ReadRefused
            && !matches!(identity, ReadIdentity::Unsigned)
        {
            self.forget_credentials();
        }
        answer
    }

    async fn get_object(
        &self,
        config: &SdkConfig,
        region: &str,
        object: &S3Object,
        kept_etag: Option<&str>,
        remaining: std::time::Duration,
    ) -> Result<GetObjectOutput, Box<SdkError<GetObjectError>>> {
        let mut builder = aws_sdk_s3::config::Builder::from(config)
            .region(Region::new(region.to_owned()))
            .timeout_config(
                TimeoutConfig::builder()
                    .operation_timeout(remaining)
                    .build(),
            );
        if names_endpoint(config) {
            builder = builder.force_path_style(true);
        }
        let mut call = Client::from_conf(builder.build())
            .get_object()
            .bucket(&object.bucket)
            .key(&object.key);
        if let Some(etag) = kept_etag {
            call = call.if_none_match(etag);
        }
        call.send().await.map_err(Box::new)
    }

    /// The configuration a read as `identity` is made with.
    async fn config_of(&self, identity: &ReadIdentity) -> SdkConfig {
        match identity {
            ReadIdentity::Unsigned => {
                aws_config::defaults(BehaviorVersion::latest())
                    .no_credentials()
                    .load()
                    .await
            }
            ReadIdentity::Machine => self.machine_config().await,
            ReadIdentity::AssumedRole(role) => {
                let machine = self.machine_config().await;
                let provider = self.role_provider(role, &machine).await;
                machine
                    .into_builder()
                    .credentials_provider(provider)
                    .build()
            }
        }
    }

    async fn machine_config(&self) -> SdkConfig {
        if let Some(config) = self
            .machine_config
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .clone()
        {
            return config;
        }
        let loaded = aws_config::defaults(BehaviorVersion::latest()).load().await;
        *self
            .machine_config
            .lock()
            .unwrap_or_else(PoisonError::into_inner) = Some(loaded.clone());
        loaded
    }

    /// The credentials of `role`, assumed with the machine's credentials and
    /// renewed by the SDK as they expire.
    async fn role_provider(&self, role: &str, machine: &SdkConfig) -> SharedCredentialsProvider {
        if let Some(provider) = self
            .role_credentials
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .get(role)
            .cloned()
        {
            return provider;
        }
        let provider = SharedCredentialsProvider::new(
            AssumeRoleProvider::builder(role)
                .session_name(&self.role_session_name)
                .configure(machine)
                .build()
                .await,
        );
        self.role_credentials
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .insert(role.to_owned(), provider.clone());
        provider
    }

    fn bucket_region(&self, bucket: &str, config: &SdkConfig) -> String {
        let known = self
            .bucket_regions
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .get(bucket)
            .cloned();
        known
            .or_else(|| config.region().map(|region| region.to_string()))
            .unwrap_or_else(|| DEFAULT_REGION.to_owned())
    }

    /// Reads the machine's credentials again when next asked, after a refusal.
    fn forget_credentials(&self) {
        *self
            .machine_config
            .lock()
            .unwrap_or_else(PoisonError::into_inner) = None;
        self.role_credentials
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .clear();
    }
}

impl S3Object {
    fn parse(url: &str) -> Result<Self, String> {
        let link = url.get(S3_PREFIX.len()..).unwrap_or_default();
        let (path, query) = match link.split_once('?') {
            Some((path, query)) => (path, Some(query)),
            None => (link, None),
        };
        let (bucket, key) = path
            .split_once('/')
            .filter(|(bucket, key)| !bucket.is_empty() && !key.is_empty())
            .ok_or_else(|| format!("an S3 URL is {}", code_span("s3://<bucket>/<key>")))?;
        let mut assume_role = None;
        for parameter in query.into_iter().flat_map(|query| query.split('&')) {
            match parameter.split_once('=') {
                Some((ASSUME_ROLE_PARAMETER, role)) if !role.is_empty() => {
                    assume_role = Some(role.to_owned());
                }
                _ => {
                    let name = parameter
                        .split_once('=')
                        .map_or(parameter, |(name, _)| name);
                    return Err(format!(
                        "{} is not a parameter of an {} URL; it takes {}",
                        code_span(name),
                        code_span("s3"),
                        code_span("assume_role=<role ARN>")
                    ));
                }
            }
        }
        Ok(Self {
            bucket: bucket.to_owned(),
            key: key.to_owned(),
            assume_role,
        })
    }
}

/// Whether the machine's AWS configuration names an endpoint for S3: the one
/// `AWS_ENDPOINT_URL_S3` or the profile's `s3` section names, else the one every
/// service shares.
fn names_endpoint(config: &SdkConfig) -> bool {
    let s3_endpoint = ServiceConfigKey::builder()
        .service_id(S3_SERVICE_ID)
        .env(ENDPOINT_VARIABLE)
        .profile(ENDPOINT_PROFILE_KEY)
        .build()
        .ok()
        .and_then(|key| config.service_config()?.load_config(key));
    s3_endpoint.is_some() || config.endpoint_url().is_some()
}

async fn sent_answer(output: GetObjectOutput) -> SchemaUrlAnswer {
    let copy_version: JsonValue = json!({ "etag": output.e_tag() });
    match output.body.collect().await {
        Ok(collected) => match String::from_utf8(collected.into_bytes().to_vec()) {
            Ok(schema_text) => SchemaUrlAnswer::of(SchemaUrlOutcome::TextSent {
                schema_text,
                copy_version,
            }),
            Err(error) => SchemaUrlAnswer::failed(error.to_string()),
        },
        Err(error) => SchemaUrlAnswer::failed(error.to_string()),
    }
}

/// The region a refused or redirected read names its bucket in, where it names one.
fn redirected_region<E: ProvideErrorMetadata>(error: &SdkError<E>) -> Option<String> {
    let SdkError::ServiceError(service) = error else {
        return None;
    };
    let code = service.err().code().unwrap_or_default();
    let redirected = REGION_REDIRECT_CODES.contains(&code)
        || matches!(service.raw().status().as_u16(), 301 | 307);
    let region = service
        .raw()
        .headers()
        .get(BUCKET_REGION_HEADER)
        .map(str::to_owned);
    redirected.then_some(region).flatten()
}

/// What an S3 error comes to.
fn error_answer<E: ProvideErrorMetadata + std::error::Error + 'static>(
    error: &SdkError<E>,
    object: &S3Object,
    identity: &ReadIdentity,
) -> SchemaUrlAnswer {
    let SdkError::ServiceError(service) = error else {
        let said = DisplayErrorContext(error).to_string();
        return credentials_answer(&said, identity)
            .unwrap_or_else(|| SchemaUrlAnswer::failed(said));
    };
    let status = service.raw().status().as_u16();
    let code = service.err().code().unwrap_or_default();
    let message = service.err().message().unwrap_or_default();
    if status == STATUS_NOT_MODIFIED {
        return SchemaUrlAnswer::of(SchemaUrlOutcome::CopyConfirmed);
    }
    match code {
        "NoSuchKey" => {
            return SchemaUrlAnswer::missing(format!(
                "no key {} in bucket {}",
                code_span(&object.key),
                code_span(&object.bucket)
            ));
        }
        "NoSuchBucket" => {
            return SchemaUrlAnswer::missing(format!("no bucket {}", code_span(&object.bucket)));
        }
        _ => {}
    }
    let reason = if code.is_empty() && message.is_empty() {
        status.to_string()
    } else {
        refusal_reason(code, message)
    };
    if CREDENTIALS_REFUSED_CODES.contains(&code) {
        return refused(identity, reason, Some(CREDENTIALS_FIX.to_owned()));
    }
    if code == "AccessDenied" || status == STATUS_FORBIDDEN {
        let fix = match identity {
            ReadIdentity::Unsigned => None,
            ReadIdentity::Machine => Some(
                "use a profile allowed `s3:GetObject` on it, such as `AWS_PROFILE=<profile>`"
                    .to_owned(),
            ),
            ReadIdentity::AssumedRole(role) => Some(format!("allow {role} `s3:GetObject` on it")),
        };
        return refused(identity, reason, fix);
    }
    SchemaUrlAnswer::failed(if code.is_empty() {
        format!("S3 answered {status}")
    } else {
        reason
    })
}

/// The refusal an error short of S3 comes to: credentials the chain does not
/// give, or a role the machine is not allowed to assume.
fn credentials_answer(said: &str, identity: &ReadIdentity) -> Option<SchemaUrlAnswer> {
    if let ReadIdentity::AssumedRole(role) = identity
        && (said.contains("AccessDenied") || said.contains("not authorized"))
    {
        return Some(refused(
            &ReadIdentity::Machine,
            refusal_reason("AccessDenied", said),
            Some(format!(
                "use a profile allowed to assume {role}, such as `AWS_PROFILE=<profile>`"
            )),
        ));
    }
    let credentials_missing = said.contains("credentials")
        && (said.contains("no providers")
            || said.contains("expired")
            || said.contains("failed to load"));
    credentials_missing.then(|| {
        refused(
            &ReadIdentity::Machine,
            "the machine's AWS credentials are absent or expired".to_owned(),
            Some(CREDENTIALS_FIX.to_owned()),
        )
    })
}

fn refused(
    identity: &ReadIdentity,
    refusal_reason: String,
    renewal_fix: Option<String>,
) -> SchemaUrlAnswer {
    let credential_origin = match identity {
        ReadIdentity::Unsigned => None,
        ReadIdentity::Machine => Some(MACHINE_CREDENTIALS_ORIGIN.to_owned()),
        ReadIdentity::AssumedRole(role) => Some(format!("role {role}")),
    };
    SchemaUrlAnswer::refused(vec![ReadRefusal {
        refused_by: AWS_REFUSER.to_owned(),
        credential_origin,
        refusal_reason,
        renewal_fix,
    }])
}

/// An S3 error cut to its code and the identity it names, such as
/// `AccessDenied for arn:aws:iam::<account>:user/<name>`.
fn refusal_reason(code: &str, message: &str) -> String {
    let named = message
        .split_once("User: ")
        .and_then(|(_, rest)| rest.split_whitespace().next());
    match (code.is_empty(), named) {
        (false, Some(named)) => format!("{code} for {named}"),
        (false, None) => code.to_owned(),
        (true, _) => message.to_owned(),
    }
}

#[cfg(test)]
mod tests {
    use std::time::Duration;

    use super::*;

    /// The Moto server the tests read from, which `make test` starts.
    fn moto_endpoint() -> String {
        let endpoint = std::env::var("AWS_ENDPOINT_URL")
            .expect("the S3 tests read from a Moto server: run them through `make test`");
        assert!(
            endpoint.starts_with("http://127.0.0.1:") || endpoint.starts_with("http://localhost:"),
            "the S3 tests read only from a local Moto server, not {endpoint}"
        );
        endpoint
    }

    /// The reader as a consumer makes it, the Moto server named by the environment only.
    fn reader() -> S3SchemaReader {
        moto_endpoint();
        S3SchemaReader::new("mock-session")
    }

    /// An object a test seeds: its key, its text, and whether anyone may read it.
    struct SeededObject {
        object_key: &'static str,
        object_text: &'static str,
        publicly_readable: bool,
    }

    /// Creates `bucket` holding `objects` in the Moto server.
    fn seed(reader: &S3SchemaReader, bucket: &str, objects: &[SeededObject]) {
        let endpoint = moto_endpoint();
        reader.runtime.block_on(async {
            let config = aws_config::defaults(BehaviorVersion::latest()).load().await;
            let client = Client::from_conf(
                aws_sdk_s3::config::Builder::from(&config)
                    .endpoint_url(endpoint)
                    .force_path_style(true)
                    .build(),
            );
            client.create_bucket().bucket(bucket).send().await.unwrap();
            for object in objects {
                let mut put = client
                    .put_object()
                    .bucket(bucket)
                    .key(object.object_key)
                    .body(object.object_text.as_bytes().to_vec().into());
                if object.publicly_readable {
                    put = put.acl(aws_sdk_s3::types::ObjectCannedAcl::PublicRead);
                }
                put.send().await.unwrap();
            }
        });
    }

    fn request(url: &str, machine_credentials: bool, kept: Option<JsonValue>) -> SchemaUrlRequest {
        SchemaUrlRequest {
            fetch_url: url.to_owned(),
            machine_credentials,
            kept_copy_version: kept,
            answer_deadline: Instant::now() + Duration::from_secs(4),
        }
    }

    const SCHEMA: &str = r#"{"type": "object"}"#;

    fn private_object(object_key: &'static str) -> SeededObject {
        SeededObject {
            object_key,
            object_text: SCHEMA,
            publicly_readable: false,
        }
    }

    #[test]
    fn an_object_is_sent_with_its_etag_and_a_current_copy_is_confirmed() {
        let reader = reader();
        let public_object = SeededObject {
            publicly_readable: true,
            ..private_object("schemas/public.schema.json")
        };
        seed(
            &reader,
            "mock-sent-bucket",
            &[private_object("schemas/mock.schema.json"), public_object],
        );
        let private_url = "s3://mock-sent-bucket/schemas/mock.schema.json";
        let public_url = "s3://mock-sent-bucket/schemas/public.schema.json";
        for (url, machine_credentials) in [(public_url, false), (private_url, true)] {
            let sent = reader.read_schema_url(&request(url, machine_credentials, None));
            let SchemaUrlOutcome::TextSent {
                schema_text,
                copy_version,
            } = sent.read_outcome
            else {
                panic!("not sent: {sent:?}");
            };
            assert_eq!(schema_text, SCHEMA);
            assert!(copy_version["etag"].as_str().is_some());
            let confirmed =
                reader.read_schema_url(&request(url, machine_credentials, Some(copy_version)));
            assert_eq!(confirmed.read_outcome, SchemaUrlOutcome::CopyConfirmed);
        }
    }

    #[test]
    fn a_private_object_read_without_credentials_is_refused() {
        let reader = reader();
        seed(
            &reader,
            "mock-private-bucket",
            &[private_object("mock.schema.json")],
        );
        let refused = reader.read_schema_url(&request(
            "s3://mock-private-bucket/mock.schema.json",
            false,
            None,
        ));
        assert_eq!(refused.read_outcome, SchemaUrlOutcome::ReadRefused);
        assert_eq!(refused.read_refusals[0].credential_origin, None);
        assert_eq!(refused.read_refusals[0].refused_by, "AWS");
        assert!(!refused.read_refusals[0].refusal_reason.is_empty());
    }

    #[test]
    fn an_object_is_read_as_the_role_its_url_names() {
        let reader = reader();
        seed(
            &reader,
            "mock-role-bucket",
            &[private_object("mock.schema.json")],
        );
        let url = "s3://mock-role-bucket/mock.schema.json?assume_role=arn:aws:iam::000000000000:role/mock-reader";
        let sent = reader.read_schema_url(&request(url, true, None));
        assert!(
            matches!(sent.read_outcome, SchemaUrlOutcome::TextSent { .. }),
            "{sent:?}"
        );
    }

    #[test]
    fn a_key_or_a_bucket_that_is_not_there_is_missing() {
        let reader = reader();
        seed(&reader, "mock-missing-bucket", &[]);
        let missing_key = reader.read_schema_url(&request(
            "s3://mock-missing-bucket/absent.json",
            false,
            None,
        ));
        assert_eq!(
            missing_key.read_outcome,
            SchemaUrlOutcome::SourceMissing {
                missing_reason: "no key `absent.json` in bucket `mock-missing-bucket`".into()
            }
        );
        let missing_bucket =
            reader.read_schema_url(&request("s3://mock-absent-bucket/absent.json", true, None));
        assert_eq!(
            missing_bucket.read_outcome,
            SchemaUrlOutcome::SourceMissing {
                missing_reason: "no bucket `mock-absent-bucket`".into()
            }
        );
    }

    #[test]
    fn an_s3_url_takes_its_bucket_its_key_and_only_the_assume_role_parameter() {
        assert_eq!(
            S3Object::parse("s3://mock-bucket/a/b.json?assume_role=arn:mock"),
            Ok(S3Object {
                bucket: "mock-bucket".into(),
                key: "a/b.json".into(),
                assume_role: Some("arn:mock".into()),
            })
        );
        assert_eq!(
            S3Object::parse("s3://mock-bucket/a.json?region=x"),
            Err(
                "`region` is not a parameter of an `s3` URL; it takes `assume_role=<role ARN>`"
                    .into()
            )
        );
        assert_eq!(
            S3Object::parse("s3://mock-bucket"),
            Err("an S3 URL is `s3://<bucket>/<key>`".into())
        );
        let failed =
            reader().read_schema_url(&request("s3://mock-bucket/a.json?mock=1", false, None));
        assert!(matches!(
            failed.read_outcome,
            SchemaUrlOutcome::ReadFailed { .. }
        ));
    }

    #[test]
    fn a_configuration_naming_an_endpoint_is_told_from_one_naming_none() {
        let unnamed = SdkConfig::builder().build();
        assert!(!names_endpoint(&unnamed));
        let named = SdkConfig::builder()
            .endpoint_url("http://localhost:4566")
            .build();
        assert!(names_endpoint(&named));
    }

    #[test]
    fn a_refusal_names_its_code_the_identity_refused_and_what_renews_it() {
        assert_eq!(
            refusal_reason(
                "AccessDenied",
                "User: arn:aws:iam::000000000000:user/mock is not authorized to perform: s3:GetObject"
            ),
            "AccessDenied for arn:aws:iam::000000000000:user/mock"
        );
        assert_eq!(refusal_reason("ExpiredToken", "mock"), "ExpiredToken");
        let role = ReadIdentity::AssumedRole("arn:mock-role".into());
        let denied =
            credentials_answer("AccessDenied: User: arn:mock-user is not authorized", &role)
                .unwrap();
        assert_eq!(
            denied.read_refusals[0].renewal_fix.as_deref(),
            Some("use a profile allowed to assume arn:mock-role, such as `AWS_PROFILE=<profile>`")
        );
        assert_eq!(
            denied.read_refusals[0].refusal_reason,
            "AccessDenied for arn:mock-user"
        );
        let absent = credentials_answer(
            "failed to load credentials: no providers in chain provided credentials",
            &ReadIdentity::Machine,
        )
        .unwrap();
        assert_eq!(
            absent.read_refusals[0].renewal_fix.as_deref(),
            Some(CREDENTIALS_FIX)
        );
        assert!(credentials_answer("dispatch failure: io error", &ReadIdentity::Machine).is_none());
    }
}
