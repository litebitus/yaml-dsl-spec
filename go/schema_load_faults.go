package yamldslspec

import (
	"fmt"
	"slices"
	"strconv"
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
	case *orderedObject:
		for _, schemaKeyword := range typedNode.keyOrder {
			keywordValue := typedNode.fieldValues[schemaKeyword]
			if foundFault := keywordFault(wholeSchema, schemaKeyword, keywordValue, nodePointer); foundFault != nil {
				return foundFault
			}
		}
	}
	return nil
}

func keywordFault(wholeSchema any, schemaKeyword string, keywordValue any, holderPointer string) error {
	keywordText, isText := keywordValue.(string)
	keywordPointer := holderPointer + "/" + escapePointer(schemaKeyword)
	switch {
	case slices.Contains(schemaDataKeywords, schemaKeyword):
		return nil
	case schemaKeyword == "$ref" && isText:
		targetPointer, isPointerReference := referenceTargetPointer(wholeSchema, holderPointer, keywordText)
		if _, isNode := jsonPointerValue(wholeSchema, targetPointer); isPointerReference && !isNode {
			return fmt.Errorf("%s names no node of the schema", CodeSpan(keywordText))
		}
		return nil
	case schemaKeyword == "pattern" && isText:
		return patternFault(keywordText)
	case slices.Contains(schemaMapKeywords, schemaKeyword) && isJSONObject(keywordValue):
		for _, memberName := range objectKeys(keywordValue) {
			if schemaKeyword == "patternProperties" {
				if memberFault := patternFault(memberName); memberFault != nil {
					return memberFault
				}
			}
			memberSchema, _ := objectField(keywordValue, memberName)
			memberPointer := keywordPointer + "/" + escapePointer(memberName)
			if memberFault := firstSchemaFault(wholeSchema, memberSchema, memberPointer); memberFault != nil {
				return memberFault
			}
		}
		return nil
	}
	return firstSchemaFault(wholeSchema, keywordValue, keywordPointer)
}

func patternFault(patternText string) error {
	if _, compileError := CompilePattern(patternText); compileError != nil {
		return fmt.Errorf("%s is not a pattern", CodeSpan(patternText))
	}
	return nil
}
