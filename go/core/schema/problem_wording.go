package schema

import (
	"slices"
	"strconv"
	"strings"

	"github.com/litebitus/yaml-dsl-spec/go/core"
)

const unstatedCondition = "a condition the schema states"

var (
	dependencyKeywords = []string{"dependencies", "dependentRequired", "dependentSchemas"}
	schemaMapKeywords  = []string{"$defs", "definitions", "dependencies", "dependentSchemas", "patternProperties", "properties"}
	definitionKeywords = []string{"$defs", "definitions"}
)

func fieldName(documentValue any, valuePointer string) string {
	if valuePointer == "" {
		return "the document"
	}
	type namedStep struct {
		stepKey  string
		isInList bool
	}
	var namedSteps []namedStep
	currentValue, isPresent := documentValue, true
	for _, rawStep := range strings.Split(valuePointer[1:], "/") {
		stepKey := core.UnescapePointer(rawStep)
		listItems, isList := currentValue.([]any)
		isInList := isPresent && isList
		switch {
		case !isPresent:
		case isInList:
			itemIndex, indexError := strconv.Atoi(stepKey)
			isPresent = indexError == nil && itemIndex >= 0 && itemIndex < len(listItems)
			if isPresent {
				currentValue = listItems[itemIndex]
			}
		default:
			currentValue, isPresent = core.ObjectField(currentValue, stepKey)
		}
		namedSteps = append(namedSteps, namedStep{stepKey: stepKey, isInList: isInList})
	}
	keyAt := -1
	for stepIndex := len(namedSteps) - 1; stepIndex >= 0; stepIndex-- {
		if !namedSteps[stepIndex].isInList {
			keyAt = stepIndex
			break
		}
	}
	var nameBuilder strings.Builder
	if keyAt >= 0 {
		nameBuilder.WriteString(namedSteps[keyAt].stepKey)
	}
	for _, indexStep := range namedSteps[keyAt+1:] {
		nameBuilder.WriteByte('[')
		nameBuilder.WriteString(indexStep.stepKey)
		nameBuilder.WriteByte(']')
	}
	return core.CodeSpan(nameBuilder.String())
}

func definitionDescription(schemaValue any, holderPointer string) *string {
	holderSteps, isPointer := strings.CutPrefix(holderPointer, "/")
	if !isPointer {
		return nil
	}
	pointerSteps := strings.Split(holderSteps, "/")
	if len(pointerSteps) != 2 || !slices.Contains(definitionKeywords, pointerSteps[0]) {
		return nil
	}
	holderNode, isFound := core.JSONPointerValue(schemaValue, holderPointer)
	if !isFound {
		return nil
	}
	return descriptionText(holderNode)
}

func alternativeConstants(alternativesValue any) ([]any, bool) {
	alternativeSchemas, _ := alternativesValue.([]any)
	var constantOptions []any
	for _, alternativeSchema := range alternativeSchemas {
		constantOption, statesConstant := core.ObjectField(alternativeSchema, "const")
		if !statesConstant {
			return nil, false
		}
		constantOptions = append(constantOptions, constantOption)
	}
	return constantOptions, len(constantOptions) > 0
}

func dependencyOf(schemaValue any, keywordPointer string, holderValue any, missingField *string) (string, string, bool) {
	var pointerSteps []string
	if keywordPointer != "" {
		for _, rawStep := range strings.Split(keywordPointer, "/")[1:] {
			pointerSteps = append(pointerSteps, core.UnescapePointer(rawStep))
		}
	}
	keywordAt := -1
	for stepIndex := len(pointerSteps) - 1; stepIndex >= 0; stepIndex-- {
		if slices.Contains(dependencyKeywords, pointerSteps[stepIndex]) &&
			(stepIndex == 0 || !slices.Contains(schemaMapKeywords, pointerSteps[stepIndex-1])) {
			keywordAt = stepIndex
			break
		}
	}
	if keywordAt < 0 {
		return "", "", false
	}
	dependencyPointer := pointerOfLocation(pointerSteps[:keywordAt+1])
	dependencyKey, isKnown := "", keywordAt+1 < len(pointerSteps)
	if isKnown {
		dependencyKey = pointerSteps[keywordAt+1]
	} else if missingField != nil {
		dependencyKey, isKnown = keyNeedingField(schemaValue, dependencyPointer, holderValue, *missingField)
	}
	if !isKnown {
		return "", "", false
	}
	if _, holderHasKey := core.ObjectField(holderValue, dependencyKey); !holderHasKey {
		return "", "", false
	}
	return dependencyPointer, dependencyKey, true
}

func keyNeedingField(schemaValue any, dependencyPointer string, holderValue any, missingField string) (string, bool) {
	listedDependencies, isFound := core.JSONPointerValue(schemaValue, dependencyPointer)
	if !isFound {
		return "", false
	}
	for _, listedKey := range core.ObjectKeys(listedDependencies) {
		neededValue, _ := core.ObjectField(listedDependencies, listedKey)
		neededFields, isList := neededValue.([]any)
		_, holderHasKey := core.ObjectField(holderValue, listedKey)
		if holderHasKey && isList && slices.Contains(neededFields, any(missingField)) {
			return listedKey, true
		}
	}
	return "", false
}

func dependencyMessage(schemaValue any, valueSchemaPointer, dependencyKey string) string {
	neededTexts := gatheredNeeds(schemaValue, valueSchemaPointer, dependencyKey, map[string]bool{})
	neededSummary := unstatedCondition
	if len(neededTexts) > 0 {
		neededSummary = strings.Join(neededTexts, "; ")
	}
	return core.CodeSpan(dependencyKey) + " needs " + neededSummary
}

func gatheredNeeds(schemaValue any, nodePointer, dependencyKey string, visitedPointers map[string]bool) []string {
	schemaNode, isFound := core.JSONPointerValue(schemaValue, nodePointer)
	if !isFound || visitedPointers[nodePointer] {
		return nil
	}
	visitedPointers[nodePointer] = true
	var neededTexts []string
	for _, schemaKeyword := range core.ObjectKeys(schemaNode) {
		keywordValue, _ := core.ObjectField(schemaNode, schemaKeyword)
		if neededValue, hasKey := core.ObjectField(keywordValue, dependencyKey); hasKey &&
			slices.Contains(dependencyKeywords, schemaKeyword) {
			neededTexts = append(neededTexts, neededText(neededValue))
		}
	}
	for _, schemaKeyword := range core.ObjectKeys(schemaNode) {
		keywordValue, _ := core.ObjectField(schemaNode, schemaKeyword)
		switch referenceText, isText := keywordValue.(string); {
		case schemaKeyword == "$ref" && isText:
			targetPointer, isPointerReference := referenceTargetPointer(schemaValue, nodePointer, referenceText)
			if isPointerReference {
				neededTexts = append(neededTexts, gatheredNeeds(schemaValue, targetPointer, dependencyKey, visitedPointers)...)
			}
		case schemaKeyword == "allOf":
			allOfParts, _ := keywordValue.([]any)
			for partIndex := range allOfParts {
				partPointer := nodePointer + "/allOf/" + strconv.Itoa(partIndex)
				neededTexts = append(neededTexts, gatheredNeeds(schemaValue, partPointer, dependencyKey, visitedPointers)...)
			}
		}
	}
	return neededTexts
}

func neededText(neededValue any) string {
	if neededFields, isList := neededValue.([]any); isList {
		return keyList(neededFields)
	}
	if consequenceSummary, isStated := consequenceText(neededValue); isStated {
		return consequenceSummary
	}
	return unstatedCondition
}

func consequenceText(schemaNode any) (string, bool) {
	if !core.IsJSONObject(schemaNode) {
		return "", false
	}
	var consequenceParts []string
	requiredValue, _ := core.ObjectField(schemaNode, "required")
	if requiredFields, isList := requiredValue.([]any); isList && len(requiredFields) > 0 {
		consequenceParts = append(consequenceParts, keyList(requiredFields))
	}
	forbiddenValue, _ := core.JSONPointerValue(schemaNode, "/not/required")
	if forbiddenFields, isList := forbiddenValue.([]any); isList && len(forbiddenFields) > 0 {
		consequenceParts = append(consequenceParts, keyList(forbiddenFields)+" must not be set")
	}
	propertiesValue, _ := core.ObjectField(schemaNode, "properties")
	for _, propertyName := range core.ObjectKeys(propertiesValue) {
		propertySchema, _ := core.ObjectField(propertiesValue, propertyName)
		referenceValue, _ := core.ObjectField(propertySchema, "$ref")
		if referenceText, isText := referenceValue.(string); isText {
			consequenceParts = append(consequenceParts,
				core.CodeSpan(propertyName)+" is a "+core.CodeSpan(referenceName(referenceText)))
		}
	}
	allOfValue, _ := core.ObjectField(schemaNode, "allOf")
	allOfParts, _ := allOfValue.([]any)
	for _, allOfPart := range allOfParts {
		if partText, isStated := consequenceText(allOfPart); isStated {
			consequenceParts = append(consequenceParts, partText)
		}
	}
	var alternativeTexts []string
	for _, alternativeKeyword := range []string{"anyOf", "oneOf"} {
		alternativesValue, _ := core.ObjectField(schemaNode, alternativeKeyword)
		alternativeSchemas, _ := alternativesValue.([]any)
		for _, alternativeSchema := range alternativeSchemas {
			alternativeText, isStated := consequenceText(alternativeSchema)
			if !isStated {
				alternativeText = unstatedCondition
			}
			alternativeTexts = append(alternativeTexts, alternativeText)
		}
	}
	if len(alternativeTexts) > 0 {
		consequenceParts = append(consequenceParts, strings.Join(alternativeTexts, ", or "))
	}
	if len(consequenceParts) == 0 {
		referenceValue, _ := core.ObjectField(schemaNode, "$ref")
		if referenceText, isText := referenceValue.(string); isText {
			consequenceParts = append(consequenceParts, "a "+core.CodeSpan(referenceName(referenceText)))
		}
	}
	if len(consequenceParts) == 0 {
		return "", false
	}
	return strings.Join(consequenceParts, "; "), true
}

func referenceName(referenceText string) string {
	lastStep := referenceText[strings.LastIndexByte(referenceText, '/')+1:]
	return strings.TrimPrefix(lastStep, "#")
}

func keyList(keyValues []any) string {
	keySpans := make([]string, 0, len(keyValues))
	for _, keyValue := range keyValues {
		keyText, isText := keyValue.(string)
		if !isText {
			keyText = core.JSONText(keyValue)
		}
		keySpans = append(keySpans, core.CodeSpan(keyText))
	}
	if len(keySpans) < 2 {
		return strings.Join(keySpans, "")
	}
	return strings.Join(keySpans[:len(keySpans)-1], ", ") + " and " + keySpans[len(keySpans)-1]
}

func duplicateItems(listItems []any) (int, int, bool) {
	for earlierIndex := len(listItems) - 1; earlierIndex >= 0; earlierIndex-- {
		for laterIndex := earlierIndex + 1; laterIndex < len(listItems); laterIndex++ {
			if core.JSONEqual(listItems[earlierIndex], listItems[laterIndex]) {
				return laterIndex, earlierIndex, true
			}
		}
	}
	return 0, 0, false
}

func containsMessage(holderNode any) string {
	boundText := func(boundKeyword string) (string, bool) {
		boundValue, isBounded := core.ObjectField(holderNode, boundKeyword)
		if !isBounded {
			return "", false
		}
		return core.CodeSpan(core.NumberText(boundValue)), true
	}
	minimumText, hasMinimum := boundText("minContains")
	if !hasMinimum {
		minimumText = core.CodeSpan("1")
	}
	if maximumText, hasMaximum := boundText("maxContains"); hasMaximum {
		return "must contain at least " + minimumText + " and no more than " + maximumText + " valid item(s)"
	}
	return "must contain at least " + minimumText + " valid item(s)"
}
