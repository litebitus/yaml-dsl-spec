package yamldslspec

import "strconv"

// ResolvedKind is what a resolved value holds.
type ResolvedKind int

const (
	ResolvedNull ResolvedKind = iota
	ResolvedBool
	ResolvedNumber
	ResolvedString
	ResolvedList
	ResolvedMap
	ResolvedReference
	ResolvedCall
	ResolvedTemplate
)

// ResolvedValue is a document value with its position: a scalar, a list, a map, or a reference, a call or a template.
type ResolvedValue struct {
	Kind          ResolvedKind
	Position      Position
	Flag          bool
	Text          string
	Items         []ResolvedValue
	Entries       []MapEntry
	Reference     *Reference
	Call          *FunctionCall
	TemplateParts []TemplatePart
}

// MapEntry is a map's key, where the key was written, and its value.
type MapEntry struct {
	Key         string
	KeyPosition Position
	Value       ResolvedValue
}

// Reference is a name read by one of the definition's reference rules.
type Reference struct {
	Scope    string
	ScopeKey *string
	Name     string
	Path     string
	Text     string
	Target   ReferenceTarget
}

// TargetKind is what a reference resolves to.
type TargetKind int

const (
	TargetLocal TargetKind = iota
	TargetLocalUnknown
	TargetBuiltin
	TargetDeclaration
	TargetUndeclared
)

// ReferenceTarget is what a reference resolves to: a local's value or a declaration's position where it has one.
type ReferenceTarget struct {
	Kind                TargetKind
	LocalValue          *ResolvedValue
	DeclarationPosition *Position
}

// FunctionCall is a call left for the target to evaluate.
type FunctionCall struct {
	Function            string
	Arguments           []ResolvedValue
	ExpandFinalArgument bool
}

// TemplatePart is text, or a reference spliced into it.
type TemplatePart struct {
	Text      string
	Reference *Reference
}

// DeclaredName is a name the resolved document declares, by one of the definition's declaration rules.
type DeclaredName struct {
	Scope       string
	ScopeKey    *string
	Name        string
	Key         string
	Path        []PathStep
	KeyPosition Position
	Value       ResolvedValue
}

// ResolvedValueFromNode is the value a node reads to, each value with its position: nothing resolved.
func ResolvedValueFromNode(yamlNode Node) ResolvedValue {
	readValue := ResolvedValue{Position: yamlNode.Position}
	switch yamlNode.Kind {
	case NodeNull:
		readValue.Kind = ResolvedNull
	case NodeBool:
		readValue.Kind = ResolvedBool
		readValue.Flag = yamlNode.Flag
	case NodeNumber:
		readValue.Kind = ResolvedNumber
		readValue.Text = yamlNode.Text
	case NodeString:
		readValue.Kind = ResolvedString
		readValue.Text = yamlNode.Text
	case NodeList:
		readValue.Kind = ResolvedList
		readValue.Items = make([]ResolvedValue, 0, len(yamlNode.Items))
		for _, listItem := range yamlNode.Items {
			readValue.Items = append(readValue.Items, ResolvedValueFromNode(listItem))
		}
	case NodeMap:
		readValue.Kind = ResolvedMap
		readValue.Entries = make([]MapEntry, 0, len(yamlNode.Entries))
		for _, nodeEntry := range yamlNode.Entries {
			readValue.Entries = append(readValue.Entries, MapEntry{
				Key:         nodeEntry.Key,
				KeyPosition: nodeEntry.KeyPosition,
				Value:       ResolvedValueFromNode(nodeEntry.Value),
			})
		}
	}
	return readValue
}

// StringValue is a string at a position.
func StringValue(stringText string, valuePosition Position) ResolvedValue {
	return ResolvedValue{Kind: ResolvedString, Position: valuePosition, Text: stringText}
}

// Get is the value of a key in a map value.
func (resolvedValue ResolvedValue) Get(entryKey string) (ResolvedValue, bool) {
	if resolvedValue.Kind != ResolvedMap {
		return ResolvedValue{}, false
	}
	for _, mapEntry := range resolvedValue.Entries {
		if mapEntry.Key == entryKey {
			return mapEntry.Value, true
		}
	}
	return ResolvedValue{}, false
}

// AsString is the text of a string value.
func (resolvedValue ResolvedValue) AsString() (string, bool) {
	if resolvedValue.Kind != ResolvedString {
		return "", false
	}
	return resolvedValue.Text, true
}

// ScalarText is a scalar's text, for splicing into a longer string.
func (resolvedValue ResolvedValue) ScalarText() (string, bool) {
	switch resolvedValue.Kind {
	case ResolvedNull:
		return "", true
	case ResolvedBool:
		return strconv.FormatBool(resolvedValue.Flag), true
	case ResolvedNumber, ResolvedString:
		return resolvedValue.Text, true
	case ResolvedReference:
		if localValue, isLocal := resolvedValue.localTarget(); isLocal {
			return localValue.ScalarText()
		}
	}
	return "", false
}

// LocalValue is the value with every local reference replaced by the local's value.
func (resolvedValue ResolvedValue) LocalValue() ResolvedValue {
	if localValue, isLocal := resolvedValue.localTarget(); isLocal {
		return localValue.LocalValue()
	}
	return resolvedValue
}

func (resolvedValue ResolvedValue) localTarget() (ResolvedValue, bool) {
	if resolvedValue.Kind != ResolvedReference || resolvedValue.Reference == nil {
		return ResolvedValue{}, false
	}
	referenceTarget := resolvedValue.Reference.Target
	if referenceTarget.Kind != TargetLocal || referenceTarget.LocalValue == nil {
		return ResolvedValue{}, false
	}
	return *referenceTarget.LocalValue, true
}
