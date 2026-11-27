package yamldslspec

import (
	"strings"
	"testing"
)

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
