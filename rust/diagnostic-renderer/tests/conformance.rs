use std::fs;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use serde_json::Value;
use yaml_dsl_spec::{Diagnostic, Position};
use yaml_dsl_spec_diagnostic_renderer::{
    DiagnosticSource, RenderStyle, Severity, render_diagnostic,
};

const POSITION_FILE: &str = "mock-position-file.yml";

fn cases_folder() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../conformance/diagnostic-rendering")
}

fn read_json(path: &Path) -> Value {
    let text = fs::read_to_string(path).unwrap_or_else(|error| panic!("{path:?}: {error}"));
    serde_json::from_str(&text).unwrap_or_else(|error| panic!("{path:?}: {error}"))
}

fn note(fields: &Value, name: &str) -> Option<String> {
    fields[name].as_str().map(str::to_owned)
}

fn case_diagnostic(fields: &Value) -> Diagnostic {
    let message = fields["message"].as_str().unwrap();
    let mut diagnostic = match (fields["line"].as_u64(), fields["column"].as_u64()) {
        (Some(line), Some(column)) => {
            let file: Arc<str> = Arc::from(POSITION_FILE);
            Diagnostic::at(
                &Position::new(&file, line as usize, column as usize),
                message,
            )
        }
        _ => Diagnostic::unplaced(message),
    };
    if let Some(label) = note(fields, "label") {
        diagnostic = diagnostic.with_label(label);
    }
    if let Some(rule) = note(fields, "rule") {
        diagnostic = diagnostic.with_rule(rule);
    }
    diagnostic
        .with_schema(note(fields, "schema"))
        .with_help(note(fields, "help"))
}

fn rendered_case(folder: &Path) -> String {
    let diagnostic = case_diagnostic(&read_json(&folder.join("diagnostic.json")));
    let rendering = read_json(&folder.join("rendering.json"));
    let severity = match rendering["severity"].as_str() {
        Some("warning") => Severity::Warning,
        _ => Severity::Error,
    };
    let source_text = fs::read_to_string(folder.join("source.yml")).unwrap_or_default();
    let source = DiagnosticSource {
        name: rendering["source_name"].as_str().unwrap(),
        text: &source_text,
    };
    let style = RenderStyle {
        color: rendering["color"].as_bool().unwrap(),
        width: rendering["width"].as_u64().map(|width| width as usize),
    };
    render_diagnostic(&diagnostic, severity, source, style)
}

#[test]
fn every_diagnostic_rendering_case_renders_its_expected_text() {
    let Ok(entries) = fs::read_dir(cases_folder()) else {
        return;
    };
    let mut folders: Vec<PathBuf> = entries
        .filter_map(|entry| Some(entry.ok()?.path()))
        .filter(|path| path.join("diagnostic.json").is_file())
        .collect();
    folders.sort();
    assert!(!folders.is_empty(), "no diagnostic rendering cases");
    let mut mismatches = Vec::new();
    for folder in folders {
        let expected = read_json(&folder.join("expected.json"));
        let expected_text = expected["lines"]
            .as_array()
            .unwrap()
            .iter()
            .map(|line| line.as_str().unwrap())
            .collect::<Vec<_>>()
            .join("\n");
        let actual_text = rendered_case(&folder);
        if actual_text != expected_text {
            let actual_lines: Vec<&str> = actual_text.split('\n').collect();
            mismatches.push(format!(
                "{}: {}",
                folder.file_name().unwrap().to_string_lossy(),
                serde_json::to_string_pretty(&serde_json::json!({ "lines": actual_lines }))
                    .unwrap()
            ));
        }
    }
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}

#[test]
fn every_subset_corpus_text_renders_its_expected_text_at_each_style() {
    let Ok(corpus_text) = fs::read_to_string(cases_folder().join("subset-corpus.json")) else {
        return;
    };
    let corpus: Value = serde_json::from_str(&corpus_text).unwrap();
    let source_name = corpus["source_name"].as_str().unwrap();
    let styles: Vec<RenderStyle> = corpus["styles"]
        .as_array()
        .unwrap()
        .iter()
        .map(|style| RenderStyle {
            color: style["color"].as_bool().unwrap(),
            width: style["width"].as_u64().map(|width| width as usize),
        })
        .collect();
    let file: Arc<str> = Arc::from(POSITION_FILE);
    let corpus_cases = corpus["cases"].as_array().unwrap();
    assert!(!corpus_cases.is_empty(), "no subset corpus cases");
    let mut mismatches = Vec::new();
    for corpus_case in corpus_cases {
        let text = corpus_case["text"].as_str().unwrap();
        let source_text = format!("{text}\n");
        let diagnostic = Diagnostic::at(&Position::new(&file, 1, 1), text);
        for rendering in corpus_case["renderings"].as_array().unwrap() {
            let style = styles[rendering["style"].as_u64().unwrap() as usize];
            let source = DiagnosticSource {
                name: source_name,
                text: &source_text,
            };
            let actual_text = render_diagnostic(&diagnostic, Severity::Error, source, style);
            if actual_text != rendering["expected"].as_str().unwrap() {
                mismatches.push(format!("{text:?} at {style:?}: {actual_text:?}"));
            }
        }
    }
    assert!(mismatches.is_empty(), "{}", mismatches.join("\n"));
}
