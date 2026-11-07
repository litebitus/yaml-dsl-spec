package yamldslspec

import (
	"reflect"
	"strconv"
	"testing"
)

func concretePath(stepTexts ...string) []PathStep {
	var pathSteps []PathStep
	for _, stepText := range stepTexts {
		if stepIndex, indexError := strconv.Atoi(stepText); indexError == nil {
			pathSteps = append(pathSteps, IndexStep(stepIndex))
		} else {
			pathSteps = append(pathSteps, KeyStep(stepText))
		}
	}
	return pathSteps
}

func parsedPattern(testingContext *testing.T, pathText string, skipKeys ...string) PathPattern {
	pathPattern, pathProblem := ParsePathPattern(pathText, skipKeys)
	if pathProblem != nil {
		testingContext.Fatal(pathProblem)
	}
	return pathPattern
}

func TestANameAndAWildcardSelectOneLevelEach(testingContext *testing.T) {
	entryPattern := parsedPattern(testingContext, "$.*.*", "locals")
	if !entryPattern.Selects(concretePath("queue", "orders")) || entryPattern.Selects(concretePath("locals", "defaults")) ||
		entryPattern.Selects(concretePath("queue")) {
		testingContext.Error("wildcards select wrongly")
	}
	if !parsedPattern(testingContext, "$.locals.*").Selects(concretePath("locals", "defaults")) {
		testingContext.Error("a name step does not select")
	}
}

func TestADescendantStepSelectsAtAnyDepth(testingContext *testing.T) {
	if !parsedPattern(testingContext, "$..['Fn::Transform']").Selects(concretePath("Resources", "Bucket", "Fn::Transform")) {
		testingContext.Error("a quoted descendant name does not select")
	}
	belowPattern := parsedPattern(testingContext, "$.cloud..*")
	if !belowPattern.Selects(concretePath("cloud", "aws_regions", "0")) || belowPattern.Selects(concretePath("cloud")) {
		testingContext.Error("a descendant wildcard selects wrongly")
	}
	if !parsedPattern(testingContext, "$.cases[*].steps[*].*").Selects(concretePath("cases", "0", "steps", "2", "call")) {
		testingContext.Error("bracket wildcards do not select list items")
	}
}

func TestSelectAnswersEveryNodeInDocumentOrder(testingContext *testing.T) {
	documentValue := ResolvedValueFromNode(parsedNode(testingContext, "queue: {b: 1, a: 2}\nlocals: {c: 3}\n", nil))
	var selectedPaths [][]PathStep
	for _, selectedValue := range parsedPattern(testingContext, "$.*.*", "locals").Select(documentValue) {
		selectedPaths = append(selectedPaths, selectedValue.Path)
	}
	if !reflect.DeepEqual(selectedPaths, [][]PathStep{concretePath("queue", "b"), concretePath("queue", "a")}) {
		testingContext.Errorf("selected %v", selectedPaths)
	}
}

func TestAPathIsReadOnlyInTheSubsetTheSchemaStates(testingContext *testing.T) {
	for _, acceptedText := range []string{"$.a", "$.*", "$[*]", "$['a.b']", "$..a", "$..['a']", "$.a..*", "$.a*b"} {
		if _, pathProblem := ParsePathPattern(acceptedText, nil); pathProblem != nil {
			testingContext.Errorf("%s refused", acceptedText)
		}
	}
	refusedTexts := []string{
		"$abc",
		"$*",
		"$.a.['b']",
		"$..[*]",
		"$..*.a",
		"$.*a",
		"$['']",
		"$['a'b']",
		`$["a"]`,
		"$.",
	}
	for _, refusedText := range refusedTexts {
		if _, pathProblem := ParsePathPattern(refusedText, nil); pathProblem == nil {
			testingContext.Errorf("%s read", refusedText)
		}
	}
	_, pathProblem := ParsePathPattern("$*", nil)
	if pathProblem.Message != "`$*` is not a path of the supported JSONPath subset" {
		testingContext.Errorf("message %q", pathProblem.Message)
	}
}

func TestTheRootSelectsTheDocumentAndASetMatchesAsOne(testingContext *testing.T) {
	rootPattern := parsedPattern(testingContext, "$")
	if !rootPattern.Selects(nil) || !rootPattern.Covers(concretePath("any", "thing")) {
		testingContext.Error("the root does not select the document")
	}
	pathSet, pathProblem := ParsePathSet([]PathEntry{{Path: "$.a"}, {Path: "$.b.*", SkipKeys: []string{"skip"}}})
	if pathProblem != nil {
		testingContext.Fatal(pathProblem)
	}
	if !pathSet.Selects(concretePath("b", "c")) || pathSet.Selects(concretePath("b", "skip")) ||
		!pathSet.Covers(concretePath("a", "d")) {
		testingContext.Error("a path set matches wrongly")
	}
}
