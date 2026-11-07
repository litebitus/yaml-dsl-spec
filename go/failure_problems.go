package yamldslspec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

var (
	boundComparisons = map[string]string{"minimum": ">=", "maximum": "<=", "exclusiveMinimum": ">", "exclusiveMaximum": "<"}
	lengthLimits     = map[string][2]string{
		"minLength":     {"fewer", "characters"},
		"maxLength":     {"more", "characters"},
		"minItems":      {"fewer", "items"},
		"maxItems":      {"more", "items"},
		"minProperties": {"fewer", "properties"},
		"maxProperties": {"more", "properties"},
	}
)

type reportedSubjects struct {
	dependencyKeys map[string]bool
	itemLists      map[string]bool
}

type schemaProblem struct {
	subjectPointer  string
	builtDiagnostic Diagnostic
}

type failureProblems struct {
	loadedSchema   *LoadedSchema
	documentJSON   any
	documentRecord *instanceRecord
	reportedCovers *reportedSubjects
}

func (problemBuilder failureProblems) ruleAt(ruleKeyword, schemaPointer string) string {
	return ruleKeyword + ", at " + problemBuilder.loadedSchema.schemaName + "#" + schemaPointer
}

func (problemBuilder failureProblems) closestSuggestion(misspeltName string, candidateNames []string) *string {
	closestCandidate, isClose := ClosestName(misspeltName, candidateNames)
	if !isClose {
		return nil
	}
	helpText := "did you mean " + codeSpan(closestCandidate) + "?"
	return &helpText
}

func (problemBuilder failureProblems) problemsOf(checkFailure failedCheck) []schemaProblem {
	loadedSchema, documentRecord := problemBuilder.loadedSchema, problemBuilder.documentRecord
	valuePointer := checkFailure.instancePointer
	checkRule := problemBuilder.ruleAt(checkFailure.failedKeyword, checkFailure.schemaPointer)
	namedField := fieldName(problemBuilder.documentJSON, valuePointer)
	holderValue, _ := jsonPointerValue(problemBuilder.documentJSON, valuePointer)
	if checkFailure.failedKeyword != "propertyNames" {
		if dependencyProblems, isDependency := problemBuilder.dependencyProblems(checkFailure, holderValue); isDependency {
			return dependencyProblems
		}
	}
	atValue := func(problemMessage string, schemaNote *string) []schemaProblem {
		valueProblem := DiagnosticAt(documentRecord.valuePosition(valuePointer), problemMessage).
			WithLabel("breaks " + codeSpan(checkFailure.failedKeyword)).
			WithRule(checkRule).
			WithSchema(schemaNote)
		return []schemaProblem{{subjectPointer: valuePointer, builtDiagnostic: valueProblem}}
	}
	atSubject := func(subjectPointer string, subjectProblem Diagnostic) []schemaProblem {
		return []schemaProblem{{subjectPointer: subjectPointer, builtDiagnostic: subjectProblem}}
	}
	schemaValue, _ := jsonPointerValue(loadedSchema.markedSchema, checkFailure.schemaPointer)
	switch checkFailure.failedKeyword {
	case "additionalProperties", "unevaluatedProperties":
		allowedNames := allowedFields(loadedSchema.originalSchema, checkFailure.schemaPointer)
		var unknownProblems []schemaProblem
		for _, unexpectedName := range checkFailure.fieldNames {
			subjectPointer := valuePointer + "/" + escapePointer(unexpectedName)
			unknownMessage := codeSpan(unexpectedName) + " is not a field of " + namedField
			unknownProblem := DiagnosticAt(documentRecord.keyPosition(subjectPointer), unknownMessage).
				WithLabel(namedField + " has no field " + codeSpan(unexpectedName)).
				WithRule(checkRule).
				WithHelp(problemBuilder.closestSuggestion(unexpectedName, allowedNames))
			unknownProblems = append(unknownProblems, atSubject(subjectPointer, unknownProblem)...)
		}
		return unknownProblems
	case "propertyNames":
		propertyName := checkFailure.fieldNames[0]
		subjectPointer := valuePointer + "/" + escapePointer(propertyName)
		keyMessage := codeSpan(propertyName) + " is not a valid key of " + namedField
		keyProblem := DiagnosticAt(documentRecord.keyPosition(subjectPointer), keyMessage).
			WithLabel("breaks `propertyNames`").
			WithRule(checkRule)
		return atSubject(subjectPointer, keyProblem)
	case "enum":
		return problemBuilder.enumProblems(checkFailure, schemaValue, holderValue, namedField, checkRule)
	case "type":
		var expectedSpans []string
		for _, expectedType := range loadedSchema.expectedTypes(checkFailure.schemaPointer) {
			expectedSpans = append(expectedSpans, codeSpan(expectedType))
		}
		typeMessage := namedField + " must be " + strings.Join(expectedSpans, " or ")
		typeProblem := DiagnosticAt(documentRecord.valuePosition(valuePointer), typeMessage).
			WithLabel("is " + withArticle(jsonTypeName(holderValue))).
			WithRule(checkRule)
		return atSubject(valuePointer, typeProblem)
	case "required":
		missingName := ""
		if checkFailure.missingField != nil {
			missingName = *checkFailure.missingField
		}
		missingProblem := DiagnosticAt(documentRecord.keyPosition(valuePointer), codeSpan(missingName)+" is required").
			WithLabel(namedField + " has no field " + codeSpan(missingName)).
			WithRule(checkRule)
		return atSubject(valuePointer, missingProblem)
	case "anyOf", "oneOf":
		if oneOfKind, isOneOf := checkFailure.errorKind.(*kind.OneOf); isOneOf && len(oneOfKind.Subschemas) > 0 {
			return atValue(namedField+" matches more than one of its allowed forms", nil)
		}
		holderSchemaPointer := checkFailure.schemaPointer[:max(strings.LastIndexByte(checkFailure.schemaPointer, '/'), 0)]
		definitionNote := definitionDescription(loadedSchema.originalSchema, holderSchemaPointer)
		return atValue(namedField+" matches none of its allowed forms", definitionNote)
	case "const":
		return atValue(namedField+" must be "+codeSpan(jsonText(schemaValue)), nil)
	case "multipleOf":
		multipleFloat, _ := jsonNumberFloat(schemaValue)
		return atValue(namedField+" must be multiple of "+codeSpan(floatNumberText(multipleFloat)), nil)
	case "pattern":
		patternText, _ := schemaValue.(string)
		return atValue(namedField+" must match pattern "+codeSpan(patternText), nil)
	case "uniqueItems":
		listItems, _ := holderValue.([]any)
		laterIndex, earlierIndex, _ := duplicateItems(listItems)
		duplicateMessage := fmt.Sprintf(
			"%s must NOT have duplicate items (items ## %d and %d are identical)",
			namedField, laterIndex, earlierIndex)
		return atValue(duplicateMessage, nil)
	case "contains":
		holderSchema, _ := jsonPointerValue(loadedSchema.markedSchema, strings.TrimSuffix(checkFailure.schemaPointer, "/contains"))
		return atValue(namedField+" "+containsMessage(holderSchema), nil)
	case "not":
		notSchema, _ := jsonPointerValue(loadedSchema.originalSchema, checkFailure.schemaPointer)
		return atValue(namedField+" matches a form that is not allowed", descriptionText(notSchema))
	case "items", "additionalItems":
		return problemBuilder.tupleProblems(checkFailure, namedField, checkRule)
	case "unevaluatedItems":
		listItems, _ := holderValue.([]any)
		return atValue(listLimitMessage(namedField, max(len(listItems)-checkFailure.itemCount, 0)), nil)
	case "falseSchema":
		return atValue(namedField+" boolean schema is false", nil)
	}
	if boundComparison, isBound := boundComparisons[checkFailure.failedKeyword]; isBound {
		return atValue(namedField+" must be "+boundComparison+" "+codeSpan(numberText(schemaValue)), nil)
	}
	if limitWords, isLimit := lengthLimits[checkFailure.failedKeyword]; isLimit {
		return atValue(namedField+" must NOT have "+limitWords[0]+" than "+codeSpan(numberText(schemaValue))+" "+limitWords[1], nil)
	}
	return atValue(namedField+" breaks "+codeSpan(checkFailure.failedKeyword), nil)
}

func (problemBuilder failureProblems) dependencyProblems(checkFailure failedCheck, holderValue any) ([]schemaProblem, bool) {
	markedSchema := problemBuilder.loadedSchema.markedSchema
	dependencyPointer, dependencyKey, isDependency := dependencyOf(
		markedSchema, checkFailure.schemaPointer, holderValue, checkFailure.missingField)
	if !isDependency {
		return nil, false
	}
	subjectPointer := checkFailure.instancePointer + "/" + escapePointer(dependencyKey)
	if problemBuilder.reportedCovers.dependencyKeys[subjectPointer] {
		return nil, true
	}
	problemBuilder.reportedCovers.dependencyKeys[subjectPointer] = true
	dependencyKeyword := dependencyPointer[strings.LastIndexByte(dependencyPointer, '/')+1:]
	dependencyMessageText := dependencyMessage(markedSchema, dependencyPointer, dependencyKey)
	dependencyProblem := DiagnosticAt(problemBuilder.documentRecord.keyPosition(subjectPointer), dependencyMessageText).
		WithLabel("breaks " + codeSpan(dependencyKeyword)).
		WithRule(problemBuilder.ruleAt(dependencyKeyword, dependencyPointer+"/"+escapePointer(dependencyKey)))
	return []schemaProblem{{subjectPointer: subjectPointer, builtDiagnostic: dependencyProblem}}, true
}

func (problemBuilder failureProblems) enumProblems(
	checkFailure failedCheck,
	schemaValue, holderValue any,
	namedField, checkRule string,
) []schemaProblem {
	listedOptions, _ := schemaValue.([]any)
	var allowedTexts, shownOptions []string
	for _, listedOption := range listedOptions {
		if optionText, isText := listedOption.(string); isText {
			allowedTexts = append(allowedTexts, optionText)
		}
		shownOptions = append(shownOptions, codeSpan(jsonText(listedOption)))
	}
	var enumHelp *string
	if instanceText, isText := holderValue.(string); isText {
		enumHelp = problemBuilder.closestSuggestion(instanceText, allowedTexts)
	}
	valuePosition := problemBuilder.documentRecord.valuePosition(checkFailure.instancePointer)
	enumProblem := DiagnosticAt(valuePosition, namedField+" must be one of "+strings.Join(shownOptions, ", ")).
		WithLabel(fmt.Sprintf("not one of the %d values `enum` allows", len(listedOptions))).
		WithRule(checkRule).
		WithHelp(enumHelp)
	return []schemaProblem{{subjectPointer: checkFailure.instancePointer, builtDiagnostic: enumProblem}}
}

func (problemBuilder failureProblems) tupleProblems(checkFailure failedCheck, namedField, checkRule string) []schemaProblem {
	listPointer := checkFailure.instancePointer
	if problemBuilder.reportedCovers.itemLists[listPointer] {
		return nil
	}
	problemBuilder.reportedCovers.itemLists[listPointer] = true
	holderSchemaPointer := checkFailure.schemaPointer[:max(strings.LastIndexByte(checkFailure.schemaPointer, '/'), 0)]
	tupleKeyword := "items"
	if checkFailure.failedKeyword == "items" {
		tupleKeyword = "prefixItems"
	}
	tupleValue, _ := jsonPointerValue(problemBuilder.loadedSchema.markedSchema, holderSchemaPointer+"/"+tupleKeyword)
	tupleSchemas, _ := tupleValue.([]any)
	listPosition := problemBuilder.documentRecord.valuePosition(listPointer)
	tupleProblem := DiagnosticAt(listPosition, listLimitMessage(namedField, len(tupleSchemas))).
		WithLabel("breaks " + codeSpan(checkFailure.failedKeyword)).
		WithRule(checkRule)
	return []schemaProblem{{subjectPointer: listPointer, builtDiagnostic: tupleProblem}}
}

func listLimitMessage(namedField string, itemLimit int) string {
	return namedField + " must NOT have more than " + codeSpan(strconv.Itoa(itemLimit)) + " items"
}

func jsonTypeName(jsonValue any) string {
	switch jsonValue.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case int64, uint64:
		return "integer"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	}
	return "object"
}

func withArticle(typeNoun string) string {
	if typeNoun != "" && strings.ContainsRune("aeiou", rune(typeNoun[0])) {
		return "an " + typeNoun
	}
	return "a " + typeNoun
}
