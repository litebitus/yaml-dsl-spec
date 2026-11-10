package yamldslspec

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

type instanceRecord struct {
	validationRules ValidationRules
	functionTable   FunctionTable
	keyPositions    map[string]Position
	valuePositions  map[string]Position
	unknownValues   map[string]bool
	copiedValues    map[string]bool
	unknownMerges   map[string]bool
}

func newInstanceRecord(validationRules ValidationRules, functionTable FunctionTable) *instanceRecord {
	return &instanceRecord{
		validationRules: validationRules,
		functionTable:   functionTable,
		keyPositions:    map[string]Position{},
		valuePositions:  map[string]Position{},
		unknownValues:   map[string]bool{},
		copiedValues:    map[string]bool{},
		unknownMerges:   map[string]bool{},
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
	case ResolvedCall:
		if mergedFields, isMerged := documentRecord.mergeCallFields(localValue, valuePointer, keyPosition); isMerged {
			return mergedFields
		}
	}
	documentRecord.unknownValues[valuePointer] = true
	return nil
}

func (documentRecord *instanceRecord) mergeCallFields(
	callValue ResolvedValue,
	valuePointer string,
	keyPosition Position,
) (map[string]any, bool) {
	if callValue.Call == nil {
		return nil, false
	}
	functionEntry, isFound := documentRecord.functionTable.Entry(callValue.Call.Function)
	if !isFound || functionEntry.Macro == nil || functionEntry.Macro.Kind != MacroKindMerge {
		return nil, false
	}
	var knownMaps []ResolvedValue
	holdsUnknownArgument := false
	for _, callArgument := range callValue.Call.Arguments {
		switch localArgument := callArgument.LocalValue(); localArgument.Kind {
		case ResolvedMap:
			knownMaps = append(knownMaps, localArgument)
		case ResolvedReference, ResolvedCall, ResolvedTemplate:
			holdsUnknownArgument = true
		}
	}
	if len(knownMaps) == 0 {
		return nil, false
	}
	mergedFields := map[string]any{}
	for _, knownMap := range knownMaps {
		argumentRecord := newInstanceRecord(documentRecord.validationRules, documentRecord.functionTable)
		argumentFields, _ := argumentRecord.jsonOf(knownMap, valuePointer, keyPosition).(map[string]any)
		documentRecord.mergeArgumentInto(
			mergedFields,
			argumentFields,
			valuePointer,
			functionEntry.Macro.MergeBehavior,
			argumentRecord,
		)
	}
	if holdsUnknownArgument {
		documentRecord.unknownMerges[valuePointer] = true
	}
	return mergedFields, true
}

func (documentRecord *instanceRecord) mergeArgumentInto(
	mergedFields, argumentFields map[string]any,
	fieldsPointer string,
	mergeBehavior MergeBehavior,
	argumentRecord *instanceRecord,
) {
	for fieldKey, argumentValue := range argumentFields {
		childPointer := fieldsPointer + "/" + escapePointer(fieldKey)
		heldValue, isHeld := mergedFields[fieldKey]
		heldFields, heldIsObject := heldValue.(map[string]any)
		argumentObject, argumentIsObject := argumentValue.(map[string]any)
		switch {
		case isHeld && mergeBehavior == MergeBehaviorJoin:
		case heldIsObject && argumentIsObject && mergeBehavior == MergeBehaviorDeep:
			documentRecord.mergeArgumentInto(heldFields, argumentObject, childPointer, mergeBehavior, argumentRecord)
		default:
			mergedFields[fieldKey] = argumentValue
			documentRecord.takeRecordsBelow(argumentRecord, childPointer)
		}
	}
}

func (documentRecord *instanceRecord) takeRecordsBelow(argumentRecord *instanceRecord, valuePointer string) {
	replacedRecords(documentRecord.keyPositions, argumentRecord.keyPositions, valuePointer)
	replacedRecords(documentRecord.valuePositions, argumentRecord.valuePositions, valuePointer)
	replacedRecords(documentRecord.unknownValues, argumentRecord.unknownValues, valuePointer)
	replacedRecords(documentRecord.copiedValues, argumentRecord.copiedValues, valuePointer)
	replacedRecords(documentRecord.unknownMerges, argumentRecord.unknownMerges, valuePointer)
}

func replacedRecords[RecordedValue any](heldRecords, takenRecords map[string]RecordedValue, valuePointer string) {
	maps.DeleteFunc(heldRecords, func(recordedPointer string, _ RecordedValue) bool {
		return pointerAtOrBelow(recordedPointer, valuePointer)
	})
	for recordedPointer, recordedValue := range takenRecords {
		if pointerAtOrBelow(recordedPointer, valuePointer) {
			heldRecords[recordedPointer] = recordedValue
		}
	}
}

func (documentRecord *instanceRecord) requiresNoKeyAt(valuePointer string) bool {
	if matchesOrBelow(documentRecord.unknownMerges, valuePointer) {
		return true
	}
	for _, rawStep := range strings.Split(valuePointer, "/")[1:] {
		if slices.Contains(documentRecord.validationRules.MergeBaseKeys, unescapePointer(rawStep)) {
			return true
		}
	}
	return false
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
		if pointerAtOrBelow(valuePointer, recordedPointer) {
			return true
		}
	}
	return false
}

func pointerAtOrBelow(valuePointer, holderPointer string) bool {
	return valuePointer == holderPointer || strings.HasPrefix(valuePointer, holderPointer+"/")
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
