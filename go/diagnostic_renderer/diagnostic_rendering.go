// Package diagnostic_renderer writes a YAML DSL standard diagnostic as a compiler writes an error, for a terminal or a log.
package diagnostic_renderer

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	yamldslspec "github.com/litebitus/yaml-dsl-spec/go"
)

// Severity is whether a diagnostic stops its reader or only warns.
type Severity int

const (
	// SeverityError is a diagnostic that stops its reader.
	SeverityError Severity = iota
	// SeverityWarning is a diagnostic that only warns.
	SeverityWarning
)

// DiagnosticSource is the source a diagnostic is about.
type DiagnosticSource struct {
	// Name is the name the `-->` line shows, such as a path.
	Name string
	// Text is the text the diagnostic's position counts lines and columns in; empty shows no source lines.
	Text string
}

// RenderStyle is how a rendering is laid out.
type RenderStyle struct {
	// Color is whether the rendering is painted for a terminal.
	Color bool
	// Width is the terminal width lines divide to; 0 for output that is not a terminal, whose lines are not divided.
	Width int
}

const (
	contextLines  = 2
	gutterColumns = 3
	resetCode     = "\x1b[0m"
	subjectCode   = "\x1b[1m"
	labelCode     = "\x1b[36m"
	attentionCode = "\x1b[33m"
	failureCode   = "\x1b[91m"
	dimCode       = "\x1b[90m"
	schemaNote    = "\x1b[38;2;166;171;242m"
	schemaMark    = "\x1b[1;38;2;166;171;242m"
	schemaPrefix  = "[schema]"
)

func (style RenderStyle) paint(colorCode, paintedText string) string {
	if style.Color {
		return colorCode + paintedText + resetCode
	}
	return paintedText
}

func (style RenderStyle) dividedWidth() *int {
	if style.Width <= 0 {
		return nil
	}
	terminalWidth := style.Width
	return &terminalWidth
}

func (style RenderStyle) headline(severityWord, wordColor, message string) string {
	headlineRows := newWrappedLine(severityWord+": "+message, style.dividedWidth()).rowTexts()
	shownRows := make([]string, 0, len(headlineRows))
	for rowIndex, rowText := range headlineRows {
		if rowIndex > 0 {
			rowWords := strings.TrimLeftFunc(rowText, unicode.IsSpace)
			shownRows = append(shownRows, rowText[:len(rowText)-len(rowWords)]+style.paint(subjectCode, rowWords))
			continue
		}
		afterWord := strings.TrimPrefix(rowText, severityWord)
		afterColon := strings.TrimPrefix(afterWord, ":")
		rowWords := strings.TrimLeftFunc(afterColon, unicode.IsSpace)
		firstRow := style.paint(wordColor, severityWord) + ":"
		if rowWords != "" {
			firstRow += " " + style.paint(subjectCode, rowWords)
		}
		shownRows = append(shownRows, firstRow)
	}
	return strings.Join(shownRows, "\n")
}

// RenderDiagnostic is a diagnostic as a compiler writes an error: the headline, the source's name and the position, the lines that hold the wrong text and the lines it is nested under, and the notes.
func RenderDiagnostic(diagnostic yamldslspec.Diagnostic, severity Severity, source DiagnosticSource, style RenderStyle) string {
	var rendered strings.Builder
	severityWord, severityCode := "error", failureCode
	if severity == SeverityWarning {
		severityWord, severityCode = "warning", attentionCode
	}
	rendered.WriteString(style.headline(severityWord, severityCode, diagnostic.Message))
	diagnosticNotes := diagnostic.Notes
	if diagnostic.Position == nil {
		rendered.WriteString(notesText(diagnosticNotes, 1, style))
		return rendered.String()
	}
	position := *diagnostic.Position
	sourceLines := textLines(source.Text)
	shown := shownLines(sourceLines, position.Line)
	numberWidth := 1
	if len(shown) > 0 {
		numberWidth = len(strconv.Itoa(shown[len(shown)-1]))
	}
	gutter := func(gutterText string) string { return style.paint(dimCode, gutterText) }
	blankGutter := gutter(strings.Repeat(" ", numberWidth) + " |")
	fmt.Fprintf(&rendered, "\n%s%s %s:%d:%d", strings.Repeat(" ", numberWidth), gutter("-->"), source.Name, position.Line, position.Column)
	if len(shown) == 0 {
		rendered.WriteString(notesText(diagnosticNotes, numberWidth, style))
		return rendered.String()
	}
	writeNewRow(&rendered, blankGutter)
	var sourceRoom *int
	if terminalWidth := style.dividedWidth(); terminalWidth != nil {
		roomColumns := max(*terminalWidth-numberWidth-gutterColumns, 0)
		sourceRoom = &roomColumns
	}
	previousNumber := 0
	for _, lineNumber := range shown {
		if previousNumber > 0 && lineNumber > previousNumber+1 {
			writeNewRow(&rendered, gutter("..."))
		}
		line := newWrappedLine(sourceLines[lineNumber-1], sourceRoom)
		isWrongLine := lineNumber == position.Line
		underlinedRow, spanStart, spanEnd := 0, 0, 0
		if isWrongLine {
			underlinedRow, spanStart, spanEnd = underlinedSpan(line, position)
		}
		for rowIndex, row := range line.lineRows {
			rowGutter := blankGutter
			if rowIndex == 0 {
				rowGutter = gutter(fmt.Sprintf("%*d |", numberWidth, lineNumber))
			}
			writeNewRow(&rendered, rowGutter, " ", line.rowText(row))
			if isWrongLine && rowIndex == underlinedRow {
				underlineRows := underline(line, row, spanStart, spanEnd, diagnosticNotes, sourceRoom, severityCode, style)
				for _, underlineRow := range underlineRows {
					writeNewRow(&rendered, blankGutter, " ", underlineRow)
				}
			}
		}
		previousNumber = lineNumber
	}
	writeNewRow(&rendered, blankGutter)
	rendered.WriteString(notesText(diagnosticNotes, numberWidth, style))
	return rendered.String()
}

func writeNewRow(rendered *strings.Builder, rowParts ...string) {
	rendered.WriteByte('\n')
	for _, rowPart := range rowParts {
		rendered.WriteString(rowPart)
	}
}

func textLines(text string) []string {
	if text == "" {
		return nil
	}
	splitLines := strings.Split(text, "\n")
	endsWithNewline := splitLines[len(splitLines)-1] == ""
	if endsWithNewline {
		splitLines = splitLines[:len(splitLines)-1]
	}
	for lineIndex := range splitLines {
		if lineIndex < len(splitLines)-1 || endsWithNewline {
			splitLines[lineIndex] = strings.TrimSuffix(splitLines[lineIndex], "\r")
		}
	}
	return splitLines
}

func shownLines(sourceLines []string, wrongLine int) []int {
	if wrongLine <= 0 || wrongLine > len(sourceLines) {
		return nil
	}
	firstShown := max(wrongLine-contextLines, 1)
	lastShown := min(wrongLine+contextLines, len(sourceLines))
	var shown []int
	for lineNumber := firstShown; lineNumber <= lastShown; lineNumber++ {
		shown = append(shown, lineNumber)
	}
	currentIndent := indentation(sourceLines[wrongLine-1])
	for lineNumber := wrongLine - 1; lineNumber >= 1; lineNumber-- {
		lineText := sourceLines[lineNumber-1]
		trimmedText := strings.TrimLeftFunc(lineText, unicode.IsSpace)
		if strings.TrimSpace(lineText) == "" || strings.HasPrefix(trimmedText, "#") {
			continue
		}
		lineIndent := indentation(lineText)
		if lineIndent < currentIndent {
			if lineNumber < firstShown {
				shown = append(shown, lineNumber)
			}
			currentIndent = lineIndent
			if currentIndent == 0 {
				break
			}
		}
	}
	slices.Sort(shown)
	return shown
}

func indentation(lineText string) int {
	return len(lineText) - len(strings.TrimLeftFunc(lineText, unicode.IsSpace))
}

func underlinedSpan(line wrappedLine, position yamldslspec.Position) (int, int, int) {
	runeCount := len(line.lineRunes)
	spanStart := min(max(position.Column-1, 0), runeCount)
	spanEnd := min(spanStart+max(tokenLength(string(line.lineRunes[spanStart:])), 1), max(runeCount, spanStart+1))
	underlinedRow := 0
	for rowIndex, row := range line.lineRows {
		if row.startIndex < spanEnd {
			underlinedRow = rowIndex
		}
	}
	return underlinedRow, spanStart, spanEnd
}

func underline(line wrappedLine, row wrappedRow, spanStart, spanEnd int, diagnosticNotes *yamldslspec.DiagnosticNotes, sourceRoom *int, severityCode string, style RenderStyle) []string {
	firstIndex := max(spanStart, row.startIndex)
	lastIndex := max(min(spanEnd, row.endIndex), firstIndex)
	caretColumn := row.indentColumns + line.columnsBetween(row.startIndex, firstIndex)
	carets := strings.Repeat("^", max(line.columnsBetween(firstIndex, lastIndex), 1))
	caretIndent := strings.Repeat(" ", caretColumn)
	if diagnosticNotes == nil || diagnosticNotes.Label == nil {
		return []string{caretIndent + style.paint(severityCode, carets)}
	}
	labelText := *diagnosticNotes.Label
	labelColumns := runeColumns([]rune(labelText))
	if sourceRoom == nil || caretColumn+len(carets)+1+labelColumns <= *sourceRoom {
		return []string{caretIndent + style.paint(severityCode, carets+" "+labelText)}
	}
	labelRoom := max(*sourceRoom-caretColumn, 0)
	underlineRows := []string{caretIndent + style.paint(severityCode, carets)}
	for _, labelRow := range newWrappedLine(labelText, &labelRoom).rowTexts() {
		underlineRows = append(underlineRows, caretIndent+style.paint(severityCode, labelRow))
	}
	return underlineRows
}

func tokenLength(restText string) int {
	if strings.HasPrefix(restText, "{{") {
		if closingAt := strings.Index(restText, "}}"); closingAt >= 0 {
			return utf8.RuneCountInString(restText[:closingAt+2])
		}
	}
	if strings.HasPrefix(restText, "\"") || strings.HasPrefix(restText, "'") {
		quoteByte := restText[0]
		isEscaped := false
		for byteIndex, restRune := range restText[1:] {
			switch {
			case isEscaped:
				isEscaped = false
			case restRune == '\\' && quoteByte == '"':
				isEscaped = true
			case restRune == rune(quoteByte):
				return utf8.RuneCountInString(restText[:byteIndex+2])
			}
		}
		return utf8.RuneCountInString(restText)
	}
	tokenEnd := len(restText)
	for _, tokenBoundary := range []string{": ", " #"} {
		if boundaryAt := strings.Index(restText, tokenBoundary); boundaryAt >= 0 {
			tokenEnd = min(tokenEnd, boundaryAt)
		}
	}
	tokenText := strings.TrimRightFunc(restText[:tokenEnd], unicode.IsSpace)
	return utf8.RuneCountInString(strings.TrimSuffix(tokenText, ":"))
}

func notesText(diagnosticNotes *yamldslspec.DiagnosticNotes, numberWidth int, style RenderStyle) string {
	if diagnosticNotes == nil {
		return ""
	}
	noteMarker := strings.Repeat(" ", numberWidth) + " ="
	var renderedNotes strings.Builder
	if diagnosticNotes.Rule != nil {
		renderedNotes.WriteString(namedNote(noteMarker, "rule", *diagnosticNotes.Rule, style))
	}
	if diagnosticNotes.Schema != nil {
		renderedNotes.WriteString(schemaNoteText(noteMarker, *diagnosticNotes.Schema, style))
	}
	if diagnosticNotes.Help != nil {
		renderedNotes.WriteString(namedNote(noteMarker, "help", *diagnosticNotes.Help, style))
	}
	return renderedNotes.String()
}

func firstNoteLine(noteText string) (string, []string) {
	noteLines := textLines(noteText)
	if len(noteLines) == 0 {
		return "", nil
	}
	return noteLines[0], noteLines[1:]
}

func namedNote(noteMarker, noteName, noteText string, style RenderStyle) string {
	firstLine, laterLines := firstNoteLine(noteText)
	noteRows := newWrappedLine(noteMarker+" "+noteName+": "+firstLine, style.dividedWidth()).rowTexts()
	var renderedNote strings.Builder
	for rowIndex, rowText := range noteRows {
		if rowIndex > 0 {
			writeNewRow(&renderedNote, rowText)
			continue
		}
		afterName, hasName := strings.CutPrefix(rowText, noteMarker+" "+noteName+":")
		if !hasName {
			afterName = ""
		}
		writeNewRow(&renderedNote,
			style.paint(dimCode, noteMarker),
			" ",
			style.paint(labelCode, noteName+":"),
			afterName,
		)
	}
	for _, continuedRow := range continuedNoteRows(noteMarker, laterLines, style) {
		writeNewRow(&renderedNote, continuedRow)
	}
	return renderedNote.String()
}

func schemaNoteText(noteMarker, noteText string, style RenderStyle) string {
	firstLine, laterLines := firstNoteLine(noteText)
	noteRows := newWrappedLine(noteMarker+" "+schemaPrefix+" "+firstLine, style.dividedWidth()).rowTexts()
	var renderedNote strings.Builder
	for rowIndex, rowText := range noteRows {
		if rowIndex > 0 {
			writeNewRow(&renderedNote, paintedWords(rowText, style))
			continue
		}
		afterPrefix, hasPrefix := strings.CutPrefix(rowText, noteMarker+" "+schemaPrefix)
		if !hasPrefix {
			afterPrefix = ""
		}
		writeNewRow(&renderedNote,
			style.paint(dimCode, noteMarker),
			" ",
			style.paint(schemaMark, schemaPrefix),
			paintedWords(afterPrefix, style),
		)
	}
	for _, continuedRow := range continuedNoteRows(noteMarker, laterLines, style) {
		writeNewRow(&renderedNote, paintedWords(continuedRow, style))
	}
	return renderedNote.String()
}

func continuedNoteRows(noteMarker string, laterLines []string, style RenderStyle) []string {
	continuationIndent := strings.Repeat(" ", indentation(noteMarker)+levelColumns)
	var continuedRows []string
	for _, laterLine := range laterLines {
		continuedRows = append(continuedRows, newWrappedLine(continuationIndent+laterLine, style.dividedWidth()).rowTexts()...)
	}
	return continuedRows
}

func paintedWords(rowText string, style RenderStyle) string {
	rowWords := strings.TrimLeftFunc(rowText, unicode.IsSpace)
	if rowWords == "" {
		return rowText
	}
	return rowText[:len(rowText)-len(rowWords)] + style.paint(schemaNote, rowWords)
}
