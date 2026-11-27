package resolution

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAModelineNamesItsURIItsFragmentAndWhereItStarts(testingContext *testing.T) {
	documentText := "name: mock\n  #  yaml-language-server:  $schema=../mock.schema.json#/a rest\n"
	foundModeline, hasModeline := DocumentModeline(documentText)
	if !hasModeline || foundModeline.SchemaURI != "../mock.schema.json#/a" || foundModeline.SchemaFragment == nil ||
		*foundModeline.SchemaFragment != "/a" || foundModeline.LineNumber != 2 || foundModeline.ColumnNumber != 37 {
		testingContext.Fatalf("modeline %+v", foundModeline)
	}
	wholeModeline, _ := DocumentModeline("# yaml-language-server: $schema=mock.schema.json\n")
	if wholeModeline.SchemaFragment != nil {
		testingContext.Fatalf("whole document modeline %+v", wholeModeline)
	}
	for _, unnamedText := range []string{"# yaml-language-server: $schema=\n", "# mock: $schema=x\n", "#mock\n"} {
		if _, hasModeline := DocumentModeline(unnamedText); hasModeline {
			testingContext.Fatalf("%q names a modeline", unnamedText)
		}
	}
}

func TestAPathIsResolvedToItsFileURLByForwardSlashSteps(testingContext *testing.T) {
	baseFolder, configRoot := "/mock/stack", "/mock/config"
	relativeLocation := ResolveLocation("../schemas/./mock.schema.json#/a", baseFolder, configRoot)
	if relativeLocation.URLScheme != "file" || relativeLocation.FilePath != "/mock/schemas/mock.schema.json" ||
		*relativeLocation.SchemaFragment != "/a" ||
		relativeLocation.WrittenLocation != "../schemas/./mock.schema.json" {
		testingContext.Fatalf("relative %+v", relativeLocation)
	}
	rootedLocation := ResolveLocation("{config_root}/mock dir/m.json", baseFolder, configRoot)
	if rootedLocation.FilePath != "/mock/config/mock dir/m.json" ||
		rootedLocation.LocationURL != "file:///mock/config/mock%20dir/m.json" {
		testingContext.Fatalf("rooted %+v", rootedLocation)
	}
	if absoluteLocation := ResolveLocation("/mock/abs.json", baseFolder, configRoot); absoluteLocation.FilePath !=
		"/mock/abs.json" {
		testingContext.Fatalf("absolute %+v", absoluteLocation)
	}
	if urlLocation := ResolveLocation("file:///mock/a%20b.json", baseFolder, configRoot); urlLocation.FilePath !=
		"/mock/a b.json" {
		testingContext.Fatalf("url %+v", urlLocation)
	}
	climbingLocation := ResolveLocation("file:///mock/a/../b/./c.json", baseFolder, configRoot)
	if climbingLocation.FilePath != "/mock/b/c.json" || climbingLocation.LocationURL != "file:///mock/b/c.json" {
		testingContext.Fatalf("climbing %+v", climbingLocation)
	}
}

func TestTheCacheParametersAreReadOutAndTheOthersKeptInOrder(testingContext *testing.T) {
	readLocation := ResolveLocation(
		"https://mock.example/s.json?b=1&yaml_dsl_max_age_days=7&a=2&yaml_dsl_machine_credentials=true",
		"/mock", "/mock")
	if readLocation.FetchURL != "https://mock.example/s.json?b=1&a=2" || readLocation.URLScheme != "https" ||
		readLocation.MaxAgeDays != 7 || !readLocation.MachineCredentials || readLocation.CacheParameterProblem != nil {
		testingContext.Fatalf("location %+v", readLocation)
	}
	expectedProblems := map[string]string{
		"yaml_dsl_mock=1":                  "`yaml_dsl_mock` is not a parameter of a schema URL",
		"yaml_dsl_max_age_days=07":         "`yaml_dsl_max_age_days` must be a whole number of days",
		"yaml_dsl_max_age_days=1x":         "`yaml_dsl_max_age_days` must be a whole number of days",
		"yaml_dsl_machine_credentials=yes": "`yaml_dsl_machine_credentials` must be `true` or `false`",
	}
	for queryText, expectedProblem := range expectedProblems {
		problemLocation := ResolveLocation("https://mock.example/s.json?"+queryText, "/mock", "/mock")
		if problemLocation.CacheParameterProblem == nil || *problemLocation.CacheParameterProblem != expectedProblem {
			testingContext.Fatalf("%s reads to %v", queryText, problemLocation.CacheParameterProblem)
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
	if drivePath := FileURLPath("file:///c:/mock/a.json"); drivePath != "c:"+string(filepath.Separator)+"mock"+
		string(filepath.Separator)+"a.json" {
		testingContext.Fatalf("drive path %s", drivePath)
	}
	if hostPath := FileURLPath("file://mock-host"); hostPath != string(filepath.Separator) {
		testingContext.Fatalf("host path %s", hostPath)
	}
	if fileURL := FileURLOf("/mock/./a b/../c.json"); fileURL != "file:///mock/a%20b/c.json" {
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
	matchedFile, hasMatch := FirstMatchingFile(filepath.Join(scratchFolder, "*", "mock.schema.json"))
	if !hasMatch || matchedFile != filepath.Join(scratchFolder, "a", "mock.schema.json") {
		testingContext.Fatalf("matched %s", matchedFile)
	}
	if _, hasMatch := FirstMatchingFile(filepath.Join(scratchFolder, "absent", "*.json")); hasMatch {
		testingContext.Fatal("a folder that is not there matched")
	}
	if !HasPattern("/mock/*.json") || HasPattern("/mock/a.json") {
		testingContext.Fatal("a pattern is read by its star")
	}
}
