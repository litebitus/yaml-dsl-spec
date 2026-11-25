package yamldslspec

import (
	"strings"
	"testing"
)

func mockTemplateBlock() map[string]any {
	return map[string]any{
		"id":            "mock-templates",
		"dsl":           "mock",
		"file_includes": []any{"{config_root}/templates/**"},
		"file_excludes": []any{},
		"delimiters":    map[string]any{"left": "[[", "right": "]]"},
		"conditional":   map[string]any{"if": "if", "else_if": "else if", "else": "else", "end": "end"},
		"loop": map[string]any{
			"start":    `^for (?<variables>\$[a-z_]+(, \$[a-z_]+)?) in `,
			"end":      "end",
			"variable": `\$[a-z_]+`,
		},
		"variable":            `\.[a-z_]+`,
		"schema_search_paths": []any{},
	}
}

func mockTemplateDefinition(testingContext *testing.T) TemplateDefinition {
	templateDefinition, definitionError := TemplateDefinitionFromJSON(mockTemplateBlock())
	if definitionError != nil {
		testingContext.Fatal(definitionError)
	}
	return templateDefinition
}

func TestATemplateBlockWithoutWorkingPatternsIsNone(testingContext *testing.T) {
	brokenBlocks := map[string]func(map[string]any){
		"not a regular expression": func(templateBlock map[string]any) { templateBlock["loop"].(map[string]any)["start"] = "(" },
		"no `(?<variables>...)`":   func(templateBlock map[string]any) { templateBlock["loop"].(map[string]any)["start"] = "^for " },
		"`loop.variable` is not":   func(templateBlock map[string]any) { templateBlock["loop"].(map[string]any)["variable"] = "[" },
		"`variable` is not":        func(templateBlock map[string]any) { templateBlock["variable"] = "[" },
		"missing field `dsl`":      func(templateBlock map[string]any) { delete(templateBlock, "dsl") },
		"invalid type for `left`":  func(templateBlock map[string]any) { templateBlock["delimiters"].(map[string]any)["left"] = 1 },
	}
	if _, definitionError := TemplateDefinitionFromJSON("mock"); definitionError == nil ||
		!strings.Contains(definitionError.Error(), "expected a map") {
		testingContext.Errorf("a block that is no map reads: %v", definitionError)
	}
	for expectedFailure, breakBlock := range brokenBlocks {
		templateBlock := mockTemplateBlock()
		breakBlock(templateBlock)
		if _, definitionError := TemplateDefinitionFromJSON(templateBlock); definitionError == nil ||
			!strings.Contains(definitionError.Error(), expectedFailure) {
			testingContext.Errorf("%s: %v", expectedFailure, definitionError)
		}
	}
}

func TestATemplatesProblemsComeInTheOrderTheyAreWritten(testingContext *testing.T) {
	templateText := "[[ end ]] [[ $mock_a ]]\n[[ if .mock ]]\n"
	templateRead := ReadTemplate("mock.yml", templateText, mockTemplateDefinition(testingContext))
	var problemPlaces []string
	for _, readProblem := range templateRead.Problems {
		problemPlaces = append(problemPlaces, readProblem.Position.String())
	}
	if strings.Join(problemPlaces, " ") != "mock.yml:1:1 mock.yml:1:14 mock.yml:2:1" {
		testingContext.Errorf("problems at %v", problemPlaces)
	}
}

func TestAConditionalInALoopIsReadInTheLoopsBody(testingContext *testing.T) {
	templateText := "[[ for $mock_a in .mocks ]]\n[[ if $mock_a ]]\nx: 1\n[[ end ]]\n[[ end ]]\n"
	templateRead := ReadTemplate("mock.yml", templateText, mockTemplateDefinition(testingContext))
	if len(templateRead.Problems) != 0 || len(templateRead.Blocks) != 1 || templateRead.Blocks[0].Loop == nil {
		testingContext.Fatalf("read %+v", templateRead)
	}
	innerBlocks := templateRead.Blocks[0].Loop.Blocks
	if len(innerBlocks) != 1 || innerBlocks[0].Conditional == nil || innerBlocks[0].Conditional.Closing == nil {
		testingContext.Errorf("the loop's body holds %+v", innerBlocks)
	}
	if boundLoop := templateRead.LoopVariables[0].Loop; boundLoop == nil || boundLoop.Line != 1 {
		testingContext.Errorf("the variable is bound by %v", boundLoop)
	}
}

func TestATemplateLanguageRepeatingAnEarlierOnesIDIsAProblem(testingContext *testing.T) {
	configText := strings.Join([]string{
		"version: \"1\"",
		"dsls: []",
		"templates:",
		templateEntryText("mock-a"),
		templateEntryText("mock-a"),
	}, "\n")
	_, readProblems := ReadDefinitions("yaml-dsl.yml", configText)
	if _, dslProblems := ReadDSLDefinitions("yaml-dsl.yml", configText); len(dslProblems) != len(readProblems) {
		testingContext.Errorf("reading the DSLs alone gives problems %v", dslProblems)
	}
	var problemLabels []string
	for _, readProblem := range readProblems {
		if readProblem.Notes != nil && readProblem.Notes.Label != nil {
			problemLabels = append(problemLabels, *readProblem.Notes.Label)
		}
	}
	if !strings.Contains(strings.Join(problemLabels, "|"), "an earlier template language of this file has this `id`") {
		testingContext.Errorf("problems %v", readProblems)
	}
}

func templateEntryText(templateID string) string {
	return strings.Join([]string{
		"  - id: " + templateID,
		"    dsl: mock",
		"    file_includes: []",
		"    file_excludes: []",
		"    delimiters: {left: \"[[\", right: \"]]\"}",
		"    conditional: {if: if, else_if: else if, else: else, end: end}",
		"    loop: {start: \"^for (?<variables>[a-z]+) in \", end: end, variable: \"[\"}",
		"    variable: \"[\"",
		"    schema_search_paths: []",
	}, "\n")
}

func TestAMatchOfNoTextIsNoLoopVariable(testingContext *testing.T) {
	templateBlock := mockTemplateBlock()
	templateBlock["loop"].(map[string]any)["variable"] = `(\$[a-z_]+)?`
	templateDefinition, definitionError := TemplateDefinitionFromJSON(templateBlock)
	if definitionError != nil {
		testingContext.Fatal(definitionError)
	}
	templateRead := ReadTemplate("mock.yml", "x: [[ .mock ]]\n", templateDefinition)
	if len(templateRead.LoopVariables) != 0 || len(templateRead.Problems) != 0 {
		testingContext.Errorf("read %+v and %v", templateRead.LoopVariables, templateRead.Problems)
	}
}
