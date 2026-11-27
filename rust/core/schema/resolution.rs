//! A document's schema found and read through a schema cache, each URL by the
//! reader of its prefix.

pub(crate) mod file_schema_reader;
mod http_reading;
pub(crate) mod http_schema_reader;
mod machine_credentials;
mod schema_finding;
mod schema_locations;
mod schema_url_reading;

pub use http_reading::{HttpReply, http_conditional_headers, http_get};
pub use machine_credentials::{HostLogin, netrc_login};
pub use schema_finding::{
    CandidateNamer, FoundSchema, SchemaCandidate, SchemaFinding, SchemaSearchPath,
};
pub use schema_locations::{SchemaModeline, document_modeline};
pub use schema_url_reading::{
    ReadRefusal, SchemaUrlAnswer, SchemaUrlOutcome, SchemaUrlReader, SchemaUrlRequest,
};
