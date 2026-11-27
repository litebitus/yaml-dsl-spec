'use strict';

const assert = require('node:assert');
const fs = require('node:fs');
const http = require('node:http');
const path = require('node:path');
const test = require('node:test');

const {
  formatLayout, parseLayout, readDslDefinitions, readDefinitions, yamlReader, schemaValidation, resolved, templateReader,
  TemplateConditional, Position,
} = require('..');
const { compilePattern, groupNamesOf } = require('../core/pattern-dialect');

const CONFORMANCE = path.join(__dirname, '..', '..', 'conformance');

function readJson(file) {
  return fs.existsSync(file) ? JSON.parse(fs.readFileSync(file, 'utf8')) : null;
}

/** The case folders of a category holding `input`. */
function caseFolders(category, input) {
  const folder = path.join(CONFORMANCE, category);
  return fs.readdirSync(folder)
    .map((name) => path.join(folder, name))
    .filter((caseFolder) => fs.existsSync(path.join(caseFolder, input)))
    .sort();
}

/** Every case of a category whose `expected.json` states otherwise than what it reads to. */
function mismatchesOf(category, input, reading) {
  const folders = caseFolders(category, input);
  assert.ok(folders.length > 0, `no ${category} cases`);
  return folders.flatMap((folder) => mismatchOf(category, folder, reading(folder)));
}

/** Every case of a category whose `expected.json` states otherwise than what it reads to, read in turn. */
async function asyncMismatchesOf(category, input, reading) {
  const folders = caseFolders(category, input);
  assert.ok(folders.length > 0, `no ${category} cases`);
  const mismatches = [];
  for (const folder of folders) mismatches.push(...mismatchOf(category, folder, await reading(folder)));
  return mismatches;
}

/** The case's mismatch, none where `expected.json` states what it read to. */
function mismatchOf(category, folder, actual) {
  const expected = readJson(path.join(folder, 'expected.json'));
  return JSON.stringify(actual) === JSON.stringify(expected)
    ? []
    : [`${category}/${path.basename(folder)}: ${JSON.stringify(actual, null, 2)}`];
}

function problemJson(problem) {
  const fields = {};
  if (problem.position) {
    fields.line = problem.position.line;
    fields.column = problem.position.column;
  }
  fields.message = problem.message;
  for (const note of ['label', 'rule', 'schema', 'help']) {
    if (problem.notes && problem.notes[note] !== null) fields[note] = problem.notes[note];
  }
  return fields;
}

function nodeJson(node) {
  const fields = { kind: node.kind, line: node.position.line, column: node.position.column };
  if (['bool', 'number', 'string'].includes(node.kind)) fields.value = node.value;
  if (node.kind === 'list') fields.items = node.value.map(nodeJson);
  if (node.kind === 'map') {
    fields.entries = node.value.map((entry) => ({
      key: entry.key,
      line: entry.keyPosition.line,
      column: entry.keyPosition.column,
      value: nodeJson(entry.value),
    }));
  }
  return fields;
}

function definitionsReading(folder) {
  const text = fs.readFileSync(path.join(folder, 'yaml-dsl.yml'), 'utf8');
  const read = readDefinitions('yaml-dsl.yml', text);
  const { templates: _templates, ...withoutTemplates } = read;
  assert.deepStrictEqual(readDslDefinitions('yaml-dsl.yml', text), withoutTemplates, `${folder}: readDslDefinitions`);
  if (!read.dsls) return { problems: read.problems.map(problemJson) };
  const reading = { dsls: read.dsls.map((dsl) => dsl.definition.id) };
  if (read.templates.length > 0) reading.templates = read.templates.map((template) => template.definition.id);
  return reading;
}

function placeJson(action) {
  return action === null ? null : { line: action.position.line, column: action.position.column };
}

function templateBlockJson(block) {
  if (block instanceof TemplateConditional) {
    return {
      conditional: {
        branches: block.branches.map((branch) => ({
          ...placeJson(branch.opening),
          blocks: branch.blocks.map(templateBlockJson),
        })),
        closing: placeJson(block.closing),
      },
    };
  }
  return {
    loop: {
      ...placeJson(block.opening),
      variables: block.variables,
      blocks: block.blocks.map(templateBlockJson),
      closing: placeJson(block.closing),
    },
  };
}

function templateReading(folder) {
  const caseConfig = path.join(folder, 'yaml-dsl.yml');
  const config = fs.existsSync(caseConfig) ? caseConfig : path.join(CONFORMANCE, 'templates', 'yaml-dsl.yml');
  const definitions = readDefinitions('yaml-dsl.yml', fs.readFileSync(config, 'utf8'));
  const [{ definition }] = definitions.templates;
  const read = templateReader.readTemplate('input.yml', fs.readFileSync(path.join(folder, 'input.yml'), 'utf8'), definition);
  return {
    masked: read.masked,
    actions: read.actions.map((action) => ({ ...placeJson(action), text: action.text, directive: action.directive })),
    blocks: read.blocks.map(templateBlockJson),
    variables: read.variables.map((variable) => ({
      line: variable.position.line,
      column: variable.position.column,
      name: variable.name,
      loop: variable.loop === null ? null : placeJson(variable.loop.opening),
    })),
    template_variables: read.templateVariables.map((variable) => ({
      line: variable.position.line,
      column: variable.position.column,
      name: variable.name,
    })),
    problems: read.problems.map(problemJson),
  };
}

function yamlReading(folder) {
  const tagKeys = readJson(path.join(folder, 'tag-keys.json')) || [];
  const read = yamlReader.parseYaml('input.yml', fs.readFileSync(path.join(folder, 'input.yml'), 'utf8'), tagKeys);
  return read.node ? { node: nodeJson(read.node) } : { problem: problemJson(read.problem) };
}

function schemaReading(folder) {
  const schema = fs.readFileSync(path.join(folder, 'schema.json'), 'utf8');
  const document = fs.readFileSync(path.join(folder, 'document.yml'), 'utf8');
  const validator = readJson(path.join(folder, 'validator.json'));
  const rules = schemaValidation.ValidationRules.fromValidator(validator && {
    merge_base_keys: [],
    child_values_from_parent_key: [],
    allow_quoted_values: [],
    ...validator,
  });
  const { node } = yamlReader.parseYaml('document.yml', document, []);
  let loaded;
  try {
    loaded = new schemaValidation.SchemaCache().load('schema.json', schema);
  } catch (error) {
    return { error: error.message };
  }
  const unknownPointers = readJson(path.join(folder, 'unknown-values.json')) || [];
  const deferredKeyPointers = readJson(path.join(folder, 'deferred-keys.json')) || [];
  const mergeCalls = readJson(path.join(folder, 'merge-calls.json')) || {};
  const documentValue = withValuesRead(
    resolved.Value.fromNode(node),
    '',
    deferredKeyPointers,
    (value, pointer) => {
      if (unknownPointers.includes(pointer)) return declarationReference(value);
      if (Object.hasOwn(mergeCalls, pointer)) {
        const callArguments = [declarationReference(value), value];
        const mergeCall = resolved.call(mergeCalls[pointer], callArguments);
        return new resolved.Value('call', mergeCall, value.position);
      }
      return value;
    },
  );
  const fragment = readJson(path.join(folder, 'fragment.json'));
  const problems = fragment === null
    ? loaded.problems(documentValue, rules)
    : loaded.fragmentProblems(fragment.pointer, documentValue, rules);
  const expectedSubjects = readJson(path.join(folder, 'subjects.json'));
  if (expectedSubjects !== null) {
    assert.deepStrictEqual(
      problems.map((problem) => problem.subject),
      expectedSubjects,
      `schema/${path.basename(folder)}: subjects`,
    );
  }
  return { problems: problems.map((problem) => problemJson(problem.diagnostic)) };
}

/** A reference to a declaration, a value not known until the document compiles, at `value`. */
function declarationReference(value) {
  const reference = resolved.reference({
    scope: 'mock',
    name: 'mock',
    path: '',
    text: value.scalarText() || '',
    target: { kind: 'declaration', position: value.position },
  });
  return new resolved.Value('reference', reference, value.position);
}

/**
 * `value` with each value at or below `pointer` read by `reading`, its own values read
 * first, and each entry at a pointer of `deferredKeyPointers` keyed by a deferred key.
 */
function withValuesRead(value, pointer, deferredKeyPointers, reading) {
  if (value.kind === 'map') {
    const entries = value.value.map((entry) => {
      const child = `${pointer}/${entry.key.replace(/~/gu, '~0').replace(/\//gu, '~1')}`;
      const deferredKey = deferredKeyPointers.includes(child)
        ? declarationReference(resolved.Value.string(entry.key, entry.keyPosition))
        : entry.deferredKey;
      const read = withValuesRead(entry.value, child, deferredKeyPointers, reading);
      return { ...entry, deferredKey, value: read };
    });
    return reading(new resolved.Value('map', entries, value.position), pointer);
  }
  if (value.kind === 'list') {
    const items = value.value.map(
      (item, index) => withValuesRead(item, `${pointer}/${index}`, deferredKeyPointers, reading),
    );
    return reading(new resolved.Value('list', items, value.position), pointer);
  }
  return reading(value, pointer);
}

function patternReading(entry) {
  const compiled = compilePattern(entry.pattern);
  if (compiled === null) return { pattern: entry.pattern, compiles: false };
  const groups = groupNamesOf(compiled);
  const texts = (entry.texts || []).map((text) => {
    const match = compiled.exec(text);
    if (match === null) return { text, captures: null };
    const captures = { match: match[0] };
    for (const group of groups) {
      captures[group] = match.groups[group] === undefined ? null : match.groups[group];
    }
    return { text, captures };
  });
  return { pattern: entry.pattern, compiles: true, groups, texts };
}

test('every definitions case reads as expected', () => {
  const mismatches = mismatchesOf('definitions', 'yaml-dsl.yml', definitionsReading);
  assert.deepStrictEqual(mismatches, []);
});

test('every templates case reads as expected', () => {
  const mismatches = mismatchesOf('templates', 'input.yml', templateReading);
  assert.deepStrictEqual(mismatches, []);
});

test('every yaml case reads as expected', () => {
  const mismatches = mismatchesOf('yaml', 'input.yml', yamlReading);
  assert.deepStrictEqual(mismatches, []);
});

test('every schema case validates as expected', () => {
  const mismatches = mismatchesOf('schema', 'schema.json', schemaReading);
  assert.deepStrictEqual(mismatches, []);
});

/** A server answering each path a case serves with its status and body, and 404 to any other. */
async function startedServer(servedPaths) {
  const server = http.createServer((request, response) => {
    const [requestPath] = request.url.split('?');
    const served = Object.hasOwn(servedPaths, requestPath) ? servedPaths[requestPath] : { status: 404 };
    response.writeHead(served.status);
    response.end(served.body || '');
  });
  await new Promise((resolve) => {
    server.listen(0, '127.0.0.1', resolve);
  });
  return server;
}

async function schemaResolutionReading(caseFolder) {
  const folder = fs.realpathSync(caseFolder);
  const caseSpec = readJson(path.join(folder, 'case.json'));
  const server = caseSpec.served ? await startedServer(caseSpec.served) : null;
  try {
    const caseUrl = `file://${folder}`;
    const serverAddress = server === null ? '' : `http://127.0.0.1:${server.address().port}`;
    const filled = (text) => text.replaceAll('{case}', caseUrl).replaceAll('{server}', serverAddress);
    const unfilled = (text) => {
      const caseUnfilled = text.replaceAll(caseUrl, '{case}');
      return serverAddress === '' ? caseUnfilled : caseUnfilled.replaceAll(serverAddress, '{server}');
    };
    const searchPaths = (caseSpec.search_paths || []).map((entry) => ({
      pathText: filled(entry.path_text),
      writtenAt: entry.line === undefined ? null : new Position('mock-yaml-dsl.yml', entry.line, entry.column),
    }));
    const document = path.join(folder, caseSpec.document);
    const text = filled(fs.readFileSync(document, 'utf8'));
    const configRoot = path.join(folder, caseSpec.config_root);
    const finding = await new schemaValidation.SchemaCache().documentSchema(document, text, searchPaths, configRoot);
    return findingJson(finding, folder, unfilled);
  } finally {
    if (server !== null) {
      server.closeAllConnections();
      server.close();
    }
  }
}

function findingJson(finding, folder, unfilled) {
  const casePrefix = `${folder}/`;
  const candidates = finding.triedCandidates.map((candidate) => {
    const { namedBy } = candidate;
    let namedByJson = 'caller';
    if (namedBy.namerKind === 'modeline') {
      namedByJson = { modeline: { line: namedBy.position.line, column: namedBy.position.column } };
    } else if (namedBy.namerKind === 'searchPath') {
      namedByJson = { search_path: namedBy.index };
    }
    return { schema_location: unfilled(candidate.schemaLocation), named_by: namedByJson };
  });
  const found = finding.foundSchema === null ? null : {
    schema_name: unfilled(finding.foundSchema.schemaName),
    schema_text: finding.foundSchema.schemaText.trimEnd(),
    schema_fragment: finding.foundSchema.schemaFragment,
    found_candidate: finding.triedCandidates.indexOf(finding.foundSchema.foundCandidate),
  };
  const problems = finding.findingProblems.map((problem) => {
    const fields = { severity: problem.severity };
    if (problem.position) {
      const file = String(problem.position.file);
      fields.file = file.startsWith(casePrefix) ? file.slice(casePrefix.length) : file;
    }
    for (const [name, value] of Object.entries(problemJson(problem))) {
      fields[name] = typeof value === 'string' ? unfilled(value) : value;
    }
    return fields;
  });
  return { tried_candidates: candidates, found_schema: found, finding_problems: problems };
}

test('every schema resolution case finds as expected', async () => {
  const mismatches = await asyncMismatchesOf('schema-resolution', 'case.json', schemaResolutionReading);
  assert.deepStrictEqual(mismatches, []);
});

function formatReading({ layout, operands }) {
  try {
    return { layout, text: formatLayout(layout, operands) };
  } catch (error) {
    return { layout, problem: error.message };
  }
}

function dateFormatReading({ parts, offset }) {
  return {
    parts: parts.map((datePart) => (datePart.dateVerb === undefined ? { text: datePart.text } : {
      date_verb: datePart.dateVerb,
      left_aligned: datePart.leftAligned,
      zero_padded: datePart.zeroPadded,
      width: datePart.width,
    })),
    offset,
  };
}

function layoutReading(layout) {
  try {
    const parts = parseLayout(layout).map((part) => (part.verb === undefined ? { text: part.text } : {
      verb: part.verb.letter,
      left_aligned: part.verb.leftAligned,
      plus_signed: part.verb.plusSigned,
      zero_padded: part.verb.zeroPadded,
      width: part.verb.width,
      precision: part.verb.precision,
      date_format: part.verb.dateFormat === null ? null : dateFormatReading(part.verb.dateFormat),
    }));
    return { layout, parts };
  } catch (error) {
    return { layout, problem: error.message };
  }
}

test('every layout case reads as expected', () => {
  const cases = readJson(path.join(CONFORMANCE, 'layouts', 'cases.json'));
  const actual = { cases: cases.layouts.map(layoutReading) };
  assert.deepStrictEqual(actual, readJson(path.join(CONFORMANCE, 'layouts', 'expected.json')));
});

test('every format case writes as expected', () => {
  const cases = readJson(path.join(CONFORMANCE, 'format', 'cases.json'));
  const actual = { cases: cases.cases.map(formatReading) };
  assert.deepStrictEqual(actual, readJson(path.join(CONFORMANCE, 'format', 'expected.json')));
});

test('every pattern case compiles and matches as expected', () => {
  const cases = readJson(path.join(CONFORMANCE, 'patterns', 'cases.json'));
  const actual = { cases: cases.cases.map(patternReading) };
  assert.deepStrictEqual(actual, readJson(path.join(CONFORMANCE, 'patterns', 'expected.json')));
});
