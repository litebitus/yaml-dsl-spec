//! The standard's format function: a layout and the operands that fill its verbs.

use serde_json::Value as JsonValue;

use crate::problem_wording::code_span;

const FORMAT_PART_LIMIT: usize = 1000;
const MAXIMUM_TIME_DIGITS: usize = 9;
const EARLIEST_FORMAT_SECONDS: i64 = -62_167_219_200;
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
    reads_date_format: bool,
}

/// One part of a layout: its literal text, or a verb.
#[derive(Clone, Debug, PartialEq)]
pub enum LayoutPart {
    Text(String),
    Verb(LayoutVerb),
}

/// A verb as a layout writes it: its letter, its flags, its width and
/// precision, and its date format, none where left out.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct LayoutVerb {
    pub letter: char,
    pub left_aligned: bool,
    pub plus_signed: bool,
    pub zero_padded: bool,
    pub width: Option<usize>,
    pub precision: Option<usize>,
    pub date_format: Option<DateFormat>,
}

/// A date format: its parts, and its time offset as written, none where left
/// out.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct DateFormat {
    pub parts: Vec<DateFormatPart>,
    pub offset: Option<String>,
}

/// One part of a date format: its literal text, `%%` read as text, or a date
/// verb.
#[derive(Clone, Debug, PartialEq)]
pub enum DateFormatPart {
    Text(String),
    DateVerb(DateVerb),
}

/// A date verb: its letter, its flags and its width, none where left out.
#[derive(Clone, Debug, PartialEq)]
pub struct DateVerb {
    pub letter: char,
    pub left_aligned: bool,
    pub zero_padded: bool,
    pub width: Option<usize>,
}

const DATE_VERB_LETTERS: &str = "YymdHMSLz";

#[derive(Default)]
struct Directive {
    verb: LayoutVerb,
    written_text: String,
    over_part_limit: bool,
    date_format_problem: Option<String>,
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
        let (directive, end_position) = read_directive(&layout_characters, position);
        if let Some(date_format_problem) = directive.date_format_problem {
            return Err(date_format_problem);
        }
        let Some(end_position) = end_position else {
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
            reads_precision: true,
            reads_date_format: true,
            ..VerbRule::default()
        },
        _ => return None,
    })
}

fn read_directive(layout_characters: &[char], start_position: usize) -> (Directive, Option<usize>) {
    let mut directive = Directive::default();
    let end_position = read_directive_parts(layout_characters, start_position, &mut directive);
    (directive, end_position)
}

fn read_directive_parts(
    layout_characters: &[char],
    start_position: usize,
    directive: &mut Directive,
) -> Option<usize> {
    let mut position = read_flags_and_width(layout_characters, start_position + 1, directive);
    if layout_characters.get(position) == Some(&'{') {
        let (date_format, after_date_format) =
            read_date_format(layout_characters, position + 1, directive)?;
        directive.verb.date_format = Some(date_format);
        position = after_date_format;
    }
    position = read_precision(layout_characters, position, directive);
    directive.verb.letter = *layout_characters.get(position)?;
    directive.written_text = layout_characters[start_position..=position]
        .iter()
        .collect();
    Some(position)
}

/// Reads a verb's or a date verb's flags and width, answering where they end.
fn read_flags_and_width(
    layout_characters: &[char],
    start_position: usize,
    directive: &mut Directive,
) -> usize {
    let mut position = start_position;
    while let Some(&flag) = layout_characters.get(position) {
        match flag {
            '-' => directive.verb.left_aligned = true,
            '+' => directive.verb.plus_signed = true,
            '0' => directive.verb.zero_padded = true,
            _ => break,
        }
        position += 1;
    }
    let (width, after_width) = read_number(layout_characters, position, directive);
    directive.verb.width = width;
    after_width
}

/// Reads a precision where one is written, `.` alone being 0, answering where
/// it ends.
fn read_precision(layout_characters: &[char], position: usize, directive: &mut Directive) -> usize {
    if layout_characters.get(position) != Some(&'.') {
        return position;
    }
    let (precision, after_precision) = read_number(layout_characters, position + 1, directive);
    directive.verb.precision = Some(precision.unwrap_or(0));
    after_precision
}

/// Reads a date format from after its opening brace to its closing one,
/// answering it and where it ends; none where it is not closed. Its first
/// problem is kept for the directive's.
fn read_date_format(
    layout_characters: &[char],
    start_position: usize,
    directive: &mut Directive,
) -> Option<(DateFormat, usize)> {
    let mut date_format = DateFormat::default();
    let mut literal_text = String::new();
    let mut position = start_position;
    while let Some(&character) = layout_characters.get(position) {
        if character == '}' || character == '|' {
            break;
        }
        if character != '%' {
            literal_text.push(character);
            position += 1;
            continue;
        }
        if layout_characters.get(position + 1) == Some(&'%') {
            literal_text.push('%');
            position += 2;
            continue;
        }
        let (date_verb, after_date_verb) = read_date_verb(
            layout_characters,
            position,
            &mut directive.date_format_problem,
        )?;
        position = after_date_verb;
        if !literal_text.is_empty() {
            date_format
                .parts
                .push(DateFormatPart::Text(std::mem::take(&mut literal_text)));
        }
        date_format.parts.push(DateFormatPart::DateVerb(date_verb));
    }
    if !literal_text.is_empty() {
        date_format.parts.push(DateFormatPart::Text(literal_text));
    }
    if layout_characters.get(position) == Some(&'|') {
        let offset_start = position + 1;
        while layout_characters
            .get(position)
            .is_some_and(|character| *character != '}')
        {
            position += 1;
        }
        let offset_text: String = layout_characters[offset_start..position].iter().collect();
        if offset_text.is_empty() {
            keep_problem(
                &mut directive.date_format_problem,
                "a date format's `|` takes an offset, such as `+08:00`".to_owned(),
            );
        } else if offset_minutes_of(&offset_text).is_none() {
            keep_problem(
                &mut directive.date_format_problem,
                format!(
                    "{} is not an offset, such as `Z` or `+08:00`",
                    code_span(&offset_text)
                ),
            );
        }
        date_format.offset = Some(offset_text);
    }
    layout_characters.get(position)?;
    Some((date_format, position + 1))
}

/// Reads a date verb from its `%` through its letter, answering it and where
/// it ends; none where the layout ends first. Its first problem is kept.
fn read_date_verb(
    layout_characters: &[char],
    start_position: usize,
    kept_problem: &mut Option<String>,
) -> Option<(DateVerb, usize)> {
    let mut directive = Directive::default();
    let mut position = read_flags_and_width(layout_characters, start_position + 1, &mut directive);
    position = read_precision(layout_characters, position, &mut directive);
    let date_letter = *layout_characters.get(position)?;
    let written_date_verb: String = layout_characters[start_position..=position]
        .iter()
        .collect();
    let fenced_date_verb = code_span(&written_date_verb);
    let verb = &directive.verb;
    let problem = if !DATE_VERB_LETTERS.contains(date_letter) {
        Some(format!(
            "{fenced_date_verb} is not a date verb `format` writes"
        ))
    } else if directive.over_part_limit {
        Some(format!(
            "{fenced_date_verb} has a width or a precision over {FORMAT_PART_LIMIT}"
        ))
    } else if verb.plus_signed {
        Some(format!("{fenced_date_verb} takes no `+` flag"))
    } else if verb.zero_padded && date_letter == 'z' {
        Some(format!("{fenced_date_verb} takes no `0` flag"))
    } else if verb.precision.is_some() {
        Some(format!("{fenced_date_verb} takes no precision"))
    } else {
        None
    };
    if let Some(problem) = problem {
        keep_problem(kept_problem, problem);
    }
    let date_verb = DateVerb {
        letter: date_letter,
        left_aligned: verb.left_aligned,
        zero_padded: verb.zero_padded,
        width: verb.width,
    };
    Some((date_verb, position + 1))
}

fn keep_problem(kept_problem: &mut Option<String>, problem: String) {
    if kept_problem.is_none() {
        *kept_problem = Some(problem);
    }
}

/// Reads a time offset in minutes: `Z`, or a sign, hours `00` to `23`, `:` and
/// minutes `00` to `59`; none where it is not one.
fn offset_minutes_of(offset_text: &str) -> Option<i64> {
    if offset_text == "Z" {
        return Some(0);
    }
    let offset_characters: Vec<char> = offset_text.chars().collect();
    let [sign, hour_tens, hour_ones, ':', minute_tens, minute_ones] = offset_characters[..] else {
        return None;
    };
    let two_digit_value =
        |tens: char, ones: char| Some(tens.to_digit(10)? * 10 + ones.to_digit(10)?);
    let hours = i64::from(two_digit_value(hour_tens, hour_ones)?);
    let minutes = i64::from(two_digit_value(minute_tens, minute_ones)?);
    if hours > 23 || minutes > 59 {
        return None;
    }
    match sign {
        '+' => Some(hours * 60 + minutes),
        '-' => Some(-(hours * 60 + minutes)),
        _ => None,
    }
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
    if verb.date_format.is_some() && !verb_rule.reads_date_format {
        return Err(format!("{fenced} takes no date format"));
    }
    if verb.letter == 'B' && verb.precision.is_none_or(|precision| precision < 1) {
        return Err(format!(
            "{fenced} takes a precision of at least 1, such as `%.64B`"
        ));
    }
    if verb.letter == 'C'
        && verb
            .precision
            .is_some_and(|precision| precision > MAXIMUM_TIME_DIGITS)
    {
        return Err(format!(
            "{fenced} takes a precision of at most {MAXIMUM_TIME_DIGITS}, such as `.3` for milliseconds"
        ));
    }
    if verb_rule.reads_date_format && verb.date_format.is_none() {
        return Err(format!(
            "{fenced} takes a date format, such as `%{{%Y-%m-%d}}C`"
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
        _ => date_text(
            operand,
            verb.precision.unwrap_or(0),
            verb.date_format.as_ref().unwrap_or(&DateFormat::default()),
            operand_number,
        )?,
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

fn date_text(
    operand: &Operand<'_>,
    time_digits: usize,
    date_format: &DateFormat,
    operand_number: usize,
) -> Result<String, String> {
    let range_problem = || {
        format!(
            "`%C` writes a time in the years 0000 to 9999, and operand {operand_number} is outside them"
        )
    };
    let near_range =
        EARLIEST_FORMAT_SECONDS - SECONDS_PER_DAY..LATEST_FORMAT_SECONDS + SECONDS_PER_DAY;
    let units_per_second = 10_i64.pow(time_digits as u32);
    let (seconds, milliseconds) = if operand.kind == OperandKind::Whole {
        let whole_value = operand.whole_value;
        (
            whole_value.div_euclid(units_per_second),
            whole_value.rem_euclid(units_per_second) * 1000 / units_per_second,
        )
    } else {
        let time_in_seconds = operand.number_value / units_per_second as f64;
        let floored_seconds = time_in_seconds.floor();
        let is_near_range =
            floored_seconds >= near_range.start as f64 && floored_seconds < near_range.end as f64;
        if !is_near_range {
            return Err(range_problem());
        }
        (
            floored_seconds as i64,
            ((time_in_seconds - floored_seconds) * 1000.0).floor() as i64,
        )
    };
    if !near_range.contains(&seconds) {
        return Err(range_problem());
    }
    let written_offset = match date_format.offset.as_deref() {
        None | Some("+00:00") => "Z",
        Some(offset) => offset,
    };
    let seconds = seconds + offset_minutes_of(written_offset).unwrap_or(0) * 60;
    if !(EARLIEST_FORMAT_SECONDS..LATEST_FORMAT_SECONDS).contains(&seconds) {
        return Err(range_problem());
    }
    let (year, month, day) = civil_date_of_days(seconds.div_euclid(SECONDS_PER_DAY));
    let second_of_day = seconds.rem_euclid(SECONDS_PER_DAY);
    let date_verb_value = |letter: char| match letter {
        'Y' => year,
        'y' => year % 100,
        'm' => month,
        'd' => day,
        'H' => second_of_day / 3600,
        'M' => second_of_day % 3600 / 60,
        'S' => second_of_day % 60,
        _ => milliseconds,
    };
    Ok(date_format
        .parts
        .iter()
        .map(|part| match part {
            DateFormatPart::Text(text) => text.clone(),
            DateFormatPart::DateVerb(date_verb) if date_verb.letter == 'z' => padded_text(
                written_offset.to_owned(),
                date_verb.width,
                date_verb.left_aligned,
                false,
            ),
            DateFormatPart::DateVerb(date_verb) => padded_text(
                date_verb_value(date_verb.letter).to_string(),
                date_verb.width,
                date_verb.left_aligned,
                date_verb.zero_padded,
            ),
        })
        .collect())
}

fn civil_date_of_days(days: i64) -> (i64, i64, i64) {
    let shifted_days = days + 719_468;
    let era = shifted_days.div_euclid(146_097);
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
    padded_text(
        verb_text,
        verb.width,
        verb.left_aligned,
        verb.zero_padded && verb_rule.reads_zero_padding,
    )
}

/// Pads text to a width: on the right where left-aligned, else with leading
/// zeros after its sign where zero-padded, else with spaces on the left.
fn padded_text(
    text: String,
    width: Option<usize>,
    left_aligned: bool,
    zero_padded: bool,
) -> String {
    let length = text.chars().count();
    let Some(padding) = width.and_then(|width| width.checked_sub(length)) else {
        return text;
    };
    if left_aligned {
        return format!("{text}{}", " ".repeat(padding));
    }
    if zero_padded {
        let sign_length = usize::from(text.starts_with(['-', '+']));
        let (sign_text, digits) = text.split_at(sign_length);
        return format!("{sign_text}{}{digits}", "0".repeat(padding));
    }
    format!("{}{text}", " ".repeat(padding))
}
