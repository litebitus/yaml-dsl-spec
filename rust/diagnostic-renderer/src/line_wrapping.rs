use unicode_linebreak::linebreaks;
use unicode_width::UnicodeWidthChar;

const SEPARATORS: &str = "=:/,;&?|.()[]{}!-";
const COMPARISON_OPERATOR_CHARACTERS: &str = "=<>!~-&|";
pub const LEVEL_COLUMNS: usize = 2;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct WrappedRow {
    pub start: usize,
    pub end: usize,
    pub indent: usize,
}

pub struct WrappedLine {
    pub characters: Vec<char>,
    pub rows: Vec<WrappedRow>,
}

impl WrappedLine {
    pub fn new(line: &str, width: Option<usize>) -> Self {
        let characters: Vec<char> = line.chars().collect();
        let rows = match width {
            Some(width) => divided_rows(&characters, width),
            None => vec![WrappedRow {
                start: 0,
                end: characters.len(),
                indent: 0,
            }],
        };
        Self { characters, rows }
    }

    pub fn row_text(&self, row: &WrappedRow) -> String {
        let text: String = self.characters[row.start..row.end].iter().collect();
        format!("{}{}", " ".repeat(row.indent), text.trim_end())
    }

    pub fn row_texts(&self) -> Vec<String> {
        self.rows.iter().map(|row| self.row_text(row)).collect()
    }

    pub fn columns(&self, start: usize, end: usize) -> usize {
        columns(&self.characters[start.min(end)..end])
    }
}

fn columns(characters: &[char]) -> usize {
    characters
        .iter()
        .map(|character| character.width().unwrap_or(0))
        .sum()
}

fn division_points(characters: &[char]) -> Vec<bool> {
    let text: String = characters.iter().collect();
    let mut unicode_allows = vec![false; characters.len() + 1];
    let mut byte_offsets = text.char_indices().map(|(offset, _)| offset).enumerate();
    for (byte_offset, _) in linebreaks(&text) {
        if let Some((index, _)) = byte_offsets.find(|(_, offset)| *offset == byte_offset) {
            unicode_allows[index] = true;
        }
    }
    (0..characters.len())
        .map(|index| {
            if index == 0 || characters[index] == ' ' {
                return false;
            }
            let (before, after) = (characters[index - 1], characters[index]);
            let inside_comparison_operator = COMPARISON_OPERATOR_CHARACTERS.contains(before)
                && COMPARISON_OPERATOR_CHARACTERS.contains(after);
            !inside_comparison_operator && (unicode_allows[index] || SEPARATORS.contains(before))
        })
        .collect()
}

fn divided_rows(characters: &[char], width: usize) -> Vec<WrappedRow> {
    let leading = characters
        .iter()
        .take_while(|character| **character == ' ')
        .count();
    let continuation_indent = leading + LEVEL_COLUMNS;
    let divides = division_points(characters);
    let printed_columns = |start: usize, end: usize| {
        let mut end = end;
        while end > start && characters[end - 1] == ' ' {
            end -= 1;
        }
        columns(&characters[start..end])
    };
    let mut rows = Vec::new();
    let mut start = 0;
    let mut indent = 0;
    while start < characters.len() {
        let mut row_indent = indent;
        let end = loop {
            let room = width.saturating_sub(row_indent);
            if printed_columns(start, characters.len()) <= room {
                break characters.len();
            }
            let fitting = (start + 1..characters.len())
                .filter(|index| divides[*index])
                .take_while(|index| printed_columns(start, *index) <= room)
                .last();
            if let Some(end) = fitting {
                break end;
            }
            if row_indent > 0 {
                row_indent = row_indent.saturating_sub(LEVEL_COLUMNS);
                continue;
            }
            break (start + 1..characters.len())
                .find(|index| divides[*index])
                .unwrap_or(characters.len());
        };
        rows.push(WrappedRow {
            start,
            end,
            indent: row_indent,
        });
        start = end;
        while start < characters.len() && characters[start] == ' ' {
            start += 1;
        }
        indent = continuation_indent;
    }
    if rows.is_empty() {
        rows.push(WrappedRow {
            start: 0,
            end: 0,
            indent: 0,
        });
    }
    rows
}

#[cfg(test)]
mod tests {
    use super::*;

    fn texts(line: &str, width: usize) -> Vec<String> {
        WrappedLine::new(line, Some(width)).row_texts()
    }

    #[test]
    fn a_line_that_fits_or_has_no_width_is_one_row() {
        assert_eq!(texts("  short: line", 40), ["  short: line"]);
        let unmeasured = "x".repeat(300);
        assert_eq!(
            WrappedLine::new(&unmeasured, None).row_texts(),
            std::slice::from_ref(&unmeasured)
        );
        assert_eq!(texts("", 10), [""]);
    }

    #[test]
    fn a_long_line_divides_after_a_separator_and_continues_one_level_in() {
        assert_eq!(
            texts("  - s3://mock-bucket/schemas/mock.schema.json", 24),
            [
                "  - s3://mock-bucket/",
                "    schemas/mock.schema.",
                "    json"
            ]
        );
    }

    #[test]
    fn a_comparison_operator_is_never_divided_and_a_word_with_no_division_overruns() {
        let divides = division_points(&"a->b x=y".chars().collect::<Vec<_>>());
        assert!(!divides[2]);
        assert!(divides[7]);
        assert_eq!(
            texts("mock_identifier_without_a_division", 10),
            ["mock_identifier_without_a_division"]
        );
    }

    #[test]
    fn the_indentation_gives_before_a_word_overruns() {
        assert_eq!(
            texts("      key: abcdefghij", 12),
            ["      key:", "  abcdefghij"]
        );
    }

    #[test]
    fn chinese_divides_between_any_two_characters() {
        assert_eq!(
            texts("    description: 这个生成器把每个声明写成", 30),
            ["    description: 这个生成器把", "      每个声明写成"]
        );
    }

    #[test]
    fn widths_are_display_columns() {
        assert_eq!(texts("名前 値値値", 8), ["名前 値", "  値値"]);
    }
}
