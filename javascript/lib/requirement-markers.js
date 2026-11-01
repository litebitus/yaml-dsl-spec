'use strict';

/*
 * Requirement markers: a field's `description` opening with `[required]`,
 * `[~required]` or `[optional]` decides whether the field is required, over its
 * parent's `required` array.
 */

const REQUIRED_MARKER = '[required]';
const REQUIREMENT_MARKERS = [REQUIRED_MARKER, '[~required]', '[optional]'];
const CONDITION_KEYWORDS = new Set(['if', 'not']);
const FORM_SELECTING_KEYWORDS = new Set(['anyOf', 'oneOf']);
const SAME_INSTANCE_SCHEMA_KEYWORDS = new Set(['else', 'if', 'not', 'then']);
const SAME_INSTANCE_LIST_KEYWORDS = new Set(['allOf', 'anyOf', 'oneOf']);
const SCHEMA_MAP_KEYWORDS = new Set([
  '$defs',
  'definitions',
  'dependentSchemas',
  'patternProperties',
  'properties',
]);
const DRAFT_KEYWORDS = {
  'draft-07': {
    childSchema: new Set([
      'additionalItems',
      'additionalProperties',
      'contains',
      'items',
      'propertyNames',
    ]),
    schemaMap: new Set([
      '$defs',
      'definitions',
      'dependencies',
      'patternProperties',
      'properties',
    ]),
  },
  'draft 2019-09': {
    childSchema: new Set([
      'additionalItems',
      'additionalProperties',
      'contains',
      'contentSchema',
      'items',
      'propertyNames',
      'unevaluatedItems',
      'unevaluatedProperties',
    ]),
    schemaMap: SCHEMA_MAP_KEYWORDS,
  },
  'draft 2020-12': {
    childSchema: new Set([
      'additionalProperties',
      'contains',
      'contentSchema',
      'items',
      'prefixItems',
      'propertyNames',
      'unevaluatedItems',
      'unevaluatedProperties',
    ]),
    schemaMap: SCHEMA_MAP_KEYWORDS,
  },
};

const isObject = (value) => value !== null && typeof value === 'object' && !Array.isArray(value);

/** The marker a schema node's `description` opens with, or `null`. */
function requirementMarker(node) {
  if (!isObject(node) || typeof node.description !== 'string') return null;
  const description = node.description.trimStart();
  return REQUIREMENT_MARKERS.find((marker) => description.startsWith(marker)) || null;
}

/** A node's description without the marker it opens with, or `null`. */
function descriptionText(node) {
  if (!isObject(node) || typeof node.description !== 'string') return null;
  const marker = requirementMarker(node);
  const text = marker ? node.description.trimStart().slice(marker.length) : node.description;
  return text.trim() || null;
}

/** `schema`, read in the draft named `draftName`, with each `required` array decided by markers. */
function markedSchema(schema, draftName) {
  return markedNode(schema, null, DRAFT_KEYWORDS[draftName]);
}

function markersOfFields(properties) {
  const markers = new Map();
  for (const [key, field] of Object.entries(properties)) {
    const marker = requirementMarker(field);
    if (marker) markers.set(key, marker);
  }
  return markers;
}

function markedNode(node, inheritedMarkers, keywords) {
  if (!isObject(node)) return node;
  const ownMarkers = isObject(node.properties) ? markersOfFields(node.properties) : null;
  const markers = ownMarkers || inheritedMarkers;
  const marked = {};
  const markedChild = (child) => markedNode(child, null, keywords);
  for (const [keyword, value] of Object.entries(node)) {
    if (keyword === 'required') continue;
    if (CONDITION_KEYWORDS.has(keyword)) marked[keyword] = value;
    else if (SAME_INSTANCE_SCHEMA_KEYWORDS.has(keyword)) {
      marked[keyword] = markedNode(value, markers, keywords);
    } else if (SAME_INSTANCE_LIST_KEYWORDS.has(keyword) && Array.isArray(value)) {
      const branchMarkers = FORM_SELECTING_KEYWORDS.has(keyword) ? null : markers;
      marked[keyword] = value.map((branch) => markedNode(branch, branchMarkers, keywords));
    } else if (keywords.childSchema.has(keyword)) {
      marked[keyword] = Array.isArray(value) ? value.map(markedChild) : markedChild(value);
    } else if (keywords.schemaMap.has(keyword) && isObject(value)) {
      marked[keyword] = Object.fromEntries(Object.entries(value)
        .map(([name, schema]) => [name, markedChild(schema)]));
    } else {
      marked[keyword] = value;
    }
  }
  const listed = Array.isArray(node.required) ? node.required : [];
  const required = listed.filter((key) => !markers || !markers.has(key));
  for (const [key, marker] of ownMarkers || []) if (marker === REQUIRED_MARKER) required.push(key);
  if (required.length > 0) marked.required = required;
  return marked;
}

module.exports = { requirementMarker, descriptionText, markedSchema };
