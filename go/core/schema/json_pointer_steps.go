package schema

import (
	"errors"
	"strings"

	"github.com/litebitus/yaml-dsl-spec/go/core"
)

// JSONPointerSteps is each step of a JSON pointer, `~1` read as `/` and `~0` as `~`, the root's pointer giving none;
// else why the text is no JSON pointer.
func JSONPointerSteps(jsonPointer string) ([]string, error) {
	if jsonPointer == "" {
		return []string{}, nil
	}
	if !strings.HasPrefix(jsonPointer, "/") {
		return nil, errors.New(core.CodeSpan(jsonPointer) + " is no JSON pointer: a pointer starts with " +
			core.CodeSpan("/"))
	}
	for _, afterTilde := range strings.Split(jsonPointer, "~")[1:] {
		if !strings.HasPrefix(afterTilde, "0") && !strings.HasPrefix(afterTilde, "1") {
			return nil, errors.New(core.CodeSpan(jsonPointer) + " is no JSON pointer: " + core.CodeSpan("~") +
				" is followed by " + core.CodeSpan("0") + " or " + core.CodeSpan("1") + " in one")
		}
	}
	rawSteps := strings.Split(jsonPointer, "/")[1:]
	pointerSteps := make([]string, len(rawSteps))
	for stepIndex, rawStep := range rawSteps {
		pointerSteps[stepIndex] = core.UnescapePointer(rawStep)
	}
	return pointerSteps, nil
}
