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

const schemaCatalog = require('./core/schema-catalog');
const { readDslDefinitions, readDefinitions } = require('./core/definition-reader');
const { Diagnostic } = require('./core/diagnostic-report');
const { Position } = require('./core/source-position');
const { Node, numberJson } = require('./core/yaml-node');
const { codeSpan, jsonPointerSteps } = require('./core/problem-wording');
const { formatLayout, parseLayout } = require('./core/format-function');
const functionTable = require('./core/function-table');
const dslDefinition = require('./core/dsl-definition');
const jsonPath = require('./core/json-path');
const resolved = require('./core/resolved-value');
const schemaValidation = require('./core/schema/schema-validation');
const { SchemaCache } = require('./core/schema/schema-cache');
const { SchemaFolderStore } = require('./core/schema/schema-folder-store');
const httpReading = require('./core/schema/resolution/http-reading');
const { HostLogin, netrcLogin } = require('./core/schema/resolution/machine-credentials');
const schemaLocations = require('./core/schema/resolution/schema-locations');
const schemaUrlReading = require('./core/schema/resolution/schema-url-reading');
const templateReader = require('./core/template-reader');
const yamlReader = require('./core/yaml-reader');

module.exports = {
  readDslDefinitions,
  readDefinitions,
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
  codeSpan,
  jsonPointerSteps,
  formatLayout,
  parseLayout,
  yamlReader: { parseYaml: yamlReader.parseYaml },
  dslDefinition: { dslDefinitionFromJson: dslDefinition.dslDefinitionFromJson },
  templateDefinition: { templateDefinitionFromJson: dslDefinition.templateDefinitionFromJson },
  templateReader: { readTemplate: templateReader.readTemplate },
  TemplateReading: templateReader.TemplateReading,
  TemplateConditional: templateReader.TemplateConditional,
  TemplateLoop: templateReader.TemplateLoop,
  jsonPath: { PathPattern: jsonPath.PathPattern, PathSet: jsonPath.PathSet },
  resolved: {
    Value: resolved.Value,
    reference: resolved.reference,
    call: resolved.call,
    loop: resolved.loop,
  },
  functionTable: {
    FUNCTION_TABLE_POINTER: functionTable.FUNCTION_TABLE_POINTER,
    FunctionTable: functionTable.FunctionTable,
    MacroEntry: functionTable.MacroEntry,
  },
  schemaValidation: {
    SchemaCache,
    SchemaFolderStore,
    LoadedSchema: schemaValidation.LoadedSchema,
    ValidationRules: schemaValidation.ValidationRules,
    childValue: schemaValidation.childValue,
  },
  schemaResolution: {
    documentModeline: schemaLocations.documentModeline,
    HostLogin,
    netrcLogin,
    httpGet: httpReading.httpGet,
    httpConditionalHeaders: httpReading.httpConditionalHeaders,
    answerOf: schemaUrlReading.answerOf,
    refusedAnswer: schemaUrlReading.refusedAnswer,
    failedAnswer: schemaUrlReading.failedAnswer,
    missingAnswer: schemaUrlReading.missingAnswer,
  },
};
