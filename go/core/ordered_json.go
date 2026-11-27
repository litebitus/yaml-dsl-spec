package core

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// OrderedObject is a JSON object with its keys in the order they are written.
type OrderedObject struct {
	KeyOrder    []string
	FieldValues map[string]any
}

// DecodeOrderedJSON is JSON text read with each object an OrderedObject and each number a json.Number.
func DecodeOrderedJSON(sourceText string) (any, error) {
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
	decodedObject := &OrderedObject{FieldValues: map[string]any{}}
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
		if _, isRepeated := decodedObject.FieldValues[fieldKey]; !isRepeated {
			decodedObject.KeyOrder = append(decodedObject.KeyOrder, fieldKey)
		}
		decodedObject.FieldValues[fieldKey] = fieldValue
	}
	_, closeError := jsonDecoder.Token()
	return decodedObject, closeError
}

// IsJSONObject is whether a JSON value is an object, ordered or not.
func IsJSONObject(jsonValue any) bool {
	switch jsonValue.(type) {
	case *OrderedObject, map[string]any:
		return true
	}
	return false
}

// ObjectField is a JSON object's field, and whether the object holds it.
func ObjectField(jsonValue any, fieldKey string) (any, bool) {
	switch typedObject := jsonValue.(type) {
	case *OrderedObject:
		fieldValue, isPresent := typedObject.FieldValues[fieldKey]
		return fieldValue, isPresent
	case map[string]any:
		fieldValue, isPresent := typedObject[fieldKey]
		return fieldValue, isPresent
	}
	return nil, false
}

// ObjectKeys is a JSON object's keys, in order.
func ObjectKeys(jsonValue any) []string {
	switch typedObject := jsonValue.(type) {
	case *OrderedObject:
		return typedObject.KeyOrder
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

// JSONPointerValue is the value a JSON pointer names in a document, and whether it names one.
func JSONPointerValue(documentValue any, jsonPointer string) (any, bool) {
	if jsonPointer == "" {
		return documentValue, true
	}
	if !strings.HasPrefix(jsonPointer, "/") {
		return nil, false
	}
	currentValue := documentValue
	for _, rawStep := range strings.Split(jsonPointer[1:], "/") {
		stepKey := UnescapePointer(rawStep)
		if listItems, isList := currentValue.([]any); isList {
			itemIndex, indexError := strconv.Atoi(stepKey)
			if indexError != nil || itemIndex < 0 || itemIndex >= len(listItems) || (len(stepKey) > 1 && stepKey[0] == '0') {
				return nil, false
			}
			currentValue = listItems[itemIndex]
			continue
		}
		fieldValue, isPresent := ObjectField(currentValue, stepKey)
		if !isPresent {
			return nil, false
		}
		currentValue = fieldValue
	}
	return currentValue, true
}

// PlainJSON is a JSON value with each OrderedObject read as a map.
func PlainJSON(jsonValue any) any {
	switch typedValue := jsonValue.(type) {
	case *OrderedObject:
		plainFields := make(map[string]any, len(typedValue.KeyOrder))
		for _, fieldKey := range typedValue.KeyOrder {
			plainFields[fieldKey] = PlainJSON(typedValue.FieldValues[fieldKey])
		}
		return plainFields
	case []any:
		plainItems := make([]any, len(typedValue))
		for itemIndex, listItem := range typedValue {
			plainItems[itemIndex] = PlainJSON(listItem)
		}
		return plainItems
	}
	return jsonValue
}

// DeepCopyJSON is a copy of a JSON value's maps and lists, sharing none with it.
func DeepCopyJSON(jsonValue any) any {
	switch typedValue := jsonValue.(type) {
	case map[string]any:
		copiedFields := make(map[string]any, len(typedValue))
		for fieldKey, fieldValue := range typedValue {
			copiedFields[fieldKey] = DeepCopyJSON(fieldValue)
		}
		return copiedFields
	case []any:
		copiedItems := make([]any, len(typedValue))
		for itemIndex, listItem := range typedValue {
			copiedItems[itemIndex] = DeepCopyJSON(listItem)
		}
		return copiedItems
	}
	return jsonValue
}

// EscapePointer is a key written as a JSON pointer step, `~` as `~0` and `/` as `~1`.
func EscapePointer(fieldKey string) string {
	return strings.ReplaceAll(strings.ReplaceAll(fieldKey, "~", "~0"), "/", "~1")
}

// UnescapePointer is the key a JSON pointer step names, `~1` read as `/` and `~0` as `~`.
func UnescapePointer(rawStep string) string {
	return strings.ReplaceAll(strings.ReplaceAll(rawStep, "~1", "/"), "~0", "~")
}

// JSONText is a JSON value written as compact JSON text, objects in key order.
func JSONText(jsonValue any) string {
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
		textBuilder.WriteString(JSONString(typedValue))
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
		for fieldIndex, fieldKey := range ObjectKeys(jsonValue) {
			if fieldIndex > 0 {
				textBuilder.WriteByte(',')
			}
			textBuilder.WriteString(JSONString(fieldKey))
			textBuilder.WriteByte(':')
			fieldValue, _ := ObjectField(jsonValue, fieldKey)
			writeJSONText(textBuilder, fieldValue)
		}
		textBuilder.WriteByte('}')
	}
}

// JSONString is a string written as a JSON string.
func JSONString(stringText string) string {
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

// NumberText is a JSON value's text, a fractional number written as FloatNumberText writes it.
func NumberText(jsonValue any) string {
	jsonNumber, isNumber := jsonValue.(json.Number)
	if !isNumber {
		return JSONText(jsonValue)
	}
	if floatValue, isFloat := NumberJSON(jsonNumber.String()).(float64); isFloat {
		return FloatNumberText(floatValue)
	}
	return JSONText(jsonValue)
}

// FloatNumberText is a float's text, a whole value written without a fraction.
func FloatNumberText(floatValue float64) string {
	if floatValue == math.Trunc(floatValue) && math.Abs(floatValue) < 1e21 {
		return strconv.FormatFloat(floatValue, 'f', 0, 64)
	}
	return serdeFloatText(floatValue)
}

// JSONNumberFloat is a JSON number's value as a float, and whether the value is a number.
func JSONNumberFloat(jsonValue any) (float64, bool) {
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

// JSONEqual is whether two JSON values are equal, objects in any key order and numbers by value.
func JSONEqual(firstValue, secondValue any) bool {
	firstNumber, firstIsNumber := JSONNumberFloat(firstValue)
	secondNumber, secondIsNumber := JSONNumberFloat(secondValue)
	if firstIsNumber || secondIsNumber {
		return firstIsNumber && secondIsNumber && firstNumber == secondNumber
	}
	if firstItems, isList := firstValue.([]any); isList {
		secondItems, isAlsoList := secondValue.([]any)
		return isAlsoList && slices.EqualFunc(firstItems, secondItems, JSONEqual)
	}
	if IsJSONObject(firstValue) {
		if !IsJSONObject(secondValue) || len(ObjectKeys(firstValue)) != len(ObjectKeys(secondValue)) {
			return false
		}
		for _, fieldKey := range ObjectKeys(firstValue) {
			firstField, _ := ObjectField(firstValue, fieldKey)
			secondField, isPresent := ObjectField(secondValue, fieldKey)
			if !isPresent || !JSONEqual(firstField, secondField) {
				return false
			}
		}
		return true
	}
	return firstValue == secondValue
}
