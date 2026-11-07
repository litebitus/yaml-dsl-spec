package yamldslspec

import (
	"slices"
	"strconv"
	"strings"
)

var hoistedKeywords = []string{"type", "const", "enum"}

type assertionHoisting struct {
	hoistedBranches map[string]string
	readKeywords    draftKeywords
}

func (schemaHoisting assertionHoisting) hoistedNode(schemaNode any, nodePointer string) any {
	sourceObject, isObject := schemaNode.(*orderedObject)
	if !isObject {
		return schemaNode
	}
	hoistedObject := &orderedObject{fieldValues: map[string]any{}}
	var hoistedSchemas []any
	for _, schemaKeyword := range sourceObject.keyOrder {
		keywordValue := sourceObject.fieldValues[schemaKeyword]
		switch {
		case schemaKeyword == "format":
		case slices.Contains(hoistedKeywords, schemaKeyword):
			hoistedSchemas = append(hoistedSchemas, &orderedObject{
				keyOrder:    []string{schemaKeyword},
				fieldValues: map[string]any{schemaKeyword: keywordValue},
			})
		default:
			keywordPointer := nodePointer + "/" + escapePointer(schemaKeyword)
			hoistedObject.keyOrder = append(hoistedObject.keyOrder, schemaKeyword)
			hoistedObject.fieldValues[schemaKeyword] = schemaHoisting.hoistedChild(schemaKeyword, keywordValue, keywordPointer)
		}
	}
	if len(hoistedSchemas) == 0 {
		return hoistedObject
	}
	allOfBranches, hasAllOf := hoistedObject.fieldValues["allOf"].([]any)
	if !hasAllOf {
		hoistedObject.keyOrder = append(hoistedObject.keyOrder, "allOf")
	}
	for _, hoistedSchema := range hoistedSchemas {
		branchPointer := nodePointer + "/allOf/" + strconv.Itoa(len(allOfBranches))
		schemaHoisting.hoistedBranches[branchPointer] = nodePointer
		allOfBranches = append(allOfBranches, hoistedSchema)
	}
	hoistedObject.fieldValues["allOf"] = allOfBranches
	return hoistedObject
}

func (schemaHoisting assertionHoisting) hoistedChild(schemaKeyword string, keywordValue any, keywordPointer string) any {
	childSchemas, isList := keywordValue.([]any)
	switch {
	case slices.Contains(sameInstanceSchemaKeywords, schemaKeyword):
		return schemaHoisting.hoistedNode(keywordValue, keywordPointer)
	case slices.Contains(sameInstanceListKeywords, schemaKeyword) ||
		slices.Contains(schemaHoisting.readKeywords.childSchemas, schemaKeyword):
		if !isList {
			return schemaHoisting.hoistedNode(keywordValue, keywordPointer)
		}
		hoistedList := make([]any, len(childSchemas))
		for childIndex, childSchema := range childSchemas {
			hoistedList[childIndex] = schemaHoisting.hoistedNode(childSchema, keywordPointer+"/"+strconv.Itoa(childIndex))
		}
		return hoistedList
	case slices.Contains(schemaHoisting.readKeywords.schemaMaps, schemaKeyword) && isJSONObject(keywordValue):
		hoistedMap := &orderedObject{fieldValues: map[string]any{}}
		for _, schemaName := range objectKeys(keywordValue) {
			namedSchema, _ := objectField(keywordValue, schemaName)
			hoistedMap.keyOrder = append(hoistedMap.keyOrder, schemaName)
			hoistedMap.fieldValues[schemaName] = schemaHoisting.hoistedNode(namedSchema, keywordPointer+"/"+escapePointer(schemaName))
		}
		return hoistedMap
	}
	return keywordValue
}

func (loadedSchema *LoadedSchema) realPointer(compiledPointer string) string {
	if holderPointer, isHoisted := loadedSchema.hoistedBranches[compiledPointer]; isHoisted {
		return holderPointer
	}
	lastSlash := strings.LastIndexByte(compiledPointer, '/')
	if lastSlash < 0 {
		return compiledPointer
	}
	if holderPointer, isHoisted := loadedSchema.hoistedBranches[compiledPointer[:lastSlash]]; isHoisted {
		return holderPointer + compiledPointer[lastSlash:]
	}
	return compiledPointer
}
