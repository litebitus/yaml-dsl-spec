package yamldslspec

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const compiledSchemaLocation = "urn:yaml-dsl-spec:document-schema"

var (
	draftSevenURIs = []string{
		"http://json-schema.org/draft-07/schema#",
		"http://json-schema.org/draft-07/schema",
		"http://json-schema.org/draft-03/schema#",
		"http://json-schema.org/draft-03/schema",
		"http://json-schema.org/draft-04/schema#",
		"http://json-schema.org/draft-04/schema",
		"http://json-schema.org/draft-06/schema#",
		"http://json-schema.org/draft-06/schema",
	}
	draftTwentyNineteenURIs = []string{
		"https://json-schema.org/draft/2019-09/schema",
		"https://json-schema.org/draft/2019-09/schema#",
	}
	draftTwentyTwentyURIs = []string{
		"https://json-schema.org/draft/2020-12/schema",
		"https://json-schema.org/draft/2020-12/schema#",
	}
)

// LoadedSchema is a document's schema, read in its draft, with its function table.
type LoadedSchema struct {
	Table           FunctionTable
	compiledSchema  *jsonschema.Schema
	originalSchema  any
	markedSchema    any
	schemaName      string
	hoistedBranches map[string]string
}

// SchemaCache holds schemas loaded once per name and text, shared by every unit that reads them.
type SchemaCache struct {
	cacheLock     sync.Mutex
	loadedSchemas map[schemaCacheKey]*schemaLoad
}

type schemaCacheKey struct {
	schemaName string
	schemaText string
}

type schemaLoad struct {
	loadOnce     sync.Once
	loadedSchema *LoadedSchema
	loadError    error
}

// NewSchemaCache is an empty schema cache.
func NewSchemaCache() *SchemaCache {
	return &SchemaCache{loadedSchemas: map[schemaCacheKey]*schemaLoad{}}
}

// Load is the schema a text holds, named as reports name it, read once per name and text; else why it does not load.
func (schemaCache *SchemaCache) Load(schemaName, schemaText string) (*LoadedSchema, error) {
	schemaCache.cacheLock.Lock()
	if schemaCache.loadedSchemas == nil {
		schemaCache.loadedSchemas = map[schemaCacheKey]*schemaLoad{}
	}
	cacheKey := schemaCacheKey{schemaName: schemaName, schemaText: schemaText}
	pendingLoad, isCached := schemaCache.loadedSchemas[cacheKey]
	if !isCached {
		pendingLoad = &schemaLoad{}
		schemaCache.loadedSchemas[cacheKey] = pendingLoad
	}
	schemaCache.cacheLock.Unlock()
	pendingLoad.loadOnce.Do(func() {
		pendingLoad.loadedSchema, pendingLoad.loadError = parseLoadedSchema(schemaName, schemaText)
	})
	return pendingLoad.loadedSchema, pendingLoad.loadError
}

func parseLoadedSchema(schemaName, schemaText string) (*LoadedSchema, error) {
	failedLoad := func(loadCause error) error { return fmt.Errorf("schema %s: %v", schemaName, loadCause) }
	schemaValue, decodeError := decodeOrderedJSON(schemaText)
	if decodeError != nil {
		return nil, failedLoad(decodeError)
	}
	functionTable, tableError := FunctionTableFromSchema(schemaValue)
	if tableError != nil {
		return nil, failedLoad(tableError)
	}
	readDraft, draftError := schemaDraftOf(schemaValue)
	if draftError != nil {
		return nil, failedLoad(draftError)
	}
	markedValue := markedSchema(schemaValue, readDraft)
	schemaHoisting := assertionHoisting{hoistedBranches: map[string]string{}, readKeywords: draftKeywordsByDraft[readDraft]}
	compilableValue := schemaHoisting.hoistedNode(markedValue, "")
	if compilableObject, isObject := compilableValue.(*orderedObject); isObject {
		declaredDraft, _ := compilableObject.fieldValues["$schema"].(string)
		if slices.Contains(draftSevenURIs, declaredDraft) {
			compilableObject.fieldValues["$schema"] = draftSevenURIs[0]
		}
	}
	schemaCompiler := jsonschema.NewCompiler()
	schemaCompiler.DefaultDraft(jsonschema.Draft7)
	schemaCompiler.UseRegexpEngine(func(patternText string) (jsonschema.Regexp, error) {
		compiledPattern, compileError := CompilePattern(patternText)
		if compileError != nil {
			return nil, compileError
		}
		return compiledPattern, nil
	})
	if resourceError := schemaCompiler.AddResource(compiledSchemaLocation, plainJSON(compilableValue)); resourceError != nil {
		return nil, failedLoad(resourceError)
	}
	compiledSchema, compileError := schemaCompiler.Compile(compiledSchemaLocation)
	if compileError != nil {
		return nil, failedLoad(compileError)
	}
	return &LoadedSchema{
		Table:           functionTable,
		compiledSchema:  compiledSchema,
		originalSchema:  schemaValue,
		markedSchema:    markedValue,
		schemaName:      schemaName,
		hoistedBranches: schemaHoisting.hoistedBranches,
	}, nil
}

func schemaDraftOf(schemaValue any) (schemaDraft, error) {
	declaredValue, isDeclared := objectField(schemaValue, "$schema")
	if !isDeclared {
		return draftSeven, nil
	}
	declaredText, _ := declaredValue.(string)
	switch {
	case slices.Contains(draftSevenURIs, declaredText):
		return draftSeven, nil
	case slices.Contains(draftTwentyNineteenURIs, declaredText):
		return draftTwentyNineteen, nil
	case slices.Contains(draftTwentyTwentyURIs, declaredText):
		return draftTwentyTwenty, nil
	}
	return draftSeven, fmt.Errorf(
		"declares %s, a draft this reader does not read; it reads draft-07, draft 2019-09, draft 2020-12",
		jsonText(declaredValue))
}

// Validate is every problem of a resolved document, read by the rules, in document order.
func (loadedSchema *LoadedSchema) Validate(documentValue ResolvedValue, validationRules ValidationRules) []Diagnostic {
	documentRecord := newInstanceRecord(validationRules)
	documentJSON := documentRecord.jsonOf(documentValue, "", documentValue.Position)
	documentJSON = loadedSchema.withQuotedValuesRead(documentJSON, validationRules)
	reportedCovers := reportedSubjects{dependencyKeys: map[string]bool{}, itemLists: map[string]bool{}}
	var collectedProblems []Diagnostic
	problemBuilder := failureProblems{
		loadedSchema:   loadedSchema,
		documentJSON:   documentJSON,
		documentRecord: documentRecord,
		reportedCovers: &reportedCovers,
	}
	for _, checkFailure := range loadedSchema.failuresOf(documentJSON, documentRecord) {
		if documentRecord.readsUnknown(checkFailure.instancePointer, checkFailure.failedKeyword) {
			continue
		}
		for _, builtProblem := range problemBuilder.problemsOf(checkFailure) {
			if !documentRecord.isCopied(builtProblem.subjectPointer) {
				collectedProblems = append(collectedProblems, builtProblem.builtDiagnostic)
			}
		}
	}
	slices.SortStableFunc(collectedProblems, compareDiagnostics)
	return slices.CompactFunc(collectedProblems, sameDiagnostic)
}

func compareDiagnostics(firstProblem, secondProblem Diagnostic) int {
	switch {
	case firstProblem.Position == nil && secondProblem.Position != nil:
		return -1
	case firstProblem.Position != nil && secondProblem.Position == nil:
		return 1
	case firstProblem.Position != nil:
		if placeOrder := comparePositions(*firstProblem.Position, *secondProblem.Position); placeOrder != 0 {
			return placeOrder
		}
	}
	return strings.Compare(firstProblem.Message, secondProblem.Message)
}

func comparePositions(firstPosition, secondPosition Position) int {
	return cmp.Or(cmp.Compare(firstPosition.Line, secondPosition.Line), cmp.Compare(firstPosition.Column, secondPosition.Column))
}

func sameDiagnostic(firstProblem, secondProblem Diagnostic) bool {
	return firstProblem.String() == secondProblem.String() && sameNotes(firstProblem.Notes, secondProblem.Notes)
}

func sameNotes(firstNotes, secondNotes *DiagnosticNotes) bool {
	if firstNotes == nil || secondNotes == nil {
		return firstNotes == secondNotes
	}
	sameText := func(firstText, secondText *string) bool {
		if firstText == nil || secondText == nil {
			return firstText == secondText
		}
		return *firstText == *secondText
	}
	return sameText(firstNotes.Label, secondNotes.Label) && sameText(firstNotes.Rule, secondNotes.Rule) &&
		sameText(firstNotes.Schema, secondNotes.Schema) && sameText(firstNotes.Help, secondNotes.Help)
}

func (loadedSchema *LoadedSchema) expectedTypes(typePointer string) []string {
	typeValue, _ := jsonPointerValue(loadedSchema.markedSchema, typePointer)
	switch typedValue := typeValue.(type) {
	case string:
		return []string{typedValue}
	case []any:
		var listedTypes []string
		for _, listedType := range typedValue {
			if typeName, isText := listedType.(string); isText {
				listedTypes = append(listedTypes, typeName)
			}
		}
		return listedTypes
	}
	return nil
}

func allowedFields(schemaValue any, keywordPointer string) []string {
	holderPointer := keywordPointer[:max(strings.LastIndexByte(keywordPointer, '/'), 0)]
	var fieldNames []string
	visitedPointers := map[string]bool{}
	if holderNode, isFound := jsonPointerValue(schemaValue, holderPointer); isFound {
		collectFields(schemaValue, holderNode, &fieldNames, visitedPointers)
	}
	return fieldNames
}

func collectFields(schemaValue, schemaNode any, fieldNames *[]string, visitedPointers map[string]bool) {
	propertiesValue, _ := objectField(schemaNode, "properties")
	for _, propertyName := range objectKeys(propertiesValue) {
		if !slices.Contains(*fieldNames, propertyName) {
			*fieldNames = append(*fieldNames, propertyName)
		}
	}
	referenceValue, _ := objectField(schemaNode, "$ref")
	if referenceText, isText := referenceValue.(string); isText {
		if referencePointer, isLocal := strings.CutPrefix(referenceText, "#"); isLocal && !visitedPointers[referencePointer] {
			visitedPointers[referencePointer] = true
			if targetNode, isFound := jsonPointerValue(schemaValue, referencePointer); isFound {
				collectFields(schemaValue, targetNode, fieldNames, visitedPointers)
			}
		}
	}
	for _, combinatorKeyword := range []string{"allOf", "anyOf", "oneOf"} {
		branchesValue, _ := objectField(schemaNode, combinatorKeyword)
		branchSchemas, _ := branchesValue.([]any)
		for _, branchSchema := range branchSchemas {
			collectFields(schemaValue, branchSchema, fieldNames, visitedPointers)
		}
	}
}

// ClosestName is the candidate a misspelt name most likely means, where one is close enough and no other is as close.
func ClosestName(misspeltName string, candidateNames []string) (string, bool) {
	type rankedCandidate struct {
		editCount     int
		candidateName string
	}
	var rankedCandidates []rankedCandidate
	for _, candidateName := range candidateNames {
		if candidateName != misspeltName {
			rankedCandidates = append(rankedCandidates, rankedCandidate{
				editCount:     editDistance(misspeltName, candidateName),
				candidateName: candidateName,
			})
		}
	}
	slices.SortFunc(rankedCandidates, func(firstCandidate, secondCandidate rankedCandidate) int {
		return cmp.Or(
			cmp.Compare(firstCandidate.editCount, secondCandidate.editCount),
			strings.Compare(firstCandidate.candidateName, secondCandidate.candidateName),
		)
	})
	editLimit := max(len([]rune(misspeltName))/3, 1)
	if len(rankedCandidates) == 0 || rankedCandidates[0].editCount > editLimit {
		return "", false
	}
	if len(rankedCandidates) > 1 && rankedCandidates[1].editCount <= rankedCandidates[0].editCount {
		return "", false
	}
	return rankedCandidates[0].candidateName, true
}

func editDistance(firstText, secondText string) int {
	secondRunes := []rune(secondText)
	previousRow := make([]int, len(secondRunes)+1)
	for columnIndex := range previousRow {
		previousRow[columnIndex] = columnIndex
	}
	for rowIndex, firstRune := range []rune(firstText) {
		currentRow := []int{rowIndex + 1}
		for columnIndex, secondRune := range secondRunes {
			substitutionCost := previousRow[columnIndex]
			if firstRune != secondRune {
				substitutionCost++
			}
			currentRow = append(currentRow, min(substitutionCost, previousRow[columnIndex+1]+1, currentRow[columnIndex]+1))
		}
		previousRow = currentRow
	}
	return previousRow[len(secondRunes)]
}
