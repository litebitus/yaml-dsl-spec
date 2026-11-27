package schema

import (
	"slices"
	"strconv"
	"strings"

	"github.com/litebitus/yaml-dsl-spec/go/core"
)

var hoistedKeywords = []string{"type", "const", "enum"}

type assertionHoisting struct {
	hoistedBranches map[string]string
	readKeywords    draftKeywords
}

func (schemaHoisting assertionHoisting) hoistedNode(schemaNode any, nodePointer string) any {
	sourceObject, isObject := schemaNode.(*core.OrderedObject)
	if !isObject {
		return schemaNode
	}
	hoistedObject := &core.OrderedObject{FieldValues: map[string]any{}}
	var hoistedSchemas []any
	for _, schemaKeyword := range sourceObject.KeyOrder {
		keywordValue := sourceObject.FieldValues[schemaKeyword]
		switch {
		case schemaKeyword == "format":
		case slices.Contains(hoistedKeywords, schemaKeyword):
			hoistedSchemas = append(hoistedSchemas, &core.OrderedObject{
				KeyOrder:    []string{schemaKeyword},
				FieldValues: map[string]any{schemaKeyword: keywordValue},
			})
		default:
			keywordPointer := nodePointer + "/" + core.EscapePointer(schemaKeyword)
			hoistedObject.KeyOrder = append(hoistedObject.KeyOrder, schemaKeyword)
			hoistedObject.FieldValues[schemaKeyword] =
				schemaHoisting.hoistedChild(schemaKeyword, keywordValue, keywordPointer)
		}
	}
	if len(hoistedSchemas) == 0 {
		return hoistedObject
	}
	allOfBranches, hasAllOf := hoistedObject.FieldValues["allOf"].([]any)
	if !hasAllOf {
		hoistedObject.KeyOrder = append(hoistedObject.KeyOrder, "allOf")
	}
	for _, hoistedSchema := range hoistedSchemas {
		branchPointer := nodePointer + "/allOf/" + strconv.Itoa(len(allOfBranches))
		schemaHoisting.hoistedBranches[branchPointer] = nodePointer
		allOfBranches = append(allOfBranches, hoistedSchema)
	}
	hoistedObject.FieldValues["allOf"] = allOfBranches
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
	case slices.Contains(schemaHoisting.readKeywords.schemaMaps, schemaKeyword) && core.IsJSONObject(keywordValue):
		hoistedMap := &core.OrderedObject{FieldValues: map[string]any{}}
		for _, schemaName := range core.ObjectKeys(keywordValue) {
			namedSchema, _ := core.ObjectField(keywordValue, schemaName)
			hoistedMap.KeyOrder = append(hoistedMap.KeyOrder, schemaName)
			hoistedMap.FieldValues[schemaName] =
				schemaHoisting.hoistedNode(namedSchema, keywordPointer+"/"+core.EscapePointer(schemaName))
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
