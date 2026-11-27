package schema

import (
	"slices"
	"strings"
	"unicode"

	"github.com/litebitus/yaml-dsl-spec/go/core"
)

type schemaDraft int

const (
	draftSeven schemaDraft = iota
	draftTwentyNineteen
	draftTwentyTwenty
)

const requiredMarker = "[required]"

var (
	requirednessMarkers        = []string{requiredMarker, "[~required]", "[optional]"}
	conditionKeywords          = []string{"if", "not"}
	formSelectingKeywords      = []string{"anyOf", "oneOf"}
	sameInstanceSchemaKeywords = []string{"else", "if", "not", "then"}
	sameInstanceListKeywords   = []string{"allOf", "anyOf", "oneOf"}
	draftKeywordsByDraft       = map[schemaDraft]draftKeywords{
		draftSeven: {
			childSchemas: []string{"additionalItems", "additionalProperties", "contains", "items", "propertyNames"},
			schemaMaps:   []string{"$defs", "definitions", "dependencies", "patternProperties", "properties"},
		},
		draftTwentyNineteen: {
			childSchemas: []string{
				"additionalItems",
				"additionalProperties",
				"contains",
				"contentSchema",
				"items",
				"propertyNames",
				"unevaluatedItems",
				"unevaluatedProperties",
			},
			schemaMaps: []string{"$defs", "definitions", "dependentSchemas", "patternProperties", "properties"},
		},
		draftTwentyTwenty: {
			childSchemas: []string{
				"additionalProperties",
				"contains",
				"contentSchema",
				"items",
				"prefixItems",
				"propertyNames",
				"unevaluatedItems",
				"unevaluatedProperties",
			},
			schemaMaps: []string{"$defs", "definitions", "dependentSchemas", "patternProperties", "properties"},
		},
	}
)

type draftKeywords struct {
	childSchemas []string
	schemaMaps   []string
}

type fieldMarker struct {
	fieldKey   string
	markerText string
}

func requirednessMarker(schemaNode any) (string, bool) {
	descriptionValue, _ := core.ObjectField(schemaNode, "description")
	writtenDescription, isText := descriptionValue.(string)
	if !isText {
		return "", false
	}
	writtenDescription = strings.TrimLeftFunc(writtenDescription, unicode.IsSpace)
	for _, markerText := range requirednessMarkers {
		if strings.HasPrefix(writtenDescription, markerText) {
			return markerText, true
		}
	}
	return "", false
}

func descriptionText(schemaNode any) *string {
	descriptionValue, _ := core.ObjectField(schemaNode, "description")
	writtenText, isText := descriptionValue.(string)
	if !isText || strings.TrimSpace(writtenText) == "" {
		return nil
	}
	return &writtenText
}

func markedSchema(schemaValue any, readDraft schemaDraft) any {
	return markedNode(schemaValue, nil, draftKeywordsByDraft[readDraft])
}

func markersOfFields(propertiesValue any) []fieldMarker {
	fieldMarkers := []fieldMarker{}
	for _, fieldKey := range core.ObjectKeys(propertiesValue) {
		fieldSchema, _ := core.ObjectField(propertiesValue, fieldKey)
		if markerText, isMarked := requirednessMarker(fieldSchema); isMarked {
			fieldMarkers = append(fieldMarkers, fieldMarker{fieldKey: fieldKey, markerText: markerText})
		}
	}
	return fieldMarkers
}

func markedNode(schemaNode any, inheritedMarkers *[]fieldMarker, readKeywords draftKeywords) any {
	sourceObject, isObject := schemaNode.(*core.OrderedObject)
	if !isObject {
		return schemaNode
	}
	var ownMarkers *[]fieldMarker
	if propertiesValue, hasProperties := sourceObject.FieldValues["properties"]; hasProperties &&
		core.IsJSONObject(propertiesValue) {
		foundMarkers := markersOfFields(propertiesValue)
		ownMarkers = &foundMarkers
	}
	activeMarkers := inheritedMarkers
	if ownMarkers != nil {
		activeMarkers = ownMarkers
	}
	markedObject := &core.OrderedObject{FieldValues: map[string]any{}}
	for _, schemaKeyword := range sourceObject.KeyOrder {
		if schemaKeyword == "required" {
			continue
		}
		markedObject.KeyOrder = append(markedObject.KeyOrder, schemaKeyword)
		keywordValue := sourceObject.FieldValues[schemaKeyword]
		markedObject.FieldValues[schemaKeyword] =
			markedKeywordValue(schemaKeyword, keywordValue, activeMarkers, readKeywords)
	}
	isMarked := func(fieldKey string) bool {
		return activeMarkers != nil && slices.ContainsFunc(*activeMarkers, func(activeMarker fieldMarker) bool {
			return activeMarker.fieldKey == fieldKey
		})
	}
	var requiredFields []any
	if listedFields, isList := sourceObject.FieldValues["required"].([]any); isList {
		for _, listedField := range listedFields {
			if fieldKey, isText := listedField.(string); isText && isMarked(fieldKey) {
				continue
			}
			requiredFields = append(requiredFields, listedField)
		}
	}
	if ownMarkers != nil {
		for _, ownMarker := range *ownMarkers {
			if ownMarker.markerText == requiredMarker {
				requiredFields = append(requiredFields, ownMarker.fieldKey)
			}
		}
	}
	if len(requiredFields) > 0 {
		markedObject.KeyOrder = append(markedObject.KeyOrder, "required")
		markedObject.FieldValues["required"] = requiredFields
	}
	return markedObject
}

func markedKeywordValue(schemaKeyword string, keywordValue any, activeMarkers *[]fieldMarker, readKeywords draftKeywords) any {
	branchSchemas, isList := keywordValue.([]any)
	switch {
	case slices.Contains(conditionKeywords, schemaKeyword):
		return keywordValue
	case slices.Contains(sameInstanceSchemaKeywords, schemaKeyword):
		return markedNode(keywordValue, activeMarkers, readKeywords)
	case slices.Contains(sameInstanceListKeywords, schemaKeyword) && isList:
		branchMarkers := activeMarkers
		if slices.Contains(formSelectingKeywords, schemaKeyword) {
			branchMarkers = nil
		}
		markedBranches := make([]any, len(branchSchemas))
		for branchIndex, branchSchema := range branchSchemas {
			markedBranches[branchIndex] = markedNode(branchSchema, branchMarkers, readKeywords)
		}
		return markedBranches
	case slices.Contains(readKeywords.childSchemas, schemaKeyword):
		if !isList {
			return markedNode(keywordValue, nil, readKeywords)
		}
		markedChildren := make([]any, len(branchSchemas))
		for childIndex, childSchema := range branchSchemas {
			markedChildren[childIndex] = markedNode(childSchema, nil, readKeywords)
		}
		return markedChildren
	case slices.Contains(readKeywords.schemaMaps, schemaKeyword) && core.IsJSONObject(keywordValue):
		markedSchemas := &core.OrderedObject{FieldValues: map[string]any{}}
		for _, schemaName := range core.ObjectKeys(keywordValue) {
			namedSchema, _ := core.ObjectField(keywordValue, schemaName)
			markedSchemas.KeyOrder = append(markedSchemas.KeyOrder, schemaName)
			markedSchemas.FieldValues[schemaName] = markedNode(namedSchema, nil, readKeywords)
		}
		return markedSchemas
	}
	return keywordValue
}
