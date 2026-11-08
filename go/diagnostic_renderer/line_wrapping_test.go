package diagnostic_renderer

import (
	"reflect"
	"strings"
	"testing"
)

func dividedTexts(lineText string, rowWidth int) []string {
	return newWrappedLine(lineText, &rowWidth).rowTexts()
}

func expectRows(testingContext *testing.T, actualRows []string, expectedRows ...string) {
	testingContext.Helper()
	if !reflect.DeepEqual(actualRows, expectedRows) {
		testingContext.Errorf("rows %q, expected %q", actualRows, expectedRows)
	}
}

func TestALineThatFitsOrHasNoWidthIsOneRow(testingContext *testing.T) {
	expectRows(testingContext, dividedTexts("  short: line", 40), "  short: line")
	unmeasuredLine := strings.Repeat("x", 300)
	expectRows(testingContext, newWrappedLine(unmeasuredLine, nil).rowTexts(), unmeasuredLine)
	expectRows(testingContext, dividedTexts("", 10), "")
}

func TestALongLineDividesAfterASeparatorAndContinuesOneLevelIn(testingContext *testing.T) {
	expectRows(testingContext, dividedTexts("  - s3://mock-bucket/schemas/mock.schema.json", 24),
		"  - s3://mock-bucket/",
		"    schemas/mock.schema.",
		"    json",
	)
}

func TestAComparisonOperatorIsNeverDividedAndAWordWithNoDivisionOverruns(testingContext *testing.T) {
	divides := divisionPoints([]rune("a->b x=y"))
	if divides[2] || !divides[7] {
		testingContext.Errorf("division points %v", divides)
	}
	expectRows(testingContext, dividedTexts("mock_identifier_without_a_division", 10), "mock_identifier_without_a_division")
}

func TestTheIndentationGivesBeforeAWordOverruns(testingContext *testing.T) {
	expectRows(testingContext, dividedTexts("      key: abcdefghij", 12), "      key:", "  abcdefghij")
}

func TestChineseDividesBetweenAnyTwoCharacters(testingContext *testing.T) {
	expectRows(testingContext, dividedTexts("    description: 这个生成器把每个声明写成", 30),
		"    description: 这个生成器把",
		"      每个声明写成",
	)
}

func TestWidthsAreDisplayColumns(testingContext *testing.T) {
	expectRows(testingContext, dividedTexts("名前 値値値", 8), "名前 値", "  値値")
	measuredColumns := map[rune]int{'a': 1, 'é': 1, 'Ж': 1, '名': 2, 'か': 2, 'カ': 2, '한': 2, '😀': 2}
	for measuredRune, expectedColumns := range measuredColumns {
		if actualColumns := displayColumns(measuredRune); actualColumns != expectedColumns {
			testingContext.Errorf("%U is %d columns, expected %d", measuredRune, actualColumns, expectedColumns)
		}
	}
}

func TestASpaceAndASeparatorDivideAndAWordStaysWhole(testingContext *testing.T) {
	divides := divisionPoints([]rune("mock $env a(b) -v"))
	expectedDivides := map[int]bool{5: true, 6: false, 10: true, 11: false, 12: true, 15: true, 16: true}
	for runeIndex, expectedDivide := range expectedDivides {
		if divides[runeIndex] != expectedDivide {
			testingContext.Errorf("divides at %d is %t, expected %t", runeIndex, divides[runeIndex], expectedDivide)
		}
	}
}

func TestWideCharactersDivideBetweenAnyTwo(testingContext *testing.T) {
	expectRows(testingContext, dividedTexts("かなカナ한국어😀😃", 8), "かなカナ", "  한국어", "  😀😃")
}
