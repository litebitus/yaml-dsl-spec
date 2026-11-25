'use strict';

/*
 * Reads a template of a DSL's documents: masks its actions so the YAML reader
 * reads it in place, and reads its conditionals, loops and loop variables, or
 * the problems in them.
 */

const { Diagnostic } = require('./diagnostic-report');
const { codeSpan } = require('./problem-wording');
const { Position } = require('./source-position');
const { LinePlaces } = require('./yaml-reader');

const MASKED_ACTION_CHARACTER = '_';
const MASKED_DIRECTIVE_CHARACTER = ' ';

/** One action of a template, as written. */
class TemplateAction {
  constructor(position, text, directive) {
    this.position = position;
    this.text = text;
    this.directive = directive;
  }
}

/** A conditional: its branches, each opened by an action, and the action closing it. */
class TemplateConditional {
  constructor(opening) {
    this.branches = [{ opening, blocks: [] }];
    this.closing = null;
  }
}

/** A loop: the action opening it, the loop variables it binds, its body and the action closing it. */
class TemplateLoop {
  constructor(opening, variables) {
    this.opening = opening;
    this.variables = variables;
    this.blocks = [];
    this.closing = null;
  }
}

/** What a template reads to: its masked text, actions, blocks, loop and template variables, and problems. */
class TemplateReading {
  constructor(masked, actions, blocks, variables, templateVariables, problems) {
    this.masked = masked;
    this.actions = actions;
    this.blocks = blocks;
    this.variables = variables;
    this.templateVariables = templateVariables;
    this.problems = problems;
  }
}

/** The directive a body writes, by the template's words, or `null` for an action that is none. */
function directiveKindOf(body, template) {
  const start = template.loop.start.exec(body);
  if (start !== null && start.index === 0) return { kind: 'loop', start };
  const words = [
    [template.conditional.if, 'if'],
    [template.conditional.else_if, 'else_if'],
    [template.conditional.else, 'else'],
    [template.conditional.end, 'end'],
    [template.loop.end, 'end'],
  ];
  let fitting = null;
  for (const [word, kind] of words) {
    const fits = body === word || body.startsWith(`${word} `);
    if (fits && (fitting === null || word.length > fitting.word.length)) fitting = { word, kind };
  }
  return fitting === null ? null : { kind: fitting.kind, word: fitting.word };
}

/** The text `text` with each character in `[begin, end)` but a line break written as `character`. */
function maskedRange(text, begin, end, character) {
  const masked = Array.from(text.slice(begin, end), (written) => (written === '\n' ? written : character));
  return masked.join('');
}

class TemplateReader {
  constructor(file, text, template) {
    this.file = file;
    this.text = text;
    this.template = template;
    this.places = new LinePlaces(text);
    this.actions = [];
    this.blocks = [];
    this.variables = [];
    this.templateVariables = [];
    this.problems = [];
    this.openBlocks = [];
    this.elseActions = new Map();
    this.variablePattern = new RegExp(template.loop.variable.source, 'gu');
    this.templateVariablePattern = new RegExp(template.variable.source, 'gu');
  }

  positionAt(offset) {
    const { line, column } = this.places.placeOf(offset);
    return new Position(this.file, line, column);
  }

  read() {
    const { left, right } = this.template.delimiters;
    let masked = '';
    let cursor = 0;
    let unread = 0;
    for (;;) {
      const begin = this.text.indexOf(left, cursor);
      if (begin < 0) break;
      const finish = this.text.indexOf(right, begin + left.length);
      if (finish < 0) {
        this.problems.push(Diagnostic.at(this.positionAt(begin), `${codeSpan(left)} opens an action that is never closed`)
          .withLabel(`no ${codeSpan(right)} after it`));
        break;
      }
      const end = finish + right.length;
      const directive = this.readAction(begin, finish, end);
      masked += this.text.slice(unread, begin)
        + maskedRange(this.text, begin, end, directive ? MASKED_DIRECTIVE_CHARACTER : MASKED_ACTION_CHARACTER);
      unread = end;
      cursor = end;
    }
    masked += this.text.slice(unread);
    for (const open of this.openBlocks) this.reportOpen(open);
    this.problems.sort((first, second) =>
      first.position.line - second.position.line || first.position.column - second.position.column);
    return new TemplateReading(masked, this.actions, this.blocks, this.variables, this.templateVariables, this.problems);
  }

  /** Reads the action from `begin` to `end`, and whether it is a directive. */
  readAction(begin, finish, end) {
    const { left } = this.template.delimiters;
    const actionText = this.text.slice(begin, end);
    const rawBody = this.text.slice(begin + left.length, finish);
    const leadingSpaces = rawBody.length - rawBody.replace(/^ +/u, '').length;
    const body = rawBody.replace(/^ +| +$/gu, '');
    const bodyOffset = begin + left.length + leadingSpaces;
    const directive = directiveKindOf(body, this.template);
    this.readTemplateVariables(body, bodyOffset);
    const action = new TemplateAction(this.positionAt(begin), actionText, directive !== null);
    this.actions.push(action);
    if (directive === null) {
      this.readVariables(body, bodyOffset, null);
      return false;
    }
    this.readDirective(directive, action, body, bodyOffset);
    return true;
  }

  readDirective(directive, action, body, bodyOffset) {
    if (directive.kind === 'loop') {
      const { start } = directive;
      const groupBegin = start.indices.groups.variables[0];
      const groupEnd = start.indices.groups.variables[1];
      this.readVariables(body, bodyOffset, [groupBegin, groupEnd]);
      this.open(new TemplateLoop(action, start.groups.variables.split(', ')));
    } else if (directive.kind === 'if') {
      this.readVariables(body, bodyOffset, null);
      this.open(new TemplateConditional(action));
    } else if (directive.kind === 'else_if' || directive.kind === 'else') {
      this.readVariables(body, bodyOffset, null);
      this.readBranch(directive.kind, action);
    } else {
      this.close(action, directive.word);
    }
  }

  readBranch(kind, action) {
    const innermost = this.openBlocks[this.openBlocks.length - 1];
    const quoted = codeSpan(action.text);
    if (!(innermost instanceof TemplateConditional)) {
      this.problems.push(Diagnostic.at(action.position, `${quoted} belongs to no conditional`));
      return;
    }
    const elseAction = this.elseActions.get(innermost);
    if (elseAction !== undefined) {
      const message = kind === 'else_if'
        ? `${quoted} comes after the conditional's ${codeSpan(elseAction.text)}`
        : `${quoted} is the conditional's second ${codeSpan(this.template.conditional.else)}`;
      this.problems.push(Diagnostic.at(action.position, message));
      return;
    }
    innermost.branches.push({ opening: action, blocks: [] });
    if (kind === 'else') this.elseActions.set(innermost, action);
  }

  open(block) {
    const innermost = this.openBlocks[this.openBlocks.length - 1];
    if (innermost === undefined) this.blocks.push(block);
    else if (innermost instanceof TemplateConditional) innermost.branches[innermost.branches.length - 1].blocks.push(block);
    else innermost.blocks.push(block);
    this.openBlocks.push(block);
  }

  close(action, closingWord) {
    const innermost = this.openBlocks[this.openBlocks.length - 1];
    if (innermost === undefined) {
      this.problems.push(Diagnostic.at(action.position, `${codeSpan(action.text)} closes no conditional or loop`));
      return;
    }
    if (closingWord !== this.endWordOf(innermost)) {
      this.problems.push(Diagnostic.at(
        action.position,
        `${codeSpan(action.text)} does not close ${codeSpan(this.openingOf(innermost).text)}`,
      ));
      return;
    }
    this.openBlocks.pop();
    innermost.closing = action;
  }

  endWordOf(block) {
    return block instanceof TemplateLoop ? this.template.loop.end : this.template.conditional.end;
  }

  openingOf(block) {
    return block instanceof TemplateLoop ? block.opening : block.branches[0].opening;
  }

  reportOpen(open) {
    const { left, right } = this.template.delimiters;
    const opening = this.openingOf(open);
    this.problems.push(Diagnostic.at(opening.position, `${codeSpan(opening.text)} is never closed`)
      .withHelp(`close it with ${codeSpan(`${left} ${this.endWordOf(open)} ${right}`)}`));
  }

  /** Reads each loop variable `body` holds, outside `skipped`, a range of the body to pass over. */
  readVariables(body, bodyOffset, skipped) {
    for (const match of body.matchAll(this.variablePattern)) {
      if (match[0] === '') continue;
      if (skipped !== null && match.index >= skipped[0] && match.index < skipped[1]) continue;
      const position = this.positionAt(bodyOffset + match.index);
      const loop = this.bindingLoop(match[0]);
      this.variables.push({ position, name: match[0], loop });
      if (loop === null) {
        this.problems.push(Diagnostic.at(position, `${codeSpan(match[0])} is bound by no loop around it`));
      }
    }
  }

  /** Reads each template variable `body` holds where no loop variable overlaps it. */
  readTemplateVariables(body, bodyOffset) {
    const loopSpans = [...body.matchAll(this.variablePattern)]
      .filter((match) => match[0] !== '')
      .map((match) => [match.index, match.index + match[0].length]);
    for (const match of body.matchAll(this.templateVariablePattern)) {
      if (match[0] === '') continue;
      const matchEnd = match.index + match[0].length;
      if (loopSpans.some(([spanBegin, spanEnd]) => spanBegin < matchEnd && match.index < spanEnd)) continue;
      this.templateVariables.push({ position: this.positionAt(bodyOffset + match.index), name: match[0] });
    }
  }

  bindingLoop(name) {
    for (let index = this.openBlocks.length - 1; index >= 0; index -= 1) {
      const block = this.openBlocks[index];
      if (block instanceof TemplateLoop && block.variables.includes(name)) return block;
    }
    return null;
  }
}

/**
 * The reading of the template `text` at `file`, read by `template`, a template
 * definition: its masked text, actions, conditionals and loops, loop variables,
 * and every problem at its directive or action.
 */
function readTemplate(file, text, template) {
  return new TemplateReader(file, text, {
    ...template,
    loop: { ...template.loop, start: new RegExp(template.loop.start.source, 'du') },
  }).read();
}

module.exports = { readTemplate, TemplateReading, TemplateConditional, TemplateLoop, TemplateAction };
