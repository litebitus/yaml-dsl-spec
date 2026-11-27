// Package schema is the YAML DSL standard's schema validation and schema resolution.
package schema

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/litebitus/yaml-dsl-spec/go/core"
)

const compiledSchemaDirectory = "https://yaml-dsl-spec.invalid/"

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
	Table           core.FunctionTable
	compiledSchema  *jsonschema.Schema
	schemaCompiler  *jsonschema.Compiler
	fragmentLock    sync.Mutex
	fragmentSchemas map[string]*jsonschema.Schema
	originalSchema  any
	markedSchema    any
	schemaName      string
	hoistedBranches map[string]string
	schemaLocation  string
}

// preparedSchema is what a schema's text prepares before a validator is built from it: the draft it is read in, the schema as written, and the schema its requiredness markers are read into. The package's own information, kept apart from the validator.
type preparedSchema struct {
	readDraft      schemaDraft
	originalSchema any
	markedSchema   any
}

var preparedDraftNames = map[schemaDraft]string{
	draftSeven:          "draft-07",
	draftTwentyNineteen: "draft-2019-09",
	draftTwentyTwenty:   "draft-2020-12",
}

func parseLoadedSchema(schemaName, schemaText string) (*LoadedSchema, error) {
	schemaPreparation, preparationError := preparedSchemaOf(schemaName, schemaText)
	if preparationError != nil {
		return nil, preparationError
	}
	return loadedSchemaFromPrepared(schemaName, schemaPreparation)
}

func preparedSchemaOf(schemaName, schemaText string) (preparedSchema, error) {
	failedLoad := func(loadCause error) error { return fmt.Errorf("schema %s: %v", schemaName, loadCause) }
	schemaValue, decodeError := core.DecodeOrderedJSON(schemaText)
	if decodeError != nil {
		return preparedSchema{}, failedLoad(decodeError)
	}
	readDraft, draftError := schemaDraftOf(schemaValue)
	if draftError != nil {
		return preparedSchema{}, failedLoad(draftError)
	}
	return preparedSchema{
		readDraft:      readDraft,
		originalSchema: schemaValue,
		markedSchema:   markedSchema(schemaValue, readDraft),
	}, nil
}

// preparedText is the prepared information as a store keeps it, the same in every language.
func (schemaPreparation preparedSchema) preparedText() string {
	return "{\"draft\":" + core.JSONString(preparedDraftNames[schemaPreparation.readDraft]) +
		",\"schema\":" + core.JSONText(schemaPreparation.originalSchema) +
		",\"marked\":" + core.JSONText(schemaPreparation.markedSchema) + "}"
}

func preparedSchemaOfText(storedText string) (preparedSchema, error) {
	storedValue, decodeError := core.DecodeOrderedJSON(storedText)
	if decodeError != nil {
		return preparedSchema{}, decodeError
	}
	draftName, _ := core.ObjectField(storedValue, "draft")
	originalSchema, holdsSchema := core.ObjectField(storedValue, "schema")
	markedValue, holdsMarked := core.ObjectField(storedValue, "marked")
	for readDraft, preparedName := range preparedDraftNames {
		if draftName == preparedName && holdsSchema && holdsMarked {
			return preparedSchema{readDraft: readDraft, originalSchema: originalSchema, markedSchema: markedValue}, nil
		}
	}
	return preparedSchema{}, fmt.Errorf("the stored text is no prepared schema")
}

func loadedSchemaFromPrepared(schemaName string, schemaPreparation preparedSchema) (*LoadedSchema, error) {
	failedLoad := func(loadCause error) error { return fmt.Errorf("schema %s: %v", schemaName, loadCause) }
	schemaValue := schemaPreparation.originalSchema
	readDraft := schemaPreparation.readDraft
	markedValue := schemaPreparation.markedSchema
	functionTable, tableError := core.FunctionTableFromSchema(schemaValue)
	if tableError != nil {
		return nil, failedLoad(tableError)
	}
	schemaHoisting := assertionHoisting{hoistedBranches: map[string]string{}, readKeywords: draftKeywordsByDraft[readDraft]}
	compilableValue := schemaHoisting.hoistedNode(markedValue, "")
	if compilableObject, isObject := compilableValue.(*core.OrderedObject); isObject {
		declaredDraft, _ := compilableObject.FieldValues["$schema"].(string)
		if slices.Contains(draftSevenURIs, declaredDraft) {
			compilableObject.FieldValues["$schema"] = draftSevenURIs[0]
		}
	}
	schemaCompiler := jsonschema.NewCompiler()
	schemaCompiler.DefaultDraft(jsonschema.Draft7)
	schemaCompiler.UseRegexpEngine(func(patternText string) (jsonschema.Regexp, error) {
		compiledPattern, compileError := core.CompilePattern(patternText)
		if compileError != nil {
			return nil, compileError
		}
		return compiledPattern, nil
	})
	if faultError := firstSchemaFault(schemaValue, schemaValue, ""); faultError != nil {
		return nil, failedLoad(faultError)
	}
	schemaLocation := unstatedSchemaLocation(schemaValue)
	libraryFailure := func(libraryError error) error {
		namedText := strings.ReplaceAll(libraryError.Error(), schemaLocation, schemaName)
		return failedLoad(errors.New(strings.ReplaceAll(namedText, compiledSchemaDirectory, "")))
	}
	if resourceError := schemaCompiler.AddResource(
		schemaLocation, core.PlainJSON(compilableValue),
	); resourceError != nil {
		return nil, libraryFailure(resourceError)
	}
	compiledSchema, compileError := schemaCompiler.Compile(schemaLocation)
	if compileError != nil {
		return nil, libraryFailure(compileError)
	}
	return &LoadedSchema{
		Table:           functionTable,
		compiledSchema:  compiledSchema,
		schemaCompiler:  schemaCompiler,
		fragmentSchemas: map[string]*jsonschema.Schema{},
		originalSchema:  schemaValue,
		markedSchema:    markedValue,
		schemaName:      schemaName,
		hoistedBranches: schemaHoisting.hoistedBranches,
		schemaLocation:  schemaLocation,
	}, nil
}

func schemaDraftOf(schemaValue any) (schemaDraft, error) {
	declaredValue, isDeclared := core.ObjectField(schemaValue, "$schema")
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
		core.JSONText(declaredValue))
}

// Validate is every problem of a resolved document, read by the rules, in document order.
func (loadedSchema *LoadedSchema) Validate(
	documentValue core.ResolvedValue, validationRules ValidationRules,
) []core.Diagnostic {
	var collectedProblems []core.Diagnostic
	for _, subjectProblem := range loadedSchema.Problems(documentValue, validationRules) {
		collectedProblems = append(collectedProblems, subjectProblem.Diagnostic)
	}
	return collectedProblems
}

// Problems is every problem Validate gives, each with its subject.
func (loadedSchema *LoadedSchema) Problems(
	documentValue core.ResolvedValue, validationRules ValidationRules,
) []SchemaProblem {
	return loadedSchema.problemsAgainst(loadedSchema.compiledSchema, false, documentValue, validationRules)
}

// FragmentProblems is every problem of a resolved document read against the schema's node at fragmentPointer, such as /$defs/queue, no key required, each with its subject.
func (loadedSchema *LoadedSchema) FragmentProblems(
	fragmentPointer string,
	documentValue core.ResolvedValue,
	validationRules ValidationRules,
) []SchemaProblem {
	fragmentSchema := loadedSchema.compiledFragment(fragmentPointer)
	if fragmentSchema == nil {
		missingMessage := "the schema has no node at " + core.CodeSpan("#"+fragmentPointer)
		return []SchemaProblem{{Subject: "", Diagnostic: core.DiagnosticAt(documentValue.Position, missingMessage)}}
	}
	return loadedSchema.problemsAgainst(fragmentSchema, true, documentValue, validationRules)
}

func (loadedSchema *LoadedSchema) compiledFragment(fragmentPointer string) *jsonschema.Schema {
	loadedSchema.fragmentLock.Lock()
	defer loadedSchema.fragmentLock.Unlock()
	if fragmentSchema, isCompiled := loadedSchema.fragmentSchemas[fragmentPointer]; isCompiled {
		return fragmentSchema
	}
	var fragmentSchema *jsonschema.Schema
	if _, isNode := core.JSONPointerValue(loadedSchema.originalSchema, fragmentPointer); isNode {
		fragmentLocation := loadedSchema.schemaLocation + "#" + (&url.URL{Fragment: fragmentPointer}).EscapedFragment()
		if compiledSchema, compileError := loadedSchema.schemaCompiler.Compile(fragmentLocation); compileError == nil {
			fragmentSchema = compiledSchema
		}
	}
	loadedSchema.fragmentSchemas[fragmentPointer] = fragmentSchema
	return fragmentSchema
}

func (loadedSchema *LoadedSchema) problemsAgainst(
	validatingSchema *jsonschema.Schema,
	requiresNoKey bool,
	documentValue core.ResolvedValue,
	validationRules ValidationRules,
) []SchemaProblem {
	documentRecord := newInstanceRecord(validationRules, loadedSchema.Table)
	requiresNoKeyAt := documentRecord.requiresNoKeyAt
	if requiresNoKey {
		requiresNoKeyAt = func(string) bool { return true }
	}
	documentJSON := documentRecord.jsonOf(documentValue, "", documentValue.Position)
	documentJSON = loadedSchema.withQuotedValuesRead(validatingSchema, documentJSON, validationRules)
	reportedCovers := reportedSubjects{dependencyKeys: map[string]bool{}, itemLists: map[string]bool{}}
	var collectedProblems []SchemaProblem
	problemBuilder := failureProblems{
		loadedSchema:   loadedSchema,
		documentJSON:   documentJSON,
		documentRecord: documentRecord,
		reportedCovers: &reportedCovers,
	}
	for _, checkFailure := range loadedSchema.failuresOf(validatingSchema, documentJSON, documentRecord, requiresNoKeyAt) {
		if checkFailure.readsUnknownValue(loadedSchema, documentRecord) {
			continue
		}
		for _, builtProblem := range problemBuilder.problemsOf(checkFailure) {
			if !documentRecord.isCopied(builtProblem.Subject) {
				collectedProblems = append(collectedProblems, builtProblem)
			}
		}
	}
	slices.SortStableFunc(collectedProblems, func(firstProblem, secondProblem SchemaProblem) int {
		return compareDiagnostics(firstProblem.Diagnostic, secondProblem.Diagnostic)
	})
	return slices.CompactFunc(collectedProblems, func(firstProblem, secondProblem SchemaProblem) bool {
		return sameDiagnostic(firstProblem.Diagnostic, secondProblem.Diagnostic)
	})
}

func compareDiagnostics(firstProblem, secondProblem core.Diagnostic) int {
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

func comparePositions(firstPosition, secondPosition core.Position) int {
	return cmp.Or(cmp.Compare(firstPosition.Line, secondPosition.Line), cmp.Compare(firstPosition.Column, secondPosition.Column))
}

func sameDiagnostic(firstProblem, secondProblem core.Diagnostic) bool {
	return firstProblem.String() == secondProblem.String() && sameNotes(firstProblem.Notes, secondProblem.Notes)
}

func sameNotes(firstNotes, secondNotes *core.DiagnosticNotes) bool {
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
	typeValue, _ := core.JSONPointerValue(loadedSchema.markedSchema, typePointer)
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
	var fieldNames []string
	reachable, isFound := reachableSchemas(schemaValue, keywordPointer)
	if !isFound {
		return fieldNames
	}
	for _, reachedNode := range reachable.schemaNodes {
		propertiesValue, _ := core.ObjectField(reachedNode.schemaNode, "properties")
		for _, propertyName := range core.ObjectKeys(propertiesValue) {
			if !slices.Contains(fieldNames, propertyName) {
				fieldNames = append(fieldNames, propertyName)
			}
		}
	}
	return fieldNames
}

func fieldCouldBeAccepted(schemaValue any, keywordPointer, fieldName string) bool {
	return schemaCouldRead(schemaValue, keywordPointer, func(schemaNode any, isHolder bool) bool {
		propertiesValue, _ := core.ObjectField(schemaNode, "properties")
		if slices.Contains(core.ObjectKeys(propertiesValue), fieldName) {
			return true
		}
		patternsValue, _ := core.ObjectField(schemaNode, "patternProperties")
		for _, patternText := range core.ObjectKeys(patternsValue) {
			compiledPattern, compileError := core.CompilePattern(patternText)
			if compileError != nil || compiledPattern.MatchString(fieldName) {
				return true
			}
		}
		return isNotFalse(schemaNode, "additionalProperties") ||
			(!isHolder && isNotFalse(schemaNode, "unevaluatedProperties"))
	})
}

func itemCouldBeEvaluated(schemaValue any, keywordPointer string, itemIndex int) bool {
	return schemaCouldRead(schemaValue, keywordPointer, func(schemaNode any, isHolder bool) bool {
		prefixValue, _ := core.ObjectField(schemaNode, "prefixItems")
		prefixSchemas, _ := prefixValue.([]any)
		_, hasContains := core.ObjectField(schemaNode, "contains")
		itemsValue, _ := core.ObjectField(schemaNode, "items")
		itemTuple, isTuple := itemsValue.([]any)
		itemsEvaluate := isNotFalse(schemaNode, "items")
		if isTuple {
			itemsEvaluate = len(itemTuple) > itemIndex || isNotFalse(schemaNode, "additionalItems")
		}
		return len(prefixSchemas) > itemIndex ||
			itemsEvaluate ||
			hasContains ||
			(!isHolder && isNotFalse(schemaNode, "unevaluatedItems"))
	})
}

func isNotFalse(schemaNode any, keywordName string) bool {
	keywordValue, isPresent := core.ObjectField(schemaNode, keywordName)
	return isPresent && keywordValue != false
}

func schemaCouldRead(schemaValue any, keywordPointer string, readsMember func(any, bool) bool) bool {
	reachable, isFound := reachableSchemas(schemaValue, keywordPointer)
	if !isFound || reachable.reachesOtherDocument {
		return true
	}
	for _, reachedNode := range reachable.schemaNodes {
		if readsMember(reachedNode.schemaNode, reachedNode.isHolder) {
			return true
		}
	}
	return false
}

type reachedSchema struct {
	schemaNode any
	isHolder   bool
}

type schemaReach struct {
	schemaNodes          []reachedSchema
	reachesOtherDocument bool
}

func reachableSchemas(schemaValue any, keywordPointer string) (schemaReach, bool) {
	holderPointer := keywordPointer[:max(strings.LastIndexByte(keywordPointer, '/'), 0)]
	holderNode, isFound := core.JSONPointerValue(schemaValue, holderPointer)
	if !isFound {
		return schemaReach{}, false
	}
	var reachable schemaReach
	collectReachable(schemaValue, holderPointer, holderNode, true, &reachable, map[string]bool{})
	return reachable, true
}

func collectReachable(
	schemaValue any,
	nodePointer string,
	schemaNode any,
	isHolder bool,
	reachable *schemaReach,
	visitedPointers map[string]bool,
) {
	reachedNode := reachedSchema{schemaNode: schemaNode, isHolder: isHolder}
	reachable.schemaNodes = append(reachable.schemaNodes, reachedNode)
	referenceValue, _ := core.ObjectField(schemaNode, "$ref")
	if referenceText, isText := referenceValue.(string); isText {
		targetPointer, isPointerReference := referenceTargetPointer(schemaValue, nodePointer, referenceText)
		if !strings.HasPrefix(referenceText, "#") {
			reachable.reachesOtherDocument = true
		} else if isPointerReference && !visitedPointers[targetPointer] {
			visitedPointers[targetPointer] = true
			if targetNode, isFound := core.JSONPointerValue(schemaValue, targetPointer); isFound {
				collectReachable(schemaValue, targetPointer, targetNode, false, reachable, visitedPointers)
			}
		}
	}
	type schemaBranch struct {
		branchPointer string
		branchSchema  any
	}
	var schemaBranches []schemaBranch
	addBranch := func(branchPointer string, branchSchema any) {
		schemaBranches = append(schemaBranches, schemaBranch{branchPointer: branchPointer, branchSchema: branchSchema})
	}
	for _, combinatorKeyword := range []string{"allOf", "anyOf", "oneOf"} {
		branchesValue, _ := core.ObjectField(schemaNode, combinatorKeyword)
		listedBranches, _ := branchesValue.([]any)
		for branchIndex, listedBranch := range listedBranches {
			addBranch(nodePointer+"/"+combinatorKeyword+"/"+strconv.Itoa(branchIndex), listedBranch)
		}
	}
	for _, conditionKeyword := range []string{"if", "then", "else"} {
		if conditionSchema, isPresent := core.ObjectField(schemaNode, conditionKeyword); isPresent {
			addBranch(nodePointer+"/"+conditionKeyword, conditionSchema)
		}
	}
	dependentsValue, _ := core.ObjectField(schemaNode, "dependentSchemas")
	for _, dependentKey := range core.ObjectKeys(dependentsValue) {
		dependentSchema, _ := core.ObjectField(dependentsValue, dependentKey)
		addBranch(nodePointer+"/dependentSchemas/"+core.EscapePointer(dependentKey), dependentSchema)
	}
	for _, reachedBranch := range schemaBranches {
		collectReachable(schemaValue, reachedBranch.branchPointer, reachedBranch.branchSchema, false, reachable, visitedPointers)
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
