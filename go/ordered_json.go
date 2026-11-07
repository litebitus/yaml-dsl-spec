package yamldslspec

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

type orderedObject struct {
	keyOrder    []string
	fieldValues map[string]any
}

func decodeOrderedJSON(sourceText string) (any, error) {
	jsonDecoder := json.NewDecoder(strings.NewReader(sourceText))
	jsonDecoder.UseNumber()
	decodedValue, decodeError := decodeOrderedValue(jsonDecoder)
	if decodeError != nil {
		return nil, decodeError
	}
	if _, trailingError := jsonDecoder.Token(); trailingError == nil {
		return nil, fmt.Errorf("trailing characters after the JSON value")
	}
	return decodedValue, nil
}

func decodeOrderedValue(jsonDecoder *json.Decoder) (any, error) {
	jsonToken, tokenError := jsonDecoder.Token()
	if tokenError != nil {
		return nil, tokenError
	}
	jsonDelimiter, isDelimiter := jsonToken.(json.Delim)
	if !isDelimiter {
		return jsonToken, nil
	}
	if jsonDelimiter == '[' {
		listItems := []any{}
		for jsonDecoder.More() {
			listItem, itemError := decodeOrderedValue(jsonDecoder)
			if itemError != nil {
				return nil, itemError
			}
			listItems = append(listItems, listItem)
		}
		_, closeError := jsonDecoder.Token()
		return listItems, closeError
	}
	decodedObject := &orderedObject{fieldValues: map[string]any{}}
	for jsonDecoder.More() {
		keyToken, keyError := jsonDecoder.Token()
		if keyError != nil {
			return nil, keyError
		}
		fieldKey, _ := keyToken.(string)
		fieldValue, valueError := decodeOrderedValue(jsonDecoder)
		if valueError != nil {
			return nil, valueError
		}
		if _, isRepeated := decodedObject.fieldValues[fieldKey]; !isRepeated {
			decodedObject.keyOrder = append(decodedObject.keyOrder, fieldKey)
		}
		decodedObject.fieldValues[fieldKey] = fieldValue
	}
	_, closeError := jsonDecoder.Token()
	return decodedObject, closeError
}

func isJSONObject(jsonValue any) bool {
	switch jsonValue.(type) {
	case *orderedObject, map[string]any:
		return true
	}
	return false
}

func objectField(jsonValue any, fieldKey string) (any, bool) {
	switch typedObject := jsonValue.(type) {
	case *orderedObject:
		fieldValue, isPresent := typedObject.fieldValues[fieldKey]
		return fieldValue, isPresent
	case map[string]any:
		fieldValue, isPresent := typedObject[fieldKey]
		return fieldValue, isPresent
	}
	return nil, false
}

func objectKeys(jsonValue any) []string {
	switch typedObject := jsonValue.(type) {
	case *orderedObject:
		return typedObject.keyOrder
	case map[string]any:
		sortedKeys := make([]string, 0, len(typedObject))
		for fieldKey := range typedObject {
			sortedKeys = append(sortedKeys, fieldKey)
		}
		slices.Sort(sortedKeys)
		return sortedKeys
	}
	return nil
}

func jsonPointerValue(documentValue any, jsonPointer string) (any, bool) {
	if jsonPointer == "" {
		return documentValue, true
	}
	if !strings.HasPrefix(jsonPointer, "/") {
		return nil, false
	}
	currentValue := documentValue
	for _, rawStep := range strings.Split(jsonPointer[1:], "/") {
		stepKey := unescapePointer(rawStep)
		if listItems, isList := currentValue.([]any); isList {
			itemIndex, indexError := strconv.Atoi(stepKey)
			if indexError != nil || itemIndex < 0 || itemIndex >= len(listItems) || (len(stepKey) > 1 && stepKey[0] == '0') {
				return nil, false
			}
			currentValue = listItems[itemIndex]
			continue
		}
		fieldValue, isPresent := objectField(currentValue, stepKey)
		if !isPresent {
			return nil, false
		}
		currentValue = fieldValue
	}
	return currentValue, true
}

func plainJSON(jsonValue any) any {
	switch typedValue := jsonValue.(type) {
	case *orderedObject:
		plainFields := make(map[string]any, len(typedValue.keyOrder))
		for _, fieldKey := range typedValue.keyOrder {
			plainFields[fieldKey] = plainJSON(typedValue.fieldValues[fieldKey])
		}
		return plainFields
	case []any:
		plainItems := make([]any, len(typedValue))
		for itemIndex, listItem := range typedValue {
			plainItems[itemIndex] = plainJSON(listItem)
		}
		return plainItems
	}
	return jsonValue
}

func deepCopyJSON(jsonValue any) any {
	switch typedValue := jsonValue.(type) {
	case map[string]any:
		copiedFields := make(map[string]any, len(typedValue))
		for fieldKey, fieldValue := range typedValue {
			copiedFields[fieldKey] = deepCopyJSON(fieldValue)
		}
		return copiedFields
	case []any:
		copiedItems := make([]any, len(typedValue))
		for itemIndex, listItem := range typedValue {
			copiedItems[itemIndex] = deepCopyJSON(listItem)
		}
		return copiedItems
	}
	return jsonValue
}

func escapePointer(fieldKey string) string {
	return strings.ReplaceAll(strings.ReplaceAll(fieldKey, "~", "~0"), "/", "~1")
}

func unescapePointer(rawStep string) string {
	return strings.ReplaceAll(strings.ReplaceAll(rawStep, "~1", "/"), "~0", "~")
}

func jsonText(jsonValue any) string {
	var textBuilder strings.Builder
	writeJSONText(&textBuilder, jsonValue)
	return textBuilder.String()
}

func writeJSONText(textBuilder *strings.Builder, jsonValue any) {
	switch typedValue := jsonValue.(type) {
	case nil:
		textBuilder.WriteString("null")
	case bool:
		textBuilder.WriteString(strconv.FormatBool(typedValue))
	case string:
		textBuilder.WriteString(jsonString(typedValue))
	case json.Number:
		textBuilder.WriteString(numberJSONText(typedValue.String()))
	case int64:
		textBuilder.WriteString(strconv.FormatInt(typedValue, 10))
	case uint64:
		textBuilder.WriteString(strconv.FormatUint(typedValue, 10))
	case float64:
		textBuilder.WriteString(serdeFloatText(typedValue))
	case []any:
		textBuilder.WriteByte('[')
		for itemIndex, listItem := range typedValue {
			if itemIndex > 0 {
				textBuilder.WriteByte(',')
			}
			writeJSONText(textBuilder, listItem)
		}
		textBuilder.WriteByte(']')
	default:
		textBuilder.WriteByte('{')
		for fieldIndex, fieldKey := range objectKeys(jsonValue) {
			if fieldIndex > 0 {
				textBuilder.WriteByte(',')
			}
			textBuilder.WriteString(jsonString(fieldKey))
			textBuilder.WriteByte(':')
			fieldValue, _ := objectField(jsonValue, fieldKey)
			writeJSONText(textBuilder, fieldValue)
		}
		textBuilder.WriteByte('}')
	}
}

func jsonString(stringText string) string {
	var textBuilder strings.Builder
	textBuilder.WriteByte('"')
	for _, textRune := range stringText {
		switch textRune {
		case '"':
			textBuilder.WriteString(`\"`)
		case '\\':
			textBuilder.WriteString(`\\`)
		case '\n':
			textBuilder.WriteString(`\n`)
		case '\r':
			textBuilder.WriteString(`\r`)
		case '\t':
			textBuilder.WriteString(`\t`)
		case '\b':
			textBuilder.WriteString(`\b`)
		case '\f':
			textBuilder.WriteString(`\f`)
		default:
			if textRune < 0x20 {
				fmt.Fprintf(&textBuilder, `\u%04x`, textRune)
			} else {
				textBuilder.WriteRune(textRune)
			}
		}
	}
	textBuilder.WriteByte('"')
	return textBuilder.String()
}

func numberJSONText(numberText string) string {
	switch readNumber := NumberJSON(numberText).(type) {
	case int64:
		return strconv.FormatInt(readNumber, 10)
	case uint64:
		return strconv.FormatUint(readNumber, 10)
	case float64:
		return serdeFloatText(readNumber)
	}
	return numberText
}

func serdeFloatText(floatValue float64) string {
	if math.IsInf(floatValue, 0) || math.IsNaN(floatValue) {
		return "null"
	}
	scientificText := strconv.FormatFloat(floatValue, 'e', -1, 64)
	mantissaText, exponentText, _ := strings.Cut(scientificText, "e")
	exponentValue, _ := strconv.Atoi(exponentText)
	if floatValue != 0 && (exponentValue < -5 || exponentValue >= 16) {
		exponentSign := "+"
		if exponentValue < 0 {
			exponentSign, exponentValue = "-", -exponentValue
		}
		return mantissaText + "e" + exponentSign + strconv.Itoa(exponentValue)
	}
	decimalText := strconv.FormatFloat(floatValue, 'f', -1, 64)
	if !strings.Contains(decimalText, ".") {
		decimalText += ".0"
	}
	return decimalText
}

func numberText(jsonValue any) string {
	jsonNumber, isNumber := jsonValue.(json.Number)
	if !isNumber {
		return jsonText(jsonValue)
	}
	if floatValue, isFloat := NumberJSON(jsonNumber.String()).(float64); isFloat {
		return floatNumberText(floatValue)
	}
	return jsonText(jsonValue)
}

func floatNumberText(floatValue float64) string {
	if floatValue == math.Trunc(floatValue) && math.Abs(floatValue) < 1e21 {
		return strconv.FormatFloat(floatValue, 'f', 0, 64)
	}
	return serdeFloatText(floatValue)
}

func jsonNumberFloat(jsonValue any) (float64, bool) {
	switch typedValue := jsonValue.(type) {
	case json.Number:
		floatValue, parseError := strconv.ParseFloat(typedValue.String(), 64)
		return floatValue, parseError == nil
	case int64:
		return float64(typedValue), true
	case uint64:
		return float64(typedValue), true
	case float64:
		return typedValue, true
	}
	return 0, false
}

func jsonEqual(firstValue, secondValue any) bool {
	firstNumber, firstIsNumber := jsonNumberFloat(firstValue)
	secondNumber, secondIsNumber := jsonNumberFloat(secondValue)
	if firstIsNumber || secondIsNumber {
		return firstIsNumber && secondIsNumber && firstNumber == secondNumber
	}
	if firstItems, isList := firstValue.([]any); isList {
		secondItems, isAlsoList := secondValue.([]any)
		return isAlsoList && slices.EqualFunc(firstItems, secondItems, jsonEqual)
	}
	if isJSONObject(firstValue) {
		if !isJSONObject(secondValue) || len(objectKeys(firstValue)) != len(objectKeys(secondValue)) {
			return false
		}
		for _, fieldKey := range objectKeys(firstValue) {
			firstField, _ := objectField(firstValue, fieldKey)
			secondField, isPresent := objectField(secondValue, fieldKey)
			if !isPresent || !jsonEqual(firstField, secondField) {
				return false
			}
		}
		return true
	}
	return firstValue == secondValue
}
