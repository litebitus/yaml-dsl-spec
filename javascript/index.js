'use strict';

const { packagedVersionFiles } = require('./schema-versions');

/** Each schema family's export, by the family's file stem. */
const FAMILIES = {
  yamlDslSchemas: 'yaml-dsl',
  functionTableSchemas: 'x-yaml-dsl-functions',
};

/** A family's schemas by version: a minor such as `1.0`, or a major such as `1`, its newest minor. */
function schemasOf(family) {
  return Object.fromEntries(
    packagedVersionFiles(family).map(([version, file]) => [version, require(file)]));
}

module.exports = Object.fromEntries(
  Object.entries(FAMILIES).map(([name, family]) => [name, schemasOf(family)]));
module.exports.FAMILIES = FAMILIES;
