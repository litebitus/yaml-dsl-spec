'use strict';

const assert = require('node:assert');
const test = require('node:test');

const { jsonPointerSteps } = require('..');

test('a json pointer gives each step unescaped', () => {
  assert.deepStrictEqual(jsonPointerSteps(''), []);
  assert.deepStrictEqual(jsonPointerSteps('/a~1b/~0c/0/'), ['a/b', '~c', '0', '']);
  assert.throws(() => jsonPointerSteps('a/b'), { message: '`a/b` is no JSON pointer: a pointer starts with `/`' });
  assert.throws(
    () => jsonPointerSteps('/a~2'),
    { message: '`/a~2` is no JSON pointer: `~` is followed by `0` or `1` in one' },
  );
});
