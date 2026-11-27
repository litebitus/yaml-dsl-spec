package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/litebitus/yaml-dsl-spec/go/core"
)

const (
	modelineKey                 = "yaml-language-server:"
	modelineSchema              = "$schema="
	configRootMarker            = "{config_root}"
	cacheParameterPrefix        = "yaml_dsl_"
	maxAgeParameter             = "yaml_dsl_max_age_days"
	machineCredentialsParameter = "yaml_dsl_machine_credentials"
	fileScheme                  = "file"
)

// schemaModeline is a document's modeline: the schema URI it names and where that URI starts.
type schemaModeline struct {
	schemaURI    string
	lineNumber   int
	columnNumber int
}

// modelineOf is the first line of documentText reading `# yaml-language-server: $schema=<uri>`.
func modelineOf(documentText string) (schemaModeline, bool) {
	textLines := strings.Split(documentText, "\n")
	if strings.HasSuffix(documentText, "\n") {
		textLines = textLines[:len(textLines)-1]
	}
	for lineIndex, lineText := range textLines {
		lineText = strings.TrimSuffix(lineText, "\r")
		afterHash, hasHash := strings.CutPrefix(strings.TrimLeftFunc(lineText, unicode.IsSpace), "#")
		if !hasHash {
			continue
		}
		afterKey, hasKey := strings.CutPrefix(strings.TrimLeftFunc(afterHash, unicode.IsSpace), modelineKey)
		if !hasKey {
			continue
		}
		uriAndRest, hasSchema := strings.CutPrefix(strings.TrimLeftFunc(afterKey, unicode.IsSpace), modelineSchema)
		if !hasSchema {
			continue
		}
		schemaURI := uriAndRest
		if spaceAt := strings.IndexFunc(uriAndRest, unicode.IsSpace); spaceAt >= 0 {
			schemaURI = uriAndRest[:spaceAt]
		}
		if schemaURI == "" {
			continue
		}
		uriStart := len(lineText) - len(uriAndRest)
		return schemaModeline{
			schemaURI:    schemaURI,
			lineNumber:   lineIndex + 1,
			columnNumber: utf8.RuneCountInString(lineText[:uriStart]) + 1,
		}, true
	}
	return schemaModeline{}, false
}

// resolvedLocation is a location as written, without its fragment, resolved: its URL, the URL a reader is asked for,
// and the cache's parameters it carries.
type resolvedLocation struct {
	writtenLocation string
	schemaFragment  *string
	locationURL     string
	urlScheme       string
	fetchURL        string
	maxAgeDays      uint64
	// machineCredentials is whether a refused read is asked again with the machine's credentials.
	machineCredentials bool
	// cacheParameterNames are the cache's parameters written in it, by name, in order.
	cacheParameterNames []string
	// cacheParameterProblem is the first of its cache's parameters that does not read, as a problem's message.
	cacheParameterProblem *string
	// filePath is the file a `file` location names, its `*` steps not yet matched; empty for any other.
	filePath string
}

// resolveLocation is writtenLocation, a path or a URL, resolved: a path against baseFolder, or against configRoot
// when it starts with `{config_root}`.
func resolveLocation(writtenLocation, baseFolder, configRoot string) resolvedLocation {
	withoutFragment, fragmentText, hasFragment := strings.Cut(writtenLocation, "#")
	var schemaFragment *string
	if hasFragment {
		schemaFragment = &fragmentText
	}
	locationAddress, queryText, hasQuery := strings.Cut(withoutFragment, "?")
	readParameters := cacheParametersOf(queryText, hasQuery)
	urlScheme, hasScheme := urlSchemeOf(locationAddress)
	var resolvedAddress, filePath string
	switch {
	case hasScheme && urlScheme == fileScheme:
		filePath = fileURLPath(locationAddress)
		resolvedAddress = fileURLOf(filePath)
	case hasScheme:
		resolvedAddress = locationAddress
	default:
		urlScheme = fileScheme
		filePath = writtenPath(locationAddress, baseFolder, configRoot)
		resolvedAddress = fileURLOf(filePath)
	}
	withQuery := func(queryParameters []string) string {
		if len(queryParameters) == 0 {
			return resolvedAddress
		}
		return resolvedAddress + "?" + strings.Join(queryParameters, "&")
	}
	var allParameters []string
	if hasQuery {
		allParameters = strings.Split(queryText, "&")
	}
	return resolvedLocation{
		writtenLocation:       withoutFragment,
		schemaFragment:        schemaFragment,
		locationURL:           withQuery(allParameters),
		urlScheme:             urlScheme,
		fetchURL:              withQuery(readParameters.passedParameters),
		maxAgeDays:            readParameters.maxAgeDays,
		machineCredentials:    readParameters.machineCredentials,
		cacheParameterNames:   readParameters.parameterNames,
		cacheParameterProblem: readParameters.parameterProblem,
		filePath:              filePath,
	}
}

// urlSchemeOf is a URL's scheme, lower case, where writtenLocation starts with `<scheme>://`.
func urlSchemeOf(writtenLocation string) (string, bool) {
	schemeText, _, hasSeparator := strings.Cut(writtenLocation, "://")
	if !hasSeparator || schemeText == "" || !isASCIILetter(schemeText[0]) {
		return "", false
	}
	for _, schemeByte := range []byte(schemeText[1:]) {
		if !isASCIILetter(schemeByte) && !isASCIIDigit(schemeByte) && !strings.ContainsRune("+-.", rune(schemeByte)) {
			return "", false
		}
	}
	return strings.ToLower(schemeText), true
}

func isASCIILetter(textByte byte) bool {
	return (textByte >= 'a' && textByte <= 'z') || (textByte >= 'A' && textByte <= 'Z')
}

func isASCIIDigit(textByte byte) bool {
	return textByte >= '0' && textByte <= '9'
}

type cacheParameters struct {
	passedParameters   []string
	parameterNames     []string
	maxAgeDays         uint64
	machineCredentials bool
	parameterProblem   *string
}

func cacheParametersOf(queryText string, hasQuery bool) cacheParameters {
	var readParameters cacheParameters
	if !hasQuery {
		return readParameters
	}
	for _, queryParameter := range strings.Split(queryText, "&") {
		parameterName, parameterValue, _ := strings.Cut(queryParameter, "=")
		if !strings.HasPrefix(parameterName, cacheParameterPrefix) {
			readParameters.passedParameters = append(readParameters.passedParameters, queryParameter)
			continue
		}
		readParameters.parameterNames = append(readParameters.parameterNames, parameterName)
		var parameterProblem string
		switch parameterName {
		case maxAgeParameter:
			if wholeDays, isWhole := wholeDaysOf(parameterValue); isWhole {
				readParameters.maxAgeDays = wholeDays
			} else {
				parameterProblem = core.CodeSpan(maxAgeParameter) + " must be a whole number of days"
			}
		case machineCredentialsParameter:
			if parameterValue == "true" || parameterValue == "false" {
				readParameters.machineCredentials = parameterValue == "true"
			} else {
				parameterProblem = fmt.Sprintf("%s must be %s or %s",
					core.CodeSpan(machineCredentialsParameter), core.CodeSpan("true"), core.CodeSpan("false"))
			}
		default:
			parameterProblem = core.CodeSpan(parameterName) + " is not a parameter of a schema URL"
		}
		if parameterProblem != "" && readParameters.parameterProblem == nil {
			readParameters.parameterProblem = &parameterProblem
		}
	}
	return readParameters
}

func wholeDaysOf(parameterValue string) (uint64, bool) {
	if parameterValue == "" || (len(parameterValue) > 1 && parameterValue[0] == '0') {
		return 0, false
	}
	for _, valueByte := range []byte(parameterValue) {
		if !isASCIIDigit(valueByte) {
			return 0, false
		}
	}
	wholeDays, parseError := strconv.ParseUint(parameterValue, 10, 64)
	return wholeDays, parseError == nil
}

// writtenPath is the machine path a written path names, its steps separated by `/`.
func writtenPath(writtenLocation, baseFolder, configRoot string) string {
	startFolder, restText := baseFolder, writtenLocation
	if afterMarker, isRooted := strings.CutPrefix(writtenLocation, configRootMarker); isRooted {
		startFolder, restText = configRoot, afterMarker
	} else if strings.HasPrefix(writtenLocation, "/") {
		startFolder = string(filepath.Separator)
	}
	machinePath := startFolder
	for _, pathStep := range strings.Split(restText, "/") {
		switch pathStep {
		case "", ".":
		case "..":
			machinePath = filepath.Dir(machinePath)
		default:
			machinePath = filepath.Join(machinePath, pathStep)
		}
	}
	return machinePath
}

// fileURLPath is the machine path a `file` URL names.
func fileURLPath(fileURL string) string {
	afterScheme := fileURL
	if separatorAt := strings.Index(fileURL, "://"); separatorAt >= 0 {
		afterScheme = fileURL[separatorAt+3:]
	}
	pathStart := strings.IndexByte(afterScheme, '/')
	if pathStart < 0 {
		pathStart = len(afterScheme)
	}
	var writtenSteps []string
	for _, pathStep := range strings.Split(percentDecoded(afterScheme[pathStart:]), "/") {
		if pathStep != "" {
			writtenSteps = append(writtenSteps, pathStep)
		}
	}
	driveText := ""
	if len(writtenSteps) > 0 && namesDrive(writtenSteps[0]) {
		driveText, writtenSteps = writtenSteps[0], writtenSteps[1:]
	}
	var pathSteps []string
	for _, pathStep := range writtenSteps {
		switch pathStep {
		case ".":
		case "..":
			pathSteps = pathSteps[:max(len(pathSteps)-1, 0)]
		default:
			pathSteps = append(pathSteps, pathStep)
		}
	}
	separatorText := string(filepath.Separator)
	return driveText + separatorText + strings.Join(pathSteps, separatorText)
}

func namesDrive(pathStep string) bool {
	return len(pathStep) == 2 && pathStep[1] == ':' && isASCIILetter(pathStep[0])
}

// fileURLOf is the `file` URL of a machine path.
func fileURLOf(machinePath string) string {
	volumeName := filepath.VolumeName(machinePath)
	var encodedSteps []string
	if volumeName != "" {
		encodedSteps = append(encodedSteps, percentEncoded(volumeName))
	}
	for _, pathStep := range strings.Split(filepath.ToSlash(machinePath[len(volumeName):]), "/") {
		if pathStep != "" && pathStep != "." && pathStep != ".." {
			encodedSteps = append(encodedSteps, percentEncoded(pathStep))
		}
	}
	return "file:///" + strings.Join(encodedSteps, "/")
}

func percentEncoded(pathStep string) string {
	var encodedBuilder strings.Builder
	for _, stepByte := range []byte(pathStep) {
		if isASCIILetter(stepByte) || isASCIIDigit(stepByte) || strings.IndexByte("-._~!$&'()*+,;=:@", stepByte) >= 0 {
			encodedBuilder.WriteByte(stepByte)
		} else {
			fmt.Fprintf(&encodedBuilder, "%%%02X", stepByte)
		}
	}
	return encodedBuilder.String()
}

func percentDecoded(encodedText string) string {
	decodedBytes := make([]byte, 0, len(encodedText))
	for byteAt := 0; byteAt < len(encodedText); {
		if encodedText[byteAt] == '%' && byteAt+3 <= len(encodedText) {
			if escapedByte, parseError := strconv.ParseUint(encodedText[byteAt+1:byteAt+3], 16, 8); parseError == nil {
				decodedBytes = append(decodedBytes, byte(escapedByte))
				byteAt += 3
				continue
			}
		}
		decodedBytes = append(decodedBytes, encodedText[byteAt])
		byteAt++
	}
	return strings.ToValidUTF8(string(decodedBytes), "�")
}

// firstMatchingFile is the first file a path names, each `*` step matching any run of characters within the step,
// the steps' matches read in path order.
func firstMatchingFile(patternPath string) (string, bool) {
	volumeName := filepath.VolumeName(patternPath)
	reachedPaths := []string{volumeName + string(filepath.Separator)}
	for _, pathStep := range strings.Split(filepath.ToSlash(patternPath[len(volumeName):]), "/") {
		if pathStep == "" || pathStep == "." {
			continue
		}
		if !strings.Contains(pathStep, "*") {
			for reachedIndex, reachedPath := range reachedPaths {
				reachedPaths[reachedIndex] = filepath.Join(reachedPath, pathStep)
			}
			continue
		}
		var matchedPaths []string
		for _, reachedFolder := range reachedPaths {
			folderEntries, readError := os.ReadDir(reachedFolder)
			if readError != nil {
				continue
			}
			for _, folderEntry := range folderEntries {
				if stepMatches(pathStep, folderEntry.Name()) {
					matchedPaths = append(matchedPaths, filepath.Join(reachedFolder, folderEntry.Name()))
				}
			}
		}
		slices.SortFunc(matchedPaths, func(firstPath, secondPath string) int {
			return slices.Compare(strings.Split(firstPath, string(filepath.Separator)),
				strings.Split(secondPath, string(filepath.Separator)))
		})
		reachedPaths = matchedPaths
	}
	for _, reachedPath := range reachedPaths {
		if pathInfo, statError := os.Stat(reachedPath); statError == nil && pathInfo.Mode().IsRegular() {
			return reachedPath, true
		}
	}
	return "", false
}

// stepMatches is whether a name matches a step whose `*` matches any run of characters.
func stepMatches(patternStep, entryName string) bool {
	stepParts := strings.Split(patternStep, "*")
	firstPart, lastPart := stepParts[0], stepParts[len(stepParts)-1]
	if !strings.HasPrefix(entryName, firstPart) || len(entryName) < len(firstPart)+len(lastPart) ||
		!strings.HasSuffix(entryName, lastPart) {
		return false
	}
	middleText := entryName[len(firstPart) : len(entryName)-len(lastPart)]
	for _, middlePart := range stepParts[1 : len(stepParts)-1] {
		partAt := strings.Index(middleText, middlePart)
		if partAt < 0 {
			return false
		}
		middleText = middleText[partAt+len(middlePart):]
	}
	return true
}

func hasPattern(machinePath string) bool {
	return strings.Contains(machinePath, "*")
}
