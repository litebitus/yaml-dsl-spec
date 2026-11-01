'use strict';

const fs = require('fs');
const path = require('path');

const { packagedVersionFiles } = require('../schema-versions');

/** Each schema family the package carries, by its file stem. */
const FAMILIES = {
  yamlDslSchemas: 'yaml-dsl',
  functionTableSchemas: 'x-yaml-dsl-functions',
};

/** A family's schema files by version: a minor such as `1.0`, or a major such as `1`, its newest minor. */
function filesOf(family) {
  return packagedVersionFiles(family).map(([version, file]) => ({
    version,
    file: path.basename(file),
    text: fs.readFileSync(file, 'utf8'),
  }));
}

const YAML_DSL_FILES = filesOf(FAMILIES.yamlDslSchemas);
const FUNCTION_TABLE_FILES = filesOf(FAMILIES.functionTableSchemas);

/** The versions the package reads, each major before its minors. */
const YAML_DSL_VERSIONS = YAML_DSL_FILES.map(({ version }) => version);
const FUNCTION_TABLE_VERSIONS = FUNCTION_TABLE_FILES.map(({ version }) => version);

function schemasByVersion(files) {
  return Object.fromEntries(files.map(({ version, text }) => [version, JSON.parse(text)]));
}

const yamlDslSchemas = schemasByVersion(YAML_DSL_FILES);
const functionTableSchemas = schemasByVersion(FUNCTION_TABLE_FILES);
for (const schemas of [yamlDslSchemas, functionTableSchemas]) {
  const byFile = new Map();
  for (const [version, schema] of Object.entries(schemas)) {
    const key = JSON.stringify(schema);
    if (byFile.has(key)) schemas[version] = byFile.get(key);
    else byFile.set(key, schema);
  }
}

/** The JSON Schema of `yaml-dsl.yml` at `version`, or `null`. */
function yamlDslSchema(version) {
  return Object.prototype.hasOwnProperty.call(yamlDslSchemas, version) ? yamlDslSchemas[version] : null;
}

/** The JSON Schema of a DSL's function table, a document schema's `x-yaml-dsl-functions`, at `version`, or `null`. */
function functionTableSchema(version) {
  return Object.prototype.hasOwnProperty.call(functionTableSchemas, version) ? functionTableSchemas[version] : null;
}

/** The file name and text of the `yaml-dsl.yml` schema at `version`, or `null`. */
function yamlDslSchemaFile(version) {
  const found = YAML_DSL_FILES.find((entry) => entry.version === version);
  return found ? { file: found.file, text: found.text } : null;
}

module.exports = {
  FAMILIES,
  YAML_DSL_VERSIONS,
  FUNCTION_TABLE_VERSIONS,
  yamlDslSchemas,
  functionTableSchemas,
  yamlDslSchema,
  functionTableSchema,
  yamlDslSchemaFile,
};
