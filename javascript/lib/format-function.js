'use strict';

const { codeSpan } = require('./problem-wording');

const FORMAT_PART_LIMIT = 1000;
const EARLIEST_FORMAT_SECONDS = -62135596800;
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
  }],
]);

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
 * `zeroPadded`, and its `width` and `precision`, `null` where left out; throws
 * the first problem in the layout.
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
    if (directive === null) throw new Error(`layout ${codeSpan(layout)} ends inside a verb`);
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

function readFormatDirective(layoutCharacters, startPosition) {
  const verb = {
    letter: null,
    leftAligned: false,
    plusSigned: false,
    zeroPadded: false,
    width: null,
    precision: null,
  };
  const directive = { verb, overPartLimit: false };
  let position = startPosition + 1;
  while (position < layoutCharacters.length && '-+0'.includes(layoutCharacters[position])) {
    if (layoutCharacters[position] === '-') verb.leftAligned = true;
    else if (layoutCharacters[position] === '+') verb.plusSigned = true;
    else verb.zeroPadded = true;
    position += 1;
  }
  const width = readFormatNumber(layoutCharacters, position, directive);
  verb.width = width.value;
  position = width.endPosition;
  if (layoutCharacters[position] === '.') {
    const precision = readFormatNumber(layoutCharacters, position + 1, directive);
    verb.precision = precision.value === null ? 0 : precision.value;
    position = precision.endPosition;
  }
  if (position >= layoutCharacters.length) return null;
  verb.letter = layoutCharacters[position];
  directive.writtenText = layoutCharacters.slice(startPosition, position + 1).join('');
  directive.endPosition = position;
  return directive;
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
  if (verb.letter === 'B' && (verb.precision === null || verb.precision < 1)) {
    return `${fenced} takes a precision of at least 1, such as \`%.64B\``;
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
    default: return dateText(operand, operandNumber);
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

function dateText(operand, operandNumber) {
  const seconds = operand.kind === 'whole'
    ? Number(operand.wholeValue)
    : Math.floor(operand.numberValue);
  if (seconds < EARLIEST_FORMAT_SECONDS || seconds >= LATEST_FORMAT_SECONDS) {
    throw new Error(
      `\`%C\` writes a time in the years 1 to 9999, and operand ${operandNumber} is outside them`,
    );
  }
  const { year, month, day } = civilDateOfDays(Math.floor(seconds / SECONDS_PER_DAY));
  const twoDigits = (value) => String(value).padStart(2, '0');
  return `${String(year).padStart(4, '0')}-${twoDigits(month)}-${twoDigits(day)}`;
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
  if (verb.width === null) return verbText;
  const padding = verb.width - Array.from(verbText).length;
  if (padding <= 0) return verbText;
  if (verb.leftAligned) return verbText + ' '.repeat(padding);
  if (verb.zeroPadded && verbRule.readsZeroPadding) {
    const signText = verbText.startsWith('-') || verbText.startsWith('+') ? verbText[0] : '';
    return signText + '0'.repeat(padding) + verbText.slice(signText.length);
  }
  return ' '.repeat(padding) + verbText;
}

module.exports = { formatLayout, parseLayout };
