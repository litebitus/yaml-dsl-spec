package yamldslspec

import (
	"slices"
	"strings"
)

// PathStep is one step of a concrete path into a document: a map key or a list index.
type PathStep struct {
	Key     string
	Index   int
	IsIndex bool
}

// KeyStep is the step to a map key.
func KeyStep(mapKey string) PathStep {
	return PathStep{Key: mapKey}
}

// IndexStep is the step to a list index.
func IndexStep(listIndex int) PathStep {
	return PathStep{Index: listIndex, IsIndex: true}
}

type selectorKind int

const (
	selectorName selectorKind = iota
	selectorWildcard
	selectorDescendantName
	selectorDescendantAll
)

type pathSelector struct {
	selectorType selectorKind
	selectedName string
}

// PathPattern is a parsed path of the JSONPath subset and the keys its wildcard steps skip.
type PathPattern struct {
	stepSelectors []pathSelector
	skipKeys      []string
}

// SelectedValue is a node a pattern selects and its path.
type SelectedValue struct {
	Path  []PathStep
	Value ResolvedValue
}

// ParsePathPattern parses a path, or names the path outside the subset.
func ParsePathPattern(pathText string, skipKeys []string) (PathPattern, *Diagnostic) {
	invalidPath := func() (PathPattern, *Diagnostic) {
		pathProblem := UnplacedDiagnostic("`" + pathText + "` is not a path of the supported JSONPath subset")
		return PathPattern{}, &pathProblem
	}
	restText, hasRoot := strings.CutPrefix(pathText, "$")
	if !hasRoot {
		return invalidPath()
	}
	var stepSelectors []pathSelector
	for restText != "" {
		stepSelector, remainingText, isParsed := nextSelector(restText)
		if !isParsed {
			return invalidPath()
		}
		stepSelectors = append(stepSelectors, stepSelector)
		restText = remainingText
	}
	return PathPattern{stepSelectors: stepSelectors, skipKeys: slices.Clone(skipKeys)}, nil
}

func nextSelector(restText string) (pathSelector, string, bool) {
	if afterDots, isDescendant := strings.CutPrefix(restText, ".."); isDescendant {
		if afterDots == "*" {
			return pathSelector{selectorType: selectorDescendantAll}, "", true
		}
		stepName, remainingText, isParsed := quotedName(afterDots)
		if !isParsed {
			stepName, remainingText, isParsed = dottedName(afterDots)
		}
		return pathSelector{selectorType: selectorDescendantName, selectedName: stepName}, remainingText, isParsed
	}
	if afterWildcard, isWildcard := strings.CutPrefix(restText, ".*"); isWildcard {
		return pathSelector{selectorType: selectorWildcard}, afterWildcard, true
	}
	if afterDot, isDotted := strings.CutPrefix(restText, "."); isDotted {
		stepName, remainingText, isParsed := dottedName(afterDot)
		return pathSelector{selectorType: selectorName, selectedName: stepName}, remainingText, isParsed
	}
	if afterBrackets, isBracketWildcard := strings.CutPrefix(restText, "[*]"); isBracketWildcard {
		return pathSelector{selectorType: selectorWildcard}, afterBrackets, true
	}
	stepName, remainingText, isParsed := quotedName(restText)
	return pathSelector{selectorType: selectorName, selectedName: stepName}, remainingText, isParsed
}

func quotedName(stepText string) (string, string, bool) {
	innerText, isOpened := strings.CutPrefix(stepText, "['")
	if !isOpened {
		return "", "", false
	}
	closeAt := strings.IndexByte(innerText, '\'')
	if closeAt <= 0 {
		return "", "", false
	}
	afterClose, isClosed := strings.CutPrefix(innerText[closeAt:], "']")
	if !isClosed {
		return "", "", false
	}
	return innerText[:closeAt], afterClose, true
}

func dottedName(stepText string) (string, string, bool) {
	if strings.HasPrefix(stepText, "*") {
		return "", "", false
	}
	nameEnd := strings.IndexAny(stepText, ".[")
	if nameEnd < 0 {
		nameEnd = len(stepText)
	}
	if nameEnd == 0 {
		return "", "", false
	}
	return stepText[:nameEnd], stepText[nameEnd:], true
}

// Select is every node of a document the pattern selects, with its path, in document order.
func (pathPattern PathPattern) Select(documentValue ResolvedValue) []SelectedValue {
	var selectedValues []SelectedValue
	pathPattern.collectSelected(documentValue, nil, &selectedValues)
	return selectedValues
}

func (pathPattern PathPattern) collectSelected(
	currentValue ResolvedValue,
	currentPath []PathStep,
	selectedValues *[]SelectedValue,
) {
	if pathPattern.Selects(currentPath) {
		*selectedValues = append(*selectedValues, SelectedValue{Path: slices.Clone(currentPath), Value: currentValue})
	}
	switch currentValue.Kind {
	case ResolvedMap:
		for _, mapEntry := range currentValue.Entries {
			pathPattern.collectSelected(mapEntry.Value, append(slices.Clone(currentPath), KeyStep(mapEntry.Key)), selectedValues)
		}
	case ResolvedList:
		for itemIndex, listItem := range currentValue.Items {
			pathPattern.collectSelected(listItem, append(slices.Clone(currentPath), IndexStep(itemIndex)), selectedValues)
		}
	}
}

// Selects tells whether the pattern selects the node at a path.
func (pathPattern PathPattern) Selects(concretePath []PathStep) bool {
	return pathPattern.matchesFrom(0, concretePath)
}

// Covers tells whether a path is a node the pattern selects or anywhere below one.
func (pathPattern PathPattern) Covers(concretePath []PathStep) bool {
	for pathLength := 0; pathLength <= len(concretePath); pathLength++ {
		if pathPattern.Selects(concretePath[:pathLength]) {
			return true
		}
	}
	return false
}

func (pathPattern PathPattern) isSkipped(pathStep PathStep) bool {
	return !pathStep.IsIndex && slices.Contains(pathPattern.skipKeys, pathStep.Key)
}

func (pathPattern PathPattern) matchesFrom(selectorIndex int, concretePath []PathStep) bool {
	if selectorIndex == len(pathPattern.stepSelectors) {
		return len(concretePath) == 0
	}
	stepSelector := pathPattern.stepSelectors[selectorIndex]
	switch stepSelector.selectorType {
	case selectorName:
		return len(concretePath) > 0 && !concretePath[0].IsIndex && concretePath[0].Key == stepSelector.selectedName &&
			pathPattern.matchesFrom(selectorIndex+1, concretePath[1:])
	case selectorWildcard:
		return len(concretePath) > 0 && !pathPattern.isSkipped(concretePath[0]) &&
			pathPattern.matchesFrom(selectorIndex+1, concretePath[1:])
	case selectorDescendantName:
		for stepDepth := range concretePath {
			if stepDepth > 0 && pathPattern.isSkipped(concretePath[stepDepth-1]) {
				return false
			}
			pathStep := concretePath[stepDepth]
			if !pathStep.IsIndex && pathStep.Key == stepSelector.selectedName &&
				pathPattern.matchesFrom(selectorIndex+1, concretePath[stepDepth+1:]) {
				return true
			}
		}
		return false
	}
	if len(concretePath) == 0 {
		return false
	}
	for _, pathStep := range concretePath {
		if pathPattern.isSkipped(pathStep) {
			return false
		}
	}
	return true
}

// PathSet is the patterns of a list of paths, matched as one.
type PathSet struct {
	pathPatterns []PathPattern
}

// ParsePathSet parses each entry, or names the first path outside the subset.
func ParsePathSet(pathEntries []PathEntry) (PathSet, *Diagnostic) {
	pathSet := PathSet{}
	for _, pathEntry := range pathEntries {
		pathPattern, pathProblem := ParsePathPattern(pathEntry.Path, pathEntry.SkipKeys)
		if pathProblem != nil {
			return PathSet{}, pathProblem
		}
		pathSet.pathPatterns = append(pathSet.pathPatterns, pathPattern)
	}
	return pathSet, nil
}

// Selects tells whether any pattern selects the node at a path.
func (pathSet PathSet) Selects(concretePath []PathStep) bool {
	return slices.ContainsFunc(pathSet.pathPatterns, func(pathPattern PathPattern) bool {
		return pathPattern.Selects(concretePath)
	})
}

// Covers tells whether any pattern covers a path.
func (pathSet PathSet) Covers(concretePath []PathStep) bool {
	return slices.ContainsFunc(pathSet.pathPatterns, func(pathPattern PathPattern) bool {
		return pathPattern.Covers(concretePath)
	})
}
