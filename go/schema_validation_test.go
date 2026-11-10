package yamldslspec

import (
	"reflect"
	"slices"
	"sync"
	"testing"
)

const mockQueueSchema = `{
  "type": "object",
  "properties": {
    "queue": {
      "type": "object",
      "additionalProperties": {
        "type": "object",
        "required": ["name", "visibility"],
        "properties": {
          "name": {"type": "string"},
          "visibility": {"type": "integer"},
          "encrypted": {"type": "boolean"}
        }
      }
    }
  }
}`

func loadedMockSchema(testingContext *testing.T, schemaText string) *LoadedSchema {
	loadedSchema, loadError := NewSchemaCache().Load("mock.schema.json", schemaText)
	if loadError != nil {
		testingContext.Fatal(loadError)
	}
	return loadedSchema
}

func problemMessages(readProblems []Diagnostic) []string {
	problemTexts := []string{}
	for _, readProblem := range readProblems {
		problemTexts = append(problemTexts, readProblem.Message)
	}
	return problemTexts
}

func TestAKeySetsItsEntryFieldBeforeValidation(testingContext *testing.T) {
	childPattern, compileError := CompilePattern("^(?<field>[a-z]+) (?<value>[a-z-]+)$")
	if compileError != nil {
		testingContext.Fatal(compileError)
	}
	documentValue := ResolvedValueFromNode(parsedNode(testingContext, "queue:\n  name mock-orders:\n    visibility: 30\n", nil))
	validationRules := ValidationRules{ChildValuePatterns: []*CompiledPattern{childPattern}}
	readProblems := loadedMockSchema(testingContext, mockQueueSchema).Validate(documentValue, validationRules)
	if len(readProblems) != 0 {
		testingContext.Errorf("problems %v", readProblems)
	}
}

func TestAMergeBaseKeySuppliesItsSiblingsAndAQuotedValueReadsAsItsType(testingContext *testing.T) {
	documentText := "queue:\n" +
		"  defaults: {name: mock, visibility: 30}\n" +
		"  orders: {encrypted: \"true\"}\n" +
		"  payments: {encrypted: \"yes\"}\n"
	documentValue := ResolvedValueFromNode(parsedNode(testingContext, documentText, nil))
	validationRules := ValidationRules{MergeBaseKeys: []string{"defaults"}, QuotedValueTypes: []QuotedValueType{QuotedValueBoolean}}
	problemTexts := problemMessages(loadedMockSchema(testingContext, mockQueueSchema).Validate(documentValue, validationRules))
	if !reflect.DeepEqual(problemTexts, []string{"`encrypted` must be `boolean`"}) {
		testingContext.Errorf("messages %v", problemTexts)
	}
}

func TestACallIsNotCheckedAndAWrongTypeIs(testingContext *testing.T) {
	mockPosition := FileStartPosition("mock.yml")
	callValue := ResolvedValue{Kind: ResolvedCall, Position: mockPosition, Call: &FunctionCall{Function: "mock"}}
	ordersValue := ResolvedValue{Kind: ResolvedMap, Position: mockPosition, Entries: []MapEntry{
		{Key: "name", KeyPosition: mockPosition, Value: callValue},
		{Key: "visibility", KeyPosition: mockPosition, Value: StringValue("thirty", mockPosition)},
	}}
	ordersEntries := []MapEntry{{Key: "orders", KeyPosition: mockPosition, Value: ordersValue}}
	queueValue := ResolvedValue{Kind: ResolvedMap, Position: mockPosition, Entries: ordersEntries}
	queueEntries := []MapEntry{{Key: "queue", KeyPosition: mockPosition, Value: queueValue}}
	documentValue := ResolvedValue{Kind: ResolvedMap, Position: mockPosition, Entries: queueEntries}
	readProblems := loadedMockSchema(testingContext, mockQueueSchema).Validate(documentValue, ValidationRules{})
	if !reflect.DeepEqual(problemMessages(readProblems), []string{"`visibility` must be `integer`"}) {
		testingContext.Fatalf("problems %v", readProblems)
	}
	expectedRule := "type, at mock.schema.json#/properties/queue/additionalProperties/properties/visibility/type"
	if *readProblems[0].Notes.Label != "is a string" || *readProblems[0].Notes.Rule != expectedRule {
		testingContext.Errorf("notes %+v", readProblems[0].Notes)
	}
}

func TestATypeAndAnotherCheckOfOneSchemaAreBothReported(testingContext *testing.T) {
	schemaText := `{"properties": {"tier": {"type": "string", "enum": ["gold"]}}}`
	documentValue := ResolvedValueFromNode(parsedNode(testingContext, "tier: 5\n", nil))
	problemTexts := problemMessages(loadedMockSchema(testingContext, schemaText).Validate(documentValue, ValidationRules{}))
	expectedTexts := []string{"`tier` must be `string`", "`tier` must be one of `\"gold\"`"}
	if !reflect.DeepEqual(problemTexts, expectedTexts) {
		testingContext.Errorf("messages %v", problemTexts)
	}
}

func TestAKeyPropertyNamesRefusesIsOneProblem(testingContext *testing.T) {
	schemaText := `{"additionalProperties": {"propertyNames": {"maxLength": 2, "pattern": "^[a-z]+$"}}}`
	documentValue := ResolvedValueFromNode(parsedNode(testingContext, "first: {Bad: 1}\nsecond: {ok: 1, BAD: 2}\n", nil))
	readProblems := loadedMockSchema(testingContext, schemaText).Validate(documentValue, ValidationRules{})
	expectedTexts := []string{"`Bad` is not a valid key of `first`", "`BAD` is not a valid key of `second`"}
	if !reflect.DeepEqual(problemMessages(readProblems), expectedTexts) {
		testingContext.Fatalf("problems %v", readProblems)
	}
	if readProblems[1].Position.Line != 2 || readProblems[1].Position.Column != 17 {
		testingContext.Errorf("second key at %v", readProblems[1].Position)
	}
}

func TestAnUnknownDraftIsRefused(testingContext *testing.T) {
	_, loadError := NewSchemaCache().Load("mock.schema.json", `{"$schema": "https://json-schema.org/draft/2030-01/schema"}`)
	expectedMessage := `schema mock.schema.json: declares "https://json-schema.org/draft/2030-01/schema", ` +
		"a draft this reader does not read; it reads draft-07, draft 2019-09, draft 2020-12"
	if loadError == nil || loadError.Error() != expectedMessage {
		testingContext.Errorf("error %v", loadError)
	}
}

func TestAMarkerDecidesOverTheRequiredArrayAndAConditionIsLeftAsWritten(testingContext *testing.T) {
	schemaValue, decodeError := decodeOrderedJSON(`{
	  "properties": {
	    "name": {"description": "[required] The name."},
	    "listed": {},
	    "legacy": {"description": "[optional] Kept."},
	    "maybe": {"description": "[~required] Sometimes."}
	  },
	  "required": ["legacy", "listed", "maybe"],
	  "if": {"required": ["legacy"]},
	  "anyOf": [{"required": ["legacy"]}],
	  "allOf": [{"required": ["legacy", "listed"]}]
	}`)
	if decodeError != nil {
		testingContext.Fatal(decodeError)
	}
	markedFields := plainJSON(markedSchema(schemaValue, draftTwentyTwenty)).(map[string]any)
	if !reflect.DeepEqual(markedFields["required"], []any{"listed", "name"}) ||
		!reflect.DeepEqual(markedFields["if"], map[string]any{"required": []any{"legacy"}}) ||
		!reflect.DeepEqual(markedFields["anyOf"], []any{map[string]any{"required": []any{"legacy"}}}) ||
		!reflect.DeepEqual(markedFields["allOf"], []any{map[string]any{"required": []any{"listed"}}}) {
		testingContext.Errorf("marked %v", markedFields)
	}
}

func TestAQuotedValueReadsAsTheFirstExpectedTypeAllowedThatItSpells(testingContext *testing.T) {
	validationRules := ValidationRules{QuotedValueTypes: []QuotedValueType{QuotedValueBoolean, QuotedValueNumber}}
	quotedCases := []struct {
		quotedText    string
		expectedTypes []string
		readValue     any
		isSpelled     bool
	}{
		{"true", []string{"boolean"}, true, true},
		{"1.5e3", []string{"number"}, 1500.0, true},
		{"7", []string{"integer"}, nil, false},
		{"yes", []string{"boolean"}, nil, false},
		{"01", []string{"number"}, nil, false},
	}
	for _, quotedCase := range quotedCases {
		readValue, isSpelled := validationRules.quotedValue(quotedCase.quotedText, quotedCase.expectedTypes)
		if isSpelled != quotedCase.isSpelled || !reflect.DeepEqual(readValue, quotedCase.readValue) {
			testingContext.Errorf("%q reads as %v %v", quotedCase.quotedText, readValue, isSpelled)
		}
	}
}

func TestAnOverlayMergesOverItsBaseAndWhatItCopiesIsKnown(testingContext *testing.T) {
	baseValue := map[string]any{"visibility": int64(30), "tags": map[string]any{"team": "mock", "tier": "gold"}, "gone": int64(1)}
	overlayFields := map[string]any{"tags": map[string]any{"tier": "silver"}, "gone": nil, "later": nil}
	isLaterUnknown := func(valuePointer string) bool { return valuePointer == "/orders/later" }
	mergedFields := mergedOver(baseValue, overlayFields, "/orders", isLaterUnknown)
	expectedFields := map[string]any{"visibility": int64(30), "tags": map[string]any{"team": "mock", "tier": "silver"}, "later": nil}
	if !reflect.DeepEqual(mergedFields, expectedFields) {
		testingContext.Errorf("merged %v", mergedFields)
	}
	copiedSet := map[string]bool{}
	for _, copiedPointer := range copiedPointers(mergedFields, overlayFields, "/orders") {
		copiedSet[copiedPointer] = true
	}
	if !reflect.DeepEqual(copiedSet, map[string]bool{"/orders/visibility": true, "/orders/tags/team": true}) {
		testingContext.Errorf("copied %v", copiedSet)
	}
	if baseValue["tags"].(map[string]any)["tier"] != "gold" {
		testingContext.Error("the merge changed its base")
	}
}

func TestAValueIsFencedInMoreBackticksThanItHoldsInARow(testingContext *testing.T) {
	fencedValues := map[string]string{
		"on":     "`on`",
		"a`b":    "``a`b``",
		"a``b`c": "```a``b`c```",
		"`on":    "`` `on ``",
		"on`":    "`` on` ``",
	}
	for plainText, fencedText := range fencedValues {
		if CodeSpan(plainText) != fencedText {
			testingContext.Errorf("%q fences as %q", plainText, CodeSpan(plainText))
		}
	}
}

func TestAFieldIsNamedByItsKeyAndTheListIndexesBelowIt(testingContext *testing.T) {
	documentValue := map[string]any{"tags": []any{[]any{"a"}}, "0": map[string]any{"name": int64(1)}}
	namedFields := map[string]string{"": "the document", "/tags/0/0": "`tags[0][0]`", "/0/name": "`name`"}
	for valuePointer, expectedName := range namedFields {
		if fieldName(documentValue, valuePointer) != expectedName {
			testingContext.Errorf("%q names %q", valuePointer, fieldName(documentValue, valuePointer))
		}
	}
	if fieldName([]any{int64(1)}, "/0") != "`[0]`" {
		testingContext.Error("a root list item is not named by its index")
	}
}

func TestADependencyNamesWhatItsKeyNeeds(testingContext *testing.T) {
	schemaValue, _ := decodeOrderedJSON(`{
	  "dependentRequired": {"queue": ["role", "region"]},
	  "dependentSchemas": {"topic": {"not": {"required": ["queue"]}}}
	}`)
	holderValue := map[string]any{"queue": int64(1), "topic": int64(2)}
	missingRole := "role"
	keywordPointer, dependencyKey, isDependency := dependencyOf(schemaValue, "/dependentRequired", holderValue, &missingRole)
	if !isDependency || keywordPointer != "/dependentRequired" || dependencyKey != "queue" {
		testingContext.Fatalf("dependency %q %q", keywordPointer, dependencyKey)
	}
	queueMessage := dependencyMessage(schemaValue, "", dependencyKey)
	if queueMessage != "`queue` needs `role` and `region`" {
		testingContext.Errorf("message %q", queueMessage)
	}
	_, topicKey, _ := dependencyOf(schemaValue, "/dependentSchemas/topic/not", holderValue, nil)
	topicMessage := dependencyMessage(schemaValue, "", topicKey)
	if topicMessage != "`topic` needs `queue` must not be set" {
		testingContext.Errorf("message %q", topicMessage)
	}
}

func TestDuplicateItemsAreFoundFromTheLastItemBack(testingContext *testing.T) {
	laterIndex, earlierIndex, isDuplicated := duplicateItems([]any{"a", "b", "a"})
	if !isDuplicated || laterIndex != 2 || earlierIndex != 0 {
		testingContext.Errorf("duplicates %d %d", laterIndex, earlierIndex)
	}
	laterIndex, earlierIndex, _ = duplicateItems([]any{int64(1), int64(1), 1.0})
	if laterIndex != 2 || earlierIndex != 1 {
		testingContext.Errorf("duplicates %d %d", laterIndex, earlierIndex)
	}
	if _, _, isDuplicated := duplicateItems([]any{int64(1), int64(2)}); isDuplicated {
		testingContext.Error("distinct items are duplicates")
	}
}

func TestAClosestNameIsOnlyOneClearlyClosest(testingContext *testing.T) {
	closestCandidate, isClose := ClosestName("visiblity", []string{"name", "visibility", "retention"})
	if !isClose || closestCandidate != "visibility" {
		testingContext.Errorf("closest %q", closestCandidate)
	}
	if _, isClose := ClosestName("aa", []string{"ab", "ac"}); isClose {
		testingContext.Error("a tie names a candidate")
	}
	if _, isClose := ClosestName("platinum", []string{"gold", "silver"}); isClose {
		testingContext.Error("a far name names a candidate")
	}
}

func TestFloatsAreWrittenAsTheReferenceWritesThem(testingContext *testing.T) {
	writtenFloats := map[float64]string{
		1e16:    "1e+16",
		1e15:    "1000000000000000.0",
		1e-7:    "1e-7",
		0.00001: "0.00001",
		100:     "100.0",
		1.5:     "1.5",
	}
	for floatValue, expectedText := range writtenFloats {
		if serdeFloatText(floatValue) != expectedText {
			testingContext.Errorf("%v writes as %q", floatValue, serdeFloatText(floatValue))
		}
	}
}

const mockFragmentSchema = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "properties": {"queue": {"$ref": "#/$defs/mock queue"}},
  "$defs": {
    "mock queue": {
      "type": "object",
      "properties": {"tags": {"$ref": "#/$defs/tags"}, "visibility": {"type": "integer"}},
      "oneOf": [{"required": ["arn"]}, {"required": ["name"]}]
    },
    "tags": {"type": "array"}
  }
}`

func problemSubjects(subjectProblems []SchemaProblem) []string {
	subjectPointers := []string{}
	for _, subjectProblem := range subjectProblems {
		subjectPointers = append(subjectPointers, subjectProblem.Subject)
	}
	return subjectPointers
}

func problemRules(subjectProblems []SchemaProblem) []string {
	ruleTexts := []string{}
	for _, subjectProblem := range subjectProblems {
		ruleTexts = append(ruleTexts, *subjectProblem.Diagnostic.Notes.Rule)
	}
	return ruleTexts
}

func TestProblemsGiveEachProblemOfValidateItsSubject(testingContext *testing.T) {
	documentValue := ResolvedValueFromNode(parsedNode(testingContext, "queue:\n  orders: {visibility: thirty}\n", nil))
	loadedSchema := loadedMockSchema(testingContext, mockQueueSchema)
	subjectProblems := loadedSchema.Problems(documentValue, ValidationRules{})
	if !reflect.DeepEqual(problemSubjects(subjectProblems), []string{"/queue/orders", "/queue/orders/visibility"}) {
		testingContext.Errorf("subjects %v", problemSubjects(subjectProblems))
	}
	var subjectDiagnostics []Diagnostic
	for _, subjectProblem := range subjectProblems {
		subjectDiagnostics = append(subjectDiagnostics, subjectProblem.Diagnostic)
	}
	if !reflect.DeepEqual(loadedSchema.Validate(documentValue, ValidationRules{}), subjectDiagnostics) {
		testingContext.Errorf("validate differs from problems %v", subjectDiagnostics)
	}
}

func TestAFragmentResolvesItsReferencesAgainstTheWholeSchemaAndRequiresNoKey(testingContext *testing.T) {
	loadedSchema := loadedMockSchema(testingContext, mockFragmentSchema)
	fragmentDocument := ResolvedValueFromNode(parsedNode(testingContext, "tags: mock\nvisibility: high\n", nil))
	fragmentProblems := loadedSchema.FragmentProblems("/$defs/mock queue", fragmentDocument, ValidationRules{})
	expectedRules := []string{
		"type, at mock.schema.json#/$defs/tags/type",
		"type, at mock.schema.json#/$defs/mock queue/properties/visibility/type",
	}
	if !reflect.DeepEqual(problemRules(fragmentProblems), expectedRules) {
		testingContext.Errorf("rules %v", problemRules(fragmentProblems))
	}
	if !reflect.DeepEqual(problemSubjects(fragmentProblems), []string{"/tags", "/visibility"}) {
		testingContext.Errorf("subjects %v", problemSubjects(fragmentProblems))
	}
	firstFragment := loadedSchema.compiledFragment("/$defs/mock queue")
	if firstFragment == nil || loadedSchema.compiledFragment("/$defs/mock queue") != firstFragment {
		testingContext.Error("the fragment is not compiled once")
	}
	fullDocument := ResolvedValueFromNode(parsedNode(testingContext, "queue: {}\n", nil))
	fullProblems := problemMessages(loadedSchema.Validate(fullDocument, ValidationRules{}))
	if !reflect.DeepEqual(fullProblems, []string{"`queue` matches none of its allowed forms"}) {
		testingContext.Errorf("full document problems %v", fullProblems)
	}
}

func TestAFragmentPointerNamingNoSchemaNodeIsOneProblem(testingContext *testing.T) {
	loadedSchema := loadedMockSchema(testingContext, mockFragmentSchema)
	documentValue := ResolvedValueFromNode(parsedNode(testingContext, "tags: []\n", nil))
	for _, fragmentPointer := range []string{"/$defs/missing", "/$defs/tags/type", "/$defs/tags/allOf/0"} {
		fragmentProblems := loadedSchema.FragmentProblems(fragmentPointer, documentValue, ValidationRules{})
		expectedProblem := SchemaProblem{
			Subject:    "",
			Diagnostic: DiagnosticAt(documentValue.Position, "the schema has no node at `#"+fragmentPointer+"`"),
		}
		if !reflect.DeepEqual(fragmentProblems, []SchemaProblem{expectedProblem}) {
			testingContext.Errorf("%s gives %v", fragmentPointer, fragmentProblems)
		}
	}
}

func TestAFragmentIsReadByConcurrentReaders(testingContext *testing.T) {
	loadedSchema := loadedMockSchema(testingContext, mockFragmentSchema)
	documentValue := ResolvedValueFromNode(parsedNode(testingContext, "tags: mock\n", nil))
	var readerGroup sync.WaitGroup
	readProblemCounts := make([]int, 8)
	for readerIndex := range readProblemCounts {
		readerGroup.Add(1)
		go func() {
			defer readerGroup.Done()
			readProblemCounts[readerIndex] = len(loadedSchema.FragmentProblems("/$defs/mock queue", documentValue, ValidationRules{}))
		}()
	}
	readerGroup.Wait()
	if !reflect.DeepEqual(readProblemCounts, []int{1, 1, 1, 1, 1, 1, 1, 1}) {
		testingContext.Errorf("problem counts %v", readProblemCounts)
	}
}

func problemLines(subjectProblems []SchemaProblem) []int {
	lineNumbers := []int{}
	for _, subjectProblem := range subjectProblems {
		lineNumbers = append(lineNumbers, subjectProblem.Diagnostic.Position.Line)
	}
	return lineNumbers
}

func TestAMergeBaseKeysContentRequiresNoKey(testingContext *testing.T) {
	schemaText := `{
	  "properties": {"queue": {"additionalProperties": {
	    "type": "object",
	    "required": ["name"],
	    "properties": {"name": {"type": "string"}, "settings": {"type": "object", "required": ["mode"]}},
	    "anyOf": [{"required": ["name"]}, {"required": ["arn"]}]
	  }}}
	}`
	documentText := "queue:\n" +
		"  defaults: {settings: {}}\n" +
		"  orders: {name: mock, settings: {mode: mock}}\n" +
		"  payments: {}\n"
	documentValue := ResolvedValueFromNode(parsedNode(testingContext, documentText, nil))
	validationRules := ValidationRules{MergeBaseKeys: []string{"defaults"}}
	subjectProblems := loadedMockSchema(testingContext, schemaText).Problems(documentValue, validationRules)
	if !reflect.DeepEqual(problemSubjects(subjectProblems), []string{"/queue/payments", "/queue/payments"}) {
		testingContext.Errorf("subjects %v", problemSubjects(subjectProblems))
	}
}

const mockMergeMacrosSchema = `{
  "x-yaml-dsl-functions": {
    "mock_deep": {
      "arguments": [{"name": "contents", "type": "map", "required": true, "repeated": true}],
      "macro": {"kind": "merge", "content": "contents", "merge_behavior": "deep"}
    },
    "mock_shallow": {
      "arguments": [{"name": "contents", "type": "map", "required": true, "repeated": true}],
      "macro": {"kind": "merge", "content": "contents", "merge_behavior": "shallow"}
    },
    "mock_join": {
      "arguments": [{"name": "contents", "type": "map", "required": true, "repeated": true}],
      "macro": {"kind": "merge", "content": "contents", "merge_behavior": "join"}
    },
    "mock_function": {"arguments": [{"name": "value", "type": "any", "required": true}]}
  },
  "properties": {"merged": {
    "type": "object",
    "additionalProperties": false,
    "required": ["name"],
    "properties": {
      "name": {"type": "string"},
      "tier": {"type": "integer"},
      "nested": {"type": "object", "properties": {"x": {"type": "integer"}, "y": {"type": "integer"}}}
    }
  }}
}`

func mergeCallDocument(testingContext *testing.T, functionName string, argumentKeys ...string) ResolvedValue {
	sourceText := "first: {tier: low, nested: {x: one}}\nsecond: {tier: 5, nested: {y: two}}\ncount: 3\n"
	sourceValue := ResolvedValueFromNode(parsedNode(testingContext, sourceText, nil))
	unknownContent := ResolvedValue{
		Kind:      ResolvedReference,
		Position:  sourceValue.Position,
		Reference: &Reference{Scope: "mock", Name: "mock", Target: ReferenceTarget{Kind: TargetDeclaration}},
	}
	var callArguments []ResolvedValue
	for _, argumentKey := range argumentKeys {
		knownArgument, isKnown := sourceValue.Get(argumentKey)
		if !isKnown {
			knownArgument = unknownContent
		}
		callArguments = append(callArguments, knownArgument)
	}
	callValue := ResolvedValue{
		Kind:     ResolvedCall,
		Position: sourceValue.Position,
		Call:     &FunctionCall{Function: functionName, Arguments: callArguments},
	}
	mergedEntry := MapEntry{Key: "merged", KeyPosition: sourceValue.Position, Value: callValue}
	return ResolvedValue{Kind: ResolvedMap, Position: sourceValue.Position, Entries: []MapEntry{mergedEntry}}
}

func TestAMergeCallIsValidatedAsItsKnownMapsMergedRequiringNoKeyOnlyBesideAnUnknownArgument(
	testingContext *testing.T,
) {
	loadedSchema := loadedMockSchema(testingContext, mockMergeMacrosSchema)
	mergeCases := []struct {
		functionName     string
		argumentKeys     []string
		expectedSubjects []string
		expectedLines    []int
	}{
		{
			"mock_deep",
			[]string{"mock_unknown", "first", "second"},
			[]string{"/merged/nested/x", "/merged/nested/y"},
			[]int{1, 2},
		},
		{
			"mock_shallow",
			[]string{"mock_unknown", "first", "second"},
			[]string{"/merged/nested/y"},
			[]int{2},
		},
		{
			"mock_join",
			[]string{"mock_unknown", "first", "second"},
			[]string{"/merged/tier", "/merged/nested/x"},
			[]int{1, 1},
		},
		{
			"mock_deep",
			[]string{"first", "second"},
			[]string{"/merged", "/merged/nested/x", "/merged/nested/y"},
			[]int{1, 1, 2},
		},
		{
			"mock_deep",
			[]string{"count", "first"},
			[]string{"/merged", "/merged/tier", "/merged/nested/x"},
			[]int{1, 1, 1},
		},
	}
	for _, mergeCase := range mergeCases {
		documentValue := mergeCallDocument(testingContext, mergeCase.functionName, mergeCase.argumentKeys...)
		subjectProblems := loadedSchema.Problems(documentValue, ValidationRules{})
		if !reflect.DeepEqual(problemSubjects(subjectProblems), mergeCase.expectedSubjects) ||
			!reflect.DeepEqual(problemLines(subjectProblems), mergeCase.expectedLines) {
			testingContext.Errorf("%s %v gives %v", mergeCase.functionName, mergeCase.argumentKeys, subjectProblems)
		}
	}
}

func TestACallWithNoKnownMapOrOfNoMergeMacroIsNotChecked(testingContext *testing.T) {
	loadedSchema := loadedMockSchema(testingContext, mockMergeMacrosSchema)
	for _, documentValue := range []ResolvedValue{
		mergeCallDocument(testingContext, "mock_deep", "mock_unknown", "count"),
		mergeCallDocument(testingContext, "mock_function", "first"),
		mergeCallDocument(testingContext, "mock_missing", "first"),
	} {
		if subjectProblems := loadedSchema.Problems(documentValue, ValidationRules{}); len(subjectProblems) != 0 {
			testingContext.Errorf("problems %v", subjectProblems)
		}
	}
}

func TestAnAlternativeBesideAnUnknownValueIsReportedWhenEveryFormFailsAKnownCheck(testingContext *testing.T) {
	schemaText := `{"additionalProperties": {"type": "object", "oneOf": [
	  {"required": ["arn"]},
	  {"required": ["size"], "properties": {"size": {"type": "integer"}}},
	  {"anyOf": [{"required": ["name"]}, {"required": ["region"], "properties": {"region": {"type": "integer"}}}]}
	]}}`
	documentText := "known: {zone: mock}\nhidden: {zone: mock, size: mock}\nnested: {region: mock}\n"
	unknownPointers := []string{"/known/zone", "/hidden/zone", "/hidden/size", "/nested/region"}
	documentValue := withValuesRead(
		ResolvedValueFromNode(parsedNode(testingContext, documentText, nil)),
		"",
		func(valuePointer string, writtenValue ResolvedValue) (ResolvedValue, bool) {
			if slices.Contains(unknownPointers, valuePointer) {
				return unknownReference(writtenValue), true
			}
			return ResolvedValue{}, false
		})
	subjectProblems := loadedMockSchema(testingContext, schemaText).Problems(documentValue, ValidationRules{})
	if !reflect.DeepEqual(problemSubjects(subjectProblems), []string{"/known"}) {
		testingContext.Errorf("problems %v", subjectProblems)
	}
}

func TestADependencyNamesWhatTheKeyNeedsAcrossItsPartsAndReferences(testingContext *testing.T) {
	schemaValue, _ := decodeOrderedJSON(`{
	  "$schema": "https://json-schema.org/draft/2020-12/schema",
	  "$defs": {
	    "shared": {"dependentRequired": {"label": ["owner"]}, "allOf": [{"$ref": "#/$defs/shared"}]},
	    "resource": {
	      "$id": "https://mock.example/mock-resource.json",
	      "$ref": "#/$defs/shared",
	      "$defs": {"shared": {"dependentRequired": {"label": ["inner"]}}}
	    }
	  },
	  "allOf": [{"dependentRequired": {"label": ["size"]}}, {"$ref": "#/$defs/shared"}],
	  "dependentSchemas": {"label": {"not": {"required": ["legacy"]}}},
	  "dependentRequired": {"label": ["kind"]}
	}`)
	expectedMessages := map[string]string{
		"":                 "`label` needs `legacy` must not be set; `kind`; `size`; `owner`",
		"/$defs/resource":  "`label` needs `inner`",
		"/$defs/shared":    "`label` needs `owner`",
		"/$defs/no-schema": "`label` needs a condition the schema states",
	}
	for valueSchemaPointer, expectedMessage := range expectedMessages {
		if labelMessage := dependencyMessage(schemaValue, valueSchemaPointer, "label"); labelMessage != expectedMessage {
			testingContext.Errorf("%q gives %q", valueSchemaPointer, labelMessage)
		}
	}
}

func TestASchemaNoteIsTheDescriptionVerbatim(testingContext *testing.T) {
	writtenText := "  [optional] Kept as written.\n"
	describedNode := map[string]any{"description": writtenText}
	if schemaNote := descriptionText(describedNode); schemaNote == nil || *schemaNote != writtenText {
		testingContext.Errorf("note %v", schemaNote)
	}
	for _, undescribedNode := range []any{map[string]any{"description": " \n\t"}, map[string]any{}} {
		if schemaNote := descriptionText(undescribedNode); schemaNote != nil {
			testingContext.Errorf("note %q", *schemaNote)
		}
	}
}
