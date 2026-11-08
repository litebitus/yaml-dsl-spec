package diagnostic_renderer

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

const (
	machineSeparators            = "=:/,;&?|.()[]{}!-"
	comparisonOperatorCharacters = "=<>!~-&|"
	levelColumns                 = 2
)

type wrappedRow struct {
	startIndex    int
	endIndex      int
	indentColumns int
}

type wrappedLine struct {
	lineRunes []rune
	lineRows  []wrappedRow
}

func newWrappedLine(lineText string, rowWidth *int) wrappedLine {
	lineRunes := []rune(lineText)
	if rowWidth == nil {
		return wrappedLine{lineRunes: lineRunes, lineRows: []wrappedRow{{endIndex: len(lineRunes)}}}
	}
	return wrappedLine{lineRunes: lineRunes, lineRows: dividedRows(lineRunes, *rowWidth)}
}

func (line wrappedLine) rowText(row wrappedRow) string {
	rowRunes := string(line.lineRunes[row.startIndex:row.endIndex])
	return strings.Repeat(" ", row.indentColumns) + strings.TrimRightFunc(rowRunes, unicode.IsSpace)
}

func (line wrappedLine) rowTexts() []string {
	printedRows := make([]string, 0, len(line.lineRows))
	for _, row := range line.lineRows {
		printedRows = append(printedRows, line.rowText(row))
	}
	return printedRows
}

func (line wrappedLine) columnsBetween(startIndex, endIndex int) int {
	return runeColumns(line.lineRunes[min(startIndex, endIndex):endIndex])
}

func runeColumns(measuredRunes []rune) int {
	totalColumns := 0
	for _, measuredRune := range measuredRunes {
		totalColumns += displayColumns(measuredRune)
	}
	return totalColumns
}

func lineBreakOpportunities(lineRunes []rune) []bool {
	unicodeAllows := make([]bool, len(lineRunes)+1)
	lineText := string(lineRunes)
	runeIndex, byteOffset, runeByteOffset := 0, 0, 0
	breakState := -1
	for remainingText := lineText; len(remainingText) > 0; {
		var lineSegment string
		lineSegment, remainingText, _, breakState = uniseg.FirstLineSegmentInString(remainingText, breakState)
		byteOffset += len(lineSegment)
		for runeIndex < len(lineRunes) && runeByteOffset < byteOffset {
			runeByteOffset += utf8.RuneLen(lineRunes[runeIndex])
			runeIndex++
		}
		if runeByteOffset == byteOffset {
			unicodeAllows[runeIndex] = true
		}
	}
	return unicodeAllows
}

func divisionPoints(lineRunes []rune) []bool {
	unicodeAllows := lineBreakOpportunities(lineRunes)
	divides := make([]bool, len(lineRunes))
	for runeIndex := 1; runeIndex < len(lineRunes); runeIndex++ {
		beforeRune, afterRune := lineRunes[runeIndex-1], lineRunes[runeIndex]
		if afterRune == ' ' {
			continue
		}
		insideComparisonOperator := strings.ContainsRune(comparisonOperatorCharacters, beforeRune) &&
			strings.ContainsRune(comparisonOperatorCharacters, afterRune)
		divides[runeIndex] = !insideComparisonOperator &&
			(unicodeAllows[runeIndex] || strings.ContainsRune(machineSeparators, beforeRune))
	}
	return divides
}

func dividedRows(lineRunes []rune, rowWidth int) []wrappedRow {
	leadingSpaces := 0
	for leadingSpaces < len(lineRunes) && lineRunes[leadingSpaces] == ' ' {
		leadingSpaces++
	}
	continuationIndent := leadingSpaces + levelColumns
	divides := divisionPoints(lineRunes)
	printedColumns := func(startIndex, endIndex int) int {
		for endIndex > startIndex && lineRunes[endIndex-1] == ' ' {
			endIndex--
		}
		return runeColumns(lineRunes[startIndex:endIndex])
	}
	var dividedLineRows []wrappedRow
	startIndex, rowIndent := 0, 0
	for startIndex < len(lineRunes) {
		currentIndent := rowIndent
		endIndex := len(lineRunes)
		for {
			roomColumns := max(rowWidth-currentIndent, 0)
			if printedColumns(startIndex, len(lineRunes)) <= roomColumns {
				endIndex = len(lineRunes)
				break
			}
			fittingEnd := -1
			for candidateEnd := startIndex + 1; candidateEnd < len(lineRunes); candidateEnd++ {
				if !divides[candidateEnd] {
					continue
				}
				if printedColumns(startIndex, candidateEnd) > roomColumns {
					break
				}
				fittingEnd = candidateEnd
			}
			if fittingEnd >= 0 {
				endIndex = fittingEnd
				break
			}
			if currentIndent > 0 {
				currentIndent = max(currentIndent-levelColumns, 0)
				continue
			}
			endIndex = len(lineRunes)
			for candidateEnd := startIndex + 1; candidateEnd < len(lineRunes); candidateEnd++ {
				if divides[candidateEnd] {
					endIndex = candidateEnd
					break
				}
			}
			break
		}
		dividedLineRows = append(dividedLineRows, wrappedRow{startIndex: startIndex, endIndex: endIndex, indentColumns: currentIndent})
		startIndex = endIndex
		for startIndex < len(lineRunes) && lineRunes[startIndex] == ' ' {
			startIndex++
		}
		rowIndent = continuationIndent
	}
	if len(dividedLineRows) == 0 {
		dividedLineRows = append(dividedLineRows, wrappedRow{})
	}
	return dividedLineRows
}
