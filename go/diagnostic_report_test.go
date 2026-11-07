package yamldslspec

import "testing"

func TestNotesAreSetOnlyWhenPresentAndACopyKeepsItsOwn(testingContext *testing.T) {
	plainProblem := DiagnosticAt(NewPosition("mock.yml", 2, 3), "mock problem")
	if plainProblem.WithSchema(nil).WithHelp(nil).Notes != nil {
		testingContext.Error("absent notes create notes")
	}
	labeledProblem := plainProblem.WithLabel("mock label")
	ruledProblem := labeledProblem.WithRule("mock rule")
	if labeledProblem.Notes.Rule != nil || *ruledProblem.Notes.Label != "mock label" || *ruledProblem.Notes.Rule != "mock rule" {
		testingContext.Error("a builder changed the diagnostic it was called on")
	}
	if ruledProblem.String() != "mock.yml:2:3: mock problem" || UnplacedDiagnostic("mock problem").String() != "mock problem" {
		testingContext.Errorf("written as %q", ruledProblem.String())
	}
}

func TestAResolvedValueKeepsEveryPosition(testingContext *testing.T) {
	documentValue := ResolvedValueFromNode(parsedNode(testingContext, "a:\n  - b\n", nil))
	listValue, _ := documentValue.Get("a")
	if listValue.Kind != ResolvedList || listValue.Items[0].Position != NewPosition("mock.yml", 2, 5) {
		testingContext.Errorf("list %+v", listValue)
	}
	if itemText, isText := listValue.Items[0].AsString(); !isText || itemText != "b" {
		testingContext.Errorf("item %q", itemText)
	}
	localValue := StringValue("mock", FileStartPosition("mock.yml"))
	localTarget := ReferenceTarget{Kind: TargetLocal, LocalValue: &localValue}
	localReference := ResolvedValue{Kind: ResolvedReference, Reference: &Reference{Target: localTarget}}
	if scalarText, _ := localReference.ScalarText(); scalarText != "mock" || localReference.LocalValue().Text != "mock" {
		testingContext.Error("a local reference does not read as its value")
	}
}
