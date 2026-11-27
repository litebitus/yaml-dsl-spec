package core

import "testing"

func TestFloatsAreWrittenAsTheReferenceWritesThem(testingContext *testing.T) {
	writtenFloats := map[float64]string{
		1e16:    "1e+16",
		1e15:    "1000000000000000.0",
		1e-7:    "1e-7",
		0.00001: "0.00001",
		100:     "100.0",
		1.5:     "1.5",
	}
	for floatValue, expectedText := range writtenFloats {
		if serdeFloatText(floatValue) != expectedText {
			testingContext.Errorf("%v writes as %q", floatValue, serdeFloatText(floatValue))
		}
	}
}
