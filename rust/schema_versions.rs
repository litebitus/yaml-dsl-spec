//! Which schema file each version of a family is: a minor is its own
//! `<family>-v<major>.<minor>.schema.json`, and a major is its newest minor's.
//! Shared by the build script and the crate's tests.

/// Each version of `family` among `file_names`, each major before its minors, with
/// the file it is.
pub fn version_files<'n>(file_names: &'n [String], family: &str) -> Vec<(String, &'n String)> {
    let mut minors: Vec<((u32, u32), &String)> = file_names
        .iter()
        .filter_map(|name| Some((minor_version_of(name, family)?, name)))
        .collect();
    minors.sort();
    let mut entries = Vec::new();
    for (index, ((major, minor), file)) in minors.iter().enumerate() {
        let first_of_major = index == 0 || minors[index - 1].0.0 != *major;
        if first_of_major {
            let newest = minors
                .iter()
                .rev()
                .find(|((other_major, _), _)| other_major == major)
                .map(|(_, newest)| *newest)
                .expect("a major has a minor");
            entries.push((major.to_string(), newest));
        }
        entries.push((format!("{major}.{minor}"), *file));
    }
    entries
}

/// The `<major>.<minor>` of a `<family>-v<major>.<minor>.schema.json` file name.
fn minor_version_of(file_name: &str, family: &str) -> Option<(u32, u32)> {
    let version = file_name
        .strip_prefix(family)?
        .strip_prefix("-v")?
        .strip_suffix(".schema.json")?;
    let (major, minor) = version.split_once('.')?;
    Some((major.parse().ok()?, minor.parse().ok()?))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_major_is_its_newest_minor_and_other_families_are_left_out() {
        let names: Vec<String> = [
            "mock-v2.0.schema.json",
            "mock-v1.10.schema.json",
            "mock-v1.2.schema.json",
            "mock-other-v1.0.schema.json",
            "mock-v1.schema.json",
            "README.md",
        ]
        .iter()
        .map(|name| (*name).to_owned())
        .collect();
        let files: Vec<(String, String)> = version_files(&names, "mock")
            .into_iter()
            .map(|(version, file)| (version, file.clone()))
            .collect();
        let expected = [
            ("1", "mock-v1.10.schema.json"),
            ("1.2", "mock-v1.2.schema.json"),
            ("1.10", "mock-v1.10.schema.json"),
            ("2", "mock-v2.0.schema.json"),
            ("2.0", "mock-v2.0.schema.json"),
        ];
        let expected: Vec<(String, String)> = expected
            .iter()
            .map(|(version, file)| ((*version).to_owned(), (*file).to_owned()))
            .collect();
        assert_eq!(files, expected);
    }
}
