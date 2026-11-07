package yamldslspec

import (
	"math"
	"strconv"
)

// NodeKind is what a node holds.
type NodeKind int

const (
	NodeNull NodeKind = iota
	NodeBool
	NodeNumber
	NodeString
	NodeList
	NodeMap
)

// Node is a YAML value as written: its kind, its value and its position.
type Node struct {
	Kind     NodeKind
	Position Position
	Flag     bool
	Text     string
	Items    []Node
	Entries  []NodeEntry
}

// NodeEntry is a map's key, where the key was written, and its value.
type NodeEntry struct {
	Key         string
	KeyPosition Position
	Value       Node
}

// Get is the value of a key in a map node.
func (yamlNode Node) Get(entryKey string) (Node, bool) {
	if yamlNode.Kind != NodeMap {
		return Node{}, false
	}
	for _, mapEntry := range yamlNode.Entries {
		if mapEntry.Key == entryKey {
			return mapEntry.Value, true
		}
	}
	return Node{}, false
}

// AsString is the text of a string node.
func (yamlNode Node) AsString() (string, bool) {
	if yamlNode.Kind != NodeString {
		return "", false
	}
	return yamlNode.Text, true
}

// IsMap tells whether the node is a map.
func (yamlNode Node) IsMap() bool {
	return yamlNode.Kind == NodeMap
}

// ScalarText is a scalar as text, for splicing into a longer string.
func (yamlNode Node) ScalarText() (string, bool) {
	switch yamlNode.Kind {
	case NodeNull:
		return "", true
	case NodeBool:
		return strconv.FormatBool(yamlNode.Flag), true
	case NodeNumber, NodeString:
		return yamlNode.Text, true
	}
	return "", false
}

// ToJSON is the node as a JSON value, a number by NumberJSON.
func (yamlNode Node) ToJSON() any {
	switch yamlNode.Kind {
	case NodeBool:
		return yamlNode.Flag
	case NodeNumber:
		return NumberJSON(yamlNode.Text)
	case NodeString:
		return yamlNode.Text
	case NodeList:
		listItems := make([]any, 0, len(yamlNode.Items))
		for _, listItem := range yamlNode.Items {
			listItems = append(listItems, listItem.ToJSON())
		}
		return listItems
	case NodeMap:
		mapFields := make(map[string]any, len(yamlNode.Entries))
		for _, mapEntry := range yamlNode.Entries {
			mapFields[mapEntry.Key] = mapEntry.Value.ToJSON()
		}
		return mapFields
	}
	return nil
}

// NumberJSON is a number's text as JSON: an int64, a uint64 or a finite float64, else the text.
func NumberJSON(numberText string) any {
	if signedInteger, signedError := strconv.ParseInt(numberText, 10, 64); signedError == nil {
		return signedInteger
	}
	if unsignedInteger, unsignedError := strconv.ParseUint(numberText, 10, 64); unsignedError == nil {
		return unsignedInteger
	}
	if !isDecimalText(numberText) {
		return numberText
	}
	floatValue, floatError := strconv.ParseFloat(numberText, 64)
	if floatError != nil || math.IsInf(floatValue, 0) || math.IsNaN(floatValue) {
		return numberText
	}
	return floatValue
}

func isDecimalText(numberText string) bool {
	unsignedText := numberText
	if len(unsignedText) > 0 && (unsignedText[0] == '+' || unsignedText[0] == '-') {
		unsignedText = unsignedText[1:]
	}
	digitsSeen := false
	for byteIndex := 0; byteIndex < len(unsignedText); byteIndex++ {
		textByte := unsignedText[byteIndex]
		switch {
		case textByte >= '0' && textByte <= '9':
			digitsSeen = true
		case textByte == '.' || textByte == 'e' || textByte == 'E' || textByte == '+' || textByte == '-':
		default:
			return false
		}
	}
	return digitsSeen
}
