package yamldslspec

import (
	"reflect"
	"slices"
	"testing"
)

func TestAnAlternativeBesideAnUnknownValueIsReportedWhenEveryFormFailsAKnownCheck(testingContext *testing.T) {
	schemaText := `{"additionalProperties": {"type": "object", "oneOf": [
	  {"required": ["arn"]},
	  {"required": ["size"], "properties": {"size": {"type": "integer"}}},
	  {"anyOf": [{"required": ["name"]}, {"required": ["region"], "properties": {"region": {"type": "integer"}}}]}
	]}}`
	documentText := "known: {zone: mock}\nhidden: {zone: mock, size: mock}\nnested: {region: mock}\n"
	unknownPointers := []string{"/known/zone", "/hidden/zone", "/hidden/size", "/nested/region"}
	documentNode, readProblem := ParseYAML("mock.yml", documentText, nil)
	if readProblem != nil {
		testingContext.Fatal(readProblem)
	}
	documentValue := withValuesRead(
		ResolvedValueFromNode(documentNode),
		"",
		nil,
		func(valuePointer string, writtenValue ResolvedValue) (ResolvedValue, bool) {
			if slices.Contains(unknownPointers, valuePointer) {
				return unknownReference(writtenValue), true
			}
			return ResolvedValue{}, false
		})
	loadedSchema, loadError := NewSchemaCache().Load("mock.schema.json", schemaText)
	if loadError != nil {
		testingContext.Fatal(loadError)
	}
	subjectPointers := []string{}
	for _, subjectProblem := range loadedSchema.Problems(documentValue, ValidationRules{}) {
		subjectPointers = append(subjectPointers, subjectProblem.Subject)
	}
	if !reflect.DeepEqual(subjectPointers, []string{"/known"}) {
		testingContext.Errorf("subjects %v", subjectPointers)
	}
}
