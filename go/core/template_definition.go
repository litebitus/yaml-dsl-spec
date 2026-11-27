package core

import (
	"fmt"
	"slices"
)

// LoopVariablesGroup is the named group of a loop's start pattern that captures the loop variables.
const LoopVariablesGroup = "variables"

// TemplateDefinition is a template block of `yaml-dsl.yml`, its fields named as the standard names them, its patterns compiled.
type TemplateDefinition struct {
	ID                      string
	DSL                     string
	FileIncludes            []string
	FileExcludes            []string
	Delimiters              TemplateDelimiters
	Conditional             TemplateConditionalWords
	Loop                    TemplateLoopSyntax
	Variable                string
	SchemaSearchPaths       []string
	loopStartPattern        *CompiledPattern
	loopVariablePattern     *CompiledPattern
	templateVariablePattern *CompiledPattern
}

// TemplateDelimiters is the text a template's action starts and ends with.
type TemplateDelimiters struct {
	Left  string
	Right string
}

// TemplateConditionalWords is the word each part of a template's conditional starts with.
type TemplateConditionalWords struct {
	If     string
	ElseIf string
	Else   string
	End    string
}

// TemplateLoopSyntax is how a template's loop is written: its opening pattern, its closing word and its loop variable's pattern.
type TemplateLoopSyntax struct {
	Start    string
	End      string
	Variable string
}

// TemplateDefinitionFromJSON reads a template block with its patterns compiled, or says why it is none.
func TemplateDefinitionFromJSON(templateBlock any) (TemplateDefinition, error) {
	blockReader := &fieldReader{}
	blockFields := blockReader.objectOf(templateBlock, "")
	delimiterFields := blockReader.objectFields(blockFields, "delimiters")
	conditionalFields := blockReader.objectFields(blockFields, "conditional")
	loopFields := blockReader.objectFields(blockFields, "loop")
	templateDefinition := TemplateDefinition{
		ID:           blockReader.requiredText(blockFields, "id"),
		DSL:          blockReader.requiredText(blockFields, "dsl"),
		FileIncludes: blockReader.requiredTexts(blockFields, "file_includes"),
		FileExcludes: blockReader.textList(blockFields, "file_excludes"),
		Delimiters: TemplateDelimiters{
			Left:  blockReader.requiredText(delimiterFields, "left"),
			Right: blockReader.requiredText(delimiterFields, "right"),
		},
		Conditional: TemplateConditionalWords{
			If:     blockReader.requiredText(conditionalFields, "if"),
			ElseIf: blockReader.requiredText(conditionalFields, "else_if"),
			Else:   blockReader.requiredText(conditionalFields, "else"),
			End:    blockReader.requiredText(conditionalFields, "end"),
		},
		Loop: TemplateLoopSyntax{
			Start:    blockReader.requiredText(loopFields, "start"),
			End:      blockReader.requiredText(loopFields, "end"),
			Variable: blockReader.requiredText(loopFields, "variable"),
		},
		Variable:          blockReader.requiredText(blockFields, "variable"),
		SchemaSearchPaths: blockReader.textList(blockFields, "schema_search_paths"),
	}
	if blockReader.firstFailure != nil {
		return TemplateDefinition{}, blockReader.firstFailure
	}
	loopStartPattern, startError := CompilePattern(templateDefinition.Loop.Start)
	if startError != nil {
		return TemplateDefinition{}, fmt.Errorf("`loop.start` is not a regular expression: %w", startError)
	}
	if !slices.Contains(loopStartPattern.GroupNames(), LoopVariablesGroup) {
		return TemplateDefinition{}, fmt.Errorf("`loop.start` has no `(?<%s>...)` group", LoopVariablesGroup)
	}
	loopVariablePattern, variableError := CompilePattern(templateDefinition.Loop.Variable)
	if variableError != nil {
		return TemplateDefinition{}, fmt.Errorf("`loop.variable` is not a regular expression: %w", variableError)
	}
	templateVariablePattern, templateVariableError := CompilePattern(templateDefinition.Variable)
	if templateVariableError != nil {
		return TemplateDefinition{}, fmt.Errorf("`variable` is not a regular expression: %w", templateVariableError)
	}
	templateDefinition.loopStartPattern = loopStartPattern
	templateDefinition.loopVariablePattern = loopVariablePattern
	templateDefinition.templateVariablePattern = templateVariablePattern
	return templateDefinition, nil
}
