//! Reads a template of a DSL's documents: masks its actions so the YAML reader
//! can read it, reads its conditionals and loops as written, resolves each loop
//! variable to the loop that binds it, and reports every problem at its action.

use std::ops::Range;
use std::path::Path;
use std::sync::Arc;

use crate::definition::TemplateDefinition;
use crate::diagnostic::Diagnostic;
use crate::position::Position;
use crate::problem_wording::code_span;

/// What a template reads to.
#[derive(Clone, Debug, PartialEq)]
pub struct TemplateReading {
    /// The template with each action masked, ready for the YAML reader.
    pub masked_text: String,
    pub actions: Vec<TemplateAction>,
    /// The conditionals and loops outside any other, nested as written.
    pub blocks: Vec<TemplateBlock>,
    pub loop_variables: Vec<TemplateLoopVariable>,
    pub template_variables: Vec<TemplateVariable>,
    pub problems: Vec<Diagnostic>,
}

/// A value the rendering tool supplies, read inside an action.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TemplateVariable {
    pub position: Position,
    pub name: String,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TemplateAction {
    pub position: Position,
    pub text: String,
    pub directive: bool,
}

#[derive(Clone, Debug, PartialEq)]
pub enum TemplateBlock {
    Conditional(TemplateConditional),
    Loop(TemplateLoop),
}

#[derive(Clone, Debug, PartialEq)]
pub struct TemplateConditional {
    pub branches: Vec<TemplateBranch>,
    /// Where its closing action is; none when it is left open.
    pub closing: Option<Position>,
}

#[derive(Clone, Debug, PartialEq)]
pub struct TemplateBranch {
    pub opening: Position,
    pub blocks: Vec<TemplateBlock>,
}

#[derive(Clone, Debug, PartialEq)]
pub struct TemplateLoop {
    pub opening: Position,
    pub variables: Vec<String>,
    pub blocks: Vec<TemplateBlock>,
    /// Where its closing action is; none when it is left open.
    pub closing: Option<Position>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct TemplateLoopVariable {
    pub position: Position,
    pub name: String,
    /// Where the opening action of the loop binding it is; none for an unbound one.
    pub bound_by: Option<Position>,
}

/// The kind of directive an action's body is.
#[derive(Clone, Copy, PartialEq, Eq)]
enum DirectiveKind {
    LoopOpening,
    ConditionalOpening,
    ElseIf,
    Else,
    Closing,
}

/// Whether `body` is `word`, or starts with it followed by a space.
fn starts_with_word(body: &str, word: &str) -> bool {
    body == word
        || body
            .strip_prefix(word)
            .is_some_and(|rest| rest.starts_with(' '))
}

/// A conditional or a loop not yet closed, and the text of its opening action.
struct OpenBlock {
    block: TemplateBlock,
    opening_text: String,
    else_text: Option<String>,
}

/// Reads the template at `file`, whose text is `text`, written in `template`'s
/// template language.
pub fn read_template(file: &Path, text: &str, template: &TemplateDefinition) -> TemplateReading {
    let file: Arc<str> = Arc::from(file.to_string_lossy().as_ref());
    TemplateScan::new(&file, text, template).read()
}

struct TemplateScan<'t> {
    file: &'t Arc<str>,
    text: &'t str,
    template: &'t TemplateDefinition,
    masked_ranges: Vec<(Range<usize>, char)>,
    actions: Vec<TemplateAction>,
    root_blocks: Vec<TemplateBlock>,
    open_blocks: Vec<OpenBlock>,
    loop_variables: Vec<TemplateLoopVariable>,
    template_variables: Vec<TemplateVariable>,
    problems: Vec<Diagnostic>,
}

impl<'t> TemplateScan<'t> {
    fn new(file: &'t Arc<str>, text: &'t str, template: &'t TemplateDefinition) -> Self {
        Self {
            file,
            text,
            template,
            masked_ranges: Vec::new(),
            actions: Vec::new(),
            root_blocks: Vec::new(),
            open_blocks: Vec::new(),
            loop_variables: Vec::new(),
            template_variables: Vec::new(),
            problems: Vec::new(),
        }
    }

    fn read(mut self) -> TemplateReading {
        let left = self.template.delimiters.left.as_str();
        let right = self.template.delimiters.right.as_str();
        let mut cursor = 0;
        while let Some(found) = self.text[cursor..].find(left) {
            let begin = cursor + found;
            let after_left = begin + left.len();
            let Some(found_right) = self.text[after_left..].find(right) else {
                self.problems.push(
                    Diagnostic::at(
                        &self.position_at(begin),
                        format!("{} opens an action that is never closed", code_span(left)),
                    )
                    .with_label(format!("no {} after it", code_span(right))),
                );
                break;
            };
            let finish = after_left + found_right;
            let end = finish + right.len();
            self.action(begin, after_left..finish, end);
            cursor = end;
        }
        self.report_left_open();
        self.problems.sort_by_key(|problem| {
            problem
                .position
                .as_ref()
                .map(|position| (position.line, position.column))
        });
        TemplateReading {
            masked_text: self.masked_text(),
            actions: self.actions,
            blocks: self.root_blocks,
            loop_variables: self.loop_variables,
            template_variables: self.template_variables,
            problems: self.problems,
        }
    }

    /// Reads the action from `begin` to `end`, whose body is written in `inside`.
    fn action(&mut self, begin: usize, inside: Range<usize>, end: usize) {
        let raw_body = &self.text[inside.clone()];
        let body = raw_body.trim_matches(' ');
        let body_offset = inside.start + (raw_body.len() - raw_body.trim_start_matches(' ').len());
        let action_text = self.text[begin..end].to_owned();
        let position = self.position_at(begin);
        let loop_opening = self
            .template
            .loop_syntax
            .start
            .captures(body)
            .ok()
            .flatten()
            .filter(|captures| captures.get(0).is_some_and(|whole| whole.start() == 0));
        let kind = if loop_opening.is_some() {
            Some(DirectiveKind::LoopOpening)
        } else {
            self.word_kind(body)
        };
        self.read_template_variables(body, body_offset);
        self.actions.push(TemplateAction {
            position: position.clone(),
            text: action_text.clone(),
            directive: kind.is_some(),
        });
        let mask = if kind.is_some() { ' ' } else { '_' };
        self.masked_ranges.push((begin..end, mask));
        match kind {
            Some(DirectiveKind::LoopOpening) => {
                let captures = loop_opening.expect("a loop's opening action matches its start");
                let group = captures.name("variables");
                let binding = group.map(|variables| variables.range());
                let variables = group
                    .map(|variables| variables.as_str().split(", ").map(str::to_owned).collect())
                    .unwrap_or_default();
                self.read_loop_variables(body, body_offset, binding);
                self.open_blocks.push(OpenBlock {
                    block: TemplateBlock::Loop(TemplateLoop {
                        opening: position,
                        variables,
                        blocks: Vec::new(),
                        closing: None,
                    }),
                    opening_text: action_text,
                    else_text: None,
                });
            }
            Some(DirectiveKind::ConditionalOpening) => {
                self.read_loop_variables(body, body_offset, None);
                self.open_blocks.push(OpenBlock {
                    block: TemplateBlock::Conditional(TemplateConditional {
                        branches: vec![TemplateBranch {
                            opening: position,
                            blocks: Vec::new(),
                        }],
                        closing: None,
                    }),
                    opening_text: action_text,
                    else_text: None,
                });
            }
            Some(branch_kind @ (DirectiveKind::ElseIf | DirectiveKind::Else)) => {
                self.read_loop_variables(body, body_offset, None);
                self.branch(branch_kind, position, &action_text);
            }
            Some(DirectiveKind::Closing) => self.close(position, &action_text, body),
            None => self.read_loop_variables(body, body_offset, None),
        }
    }

    /// The directive a body starting with one of the template's words is, the
    /// longest word that fits read.
    fn word_kind(&self, body: &str) -> Option<DirectiveKind> {
        let words = &self.template.conditional;
        let candidates = [
            (words.if_word.as_str(), DirectiveKind::ConditionalOpening),
            (words.else_if_word.as_str(), DirectiveKind::ElseIf),
            (words.else_word.as_str(), DirectiveKind::Else),
            (words.end_word.as_str(), DirectiveKind::Closing),
            (
                self.template.loop_syntax.end_word.as_str(),
                DirectiveKind::Closing,
            ),
        ];
        let mut longest: Option<(usize, DirectiveKind)> = None;
        for (word, kind) in candidates {
            if starts_with_word(body, word) && longest.is_none_or(|(length, _)| word.len() > length)
            {
                longest = Some((word.len(), kind));
            }
        }
        longest.map(|(_, kind)| kind)
    }

    fn branch(&mut self, kind: DirectiveKind, position: Position, action_text: &str) {
        let else_word = self.template.conditional.else_word.clone();
        let quoted = code_span(action_text);
        let Some(open) = self
            .open_blocks
            .last_mut()
            .filter(|open| matches!(open.block, TemplateBlock::Conditional(_)))
        else {
            self.problems.push(Diagnostic::at(
                &position,
                format!("{quoted} belongs to no conditional"),
            ));
            return;
        };
        if let Some(else_text) = &open.else_text {
            let message = if kind == DirectiveKind::ElseIf {
                format!(
                    "{quoted} comes after the conditional's {}",
                    code_span(else_text)
                )
            } else {
                format!(
                    "{quoted} is the conditional's second {}",
                    code_span(&else_word)
                )
            };
            self.problems.push(Diagnostic::at(&position, message));
            return;
        }
        if let TemplateBlock::Conditional(conditional) = &mut open.block {
            conditional.branches.push(TemplateBranch {
                opening: position,
                blocks: Vec::new(),
            });
        }
        if kind == DirectiveKind::Else {
            open.else_text = Some(action_text.to_owned());
        }
    }

    /// Closes the innermost open block when `body` starts with that block's own
    /// end word; any other closing word closes nothing.
    fn close(&mut self, position: Position, action_text: &str, body: &str) {
        let Some(innermost) = self.open_blocks.last() else {
            self.problems.push(Diagnostic::at(
                &position,
                format!("{} closes no conditional or loop", code_span(action_text)),
            ));
            return;
        };
        let own_end_word = match innermost.block {
            TemplateBlock::Conditional(_) => &self.template.conditional.end_word,
            TemplateBlock::Loop(_) => &self.template.loop_syntax.end_word,
        };
        if !starts_with_word(body, own_end_word) {
            self.problems.push(Diagnostic::at(
                &position,
                format!(
                    "{} does not close {}",
                    code_span(action_text),
                    code_span(&innermost.opening_text)
                ),
            ));
            return;
        }
        let mut open = self.open_blocks.pop().expect("the innermost block is open");
        match &mut open.block {
            TemplateBlock::Conditional(conditional) => conditional.closing = Some(position),
            TemplateBlock::Loop(loop_block) => loop_block.closing = Some(position),
        }
        self.attach(open.block);
    }

    /// Places a finished block in the body of the block around it, or at the top.
    fn attach(&mut self, block: TemplateBlock) {
        match self.open_blocks.last_mut().map(|open| &mut open.block) {
            Some(TemplateBlock::Conditional(conditional)) => conditional
                .branches
                .last_mut()
                .expect("a conditional has its opening branch")
                .blocks
                .push(block),
            Some(TemplateBlock::Loop(loop_block)) => loop_block.blocks.push(block),
            None => self.root_blocks.push(block),
        }
    }

    /// Reports each block left open, the outermost first, and places it as written.
    fn report_left_open(&mut self) {
        let left = &self.template.delimiters.left;
        let right = &self.template.delimiters.right;
        for open in &self.open_blocks {
            let (opening, end_word) = match &open.block {
                TemplateBlock::Conditional(conditional) => (
                    &conditional.branches[0].opening,
                    &self.template.conditional.end_word,
                ),
                TemplateBlock::Loop(loop_block) => {
                    (&loop_block.opening, &self.template.loop_syntax.end_word)
                }
            };
            let closing = format!("{left} {end_word} {right}");
            self.problems.push(
                Diagnostic::at(
                    opening,
                    format!("{} is never closed", code_span(&open.opening_text)),
                )
                .with_help(Some(format!("close it with {}", code_span(&closing)))),
            );
        }
        while let Some(open) = self.open_blocks.pop() {
            self.attach(open.block);
        }
    }

    /// Reads each loop variable in `body`, which starts at `body_offset`, but the
    /// ones in `binding`, the loop variables an opening action binds.
    fn read_loop_variables(
        &mut self,
        body: &str,
        body_offset: usize,
        binding: Option<Range<usize>>,
    ) {
        let found: Vec<(usize, String)> = self
            .template
            .loop_syntax
            .variable
            .find_iter(body)
            .filter_map(Result::ok)
            .filter(|found| !found.as_str().is_empty())
            .filter(|found| {
                binding
                    .as_ref()
                    .is_none_or(|binding| !binding.contains(&found.start()))
            })
            .map(|found| (found.start(), found.as_str().to_owned()))
            .collect();
        for (start, name) in found {
            let position = self.position_at(body_offset + start);
            let bound_by = self.binding_loop(&name);
            if bound_by.is_none() {
                self.problems.push(Diagnostic::at(
                    &position,
                    format!("{} is bound by no loop around it", code_span(&name)),
                ));
            }
            self.loop_variables.push(TemplateLoopVariable {
                position,
                name,
                bound_by,
            });
        }
    }

    /// Reads each template variable in `body`, which starts at `body_offset`,
    /// but where a loop variable's match overlaps it.
    fn read_template_variables(&mut self, body: &str, body_offset: usize) {
        let loop_spans: Vec<Range<usize>> = self
            .template
            .loop_syntax
            .variable
            .find_iter(body)
            .filter_map(Result::ok)
            .filter(|found| !found.as_str().is_empty())
            .map(|found| found.range())
            .collect();
        let found: Vec<(usize, String)> = self
            .template
            .template_variable
            .find_iter(body)
            .filter_map(Result::ok)
            .filter(|found| !found.as_str().is_empty())
            .filter(|found| {
                !loop_spans
                    .iter()
                    .any(|span| span.start < found.end() && found.start() < span.end)
            })
            .map(|found| (found.start(), found.as_str().to_owned()))
            .collect();
        for (start, name) in found {
            let position = self.position_at(body_offset + start);
            self.template_variables
                .push(TemplateVariable { position, name });
        }
    }

    /// The opening of the innermost open loop binding `name`.
    fn binding_loop(&self, name: &str) -> Option<Position> {
        self.open_blocks
            .iter()
            .rev()
            .find_map(|open| match &open.block {
                TemplateBlock::Loop(loop_block)
                    if loop_block.variables.iter().any(|variable| variable == name) =>
                {
                    Some(loop_block.opening.clone())
                }
                _ => None,
            })
    }

    fn position_at(&self, offset: usize) -> Position {
        let before = &self.text[..offset];
        let line_start = before.rfind('\n').map_or(0, |newline| newline + 1);
        Position::new(
            self.file,
            before.matches('\n').count() + 1,
            before[line_start..].chars().count() + 1,
        )
    }

    fn masked_text(&self) -> String {
        self.text
            .char_indices()
            .map(|(offset, character)| {
                if character == '\n' {
                    return character;
                }
                self.masked_ranges
                    .iter()
                    .find(|(range, _)| range.contains(&offset))
                    .map_or(character, |(_, mask)| *mask)
            })
            .collect()
    }
}
