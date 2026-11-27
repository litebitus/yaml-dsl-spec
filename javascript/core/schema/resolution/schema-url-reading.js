'use strict';

/*
 * What a schema cache asks a URL reader and hears back. A reader is
 * `{ urlPrefixes, readsRemoteSources, readSchemaUrl(request) }`, its answer a
 * promise of `{ readOutcome, readRefusals }`.
 */

/** An answer of `readOutcome` with no refusal. */
function answerOf(readOutcome) {
  return { readOutcome, readRefusals: [] };
}

/** A read the source refused, with each refusal it gave. */
function refusedAnswer(readRefusals) {
  return { readOutcome: { outcomeKind: 'readRefused' }, readRefusals };
}

/** A read that failed, and why. */
function failedAnswer(failureReason) {
  return answerOf({ outcomeKind: 'readFailed', failureReason });
}

/** A source that is not there, and why. */
function missingAnswer(missingReason) {
  return answerOf({ outcomeKind: 'sourceMissing', missingReason });
}

module.exports = { answerOf, refusedAnswer, failedAnswer, missingAnswer };
