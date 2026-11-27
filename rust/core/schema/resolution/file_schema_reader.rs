//! The core's reader of `file` URLs: a file on the machine, read each time.

use std::fs;
use std::io::ErrorKind;

use serde_json::Value as JsonValue;

use super::schema_locations::{FILE_SCHEME, file_url_path};
use super::schema_url_reading::{
    SchemaUrlAnswer, SchemaUrlOutcome, SchemaUrlReader, SchemaUrlRequest,
};
use crate::problem_wording::code_span;

const NO_FILE_REASON: &str = "no file is there";

pub struct FileSchemaReader;

impl SchemaUrlReader for FileSchemaReader {
    fn url_prefixes(&self) -> &[&str] {
        &["file://"]
    }

    fn reads_remote_sources(&self) -> bool {
        false
    }

    fn read_schema_url(&self, request: &SchemaUrlRequest) -> SchemaUrlAnswer {
        let (address, query) = match request.fetch_url.split_once('?') {
            Some((address, query)) => (address, Some(query)),
            None => (request.fetch_url.as_str(), None),
        };
        if let Some(parameter) = query.and_then(|query| query.split('&').next()) {
            let name = parameter
                .split_once('=')
                .map_or(parameter, |(name, _)| name);
            return SchemaUrlAnswer::failed(format!(
                "{} is not a parameter of a {} URL",
                code_span(name),
                code_span(FILE_SCHEME)
            ));
        }
        let path = file_url_path(address);
        if !path.is_file() {
            return SchemaUrlAnswer::missing(NO_FILE_REASON);
        }
        match fs::read_to_string(&path) {
            Ok(schema_text) => SchemaUrlAnswer::of(SchemaUrlOutcome::TextSent {
                schema_text,
                copy_version: JsonValue::Null,
            }),
            Err(error) if error.kind() == ErrorKind::NotFound => {
                SchemaUrlAnswer::missing(NO_FILE_REASON)
            }
            Err(error) => SchemaUrlAnswer::failed(error.to_string()),
        }
    }
}
