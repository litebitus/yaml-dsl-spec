'use strict';

const assert = require('node:assert');
const test = require('node:test');

const { formatLayout } = require('..');

test('an operand of no kind the format function writes is a problem naming its kind', () => {
  for (const [operandValue, kindName] of [
    [Number.NaN, 'a number that is not finite'],
    [Number.NEGATIVE_INFINITY, 'a number that is not finite'],
    [undefined, 'a JavaScript undefined'],
    [10n, 'a JavaScript bigint'],
  ]) {
    assert.throws(() => formatLayout('%f', [operandValue]), {
      message: `\`%f\` writes a number, and operand 1 is ${kindName}`,
    });
  }
});
