package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAModelineNamesItsURIAndWhereItStarts(testingContext *testing.T) {
	documentText := "name: mock\n  #  yaml-language-server:  $schema=../mock.schema.json#/a rest\n"
	foundModeline, hasModeline := modelineOf(documentText)
	expectedModeline := schemaModeline{schemaURI: "../mock.schema.json#/a", lineNumber: 2, columnNumber: 37}
	if !hasModeline || foundModeline != expectedModeline {
		testingContext.Fatalf("modeline %+v", foundModeline)
	}
	for _, unnamedText := range []string{"# yaml-language-server: $schema=\n", "# mock: $schema=x\n", "#mock\n"} {
		if _, hasModeline := modelineOf(unnamedText); hasModeline {
			testingContext.Fatalf("%q names a modeline", unnamedText)
		}
	}
}

func TestAPathIsResolvedToItsFileURLByForwardSlashSteps(testingContext *testing.T) {
	baseFolder, configRoot := "/mock/stack", "/mock/config"
	relativeLocation := resolveLocation("../schemas/./mock.schema.json#/a", baseFolder, configRoot)
	if relativeLocation.urlScheme != "file" || relativeLocation.filePath != "/mock/schemas/mock.schema.json" ||
		*relativeLocation.schemaFragment != "/a" ||
		relativeLocation.writtenLocation != "../schemas/./mock.schema.json" {
		testingContext.Fatalf("relative %+v", relativeLocation)
	}
	rootedLocation := resolveLocation("{config_root}/mock dir/m.json", baseFolder, configRoot)
	if rootedLocation.filePath != "/mock/config/mock dir/m.json" ||
		rootedLocation.locationURL != "file:///mock/config/mock%20dir/m.json" {
		testingContext.Fatalf("rooted %+v", rootedLocation)
	}
	if absoluteLocation := resolveLocation("/mock/abs.json", baseFolder, configRoot); absoluteLocation.filePath !=
		"/mock/abs.json" {
		testingContext.Fatalf("absolute %+v", absoluteLocation)
	}
	if urlLocation := resolveLocation("file:///mock/a%20b.json", baseFolder, configRoot); urlLocation.filePath !=
		"/mock/a b.json" {
		testingContext.Fatalf("url %+v", urlLocation)
	}
	climbingLocation := resolveLocation("file:///mock/a/../b/./c.json", baseFolder, configRoot)
	if climbingLocation.filePath != "/mock/b/c.json" || climbingLocation.locationURL != "file:///mock/b/c.json" {
		testingContext.Fatalf("climbing %+v", climbingLocation)
	}
}

func TestTheCacheParametersAreReadOutAndTheOthersKeptInOrder(testingContext *testing.T) {
	readLocation := resolveLocation(
		"https://mock.example/s.json?b=1&yaml_dsl_max_age_days=7&a=2&yaml_dsl_machine_credentials=true",
		"/mock", "/mock")
	if readLocation.fetchURL != "https://mock.example/s.json?b=1&a=2" || readLocation.urlScheme != "https" ||
		readLocation.maxAgeDays != 7 || !readLocation.machineCredentials || readLocation.cacheParameterProblem != nil {
		testingContext.Fatalf("location %+v", readLocation)
	}
	expectedProblems := map[string]string{
		"yaml_dsl_mock=1":                  "`yaml_dsl_mock` is not a parameter of a schema URL",
		"yaml_dsl_max_age_days=07":         "`yaml_dsl_max_age_days` must be a whole number of days",
		"yaml_dsl_max_age_days=1x":         "`yaml_dsl_max_age_days` must be a whole number of days",
		"yaml_dsl_machine_credentials=yes": "`yaml_dsl_machine_credentials` must be `true` or `false`",
	}
	for queryText, expectedProblem := range expectedProblems {
		problemLocation := resolveLocation("https://mock.example/s.json?"+queryText, "/mock", "/mock")
		if problemLocation.cacheParameterProblem == nil || *problemLocation.cacheParameterProblem != expectedProblem {
			testingContext.Fatalf("%s reads to %v", queryText, problemLocation.cacheParameterProblem)
		}
	}
}

func TestASchemeIsReadOnlyWhereAURLStartsWithOne(testingContext *testing.T) {
	expectedSchemes := map[string]string{"S3://mock/key": "s3", "gs+x.y-z://mock": "gs+x.y-z"}
	for writtenLocation, expectedScheme := range expectedSchemes {
		if urlScheme, hasScheme := urlSchemeOf(writtenLocation); !hasScheme || urlScheme != expectedScheme {
			testingContext.Fatalf("%s reads to %s", writtenLocation, urlScheme)
		}
	}
	for _, schemelessLocation := range []string{"mock/https://x", "1x://mock", "m_x://mock"} {
		if _, hasScheme := urlSchemeOf(schemelessLocation); hasScheme {
			testingContext.Fatalf("%s reads to a scheme", schemelessLocation)
		}
	}
}

func TestAStarStepMatchesAnyRunOfCharactersWithinIt(testingContext *testing.T) {
	matchCases := map[[2]string]bool{
		{"*.json", "mock.json"}: true,
		{"a*b*c", "a-x-b-y-c"}:  true,
		{"a*b*c", "a-x-c"}:      false,
		{"ab*ba", "aba"}:        false,
		{"*", ""}:               true,
	}
	for matchCase, expectedMatch := range matchCases {
		if stepMatches(matchCase[0], matchCase[1]) != expectedMatch {
			testingContext.Fatalf("%v matches %v", matchCase, !expectedMatch)
		}
	}
}

func TestAFileURLNamesItsPathAndADrive(testingContext *testing.T) {
	if drivePath := fileURLPath("file:///c:/mock/a.json"); drivePath != "c:"+string(filepath.Separator)+"mock"+
		string(filepath.Separator)+"a.json" {
		testingContext.Fatalf("drive path %s", drivePath)
	}
	if hostPath := fileURLPath("file://mock-host"); hostPath != string(filepath.Separator) {
		testingContext.Fatalf("host path %s", hostPath)
	}
	if fileURL := fileURLOf("/mock/./a b/../c.json"); fileURL != "file:///mock/a%20b/c.json" {
		testingContext.Fatalf("file URL %s", fileURL)
	}
}

func TestAPatternTakesTheFirstFileInPathOrder(testingContext *testing.T) {
	scratchFolder := testingContext.TempDir()
	for _, folderName := range []string{"b", "a-b", "a"} {
		if makeError := os.MkdirAll(filepath.Join(scratchFolder, folderName), 0o755); makeError != nil {
			testingContext.Fatal(makeError)
		}
		schemaFile := filepath.Join(scratchFolder, folderName, "mock.schema.json")
		if writeError := os.WriteFile(schemaFile, []byte("{}"), 0o644); writeError != nil {
			testingContext.Fatal(writeError)
		}
	}
	matchedFile, hasMatch := firstMatchingFile(filepath.Join(scratchFolder, "*", "mock.schema.json"))
	if !hasMatch || matchedFile != filepath.Join(scratchFolder, "a", "mock.schema.json") {
		testingContext.Fatalf("matched %s", matchedFile)
	}
	if _, hasMatch := firstMatchingFile(filepath.Join(scratchFolder, "absent", "*.json")); hasMatch {
		testingContext.Fatal("a folder that is not there matched")
	}
	if !hasPattern("/mock/*.json") || hasPattern("/mock/a.json") {
		testingContext.Fatal("a pattern is read by its star")
	}
}

func TestAFileNameIsTheLastStepOfItsURLElseTheURL(testingContext *testing.T) {
	expectedNames := map[string]string{
		"file:///mock/mock%20schema.json": "mock schema.json",
		"file:///mock/":                   "file:///mock/",
	}
	for fileURL, expectedName := range expectedNames {
		if fileName := fileNameOf(fileURL); fileName != expectedName {
			testingContext.Fatalf("%s names %s", fileURL, fileName)
		}
	}
}
