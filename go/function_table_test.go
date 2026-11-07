package yamldslspec

import (
	"reflect"
	"testing"
)

func mockMarker() MarkerFunction {
	splatOperator := "*"
	return MarkerFunction{CallMarker: "fn.", SplatOperator: &splatOperator, CallsWithoutNameAllowed: true}
}

func TestNamedAndNamelessCallsAreParsed(testingContext *testing.T) {
	namedCall, isWritten := ParseCallKey("tags fn.merge*", mockMarker())
	if !isWritten || namedCall.Name == nil || *namedCall.Name != "tags" || namedCall.Function != "merge" || !namedCall.Splat {
		testingContext.Errorf("named call %+v", namedCall)
	}
	namelessCall, isWritten := ParseCallKey("fn.loop", mockMarker())
	if !isWritten || namelessCall.Name != nil || namelessCall.Function != "loop" || namelessCall.Splat {
		testingContext.Errorf("nameless call %+v", namelessCall)
	}
	if _, isWritten := ParseCallKey("plain key", mockMarker()); isWritten {
		testingContext.Error("a plain key writes a call")
	}
}

func TestMacroRolesAreReadFromTheTable(testingContext *testing.T) {
	functionTable, tableError := FunctionTableFromSchema(map[string]any{
		"x-yaml-dsl-functions": map[string]any{
			"loop": map[string]any{
				"arguments": []any{
					map[string]any{"name": "in", "repeated": false},
					map[string]any{"name": "body", "repeated": false},
					map[string]any{"name": "for", "repeated": false, "default": []any{"it"}},
				},
				"macro": map[string]any{"kind": "loop", "loop_variables": "for", "collection": "in", "body": "body"},
			},
		},
	})
	if tableError != nil {
		testingContext.Fatal(tableError)
	}
	loopEntry, isFound := functionTable.Entry("loop")
	if !isFound || loopEntry.Macro == nil || loopEntry.Macro.Kind != MacroKindLoop {
		testingContext.Fatalf("entry %+v", loopEntry)
	}
	if collectionArgument, _ := loopEntry.Macro.Role("collection"); collectionArgument != "in" {
		testingContext.Errorf("collection is %q", collectionArgument)
	}
	if !loopEntry.Arguments[2].HasDefault || !reflect.DeepEqual(loopEntry.Arguments[2].DefaultValue, []any{"it"}) {
		testingContext.Errorf("default %+v", loopEntry.Arguments[2])
	}
}

func TestDeprecatedFormsAreReadUntilTheNextMajor(testingContext *testing.T) {
	functionTable, tableError := FunctionTableFromSchema(map[string]any{
		"x-yaml-dsl-functions": map[string]any{
			"join": []any{map[string]any{"name": "parts", "repeated": true}},
			"loop": map[string]any{
				"arguments": []any{map[string]any{"name": "for"}},
				"macro":     map[string]any{"kind": "loop", "loop_variable": "for"},
			},
		},
	})
	if tableError != nil {
		testingContext.Fatal(tableError)
	}
	joinEntry, _ := functionTable.Entry("join")
	if joinEntry.Arguments[0].Name != "parts" || !joinEntry.Arguments[0].Repeated || joinEntry.Macro != nil {
		testingContext.Errorf("join %+v", joinEntry)
	}
	loopEntry, _ := functionTable.Entry("loop")
	if loopVariables, _ := loopEntry.Macro.Role("loop_variables"); loopVariables != "for" {
		testingContext.Errorf("loop variables %q", loopVariables)
	}
}

func TestASuffixedMacroNameFindsItsEntry(testingContext *testing.T) {
	functionTable, tableError := FunctionTableFromSchema(map[string]any{
		"x-yaml-dsl-functions": map[string]any{
			"merge": map[string]any{
				"arguments": []any{map[string]any{"name": "content"}},
				"macro":     map[string]any{"kind": "merge", "name_suffix_pattern": "[0-9]+"},
			},
		},
	})
	if tableError != nil {
		testingContext.Fatal(tableError)
	}
	if _, isFound := functionTable.Entry("merge2"); !isFound {
		testingContext.Error("a suffixed name finds no entry")
	}
	if _, isFound := functionTable.Entry("mergex"); isFound {
		testingContext.Error("a name the suffix pattern refuses finds an entry")
	}
	emptyTable, emptyError := FunctionTableFromSchema(map[string]any{"type": "object"})
	if _, isFound := emptyTable.Entry("merge"); emptyError != nil || isFound {
		testingContext.Error("a schema without a table reads to a table with entries")
	}
}
