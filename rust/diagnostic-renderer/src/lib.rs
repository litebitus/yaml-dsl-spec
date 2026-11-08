//! A YAML DSL standard diagnostic written as a compiler writes an error, for a terminal or a log.

mod line_wrapping;
mod report_rendering;

pub use report_rendering::render_diagnostic;

/// Whether a diagnostic stops its reader or only warns.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Severity {
    Error,
    Warning,
}

/// The source a diagnostic is about.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct DiagnosticSource<'a> {
    /// The name the `-->` line shows, such as a path.
    pub name: &'a str,
    /// The text the diagnostic's position counts lines and columns in; empty shows no source lines.
    pub text: &'a str,
}

/// How a rendering is laid out.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RenderStyle {
    /// Whether the rendering is painted for a terminal.
    pub color: bool,
    /// The terminal width lines divide to; `None` for output that is not a terminal.
    pub width: Option<usize>,
}
