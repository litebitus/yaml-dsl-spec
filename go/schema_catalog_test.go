package yamldslspec

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestAMajorStandsForItsNewestMinor(testingContext *testing.T) {
	if readVersions := YAMLDSLVersions(); !reflect.DeepEqual(readVersions, []string{"1", "1.0", "1.1", "1.2", "1.3"}) {
		testingContext.Errorf("yaml-dsl versions %v", readVersions)
	}
	if readVersions := FunctionTableVersions(); !reflect.DeepEqual(readVersions, []string{"1", "1.0", "1.1"}) {
		testingContext.Errorf("function table versions %v", readVersions)
	}
	majorSchema, _ := YAMLDSLSchema("1")
	newestSchema, _ := YAMLDSLSchema("1.3")
	olderSchema, _ := YAMLDSLSchema("1.2")
	if majorSchema != newestSchema || majorSchema == olderSchema {
		testingContext.Error("major 1 is not its newest minor")
	}
	for _, minorVersion := range []string{"1.0", "1.1", "1.2", "1.3"} {
		minorSchema, isCarried := YAMLDSLSchema(minorVersion)
		if !isCarried || !strings.Contains(minorSchema, `"title": "yaml-dsl.yml v`+minorVersion+`"`) {
			testingContext.Errorf("schema %s does not state its minor", minorVersion)
		}
	}
	if _, isCarried := YAMLDSLSchema("2"); isCarried {
		testingContext.Error("a version the package does not carry is read")
	}
	tableMajor, _ := FunctionTableSchema("1")
	tableNewest, _ := FunctionTableSchema("1.1")
	tableOlder, _ := FunctionTableSchema("1.0")
	if tableMajor != tableNewest || tableMajor == tableOlder {
		testingContext.Error("function table major 1 is not its newest minor")
	}
}

func TestVersionFilesLeaveOtherFamiliesOut(testingContext *testing.T) {
	fileNames := []string{
		"mock-v2.0.schema.json",
		"mock-v1.10.schema.json",
		"mock-v1.2.schema.json",
		"mock-other-v1.0.schema.json",
		"mock-v1.schema.json",
		"README.md",
	}
	var versionPairs []string
	for _, versionFile := range versionFiles(fileNames, "mock") {
		versionPairs = append(versionPairs, versionFile.schemaVersion+"="+versionFile.fileName)
	}
	expectedPairs := []string{
		"1=mock-v1.10.schema.json",
		"1.2=mock-v1.2.schema.json",
		"1.10=mock-v1.10.schema.json",
		"2=mock-v2.0.schema.json",
		"2.0=mock-v2.0.schema.json",
	}
	if !reflect.DeepEqual(versionPairs, expectedPairs) {
		testingContext.Errorf("version files %v", versionPairs)
	}
}

func TestEverySchemaIsJSON(testingContext *testing.T) {
	for _, versionFile := range slices.Concat(yamlDSLSchemaFiles, functionTableSchemaFiles) {
		var decodedSchema any
		if decodeError := json.Unmarshal([]byte(versionFile.schemaText), &decodedSchema); decodeError != nil {
			testingContext.Errorf("%s: %v", versionFile.fileName, decodeError)
		}
	}
}
