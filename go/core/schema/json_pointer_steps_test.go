package schema

import (
	"reflect"
	"testing"
)

func TestAPointerIsReadToItsUnescapedStepsElseWhyItIsNoPointer(testingContext *testing.T) {
	expectedSteps := map[string][]string{
		"":             {},
		"/a~1b/~0c/0/": {"a/b", "~c", "0", ""},
	}
	for jsonPointer, expectedStep := range expectedSteps {
		if pointerSteps, stepsError := JSONPointerSteps(jsonPointer); stepsError != nil ||
			!reflect.DeepEqual(pointerSteps, expectedStep) {
			testingContext.Fatalf("%q reads to %q, %v", jsonPointer, pointerSteps, stepsError)
		}
	}
	expectedErrors := map[string]string{
		"a/b":  "`a/b` is no JSON pointer: a pointer starts with `/`",
		"/a~2": "`/a~2` is no JSON pointer: `~` is followed by `0` or `1` in one",
	}
	for jsonPointer, expectedError := range expectedErrors {
		if _, stepsError := JSONPointerSteps(jsonPointer); stepsError == nil || stepsError.Error() != expectedError {
			testingContext.Fatalf("%q reads to %v", jsonPointer, stepsError)
		}
	}
}
