'use strict';

/* The core's reader of `file` URLs: a file on the machine, read each time. */

const fs = require('fs');

const { codeSpan } = require('../../problem-wording');
const { FILE_SCHEME, fileUrlPath } = require('./schema-locations');
const { answerOf, failedAnswer, missingAnswer } = require('./schema-url-reading');

const NO_FILE_REASON = 'no file is there';
const UTF8_TEXT = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });

class FileSchemaReader {
  constructor() {
    this.urlPrefixes = ['file://'];
    this.readsRemoteSources = false;
  }

  async readSchemaUrl(request) {
    const queryStart = request.fetchUrl.indexOf('?');
    if (queryStart !== -1) {
      const [firstParameter] = request.fetchUrl.slice(queryStart + 1).split('&');
      const equalsAt = firstParameter.indexOf('=');
      const parameterName = equalsAt === -1 ? firstParameter : firstParameter.slice(0, equalsAt);
      return failedAnswer(`${codeSpan(parameterName)} is not a parameter of a ${codeSpan(FILE_SCHEME)} URL`);
    }
    const machinePath = fileUrlPath(request.fetchUrl);
    const status = fs.statSync(machinePath, { throwIfNoEntry: false });
    if (status === undefined || !status.isFile()) return missingAnswer(NO_FILE_REASON);
    try {
      const schemaText = UTF8_TEXT.decode(await fs.promises.readFile(machinePath));
      return answerOf({ outcomeKind: 'textSent', schemaText, copyVersion: null });
    } catch (error) {
      if (error.code === 'ENOENT') return missingAnswer(NO_FILE_REASON);
      return failedAnswer(error.message);
    }
  }
}

module.exports = { FileSchemaReader };
