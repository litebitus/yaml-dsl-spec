package yamldslspec

import (
	"errors"
	"io"
	"strings"

	"go.yaml.in/yaml/v4"
)

// ParseYAML reads the first document of a text, a node carrying a listed tag read as a map holding its key.
func ParseYAML(fileName, fileText string, tagKeys []TagKey) (Node, *Diagnostic) {
	rootNode, firstProblem, syntaxLine := parseFirstDocument(fileName, fileText, tagKeys)
	textLines := strings.SplitAfter(fileText, "\n")
	for cutLine := syntaxLine; cutLine > 1; {
		textBefore := strings.Join(textLines[:cutLine-1], "")
		_, earlierProblem, earlierSyntaxLine := parseFirstDocument(fileName, textBefore, tagKeys)
		if earlierProblem != nil && earlierSyntaxLine == 0 {
			return Node{}, earlierProblem
		}
		if earlierSyntaxLine == 0 || earlierSyntaxLine >= cutLine {
			break
		}
		cutLine = earlierSyntaxLine
	}
	return rootNode, firstProblem
}

func parseFirstDocument(fileName, fileText string, tagKeys []TagKey) (Node, *Diagnostic, int) {
	readableText := withVersionDirectivesRead(fileText)
	yamlDecoder := yaml.NewDecoder(strings.NewReader(readableText))
	var documentNode yaml.Node
	if decodeError := yamlDecoder.Decode(&documentNode); decodeError != nil {
		if errors.Is(decodeError, io.EOF) {
			return Node{Kind: NodeNull, Position: FileStartPosition(fileName)}, nil, 0
		}
		syntaxProblem, syntaxLine := yamlProblem(fileName, decodeError)
		return Node{}, &syntaxProblem, syntaxLine
	}
	if documentNode.Kind != yaml.DocumentNode || len(documentNode.Content) == 0 {
		return Node{Kind: NodeNull, Position: FileStartPosition(fileName)}, nil, 0
	}
	treeBuilder := nodeBuilder{
		fileName:      fileName,
		tagKeys:       tagKeys,
		anchoredNodes: map[*yaml.Node]Node{},
		textLines:     runeLines(readableText),
	}
	rootNode, treeProblem := treeBuilder.convertNode(documentNode.Content[0], nodeContext{parentIndent: -1})
	return rootNode, treeProblem, 0
}

func withVersionDirectivesRead(fileText string) string {
	textLines := strings.SplitAfter(fileText, "\n")
	for lineIndex, lineText := range textLines {
		trimmedLine := strings.TrimRight(lineText, "\r\n")
		if strings.HasPrefix(trimmedLine, "%YAML ") || strings.HasPrefix(trimmedLine, "%YAML\t") {
			textLines[lineIndex] = "%YAML 1.1" + lineText[len(trimmedLine):]
			continue
		}
		contentText := strings.TrimSpace(trimmedLine)
		if !strings.HasPrefix(trimmedLine, "%") && contentText != "" && !strings.HasPrefix(contentText, "#") {
			break
		}
	}
	return strings.Join(textLines, "")
}

func runeLines(fileText string) [][]rune {
	var textLines [][]rune
	for _, lineText := range strings.Split(fileText, "\n") {
		textLines = append(textLines, []rune(strings.TrimSuffix(lineText, "\r")))
	}
	return textLines
}

func yamlProblem(fileName string, decodeError error) (Diagnostic, int) {
	var loadError *yaml.LoadError
	if !errors.As(decodeError, &loadError) || loadError.Mark.Line < 1 {
		return UnplacedDiagnostic(decodeError.Error()), 0
	}
	problemMessage := loadError.Message
	syntaxLine := loadError.Mark.Line
	if loadError.ContextMsg != "" {
		problemMessage = loadError.ContextMsg + ", " + problemMessage
		if loadError.ContextMark.Line > 0 {
			syntaxLine = min(syntaxLine, loadError.ContextMark.Line)
		}
	}
	problemPosition := NewPosition(fileName, loadError.Mark.Line, max(loadError.Mark.Column, 1))
	return DiagnosticAt(problemPosition, problemMessage), syntaxLine
}

type nodeRole int

const (
	rootRole nodeRole = iota
	mappingKeyRole
	mappingValueRole
	sequenceItemRole
)

type nodeContext struct {
	nodeRole      nodeRole
	parentIndent  int
	blockSequence bool
}

type nodeBuilder struct {
	fileName      string
	tagKeys       []TagKey
	anchoredNodes map[*yaml.Node]Node
	textLines     [][]rune
}

func (treeBuilder nodeBuilder) sourcePosition(sourceNode *yaml.Node) Position {
	return NewPosition(treeBuilder.fileName, sourceNode.Line, sourceNode.Column)
}

func (treeBuilder nodeBuilder) convertNode(sourceNode *yaml.Node, readContext nodeContext) (Node, *Diagnostic) {
	if sourceNode.Kind == yaml.AliasNode {
		if sourceNode.Alias == nil {
			aliasProblem := DiagnosticAt(treeBuilder.sourcePosition(sourceNode), "alias of an anchor not defined before it")
			return Node{}, &aliasProblem
		}
		return treeBuilder.convertNode(sourceNode.Alias, readContext)
	}
	if anchoredNode, isSeen := treeBuilder.anchoredNodes[sourceNode]; isSeen {
		return anchoredNode, nil
	}
	var convertedNode Node
	switch sourceNode.Kind {
	case yaml.ScalarNode:
		convertedNode = treeBuilder.scalarNode(sourceNode, readContext)
	case yaml.SequenceNode:
		listNode, listProblem := treeBuilder.listNode(sourceNode)
		if listProblem != nil {
			return Node{}, listProblem
		}
		convertedNode = listNode
	case yaml.MappingNode:
		mapNode, mapProblem := treeBuilder.mapNode(sourceNode)
		if mapProblem != nil {
			return Node{}, mapProblem
		}
		convertedNode = mapNode
	default:
		convertedNode = Node{Kind: NodeNull, Position: treeBuilder.sourcePosition(sourceNode)}
	}
	convertedNode = treeBuilder.withTagKey(convertedNode, sourceNode)
	if sourceNode.Anchor != "" {
		treeBuilder.anchoredNodes[sourceNode] = convertedNode
	}
	return convertedNode, nil
}

func (treeBuilder nodeBuilder) listNode(sourceNode *yaml.Node) (Node, *Diagnostic) {
	listNode := Node{Kind: NodeList, Position: treeBuilder.sourcePosition(sourceNode), Items: []Node{}}
	itemContext := nodeContext{
		nodeRole:      sequenceItemRole,
		parentIndent:  sourceNode.Column - 1,
		blockSequence: sourceNode.Style&yaml.FlowStyle == 0,
	}
	for _, itemSource := range sourceNode.Content {
		itemNode, itemProblem := treeBuilder.convertNode(itemSource, itemContext)
		if itemProblem != nil {
			return Node{}, itemProblem
		}
		listNode.Items = append(listNode.Items, itemNode)
	}
	return listNode, nil
}

func (treeBuilder nodeBuilder) mapNode(sourceNode *yaml.Node) (Node, *Diagnostic) {
	mapNode := Node{Kind: NodeMap, Position: treeBuilder.sourcePosition(sourceNode), Entries: []NodeEntry{}}
	for pairIndex := 0; pairIndex+1 < len(sourceNode.Content); pairIndex += 2 {
		keySource := sourceNode.Content[pairIndex]
		keyContext := nodeContext{nodeRole: mappingKeyRole, parentIndent: sourceNode.Column - 1}
		keyNode, keyProblem := treeBuilder.convertNode(keySource, keyContext)
		if keyProblem != nil {
			return Node{}, keyProblem
		}
		keyText, isScalar := keyNode.ScalarText()
		if !isScalar || keyNode.Kind == NodeNull {
			scalarProblem := DiagnosticAt(keyNode.Position, "a mapping key must be a scalar")
			return Node{}, &scalarProblem
		}
		for _, earlierEntry := range mapNode.Entries {
			if earlierEntry.Key == keyText {
				duplicateProblem := DiagnosticAt(keyNode.Position, "duplicate key `"+keyText+"`")
				return Node{}, &duplicateProblem
			}
		}
		valueContext := nodeContext{nodeRole: mappingValueRole, parentIndent: keySource.Column - 1}
		valueNode, valueProblem := treeBuilder.convertNode(sourceNode.Content[pairIndex+1], valueContext)
		if valueProblem != nil {
			return Node{}, valueProblem
		}
		mapNode.Entries = append(mapNode.Entries, NodeEntry{Key: keyText, KeyPosition: keyNode.Position, Value: valueNode})
	}
	return mapNode, nil
}

func (treeBuilder nodeBuilder) withTagKey(convertedNode Node, sourceNode *yaml.Node) Node {
	if sourceNode.Style&yaml.TaggedStyle == 0 {
		return convertedNode
	}
	for _, tagKey := range treeBuilder.tagKeys {
		if tagKey.Tag == sourceNode.Tag {
			keyEntry := NodeEntry{Key: tagKey.Key, KeyPosition: convertedNode.Position, Value: convertedNode}
			return Node{Kind: NodeMap, Position: convertedNode.Position, Entries: []NodeEntry{keyEntry}}
		}
	}
	return convertedNode
}

func (treeBuilder nodeBuilder) scalarNode(sourceNode *yaml.Node, readContext nodeContext) Node {
	scalarPosition := treeBuilder.scalarPosition(sourceNode, readContext)
	quotedStyles := yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle | yaml.LiteralStyle | yaml.FoldedStyle
	isExplicitString := sourceNode.Style&yaml.TaggedStyle != 0 &&
		(sourceNode.Tag == "!!str" || sourceNode.Tag == "tag:yaml.org,2002:str")
	if sourceNode.Style&quotedStyles != 0 || isExplicitString {
		return Node{Kind: NodeString, Position: scalarPosition, Text: sourceNode.Value}
	}
	return plainScalarNode(sourceNode.Value, scalarPosition)
}

func (treeBuilder nodeBuilder) scalarPosition(sourceNode *yaml.Node, readContext nodeContext) Position {
	scalarPosition := treeBuilder.sourcePosition(sourceNode)
	hasProperties := sourceNode.Style&yaml.TaggedStyle != 0 || sourceNode.Anchor != ""
	if hasProperties || sourceNode.Line < 1 || sourceNode.Line > len(treeBuilder.textLines) {
		return scalarPosition
	}
	switch {
	case sourceNode.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		return treeBuilder.blockScalarPosition(scalarPosition, readContext)
	case sourceNode.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 || sourceNode.Value != "":
		return scalarPosition
	case readContext.nodeRole == mappingValueRole:
		return treeBuilder.valueIndicatorPosition(scalarPosition)
	case readContext.nodeRole == sequenceItemRole && readContext.blockSequence:
		lineRunes := treeBuilder.textLines[sourceNode.Line-1]
		restText := strings.TrimSpace(string(lineRunes[min(sourceNode.Column-1, len(lineRunes)):]))
		if restText == "" || strings.HasPrefix(restText, "#") {
			scalarPosition.Column = len(lineRunes) + 1
		}
	}
	return scalarPosition
}

func (treeBuilder nodeBuilder) valueIndicatorPosition(scalarPosition Position) Position {
	lineIndex, columnIndex := scalarPosition.Line-1, scalarPosition.Column-2
	for lineIndex >= 0 {
		lineRunes := treeBuilder.textLines[lineIndex]
		columnIndex = min(columnIndex, len(lineRunes)-1)
		for columnIndex >= 0 && (lineRunes[columnIndex] == ' ' || lineRunes[columnIndex] == '\t') {
			columnIndex--
		}
		if columnIndex >= 0 {
			if lineRunes[columnIndex] == ':' {
				return NewPosition(scalarPosition.File, lineIndex+1, columnIndex+1)
			}
			return scalarPosition
		}
		lineIndex--
		columnIndex = len(treeBuilder.textLines[max(lineIndex, 0)]) - 1
	}
	return scalarPosition
}

func (treeBuilder nodeBuilder) blockScalarPosition(indicatorPosition Position, readContext nodeContext) Position {
	indicatorLine := treeBuilder.textLines[indicatorPosition.Line-1]
	explicitIndent := 0
	for _, headerRune := range indicatorLine[min(indicatorPosition.Column, len(indicatorLine)):] {
		if headerRune >= '1' && headerRune <= '9' {
			explicitIndent = int(headerRune - '0')
			break
		}
		if headerRune != '+' && headerRune != '-' {
			break
		}
	}
	for lineIndex := indicatorPosition.Line; lineIndex < len(treeBuilder.textLines); lineIndex++ {
		lineRunes := treeBuilder.textLines[lineIndex]
		leadingSpaces := 0
		for leadingSpaces < len(lineRunes) && lineRunes[leadingSpaces] == ' ' {
			leadingSpaces++
		}
		if leadingSpaces == len(lineRunes) {
			continue
		}
		contentColumn := leadingSpaces + 1
		if explicitIndent > 0 && leadingSpaces > readContext.parentIndent {
			contentColumn = max(readContext.parentIndent, 0) + explicitIndent + 1
		}
		return NewPosition(indicatorPosition.File, lineIndex+1, contentColumn)
	}
	return indicatorPosition
}

func plainScalarNode(scalarText string, scalarPosition Position) Node {
	switch scalarText {
	case "", "~", "null", "Null", "NULL":
		return Node{Kind: NodeNull, Position: scalarPosition}
	case "true", "True", "TRUE":
		return Node{Kind: NodeBool, Position: scalarPosition, Flag: true}
	case "false", "False", "FALSE":
		return Node{Kind: NodeBool, Position: scalarPosition, Flag: false}
	}
	if isCoreNumber(scalarText) {
		return Node{Kind: NodeNumber, Position: scalarPosition, Text: scalarText}
	}
	return Node{Kind: NodeString, Position: scalarPosition, Text: scalarText}
}

func isCoreNumber(scalarText string) bool {
	unsignedText := scalarText
	if len(unsignedText) > 0 && (unsignedText[0] == '-' || unsignedText[0] == '+') {
		unsignedText = unsignedText[1:]
	}
	if unsignedText == "" {
		return false
	}
	if onlyDigits(unsignedText) {
		return true
	}
	mantissaText, exponentText, hasExponent := unsignedText, "", false
	if exponentAt := strings.IndexAny(unsignedText, "eE"); exponentAt >= 0 {
		mantissaText, exponentText, hasExponent = unsignedText[:exponentAt], unsignedText[exponentAt+1:], true
	}
	var isMantissaValid bool
	if wholeText, fractionText, hasFraction := strings.Cut(mantissaText, "."); hasFraction {
		isMantissaValid = (wholeText != "" || fractionText != "") && onlyDigits(wholeText) && onlyDigits(fractionText)
	} else {
		isMantissaValid = mantissaText != "" && onlyDigits(mantissaText)
	}
	isExponentValid := true
	if hasExponent {
		exponentDigits := exponentText
		if len(exponentDigits) > 0 && (exponentDigits[0] == '-' || exponentDigits[0] == '+') {
			exponentDigits = exponentDigits[1:]
		}
		isExponentValid = exponentDigits != "" && onlyDigits(exponentDigits)
	}
	return isMantissaValid && isExponentValid && (strings.Contains(mantissaText, ".") || hasExponent)
}

func onlyDigits(digitText string) bool {
	for byteIndex := 0; byteIndex < len(digitText); byteIndex++ {
		if digitText[byteIndex] < '0' || digitText[byteIndex] > '9' {
			return false
		}
	}
	return true
}
