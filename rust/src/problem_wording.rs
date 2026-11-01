//! How a schema problem is worded: the field it names and the sentence about it.

use serde_json::Value as JsonValue;

use crate::requirement_markers::description_text;

const DEPENDENCY_KEYWORDS: [&str; 3] = ["dependencies", "dependentRequired", "dependentSchemas"];
const SCHEMA_MAP_KEYWORDS: [&str; 6] = [
    "$defs",
    "definitions",
    "dependencies",
    "dependentSchemas",
    "patternProperties",
    "properties",
];
const DEFINITION_KEYWORDS: [&str; 2] = ["$defs", "definitions"];
const UNSTATED_CONDITION: &str = "a condition the schema states";

/// The field a problem names: the key of the value at `pointer` in `document`,
/// then `[<index>]` for each list index below it; the root is `the document`.
pub fn field_name(document: &JsonValue, pointer: &str) -> String {
    if pointer.is_empty() {
        return "the document".to_owned();
    }
    let mut steps: Vec<(String, bool)> = Vec::new();
    let mut node = Some(document);
    for step in pointer[1..].split('/') {
        let key = unescape_pointer(step);
        let in_list = matches!(node, Some(JsonValue::Array(_)));
        node = match node {
            Some(JsonValue::Array(items)) => key.parse::<usize>().ok().and_then(|at| items.get(at)),
            Some(JsonValue::Object(fields)) => fields.get(&key),
            _ => None,
        };
        steps.push((key, in_list));
    }
    let key_at = steps.iter().rposition(|(_, is_index)| !is_index);
    let key = key_at.map_or("", |at| steps[at].0.as_str());
    let indexes: String = steps[key_at.map_or(0, |at| at + 1)..]
        .iter()
        .map(|(index, _)| format!("[{index}]"))
        .collect();
    format!("`{key}{indexes}`")
}

pub fn unescape_pointer(step: &str) -> String {
    step.replace("~1", "/").replace("~0", "~")
}

/// A JSON number as JavaScript writes one: an integral value without a fraction.
pub fn number_text(number: &JsonValue) -> String {
    match number.as_f64() {
        Some(value) if number.is_f64() && value.fract() == 0.0 && value.abs() < 1e21 => {
            format!("{value:.0}")
        }
        _ => number.to_string(),
    }
}

/// The sentence of an alternative none of whose forms holds: the first sentence of
/// the definition holding the alternatives, where they are a described
/// definition's, `#/$defs/<name>` or `#/definitions/<name>`.
pub fn described_form(schema: &JsonValue, holder_pointer: &str) -> Option<String> {
    let mut steps = holder_pointer.strip_prefix('/')?.split('/');
    let (Some(container), Some(_), None) = (steps.next(), steps.next(), steps.next()) else {
        return None;
    };
    if !DEFINITION_KEYWORDS.contains(&container) {
        return None;
    }
    let description = description_text(schema.pointer(holder_pointer)?)?;
    let sentence_end = description
        .char_indices()
        .zip(description.chars().skip(1))
        .find(|((_, character), next)| *character == '.' && next.is_whitespace())
        .map_or(description.len(), |((at, _), _)| at);
    let sentence = &description[..sentence_end];
    let first_sentence = sentence.strip_suffix('.').unwrap_or(sentence);
    let leading_word: String = first_sentence
        .chars()
        .take_while(char::is_ascii_alphanumeric)
        .collect();
    let acronym = leading_word.chars().count() >= 2
        && leading_word
            .chars()
            .all(|character| character.is_ascii_uppercase());
    if acronym {
        return Some(first_sentence.to_owned());
    }
    let mut characters = first_sentence.chars();
    let first = characters.next()?;
    Some(first.to_lowercase().chain(characters).collect())
}

/// The key a dependency names, where `keyword_pointer` lies inside one: the
/// dependency keyword's pointer and the key, read from the pointer or, for a
/// dependency listing fields, from the fields the document's `holder` misses.
pub fn dependency_of(
    schema: &JsonValue,
    keyword_pointer: &str,
    holder: &JsonValue,
    missing_field: Option<&str>,
) -> Option<(String, String)> {
    let steps: Vec<String> = keyword_pointer
        .split('/')
        .skip(1)
        .map(unescape_pointer)
        .collect();
    let at = (0..steps.len()).rev().find(|&at| {
        DEPENDENCY_KEYWORDS.contains(&steps[at].as_str())
            && (at == 0 || !SCHEMA_MAP_KEYWORDS.contains(&steps[at - 1].as_str()))
    })?;
    let keyword_at: String = steps[..=at]
        .iter()
        .map(|step| format!("/{}", escape(step)))
        .collect();
    let key = match steps.get(at + 1) {
        Some(key) => key.clone(),
        None => {
            let missing_field = missing_field?;
            let listed = schema.pointer(&keyword_at)?.as_object()?;
            listed
                .iter()
                .find(|(key, needed)| {
                    holder.get(key.as_str()).is_some()
                        && needed
                            .as_array()
                            .is_some_and(|fields| fields.iter().any(|field| field == missing_field))
                })?
                .0
                .clone()
        }
    };
    holder.get(&key)?;
    Some((keyword_at, key))
}

/// `` `key` needs <what> ``: what each dependency of the node holding
/// `keyword_pointer` states for `key`.
pub fn dependency_message(schema: &JsonValue, keyword_pointer: &str, key: &str) -> String {
    let holder_pointer = keyword_pointer
        .rsplit_once('/')
        .map_or("", |(holder, _)| holder);
    let needed_texts: Vec<String> = schema
        .pointer(holder_pointer)
        .into_iter()
        .flat_map(|holder| {
            DEPENDENCY_KEYWORDS
                .iter()
                .filter_map(|keyword| holder.get(*keyword)?.get(key))
        })
        .map(needed_text)
        .collect();
    let needed = if needed_texts.is_empty() {
        UNSTATED_CONDITION.to_owned()
    } else {
        needed_texts.join("; ")
    };
    format!("`{key}` needs {needed}")
}

fn needed_text(needed: &JsonValue) -> String {
    match needed {
        JsonValue::Array(fields) => key_list(fields),
        node => consequence_text(node).unwrap_or_else(|| UNSTATED_CONDITION.to_owned()),
    }
}

fn consequence_text(node: &JsonValue) -> Option<String> {
    let fields = node.as_object()?;
    let mut parts = Vec::new();
    if let Some(required) = fields.get("required").and_then(JsonValue::as_array)
        && !required.is_empty()
    {
        parts.push(key_list(required));
    }
    if let Some(forbidden) = node.pointer("/not/required").and_then(JsonValue::as_array)
        && !forbidden.is_empty()
    {
        parts.push(format!("{} must not be set", key_list(forbidden)));
    }
    for (field, field_schema) in fields
        .get("properties")
        .and_then(JsonValue::as_object)
        .into_iter()
        .flatten()
    {
        if let Some(reference) = field_schema.get("$ref").and_then(JsonValue::as_str) {
            parts.push(format!("`{field}` is a `{}`", reference_name(reference)));
        }
    }
    for part in fields
        .get("allOf")
        .and_then(JsonValue::as_array)
        .into_iter()
        .flatten()
    {
        parts.extend(consequence_text(part));
    }
    let alternatives: Vec<String> = ["anyOf", "oneOf"]
        .iter()
        .filter_map(|keyword| fields.get(*keyword)?.as_array())
        .flatten()
        .map(|alternative| {
            consequence_text(alternative).unwrap_or_else(|| UNSTATED_CONDITION.to_owned())
        })
        .collect();
    if !alternatives.is_empty() {
        parts.push(alternatives.join(", or "));
    }
    if parts.is_empty()
        && let Some(reference) = fields.get("$ref").and_then(JsonValue::as_str)
    {
        parts.push(format!("a `{}`", reference_name(reference)));
    }
    (!parts.is_empty()).then(|| parts.join("; "))
}

fn reference_name(reference: &str) -> &str {
    let name = reference.rsplit('/').next().unwrap_or(reference);
    name.strip_prefix('#').unwrap_or(name)
}

fn key_list(keys: &[JsonValue]) -> String {
    let spans: Vec<String> = keys
        .iter()
        .map(|key| {
            format!(
                "`{}`",
                key.as_str().map_or_else(|| key.to_string(), str::to_owned)
            )
        })
        .collect();
    match spans.split_last() {
        Some((last, rest)) if !rest.is_empty() => format!("{} and {last}", rest.join(", ")),
        _ => spans.concat(),
    }
}

/// The indexes of the duplicate items a list reports, later first: from the last
/// item back, the first one equal to an item after it, and the nearest such item.
pub fn duplicate_items(items: &[JsonValue]) -> Option<(usize, usize)> {
    (0..items.len()).rev().find_map(|earlier| {
        (earlier + 1..items.len())
            .find(|&later| json_equal(&items[earlier], &items[later]))
            .map(|later| (later, earlier))
    })
}

fn json_equal(first: &JsonValue, second: &JsonValue) -> bool {
    match (first, second) {
        (JsonValue::Number(first), JsonValue::Number(second)) => first.as_f64() == second.as_f64(),
        (JsonValue::Array(first), JsonValue::Array(second)) => {
            first.len() == second.len()
                && first
                    .iter()
                    .zip(second)
                    .all(|(left, right)| json_equal(left, right))
        }
        (JsonValue::Object(first), JsonValue::Object(second)) => {
            first.len() == second.len()
                && first
                    .iter()
                    .all(|(key, left)| second.get(key).is_some_and(|right| json_equal(left, right)))
        }
        _ => first == second,
    }
}

/// `must contain at least <min> valid item(s)`, with `and no more than <max>`
/// where the node holding `contains` bounds it.
pub fn contains_message(holder: Option<&JsonValue>) -> String {
    let bound = |keyword: &str| holder.and_then(|node| node.get(keyword)).map(number_text);
    let minimum = bound("minContains").unwrap_or_else(|| "1".to_owned());
    match bound("maxContains") {
        Some(maximum) => {
            format!("must contain at least {minimum} and no more than {maximum} valid item(s)")
        }
        None => format!("must contain at least {minimum} valid item(s)"),
    }
}

fn escape(step: &str) -> String {
    step.replace('~', "~0").replace('/', "~1")
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn a_field_is_named_by_its_key_and_the_list_indexes_below_it() {
        let document = json!({"tags": [["a"]], "0": {"name": 1}});
        assert_eq!(field_name(&document, ""), "the document");
        assert_eq!(field_name(&document, "/tags/0/0"), "`tags[0][0]`");
        assert_eq!(field_name(&document, "/0/name"), "`name`");
        assert_eq!(field_name(&json!([1]), "/0"), "`[0]`");
    }

    #[test]
    fn a_dependency_names_what_its_key_needs() {
        let schema = json!({
            "dependentRequired": {"queue": ["role", "region"]},
            "dependentSchemas": {"topic": {"not": {"required": ["queue"]}}}
        });
        let holder = json!({"queue": 1, "topic": 2});
        let queue = dependency_of(&schema, "/dependentRequired", &holder, Some("role")).unwrap();
        assert_eq!(queue, ("/dependentRequired".to_owned(), "queue".to_owned()));
        assert_eq!(
            dependency_message(&schema, &queue.0, &queue.1),
            "`queue` needs `role` and `region`"
        );
        let topic = dependency_of(&schema, "/dependentSchemas/topic/not", &holder, None).unwrap();
        assert_eq!(
            dependency_message(&schema, &topic.0, &topic.1),
            "`topic` needs `queue` must not be set"
        );
    }

    #[test]
    fn duplicate_items_are_found_from_the_last_item_back() {
        assert_eq!(
            duplicate_items(&[json!("a"), json!("b"), json!("a")]),
            Some((2, 0))
        );
        assert_eq!(
            duplicate_items(&[json!(1), json!(1), json!(1.0)]),
            Some((2, 1))
        );
        assert_eq!(duplicate_items(&[json!(1), json!(2)]), None);
    }

    #[test]
    fn alternatives_of_a_described_definition_read_as_its_first_sentence() {
        let schema = json!({"$defs": {
            "size": {"description": "[optional] A literal number, or a ref. More."},
            "arn": {"description": "ARN of a role."}
        }});
        assert_eq!(
            described_form(&schema, "/$defs/size").as_deref(),
            Some("a literal number, or a ref")
        );
        assert_eq!(
            described_form(&schema, "/$defs/arn").as_deref(),
            Some("ARN of a role")
        );
        assert_eq!(described_form(&schema, "/properties/size"), None);
    }
}
