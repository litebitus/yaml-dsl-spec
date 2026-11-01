//! How a document is read before it is validated: the DSL definition's
//! `validator` block, ready to apply.

use fancy_regex::Regex;
use serde_json::{Map, Value as JsonValue};

use crate::definition::{QuotedValueType, Validator};
use crate::node::number_json;

/// A DSL's `validator` block with its patterns compiled.
#[derive(Clone, Debug, Default)]
pub struct ValidationRules {
    pub merge_base_keys: Vec<String>,
    pub child_value_patterns: Vec<Regex>,
    pub quoted_value_types: Vec<QuotedValueType>,
}

impl ValidationRules {
    /// The rules a DSL's `validator` block states; none for a DSL without one.
    pub fn from_validator(validator: Option<&Validator>) -> Result<Self, String> {
        let Some(validator) = validator else {
            return Ok(Self::default());
        };
        let child_value_patterns = validator
            .child_values_from_parent_key
            .iter()
            .map(|rule| {
                Regex::new(&rule.pattern).map_err(|error| format!("`{}`: {error}", rule.pattern))
            })
            .collect::<Result<Vec<_>, String>>()?;
        Ok(Self {
            merge_base_keys: validator.merge_base_keys.clone(),
            child_value_patterns,
            quoted_value_types: validator.allow_quoted_values.clone(),
        })
    }

    /// The field a key such as `name mock-orders` sets on the entry it keys, and the
    /// value it sets, by the first pattern with `field` and `value` groups that matches.
    pub fn child_value(&self, key: &str) -> Option<(String, String)> {
        child_value(key, &self.child_value_patterns)
    }

    /// The value a quoted `text` is validated as where the schema expects one of
    /// `expected_types`, in their order.
    pub fn quoted_value(&self, text: &str, expected_types: &[String]) -> Option<JsonValue> {
        expected_types.iter().find_map(|expected_type| {
            let quoted_type = match expected_type.as_str() {
                "boolean" => QuotedValueType::Boolean,
                "integer" => QuotedValueType::Integer,
                "number" => QuotedValueType::Number,
                _ => return None,
            };
            if !self.quoted_value_types.contains(&quoted_type) {
                return None;
            }
            read_quoted_value(text, quoted_type)
        })
    }
}

/// The field a key such as `name mock-orders` sets on the entry it keys, and the
/// value it sets, by the first of `patterns` with `field` and `value` groups that
/// matches.
pub fn child_value(key: &str, patterns: &[Regex]) -> Option<(String, String)> {
    patterns.iter().find_map(|pattern| {
        let captures = pattern.captures(key).ok()??;
        Some((
            captures.name("field")?.as_str().to_owned(),
            captures.name("value")?.as_str().to_owned(),
        ))
    })
}

fn read_quoted_value(text: &str, quoted_type: QuotedValueType) -> Option<JsonValue> {
    match quoted_type {
        QuotedValueType::Boolean => match text {
            "true" => Some(JsonValue::Bool(true)),
            "false" => Some(JsonValue::Bool(false)),
            _ => None,
        },
        QuotedValueType::Integer => spells_integer(text).then(|| number_json(text)),
        QuotedValueType::Number => spells_number(text).then(|| number_json(text)),
    }
}

fn spells_integer(text: &str) -> bool {
    let digits = text.strip_prefix('-').unwrap_or(text);
    digits == "0"
        || (digits.starts_with(|first: char| ('1'..='9').contains(&first))
            && digits.chars().all(|character| character.is_ascii_digit()))
}

fn spells_number(text: &str) -> bool {
    let (mantissa, exponent) = match text.find(['e', 'E']) {
        Some(at) => (&text[..at], Some(&text[at + 1..])),
        None => (text, None),
    };
    let (whole, fraction) = match mantissa.split_once('.') {
        Some((whole, fraction)) => (whole, Some(fraction)),
        None => (mantissa, None),
    };
    let all_digits =
        |part: &str| !part.is_empty() && part.chars().all(|digit| digit.is_ascii_digit());
    let exponent_spelled = exponent
        .is_none_or(|exponent| all_digits(exponent.strip_prefix(['+', '-']).unwrap_or(exponent)));
    spells_integer(whole) && fraction.is_none_or(all_digits) && exponent_spelled
}

/// `overlay` merged over `base` as an overlay merges over its common layer: maps
/// merge key by key, an empty map or any other value replaces, and a null deletes
/// the key unless `holds_unknown` says the null stands for a value not yet known.
pub fn merged_over(
    base: &JsonValue,
    overlay: &Map<String, JsonValue>,
    overlay_pointer: &str,
    holds_unknown: &dyn Fn(&str) -> bool,
) -> JsonValue {
    let mut merged = base.as_object().cloned().unwrap_or_default();
    merge_into(&mut merged, overlay, overlay_pointer, holds_unknown);
    JsonValue::Object(merged)
}

fn merge_into(
    target: &mut Map<String, JsonValue>,
    overlay: &Map<String, JsonValue>,
    overlay_pointer: &str,
    holds_unknown: &dyn Fn(&str) -> bool,
) {
    for (key, value) in overlay {
        let child_pointer = format!("{overlay_pointer}/{}", escape_pointer(key));
        if value.is_null() && !holds_unknown(&child_pointer) {
            target.shift_remove(key);
            continue;
        }
        match (target.get_mut(key), value) {
            (Some(JsonValue::Object(held)), JsonValue::Object(fields)) if !fields.is_empty() => {
                merge_into(held, fields, &child_pointer, holds_unknown);
            }
            _ => {
                target.insert(key.clone(), value.clone());
            }
        }
    }
}

/// The pointers of `merged` that `written` does not hold: the values a merge copied in.
pub fn copied_pointers(merged: &JsonValue, written: &JsonValue, pointer: &str) -> Vec<String> {
    let (Some(merged_fields), Some(written_fields)) = (merged.as_object(), written.as_object())
    else {
        return Vec::new();
    };
    let mut copied = Vec::new();
    for (key, value) in merged_fields {
        let child_pointer = format!("{pointer}/{}", escape_pointer(key));
        match written_fields.get(key) {
            None => copied.push(child_pointer),
            Some(written_value) => {
                copied.extend(copied_pointers(value, written_value, &child_pointer));
            }
        }
    }
    copied
}

pub fn escape_pointer(key: &str) -> String {
    key.replace('~', "~0").replace('/', "~1")
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn a_quoted_value_reads_as_the_first_expected_type_allowed_that_it_spells() {
        let rules = ValidationRules {
            quoted_value_types: vec![QuotedValueType::Boolean, QuotedValueType::Number],
            ..ValidationRules::default()
        };
        let expected = |types: &[&str]| {
            types
                .iter()
                .map(|text| (*text).to_owned())
                .collect::<Vec<_>>()
        };
        assert_eq!(
            rules.quoted_value("true", &expected(&["boolean"])),
            Some(json!(true))
        );
        assert_eq!(
            rules.quoted_value("1.5e3", &expected(&["number"])),
            Some(json!(1500.0))
        );
        assert_eq!(rules.quoted_value("7", &expected(&["integer"])), None);
        assert_eq!(rules.quoted_value("yes", &expected(&["boolean"])), None);
        assert_eq!(rules.quoted_value("01", &expected(&["number"])), None);
    }

    #[test]
    fn an_overlay_merges_over_its_base_and_what_it_copies_is_known() {
        let base = json!({"visibility": 30, "tags": {"team": "mock", "tier": "gold"}, "gone": 1});
        let overlay = json!({"tags": {"tier": "silver"}, "gone": null, "later": null});
        let merged = merged_over(
            &base,
            overlay.as_object().unwrap(),
            "/orders",
            &|pointer: &str| pointer == "/orders/later",
        );
        assert_eq!(
            merged,
            json!({"visibility": 30, "tags": {"team": "mock", "tier": "silver"}, "later": null})
        );
        assert_eq!(
            copied_pointers(&merged, &overlay, "/orders"),
            ["/orders/visibility", "/orders/tags/team"]
        );
    }
}
