package yamldslspec

import (
	"reflect"
	"testing"
)

const draft202012 = `"$schema": "https://json-schema.org/draft/2020-12/schema", `

func validatedMessages(testingContext *testing.T, schemaText, documentText string) []string {
	documentValue := ResolvedValueFromNode(parsedNode(testingContext, documentText, nil))
	loadedSchema := loadedMockSchema(testingContext, "{"+draft202012+schemaText+"}")
	return problemMessages(loadedSchema.Validate(documentValue, ValidationRules{}))
}

func TestAConstIsWrittenAsJSONInSchemaOrder(testingContext *testing.T) {
	problemTexts := validatedMessages(testingContext,
		`"properties": {"mode": {"const": {"b": [1, "x\"\n\u0001"], "a": null}}}`,
		"mode: 1\n",
	)
	wantedTexts := []string{"`mode` must be `{\"b\":[1,\"x\\\"\\n\\u0001\"],\"a\":null}`"}
	if !reflect.DeepEqual(problemTexts, wantedTexts) {
		testingContext.Errorf("messages %q", problemTexts)
	}
}

func TestAnEnumWritesEachValueAsJSON(testingContext *testing.T) {
	problemTexts := validatedMessages(testingContext,
		`"properties": {"size": {"enum": [1.5, 18446744073709551615, true, "\t"]}}`,
		"size: 2\n",
	)
	wantedTexts := []string{"`size` must be one of `1.5`, `18446744073709551615`, `true`, `\"\\t\"`"}
	if !reflect.DeepEqual(problemTexts, wantedTexts) {
		testingContext.Errorf("messages %q", problemTexts)
	}
}

func TestMapsEqualInAnyKeyOrderAndNumbersEqualByValue(testingContext *testing.T) {
	problemTexts := validatedMessages(testingContext,
		`"properties": {"tags": {"uniqueItems": true}}`,
		"tags:\n  - {a: 1, b: [2]}\n  - {b: [2.0], a: 1}\n  - x\n",
	)
	wantedTexts := []string{"`tags` must NOT have duplicate items (items ## 1 and 0 are identical)"}
	if !reflect.DeepEqual(problemTexts, wantedTexts) {
		testingContext.Errorf("messages %q", problemTexts)
	}
}

func TestADependentSchemaNamesWhatItRequiresForbidsAndReferences(testingContext *testing.T) {
	problemTexts := validatedMessages(testingContext,
		`"$defs": {"certificate": {"type": "string"}}, `+
			`"dependentSchemas": {"tls": {"required": ["cert"], "not": {"required": ["plain"]}, `+
			`"properties": {"cert": {"$ref": "#/$defs/certificate"}}}}`,
		"tls: true\n",
	)
	wantedTexts := []string{"`tls` needs `cert`; `plain` must not be set; `cert` is a `certificate`"}
	if !reflect.DeepEqual(problemTexts, wantedTexts) {
		testingContext.Errorf("messages %q", problemTexts)
	}
}

func TestADependentSchemaNamesEachAlternative(testingContext *testing.T) {
	problemTexts := validatedMessages(testingContext,
		`"$defs": {"certificate": {"type": "string"}}, `+
			`"dependentSchemas": {"tls": {"anyOf": [{"required": ["cert"]}, {"$ref": "#/$defs/certificate"}]}}`,
		"tls: true\n",
	)
	wantedTexts := []string{"`tls` needs `cert`, or a `certificate`"}
	if !reflect.DeepEqual(problemTexts, wantedTexts) {
		testingContext.Errorf("messages %q", problemTexts)
	}
}

func TestABlockFieldOfTheWrongTypeIsNotADefinition(testingContext *testing.T) {
	wrongBlocks := []struct {
		blockValue   any
		wantedReason string
	}{
		{"mock", "invalid type: expected a map"},
		{map[string]any{"id": 5, "file_includes": []any{}}, "invalid type for `id`: expected a string"},
		{map[string]any{"id": "mock", "file_includes": "*.yml"}, "invalid type for `file_includes`: expected a sequence"},
		{
			map[string]any{"id": "mock", "file_includes": []any{}, "layers": "mock"},
			"invalid type for `layers`: expected a map",
		},
		{
			map[string]any{
				"id":            "mock",
				"file_includes": []any{},
				"layers": map[string]any{
					"common_layer_discovery": "parent",
					"common_layer_files":     map[string]any{"mock": "mock.yml"},
				},
			},
			"invalid type for `common_layer_files`: expected a sequence",
		},
	}
	for _, wrongBlock := range wrongBlocks {
		_, definitionError := DSLDefinitionFromJSON(wrongBlock.blockValue)
		if definitionError == nil || definitionError.Error() != wrongBlock.wantedReason {
			testingContext.Errorf("block %v: error %v", wrongBlock.blockValue, definitionError)
		}
	}
}

func TestANodeTellsItsKind(testingContext *testing.T) {
	rootNode := parsedNode(testingContext, "mock: {}\nflag: true\n", nil)
	if !rootNode.IsMap() || !rootNode.Entries[0].Value.IsMap() || rootNode.Entries[1].Value.IsMap() {
		testingContext.Errorf("node %v", rootNode)
	}
}

func TestAResolvedScalarReadsAsText(testingContext *testing.T) {
	rootValue := ResolvedValueFromNode(parsedNode(testingContext, "empty: null\nflag: false\ncount: 3\nlist: []\n", nil))
	wantedTexts := []struct {
		scalarText string
		isScalar   bool
	}{{"", true}, {"false", true}, {"3", true}, {"", false}}
	for entryIndex, mapEntry := range rootValue.Entries {
		scalarText, isScalar := mapEntry.Value.ScalarText()
		if scalarText != wantedTexts[entryIndex].scalarText || isScalar != wantedTexts[entryIndex].isScalar {
			testingContext.Errorf("`%s`: %q %v", mapEntry.Key, scalarText, isScalar)
		}
	}
}
