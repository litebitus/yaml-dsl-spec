'use strict';

/*
 * The YAML DSL standard in JavaScript: the JSON Schemas of `yaml-dsl.yml` and of a
 * DSL's function table at each of their versions, a reader that returns a
 * `yaml-dsl.yml`'s DSL definitions fully validated, and the YAML reading,
 * diagnostics and schema validation it and every reader of a DSL stand on.
 *
 * A version is a minor, such as `1.0`, or a major, such as `1`, which stands for
 * the newest minor of that major the package carries.
 */

const schemaCatalog = require('./lib/schema-catalog');
const { readDslDefinitions } = require('./lib/definition-reader');
const { Diagnostic } = require('./lib/diagnostic-report');
const { Position } = require('./lib/source-position');
const { Node, numberJson } = require('./lib/yaml-node');
const functionTable = require('./lib/function-table');
const dslDefinition = require('./lib/dsl-definition');
const jsonPath = require('./lib/json-path');
const resolved = require('./lib/resolved-value');
const schemaValidation = require('./lib/schema-validation');
const yamlReader = require('./lib/yaml-reader');

module.exports = {
  readDslDefinitions,
  yamlDslSchema: schemaCatalog.yamlDslSchema,
  functionTableSchema: schemaCatalog.functionTableSchema,
  YAML_DSL_VERSIONS: schemaCatalog.YAML_DSL_VERSIONS,
  FUNCTION_TABLE_VERSIONS: schemaCatalog.FUNCTION_TABLE_VERSIONS,
  yamlDslSchemas: schemaCatalog.yamlDslSchemas,
  functionTableSchemas: schemaCatalog.functionTableSchemas,
  FAMILIES: schemaCatalog.FAMILIES,
  Diagnostic,
  Position,
  Node,
  numberJson,
  parseCallKey: functionTable.parseCallKey,
  closestName: schemaValidation.closestName,
  yamlReader: { parseYaml: yamlReader.parseYaml },
  dslDefinition: { dslDefinitionFromJson: dslDefinition.dslDefinitionFromJson },
  jsonPath: { PathPattern: jsonPath.PathPattern, PathSet: jsonPath.PathSet },
  resolved: { Value: resolved.Value, reference: resolved.reference, call: resolved.call },
  functionTable: {
    FUNCTION_TABLE_POINTER: functionTable.FUNCTION_TABLE_POINTER,
    FunctionTable: functionTable.FunctionTable,
    MacroEntry: functionTable.MacroEntry,
  },
  schemaValidation: {
    SchemaCache: schemaValidation.SchemaCache,
    LoadedSchema: schemaValidation.LoadedSchema,
    childValue: schemaValidation.childValue,
  },
};
