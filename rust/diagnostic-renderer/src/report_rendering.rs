use std::ops::Range;

use yaml_dsl_spec::{Diagnostic, DiagnosticNotes, Position};

use crate::line_wrapping::{LEVEL_COLUMNS, WrappedLine, WrappedRow};
use crate::{DiagnosticSource, RenderStyle, Severity};

const CONTEXT_LINES: usize = 2;
const GUTTER_COLUMNS: usize = 3;
const RESET: &str = "\x1b[0m";
const SUBJECT: &str = "\x1b[1m";
const LABEL: &str = "\x1b[36m";
const ATTENTION: &str = "\x1b[33m";
const FAILURE: &str = "\x1b[91m";
const DIM: &str = "\x1b[90m";
const SCHEMA_NOTE: &str = "\x1b[38;2;166;171;242m";
const SCHEMA_MARK: &str = "\x1b[1;38;2;166;171;242m";
const SCHEMA_PREFIX: &str = "[schema]";

impl RenderStyle {
    fn paint(self, code: &str, text: &str) -> String {
        if self.color {
            format!("{code}{text}{RESET}")
        } else {
            text.to_owned()
        }
    }

    fn headline(self, word: &str, word_color: &str, message: &str) -> String {
        let line = WrappedLine::new(&format!("{word}: {message}"), self.width);
        let rows = line.row_texts();
        let mut shown = Vec::with_capacity(rows.len());
        for (index, row) in rows.iter().enumerate() {
            if index > 0 {
                let words = row.trim_start();
                let indent = &row[..row.len() - words.len()];
                shown.push(format!("{indent}{}", self.paint(SUBJECT, words)));
                continue;
            }
            let after_word = row.strip_prefix(word).unwrap_or(row);
            let after_colon = after_word.strip_prefix(':').unwrap_or(after_word);
            let words = after_colon.trim_start();
            let mut first = format!("{}:", self.paint(word_color, word));
            if !words.is_empty() {
                first.push_str(&format!(" {}", self.paint(SUBJECT, words)));
            }
            shown.push(first);
        }
        shown.join("\n")
    }
}

/// A diagnostic as a compiler writes an error: the headline, the source's name and the position, the lines that hold the wrong text and the lines it is nested under, and the notes.
pub fn render_diagnostic(
    diagnostic: &Diagnostic,
    severity: Severity,
    source: DiagnosticSource<'_>,
    style: RenderStyle,
) -> String {
    let (severity_word, severity_color) = match severity {
        Severity::Error => ("error", FAILURE),
        Severity::Warning => ("warning", ATTENTION),
    };
    let mut rendered = style.headline(severity_word, severity_color, &diagnostic.message);
    let notes = diagnostic.notes.as_deref();
    let Some(position) = &diagnostic.position else {
        return rendered + &notes_text(notes, 1, style);
    };
    let lines: Vec<&str> = source.text.lines().collect();
    let shown = shown_lines(&lines, position.line);
    let width = shown.last().map_or(1, |line| line.to_string().len());
    let gutter = |text: &str| style.paint(DIM, text);
    let blank = gutter(&format!("{} |", " ".repeat(width)));
    rendered.push_str(&format!(
        "\n{}{} {}:{}:{}",
        " ".repeat(width),
        gutter("-->"),
        source.name,
        position.line,
        position.column
    ));
    if shown.is_empty() {
        return rendered + &notes_text(notes, width, style);
    }
    rendered.push_str(&format!("\n{blank}"));
    let room = style
        .width
        .map(|columns| columns.saturating_sub(width + GUTTER_COLUMNS));
    let mut previous: Option<usize> = None;
    for &number in &shown {
        if previous.is_some_and(|previous| number > previous + 1) {
            rendered.push_str(&format!("\n{}", gutter("...")));
        }
        let line = WrappedLine::new(lines[number - 1], room);
        let underlined = (number == position.line).then(|| underlined_span(&line, position));
        for (index, row) in line.rows.iter().enumerate() {
            let row_gutter = if index == 0 {
                gutter(&format!("{number:>width$} |"))
            } else {
                blank.clone()
            };
            rendered.push_str(&format!("\n{row_gutter} {}", line.row_text(row)));
            if let Some((row_index, start, end)) = underlined
                && row_index == index
            {
                let underline_rows =
                    underline(&line, row, start..end, notes, room, severity_color, style);
                for underline_row in underline_rows {
                    rendered.push_str(&format!("\n{blank} {underline_row}"));
                }
            }
        }
        previous = Some(number);
    }
    rendered.push_str(&format!("\n{blank}"));
    rendered + &notes_text(notes, width, style)
}

fn shown_lines(lines: &[&str], line: usize) -> Vec<usize> {
    if line == 0 || line > lines.len() {
        return Vec::new();
    }
    let first = line.saturating_sub(CONTEXT_LINES).max(1);
    let last = (line + CONTEXT_LINES).min(lines.len());
    let mut shown: Vec<usize> = (first..=last).collect();
    let mut indent = indentation(lines[line - 1]);
    for number in (1..line).rev() {
        let text = lines[number - 1];
        if text.trim().is_empty() || text.trim_start().starts_with('#') {
            continue;
        }
        let nested_under = indentation(text);
        if nested_under < indent {
            if number < first {
                shown.push(number);
            }
            indent = nested_under;
            if indent == 0 {
                break;
            }
        }
    }
    shown.sort_unstable();
    shown
}

fn indentation(text: &str) -> usize {
    text.len() - text.trim_start().len()
}

fn underlined_span(line: &WrappedLine, position: &Position) -> (usize, usize, usize) {
    let start = position.column.saturating_sub(1).min(line.characters.len());
    let rest: String = line.characters[start..].iter().collect();
    let end = (start + token_length(&rest).max(1)).min(line.characters.len().max(start + 1));
    let row_index = line
        .rows
        .iter()
        .rposition(|row| row.start < end)
        .unwrap_or(0);
    (row_index, start, end)
}

fn underline(
    line: &WrappedLine,
    row: &WrappedRow,
    span: Range<usize>,
    notes: Option<&DiagnosticNotes>,
    room: Option<usize>,
    severity_color: &str,
    style: RenderStyle,
) -> Vec<String> {
    let first = span.start.max(row.start);
    let last = span.end.min(row.end).max(first);
    let caret_column = row.indent + line.columns(row.start, first);
    let carets = "^".repeat(line.columns(first, last).max(1));
    let indent = " ".repeat(caret_column);
    let Some(label) = notes.and_then(|notes| notes.label.as_deref()) else {
        return vec![format!("{indent}{}", style.paint(severity_color, &carets))];
    };
    let label_columns = WrappedLine::new(label, None).columns(0, label.chars().count());
    let fits_beside =
        room.is_none_or(|room| caret_column + carets.len() + 1 + label_columns <= room);
    if fits_beside {
        return vec![format!(
            "{indent}{}",
            style.paint(severity_color, &format!("{carets} {label}"))
        )];
    }
    let label_room = room.map(|room| room.saturating_sub(caret_column));
    let mut rows = vec![format!("{indent}{}", style.paint(severity_color, &carets))];
    for label_row in WrappedLine::new(label, label_room).row_texts() {
        rows.push(format!(
            "{indent}{}",
            style.paint(severity_color, &label_row)
        ));
    }
    rows
}

fn token_length(rest: &str) -> usize {
    if rest.starts_with("{{")
        && let Some(end) = rest.find("}}")
    {
        return rest[..end + 2].chars().count();
    }
    let mut characters = rest.char_indices();
    if let Some((_, quote @ ('"' | '\''))) = characters.next() {
        let mut escaped = false;
        for (index, character) in characters {
            if escaped {
                escaped = false;
            } else if character == '\\' && quote == '"' {
                escaped = true;
            } else if character == quote {
                return rest[..=index].chars().count();
            }
        }
        return rest.chars().count();
    }
    let end = [rest.find(": "), rest.find(" #")]
        .into_iter()
        .flatten()
        .min()
        .unwrap_or(rest.len());
    let token = rest[..end].trim_end();
    token.strip_suffix(':').unwrap_or(token).chars().count()
}

fn notes_text(notes: Option<&DiagnosticNotes>, width: usize, style: RenderStyle) -> String {
    let Some(notes) = notes else {
        return String::new();
    };
    let marker = format!("{} =", " ".repeat(width));
    let mut text = String::new();
    if let Some(rule) = &notes.rule {
        text.push_str(&named_note(&marker, "rule", rule, style));
    }
    if let Some(schema) = &notes.schema {
        text.push_str(&schema_note(&marker, schema, style));
    }
    if let Some(help) = &notes.help {
        text.push_str(&named_note(&marker, "help", help, style));
    }
    text
}

fn named_note(marker: &str, name: &str, note: &str, style: RenderStyle) -> String {
    let mut note_lines = note.lines();
    let first_line = note_lines.next().unwrap_or_default();
    let line = WrappedLine::new(&format!("{marker} {name}: {first_line}"), style.width);
    let mut text = String::new();
    for (index, row) in line.row_texts().iter().enumerate() {
        if index > 0 {
            text.push_str(&format!("\n{row}"));
            continue;
        }
        let after_name = row
            .strip_prefix(&format!("{marker} {name}:"))
            .unwrap_or_default();
        text.push_str(&format!(
            "\n{} {}{after_name}",
            style.paint(DIM, marker),
            style.paint(LABEL, &format!("{name}:"))
        ));
    }
    for row in continued_note_rows(marker, note_lines, style) {
        text.push_str(&format!("\n{row}"));
    }
    text
}

fn schema_note(marker: &str, note: &str, style: RenderStyle) -> String {
    let mut note_lines = note.lines();
    let first_line = note_lines.next().unwrap_or_default();
    let line = WrappedLine::new(
        &format!("{marker} {SCHEMA_PREFIX} {first_line}"),
        style.width,
    );
    let mut text = String::new();
    for (index, row) in line.row_texts().iter().enumerate() {
        if index > 0 {
            text.push_str(&format!("\n{}", painted_words(row, style)));
            continue;
        }
        let after_prefix = row
            .strip_prefix(&format!("{marker} {SCHEMA_PREFIX}"))
            .unwrap_or_default();
        text.push_str(&format!(
            "\n{} {}{}",
            style.paint(DIM, marker),
            style.paint(SCHEMA_MARK, SCHEMA_PREFIX),
            painted_words(after_prefix, style)
        ));
    }
    for row in continued_note_rows(marker, note_lines, style) {
        text.push_str(&format!("\n{}", painted_words(&row, style)));
    }
    text
}

fn continued_note_rows<'a>(
    marker: &str,
    note_lines: impl Iterator<Item = &'a str>,
    style: RenderStyle,
) -> Vec<String> {
    let indent = " ".repeat(indentation(marker) + LEVEL_COLUMNS);
    note_lines
        .flat_map(|note_line| {
            WrappedLine::new(&format!("{indent}{note_line}"), style.width).row_texts()
        })
        .collect()
}

fn painted_words(row: &str, style: RenderStyle) -> String {
    let words = row.trim_start();
    if words.is_empty() {
        return row.to_owned();
    }
    let indent = &row[..row.len() - words.len()];
    format!("{indent}{}", style.paint(SCHEMA_NOTE, words))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_lines_nested_under_are_found_from_the_wrong_line_up_through_the_lines_around_it() {
        let lines = [
            "data_source:",
            "  mock_bucket:",
            "    type: remote",
            "    outputs: [mock]",
            "    path: mock",
            "",
            "  # mock comment",
            "  mock_key:",
            "    type: kms_keys",
        ];
        assert_eq!(shown_lines(&lines, 9), [1, 7, 8, 9]);
        assert_eq!(shown_lines(&lines, 0), Vec::<usize>::new());
        assert_eq!(shown_lines(&lines, 10), Vec::<usize>::new());
    }

    #[test]
    fn a_token_ends_at_its_colon_its_quote_or_a_comment() {
        assert_eq!(token_length("brnch: main"), 5);
        assert_eq!(token_length("\"gld\" # tier"), 5);
        assert_eq!(token_length("\"a\\\"b\" x"), 6);
        assert_eq!(token_length("'unclosed"), 9);
        assert_eq!(token_length("{{ mock.ref }} tail"), 14);
        assert_eq!(token_length("kms_keys  # typo"), 8);
        assert_eq!(token_length("2"), 1);
    }
}
