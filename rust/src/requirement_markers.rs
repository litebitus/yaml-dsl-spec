//! Requirement markers: a field's `description` opening with `[required]`,
//! `[~required]` or `[optional]` decides whether the field is required, over its
//! parent's `required` array.

use jsonschema::Draft;
use serde_json::{Map, Value as JsonValue};

const REQUIRED_MARKER: &str = "[required]";
const REQUIREMENT_MARKERS: [&str; 3] = [REQUIRED_MARKER, "[~required]", "[optional]"];
const CONDITION_KEYWORDS: [&str; 2] = ["if", "not"];
const FORM_SELECTING_KEYWORDS: [&str; 2] = ["anyOf", "oneOf"];
const SAME_INSTANCE_SCHEMA_KEYWORDS: [&str; 4] = ["else", "if", "not", "then"];
const SAME_INSTANCE_LIST_KEYWORDS: [&str; 3] = ["allOf", "anyOf", "oneOf"];

/// The keywords whose values are schemas, by how they hold them, in one draft.
struct DraftKeywords {
    child_schema: &'static [&'static str],
    schema_map: &'static [&'static str],
}

impl DraftKeywords {
    fn of(draft: Draft) -> Self {
        match draft {
            Draft::Draft201909 => Self {
                child_schema: &[
                    "additionalItems",
                    "additionalProperties",
                    "contains",
                    "contentSchema",
                    "items",
                    "propertyNames",
                    "unevaluatedItems",
                    "unevaluatedProperties",
                ],
                schema_map: &[
                    "$defs",
                    "definitions",
                    "dependentSchemas",
                    "patternProperties",
                    "properties",
                ],
            },
            Draft::Draft202012 => Self {
                child_schema: &[
                    "additionalProperties",
                    "contains",
                    "contentSchema",
                    "items",
                    "prefixItems",
                    "propertyNames",
                    "unevaluatedItems",
                    "unevaluatedProperties",
                ],
                schema_map: &[
                    "$defs",
                    "definitions",
                    "dependentSchemas",
                    "patternProperties",
                    "properties",
                ],
            },
            _ => Self {
                child_schema: &[
                    "additionalItems",
                    "additionalProperties",
                    "contains",
                    "items",
                    "propertyNames",
                ],
                schema_map: &[
                    "$defs",
                    "definitions",
                    "dependencies",
                    "patternProperties",
                    "properties",
                ],
            },
        }
    }
}

/// The marker a schema node's `description` opens with.
pub fn requirement_marker(node: &JsonValue) -> Option<&'static str> {
    let description = node.get("description")?.as_str()?.trim_start();
    REQUIREMENT_MARKERS
        .into_iter()
        .find(|marker| description.starts_with(marker))
}

/// A node's description without the marker it opens with.
pub fn description_text(node: &JsonValue) -> Option<String> {
    let description = node.get("description")?.as_str()?;
    let text = match requirement_marker(node) {
        Some(marker) => &description.trim_start()[marker.len()..],
        None => description,
    };
    let text = text.trim();
    (!text.is_empty()).then(|| text.to_owned())
}

/// `schema` with each `required` array decided by the markers of the fields it lists.
pub fn marked_schema(schema: &JsonValue, draft: Draft) -> JsonValue {
    marked_node(schema, None, &DraftKeywords::of(draft))
}

type FieldMarkers = Vec<(String, &'static str)>;

fn markers_of_fields(properties: &Map<String, JsonValue>) -> FieldMarkers {
    properties
        .iter()
        .filter_map(|(key, field)| requirement_marker(field).map(|marker| (key.clone(), marker)))
        .collect()
}

fn marked_node(
    node: &JsonValue,
    inherited_markers: Option<&FieldMarkers>,
    keywords: &DraftKeywords,
) -> JsonValue {
    let JsonValue::Object(fields) = node else {
        return node.clone();
    };
    let own_markers = fields
        .get("properties")
        .and_then(JsonValue::as_object)
        .map(markers_of_fields);
    let markers = own_markers.as_ref().or(inherited_markers);
    let mut marked = Map::new();
    for (keyword, value) in fields {
        let keyword_name = keyword.as_str();
        let marked_value = if keyword_name == "required" {
            continue;
        } else if CONDITION_KEYWORDS.contains(&keyword_name) {
            value.clone()
        } else if SAME_INSTANCE_SCHEMA_KEYWORDS.contains(&keyword_name) {
            marked_node(value, markers, keywords)
        } else if SAME_INSTANCE_LIST_KEYWORDS.contains(&keyword_name)
            && let JsonValue::Array(branches) = value
        {
            let branch_markers = if FORM_SELECTING_KEYWORDS.contains(&keyword_name) {
                None
            } else {
                markers
            };
            let marked_branches = branches
                .iter()
                .map(|branch| marked_node(branch, branch_markers, keywords))
                .collect();
            JsonValue::Array(marked_branches)
        } else if keywords.child_schema.contains(&keyword_name) {
            match value {
                JsonValue::Array(children) => JsonValue::Array(
                    children
                        .iter()
                        .map(|child| marked_node(child, None, keywords))
                        .collect(),
                ),
                child => marked_node(child, None, keywords),
            }
        } else if keywords.schema_map.contains(&keyword_name)
            && let JsonValue::Object(schemas) = value
        {
            let marked_schemas = schemas
                .iter()
                .map(|(name, schema)| (name.clone(), marked_node(schema, None, keywords)))
                .collect();
            JsonValue::Object(marked_schemas)
        } else {
            value.clone()
        };
        marked.insert(keyword.clone(), marked_value);
    }
    let is_marked =
        |key: &str| markers.is_some_and(|markers| markers.iter().any(|(name, _)| name == key));
    let mut required: Vec<JsonValue> = fields
        .get("required")
        .and_then(JsonValue::as_array)
        .into_iter()
        .flatten()
        .filter(|key| key.as_str().is_none_or(|key| !is_marked(key)))
        .cloned()
        .collect();
    for (key, marker) in own_markers.iter().flatten() {
        if *marker == REQUIRED_MARKER {
            required.push(JsonValue::String(key.clone()));
        }
    }
    if !required.is_empty() {
        marked.insert("required".to_owned(), JsonValue::Array(required));
    }
    JsonValue::Object(marked)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn a_marker_decides_over_the_required_array_and_a_condition_is_left_as_written() {
        let schema = json!({
            "properties": {
                "name": {"description": "[required] The name."},
                "listed": {},
                "legacy": {"description": "[optional] Kept."},
                "maybe": {"description": "[~required] Sometimes."}
            },
            "required": ["legacy", "listed", "maybe"],
            "if": {"required": ["legacy"]},
            "anyOf": [{"required": ["legacy"]}],
            "allOf": [{"required": ["legacy", "listed"]}]
        });
        let marked = marked_schema(&schema, Draft::Draft202012);
        assert_eq!(marked["required"], json!(["listed", "name"]));
        assert_eq!(marked["if"], json!({"required": ["legacy"]}));
        assert_eq!(marked["anyOf"], json!([{"required": ["legacy"]}]));
        assert_eq!(marked["allOf"], json!([{"required": ["listed"]}]));
    }
}
