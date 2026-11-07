// Package yamldslspec is the core of the YAML DSL standard in Go.
package yamldslspec

//go:generate sh -c "mkdir -p schemas && cp ../schemas/*-v*.schema.json schemas/"

import (
	"embed"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed schemas/*.schema.json
var schemaFiles embed.FS

const (
	yamlDSLFamily       = "yaml-dsl"
	functionTableFamily = "x-yaml-dsl-functions"
)

type versionedSchema struct {
	schemaVersion string
	fileName      string
	schemaText    string
}

var (
	yamlDSLSchemaFiles       = familySchemas(yamlDSLFamily)
	functionTableSchemaFiles = familySchemas(functionTableFamily)
)

// YAMLDSLSchema is the JSON Schema of `yaml-dsl.yml` at a version, if the package carries it.
func YAMLDSLSchema(schemaVersion string) (string, bool) {
	foundSchema, isCarried := schemaAt(yamlDSLSchemaFiles, schemaVersion)
	return foundSchema.schemaText, isCarried
}

// FunctionTableSchema is the JSON Schema of a DSL's function table at a version, if the package carries it.
func FunctionTableSchema(schemaVersion string) (string, bool) {
	foundSchema, isCarried := schemaAt(functionTableSchemaFiles, schemaVersion)
	return foundSchema.schemaText, isCarried
}

// YAMLDSLVersions lists every `yaml-dsl.yml` version the package reads, each major before its minors.
func YAMLDSLVersions() []string {
	return versionsOf(yamlDSLSchemaFiles)
}

// FunctionTableVersions lists every function table version the package reads, each major before its minors.
func FunctionTableVersions() []string {
	return versionsOf(functionTableSchemaFiles)
}

func schemaAt(carriedSchemas []versionedSchema, schemaVersion string) (versionedSchema, bool) {
	for _, carriedSchema := range carriedSchemas {
		if carriedSchema.schemaVersion == schemaVersion {
			return carriedSchema, true
		}
	}
	return versionedSchema{}, false
}

func versionsOf(carriedSchemas []versionedSchema) []string {
	schemaVersions := make([]string, 0, len(carriedSchemas))
	for _, carriedSchema := range carriedSchemas {
		schemaVersions = append(schemaVersions, carriedSchema.schemaVersion)
	}
	return schemaVersions
}

func familySchemas(familyStem string) []versionedSchema {
	directoryEntries, readError := schemaFiles.ReadDir("schemas")
	if readError != nil {
		panic(readError)
	}
	fileNames := make([]string, 0, len(directoryEntries))
	for _, directoryEntry := range directoryEntries {
		fileNames = append(fileNames, directoryEntry.Name())
	}
	var carriedSchemas []versionedSchema
	for _, versionFile := range versionFiles(fileNames, familyStem) {
		fileContent, contentError := schemaFiles.ReadFile(path.Join("schemas", versionFile.fileName))
		if contentError != nil {
			panic(contentError)
		}
		versionFile.schemaText = string(fileContent)
		carriedSchemas = append(carriedSchemas, versionFile)
	}
	return carriedSchemas
}

type minorFile struct {
	majorNumber int
	minorNumber int
	fileName    string
}

func versionFiles(fileNames []string, familyStem string) []versionedSchema {
	var minorFiles []minorFile
	for _, fileName := range fileNames {
		if majorNumber, minorNumber, isMinor := minorVersionOf(fileName, familyStem); isMinor {
			minorFiles = append(minorFiles, minorFile{majorNumber: majorNumber, minorNumber: minorNumber, fileName: fileName})
		}
	}
	sort.Slice(minorFiles, func(firstIndex, secondIndex int) bool {
		if minorFiles[firstIndex].majorNumber != minorFiles[secondIndex].majorNumber {
			return minorFiles[firstIndex].majorNumber < minorFiles[secondIndex].majorNumber
		}
		return minorFiles[firstIndex].minorNumber < minorFiles[secondIndex].minorNumber
	})
	var versionEntries []versionedSchema
	for fileIndex, currentFile := range minorFiles {
		if fileIndex == 0 || minorFiles[fileIndex-1].majorNumber != currentFile.majorNumber {
			newestFile := currentFile
			for _, otherFile := range minorFiles {
				if otherFile.majorNumber == currentFile.majorNumber {
					newestFile = otherFile
				}
			}
			versionEntries = append(versionEntries, versionedSchema{
				schemaVersion: strconv.Itoa(currentFile.majorNumber),
				fileName:      newestFile.fileName,
			})
		}
		minorVersion := strconv.Itoa(currentFile.majorNumber) + "." + strconv.Itoa(currentFile.minorNumber)
		versionEntries = append(versionEntries, versionedSchema{schemaVersion: minorVersion, fileName: currentFile.fileName})
	}
	return versionEntries
}

func minorVersionOf(fileName, familyStem string) (int, int, bool) {
	namePrefix := familyStem + "-v"
	nameSuffix := ".schema.json"
	if !strings.HasPrefix(fileName, namePrefix) || !strings.HasSuffix(fileName, nameSuffix) {
		return 0, 0, false
	}
	versionText := strings.TrimSuffix(strings.TrimPrefix(fileName, namePrefix), nameSuffix)
	majorText, minorText, hasDot := strings.Cut(versionText, ".")
	if !hasDot || majorText == "" || minorText == "" || !onlyDigits(majorText) || !onlyDigits(minorText) {
		return 0, 0, false
	}
	majorNumber, majorError := strconv.Atoi(majorText)
	minorNumber, minorError := strconv.Atoi(minorText)
	if majorError != nil || minorError != nil {
		return 0, 0, false
	}
	return majorNumber, minorNumber, true
}
