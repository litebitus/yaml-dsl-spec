//! The standard's format function: a layout and the operands that fill its verbs.

use serde_json::Value as JsonValue;

use crate::problem_wording::code_span;

const FORMAT_PART_LIMIT: usize = 1000;
const EARLIEST_FORMAT_SECONDS: i64 = -62_135_596_800;
const LATEST_FORMAT_SECONDS: i64 = 253_402_300_800;
const SECONDS_PER_DAY: i64 = 86_400;
const TEXT_KINDS: &str = "text";
const NUMBER_KINDS: &str = "a number";
const WHOLE_KINDS: &str = "a whole number";
const WHOLE_NUMBER_LIMIT: f64 = 9_223_372_036_854_775_808.0;

#[derive(Clone, Copy, PartialEq)]
enum OperandKind {
    Text,
    Whole,
    Number,
    Boolean,
    Other,
}

#[derive(Clone, Copy, Default)]
struct VerbRule {
    accepted_kinds: &'static str,
    takes_operand: bool,
    accepts_text: bool,
    accepts_whole: bool,
    accepts_number: bool,
    accepts_boolean: bool,
    reads_width: bool,
    reads_precision: bool,
    reads_zero_padding: bool,
    reads_plus_sign: bool,
}

/// One part of a layout: its literal text, or a verb.
#[derive(Clone, Debug, PartialEq)]
pub enum LayoutPart {
    Text(String),
    Verb(LayoutVerb),
}

/// A verb as a layout writes it: its letter, its flags, and its width and
/// precision, none where left out.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct LayoutVerb {
    pub letter: char,
    pub left_aligned: bool,
    pub plus_signed: bool,
    pub zero_padded: bool,
    pub width: Option<usize>,
    pub precision: Option<usize>,
}

#[derive(Default)]
struct Directive {
    verb: LayoutVerb,
    written_text: String,
    over_part_limit: bool,
}

struct Operand<'operand> {
    kind: OperandKind,
    kind_name: &'static str,
    text_value: &'operand str,
    whole_value: i64,
    number_value: f64,
    boolean_value: bool,
}

/// Writes `layout` with each verb filled from the next of `operands`, or the
/// problem that stops it.
pub fn format_layout(layout: &str, operands: &[JsonValue]) -> Result<String, String> {
    let mut written_text = String::new();
    let mut next_operand = 0;
    for layout_part in parse_layout(layout)? {
        let verb = match layout_part {
            LayoutPart::Text(text) => {
                written_text.push_str(&text);
                continue;
            }
            LayoutPart::Verb(verb) => verb,
        };
        let Some(operand_value) = operands.get(next_operand) else {
            return Err(format!(
                "layout {} has more verbs than operands",
                code_span(layout)
            ));
        };
        next_operand += 1;
        let verb_rule = verb_rule_of(verb.letter).unwrap_or_default();
        let operand = operand_of(operand_value);
        let verb_text = formatted_verb_text(&verb, &verb_rule, &operand, next_operand)?;
        written_text.push_str(&padded_verb_text(&verb, &verb_rule, verb_text));
    }
    if next_operand < operands.len() {
        return Err(format!(
            "layout {} has more operands than verbs",
            code_span(layout)
        ));
    }
    Ok(written_text)
}

/// Reads `layout` into its parts, literal text and verbs, `%%` read as text;
/// else the first problem in the layout.
pub fn parse_layout(layout: &str) -> Result<Vec<LayoutPart>, String> {
    let layout_characters: Vec<char> = layout.chars().collect();
    let mut layout_parts = Vec::new();
    let mut literal_text = String::new();
    let mut position = 0;
    while position < layout_characters.len() {
        if layout_characters[position] != '%' {
            literal_text.push(layout_characters[position]);
            position += 1;
            continue;
        }
        let Some((directive, end_position)) = read_directive(&layout_characters, position) else {
            return Err(format!("layout {} ends inside a verb", code_span(layout)));
        };
        position = end_position + 1;
        let Some(verb_rule) = verb_rule_of(directive.verb.letter) else {
            return Err(format!(
                "{} is not a verb `format` writes",
                code_span(&directive.written_text)
            ));
        };
        directive_part_problem(&directive, &verb_rule)?;
        if !verb_rule.takes_operand {
            literal_text.push('%');
            continue;
        }
        if !literal_text.is_empty() {
            layout_parts.push(LayoutPart::Text(std::mem::take(&mut literal_text)));
        }
        layout_parts.push(LayoutPart::Verb(directive.verb));
    }
    if !literal_text.is_empty() {
        layout_parts.push(LayoutPart::Text(literal_text));
    }
    Ok(layout_parts)
}

fn verb_rule_of(verb: char) -> Option<VerbRule> {
    let text_verb = VerbRule {
        accepted_kinds: TEXT_KINDS,
        takes_operand: true,
        accepts_text: true,
        reads_width: true,
        reads_precision: true,
        ..VerbRule::default()
    };
    let hexadecimal_verb = VerbRule {
        accepted_kinds: "a whole number or text",
        takes_operand: true,
        accepts_text: true,
        accepts_whole: true,
        reads_width: true,
        reads_zero_padding: true,
        ..VerbRule::default()
    };
    Some(match verb {
        '%' => VerbRule::default(),
        's' | 'q' | 'u' | 'l' | 'A' | 'B' => text_verb,
        'v' => VerbRule {
            accepted_kinds: "text, a whole number or a boolean",
            takes_operand: true,
            accepts_text: true,
            accepts_whole: true,
            accepts_boolean: true,
            reads_width: true,
            ..VerbRule::default()
        },
        'd' => VerbRule {
            accepted_kinds: WHOLE_KINDS,
            takes_operand: true,
            accepts_whole: true,
            reads_width: true,
            reads_zero_padding: true,
            reads_plus_sign: true,
            ..VerbRule::default()
        },
        'x' | 'X' => hexadecimal_verb,
        'f' => VerbRule {
            accepted_kinds: NUMBER_KINDS,
            takes_operand: true,
            accepts_whole: true,
            accepts_number: true,
            reads_width: true,
            reads_precision: true,
            reads_zero_padding: true,
            reads_plus_sign: true,
            ..VerbRule::default()
        },
        'C' => VerbRule {
            accepted_kinds: NUMBER_KINDS,
            takes_operand: true,
            accepts_whole: true,
            accepts_number: true,
            reads_width: true,
            ..VerbRule::default()
        },
        _ => return None,
    })
}

fn read_directive(layout_characters: &[char], start_position: usize) -> Option<(Directive, usize)> {
    let mut directive = Directive::default();
    let mut position = start_position + 1;
    while let Some(&flag) = layout_characters.get(position) {
        match flag {
            '-' => directive.verb.left_aligned = true,
            '+' => directive.verb.plus_signed = true,
            '0' => directive.verb.zero_padded = true,
            _ => break,
        }
        position += 1;
    }
    let (width, after_width) = read_number(layout_characters, position, &mut directive);
    directive.verb.width = width;
    position = after_width;
    if layout_characters.get(position) == Some(&'.') {
        let (precision, after_precision) =
            read_number(layout_characters, position + 1, &mut directive);
        directive.verb.precision = Some(precision.unwrap_or(0));
        position = after_precision;
    }
    directive.verb.letter = *layout_characters.get(position)?;
    directive.written_text = layout_characters[start_position..=position]
        .iter()
        .collect();
    Some((directive, position))
}

fn read_number(
    layout_characters: &[char],
    start_position: usize,
    directive: &mut Directive,
) -> (Option<usize>, usize) {
    let mut value = 0;
    let mut position = start_position;
    while let Some(digit) = layout_characters
        .get(position)
        .and_then(|character| character.to_digit(10))
    {
        if value > FORMAT_PART_LIMIT {
            directive.over_part_limit = true;
        } else {
            value = value * 10 + digit as usize;
        }
        position += 1;
    }
    if value > FORMAT_PART_LIMIT {
        directive.over_part_limit = true;
    }
    ((position > start_position).then_some(value), position)
}

fn directive_part_problem(directive: &Directive, verb_rule: &VerbRule) -> Result<(), String> {
    let fenced = code_span(&directive.written_text);
    let verb = &directive.verb;
    if directive.over_part_limit {
        return Err(format!(
            "{fenced} has a width or a precision over {FORMAT_PART_LIMIT}"
        ));
    }
    if verb.left_aligned && !verb_rule.reads_width {
        return Err(format!("{fenced} takes no `-` flag"));
    }
    if verb.plus_signed && !verb_rule.reads_plus_sign {
        return Err(format!("{fenced} takes no `+` flag"));
    }
    if verb.zero_padded && !verb_rule.reads_zero_padding {
        return Err(format!("{fenced} takes no `0` flag"));
    }
    if verb.width.is_some() && !verb_rule.reads_width {
        return Err(format!("{fenced} takes no width"));
    }
    if verb.precision.is_some() && !verb_rule.reads_precision {
        return Err(format!("{fenced} takes no precision"));
    }
    if verb.letter == 'B' && verb.precision.is_none_or(|precision| precision < 1) {
        return Err(format!(
            "{fenced} takes a precision of at least 1, such as `%.64B`"
        ));
    }
    Ok(())
}

fn operand_of(operand_value: &JsonValue) -> Operand<'_> {
    let mut operand = Operand {
        kind: OperandKind::Other,
        kind_name: "null",
        text_value: "",
        whole_value: 0,
        number_value: 0.0,
        boolean_value: false,
    };
    match operand_value {
        JsonValue::String(text) => {
            operand.kind = OperandKind::Text;
            operand.kind_name = TEXT_KINDS;
            operand.text_value = text;
        }
        JsonValue::Bool(boolean) => {
            operand.kind = OperandKind::Boolean;
            operand.kind_name = "a boolean";
            operand.boolean_value = *boolean;
        }
        JsonValue::Array(_) => operand.kind_name = "a list",
        JsonValue::Object(_) => operand.kind_name = "a map",
        JsonValue::Null => {}
        JsonValue::Number(number) => {
            let number_value = number.as_f64().unwrap_or(f64::NAN);
            if let Some(whole_value) = number.as_i64() {
                operand.kind = OperandKind::Whole;
                operand.kind_name = WHOLE_KINDS;
                operand.whole_value = whole_value;
            } else if number_value.fract() == 0.0
                && (-WHOLE_NUMBER_LIMIT..WHOLE_NUMBER_LIMIT).contains(&number_value)
            {
                operand.kind = OperandKind::Whole;
                operand.kind_name = WHOLE_KINDS;
                operand.whole_value = number_value as i64;
            } else {
                operand.kind = OperandKind::Number;
                operand.kind_name = NUMBER_KINDS;
                operand.number_value = number_value;
            }
        }
    }
    operand
}

fn accepts(verb_rule: &VerbRule, kind: OperandKind) -> bool {
    match kind {
        OperandKind::Text => verb_rule.accepts_text,
        OperandKind::Whole => verb_rule.accepts_whole,
        OperandKind::Number => verb_rule.accepts_number,
        OperandKind::Boolean => verb_rule.accepts_boolean,
        OperandKind::Other => false,
    }
}

fn formatted_verb_text(
    verb: &LayoutVerb,
    verb_rule: &VerbRule,
    operand: &Operand<'_>,
    operand_number: usize,
) -> Result<String, String> {
    if !accepts(verb_rule, operand.kind) {
        return Err(format!(
            "{} writes {}, and operand {operand_number} is {}",
            code_span(&format!("%{}", verb.letter)),
            verb_rule.accepted_kinds,
            operand.kind_name
        ));
    }
    let read_text: String = match verb.precision {
        Some(precision) if verb.letter != 'B' => {
            operand.text_value.chars().take(precision).collect()
        }
        _ => operand.text_value.to_owned(),
    };
    Ok(match verb.letter {
        's' => read_text,
        'q' => go_quoted_text(&read_text),
        'u' => read_text.chars().map(simple_upper_case).collect(),
        'l' => read_text.chars().map(simple_lower_case).collect(),
        'A' => read_text.replace('/', "_"),
        'B' => tail_bounded_text(&read_text, verb.precision.unwrap_or(1)),
        'v' => match operand.kind {
            OperandKind::Whole => operand.whole_value.to_string(),
            OperandKind::Boolean => operand.boolean_value.to_string(),
            _ => read_text,
        },
        'd' => signed_number_text(operand.whole_value.to_string(), verb.plus_signed),
        'x' => hexadecimal_text(operand),
        'X' => hexadecimal_text(operand).to_uppercase(),
        'f' => signed_number_text(
            fixed_point_text(operand, verb.precision.unwrap_or(6)),
            verb.plus_signed,
        ),
        _ => date_text(operand, operand_number)?,
    })
}

fn simple_upper_case(character: char) -> char {
    let code_point = character as u32;
    let excepted = match code_point {
        0x1f80..=0x1f87 | 0x1f90..=0x1f97 | 0x1fa0..=0x1fa7 => Some(code_point + 8),
        0x1fb3 | 0x1fc3 | 0x1ff3 => Some(code_point + 9),
        _ => None,
    };
    if let Some(mapped) = excepted.and_then(char::from_u32) {
        return mapped;
    }
    single_character(character.to_uppercase()).unwrap_or(character)
}

fn simple_lower_case(character: char) -> char {
    if character == '\u{130}' {
        return 'i';
    }
    single_character(character.to_lowercase()).unwrap_or(character)
}

fn single_character(mut mapped: impl Iterator<Item = char>) -> Option<char> {
    let first = mapped.next()?;
    mapped.next().is_none().then_some(first)
}

fn tail_bounded_text(text: &str, precision: usize) -> String {
    let length = text.chars().count();
    if length <= precision {
        return text.to_owned();
    }
    let tail: String = text.chars().skip(length - (precision - 1)).collect();
    format!("-{tail}")
}

fn signed_number_text(number_text: String, plus_signed: bool) -> String {
    if plus_signed && !number_text.starts_with('-') {
        format!("+{number_text}")
    } else {
        number_text
    }
}

fn hexadecimal_text(operand: &Operand<'_>) -> String {
    if operand.kind == OperandKind::Text {
        return operand
            .text_value
            .bytes()
            .map(|byte| format!("{byte:02x}"))
            .collect();
    }
    let whole_value = i128::from(operand.whole_value);
    if whole_value < 0 {
        format!("-{:x}", -whole_value)
    } else {
        format!("{whole_value:x}")
    }
}

fn fixed_point_text(operand: &Operand<'_>, precision: usize) -> String {
    if operand.kind == OperandKind::Whole {
        if precision == 0 {
            return operand.whole_value.to_string();
        }
        return format!("{}.{}", operand.whole_value, "0".repeat(precision));
    }
    format!("{:.precision$}", operand.number_value)
}

fn date_text(operand: &Operand<'_>, operand_number: usize) -> Result<String, String> {
    let range = EARLIEST_FORMAT_SECONDS..LATEST_FORMAT_SECONDS;
    let seconds = if operand.kind == OperandKind::Whole {
        Some(operand.whole_value).filter(|seconds| range.contains(seconds))
    } else {
        let floored = operand.number_value.floor();
        (floored >= EARLIEST_FORMAT_SECONDS as f64 && floored < LATEST_FORMAT_SECONDS as f64)
            .then_some(floored as i64)
    };
    let Some(seconds) = seconds else {
        return Err(format!(
            "`%C` writes a time in the years 1 to 9999, and operand {operand_number} is outside them"
        ));
    };
    let (year, month, day) = civil_date_of_days(seconds.div_euclid(SECONDS_PER_DAY));
    Ok(format!("{year:04}-{month:02}-{day:02}"))
}

fn civil_date_of_days(days: i64) -> (i64, i64, i64) {
    let shifted_days = days + 719_468;
    let era = shifted_days / 146_097;
    let day_of_era = shifted_days - era * 146_097;
    let year_of_era =
        (day_of_era - day_of_era / 1460 + day_of_era / 36_524 - day_of_era / 146_096) / 365;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let shifted_month = (5 * day_of_year + 2) / 153;
    let day = day_of_year - (153 * shifted_month + 2) / 5 + 1;
    let month = if shifted_month < 10 {
        shifted_month + 3
    } else {
        shifted_month - 9
    };
    let year = year_of_era + era * 400 + i64::from(month <= 2);
    (year, month, day)
}

fn go_quoted_text(text: &str) -> String {
    let mut quoted = String::from("\"");
    for character in text.chars() {
        match character {
            '"' => quoted.push_str("\\\""),
            '\\' => quoted.push_str("\\\\"),
            '\u{7}' => quoted.push_str("\\a"),
            '\u{8}' => quoted.push_str("\\b"),
            '\u{c}' => quoted.push_str("\\f"),
            '\n' => quoted.push_str("\\n"),
            '\r' => quoted.push_str("\\r"),
            '\t' => quoted.push_str("\\t"),
            '\u{b}' => quoted.push_str("\\v"),
            '\u{0}'..='\u{1f}' | '\u{7f}' => {
                quoted.push_str(&format!("\\x{:02x}", character as u32));
            }
            '\u{80}'..='\u{9f}' => quoted.push_str(&format!("\\u{:04x}", character as u32)),
            _ => quoted.push(character),
        }
    }
    quoted.push('"');
    quoted
}

fn padded_verb_text(verb: &LayoutVerb, verb_rule: &VerbRule, verb_text: String) -> String {
    let length = verb_text.chars().count();
    let Some(padding) = verb.width.and_then(|width| width.checked_sub(length)) else {
        return verb_text;
    };
    if verb.left_aligned {
        return format!("{verb_text}{}", " ".repeat(padding));
    }
    if verb.zero_padded && verb_rule.reads_zero_padding {
        let sign_length = usize::from(verb_text.starts_with(['-', '+']));
        let (sign_text, digits) = verb_text.split_at(sign_length);
        return format!("{sign_text}{}{digits}", "0".repeat(padding));
    }
    format!("{}{verb_text}", " ".repeat(padding))
}
