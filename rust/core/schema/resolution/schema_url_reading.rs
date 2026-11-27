//! The reader of schema URLs, and what a schema cache asks it and hears back.

use std::time::Instant;

use serde_json::Value as JsonValue;

/// A reader of the URLs of its prefixes, read for a schema cache.
pub trait SchemaUrlReader: Send + Sync {
    /// The prefixes of the URLs it reads, such as `s3://`.
    fn url_prefixes(&self) -> &[&str];
    /// Whether its sources are remote, so a cache keeps a copy of what it reads.
    fn reads_remote_sources(&self) -> bool;
    fn read_schema_url(&self, request: &SchemaUrlRequest) -> SchemaUrlAnswer;
}

/// What a schema cache asks a reader for.
#[derive(Clone, Debug, PartialEq)]
pub struct SchemaUrlRequest {
    /// The URL without the cache's parameters, every other parameter as written.
    pub fetch_url: String,
    pub machine_credentials: bool,
    /// The version the reader gave the kept copy, if a copy is kept.
    pub kept_copy_version: Option<JsonValue>,
    pub answer_deadline: Instant,
}

/// What a read came to, and each refusal the source gave in it.
#[derive(Clone, Debug, PartialEq)]
pub struct SchemaUrlAnswer {
    pub read_outcome: SchemaUrlOutcome,
    pub read_refusals: Vec<ReadRefusal>,
}

#[derive(Clone, Debug, PartialEq)]
pub enum SchemaUrlOutcome {
    TextSent {
        schema_text: String,
        copy_version: JsonValue,
    },
    CopyConfirmed,
    SourceMissing {
        missing_reason: String,
    },
    ReadRefused,
    ReadFailed {
        failure_reason: String,
    },
}

/// A refusal a source gave.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct ReadRefusal {
    /// Who refused, such as `GitHub`.
    pub refused_by: String,
    /// Where the refused credential came from, such as `` `GH_TOKEN` ``; none for a
    /// read without credentials.
    pub credential_origin: Option<String>,
    /// What the source answered.
    pub refusal_reason: String,
    pub renewal_fix: Option<String>,
}

impl SchemaUrlAnswer {
    pub fn of(read_outcome: SchemaUrlOutcome) -> Self {
        Self {
            read_outcome,
            read_refusals: Vec::new(),
        }
    }

    pub fn refused(read_refusals: Vec<ReadRefusal>) -> Self {
        Self {
            read_outcome: SchemaUrlOutcome::ReadRefused,
            read_refusals,
        }
    }

    pub fn failed(failure_reason: impl Into<String>) -> Self {
        Self::of(SchemaUrlOutcome::ReadFailed {
            failure_reason: failure_reason.into(),
        })
    }

    pub fn missing(missing_reason: impl Into<String>) -> Self {
        Self::of(SchemaUrlOutcome::SourceMissing {
            missing_reason: missing_reason.into(),
        })
    }
}
