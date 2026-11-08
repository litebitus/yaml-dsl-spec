package yamldslspec

import (
	"slices"
	"strconv"
	"strings"
)

type instanceRecord struct {
	validationRules ValidationRules
	keyPositions    map[string]Position
	valuePositions  map[string]Position
	unknownValues   map[string]bool
	copiedValues    map[string]bool
}

func newInstanceRecord(validationRules ValidationRules) *instanceRecord {
	return &instanceRecord{
		validationRules: validationRules,
		keyPositions:    map[string]Position{},
		valuePositions:  map[string]Position{},
		unknownValues:   map[string]bool{},
		copiedValues:    map[string]bool{},
	}
}

func (documentRecord *instanceRecord) jsonOf(resolvedValue ResolvedValue, valuePointer string, keyPosition Position) any {
	documentRecord.keyPositions[valuePointer] = keyPosition
	documentRecord.valuePositions[valuePointer] = resolvedValue.Position
	localValue := resolvedValue.LocalValue()
	switch localValue.Kind {
	case ResolvedNull:
		return nil
	case ResolvedBool:
		return localValue.Flag
	case ResolvedNumber:
		return NumberJSON(localValue.Text)
	case ResolvedString:
		return localValue.Text
	case ResolvedList:
		listItems := make([]any, 0, len(localValue.Items))
		for itemIndex, listItem := range localValue.Items {
			listItems = append(listItems, documentRecord.jsonOf(listItem, valuePointer+"/"+strconv.Itoa(itemIndex), listItem.Position))
		}
		return listItems
	case ResolvedMap:
		objectFields := map[string]any{}
		var keyOrder []string
		for _, mapEntry := range localValue.Entries {
			childPointer := valuePointer + "/" + escapePointer(mapEntry.Key)
			childJSON := documentRecord.jsonOf(mapEntry.Value, childPointer, mapEntry.KeyPosition)
			if childFields, isObject := childJSON.(map[string]any); isObject {
				documentRecord.setChildValue(mapEntry.Key, childFields)
			}
			if _, isRepeated := objectFields[mapEntry.Key]; !isRepeated {
				keyOrder = append(keyOrder, mapEntry.Key)
			}
			objectFields[mapEntry.Key] = childJSON
		}
		documentRecord.mergeBaseKeys(objectFields, keyOrder, valuePointer)
		return objectFields
	}
	documentRecord.unknownValues[valuePointer] = true
	return nil
}

func (documentRecord *instanceRecord) setChildValue(entryKey string, childFields map[string]any) {
	childField, childText, isSet := documentRecord.validationRules.ChildValue(entryKey)
	if _, isWritten := childFields[childField]; isSet && !isWritten {
		childFields[childField] = childText
	}
}

func (documentRecord *instanceRecord) mergeBaseKeys(objectFields map[string]any, keyOrder []string, objectPointer string) {
	mergeBaseKeys := documentRecord.validationRules.MergeBaseKeys
	for _, baseKey := range mergeBaseKeys {
		baseValue, hasBase := objectFields[baseKey]
		if !hasBase {
			continue
		}
		for _, siblingKey := range keyOrder {
			writtenFields, isObject := objectFields[siblingKey].(map[string]any)
			if slices.Contains(mergeBaseKeys, siblingKey) || !isObject {
				continue
			}
			entryPointer := objectPointer + "/" + escapePointer(siblingKey)
			mergedFields := mergedOver(baseValue, writtenFields, entryPointer, documentRecord.isUnknown)
			for _, copiedPointer := range copiedPointers(mergedFields, writtenFields, entryPointer) {
				documentRecord.copiedValues[copiedPointer] = true
			}
			objectFields[siblingKey] = mergedFields
		}
	}
}

func (documentRecord *instanceRecord) isCopied(valuePointer string) bool {
	return matchesOrBelow(documentRecord.copiedValues, valuePointer)
}

func (documentRecord *instanceRecord) isUnknown(valuePointer string) bool {
	return matchesOrBelow(documentRecord.unknownValues, valuePointer)
}

func matchesOrBelow(recordedPointers map[string]bool, valuePointer string) bool {
	for recordedPointer := range recordedPointers {
		if valuePointer == recordedPointer || strings.HasPrefix(valuePointer, recordedPointer+"/") {
			return true
		}
	}
	return false
}

func (documentRecord *instanceRecord) readsUnknown(valuePointer, failedKeyword string) bool {
	if documentRecord.isUnknown(valuePointer) {
		return true
	}
	return slices.Contains(readsBelowKeywords, failedKeyword) && documentRecord.holdsUnknownBelow(valuePointer)
}

func (documentRecord *instanceRecord) memberReadsUnknown(valuePointer string, schemaCouldRead func() bool) bool {
	return documentRecord.holdsUnknownBelow(valuePointer) && schemaCouldRead()
}

func (documentRecord *instanceRecord) holdsUnknownBelow(valuePointer string) bool {
	for unknownPointer := range documentRecord.unknownValues {
		if strings.HasPrefix(unknownPointer, valuePointer+"/") {
			return true
		}
	}
	return false
}

func (documentRecord *instanceRecord) valuePosition(valuePointer string) Position {
	if valuePosition, isFound := documentRecord.valuePositions[valuePointer]; isFound {
		return valuePosition
	}
	return documentRecord.keyPosition(valuePointer)
}

func (documentRecord *instanceRecord) keyPosition(valuePointer string) Position {
	currentPointer := valuePointer
	for {
		if keyPosition, isFound := documentRecord.keyPositions[currentPointer]; isFound {
			return keyPosition
		}
		if currentPointer == "" {
			return Position{}
		}
		currentPointer = currentPointer[:max(strings.LastIndexByte(currentPointer, '/'), 0)]
	}
}
