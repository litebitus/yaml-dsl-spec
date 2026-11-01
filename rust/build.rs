//! Builds the standard's schemas into the crate: each
//! `<family>-v<major>.<minor>.schema.json` of the repo's `schemas/` folder in a
//! checkout, else of the copies a published crate carries.

use std::env;
use std::fs;
use std::path::{Path, PathBuf};

#[path = "schema_versions.rs"]
mod schema_versions;

/// Each schema family, by its file stem, and the names its table and version list
/// take in the crate.
const FAMILIES: [(&str, &str, &str); 2] = [
    ("yaml-dsl", "YAML_DSL_SCHEMAS", "YAML_DSL_VERSIONS"),
    (
        "x-yaml-dsl-functions",
        "FUNCTION_TABLE_SCHEMAS",
        "FUNCTION_TABLE_VERSIONS",
    ),
];

fn main() {
    let out_directory = PathBuf::from(env::var("OUT_DIR").expect("cargo sets OUT_DIR"));
    let checkout = Path::new("../schemas");
    let folder = if checkout.is_dir() {
        checkout
    } else {
        Path::new("schemas")
    };
    println!("cargo::rerun-if-changed={}", folder.display());
    println!("cargo::rerun-if-changed=schema_versions.rs");
    let file_names: Vec<String> = fs::read_dir(folder)
        .unwrap_or_else(|error| panic!("reading {}: {error}", folder.display()))
        .filter_map(|entry| entry.ok()?.file_name().into_string().ok())
        .collect();
    let mut generated = String::new();
    for (family, table, version_list) in FAMILIES {
        let entries = schema_versions::version_files(&file_names, family);
        generated.push_str(&format!("const {table}: &[(&str, &str, &str)] = &[\n"));
        for (version, file) in &entries {
            let source = folder.join(file);
            if let Err(error) = fs::copy(&source, out_directory.join(file)) {
                panic!("copying {}: {error}", source.display());
            }
            generated.push_str(&format!(
                "    ({version:?}, {file:?}, include_str!(concat!(env!(\"OUT_DIR\"), \"/{file}\"))),\n"
            ));
        }
        generated.push_str("];\n\n");
        let versions: Vec<String> = entries
            .iter()
            .map(|(version, _)| format!("{version:?}"))
            .collect();
        generated.push_str(&format!(
            "/// The versions the crate reads, each major before its minors.\n\
             pub const {version_list}: &[&str] = &[{}];\n\n",
            versions.join(", ")
        ));
    }
    let table_file = out_directory.join("schema_versions.rs");
    if let Err(error) = fs::write(&table_file, generated) {
        panic!("writing {}: {error}", table_file.display());
    }
}
