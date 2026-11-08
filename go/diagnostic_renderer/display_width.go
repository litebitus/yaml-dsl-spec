package diagnostic_renderer

import "github.com/rivo/uniseg"

func displayColumns(measuredRune rune) int {
	return uniseg.StringWidth(string(measuredRune))
}
