'use strict';

/** The regular expression a `yaml-dsl.yml` pattern writes, or `null` where it writes none. */
function compilePattern(text) {
  try {
    return new RegExp(text, 'u');
  } catch {
    return null;
  }
}

/** The named groups of a compiled pattern, in the order the pattern writes them. */
function groupNamesOf(pattern) {
  const anyText = new RegExp(`${pattern.source}|`, 'u');
  return Object.keys(anyText.exec('').groups || {});
}

module.exports = { compilePattern, groupNamesOf };
