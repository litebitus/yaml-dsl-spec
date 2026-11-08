package yamldslspec

import (
	"slices"
	"strconv"
	"strings"
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
		stepKey := unescapePointer(rawStep)
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
			currentValue, isPresent = objectField(currentValue, stepKey)
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
	return CodeSpan(nameBuilder.String())
}

// CodeSpan is a value fenced in the fewest backticks it does not hold, padded with a space where it begins or ends with one.
func CodeSpan(spanText string) string {
	longestRun, currentRun := 0, 0
	for _, textRune := range spanText {
		if textRune == '`' {
			currentRun++
			longestRun = max(longestRun, currentRun)
		} else {
			currentRun = 0
		}
	}
	fenceText := strings.Repeat("`", longestRun+1)
	if strings.HasPrefix(spanText, "`") || strings.HasSuffix(spanText, "`") {
		return fenceText + " " + spanText + " " + fenceText
	}
	return fenceText + spanText + fenceText
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
	holderNode, isFound := jsonPointerValue(schemaValue, holderPointer)
	if !isFound {
		return nil
	}
	return descriptionText(holderNode)
}

func dependencyOf(schemaValue any, keywordPointer string, holderValue any, missingField *string) (string, string, bool) {
	var pointerSteps []string
	if keywordPointer != "" {
		for _, rawStep := range strings.Split(keywordPointer, "/")[1:] {
			pointerSteps = append(pointerSteps, unescapePointer(rawStep))
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
	if _, holderHasKey := objectField(holderValue, dependencyKey); !holderHasKey {
		return "", "", false
	}
	return dependencyPointer, dependencyKey, true
}

func keyNeedingField(schemaValue any, dependencyPointer string, holderValue any, missingField string) (string, bool) {
	listedDependencies, isFound := jsonPointerValue(schemaValue, dependencyPointer)
	if !isFound {
		return "", false
	}
	for _, listedKey := range objectKeys(listedDependencies) {
		neededValue, _ := objectField(listedDependencies, listedKey)
		neededFields, isList := neededValue.([]any)
		_, holderHasKey := objectField(holderValue, listedKey)
		if holderHasKey && isList && slices.Contains(neededFields, any(missingField)) {
			return listedKey, true
		}
	}
	return "", false
}

func dependencyMessage(schemaValue any, keywordPointer, dependencyKey string) string {
	holderPointer := keywordPointer[:max(strings.LastIndexByte(keywordPointer, '/'), 0)]
	var neededTexts []string
	if holderNode, isFound := jsonPointerValue(schemaValue, holderPointer); isFound {
		for _, dependencyKeyword := range dependencyKeywords {
			keywordValue, _ := objectField(holderNode, dependencyKeyword)
			if neededValue, hasKey := objectField(keywordValue, dependencyKey); hasKey {
				neededTexts = append(neededTexts, neededText(neededValue))
			}
		}
	}
	neededSummary := unstatedCondition
	if len(neededTexts) > 0 {
		neededSummary = strings.Join(neededTexts, "; ")
	}
	return CodeSpan(dependencyKey) + " needs " + neededSummary
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
	if !isJSONObject(schemaNode) {
		return "", false
	}
	var consequenceParts []string
	requiredValue, _ := objectField(schemaNode, "required")
	if requiredFields, isList := requiredValue.([]any); isList && len(requiredFields) > 0 {
		consequenceParts = append(consequenceParts, keyList(requiredFields))
	}
	forbiddenValue, _ := jsonPointerValue(schemaNode, "/not/required")
	if forbiddenFields, isList := forbiddenValue.([]any); isList && len(forbiddenFields) > 0 {
		consequenceParts = append(consequenceParts, keyList(forbiddenFields)+" must not be set")
	}
	propertiesValue, _ := objectField(schemaNode, "properties")
	for _, propertyName := range objectKeys(propertiesValue) {
		propertySchema, _ := objectField(propertiesValue, propertyName)
		referenceValue, _ := objectField(propertySchema, "$ref")
		if referenceText, isText := referenceValue.(string); isText {
			consequenceParts = append(consequenceParts, CodeSpan(propertyName)+" is a "+CodeSpan(referenceName(referenceText)))
		}
	}
	allOfValue, _ := objectField(schemaNode, "allOf")
	allOfParts, _ := allOfValue.([]any)
	for _, allOfPart := range allOfParts {
		if partText, isStated := consequenceText(allOfPart); isStated {
			consequenceParts = append(consequenceParts, partText)
		}
	}
	var alternativeTexts []string
	for _, alternativeKeyword := range []string{"anyOf", "oneOf"} {
		alternativesValue, _ := objectField(schemaNode, alternativeKeyword)
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
		referenceValue, _ := objectField(schemaNode, "$ref")
		if referenceText, isText := referenceValue.(string); isText {
			consequenceParts = append(consequenceParts, "a "+CodeSpan(referenceName(referenceText)))
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
			keyText = jsonText(keyValue)
		}
		keySpans = append(keySpans, CodeSpan(keyText))
	}
	if len(keySpans) < 2 {
		return strings.Join(keySpans, "")
	}
	return strings.Join(keySpans[:len(keySpans)-1], ", ") + " and " + keySpans[len(keySpans)-1]
}

func duplicateItems(listItems []any) (int, int, bool) {
	for earlierIndex := len(listItems) - 1; earlierIndex >= 0; earlierIndex-- {
		for laterIndex := earlierIndex + 1; laterIndex < len(listItems); laterIndex++ {
			if jsonEqual(listItems[earlierIndex], listItems[laterIndex]) {
				return laterIndex, earlierIndex, true
			}
		}
	}
	return 0, 0, false
}

func containsMessage(holderNode any) string {
	boundText := func(boundKeyword string) (string, bool) {
		boundValue, isBounded := objectField(holderNode, boundKeyword)
		if !isBounded {
			return "", false
		}
		return CodeSpan(numberText(boundValue)), true
	}
	minimumText, hasMinimum := boundText("minContains")
	if !hasMinimum {
		minimumText = CodeSpan("1")
	}
	if maximumText, hasMaximum := boundText("maxContains"); hasMaximum {
		return "must contain at least " + minimumText + " and no more than " + maximumText + " valid item(s)"
	}
	return "must contain at least " + minimumText + " valid item(s)"
}
