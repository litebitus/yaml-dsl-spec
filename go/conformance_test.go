package yamldslspec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"testing"
)

const conformanceFolder = "../conformance"

func readOptionalJSON(testingContext *testing.T, jsonPath string) (any, bool) {
	fileContent, readError := os.ReadFile(jsonPath)
	if readError != nil {
		return nil, false
	}
	var decodedValue any
	if decodeError := json.Unmarshal(fileContent, &decodedValue); decodeError != nil {
		testingContext.Fatalf("%s: %v", jsonPath, decodeError)
	}
	return decodedValue, true
}

func normalizedJSON(testingContext *testing.T, jsonValue any) any {
	encodedValue, encodeError := json.Marshal(jsonValue)
	if encodeError != nil {
		testingContext.Fatal(encodeError)
	}
	var decodedValue any
	if decodeError := json.Unmarshal(encodedValue, &decodedValue); decodeError != nil {
		testingContext.Fatal(decodeError)
	}
	return decodedValue
}

func caseFolders(testingContext *testing.T, caseCategory, inputName string) []string {
	directoryEntries, readError := os.ReadDir(filepath.Join(conformanceFolder, caseCategory))
	if readError != nil {
		testingContext.Fatal(readError)
	}
	var caseFolderPaths []string
	for _, directoryEntry := range directoryEntries {
		caseFolder := filepath.Join(conformanceFolder, caseCategory, directoryEntry.Name())
		if _, statError := os.Stat(filepath.Join(caseFolder, inputName)); statError == nil {
			caseFolderPaths = append(caseFolderPaths, caseFolder)
		}
	}
	sort.Strings(caseFolderPaths)
	if len(caseFolderPaths) == 0 {
		testingContext.Fatalf("no %s cases", caseCategory)
	}
	return caseFolderPaths
}

func runCases(testingContext *testing.T, caseCategory, inputName string, caseReading func(*testing.T, string) any) {
	for _, caseFolder := range caseFolders(testingContext, caseCategory, inputName) {
		testingContext.Run(filepath.Base(caseFolder), func(caseContext *testing.T) {
			actualValue := normalizedJSON(caseContext, caseReading(caseContext, caseFolder))
			expectedValue, _ := readOptionalJSON(caseContext, filepath.Join(caseFolder, "expected.json"))
			if !reflect.DeepEqual(actualValue, expectedValue) {
				prettyText, _ := json.MarshalIndent(actualValue, "", "  ")
				caseContext.Errorf("%s/%s reads to:\n%s", caseCategory, filepath.Base(caseFolder), prettyText)
			}
		})
	}
}

func problemJSON(readProblem Diagnostic) map[string]any {
	problemFields := map[string]any{"message": readProblem.Message}
	if readProblem.Position != nil {
		problemFields["line"] = readProblem.Position.Line
		problemFields["column"] = readProblem.Position.Column
	}
	if readProblem.Notes != nil {
		for noteName, noteText := range map[string]*string{
			"label":  readProblem.Notes.Label,
			"rule":   readProblem.Notes.Rule,
			"schema": readProblem.Notes.Schema,
			"help":   readProblem.Notes.Help,
		} {
			if noteText != nil {
				problemFields[noteName] = *noteText
			}
		}
	}
	return problemFields
}

func problemsJSON(readProblems []Diagnostic) []any {
	listedProblems := []any{}
	for _, readProblem := range readProblems {
		listedProblems = append(listedProblems, problemJSON(readProblem))
	}
	return listedProblems
}

func nodeJSON(yamlNode Node) map[string]any {
	kindNames := map[NodeKind]string{
		NodeNull:   "null",
		NodeBool:   "bool",
		NodeNumber: "number",
		NodeString: "string",
		NodeList:   "list",
		NodeMap:    "map",
	}
	nodeFields := map[string]any{
		"kind":   kindNames[yamlNode.Kind],
		"line":   yamlNode.Position.Line,
		"column": yamlNode.Position.Column,
	}
	switch yamlNode.Kind {
	case NodeBool:
		nodeFields["value"] = yamlNode.Flag
	case NodeNumber, NodeString:
		nodeFields["value"] = yamlNode.Text
	case NodeList:
		listedItems := []any{}
		for _, listItem := range yamlNode.Items {
			listedItems = append(listedItems, nodeJSON(listItem))
		}
		nodeFields["items"] = listedItems
	case NodeMap:
		listedEntries := []any{}
		for _, mapEntry := range yamlNode.Entries {
			listedEntries = append(listedEntries, map[string]any{
				"key":    mapEntry.Key,
				"line":   mapEntry.KeyPosition.Line,
				"column": mapEntry.KeyPosition.Column,
				"value":  nodeJSON(mapEntry.Value),
			})
		}
		nodeFields["entries"] = listedEntries
	}
	return nodeFields
}

func definitionsReading(testingContext *testing.T, caseFolder string) any {
	fileContent, readError := os.ReadFile(filepath.Join(caseFolder, "yaml-dsl.yml"))
	if readError != nil {
		testingContext.Fatal(readError)
	}
	declaredDSLs, readProblems := ReadDSLDefinitions("yaml-dsl.yml", string(fileContent))
	if readProblems != nil {
		return map[string]any{"problems": problemsJSON(readProblems)}
	}
	dslIDs := []any{}
	for _, declaredDSL := range declaredDSLs {
		dslIDs = append(dslIDs, declaredDSL.Definition.ID)
	}
	return map[string]any{"dsls": dslIDs}
}

func yamlReading(testingContext *testing.T, caseFolder string) any {
	fileContent, readError := os.ReadFile(filepath.Join(caseFolder, "input.yml"))
	if readError != nil {
		testingContext.Fatal(readError)
	}
	var tagKeys []TagKey
	if tagKeysText, tagKeysError := os.ReadFile(filepath.Join(caseFolder, "tag-keys.json")); tagKeysError == nil {
		if decodeError := json.Unmarshal(tagKeysText, &tagKeys); decodeError != nil {
			testingContext.Fatal(decodeError)
		}
	}
	rootNode, readProblem := ParseYAML("input.yml", string(fileContent), tagKeys)
	if readProblem != nil {
		return map[string]any{"problem": problemJSON(*readProblem)}
	}
	return map[string]any{"node": nodeJSON(rootNode)}
}

func schemaReading(testingContext *testing.T, caseFolder string) any {
	schemaText, schemaError := os.ReadFile(filepath.Join(caseFolder, "schema.json"))
	documentText, documentError := os.ReadFile(filepath.Join(caseFolder, "document.yml"))
	if schemaError != nil || documentError != nil {
		testingContext.Fatal(schemaError, documentError)
	}
	var dslValidator *Validator
	if validatorValue, isPresent := readOptionalJSON(testingContext, filepath.Join(caseFolder, "validator.json")); isPresent {
		dslDefinition, definitionError := DSLDefinitionFromJSON(map[string]any{
			"id":            "mock",
			"file_includes": []any{},
			"validator":     validatorValue,
		})
		if definitionError != nil {
			testingContext.Fatal(definitionError)
		}
		dslValidator = dslDefinition.Validator
	}
	validationRules, rulesError := ValidationRulesFromValidator(dslValidator)
	if rulesError != nil {
		testingContext.Fatal(rulesError)
	}
	rootNode, readProblem := ParseYAML("document.yml", string(documentText), nil)
	if readProblem != nil {
		testingContext.Fatal(readProblem)
	}
	loadedSchema, loadError := NewSchemaCache().Load("schema.json", string(schemaText))
	if loadError != nil {
		return map[string]any{"error": loadError.Error()}
	}
	var unknownPointers []string
	if pointersValue, isPresent := readOptionalJSON(testingContext, filepath.Join(caseFolder, "unknown-values.json")); isPresent {
		for _, pointerValue := range pointersValue.([]any) {
			unknownPointers = append(unknownPointers, pointerValue.(string))
		}
	}
	mergeCalls := map[string]string{}
	if callsValue, isPresent := readOptionalJSON(testingContext, filepath.Join(caseFolder, "merge-calls.json")); isPresent {
		for callPointer, macroName := range callsValue.(map[string]any) {
			mergeCalls[callPointer] = macroName.(string)
		}
	}
	valueReadAt := func(valuePointer string, writtenValue ResolvedValue) (ResolvedValue, bool) {
		if slices.Contains(unknownPointers, valuePointer) {
			return unknownReference(writtenValue), true
		}
		if macroName, isCall := mergeCalls[valuePointer]; isCall {
			callArguments := []ResolvedValue{unknownReference(writtenValue), writtenValue}
			mergeCall := &FunctionCall{Function: macroName, Arguments: callArguments}
			return ResolvedValue{Kind: ResolvedCall, Position: writtenValue.Position, Call: mergeCall}, true
		}
		return ResolvedValue{}, false
	}
	documentValue := withValuesRead(ResolvedValueFromNode(rootNode), "", valueReadAt)
	subjectProblems := loadedSchema.Problems(documentValue, validationRules)
	if fragmentValue, isPresent := readOptionalJSON(testingContext, filepath.Join(caseFolder, "fragment.json")); isPresent {
		fragmentPointer, _ := fragmentValue.(map[string]any)["pointer"].(string)
		subjectProblems = loadedSchema.FragmentProblems(fragmentPointer, documentValue, validationRules)
	}
	var readProblems []Diagnostic
	readSubjects := []any{}
	for _, subjectProblem := range subjectProblems {
		readProblems = append(readProblems, subjectProblem.Diagnostic)
		readSubjects = append(readSubjects, subjectProblem.Subject)
	}
	if expectedSubjects, isPresent := readOptionalJSON(testingContext, filepath.Join(caseFolder, "subjects.json")); isPresent {
		if !reflect.DeepEqual(readSubjects, expectedSubjects) {
			testingContext.Errorf("%s problems are about %q", filepath.Base(caseFolder), readSubjects)
		}
	}
	return map[string]any{"problems": problemsJSON(readProblems)}
}

func unknownReference(writtenValue ResolvedValue) ResolvedValue {
	referenceText, _ := writtenValue.ScalarText()
	declarationPosition := writtenValue.Position
	return ResolvedValue{
		Kind:     ResolvedReference,
		Position: writtenValue.Position,
		Reference: &Reference{
			Scope:  "mock",
			Name:   "mock",
			Text:   referenceText,
			Target: ReferenceTarget{Kind: TargetDeclaration, DeclarationPosition: &declarationPosition},
		},
	}
}

func withValuesRead(
	documentValue ResolvedValue,
	valuePointer string,
	valueReadAt func(string, ResolvedValue) (ResolvedValue, bool),
) ResolvedValue {
	if replacementValue, isReplaced := valueReadAt(valuePointer, documentValue); isReplaced {
		return replacementValue
	}
	readValue := documentValue
	readValue.Entries = nil
	for _, mapEntry := range documentValue.Entries {
		entryPointer := valuePointer + "/" + escapePointer(mapEntry.Key)
		mapEntry.Value = withValuesRead(mapEntry.Value, entryPointer, valueReadAt)
		readValue.Entries = append(readValue.Entries, mapEntry)
	}
	readValue.Items = nil
	for itemIndex, listItem := range documentValue.Items {
		itemPointer := valuePointer + "/" + strconv.Itoa(itemIndex)
		readValue.Items = append(readValue.Items, withValuesRead(listItem, itemPointer, valueReadAt))
	}
	return readValue
}

func patternReading(patternCase map[string]any) map[string]any {
	patternText, _ := patternCase["pattern"].(string)
	compiledPattern, compileError := CompilePattern(patternText)
	if compileError != nil {
		return map[string]any{"pattern": patternText, "compiles": false}
	}
	groupNames := []any{}
	for _, groupName := range compiledPattern.GroupNames() {
		groupNames = append(groupNames, groupName)
	}
	caseTexts, _ := patternCase["texts"].([]any)
	readTexts := []any{}
	for _, caseText := range caseTexts {
		matchedText, _ := caseText.(string)
		foundMatch, isMatched := compiledPattern.FindMatch(matchedText)
		if !isMatched {
			readTexts = append(readTexts, map[string]any{"text": matchedText, "captures": nil})
			continue
		}
		groupCaptures := map[string]any{"match": foundMatch.WholeMatch}
		for _, groupName := range compiledPattern.GroupNames() {
			if capturedText, isCaptured := foundMatch.NamedGroups[groupName]; isCaptured {
				groupCaptures[groupName] = capturedText
			} else {
				groupCaptures[groupName] = nil
			}
		}
		readTexts = append(readTexts, map[string]any{"text": matchedText, "captures": groupCaptures})
	}
	return map[string]any{"pattern": patternText, "compiles": true, "groups": groupNames, "texts": readTexts}
}

func TestEveryDefinitionsCaseReadsAsExpected(testingContext *testing.T) {
	runCases(testingContext, "definitions", "yaml-dsl.yml", definitionsReading)
}

func TestEveryYAMLCaseReadsAsExpected(testingContext *testing.T) {
	runCases(testingContext, "yaml", "input.yml", yamlReading)
}

func TestEverySchemaCaseValidatesAsExpected(testingContext *testing.T) {
	runCases(testingContext, "schema", "schema.json", schemaReading)
}

func TestEveryFormatCaseWritesAsExpected(testingContext *testing.T) {
	casesValue, isPresent := readOptionalJSON(testingContext, filepath.Join(conformanceFolder, "format", "cases.json"))
	if !isPresent {
		testingContext.Fatal("no format cases")
	}
	listedCases, _ := casesValue.(map[string]any)["cases"].([]any)
	actualCases := []any{}
	for _, listedCase := range listedCases {
		formatCase, _ := listedCase.(map[string]any)
		layoutText, _ := formatCase["layout"].(string)
		caseOperands, _ := formatCase["operands"].([]any)
		writtenText, formatError := FormatLayout(layoutText, caseOperands)
		if formatError != nil {
			actualCases = append(actualCases, map[string]any{"layout": layoutText, "problem": formatError.Error()})
		} else {
			actualCases = append(actualCases, map[string]any{"layout": layoutText, "text": writtenText})
		}
	}
	actualValue := normalizedJSON(testingContext, map[string]any{"cases": actualCases})
	expectedValue, _ := readOptionalJSON(testingContext, filepath.Join(conformanceFolder, "format", "expected.json"))
	if !reflect.DeepEqual(actualValue, expectedValue) {
		prettyText, _ := json.MarshalIndent(actualValue, "", "  ")
		testingContext.Errorf("format cases wrote:\n%s", prettyText)
	}
}

func layoutReading(layoutText string) map[string]any {
	layoutParts, layoutError := ParseLayout(layoutText)
	if layoutError != nil {
		return map[string]any{"layout": layoutText, "problem": layoutError.Error()}
	}
	readParts := []any{}
	for _, layoutPart := range layoutParts {
		if layoutPart.Verb == nil {
			readParts = append(readParts, map[string]any{"text": layoutPart.Text})
			continue
		}
		readParts = append(readParts, map[string]any{
			"verb":         string(layoutPart.Verb.Letter),
			"left_aligned": layoutPart.Verb.LeftAligned,
			"plus_signed":  layoutPart.Verb.PlusSigned,
			"zero_padded":  layoutPart.Verb.ZeroPadded,
			"width":        layoutPart.Verb.Width,
			"precision":    layoutPart.Verb.Precision,
		})
	}
	return map[string]any{"layout": layoutText, "parts": readParts}
}

func TestEveryLayoutCaseReadsAsExpected(testingContext *testing.T) {
	casesValue, isPresent := readOptionalJSON(testingContext, filepath.Join(conformanceFolder, "layouts", "cases.json"))
	if !isPresent {
		testingContext.Fatal("no layout cases")
	}
	listedLayouts, _ := casesValue.(map[string]any)["layouts"].([]any)
	actualCases := []any{}
	for _, listedLayout := range listedLayouts {
		layoutText, _ := listedLayout.(string)
		actualCases = append(actualCases, layoutReading(layoutText))
	}
	actualValue := normalizedJSON(testingContext, map[string]any{"cases": actualCases})
	expectedValue, _ := readOptionalJSON(testingContext, filepath.Join(conformanceFolder, "layouts", "expected.json"))
	if !reflect.DeepEqual(actualValue, expectedValue) {
		prettyText, _ := json.MarshalIndent(actualValue, "", "  ")
		testingContext.Errorf("layouts read to:\n%s", prettyText)
	}
}

func TestEveryPatternCaseCompilesAndMatchesAsExpected(testingContext *testing.T) {
	casesValue, isPresent := readOptionalJSON(testingContext, filepath.Join(conformanceFolder, "patterns", "cases.json"))
	if !isPresent {
		testingContext.Fatal("no pattern cases")
	}
	listedCases, _ := casesValue.(map[string]any)["cases"].([]any)
	actualCases := []any{}
	for _, listedCase := range listedCases {
		patternCase, _ := listedCase.(map[string]any)
		actualCases = append(actualCases, patternReading(patternCase))
	}
	actualValue := normalizedJSON(testingContext, map[string]any{"cases": actualCases})
	expectedValue, _ := readOptionalJSON(testingContext, filepath.Join(conformanceFolder, "patterns", "expected.json"))
	if !reflect.DeepEqual(actualValue, expectedValue) {
		prettyText, _ := json.MarshalIndent(actualValue, "", "  ")
		testingContext.Errorf("patterns read to:\n%s", prettyText)
	}
}
