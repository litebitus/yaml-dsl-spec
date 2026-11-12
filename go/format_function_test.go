package yamldslspec

import (
	"math"
	"testing"
)

func TestGoOperandsReadAsTheKindTheirValueIs(testingContext *testing.T) {
	for _, operandCase := range []struct {
		layout       string
		operandValue any
		expectedText string
	}{
		{"%d", int8(-7), "-7"},
		{"%d", uint16(7), "7"},
		{"%d", uint64(math.MaxInt64), "9223372036854775807"},
		{"%f", uint64(math.MaxUint64), "18446744073709551616.000000"},
		{"%.1f", float32(1.5), "1.5"},
	} {
		writtenText, formatError := FormatLayout(operandCase.layout, []any{operandCase.operandValue})
		if formatError != nil || writtenText != operandCase.expectedText {
			testingContext.Errorf("%T: got %q (%v), want %q", operandCase.operandValue, writtenText, formatError, operandCase.expectedText)
		}
	}
}

func TestGoOperandsOfNoKindTheFunctionWritesAreProblems(testingContext *testing.T) {
	for _, operandCase := range []struct {
		operandValue    any
		expectedProblem string
	}{
		{math.NaN(), "`%f` writes a number, and operand 1 is a number that is not finite"},
		{math.Inf(-1), "`%f` writes a number, and operand 1 is a number that is not finite"},
		{[]string{"mock"}, "`%f` writes a number, and operand 1 is a list"},
		{map[int]int{}, "`%f` writes a number, and operand 1 is a map"},
		{struct{ mockField int }{}, "`%f` writes a number, and operand 1 is a Go struct { mockField int }"},
	} {
		_, formatError := FormatLayout("%f", []any{operandCase.operandValue})
		if formatError == nil || formatError.Error() != operandCase.expectedProblem {
			testingContext.Errorf("%T: %v, want %q", operandCase.operandValue, formatError, operandCase.expectedProblem)
		}
	}
}
