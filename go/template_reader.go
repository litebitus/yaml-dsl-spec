package yamldslspec

import (
	"slices"
	"strings"
)

// TemplateReading is a template read: its masked text, its actions, its conditionals and loops, its loop variables and its problems.
type TemplateReading struct {
	MaskedText    string
	Actions       []TemplateAction
	Blocks        []TemplateBlock
	LoopVariables []TemplateLoopVariable
	Problems      []Diagnostic
}

// TemplateAction is an action of a template: where it starts, its text and whether it is a directive.
type TemplateAction struct {
	Position    Position
	Text        string
	IsDirective bool
}

// TemplateBlock is a conditional or a loop, as written in a template or in a body of one.
type TemplateBlock struct {
	Conditional *TemplateConditional
	Loop        *TemplateLoop
}

// TemplateConditional is a template's conditional: its branches and the action that closes it, none when it is left open.
type TemplateConditional struct {
	Branches []TemplateBranch
	Closing  *Position
}

// TemplateBranch is a branch of a conditional: the action that opens it and its body's conditionals and loops.
type TemplateBranch struct {
	Opening Position
	Blocks  []TemplateBlock
}

// TemplateLoop is a template's loop: the action that opens it, the loop variables it binds, its body's conditionals and loops, and the action that closes it, none when it is left open.
type TemplateLoop struct {
	Opening   Position
	Variables []string
	Blocks    []TemplateBlock
	Closing   *Position
}

// TemplateLoopVariable is a loop variable read inside an action, with the opening of the loop that binds it, none when no loop does.
type TemplateLoopVariable struct {
	Position Position
	Name     string
	Loop     *Position
}

type directiveKind int

const (
	notDirective directiveKind = iota
	conditionalOpening
	conditionalFurtherBranch
	conditionalLastBranch
	directiveClosing
	loopOpening
)

type openTemplateBlock struct {
	conditional     *openTemplateConditional
	loop            *openTemplateLoop
	openingText     string
	closingWord     string
	openingPosition Position
}

type openTemplateConditional struct {
	branches       []*openTemplateBranch
	closing        *Position
	lastBranchText *string
}

type openTemplateBranch struct {
	opening Position
	blocks  []*openTemplateBlock
}

type openTemplateLoop struct {
	opening   Position
	variables []string
	blocks    []*openTemplateBlock
	closing   *Position
}

type templateReader struct {
	fileName           string
	templateRunes      []rune
	templateDefinition TemplateDefinition
	lineStarts         []int
	maskedRunes        []rune
	reading            TemplateReading
	rootBlocks         []*openTemplateBlock
	openBlocks         []*openTemplateBlock
}

// ReadTemplate reads a template by its template language: its actions masked, its conditionals and loops read, and every problem at its directive or action.
func ReadTemplate(fileName, templateText string, templateDefinition TemplateDefinition) TemplateReading {
	templateRunes := []rune(templateText)
	reader := &templateReader{
		fileName:           fileName,
		templateRunes:      templateRunes,
		templateDefinition: templateDefinition,
		lineStarts:         lineStartOffsets(templateRunes),
		maskedRunes:        slices.Clone(templateRunes),
		reading:            TemplateReading{Actions: []TemplateAction{}, LoopVariables: []TemplateLoopVariable{}, Problems: []Diagnostic{}},
	}
	reader.readActions()
	reader.reportOpenBlocks()
	slices.SortStableFunc(reader.reading.Problems, func(firstProblem, secondProblem Diagnostic) int {
		if firstProblem.Position.Line != secondProblem.Position.Line {
			return firstProblem.Position.Line - secondProblem.Position.Line
		}
		return firstProblem.Position.Column - secondProblem.Position.Column
	})
	reader.reading.MaskedText = string(reader.maskedRunes)
	reader.reading.Blocks = closedBlocks(reader.rootBlocks)
	return reader.reading
}

func lineStartOffsets(templateRunes []rune) []int {
	lineStarts := []int{0}
	for runeOffset, templateRune := range templateRunes {
		if templateRune == '\n' {
			lineStarts = append(lineStarts, runeOffset+1)
		}
	}
	return lineStarts
}

func (reader *templateReader) positionAt(runeOffset int) Position {
	lineIndex, isExact := slices.BinarySearch(reader.lineStarts, runeOffset)
	if !isExact {
		lineIndex--
	}
	return NewPosition(reader.fileName, lineIndex+1, runeOffset-reader.lineStarts[lineIndex]+1)
}

func indexOfRunes(templateRunes, soughtRunes []rune, fromOffset int) int {
	for runeOffset := fromOffset; runeOffset+len(soughtRunes) <= len(templateRunes); runeOffset++ {
		if slices.Equal(templateRunes[runeOffset:runeOffset+len(soughtRunes)], soughtRunes) {
			return runeOffset
		}
	}
	return -1
}

func (reader *templateReader) readActions() {
	leftRunes := []rune(reader.templateDefinition.Delimiters.Left)
	rightRunes := []rune(reader.templateDefinition.Delimiters.Right)
	cursorOffset := 0
	for {
		actionStart := indexOfRunes(reader.templateRunes, leftRunes, cursorOffset)
		if actionStart < 0 {
			return
		}
		bodyEnd := indexOfRunes(reader.templateRunes, rightRunes, actionStart+len(leftRunes))
		if bodyEnd < 0 {
			rightHelp := "no " + CodeSpan(reader.templateDefinition.Delimiters.Right) + " after it"
			reader.reportProblem(DiagnosticAt(reader.positionAt(actionStart),
				CodeSpan(reader.templateDefinition.Delimiters.Left)+" opens an action that is never closed").WithLabel(rightHelp))
			return
		}
		actionEnd := bodyEnd + len(rightRunes)
		writtenBody := string(reader.templateRunes[actionStart+len(leftRunes) : bodyEnd])
		actionBody := strings.Trim(writtenBody, " ")
		bodyOffset := actionStart + len(leftRunes) + len([]rune(writtenBody)) - len([]rune(strings.TrimLeft(writtenBody, " ")))
		reader.readAction(actionStart, actionEnd, actionBody, bodyOffset)
		cursorOffset = actionEnd
	}
}

func (reader *templateReader) readAction(actionStart, actionEnd int, actionBody string, bodyOffset int) {
	actionText := string(reader.templateRunes[actionStart:actionEnd])
	actionPosition := reader.positionAt(actionStart)
	variablesSpan, variablesText, isLoopOpening := reader.templateDefinition.loopStartPattern.groupSpanOfLeadingMatch(actionBody, loopVariablesGroup)
	readKind, directiveWord := loopOpening, ""
	if !isLoopOpening {
		readKind, directiveWord = reader.directiveKindOf(actionBody)
	}
	isDirective := readKind != notDirective
	reader.reading.Actions = append(reader.reading.Actions, TemplateAction{Position: actionPosition, Text: actionText, IsDirective: isDirective})
	maskRune := '_'
	if isDirective {
		maskRune = ' '
	}
	for runeOffset := actionStart; runeOffset < actionEnd; runeOffset++ {
		if reader.maskedRunes[runeOffset] != '\n' {
			reader.maskedRunes[runeOffset] = maskRune
		}
	}
	quotedAction := CodeSpan(actionText)
	switch readKind {
	case loopOpening:
		reader.readLoopVariables(actionBody, bodyOffset, &variablesSpan)
		openedLoop := &openTemplateLoop{opening: actionPosition, variables: strings.Split(variablesText, ", ")}
		reader.openBlock(&openTemplateBlock{loop: openedLoop, openingText: actionText,
			closingWord: reader.templateDefinition.Loop.End, openingPosition: actionPosition})
	case conditionalOpening:
		reader.readLoopVariables(actionBody, bodyOffset, nil)
		openedConditional := &openTemplateConditional{branches: []*openTemplateBranch{{opening: actionPosition}}}
		reader.openBlock(&openTemplateBlock{conditional: openedConditional, openingText: actionText,
			closingWord: reader.templateDefinition.Conditional.End, openingPosition: actionPosition})
	case conditionalFurtherBranch, conditionalLastBranch:
		reader.readLoopVariables(actionBody, bodyOffset, nil)
		reader.readBranch(readKind, actionPosition, actionText, quotedAction)
	case directiveClosing:
		if len(reader.openBlocks) == 0 {
			reader.reportProblem(DiagnosticAt(actionPosition, quotedAction+" closes no conditional or loop"))
			return
		}
		closedBlock := reader.openBlocks[len(reader.openBlocks)-1]
		if directiveWord != closedBlock.closingWord {
			reader.reportProblem(DiagnosticAt(actionPosition, quotedAction+" does not close "+CodeSpan(closedBlock.openingText)))
			return
		}
		reader.openBlocks = reader.openBlocks[:len(reader.openBlocks)-1]
		closingPosition := actionPosition
		if closedBlock.conditional != nil {
			closedBlock.conditional.closing = &closingPosition
		} else {
			closedBlock.loop.closing = &closingPosition
		}
	default:
		reader.readLoopVariables(actionBody, bodyOffset, nil)
	}
}

func (reader *templateReader) directiveKindOf(actionBody string) (directiveKind, string) {
	conditionalWords := reader.templateDefinition.Conditional
	directiveWords := []struct {
		directiveWord string
		readKind      directiveKind
	}{
		{conditionalWords.If, conditionalOpening},
		{conditionalWords.ElseIf, conditionalFurtherBranch},
		{conditionalWords.Else, conditionalLastBranch},
		{conditionalWords.End, directiveClosing},
		{reader.templateDefinition.Loop.End, directiveClosing},
	}
	readKind, fittingWord := notDirective, ""
	for _, candidateWord := range directiveWords {
		isFitting := actionBody == candidateWord.directiveWord || strings.HasPrefix(actionBody, candidateWord.directiveWord+" ")
		if isFitting && (readKind == notDirective || len(candidateWord.directiveWord) > len(fittingWord)) {
			readKind, fittingWord = candidateWord.readKind, candidateWord.directiveWord
		}
	}
	return readKind, fittingWord
}

func (reader *templateReader) readBranch(readKind directiveKind, actionPosition Position, actionText, quotedAction string) {
	var innermostBlock *openTemplateBlock
	if len(reader.openBlocks) > 0 {
		innermostBlock = reader.openBlocks[len(reader.openBlocks)-1]
	}
	if innermostBlock == nil || innermostBlock.conditional == nil {
		reader.reportProblem(DiagnosticAt(actionPosition, quotedAction+" belongs to no conditional"))
		return
	}
	openConditional := innermostBlock.conditional
	if openConditional.lastBranchText != nil {
		if readKind == conditionalFurtherBranch {
			reader.reportProblem(DiagnosticAt(actionPosition,
				quotedAction+" comes after the conditional's "+CodeSpan(*openConditional.lastBranchText)))
		} else {
			reader.reportProblem(DiagnosticAt(actionPosition,
				quotedAction+" is the conditional's second "+CodeSpan(reader.templateDefinition.Conditional.Else)))
		}
		return
	}
	openConditional.branches = append(openConditional.branches, &openTemplateBranch{opening: actionPosition})
	if readKind == conditionalLastBranch {
		openConditional.lastBranchText = &actionText
	}
}

func (reader *templateReader) openBlock(openedBlock *openTemplateBlock) {
	if len(reader.openBlocks) == 0 {
		reader.rootBlocks = append(reader.rootBlocks, openedBlock)
	} else {
		innermostBlock := reader.openBlocks[len(reader.openBlocks)-1]
		if innermostBlock.conditional != nil {
			lastBranch := innermostBlock.conditional.branches[len(innermostBlock.conditional.branches)-1]
			lastBranch.blocks = append(lastBranch.blocks, openedBlock)
		} else {
			innermostBlock.loop.blocks = append(innermostBlock.loop.blocks, openedBlock)
		}
	}
	reader.openBlocks = append(reader.openBlocks, openedBlock)
}

func (reader *templateReader) readLoopVariables(actionBody string, bodyOffset int, bindingSpan *runeSpan) {
	bodyRunes := []rune(actionBody)
	for _, variableSpan := range reader.templateDefinition.loopVariablePattern.everyMatchSpan(actionBody) {
		isBinding := bindingSpan != nil && variableSpan.runeOffset >= bindingSpan.runeOffset &&
			variableSpan.runeOffset < bindingSpan.runeOffset+bindingSpan.runeLength
		if isBinding || variableSpan.runeLength == 0 {
			continue
		}
		variableName := string(bodyRunes[variableSpan.runeOffset : variableSpan.runeOffset+variableSpan.runeLength])
		variablePosition := reader.positionAt(bodyOffset + variableSpan.runeOffset)
		bindingLoop := reader.loopBinding(variableName)
		reader.reading.LoopVariables = append(reader.reading.LoopVariables,
			TemplateLoopVariable{Position: variablePosition, Name: variableName, Loop: bindingLoop})
		if bindingLoop == nil {
			reader.reportProblem(DiagnosticAt(variablePosition, CodeSpan(variableName)+" is bound by no loop around it"))
		}
	}
}

func (reader *templateReader) loopBinding(variableName string) *Position {
	for blockIndex := len(reader.openBlocks) - 1; blockIndex >= 0; blockIndex-- {
		openLoop := reader.openBlocks[blockIndex].loop
		if openLoop != nil && slices.Contains(openLoop.variables, variableName) {
			loopOpening := openLoop.opening
			return &loopOpening
		}
	}
	return nil
}

func (reader *templateReader) reportOpenBlocks() {
	delimiters := reader.templateDefinition.Delimiters
	for _, openBlock := range reader.openBlocks {
		closingHelp := "close it with " + CodeSpan(delimiters.Left+" "+openBlock.closingWord+" "+delimiters.Right)
		reader.reportProblem(DiagnosticAt(openBlock.openingPosition, CodeSpan(openBlock.openingText)+" is never closed").
			WithHelp(&closingHelp))
	}
}

func (reader *templateReader) reportProblem(readProblem Diagnostic) {
	reader.reading.Problems = append(reader.reading.Problems, readProblem)
}

func closedBlocks(openBlocks []*openTemplateBlock) []TemplateBlock {
	readBlocks := []TemplateBlock{}
	for _, openBlock := range openBlocks {
		if openBlock.conditional != nil {
			readConditional := &TemplateConditional{Branches: []TemplateBranch{}, Closing: openBlock.conditional.closing}
			for _, openBranch := range openBlock.conditional.branches {
				readConditional.Branches = append(readConditional.Branches,
					TemplateBranch{Opening: openBranch.opening, Blocks: closedBlocks(openBranch.blocks)})
			}
			readBlocks = append(readBlocks, TemplateBlock{Conditional: readConditional})
			continue
		}
		readBlocks = append(readBlocks, TemplateBlock{Loop: &TemplateLoop{
			Opening:   openBlock.loop.opening,
			Variables: openBlock.loop.variables,
			Blocks:    closedBlocks(openBlock.loop.blocks),
			Closing:   openBlock.loop.closing,
		}})
	}
	return readBlocks
}
