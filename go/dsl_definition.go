package yamldslspec

import (
	"fmt"
	"strings"
)

// DSLDefinition is a DSL block of `yaml-dsl.yml`, its fields named as the standard names them.
type DSLDefinition struct {
	ID                string
	FileIncludes      []string
	FileExcludes      []string
	SchemaSearchPaths []string
	Layers            *Layers
	Placeholder       *Placeholder
	Function          *FunctionSettings
	Locals            *Locals
	Loops             *Loops
	Scopes            map[string]Scope
	Declarations      []DeclarationRule
	References        []ReferenceRule
	TagKeys           []TagKey
	Validator         *Validator
}

// Layers is a DSL's `layers` block.
type Layers struct {
	OverlayFolders       []string
	CommonLayerDiscovery CommonLayerDiscovery
	CommonLayerFiles     map[string][]string
}

// CommonLayerDiscovery is how a DSL finds an overlay's common layer.
type CommonLayerDiscovery string

const (
	CommonLayerDiscoveryParent          CommonLayerDiscovery = "parent"
	CommonLayerDiscoveryAncestor        CommonLayerDiscovery = "ancestor"
	CommonLayerDiscoveryOverlayIncludes CommonLayerDiscovery = "overlay_includes"
)

// Placeholder is a DSL's `placeholder` block.
type Placeholder struct {
	Pattern        string
	EscapePrefix   *string
	UnscannedPaths []PathEntry
}

// PathEntry is a path and the keys its wildcard steps skip.
type PathEntry struct {
	Path     string
	SkipKeys []string
}

// FunctionSettings is a DSL's `function` block.
type FunctionSettings struct {
	Definitions       []string
	CallsNotAllowedAt []PathEntry
	MarkerFunction    *MarkerFunction
}

// MarkerFunction is how a key writes a call.
type MarkerFunction struct {
	CallMarker              string
	SplatOperator           *string
	CallsWithoutNameAllowed bool
}

// Locals is a DSL's `locals` block.
type Locals struct {
	ScopeName     string
	PathSeparator *string
}

// Loops is a DSL's `loops` block.
type Loops struct {
	ScopeName string
}

// Scope is a scope of a DSL's `scopes`.
type Scope struct {
	Regions                   []string
	LaterItemsOfDeclaringList bool
	NamedByParentKey          bool
	BuiltinNames              []string
}

// DeclarationRule is an entry of a DSL's `declarations`.
type DeclarationRule struct {
	Path              string
	SkipKeys          []string
	ExcludeCandidates []string
	NameSource        NameSource
	KeyToken          *KeyToken
	NameSpelling      NameSpelling
	MetaArgumentName  *string
	DeclaresEveryName bool
	ScopeName         string
}

// NameSource is where a declaration rule reads a name.
type NameSource string

const (
	NameSourceKey          NameSource = "key"
	NameSourceValue        NameSource = "value"
	NameSourceMetaArgument NameSource = "meta_argument"
)

// KeyToken is which words of a key a declaration rule reads.
type KeyToken string

const (
	KeyTokenAllWords  KeyToken = "all_words"
	KeyTokenFirstWord KeyToken = "first_word"
	KeyTokenLastWord  KeyToken = "last_word"
)

// NameSpelling is how a declaration rule spells a name.
type NameSpelling string

const (
	NameSpellingAsWritten           NameSpelling = "as_written"
	NameSpellingDashesAsUnderscores NameSpelling = "dashes_as_underscores"
)

// ReferenceRule is an entry of a DSL's `references`.
type ReferenceRule struct {
	Pattern              string
	Positions            []ReferencePosition
	TextAfterNameAllowed bool
	ScopeName            string
	ScopeGroup           *string
	NameGroup            string
}

// ReferencePosition is where a reference rule reads a reference.
type ReferencePosition string

const (
	ReferencePositionWholeScalar       ReferencePosition = "whole_scalar"
	ReferencePositionWholePlaceholder  ReferencePosition = "whole_placeholder"
	ReferencePositionPlaceholderInText ReferencePosition = "placeholder_in_text"
	ReferencePositionAnywhereInScalar  ReferencePosition = "anywhere_in_scalar"
)

// TagKey is a YAML tag that stands for a key.
type TagKey struct {
	Tag string `json:"tag"`
	Key string `json:"key"`
}

// Validator is a DSL's `validator` block.
type Validator struct {
	MergeBaseKeys            []string
	ChildValuesFromParentKey []ChildValueRule
	AllowQuotedValues        []QuotedValueType
}

// QuotedValueType is a type a quoted value may be validated as.
type QuotedValueType string

const (
	QuotedValueBoolean QuotedValueType = "boolean"
	QuotedValueInteger QuotedValueType = "integer"
	QuotedValueNumber  QuotedValueType = "number"
)

// ChildValueRule is an entry of a validator's `child_values_from_parent_key`.
type ChildValueRule struct {
	Pattern string
}

var (
	discoveryVariants         = []string{"parent", "ancestor", "overlay_includes"}
	nameSourceVariants        = []string{"key", "value", "meta_argument"}
	nameSpellingVariants      = []string{"as_written", "dashes_as_underscores"}
	keyTokenVariants          = []string{"all_words", "first_word", "last_word"}
	referencePositionVariants = []string{"whole_scalar", "whole_placeholder", "placeholder_in_text", "anywhere_in_scalar"}
	quotedTypeVariants        = []string{"boolean", "integer", "number"}
)

// DSLDefinitionFromJSON reads a DSL block, each field with its default, or says why it is none.
func DSLDefinitionFromJSON(dslBlock any) (DSLDefinition, error) {
	blockReader := &fieldReader{}
	blockFields := blockReader.objectOf(dslBlock, "")
	dslDefinition := DSLDefinition{
		ID:                blockReader.requiredText(blockFields, "id"),
		FileIncludes:      blockReader.requiredTexts(blockFields, "file_includes"),
		FileExcludes:      blockReader.textList(blockFields, "file_excludes"),
		SchemaSearchPaths: blockReader.textList(blockFields, "schema_search_paths"),
		Layers:            blockReader.layersOf(blockFields),
		Placeholder:       blockReader.placeholderOf(blockFields),
		Function:          blockReader.functionSettingsOf(blockFields),
		Locals:            blockReader.localsOf(blockFields),
		Loops:             blockReader.loopsOf(blockFields),
		Scopes:            blockReader.scopesOf(blockFields),
		Declarations:      blockReader.declarationsOf(blockFields),
		References:        blockReader.referencesOf(blockFields),
		TagKeys:           blockReader.tagKeysOf(blockFields),
		Validator:         blockReader.validatorOf(blockFields),
	}
	if blockReader.firstFailure != nil {
		return DSLDefinition{}, blockReader.firstFailure
	}
	return dslDefinition, nil
}

type fieldReader struct {
	firstFailure error
}

func (blockReader *fieldReader) localsOf(blockFields map[string]any) *Locals {
	localsFields, isPresent := blockReader.optionalObject(blockFields, "locals")
	if !isPresent {
		return nil
	}
	return &Locals{
		ScopeName:     blockReader.requiredText(localsFields, "scope_name"),
		PathSeparator: blockReader.optionalText(localsFields, "path_separator"),
	}
}

func (blockReader *fieldReader) loopsOf(blockFields map[string]any) *Loops {
	loopsFields, isPresent := blockReader.optionalObject(blockFields, "loops")
	if !isPresent {
		return nil
	}
	return &Loops{ScopeName: blockReader.requiredText(loopsFields, "scope_name")}
}

func (blockReader *fieldReader) layersOf(blockFields map[string]any) *Layers {
	layersFields, isPresent := blockReader.optionalObject(blockFields, "layers")
	if !isPresent {
		return nil
	}
	discoveryText := blockReader.requiredVariant(layersFields, "common_layer_discovery", discoveryVariants)
	return &Layers{
		OverlayFolders:       blockReader.textList(layersFields, "overlay_folders"),
		CommonLayerDiscovery: CommonLayerDiscovery(discoveryText),
		CommonLayerFiles:     blockReader.textLists(layersFields, "common_layer_files"),
	}
}

func (blockReader *fieldReader) placeholderOf(blockFields map[string]any) *Placeholder {
	placeholderFields, isPresent := blockReader.optionalObject(blockFields, "placeholder")
	if !isPresent {
		return nil
	}
	return &Placeholder{
		Pattern:        blockReader.requiredText(placeholderFields, "pattern"),
		EscapePrefix:   blockReader.optionalText(placeholderFields, "escape_prefix"),
		UnscannedPaths: blockReader.pathEntries(placeholderFields, "unscanned_paths"),
	}
}

func (blockReader *fieldReader) functionSettingsOf(blockFields map[string]any) *FunctionSettings {
	functionFields, isPresent := blockReader.optionalObject(blockFields, "function")
	if !isPresent {
		return nil
	}
	functionSettings := &FunctionSettings{
		Definitions:       blockReader.textList(functionFields, "definitions"),
		CallsNotAllowedAt: blockReader.pathEntries(functionFields, "calls_not_allowed_at"),
	}
	if markerFields, isMarked := blockReader.optionalObject(functionFields, "marker_function"); isMarked {
		functionSettings.MarkerFunction = &MarkerFunction{
			CallMarker:              blockReader.requiredText(markerFields, "call_marker"),
			SplatOperator:           blockReader.optionalText(markerFields, "splat_operator"),
			CallsWithoutNameAllowed: blockReader.requiredFlag(markerFields, "calls_without_name_allowed"),
		}
	}
	return functionSettings
}

func (blockReader *fieldReader) scopesOf(blockFields map[string]any) map[string]Scope {
	declaredScopes := map[string]Scope{}
	for scopeName, scopeValue := range blockReader.objectFields(blockFields, "scopes") {
		scopeFields := blockReader.objectOf(scopeValue, "scopes")
		declaredScopes[scopeName] = Scope{
			Regions:                   blockReader.textList(scopeFields, "regions"),
			LaterItemsOfDeclaringList: blockReader.optionalFlag(scopeFields, "later_items_of_declaring_list"),
			NamedByParentKey:          blockReader.optionalFlag(scopeFields, "named_by_parent_key"),
			BuiltinNames:              blockReader.textList(scopeFields, "builtin_names"),
		}
	}
	return declaredScopes
}

func (blockReader *fieldReader) declarationsOf(blockFields map[string]any) []DeclarationRule {
	declarationRules := []DeclarationRule{}
	for _, ruleValue := range blockReader.listField(blockFields, "declarations") {
		ruleFields := blockReader.objectOf(ruleValue, "declarations")
		declarationRule := DeclarationRule{
			Path:              blockReader.requiredText(ruleFields, "path"),
			SkipKeys:          blockReader.textList(ruleFields, "skip_keys"),
			ExcludeCandidates: blockReader.textList(ruleFields, "exclude_candidates"),
			NameSource:        NameSource(blockReader.requiredVariant(ruleFields, "name_source", nameSourceVariants)),
			NameSpelling:      NameSpelling(blockReader.requiredVariant(ruleFields, "name_spelling", nameSpellingVariants)),
			MetaArgumentName:  blockReader.optionalText(ruleFields, "meta_argument_name"),
			DeclaresEveryName: blockReader.optionalFlag(ruleFields, "declares_every_name"),
			ScopeName:         blockReader.requiredText(ruleFields, "scope_name"),
		}
		if tokenText := blockReader.optionalVariant(ruleFields, "key_token", keyTokenVariants); tokenText != nil {
			keyToken := KeyToken(*tokenText)
			declarationRule.KeyToken = &keyToken
		}
		declarationRules = append(declarationRules, declarationRule)
	}
	return declarationRules
}

func (blockReader *fieldReader) referencesOf(blockFields map[string]any) []ReferenceRule {
	referenceRules := []ReferenceRule{}
	for _, ruleValue := range blockReader.listField(blockFields, "references") {
		ruleFields := blockReader.objectOf(ruleValue, "references")
		referenceRule := ReferenceRule{
			Pattern:              blockReader.requiredText(ruleFields, "pattern"),
			TextAfterNameAllowed: blockReader.requiredFlag(ruleFields, "text_after_name_allowed"),
			ScopeName:            blockReader.requiredText(ruleFields, "scope_name"),
			ScopeGroup:           blockReader.optionalText(ruleFields, "scope_group"),
			NameGroup:            blockReader.requiredText(ruleFields, "name_group"),
			Positions:            []ReferencePosition{},
		}
		positionValues, isPresent := ruleFields["positions"]
		if !isPresent {
			blockReader.failWith("missing field `positions`")
		}
		for _, positionValue := range blockReader.listOf(positionValues, "positions") {
			positionText := blockReader.variantOf(positionValue, "positions", referencePositionVariants)
			referenceRule.Positions = append(referenceRule.Positions, ReferencePosition(positionText))
		}
		referenceRules = append(referenceRules, referenceRule)
	}
	return referenceRules
}

func (blockReader *fieldReader) tagKeysOf(blockFields map[string]any) []TagKey {
	tagKeys := []TagKey{}
	for _, entryValue := range blockReader.listField(blockFields, "tag_keys") {
		entryFields := blockReader.objectOf(entryValue, "tag_keys")
		tagKeys = append(tagKeys, TagKey{
			Tag: blockReader.requiredText(entryFields, "tag"),
			Key: blockReader.requiredText(entryFields, "key"),
		})
	}
	return tagKeys
}

func (blockReader *fieldReader) validatorOf(blockFields map[string]any) *Validator {
	validatorFields, isPresent := blockReader.optionalObject(blockFields, "validator")
	if !isPresent {
		return nil
	}
	readValidator := &Validator{
		MergeBaseKeys:            blockReader.textList(validatorFields, "merge_base_keys"),
		ChildValuesFromParentKey: []ChildValueRule{},
		AllowQuotedValues:        []QuotedValueType{},
	}
	for _, ruleValue := range blockReader.listField(validatorFields, "child_values_from_parent_key") {
		ruleFields := blockReader.objectOf(ruleValue, "child_values_from_parent_key")
		childRule := ChildValueRule{Pattern: blockReader.requiredText(ruleFields, "pattern")}
		readValidator.ChildValuesFromParentKey = append(readValidator.ChildValuesFromParentKey, childRule)
	}
	for _, typeValue := range blockReader.listField(validatorFields, "allow_quoted_values") {
		quotedType := blockReader.variantOf(typeValue, "allow_quoted_values", quotedTypeVariants)
		readValidator.AllowQuotedValues = append(readValidator.AllowQuotedValues, QuotedValueType(quotedType))
	}
	return readValidator
}

func (blockReader *fieldReader) failWith(failureMessage string) {
	if blockReader.firstFailure == nil {
		blockReader.firstFailure = fmt.Errorf("%s", failureMessage)
	}
}

func (blockReader *fieldReader) objectOf(fieldValue any, fieldName string) map[string]any {
	objectFields, isObject := fieldValue.(map[string]any)
	if isObject {
		return objectFields
	}
	if fieldName == "" {
		blockReader.failWith("invalid type: expected a map")
	} else {
		blockReader.failWith("invalid type for `" + fieldName + "`: expected a map")
	}
	return map[string]any{}
}

func (blockReader *fieldReader) optionalObject(holderFields map[string]any, fieldName string) (map[string]any, bool) {
	fieldValue, isPresent := holderFields[fieldName]
	if !isPresent || fieldValue == nil {
		return nil, false
	}
	return blockReader.objectOf(fieldValue, fieldName), true
}

func (blockReader *fieldReader) objectFields(holderFields map[string]any, fieldName string) map[string]any {
	fieldValue, isPresent := holderFields[fieldName]
	if !isPresent {
		return map[string]any{}
	}
	return blockReader.objectOf(fieldValue, fieldName)
}

func (blockReader *fieldReader) textOf(fieldValue any, fieldName string) string {
	fieldText, isText := fieldValue.(string)
	if !isText {
		blockReader.failWith("invalid type for `" + fieldName + "`: expected a string")
	}
	return fieldText
}

func (blockReader *fieldReader) requiredText(holderFields map[string]any, fieldName string) string {
	fieldValue, isPresent := holderFields[fieldName]
	if !isPresent {
		blockReader.failWith("missing field `" + fieldName + "`")
		return ""
	}
	return blockReader.textOf(fieldValue, fieldName)
}

func (blockReader *fieldReader) optionalText(holderFields map[string]any, fieldName string) *string {
	fieldValue, isPresent := holderFields[fieldName]
	if !isPresent || fieldValue == nil {
		return nil
	}
	fieldText := blockReader.textOf(fieldValue, fieldName)
	return &fieldText
}

func (blockReader *fieldReader) flagOf(fieldValue any, fieldName string) bool {
	fieldFlag, isFlag := fieldValue.(bool)
	if !isFlag {
		blockReader.failWith("invalid type for `" + fieldName + "`: expected a boolean")
	}
	return fieldFlag
}

func (blockReader *fieldReader) requiredFlag(holderFields map[string]any, fieldName string) bool {
	fieldValue, isPresent := holderFields[fieldName]
	if !isPresent {
		blockReader.failWith("missing field `" + fieldName + "`")
		return false
	}
	return blockReader.flagOf(fieldValue, fieldName)
}

func (blockReader *fieldReader) optionalFlag(holderFields map[string]any, fieldName string) bool {
	fieldValue, isPresent := holderFields[fieldName]
	if !isPresent {
		return false
	}
	return blockReader.flagOf(fieldValue, fieldName)
}

func (blockReader *fieldReader) listOf(fieldValue any, fieldName string) []any {
	listItems, isList := fieldValue.([]any)
	if !isList {
		blockReader.failWith("invalid type for `" + fieldName + "`: expected a sequence")
	}
	return listItems
}

func (blockReader *fieldReader) listField(holderFields map[string]any, fieldName string) []any {
	fieldValue, isPresent := holderFields[fieldName]
	if !isPresent {
		return nil
	}
	return blockReader.listOf(fieldValue, fieldName)
}

func (blockReader *fieldReader) textList(holderFields map[string]any, fieldName string) []string {
	listTexts := []string{}
	for _, listItem := range blockReader.listField(holderFields, fieldName) {
		listTexts = append(listTexts, blockReader.textOf(listItem, fieldName))
	}
	return listTexts
}

func (blockReader *fieldReader) requiredTexts(holderFields map[string]any, fieldName string) []string {
	if _, isPresent := holderFields[fieldName]; !isPresent {
		blockReader.failWith("missing field `" + fieldName + "`")
		return []string{}
	}
	return blockReader.textList(holderFields, fieldName)
}

func (blockReader *fieldReader) textLists(holderFields map[string]any, fieldName string) map[string][]string {
	namedLists := map[string][]string{}
	for listKey, listValue := range blockReader.objectFields(holderFields, fieldName) {
		listTexts := []string{}
		for _, listItem := range blockReader.listOf(listValue, fieldName) {
			listTexts = append(listTexts, blockReader.textOf(listItem, fieldName))
		}
		namedLists[listKey] = listTexts
	}
	return namedLists
}

func (blockReader *fieldReader) pathEntries(holderFields map[string]any, fieldName string) []PathEntry {
	pathEntries := []PathEntry{}
	for _, entryValue := range blockReader.listField(holderFields, fieldName) {
		entryFields := blockReader.objectOf(entryValue, fieldName)
		pathEntries = append(pathEntries, PathEntry{
			Path:     blockReader.requiredText(entryFields, "path"),
			SkipKeys: blockReader.textList(entryFields, "skip_keys"),
		})
	}
	return pathEntries
}

func (blockReader *fieldReader) variantOf(fieldValue any, fieldName string, knownVariants []string) string {
	variantText, isText := fieldValue.(string)
	for _, knownVariant := range knownVariants {
		if isText && variantText == knownVariant {
			return variantText
		}
	}
	listedVariants := make([]string, 0, len(knownVariants))
	for _, knownVariant := range knownVariants {
		listedVariants = append(listedVariants, "`"+knownVariant+"`")
	}
	blockReader.failWith(fmt.Sprintf("unknown variant `%v` for `%s`, expected one of %s",
		fieldValue, fieldName, strings.Join(listedVariants, ", ")))
	return ""
}

func (blockReader *fieldReader) requiredVariant(holderFields map[string]any, fieldName string, knownVariants []string) string {
	fieldValue, isPresent := holderFields[fieldName]
	if !isPresent {
		blockReader.failWith("missing field `" + fieldName + "`")
		return ""
	}
	return blockReader.variantOf(fieldValue, fieldName, knownVariants)
}

func (blockReader *fieldReader) optionalVariant(holderFields map[string]any, fieldName string, knownVariants []string) *string {
	fieldValue, isPresent := holderFields[fieldName]
	if !isPresent || fieldValue == nil {
		return nil
	}
	variantText := blockReader.variantOf(fieldValue, fieldName, knownVariants)
	return &variantText
}
