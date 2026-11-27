// Package core is the part of the YAML DSL standard's core that schema validation and the definition reader build on.
package core

import "strings"

// CodeSpan is a value fenced in the fewest backticks it does not hold, padded with a space where it begins or ends with one.
func CodeSpan(spanText string) string {
	longestRun, currentRun := 0, 0
	for _, textRune := range spanText {
		if textRune == '`' {
			currentRun++
			longestRun = max(longestRun, currentRun)
		} else {
			currentRun = 0
		}
	}
	fenceText := strings.Repeat("`", longestRun+1)
	if strings.HasPrefix(spanText, "`") || strings.HasSuffix(spanText, "`") {
		return fenceText + " " + spanText + " " + fenceText
	}
	return fenceText + spanText + fenceText
}
