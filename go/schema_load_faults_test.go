package yamldslspec

import (
	"strings"
	"testing"
)

func TestAReferenceOrPatternThatDoesNotLoadIsTheFirstInDocumentOrder(testingContext *testing.T) {
	faultySchemas := []struct {
		schemaText  string
		expectedWhy string
	}{
		{`{"properties": {"mock": {"$ref": "#/$defs/missing"}}}`, "`#/$defs/missing` names no node of the schema"},
		{`{"properties": {"mock": {"pattern": "(_a"}, "other": {"$ref": "#/$defs/missing"}}}`, "`(_a` is not a pattern"},
		{`{"patternProperties": {"(_b": {"$ref": "#/$defs/missing"}}}`, "`(_b` is not a pattern"},
		{`{"properties": {"enum": {"$ref": "#/$defs/missing"}}}`, "`#/$defs/missing` names no node of the schema"},
		{`{"$defs": {"mock": {"items": [{"pattern": "[z-a]"}]}}}`, "`[z-a]` is not a pattern"},
		{
			`{"$schema": "https://json-schema.org/draft/2020-12/schema", ` +
				`"$defs": {"outer": {}, "mock": {"$id": "https://mock.example/mock.json", "$ref": "#/$defs/outer"}}}`,
			"`#/$defs/outer` names no node of the schema",
		},
	}
	for _, faultySchema := range faultySchemas {
		_, loadError := NewSchemaCache().Load("mock.schema.json", faultySchema.schemaText)
		if loadError == nil || loadError.Error() != "schema mock.schema.json: "+faultySchema.expectedWhy {
			testingContext.Errorf("%s fails to load with %v", faultySchema.schemaText, loadError)
		}
	}
}

func TestAReferenceNamingANodeAndDataThatLooksLikeASchemaLoad(testingContext *testing.T) {
	schemaText := `{
	  "$defs": {"mock queue": {"type": "object"}},
	  "properties": {
	    "queue": {"$ref": "#/$defs/mock%20queue"},
	    "whole": {"$ref": "#"},
	    "resource": {
	      "$id": "https://mock.example/mock.json",
	      "$defs": {"inner": {}},
	      "properties": {"size": {"$ref": "#/$defs/inner"}}
	    },
	    "beside": {"$id": "https://mock.example/beside.json", "$ref": "#/$defs/mock%20queue"},
	    "listed": {"enum": [{"$ref": "#/$defs/missing"}], "default": {"pattern": "(_x"}}
	  }
	}`
	if _, loadError := NewSchemaCache().Load("mock.schema.json", schemaText); loadError != nil {
		testingContext.Error(loadError)
	}
}

func TestALibraryFailureNamesTheSchemaByTheNameItWasLoadedWith(testingContext *testing.T) {
	_, loadError := NewSchemaCache().Load("mock.schema.json", `{"type": 5}`)
	if loadError == nil || strings.Contains(loadError.Error(), compiledSchemaDirectory) ||
		!strings.Contains(loadError.Error(), `"mock.schema.json#"`) {
		testingContext.Errorf("error %v", loadError)
	}
}

func TestTheSchemaIsRegisteredWhereNoIDItStatesResolves(testingContext *testing.T) {
	schemaValue, _ := decodeOrderedJSON(`{
	  "$id": "#mock-anchor",
	  "$defs": {"mock": {"$id": "document-schema-0.json", "$defs": {"inner": {"$id": "#mock-inner"}}}}
	}`)
	expectedLocation := compiledSchemaDirectory + "document-schema-1.json"
	if schemaLocation := unstatedSchemaLocation(schemaValue); schemaLocation != expectedLocation {
		testingContext.Errorf("location %q", schemaLocation)
	}
}

func TestAReferenceInsideAnIDResourceReachesTheResourcesNodes(testingContext *testing.T) {
	schemaValue, _ := decodeOrderedJSON(`{
	  "$schema": "https://json-schema.org/draft/2020-12/schema",
	  "$defs": {
	    "mock_wide": {"properties": {"mock_outer": {}}},
	    "mock_resource": {
	      "$id": "mock-resource.json",
	      "$ref": "#/$defs/mock_wide",
	      "unevaluatedProperties": false,
	      "$defs": {"mock_wide": {"properties": {"mock_inner": {}}}}
	    }
	  }
	}`)
	keywordPointer := "/$defs/mock_resource/unevaluatedProperties"
	if !fieldCouldBeAccepted(schemaValue, keywordPointer, "mock_inner") ||
		fieldCouldBeAccepted(schemaValue, keywordPointer, "mock_outer") {
		testingContext.Error("the reference resolves outside its resource")
	}
}
