package yamldslspec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEveryExampleReadsToValidatedDefinitions(testingContext *testing.T) {
	examplePaths, globError := filepath.Glob("../examples/*/yaml-dsl.yml")
	if globError != nil || len(examplePaths) == 0 {
		testingContext.Fatal("no examples", globError)
	}
	for _, examplePath := range examplePaths {
		fileContent, readError := os.ReadFile(examplePath)
		if readError != nil {
			testingContext.Fatal(readError)
		}
		declaredDSLs, readProblems := ReadDSLDefinitions(examplePath, string(fileContent))
		if readProblems != nil || len(declaredDSLs) == 0 {
			testingContext.Errorf("%s: %v", examplePath, readProblems)
		}
	}
}

func TestADefinitionKeepsItsBlockAsWritten(testingContext *testing.T) {
	fileContent, readError := os.ReadFile("../examples/layers/yaml-dsl.yml")
	if readError != nil {
		testingContext.Fatal(readError)
	}
	declaredDSLs, readProblems := ReadDSLDefinitions("yaml-dsl.yml", string(fileContent))
	if readProblems != nil {
		testingContext.Fatal(readProblems)
	}
	dslDefinition := declaredDSLs[0].Definition
	if dslDefinition.ID != "mock-queues" || dslDefinition.Layers.CommonLayerDiscovery != CommonLayerDiscoveryAncestor {
		testingContext.Errorf("definition %+v", dslDefinition)
	}
	if declaredDSLs[0].Block.Position != NewPosition("yaml-dsl.yml", 4, 5) {
		testingContext.Errorf("block written at %v", declaredDSLs[0].Block.Position)
	}
}

func TestABlockReadsWithItsDefaults(testingContext *testing.T) {
	dslDefinition, definitionError := DSLDefinitionFromJSON(map[string]any{
		"id":            "mock-queues",
		"file_includes": []any{"**/*.mock.yml"},
		"declarations": []any{map[string]any{
			"path":          "$.queues.*",
			"name_source":   "key",
			"name_spelling": "dashes_as_underscores",
			"scope_name":    "queue",
		}},
	})
	if definitionError != nil {
		testingContext.Fatal(definitionError)
	}
	if dslDefinition.FileExcludes == nil || len(dslDefinition.FileExcludes) != 0 ||
		dslDefinition.Layers != nil || len(dslDefinition.Scopes) != 0 {
		testingContext.Errorf("definition %+v", dslDefinition)
	}
	declarationRule := dslDefinition.Declarations[0]
	if declarationRule.NameSpelling != NameSpellingDashesAsUnderscores ||
		declarationRule.KeyToken != nil || declarationRule.DeclaresEveryName {
		testingContext.Errorf("declaration %+v", declarationRule)
	}
}

func TestAMisspeltVersionIsAnsweredWithTheClosestOne(testingContext *testing.T) {
	_, readProblems := ReadDSLDefinitions("mock.yml", "version: \"2.1\"\ndsls: []\n")
	if len(readProblems) != 1 || readProblems[0].Notes == nil || readProblems[0].Notes.Help == nil {
		testingContext.Fatalf("problems %v", readProblems)
	}
	versionProblem := readProblems[0]
	if versionProblem.Message != "`2.1` is not a version this reader reads" ||
		*versionProblem.Notes.Help != "did you mean `\"1.1\"`?" {
		testingContext.Errorf("problem %v help %v", versionProblem, *versionProblem.Notes.Help)
	}
}

func TestABlockMissingAFieldIsNotADefinition(testingContext *testing.T) {
	_, definitionError := DSLDefinitionFromJSON(map[string]any{"file_includes": []any{}})
	if definitionError == nil || definitionError.Error() != "missing field `id`" {
		testingContext.Errorf("error %v", definitionError)
	}
	_, variantError := DSLDefinitionFromJSON(map[string]any{
		"id":            "mock",
		"file_includes": []any{},
		"layers":        map[string]any{"common_layer_discovery": "sideways"},
	})
	if variantError == nil {
		testingContext.Error("an unknown variant is read")
	}
}
