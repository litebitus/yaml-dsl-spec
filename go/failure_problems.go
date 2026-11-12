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

// SchemaProblem is a problem of a validated document with its subject, the JSON pointer of the value it is about.
type SchemaProblem struct {
	Subject    string
	Diagnostic Diagnostic
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
	helpText := "did you mean " + CodeSpan(closestCandidate) + "?"
	return &helpText
}

func (problemBuilder failureProblems) problemsOf(checkFailure failedCheck) []SchemaProblem {
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
	atValue := func(problemMessage string, schemaNote *string) []SchemaProblem {
		valueProblem := DiagnosticAt(documentRecord.valuePosition(valuePointer), problemMessage).
			WithLabel("breaks " + CodeSpan(checkFailure.failedKeyword)).
			WithRule(checkRule).
			WithSchema(schemaNote)
		return []SchemaProblem{{Subject: valuePointer, Diagnostic: valueProblem}}
	}
	atSubject := func(subjectPointer string, subjectProblem Diagnostic) []SchemaProblem {
		return []SchemaProblem{{Subject: subjectPointer, Diagnostic: subjectProblem}}
	}
	schemaValue, _ := jsonPointerValue(loadedSchema.markedSchema, checkFailure.schemaPointer)
	switch checkFailure.failedKeyword {
	case "additionalProperties", "unevaluatedProperties":
		allowedNames := allowedFields(loadedSchema.originalSchema, checkFailure.schemaPointer)
		isUnevaluated := checkFailure.failedKeyword == "unevaluatedProperties"
		var unknownProblems []SchemaProblem
		for _, unexpectedName := range checkFailure.fieldNames {
			subjectPointer := valuePointer + "/" + escapePointer(unexpectedName)
			if isUnevaluated && checkFailure.readsUnknownMember(loadedSchema, documentRecord, unexpectedName) {
				continue
			}
			unknownMessage := CodeSpan(unexpectedName) + " is not defined in the schema"
			unknownProblem := DiagnosticAt(documentRecord.keyPosition(subjectPointer), unknownMessage).
				WithRule(checkRule).
				WithHelp(problemBuilder.closestSuggestion(unexpectedName, allowedNames))
			unknownProblems = append(unknownProblems, atSubject(subjectPointer, unknownProblem)...)
		}
		return unknownProblems
	case "propertyNames":
		propertyName := checkFailure.fieldNames[0]
		subjectPointer := valuePointer + "/" + escapePointer(propertyName)
		keyMessage := CodeSpan(propertyName) + " is not a valid key of " + namedField
		keyProblem := DiagnosticAt(documentRecord.keyPosition(subjectPointer), keyMessage).
			WithLabel("breaks `propertyNames`").
			WithRule(checkRule)
		return atSubject(subjectPointer, keyProblem)
	case "enum":
		listedOptions, _ := schemaValue.([]any)
		return problemBuilder.listedValueProblems(checkFailure, listedOptions, holderValue, namedField, checkRule, nil)
	case "type":
		var expectedSpans []string
		for _, expectedType := range loadedSchema.expectedTypes(checkFailure.schemaPointer) {
			expectedSpans = append(expectedSpans, CodeSpan(expectedType))
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
		missingProblem := DiagnosticAt(documentRecord.keyPosition(valuePointer), CodeSpan(missingName)+" is required").
			WithLabel(namedField + " has no field " + CodeSpan(missingName)).
			WithRule(checkRule)
		return atSubject(valuePointer, missingProblem)
	case "anyOf", "oneOf":
		if oneOfKind, isOneOf := checkFailure.errorKind.(*kind.OneOf); isOneOf && len(oneOfKind.Subschemas) > 0 {
			return atValue(namedField+" matches more than one of its allowed forms", nil)
		}
		holderSchemaPointer := checkFailure.schemaPointer[:max(strings.LastIndexByte(checkFailure.schemaPointer, '/'), 0)]
		definitionNote := definitionDescription(loadedSchema.originalSchema, holderSchemaPointer)
		if constantOptions, areConstants := alternativeConstants(schemaValue); areConstants {
			return problemBuilder.listedValueProblems(checkFailure, constantOptions, holderValue, namedField, checkRule, definitionNote)
		}
		return atValue(namedField+" matches none of its allowed forms", definitionNote)
	case "const":
		return atValue(namedField+" must be "+CodeSpan(jsonText(schemaValue)), nil)
	case "multipleOf":
		multipleFloat, _ := jsonNumberFloat(schemaValue)
		return atValue(namedField+" must be multiple of "+CodeSpan(floatNumberText(multipleFloat)), nil)
	case "pattern":
		patternText, _ := schemaValue.(string)
		return atValue(namedField+" must match pattern "+CodeSpan(patternText), nil)
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
		beyondCount := 0
		for _, indexText := range checkFailure.fieldNames {
			if !checkFailure.readsUnknownMember(loadedSchema, documentRecord, indexText) {
				beyondCount++
			}
		}
		if beyondCount == 0 {
			return nil
		}
		return atValue(listLimitMessage(namedField, len(listItems)-beyondCount), nil)
	case "falseSchema":
		return atValue(namedField+" boolean schema is false", nil)
	}
	if boundComparison, isBound := boundComparisons[checkFailure.failedKeyword]; isBound {
		return atValue(namedField+" must be "+boundComparison+" "+CodeSpan(numberText(schemaValue)), nil)
	}
	if limitWords, isLimit := lengthLimits[checkFailure.failedKeyword]; isLimit {
		return atValue(namedField+" must NOT have "+limitWords[0]+" than "+CodeSpan(numberText(schemaValue))+" "+limitWords[1], nil)
	}
	return atValue(namedField+" breaks "+CodeSpan(checkFailure.failedKeyword), nil)
}

func (problemBuilder failureProblems) dependencyProblems(checkFailure failedCheck, holderValue any) ([]SchemaProblem, bool) {
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
	dependencyMessageText := dependencyMessage(markedSchema, checkFailure.valueSchemaPointer, dependencyKey)
	dependencyProblem := DiagnosticAt(problemBuilder.documentRecord.keyPosition(subjectPointer), dependencyMessageText).
		WithLabel("breaks " + CodeSpan(dependencyKeyword)).
		WithRule(problemBuilder.ruleAt(dependencyKeyword, dependencyPointer+"/"+escapePointer(dependencyKey)))
	return []SchemaProblem{{Subject: subjectPointer, Diagnostic: dependencyProblem}}, true
}

func (problemBuilder failureProblems) listedValueProblems(
	checkFailure failedCheck,
	listedOptions []any,
	holderValue any,
	namedField, checkRule string,
	schemaNote *string,
) []SchemaProblem {
	var allowedTexts, shownOptions []string
	for _, listedOption := range listedOptions {
		if optionText, isText := listedOption.(string); isText {
			allowedTexts = append(allowedTexts, optionText)
		}
		shownOptions = append(shownOptions, CodeSpan(jsonText(listedOption)))
	}
	var enumHelp *string
	if instanceText, isText := holderValue.(string); isText {
		enumHelp = problemBuilder.closestSuggestion(instanceText, allowedTexts)
	}
	valuePosition := problemBuilder.documentRecord.valuePosition(checkFailure.instancePointer)
	enumProblem := DiagnosticAt(valuePosition, namedField+" must be one of "+strings.Join(shownOptions, ", ")).
		WithLabel(fmt.Sprintf("not one of the %d values %s allows", len(listedOptions), CodeSpan(checkFailure.failedKeyword))).
		WithRule(checkRule).
		WithSchema(schemaNote).
		WithHelp(enumHelp)
	return []SchemaProblem{{Subject: checkFailure.instancePointer, Diagnostic: enumProblem}}
}

func (problemBuilder failureProblems) tupleProblems(checkFailure failedCheck, namedField, checkRule string) []SchemaProblem {
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
		WithLabel("breaks " + CodeSpan(checkFailure.failedKeyword)).
		WithRule(checkRule)
	return []SchemaProblem{{Subject: listPointer, Diagnostic: tupleProblem}}
}

func listLimitMessage(namedField string, itemLimit int) string {
	return namedField + " must NOT have more than " + CodeSpan(strconv.Itoa(itemLimit)) + " items"
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
