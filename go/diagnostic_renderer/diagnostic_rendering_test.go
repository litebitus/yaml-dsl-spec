package diagnostic_renderer

import (
	"reflect"
	"testing"
)

func TestTheLinesNestedUnderAreFoundFromTheWrongLineUpThroughTheLinesAroundIt(testingContext *testing.T) {
	sourceLines := []string{
		"data_source:",
		"  mock_bucket:",
		"    type: remote",
		"    outputs: [mock]",
		"    path: mock",
		"",
		"  # mock comment",
		"  mock_key:",
		"    type: kms_keys",
	}
	if shown := shownLines(sourceLines, 9); !reflect.DeepEqual(shown, []int{1, 7, 8, 9}) {
		testingContext.Errorf("shown %v", shown)
	}
	if shownLines(sourceLines, 0) != nil || shownLines(sourceLines, 10) != nil {
		testingContext.Error("a line outside the text shows lines")
	}
}

func TestATokenEndsAtItsColonItsQuoteOrAComment(testingContext *testing.T) {
	tokenLengths := map[string]int{
		"brnch: main":         5,
		"\"gld\" # tier":      5,
		"\"a\\\"b\" x":        6,
		"'unclosed":           9,
		"{{ mock.ref }} tail": 14,
		"kms_keys  # typo":    8,
		"2":                   1,
	}
	for restText, expectedLength := range tokenLengths {
		if actualLength := tokenLength(restText); actualLength != expectedLength {
			testingContext.Errorf("%q is %d long, expected %d", restText, actualLength, expectedLength)
		}
	}
}

func TestTextIsReadAsLinesEachEndedByANewline(testingContext *testing.T) {
	textCases := map[string][]string{
		"":              nil,
		"\n":            {""},
		"a\r\nb\n\n":    {"a", "b", ""},
		"a\nb\r":        {"a", "b\r"},
		"mock-only\r\n": {"mock-only"},
	}
	for sourceText, expectedLines := range textCases {
		if actualLines := textLines(sourceText); !reflect.DeepEqual(actualLines, expectedLines) {
			testingContext.Errorf("%q reads as %q, expected %q", sourceText, actualLines, expectedLines)
		}
	}
}
