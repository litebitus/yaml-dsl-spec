'use strict';

const { codeSpan } = require('./problem-wording');

const FORMAT_PART_LIMIT = 1000;
const MAXIMUM_TIME_DIGITS = 9;
const EARLIEST_FORMAT_SECONDS = -62167219200;
const LATEST_FORMAT_SECONDS = 253402300800;
const SECONDS_PER_DAY = 86400;
const WHOLE_NUMBER_LIMIT = 2 ** 63;
const TEXT_KINDS = 'text';
const NUMBER_KINDS = 'a number';
const WHOLE_KINDS = 'a whole number';
const GREEK_UPPER_CASE_STARTS = [0x1f80, 0x1f90, 0x1fa0];
const UPPER_CASE_EXCEPTIONS = new Map([
  ...GREEK_UPPER_CASE_STARTS.flatMap((start) => Array.from(
    { length: 8 },
    (_, step) => [start + step, start + step + 8],
  )),
  [0x1fb3, 0x1fbc],
  [0x1fc3, 0x1fcc],
  [0x1ff3, 0x1ffc],
]);
const LOWER_CASE_EXCEPTIONS = new Map([[0x130, 0x69]]);
const QUOTED_ESCAPES = new Map([
  ['"', '\\"'],
  ['\\', '\\\\'],
  ['\u0007', '\\a'],
  ['\b', '\\b'],
  ['\f', '\\f'],
  ['\n', '\\n'],
  ['\r', '\\r'],
  ['\t', '\\t'],
  ['\v', '\\v'],
]);

const TEXT_VERB = {
  acceptedKinds: TEXT_KINDS,
  takesOperand: true,
  kinds: ['text'],
  readsWidth: true,
  readsPrecision: true,
};
const HEXADECIMAL_VERB = {
  acceptedKinds: 'a whole number or text',
  takesOperand: true,
  kinds: ['text', 'whole'],
  readsWidth: true,
  readsZeroPadding: true,
};
const FORMAT_VERB_RULES = new Map([
  ['%', { kinds: [] }],
  ['s', TEXT_VERB],
  ['q', TEXT_VERB],
  ['u', TEXT_VERB],
  ['l', TEXT_VERB],
  ['A', TEXT_VERB],
  ['B', TEXT_VERB],
  ['v', {
    acceptedKinds: 'text, a whole number or a boolean',
    takesOperand: true,
    kinds: ['text', 'whole', 'boolean'],
    readsWidth: true,
  }],
  ['d', {
    acceptedKinds: WHOLE_KINDS,
    takesOperand: true,
    kinds: ['whole'],
    readsWidth: true,
    readsZeroPadding: true,
    readsPlusSign: true,
  }],
  ['x', HEXADECIMAL_VERB],
  ['X', HEXADECIMAL_VERB],
  ['f', {
    acceptedKinds: NUMBER_KINDS,
    takesOperand: true,
    kinds: ['whole', 'number'],
    readsWidth: true,
    readsPrecision: true,
    readsZeroPadding: true,
    readsPlusSign: true,
  }],
  ['C', {
    acceptedKinds: NUMBER_KINDS,
    takesOperand: true,
    kinds: ['whole', 'number'],
    readsWidth: true,
    readsPrecision: true,
    readsDateFormat: true,
  }],
]);
const DATE_VERB_LETTERS = 'YymdHMSLz';

/**
 * The standard's format function: the layout with each verb written from the
 * next operand; throws the problem that stops it.
 */
function formatLayout(layout, operands) {
  let writtenText = '';
  let nextOperand = 0;
  for (const layoutPart of parseLayout(layout)) {
    if (layoutPart.verb === undefined) {
      writtenText += layoutPart.text;
      continue;
    }
    if (nextOperand >= operands.length) {
      throw new Error(`layout ${codeSpan(layout)} has more verbs than operands`);
    }
    const verbRule = FORMAT_VERB_RULES.get(layoutPart.verb.letter);
    const operand = formatOperandOf(operands[nextOperand]);
    nextOperand += 1;
    const verbText = formattedVerbText(layoutPart.verb, verbRule, operand, nextOperand);
    writtenText += paddedVerbText(layoutPart.verb, verbRule, verbText);
  }
  if (nextOperand < operands.length) {
    throw new Error(`layout ${codeSpan(layout)} has more operands than verbs`);
  }
  return writtenText;
}

/**
 * The layout read into its parts, `{ text }` for literal text, `%%` read as
 * text, and `{ verb }` for a verb: its `letter`, `leftAligned`, `plusSigned`,
 * `zeroPadded`, its `width` and `precision`, and its `dateFormat`, `null` where
 * left out. A date format is its `parts`, `{ text }` with `%%` read as text and
 * `{ dateVerb }` with its `leftAligned`, `zeroPadded` and `width`, and its
 * `offset` as written; throws the first problem in the layout.
 */
function parseLayout(layout) {
  const layoutCharacters = Array.from(layout);
  const layoutParts = [];
  let literalText = '';
  for (let position = 0; position < layoutCharacters.length; position += 1) {
    if (layoutCharacters[position] !== '%') {
      literalText += layoutCharacters[position];
      continue;
    }
    const directive = readFormatDirective(layoutCharacters, position);
    if (directive.dateFormatProblem !== null) throw new Error(directive.dateFormatProblem);
    if (!directive.isComplete) throw new Error(`layout ${codeSpan(layout)} ends inside a verb`);
    position = directive.endPosition;
    const verbRule = FORMAT_VERB_RULES.get(directive.verb.letter);
    if (verbRule === undefined) {
      throw new Error(`${codeSpan(directive.writtenText)} is not a verb \`format\` writes`);
    }
    const partProblem = directivePartProblem(directive, verbRule);
    if (partProblem !== null) throw new Error(partProblem);
    if (!verbRule.takesOperand) {
      literalText += '%';
      continue;
    }
    if (literalText !== '') layoutParts.push({ text: literalText });
    literalText = '';
    layoutParts.push({ verb: directive.verb });
  }
  if (literalText !== '') layoutParts.push({ text: literalText });
  return layoutParts;
}

function emptyFormatDirective() {
  return {
    verb: {
      letter: null,
      leftAligned: false,
      plusSigned: false,
      zeroPadded: false,
      width: null,
      precision: null,
      dateFormat: null,
    },
    overPartLimit: false,
    dateFormatProblem: null,
    isComplete: false,
  };
}

function readFormatDirective(layoutCharacters, startPosition) {
  const directive = emptyFormatDirective();
  let position = readFlagsAndWidth(layoutCharacters, startPosition + 1, directive);
  if (layoutCharacters[position] === '{') {
    const dateFormatEnd = readDateFormat(layoutCharacters, position + 1, directive);
    if (!dateFormatEnd.isClosed) return directive;
    position = dateFormatEnd.endPosition;
  }
  position = readPrecision(layoutCharacters, position, directive);
  if (position >= layoutCharacters.length) return directive;
  directive.verb.letter = layoutCharacters[position];
  directive.writtenText = layoutCharacters.slice(startPosition, position + 1).join('');
  directive.endPosition = position;
  directive.isComplete = true;
  return directive;
}

/** A verb's or a date verb's flags and width read onto the directive; answers where they end. */
function readFlagsAndWidth(layoutCharacters, startPosition, directive) {
  const { verb } = directive;
  let position = startPosition;
  while (position < layoutCharacters.length && '-+0'.includes(layoutCharacters[position])) {
    if (layoutCharacters[position] === '-') verb.leftAligned = true;
    else if (layoutCharacters[position] === '+') verb.plusSigned = true;
    else verb.zeroPadded = true;
    position += 1;
  }
  const width = readFormatNumber(layoutCharacters, position, directive);
  verb.width = width.value;
  return width.endPosition;
}

/** A precision read onto the directive where one is written, `.` alone being 0; answers where it ends. */
function readPrecision(layoutCharacters, position, directive) {
  if (layoutCharacters[position] !== '.') return position;
  const precision = readFormatNumber(layoutCharacters, position + 1, directive);
  directive.verb.precision = precision.value === null ? 0 : precision.value;
  return precision.endPosition;
}

/**
 * The date format from after its opening brace to its closing one, set on the directive where closed;
 * answers where it ends and whether it is closed. Its first problem is kept on the directive.
 */
function readDateFormat(layoutCharacters, startPosition, directive) {
  const dateFormat = { parts: [], offset: null };
  const keepProblem = (problem) => {
    if (directive.dateFormatProblem === null) directive.dateFormatProblem = problem;
  };
  let literalText = '';
  let position = startPosition;
  const endsDateParts = (character) => character === '}' || character === '|';
  while (position < layoutCharacters.length && !endsDateParts(layoutCharacters[position])) {
    if (layoutCharacters[position] !== '%') {
      literalText += layoutCharacters[position];
      position += 1;
      continue;
    }
    if (layoutCharacters[position + 1] === '%') {
      literalText += '%';
      position += 2;
      continue;
    }
    const dateVerbRead = readDateVerb(layoutCharacters, position, keepProblem);
    if (!dateVerbRead.isComplete) return { isClosed: false };
    position = dateVerbRead.endPosition;
    if (literalText !== '') dateFormat.parts.push({ text: literalText });
    literalText = '';
    dateFormat.parts.push(dateVerbRead.dateFormatPart);
  }
  if (literalText !== '') dateFormat.parts.push({ text: literalText });
  if (layoutCharacters[position] === '|') {
    const offsetStart = position + 1;
    while (position < layoutCharacters.length && layoutCharacters[position] !== '}') position += 1;
    const offsetText = layoutCharacters.slice(offsetStart, position).join('');
    if (offsetText === '') {
      keepProblem('a date format\'s `|` takes an offset, such as `+08:00`');
    } else if (!offsetMinutesOf(offsetText).isOffset) {
      keepProblem(`${codeSpan(offsetText)} is not an offset, such as \`Z\` or \`+08:00\``);
    }
    dateFormat.offset = offsetText;
  }
  if (position >= layoutCharacters.length) return { isClosed: false };
  directive.verb.dateFormat = dateFormat;
  return { isClosed: true, endPosition: position + 1 };
}

/**
 * A date verb from its `%` through its letter: its date format part, where it ends and whether the layout
 * holds its letter; its first problem is kept.
 */
function readDateVerb(layoutCharacters, startPosition, keepProblem) {
  const directive = emptyFormatDirective();
  let position = readFlagsAndWidth(layoutCharacters, startPosition + 1, directive);
  position = readPrecision(layoutCharacters, position, directive);
  if (position >= layoutCharacters.length) return { isComplete: false };
  const { verb } = directive;
  const dateLetter = layoutCharacters[position];
  const fencedDateVerb = codeSpan(layoutCharacters.slice(startPosition, position + 1).join(''));
  if (!DATE_VERB_LETTERS.includes(dateLetter)) {
    keepProblem(`${fencedDateVerb} is not a date verb \`format\` writes`);
  } else if (directive.overPartLimit) {
    keepProblem(`${fencedDateVerb} has a width or a precision over ${FORMAT_PART_LIMIT}`);
  } else if (verb.plusSigned) {
    keepProblem(`${fencedDateVerb} takes no \`+\` flag`);
  } else if (verb.zeroPadded && dateLetter === 'z') {
    keepProblem(`${fencedDateVerb} takes no \`0\` flag`);
  } else if (verb.precision !== null) {
    keepProblem(`${fencedDateVerb} takes no precision`);
  }
  return {
    isComplete: true,
    endPosition: position + 1,
    dateFormatPart: {
      dateVerb: dateLetter,
      leftAligned: verb.leftAligned,
      zeroPadded: verb.zeroPadded,
      width: verb.width,
    },
  };
}

/** A time offset read: `Z`, or a sign, hours `00` to `23`, `:` and minutes `00` to `59`. */
function offsetMinutesOf(offsetText) {
  if (offsetText === 'Z') return { offsetMinutes: 0, isOffset: true };
  const written = /^([+-])([0-9]{2}):([0-9]{2})$/u.exec(offsetText);
  if (written === null) return { offsetMinutes: 0, isOffset: false };
  const hours = Number(written[2]);
  const minutes = Number(written[3]);
  const offsetMinutes = (written[1] === '-' ? -1 : 1) * (hours * 60 + minutes);
  return { offsetMinutes, isOffset: hours <= 23 && minutes <= 59 };
}

function readFormatNumber(layoutCharacters, startPosition, directive) {
  let value = 0;
  let position = startPosition;
  while (position < layoutCharacters.length && /^[0-9]$/u.test(layoutCharacters[position])) {
    if (value > FORMAT_PART_LIMIT) directive.overPartLimit = true;
    else value = value * 10 + Number(layoutCharacters[position]);
    position += 1;
  }
  if (value > FORMAT_PART_LIMIT) directive.overPartLimit = true;
  return { value: position > startPosition ? value : null, endPosition: position };
}

function directivePartProblem({ verb, writtenText, overPartLimit }, verbRule) {
  const fenced = codeSpan(writtenText);
  if (overPartLimit) return `${fenced} has a width or a precision over ${FORMAT_PART_LIMIT}`;
  if (verb.leftAligned && !verbRule.readsWidth) return `${fenced} takes no \`-\` flag`;
  if (verb.plusSigned && !verbRule.readsPlusSign) return `${fenced} takes no \`+\` flag`;
  if (verb.zeroPadded && !verbRule.readsZeroPadding) return `${fenced} takes no \`0\` flag`;
  if (verb.width !== null && !verbRule.readsWidth) return `${fenced} takes no width`;
  if (verb.precision !== null && !verbRule.readsPrecision) return `${fenced} takes no precision`;
  if (verb.dateFormat !== null && !verbRule.readsDateFormat) return `${fenced} takes no date format`;
  if (verb.letter === 'B' && (verb.precision === null || verb.precision < 1)) {
    return `${fenced} takes a precision of at least 1, such as \`%.64B\``;
  }
  if (verb.letter === 'C' && verb.precision !== null && verb.precision > MAXIMUM_TIME_DIGITS) {
    return `${fenced} takes a precision of at most ${MAXIMUM_TIME_DIGITS}, such as \`.3\` for milliseconds`;
  }
  if (verbRule.readsDateFormat && verb.dateFormat === null) {
    return `${fenced} takes a date format, such as \`%{%Y-%m-%d}C\``;
  }
  return null;
}

function formatOperandOf(operandValue) {
  if (typeof operandValue === 'string') {
    return { kind: 'text', kindName: 'text', textValue: operandValue };
  }
  if (typeof operandValue === 'boolean') {
    return { kind: 'boolean', kindName: 'a boolean', booleanValue: operandValue };
  }
  if (operandValue === null) return { kind: 'other', kindName: 'null' };
  if (Array.isArray(operandValue)) return { kind: 'other', kindName: 'a list' };
  if (typeof operandValue === 'object') return { kind: 'other', kindName: 'a map' };
  if (typeof operandValue !== 'number') {
    return { kind: 'other', kindName: `a JavaScript ${typeof operandValue}` };
  }
  if (!Number.isFinite(operandValue)) {
    return { kind: 'other', kindName: 'a number that is not finite' };
  }
  const isWhole = Number.isInteger(operandValue)
    && operandValue >= -WHOLE_NUMBER_LIMIT
    && operandValue < WHOLE_NUMBER_LIMIT;
  if (isWhole) return { kind: 'whole', kindName: WHOLE_KINDS, wholeValue: BigInt(operandValue) };
  return { kind: 'number', kindName: NUMBER_KINDS, numberValue: operandValue };
}

function formattedVerbText(verb, verbRule, operand, operandNumber) {
  if (!verbRule.kinds.includes(operand.kind)) {
    throw new Error(
      `${codeSpan(`%${verb.letter}`)} writes ${verbRule.acceptedKinds}, `
        + `and operand ${operandNumber} is ${operand.kindName}`,
    );
  }
  let readText = operand.textValue;
  if (verb.precision !== null && operand.kind === 'text' && verb.letter !== 'B') {
    readText = Array.from(readText).slice(0, verb.precision).join('');
  }
  const precision = verb.precision === null ? 6 : verb.precision;
  switch (verb.letter) {
    case 's': return readText;
    case 'q': return goQuotedText(readText);
    case 'u': return mappedCase(readText, UPPER_CASE_EXCEPTIONS, (text) => text.toUpperCase());
    case 'l': return mappedCase(readText, LOWER_CASE_EXCEPTIONS, (text) => text.toLowerCase());
    case 'A': return readText.replaceAll('/', '_');
    case 'B': return tailBoundedText(readText, verb.precision);
    case 'v': return valueVerbText(operand);
    case 'd': return signedNumberText(operand.wholeValue.toString(), verb.plusSigned);
    case 'x': return hexadecimalText(operand);
    case 'X': return hexadecimalText(operand).toUpperCase();
    case 'f': return signedNumberText(fixedPointText(operand, precision), verb.plusSigned);
    default: return dateText(
      operand,
      verb.precision === null ? 0 : verb.precision,
      verb.dateFormat,
      operandNumber,
    );
  }
}

function valueVerbText(operand) {
  if (operand.kind === 'text') return operand.textValue;
  return String(operand.kind === 'whole' ? operand.wholeValue : operand.booleanValue);
}

function mappedCase(text, exceptions, fullMapping) {
  return Array.from(text, (character) => {
    const codePoint = character.codePointAt(0);
    if (exceptions.has(codePoint)) return String.fromCodePoint(exceptions.get(codePoint));
    const mapped = fullMapping(character);
    return Array.from(mapped).length === 1 ? mapped : character;
  }).join('');
}

function tailBoundedText(text, precision) {
  const characters = Array.from(text);
  if (characters.length <= precision) return text;
  return `-${characters.slice(characters.length - (precision - 1)).join('')}`;
}

function signedNumberText(numberText, plusSigned) {
  return plusSigned && !numberText.startsWith('-') ? `+${numberText}` : numberText;
}

function hexadecimalText(operand) {
  if (operand.kind === 'text') return Buffer.from(operand.textValue, 'utf8').toString('hex');
  if (operand.wholeValue < 0n) return `-${(-operand.wholeValue).toString(16)}`;
  return operand.wholeValue.toString(16);
}

function fixedPointText(operand, precision) {
  if (operand.kind === 'whole') {
    if (precision === 0) return operand.wholeValue.toString();
    return `${operand.wholeValue}.${'0'.repeat(precision)}`;
  }
  const { mantissa, binaryExponent } = exactBinaryParts(Math.abs(operand.numberValue));
  const scale = 10n ** BigInt(precision);
  let scaled;
  if (binaryExponent >= 0) {
    scaled = mantissa * scale * 2n ** BigInt(binaryExponent);
  } else {
    const numerator = mantissa * scale;
    const denominator = 2n ** BigInt(-binaryExponent);
    scaled = numerator / denominator;
    const twiceRemainder = 2n * (numerator % denominator);
    const roundsUp = twiceRemainder > denominator
      || (twiceRemainder === denominator && scaled % 2n === 1n);
    if (roundsUp) scaled += 1n;
  }
  const digits = scaled.toString().padStart(precision + 1, '0');
  const signText = operand.numberValue < 0 ? '-' : '';
  if (precision === 0) return `${signText}${digits}`;
  return `${signText}${digits.slice(0, -precision)}.${digits.slice(-precision)}`;
}

function exactBinaryParts(positiveNumber) {
  const view = new DataView(new ArrayBuffer(8));
  view.setFloat64(0, positiveNumber);
  const storedExponent = (view.getUint32(0) >>> 20) & 0x7ff;
  const fraction = (BigInt(view.getUint32(0) & 0xfffff) << 32n) | BigInt(view.getUint32(4));
  if (storedExponent === 0) return { mantissa: fraction, binaryExponent: -1074 };
  return { mantissa: fraction | (1n << 52n), binaryExponent: storedExponent - 1075 };
}

function dateText(operand, timeDigits, dateFormat, operandNumber) {
  const unitsPerSecond = 10n ** BigInt(timeDigits);
  const isNearRange = (seconds) => seconds >= EARLIEST_FORMAT_SECONDS - SECONDS_PER_DAY
    && seconds < LATEST_FORMAT_SECONDS + SECONDS_PER_DAY;
  let seconds;
  let milliseconds;
  if (operand.kind === 'whole') {
    let wholeSeconds = operand.wholeValue / unitsPerSecond;
    let remainder = operand.wholeValue % unitsPerSecond;
    if (remainder < 0n) {
      wholeSeconds -= 1n;
      remainder += unitsPerSecond;
    }
    seconds = Number(wholeSeconds);
    milliseconds = Number((remainder * 1000n) / unitsPerSecond);
  } else {
    const timeInSeconds = operand.numberValue / Number(unitsPerSecond);
    seconds = Math.floor(timeInSeconds);
    milliseconds = Math.floor((timeInSeconds - seconds) * 1000);
  }
  if (!isNearRange(seconds)) throw dateRangeProblem(operandNumber);
  const { offset } = dateFormat;
  const writtenOffset = offset === null || offset === '+00:00' ? 'Z' : offset;
  seconds += offsetMinutesOf(writtenOffset).offsetMinutes * 60;
  if (seconds < EARLIEST_FORMAT_SECONDS || seconds >= LATEST_FORMAT_SECONDS) {
    throw dateRangeProblem(operandNumber);
  }
  const days = Math.floor(seconds / SECONDS_PER_DAY);
  const secondOfDay = seconds - days * SECONDS_PER_DAY;
  const { year, month, day } = civilDateOfDays(days);
  const dateVerbValues = {
    Y: year,
    y: year % 100,
    m: month,
    d: day,
    H: Math.floor(secondOfDay / 3600),
    M: Math.floor((secondOfDay % 3600) / 60),
    S: secondOfDay % 60,
    L: milliseconds,
  };
  return dateFormat.parts.map((part) => {
    if (part.dateVerb === undefined) return part.text;
    if (part.dateVerb === 'z') return paddedText(writtenOffset, part.width, part.leftAligned, false);
    return paddedText(String(dateVerbValues[part.dateVerb]), part.width, part.leftAligned, part.zeroPadded);
  }).join('');
}

function dateRangeProblem(operandNumber) {
  return new Error(
    `\`%C\` writes a time in the years 0000 to 9999, and operand ${operandNumber} is outside them`,
  );
}

function civilDateOfDays(days) {
  const shiftedDays = days + 719468;
  const era = Math.floor(shiftedDays / 146097);
  const dayOfEra = shiftedDays - era * 146097;
  const yearOfEra = Math.floor((
    dayOfEra
    - Math.floor(dayOfEra / 1460)
    + Math.floor(dayOfEra / 36524)
    - Math.floor(dayOfEra / 146096)
  ) / 365);
  const dayOfYear = dayOfEra
    - (365 * yearOfEra + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100));
  const shiftedMonth = Math.floor((5 * dayOfYear + 2) / 153);
  const day = dayOfYear - Math.floor((153 * shiftedMonth + 2) / 5) + 1;
  const month = shiftedMonth < 10 ? shiftedMonth + 3 : shiftedMonth - 9;
  return { year: yearOfEra + era * 400 + (month <= 2 ? 1 : 0), month, day };
}

function goQuotedText(text) {
  const escaped = Array.from(text, (character) => {
    if (QUOTED_ESCAPES.has(character)) return QUOTED_ESCAPES.get(character);
    const codePoint = character.codePointAt(0);
    if (codePoint < 0x20 || codePoint === 0x7f) {
      return `\\x${codePoint.toString(16).padStart(2, '0')}`;
    }
    if (codePoint >= 0x80 && codePoint <= 0x9f) {
      return `\\u${codePoint.toString(16).padStart(4, '0')}`;
    }
    return character;
  });
  return `"${escaped.join('')}"`;
}

function paddedVerbText(verb, verbRule, verbText) {
  const zeroPadded = verb.zeroPadded && verbRule.readsZeroPadding === true;
  return paddedText(verbText, verb.width, verb.leftAligned, zeroPadded);
}

/**
 * Text padded to a width: on the right where left-aligned, else with leading zeros after its sign where
 * zero-padded, else with spaces on the left.
 */
function paddedText(text, width, leftAligned, zeroPadded) {
  if (width === null) return text;
  const padding = width - Array.from(text).length;
  if (padding <= 0) return text;
  if (leftAligned) return text + ' '.repeat(padding);
  if (zeroPadded) {
    const signText = text.startsWith('-') || text.startsWith('+') ? text[0] : '';
    return signText + '0'.repeat(padding) + text.slice(signText.length);
  }
  return ' '.repeat(padding) + text;
}

module.exports = { formatLayout, parseLayout };
