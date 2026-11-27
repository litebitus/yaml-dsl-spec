package yamldslspec

import "github.com/litebitus/yaml-dsl-spec/go/core"

// The types of package core, read as it defines them.
type (
	CallKey                  = core.CallKey
	ChildValueRule           = core.ChildValueRule
	CommonLayerDiscovery     = core.CommonLayerDiscovery
	CompiledPattern          = core.CompiledPattern
	DSLDefinition            = core.DSLDefinition
	DateFormat               = core.DateFormat
	DateFormatPart           = core.DateFormatPart
	DeclarationRule          = core.DeclarationRule
	DeclaredName             = core.DeclaredName
	Diagnostic               = core.Diagnostic
	DiagnosticNotes          = core.DiagnosticNotes
	FunctionArgument         = core.FunctionArgument
	FunctionCall             = core.FunctionCall
	FunctionEntry            = core.FunctionEntry
	FunctionSettings         = core.FunctionSettings
	FunctionTable            = core.FunctionTable
	KeyToken                 = core.KeyToken
	Layers                   = core.Layers
	LayoutPart               = core.LayoutPart
	LayoutVerb               = core.LayoutVerb
	Locals                   = core.Locals
	Loop                     = core.Loop
	LoopBody                 = core.LoopBody
	LoopEntry                = core.LoopEntry
	Loops                    = core.Loops
	MacroEntry               = core.MacroEntry
	MacroKind                = core.MacroKind
	MapEntry                 = core.MapEntry
	MarkerFunction           = core.MarkerFunction
	MergeBehavior            = core.MergeBehavior
	NameSource               = core.NameSource
	NameSpelling             = core.NameSpelling
	Node                     = core.Node
	NodeEntry                = core.NodeEntry
	NodeKind                 = core.NodeKind
	PathEntry                = core.PathEntry
	PathPattern              = core.PathPattern
	PathSet                  = core.PathSet
	PathStep                 = core.PathStep
	PatternMatch             = core.PatternMatch
	Placeholder              = core.Placeholder
	Position                 = core.Position
	QuotedValueType          = core.QuotedValueType
	Reference                = core.Reference
	ReferencePosition        = core.ReferencePosition
	ReferenceRule            = core.ReferenceRule
	ReferenceTarget          = core.ReferenceTarget
	ResolvedKind             = core.ResolvedKind
	ResolvedValue            = core.ResolvedValue
	Scope                    = core.Scope
	SelectedValue            = core.SelectedValue
	Severity                 = core.Severity
	TagKey                   = core.TagKey
	TargetKind               = core.TargetKind
	TemplateAction           = core.TemplateAction
	TemplateBlock            = core.TemplateBlock
	TemplateBranch           = core.TemplateBranch
	TemplateConditional      = core.TemplateConditional
	TemplateConditionalWords = core.TemplateConditionalWords
	TemplateDefinition       = core.TemplateDefinition
	TemplateDelimiters       = core.TemplateDelimiters
	TemplateLoop             = core.TemplateLoop
	TemplateLoopSyntax       = core.TemplateLoopSyntax
	TemplateLoopVariable     = core.TemplateLoopVariable
	TemplatePart             = core.TemplatePart
	TemplateReading          = core.TemplateReading
	TemplateVariable         = core.TemplateVariable
	Validator                = core.Validator
)

// The constants of package core, read as it defines them.
const (
	CommonLayerDiscoveryAncestor        = core.CommonLayerDiscoveryAncestor
	CommonLayerDiscoveryOverlayIncludes = core.CommonLayerDiscoveryOverlayIncludes
	CommonLayerDiscoveryParent          = core.CommonLayerDiscoveryParent
	FunctionTablePointer                = core.FunctionTablePointer
	KeyTokenAllWords                    = core.KeyTokenAllWords
	KeyTokenFirstWord                   = core.KeyTokenFirstWord
	KeyTokenLastWord                    = core.KeyTokenLastWord
	MacroKindFunctionCall               = core.MacroKindFunctionCall
	MacroKindLoop                       = core.MacroKindLoop
	MacroKindMerge                      = core.MacroKindMerge
	MacroKindTransform                  = core.MacroKindTransform
	MergeBehaviorDeep                   = core.MergeBehaviorDeep
	MergeBehaviorJoin                   = core.MergeBehaviorJoin
	MergeBehaviorShallow                = core.MergeBehaviorShallow
	NameSourceKey                       = core.NameSourceKey
	NameSourceMetaArgument              = core.NameSourceMetaArgument
	NameSourceValue                     = core.NameSourceValue
	NameSpellingAsWritten               = core.NameSpellingAsWritten
	NameSpellingDashesAsUnderscores     = core.NameSpellingDashesAsUnderscores
	NodeBool                            = core.NodeBool
	NodeList                            = core.NodeList
	NodeMap                             = core.NodeMap
	NodeNull                            = core.NodeNull
	NodeNumber                          = core.NodeNumber
	NodeString                          = core.NodeString
	QuotedValueBoolean                  = core.QuotedValueBoolean
	QuotedValueInteger                  = core.QuotedValueInteger
	QuotedValueNumber                   = core.QuotedValueNumber
	ReferencePositionAnywhereInScalar   = core.ReferencePositionAnywhereInScalar
	ReferencePositionPlaceholderInText  = core.ReferencePositionPlaceholderInText
	ReferencePositionWholePlaceholder   = core.ReferencePositionWholePlaceholder
	ReferencePositionWholeScalar        = core.ReferencePositionWholeScalar
	ResolvedBool                        = core.ResolvedBool
	ResolvedCall                        = core.ResolvedCall
	ResolvedList                        = core.ResolvedList
	ResolvedLoop                        = core.ResolvedLoop
	ResolvedMap                         = core.ResolvedMap
	ResolvedNull                        = core.ResolvedNull
	ResolvedNumber                      = core.ResolvedNumber
	ResolvedReference                   = core.ResolvedReference
	ResolvedString                      = core.ResolvedString
	ResolvedTemplate                    = core.ResolvedTemplate
	SeverityError                       = core.SeverityError
	SeverityWarning                     = core.SeverityWarning
	TargetBuiltin                       = core.TargetBuiltin
	TargetDeclaration                   = core.TargetDeclaration
	TargetLocal                         = core.TargetLocal
	TargetLocalUnknown                  = core.TargetLocalUnknown
	TargetLoopVariable                  = core.TargetLoopVariable
	TargetUndeclared                    = core.TargetUndeclared
)

// CodeSpan is core.CodeSpan.
func CodeSpan(spanText string) string {
	return core.CodeSpan(spanText)
}

// CompilePattern is core.CompilePattern.
func CompilePattern(patternText string) (*CompiledPattern, error) {
	return core.CompilePattern(patternText)
}

// DSLDefinitionFromJSON is core.DSLDefinitionFromJSON.
func DSLDefinitionFromJSON(dslBlock any) (DSLDefinition, error) {
	return core.DSLDefinitionFromJSON(dslBlock)
}

// DiagnosticAt is core.DiagnosticAt.
func DiagnosticAt(problemPosition Position, problemMessage string) Diagnostic {
	return core.DiagnosticAt(problemPosition, problemMessage)
}

// FileStartPosition is core.FileStartPosition.
func FileStartPosition(fileName string) Position {
	return core.FileStartPosition(fileName)
}

// FormatLayout is core.FormatLayout.
func FormatLayout(layout string, operands []any) (string, error) {
	return core.FormatLayout(layout, operands)
}

// FunctionTableFromSchema is core.FunctionTableFromSchema.
func FunctionTableFromSchema(documentSchema any) (FunctionTable, error) {
	return core.FunctionTableFromSchema(documentSchema)
}

// IndexStep is core.IndexStep.
func IndexStep(listIndex int) PathStep {
	return core.IndexStep(listIndex)
}

// KeyStep is core.KeyStep.
func KeyStep(mapKey string) PathStep {
	return core.KeyStep(mapKey)
}

// NewPosition is core.NewPosition.
func NewPosition(fileName string, lineNumber, columnNumber int) Position {
	return core.NewPosition(fileName, lineNumber, columnNumber)
}

// NumberJSON is core.NumberJSON.
func NumberJSON(numberText string) any {
	return core.NumberJSON(numberText)
}

// ParseCallKey is core.ParseCallKey.
func ParseCallKey(entryKey string, markerFunction MarkerFunction) (CallKey, bool) {
	return core.ParseCallKey(entryKey, markerFunction)
}

// ParseLayout is core.ParseLayout.
func ParseLayout(layout string) ([]LayoutPart, error) {
	return core.ParseLayout(layout)
}

// ParsePathPattern is core.ParsePathPattern.
func ParsePathPattern(pathText string, skipKeys []string) (PathPattern, *Diagnostic) {
	return core.ParsePathPattern(pathText, skipKeys)
}

// ParsePathSet is core.ParsePathSet.
func ParsePathSet(pathEntries []PathEntry) (PathSet, *Diagnostic) {
	return core.ParsePathSet(pathEntries)
}

// ParseYAML is core.ParseYAML.
func ParseYAML(fileName, fileText string, tagKeys []TagKey) (Node, *Diagnostic) {
	return core.ParseYAML(fileName, fileText, tagKeys)
}

// ReadTemplate is core.ReadTemplate.
func ReadTemplate(fileName, templateText string, templateDefinition TemplateDefinition) TemplateReading {
	return core.ReadTemplate(fileName, templateText, templateDefinition)
}

// ResolvedValueFromNode is core.ResolvedValueFromNode.
func ResolvedValueFromNode(yamlNode Node) ResolvedValue {
	return core.ResolvedValueFromNode(yamlNode)
}

// StringValue is core.StringValue.
func StringValue(stringText string, valuePosition Position) ResolvedValue {
	return core.StringValue(stringText, valuePosition)
}

// TemplateDefinitionFromJSON is core.TemplateDefinitionFromJSON.
func TemplateDefinitionFromJSON(templateBlock any) (TemplateDefinition, error) {
	return core.TemplateDefinitionFromJSON(templateBlock)
}

// UnplacedDiagnostic is core.UnplacedDiagnostic.
func UnplacedDiagnostic(problemMessage string) Diagnostic {
	return core.UnplacedDiagnostic(problemMessage)
}

// WarningDiagnosticAt is core.WarningDiagnosticAt.
func WarningDiagnosticAt(warningPosition Position, warningMessage string) Diagnostic {
	return core.WarningDiagnosticAt(warningPosition, warningMessage)
}
