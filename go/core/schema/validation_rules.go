package schema

import (
	"fmt"
	"slices"
	"strings"

	"github.com/litebitus/yaml-dsl-spec/go/core"
)

// ValidationRules is a DSL's `validator` block with its patterns compiled.
type ValidationRules struct {
	MergeBaseKeys      []string
	ChildValuePatterns []*core.CompiledPattern
	QuotedValueTypes   []core.QuotedValueType
}

// ValidationRulesFromValidator is the rules a DSL's `validator` block states; no rules for none.
func ValidationRulesFromValidator(dslValidator *core.Validator) (ValidationRules, error) {
	if dslValidator == nil {
		return ValidationRules{}, nil
	}
	var childValuePatterns []*core.CompiledPattern
	for _, childRule := range dslValidator.ChildValuesFromParentKey {
		compiledPattern, compileError := core.CompilePattern(childRule.Pattern)
		if compileError != nil {
			return ValidationRules{}, fmt.Errorf("`%s`: %v", childRule.Pattern, compileError)
		}
		childValuePatterns = append(childValuePatterns, compiledPattern)
	}
	return ValidationRules{
		MergeBaseKeys:      slices.Clone(dslValidator.MergeBaseKeys),
		ChildValuePatterns: childValuePatterns,
		QuotedValueTypes:   slices.Clone(dslValidator.AllowQuotedValues),
	}, nil
}

// ChildValue is the field and value a key sets on the entry it keys, by the rules' patterns.
func (validationRules ValidationRules) ChildValue(entryKey string) (string, string, bool) {
	return ChildValue(entryKey, validationRules.ChildValuePatterns)
}

// ChildValue is the field and value a key sets, by the first pattern with `field` and `value` groups that matches.
func ChildValue(entryKey string, childPatterns []*core.CompiledPattern) (string, string, bool) {
	for _, childPattern := range childPatterns {
		foundMatch, isMatched := childPattern.FindMatch(entryKey)
		if !isMatched {
			continue
		}
		childField, hasField := foundMatch.NamedGroups["field"]
		childValue, hasValue := foundMatch.NamedGroups["value"]
		if hasField && hasValue {
			return childField, childValue, true
		}
	}
	return "", "", false
}

func (validationRules ValidationRules) quotedValue(quotedText string, expectedTypes []string) (any, bool) {
	for _, expectedType := range expectedTypes {
		quotedType := core.QuotedValueType(expectedType)
		if !slices.Contains(core.QuotedTypeVariants, expectedType) ||
			!slices.Contains(validationRules.QuotedValueTypes, quotedType) {
			continue
		}
		if readValue, isSpelled := readQuotedValue(quotedText, quotedType); isSpelled {
			return readValue, true
		}
	}
	return nil, false
}

func readQuotedValue(quotedText string, quotedType core.QuotedValueType) (any, bool) {
	switch {
	case quotedType == core.QuotedValueBoolean && (quotedText == "true" || quotedText == "false"):
		return quotedText == "true", true
	case quotedType == core.QuotedValueInteger && spellsInteger(quotedText):
		return core.NumberJSON(quotedText), true
	case quotedType == core.QuotedValueNumber && spellsNumber(quotedText):
		return core.NumberJSON(quotedText), true
	}
	return nil, false
}

func spellsInteger(quotedText string) bool {
	digitText := strings.TrimPrefix(quotedText, "-")
	if digitText == "0" {
		return true
	}
	return digitText != "" && digitText[0] >= '1' && digitText[0] <= '9' && core.OnlyDigits(digitText)
}

func spellsNumber(quotedText string) bool {
	mantissaText, exponentText, hasExponent := quotedText, "", false
	if exponentAt := strings.IndexAny(quotedText, "eE"); exponentAt >= 0 {
		mantissaText, exponentText, hasExponent = quotedText[:exponentAt], quotedText[exponentAt+1:], true
	}
	wholeText, fractionText, hasFraction := strings.Cut(mantissaText, ".")
	someDigits := func(partText string) bool { return partText != "" && core.OnlyDigits(partText) }
	exponentSpelled := true
	if hasExponent {
		exponentDigits := exponentText
		if exponentDigits != "" && (exponentDigits[0] == '+' || exponentDigits[0] == '-') {
			exponentDigits = exponentDigits[1:]
		}
		exponentSpelled = someDigits(exponentDigits)
	}
	return spellsInteger(wholeText) && (!hasFraction || someDigits(fractionText)) && exponentSpelled
}

func mergedOver(
	baseValue any,
	overlayFields map[string]any,
	overlayPointer string,
	holdsUnknown func(string) bool,
) map[string]any {
	mergedFields, isObject := core.DeepCopyJSON(baseValue).(map[string]any)
	if !isObject {
		mergedFields = map[string]any{}
	}
	mergeInto(mergedFields, overlayFields, overlayPointer, holdsUnknown)
	return mergedFields
}

func mergeInto(targetFields, overlayFields map[string]any, overlayPointer string, holdsUnknown func(string) bool) {
	for fieldKey, overlayValue := range overlayFields {
		childPointer := overlayPointer + "/" + core.EscapePointer(fieldKey)
		if overlayValue == nil && !holdsUnknown(childPointer) {
			delete(targetFields, fieldKey)
			continue
		}
		heldFields, heldIsObject := targetFields[fieldKey].(map[string]any)
		overlayObject, overlayIsObject := overlayValue.(map[string]any)
		if heldIsObject && overlayIsObject && len(overlayObject) > 0 {
			mergeInto(heldFields, overlayObject, childPointer, holdsUnknown)
			continue
		}
		targetFields[fieldKey] = core.DeepCopyJSON(overlayValue)
	}
}

func copiedPointers(mergedValue, writtenValue any, valuePointer string) []string {
	mergedFields, mergedIsObject := mergedValue.(map[string]any)
	writtenFields, writtenIsObject := writtenValue.(map[string]any)
	if !mergedIsObject || !writtenIsObject {
		return nil
	}
	var copiedFields []string
	for fieldKey, fieldValue := range mergedFields {
		childPointer := valuePointer + "/" + core.EscapePointer(fieldKey)
		writtenField, isWritten := writtenFields[fieldKey]
		if !isWritten {
			copiedFields = append(copiedFields, childPointer)
			continue
		}
		copiedFields = append(copiedFields, copiedPointers(fieldValue, writtenField, childPointer)...)
	}
	return copiedFields
}
