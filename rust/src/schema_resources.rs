//! The resources a schema embeds by `$id`, and the nodes its references name,
//! each as a JSON pointer into the whole schema.

use std::collections::BTreeMap;

use serde_json::Value as JsonValue;

use crate::problem_wording::unescape_pointer;
use crate::validation_rules::escape_pointer;

/// The keywords whose values are data, never schemas.
const DATA_KEYWORDS: [&str; 4] = ["const", "default", "enum", "examples"];
/// The keywords whose values map names to schemas.
pub const NAMED_SCHEMA_KEYWORDS: [&str; 6] = [
    "$defs",
    "definitions",
    "dependencies",
    "dependentSchemas",
    "patternProperties",
    "properties",
];
const REFERENCE_KEYWORDS: [&str; 3] = ["$ref", "$dynamicRef", "$recursiveRef"];
const ANCHOR_KEYWORDS: [&str; 2] = ["$anchor", "$dynamicAnchor"];

/// A schema a keyword's value holds: its pointer below the node holding the
/// keyword, and the name it is held by in a map of schemas.
pub struct ChildSchema<'s> {
    pub pointer_suffix: String,
    pub name: Option<&'s str>,
    pub schema: &'s JsonValue,
}

/// The schemas the value of `keyword` holds: none for data.
pub fn child_schemas<'s>(keyword: &str, value: &'s JsonValue) -> Vec<ChildSchema<'s>> {
    let keyword_step = escape_pointer(keyword);
    match value {
        _ if DATA_KEYWORDS.contains(&keyword) => Vec::new(),
        JsonValue::Object(schemas) if NAMED_SCHEMA_KEYWORDS.contains(&keyword) => schemas
            .iter()
            .map(|(name, schema)| ChildSchema {
                pointer_suffix: format!("/{keyword_step}/{}", escape_pointer(name)),
                name: Some(name),
                schema,
            })
            .collect(),
        JsonValue::Array(items) => items
            .iter()
            .enumerate()
            .map(|(index, schema)| ChildSchema {
                pointer_suffix: format!("/{keyword_step}/{index}"),
                name: None,
                schema,
            })
            .collect(),
        JsonValue::Object(_) => vec![ChildSchema {
            pointer_suffix: format!("/{keyword_step}"),
            name: None,
            schema: value,
        }],
        _ => Vec::new(),
    }
}

/// A schema resource: the root, or a node with an `$id`.
struct ResourceRoot {
    pointer: String,
    uri: String,
}

/// The resources a schema embeds, each with its `$id` read against the resource
/// holding it, and the anchors each declares.
pub struct SchemaResources {
    resource_roots: Vec<ResourceRoot>,
    /// Each anchor's pointer, by its resource's URI and its name.
    anchors: BTreeMap<(String, String), String>,
}

impl SchemaResources {
    pub fn of(schema: &JsonValue) -> Self {
        let mut resources = Self {
            resource_roots: vec![ResourceRoot {
                pointer: String::new(),
                uri: String::new(),
            }],
            anchors: BTreeMap::new(),
        };
        resources.collect_resources(schema, String::new());
        resources
    }

    fn collect_resources(&mut self, node: &JsonValue, pointer: String) {
        let Some(keywords) = node.as_object() else {
            return;
        };
        let holder_uri = self.resource_of(&pointer).uri.clone();
        match keywords.get("$id").and_then(JsonValue::as_str) {
            Some(anchor) if anchor.starts_with('#') => {
                self.anchors.insert(
                    (holder_uri.clone(), anchor[1..].to_owned()),
                    pointer.clone(),
                );
            }
            Some(identifier) => {
                let uri = resolve_uri(&holder_uri, uri_without_fragment(identifier));
                match self
                    .resource_roots
                    .iter_mut()
                    .find(|root| root.pointer == pointer)
                {
                    Some(root) => root.uri = uri,
                    None => self.resource_roots.push(ResourceRoot {
                        pointer: pointer.clone(),
                        uri,
                    }),
                }
            }
            None => {}
        }
        let resource_uri = self.resource_of(&pointer).uri.clone();
        for anchor_keyword in ANCHOR_KEYWORDS {
            if let Some(anchor) = keywords.get(anchor_keyword).and_then(JsonValue::as_str) {
                self.anchors
                    .insert((resource_uri.clone(), anchor.to_owned()), pointer.clone());
            }
        }
        for (keyword, value) in keywords {
            for child in child_schemas(keyword, value) {
                self.collect_resources(child.schema, format!("{pointer}{}", child.pointer_suffix));
            }
        }
    }

    /// The resource holding the node at `pointer`: the innermost one around it.
    fn resource_of(&self, pointer: &str) -> &ResourceRoot {
        self.resource_roots
            .iter()
            .filter(|root| {
                pointer == root.pointer || pointer.starts_with(&format!("{}/", root.pointer))
            })
            .max_by_key(|root| root.pointer.len())
            .unwrap_or(&self.resource_roots[0])
    }

    /// The pointer of the node `reference`, written in the node at
    /// `holder_pointer`, names, read against the resource holding that node; none
    /// for a resource or an anchor the schema does not hold. The node itself may
    /// be absent.
    pub fn reference_target(&self, holder_pointer: &str, reference: &str) -> Option<String> {
        let (uri, fragment) = reference.split_once('#').unwrap_or((reference, ""));
        let holder = self.resource_of(holder_pointer);
        let target = if uri.is_empty() {
            holder
        } else {
            let target_uri = resolve_uri(&holder.uri, uri);
            self.resource_roots
                .iter()
                .find(|root| root.uri == target_uri)?
        };
        if fragment.is_empty() || fragment.starts_with('/') {
            Some(format!(
                "{}{}",
                target.pointer,
                pointer_of_fragment(fragment)?
            ))
        } else {
            self.anchors
                .get(&(target.uri.clone(), fragment.to_owned()))
                .cloned()
        }
    }

    /// The pointer of the keyword `evaluation_path` reaches from the node at
    /// `start_pointer`, each reference on the way followed.
    pub fn evaluation_pointer(
        &self,
        schema: &JsonValue,
        start_pointer: &str,
        evaluation_path: &str,
    ) -> Option<String> {
        let mut pointer = start_pointer.to_owned();
        let mut is_name = false;
        for step in evaluation_path.split('/').skip(1) {
            let keyword = unescape_pointer(step);
            if !is_name && REFERENCE_KEYWORDS.contains(&keyword.as_str()) {
                let reference = schema.pointer(&pointer)?.get(&keyword)?.as_str()?;
                pointer = self.reference_target(&pointer, reference)?;
                continue;
            }
            is_name = !is_name && NAMED_SCHEMA_KEYWORDS.contains(&keyword.as_str());
            pointer = format!("{pointer}/{step}");
        }
        Some(pointer)
    }
}

fn uri_without_fragment(uri: &str) -> &str {
    uri.split_once('#')
        .map_or(uri, |(without_fragment, _)| without_fragment)
}

fn has_scheme(uri: &str) -> bool {
    let Some((scheme, _)) = uri.split_once(':') else {
        return false;
    };
    scheme.starts_with(|first: char| first.is_ascii_alphabetic())
        && scheme
            .chars()
            .all(|character| character.is_ascii_alphanumeric() || "+-.".contains(character))
}

/// `reference` read against the URI `base`: whole where it names a scheme, else
/// joined to the base's scheme, authority or directory.
fn resolve_uri(base: &str, reference: &str) -> String {
    if reference.is_empty() {
        return base.to_owned();
    }
    if has_scheme(reference) {
        return reference.to_owned();
    }
    let scheme_end = base.find(':').filter(|_| has_scheme(base));
    if reference.starts_with("//") {
        return match scheme_end {
            Some(end) => format!("{}{reference}", &base[..=end]),
            None => reference.to_owned(),
        };
    }
    let authority_end = base
        .find("://")
        .map(|start| start + 3)
        .map(|start| base[start..].find('/').map_or(base.len(), |at| start + at));
    if reference.starts_with('/') {
        return match authority_end {
            Some(end) => format!("{}{reference}", &base[..end]),
            None => reference.to_owned(),
        };
    }
    match (base.rfind('/'), authority_end) {
        (_, Some(end)) if end == base.len() => format!("{base}/{reference}"),
        (Some(last_slash), _) => format!("{}{reference}", &base[..=last_slash]),
        (None, _) => reference.to_owned(),
    }
}

/// The JSON pointer a URI fragment spells, its percent escapes read; none where
/// an escape is not one.
pub fn pointer_of_fragment(fragment: &str) -> Option<String> {
    let bytes = fragment.as_bytes();
    let mut pointer_bytes = Vec::with_capacity(bytes.len());
    let mut at = 0;
    while at < bytes.len() {
        if bytes[at] == b'%' {
            let escape = fragment.get(at + 1..at + 3)?;
            pointer_bytes.push(u8::from_str_radix(escape, 16).ok()?);
            at += 3;
        } else {
            pointer_bytes.push(bytes[at]);
            at += 1;
        }
    }
    String::from_utf8(pointer_bytes).ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn a_uri_reads_against_its_base() {
        let base = "https://mock.example/a/root.json";
        assert_eq!(
            resolve_uri(base, "sub/r.json"),
            "https://mock.example/a/sub/r.json"
        );
        assert_eq!(resolve_uri(base, "/z.json"), "https://mock.example/z.json");
        assert_eq!(
            resolve_uri(base, "//other.example/z.json"),
            "https://other.example/z.json"
        );
        assert_eq!(resolve_uri(base, "urn:mock:z"), "urn:mock:z");
        assert_eq!(resolve_uri(base, ""), base);
        assert_eq!(
            resolve_uri("https://mock.example", "z.json"),
            "https://mock.example/z.json"
        );
        assert_eq!(resolve_uri("", "z.json"), "z.json");
        assert_eq!(resolve_uri("", "/z.json"), "/z.json");
        assert_eq!(
            resolve_uri("", "//other.example/z.json"),
            "//other.example/z.json"
        );
        assert_eq!(resolve_uri("mock/root.json", "z.json"), "mock/z.json");
    }

    #[test]
    fn a_reference_names_a_node_of_the_resource_holding_it() {
        let schema = json!({
            "$id": "https://mock.example/root.json",
            "$defs": {
                "mock_resource": {
                    "$id": "sub/mock.json#",
                    "properties": {"$ref": {"$ref": "#/$defs/mock_size"}},
                    "$defs": {
                        "mock_size": {"$anchor": "mock-size"},
                        "mock_named": {"$id": "#mock-named"}
                    }
                }
            }
        });
        let resources = SchemaResources::of(&schema);
        let holder = "/$defs/mock_resource/properties/$ref";
        assert_eq!(
            resources
                .reference_target(holder, "#/$defs/mock_size")
                .as_deref(),
            Some("/$defs/mock_resource/$defs/mock_size")
        );
        assert_eq!(
            resources.reference_target(holder, "#mock-size").as_deref(),
            Some("/$defs/mock_resource/$defs/mock_size")
        );
        assert_eq!(
            resources
                .reference_target("", "sub/mock.json#mock-named")
                .as_deref(),
            Some("/$defs/mock_resource/$defs/mock_named")
        );
        assert_eq!(
            resources
                .reference_target("", "root.json#/$defs")
                .as_deref(),
            Some("/$defs")
        );
        assert_eq!(resources.reference_target("", "other.json#/x"), None);
        assert_eq!(resources.reference_target("", "#mock-missing"), None);
        assert_eq!(
            resources.evaluation_pointer(
                &schema,
                "/$defs/mock_resource",
                "/properties/$ref/$ref/type"
            ),
            Some("/$defs/mock_resource/$defs/mock_size/type".to_owned())
        );
        assert_eq!(
            resources.evaluation_pointer(&schema, "", "/missing/$ref/type"),
            None
        );
        assert_eq!(pointer_of_fragment("/mock%2"), None);
        assert_eq!(pointer_of_fragment("/mock%zz"), None);
        assert_eq!(pointer_of_fragment("/a%20b").as_deref(), Some("/a b"));
    }
}
