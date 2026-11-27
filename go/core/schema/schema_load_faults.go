package schema

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/litebitus/yaml-dsl-spec/go/core"
)

var schemaDataKeywords = []string{"const", "default", "enum", "examples"}

func firstSchemaFault(wholeSchema, schemaNode any, nodePointer string) error {
	switch typedNode := schemaNode.(type) {
	case []any:
		for itemIndex, listedNode := range typedNode {
			itemPointer := nodePointer + "/" + strconv.Itoa(itemIndex)
			if listedFault := firstSchemaFault(wholeSchema, listedNode, itemPointer); listedFault != nil {
				return listedFault
			}
		}
	case *core.OrderedObject:
		for _, schemaKeyword := range typedNode.KeyOrder {
			keywordValue := typedNode.FieldValues[schemaKeyword]
			if foundFault := keywordFault(wholeSchema, schemaKeyword, keywordValue, nodePointer); foundFault != nil {
				return foundFault
			}
		}
	}
	return nil
}

func keywordFault(wholeSchema any, schemaKeyword string, keywordValue any, holderPointer string) error {
	keywordText, isText := keywordValue.(string)
	keywordPointer := holderPointer + "/" + core.EscapePointer(schemaKeyword)
	switch {
	case slices.Contains(schemaDataKeywords, schemaKeyword):
		return nil
	case schemaKeyword == "$ref" && isText:
		targetPointer, isPointerReference := referenceTargetPointer(wholeSchema, holderPointer, keywordText)
		if _, isNode := core.JSONPointerValue(wholeSchema, targetPointer); isPointerReference && !isNode {
			return fmt.Errorf("%s names no node of the schema", core.CodeSpan(keywordText))
		}
		return nil
	case schemaKeyword == "pattern" && isText:
		return patternFault(keywordText)
	case slices.Contains(schemaMapKeywords, schemaKeyword) && core.IsJSONObject(keywordValue):
		for _, memberName := range core.ObjectKeys(keywordValue) {
			if schemaKeyword == "patternProperties" {
				if memberFault := patternFault(memberName); memberFault != nil {
					return memberFault
				}
			}
			memberSchema, _ := core.ObjectField(keywordValue, memberName)
			memberPointer := keywordPointer + "/" + core.EscapePointer(memberName)
			if memberFault := firstSchemaFault(wholeSchema, memberSchema, memberPointer); memberFault != nil {
				return memberFault
			}
		}
		return nil
	}
	return firstSchemaFault(wholeSchema, keywordValue, keywordPointer)
}

func patternFault(patternText string) error {
	if _, compileError := core.CompilePattern(patternText); compileError != nil {
		return fmt.Errorf("%s is not a pattern", core.CodeSpan(patternText))
	}
	return nil
}
