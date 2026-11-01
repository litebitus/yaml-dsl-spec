//! Reads a `yaml-dsl.yml`: checks its `version`, validates it against that
//! version's schema, checks what the schema cannot state, and returns each DSL's
//! definition, or every problem at the field it is about.

use std::path::Path;
use std::sync::Arc;

use fancy_regex::Regex;

use crate::definition::DslDefinition;
use crate::diagnostic::Diagnostic;
use crate::json_path::PathPattern;
use crate::node::{Node, NodeKind};
use crate::position::Position;
use crate::resolved::Value;
use crate::schema::{SchemaCache, closest_name};
use crate::validation_rules::ValidationRules;
use crate::{YAML_DSL_SCHEMAS, YAML_DSL_VERSIONS, schema_at, yaml_reader};

/// The text in a `common_layer_files` name that stands for a concern.
const CONCERN_TOKEN: &str = "{concern}";

/// A DSL of a `yaml-dsl.yml`: its validated definition, and its block as written,
/// for a caller to place its own errors in it.
#[derive(Clone, Debug)]
pub struct DeclaredDsl {
    pub definition: DslDefinition,
    pub block: Node,
}

/// Each DSL of the `yaml-dsl.yml` at `file`, whose text is `text`, validated, or
/// every problem found, each at the field it is about.
pub fn read_dsl_definitions(file: &Path, text: &str) -> Result<Vec<DeclaredDsl>, Vec<Diagnostic>> {
    let file: Arc<str> = Arc::from(file.to_string_lossy().as_ref());
    let root = yaml_reader::parse(&file, text, &[]).map_err(|problem| vec![problem])?;
    let (schema_file, schema_text) = version_schema(&root, &file)?;
    let schema = SchemaCache::default()
        .load(schema_file, schema_text)
        .map_err(|message| vec![Diagnostic::unplaced(message)])?;
    let problems = schema.validate(&Value::from_node(&root), &ValidationRules::default());
    if !problems.is_empty() {
        return Err(problems);
    }
    let blocks = root.get("dsls").and_then(Node::items).unwrap_or_default();
    let mut problems = Vec::new();
    let mut declared = Vec::new();
    for (index, block) in blocks.iter().enumerate() {
        problems.extend(repeated_id(block, &blocks[..index]));
        let block_problems = BlockChecks::new(block).problems();
        if !block_problems.is_empty() {
            problems.extend(block_problems);
            continue;
        }
        match DslDefinition::from_json(&block.to_json()) {
            Ok(definition) => declared.push(DeclaredDsl {
                definition,
                block: block.clone(),
            }),
            Err(message) => problems.push(Diagnostic::at(&block.position, message)),
        }
    }
    if problems.is_empty() {
        Ok(declared)
    } else {
        Err(problems)
    }
}

/// The file name and text of the schema the file's `version` names.
fn version_schema(
    root: &Node,
    file: &Arc<str>,
) -> Result<(&'static str, &'static str), Vec<Diagnostic>> {
    let versions: Vec<String> = YAML_DSL_VERSIONS
        .iter()
        .map(|version| (*version).to_owned())
        .collect();
    let listed = versions
        .iter()
        .map(|version| format!("`\"{version}\"`"))
        .collect::<Vec<_>>()
        .join(", ");
    let Some(version_node) = root.get("version") else {
        let position = Position::file_start(file);
        return Err(vec![
            Diagnostic::at(&position, "field `version` is required")
                .with_label(format!("`version` is one of {listed}")),
        ]);
    };
    let version = version_node.as_str().unwrap_or_default();
    schema_at(YAML_DSL_SCHEMAS, version).ok_or_else(|| {
        let shown = version_node.as_str().map_or_else(
            || format!("`{}`", version_node.to_json()),
            |text| format!("`{text}`"),
        );
        vec![
            Diagnostic::at(
                &version_node.position,
                format!("{shown} is not a version this reader reads"),
            )
            .with_label(format!("`version` is one of {listed}"))
            .with_help(
                closest_name(version, &versions)
                    .map(|close| format!("did you mean `\"{close}\"`?")),
            ),
        ]
    })
}

/// A second DSL of the file with the same `id`.
fn repeated_id(block: &Node, earlier: &[Node]) -> Option<Diagnostic> {
    let id_node = block.get("id")?;
    let id = id_node.as_str()?;
    let repeated = earlier
        .iter()
        .any(|other| other.get("id").and_then(Node::as_str) == Some(id));
    repeated.then(|| {
        Diagnostic::at(
            &id_node.position,
            format!("DSL `{id}` is declared more than once"),
        )
        .with_label("an earlier DSL of this file has this `id`")
    })
}

/// The checks of one DSL block that its schema cannot state. The block has
/// already passed the schema, so every field has its shape.
struct BlockChecks<'b> {
    block: &'b Node,
    problems: Vec<Diagnostic>,
}

impl<'b> BlockChecks<'b> {
    fn new(block: &'b Node) -> Self {
        Self {
            block,
            problems: Vec::new(),
        }
    }

    fn problems(mut self) -> Vec<Diagnostic> {
        self.patterns();
        self.paths();
        self.repeated_path_entries();
        self.key_sort_orders();
        self.common_layer_files();
        self.scope_names();
        self.locals_and_loops();
        self.tag_keys();
        self.problems
    }

    fn report(&mut self, node: &Node, message: impl Into<String>, label: impl Into<String>) {
        self.problems
            .push(Diagnostic::at(&node.position, message).with_label(label));
    }

    /// Each regular expression compiles, and holds the named groups its rule reads.
    fn patterns(&mut self) {
        if let Some(pattern) = at(self.block, &["placeholder", "pattern"]) {
            self.pattern(pattern, &[("body", "a placeholder's body")]);
        }
        for rule in list(self.block, &["references"]) {
            let Some(pattern) = rule.get("pattern") else {
                continue;
            };
            let mut groups = Vec::new();
            if let Some(name) = rule.get("name_group").and_then(Node::as_str) {
                groups.push((name, "the name, by `name_group`"));
            }
            if let Some(scope) = rule.get("scope_group").and_then(Node::as_str) {
                groups.push((scope, "the scope, by `scope_group`"));
            }
            self.pattern(pattern, &groups);
        }
        for rule in list(self.block, &["validator", "child_values_from_parent_key"]) {
            if let Some(pattern) = rule.get("pattern") {
                self.pattern(
                    pattern,
                    &[("field", "the child field"), ("value", "its value")],
                );
            }
        }
    }

    fn pattern(&mut self, node: &Node, groups: &[(&str, &str)]) {
        let Some(text) = node.as_str() else {
            return;
        };
        let compiled = match Regex::new(text) {
            Ok(compiled) => compiled,
            Err(_) => {
                self.report(
                    node,
                    "the pattern is not a regular expression",
                    "a regular expression the reader compiles",
                );
                return;
            }
        };
        let names: Vec<&str> = compiled.capture_names().flatten().collect();
        for (group, holds) in groups {
            if !names.contains(group) {
                self.report(
                    node,
                    format!("the pattern has no `(?<{group}>...)` group"),
                    format!("the group holds {holds}"),
                );
            }
        }
    }

    /// Each JSONPath is one of the subset the standard reads.
    fn paths(&mut self) {
        let mut nodes: Vec<&Node> = Vec::new();
        for rule in list(self.block, &["declarations"]) {
            nodes.extend(rule.get("path"));
        }
        for scope in map_values(self.block, &["scopes"]) {
            nodes.extend(list(scope, &["regions"]));
        }
        for field in [
            &["placeholder", "unscanned_paths"][..],
            &["function", "calls_not_allowed_at"],
            &["key_sort_orders"],
        ] {
            for entry in list(self.block, field) {
                nodes.extend(entry.get("path"));
            }
        }
        for include in list(self.block, &["layers", "overlay_includes"]) {
            nodes.extend(list(include, &["key_paths"]));
            nodes.extend(include.get("location_path"));
        }
        for node in nodes {
            let Some(text) = node.as_str() else {
                continue;
            };
            if let Err(error) = PathPattern::parse(text, &[]) {
                self.report(
                    node,
                    error.message,
                    "a path of the standard's JSONPath subset",
                );
            }
        }
    }

    /// No list of path entries holds one path twice.
    fn repeated_path_entries(&mut self) {
        for field in [
            &["placeholder", "unscanned_paths"][..],
            &["function", "calls_not_allowed_at"],
            &["key_sort_orders"],
        ] {
            let entries = list(self.block, field);
            for (index, entry) in entries.iter().enumerate() {
                let Some(path) = entry.get("path") else {
                    continue;
                };
                let repeated = entries[..index]
                    .iter()
                    .any(|earlier| earlier.get("path").and_then(Node::as_str) == path.as_str());
                if repeated {
                    self.report(
                        path,
                        format!(
                            "path `{}` is listed more than once",
                            path.as_str().unwrap_or("")
                        ),
                        format!("an earlier entry of `{}` has this path", field.join(".")),
                    );
                }
            }
        }
    }

    /// An order's first and last keys are distinct, and alphabetical order names none.
    fn key_sort_orders(&mut self) {
        for entry in list(self.block, &["key_sort_orders"]) {
            let alphabetical = entry.get("order").and_then(Node::as_str) == Some("alphabetical");
            for field in ["first_keys", "last_keys"] {
                let keys = list(entry, &[field]);
                if alphabetical && !keys.is_empty() {
                    self.report(
                        entry.get(field).unwrap_or(entry),
                        format!("`{field}` is not read with alphabetical order"),
                        "`[]` under alphabetical order",
                    );
                }
                for (index, key) in keys.iter().enumerate() {
                    if keys[..index]
                        .iter()
                        .any(|earlier| earlier.as_str() == key.as_str())
                    {
                        self.report(
                            key,
                            format!("`{field}` lists a key more than once"),
                            "an earlier item is this key",
                        );
                    }
                }
            }
            let last = list(entry, &["last_keys"]);
            for key in list(entry, &["first_keys"]) {
                if last.iter().any(|other| other.as_str() == key.as_str()) {
                    self.report(
                        key,
                        "a key is in both `first_keys` and `last_keys`",
                        "`last_keys` lists it too",
                    );
                }
            }
        }
    }

    /// The names a layer file's common layer may take: each a file name, each
    /// once, `{concern}` at most once in a name, one without it, and none with it
    /// where only an ancestor reaches a concern below its overlays.
    fn common_layer_files(&mut self) {
        let parent_discovery = at(self.block, &["layers", "common_layer_discovery"])
            .and_then(Node::as_str)
            == Some("parent");
        for files in map_values(self.block, &["layers", "common_layer_files"]) {
            let names = files.items().unwrap_or_default();
            for (index, name) in names.iter().enumerate() {
                let text = name.as_str().unwrap_or_default();
                if text.is_empty() || text == "." || text == ".." || text.contains(['/', '\\']) {
                    self.report(
                        name,
                        format!("`{text}` is not a file name"),
                        "a file name, with no folder",
                    );
                }
                if names[..index]
                    .iter()
                    .any(|earlier| earlier.as_str() == name.as_str())
                {
                    self.report(
                        name,
                        format!("`{text}` is listed more than once"),
                        "an earlier item is this name",
                    );
                }
                if text.matches(CONCERN_TOKEN).count() > 1 {
                    self.report(
                        name,
                        format!("`{text}` holds `{CONCERN_TOKEN}` more than once"),
                        format!("a name holds `{CONCERN_TOKEN}` at most once"),
                    );
                }
                if parent_discovery && text.contains(CONCERN_TOKEN) {
                    self.report(
                        name,
                        format!("`{text}` names a concern's common layer"),
                        "only `common_layer_discovery: ancestor` reaches a concern below its overlays",
                    );
                }
            }
            let all_concern = !names.is_empty()
                && names
                    .iter()
                    .all(|name| name.as_str().unwrap_or_default().contains(CONCERN_TOKEN));
            if all_concern {
                self.report(
                    files,
                    format!("every name holds `{CONCERN_TOKEN}`"),
                    "a name without it is the common layer of a concern above its overlays",
                );
            }
        }
    }

    /// Every scope a rule names is declared, and a reference names its scope by a
    /// group exactly when the scope is named by a parent key.
    fn scope_names(&mut self) {
        let scopes = self.scopes();
        let declared: Vec<String> = scopes.iter().map(|(name, _)| name.clone()).collect();
        let mut named: Vec<&Node> = Vec::new();
        for rule in list(self.block, &["declarations"])
            .iter()
            .chain(list(self.block, &["references"]))
        {
            named.extend(rule.get("scope_name"));
        }
        named.extend(at(self.block, &["locals", "scope_name"]));
        named.extend(at(self.block, &["loops", "scope_name"]));
        for node in named {
            let name = node.as_str().unwrap_or_default();
            if !declared.iter().any(|scope| scope == name) {
                self.problems.push(
                    Diagnostic::at(&node.position, format!("scope `{name}` is not declared"))
                        .with_label("`scopes` declares no scope of this name")
                        .with_help(
                            closest_name(name, &declared)
                                .map(|close| format!("did you mean `{close}`?")),
                        ),
                );
            }
        }
        for rule in list(self.block, &["references"]) {
            let Some(name) = rule.get("scope_name").and_then(Node::as_str) else {
                continue;
            };
            let Some((_, named_by_parent_key)) = scopes.iter().find(|(scope, _)| scope == name)
            else {
                continue;
            };
            let group = rule.get("scope_group");
            let has_group = group.and_then(Node::as_str).is_some();
            if *named_by_parent_key && !has_group {
                self.report(
                    group.unwrap_or(rule),
                    "`scope_group` is required",
                    format!("scope `{name}` is named by a parent key"),
                );
            }
            if !*named_by_parent_key && has_group {
                self.report(
                    group.unwrap_or(rule),
                    "`scope_group` must be null",
                    format!("scope `{name}` is not named by a parent key"),
                );
            }
        }
    }

    /// Locals have a rule declaring their names from keys and one reading them;
    /// loop variables have a rule reading them.
    fn locals_and_loops(&mut self) {
        if let Some(scope) = at(self.block, &["locals", "scope_name"]) {
            let name = scope.as_str().unwrap_or_default();
            let declared_by_key = list(self.block, &["declarations"]).iter().any(|rule| {
                rule.get("scope_name").and_then(Node::as_str) == Some(name)
                    && rule.get("name_source").and_then(Node::as_str) == Some("key")
                    && !is_true(rule.get("declares_every_name"))
            });
            if !declared_by_key {
                self.report(
                    scope,
                    format!("no declaration rule reads `{name}` names from keys"),
                    "a local is declared by its key",
                );
            }
            self.read_by_reference_rule(scope, name, "a local");
        }
        if let Some(scope) = at(self.block, &["loops", "scope_name"]) {
            let name = scope.as_str().unwrap_or_default();
            self.read_by_reference_rule(scope, name, "a loop variable");
        }
    }

    fn read_by_reference_rule(&mut self, scope: &Node, name: &str, what: &str) {
        let read = list(self.block, &["references"])
            .iter()
            .any(|rule| rule.get("scope_name").and_then(Node::as_str) == Some(name));
        if !read {
            self.report(
                scope,
                format!("no reference rule reads `{name}`"),
                format!("{what} is read by a reference rule of its scope"),
            );
        }
    }

    /// No tag, and no key, is listed twice.
    fn tag_keys(&mut self) {
        let entries = list(self.block, &["tag_keys"]);
        for (index, entry) in entries.iter().enumerate() {
            for field in ["tag", "key"] {
                let Some(node) = entry.get(field) else {
                    continue;
                };
                let repeated = entries[..index]
                    .iter()
                    .any(|earlier| earlier.get(field).and_then(Node::as_str) == node.as_str());
                if repeated {
                    self.report(
                        node,
                        format!(
                            "{field} `{}` is listed more than once",
                            node.as_str().unwrap_or("")
                        ),
                        format!("an earlier entry of `tag_keys` has this {field}"),
                    );
                }
            }
        }
    }

    /// Each declared scope's name, and whether it is named by a parent key.
    fn scopes(&self) -> Vec<(String, bool)> {
        self.block
            .get("scopes")
            .and_then(Node::entries)
            .unwrap_or_default()
            .iter()
            .map(|entry| {
                (
                    entry.key.clone(),
                    is_true(entry.value.get("named_by_parent_key")),
                )
            })
            .collect()
    }
}

/// Whether a field is written `true`.
fn is_true(node: Option<&Node>) -> bool {
    node.is_some_and(|node| matches!(node.kind, NodeKind::Bool(true)))
}

/// The node at `steps` of map keys below `node`.
fn at<'n>(node: &'n Node, steps: &[&str]) -> Option<&'n Node> {
    steps
        .iter()
        .try_fold(node, |current, step| current.get(step))
}

/// The items of the list at `steps`, none where there is no list.
fn list<'n>(node: &'n Node, steps: &[&str]) -> &'n [Node] {
    at(node, steps).and_then(Node::items).unwrap_or_default()
}

/// The values of the map at `steps`, none where there is no map.
fn map_values<'n>(node: &'n Node, steps: &[&str]) -> Vec<&'n Node> {
    at(node, steps)
        .and_then(Node::entries)
        .unwrap_or_default()
        .iter()
        .map(|entry| &entry.value)
        .collect()
}
