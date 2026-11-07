package yamldslspec

import (
	"strconv"
	"time"

	"github.com/dlclark/regexp2"
)

const patternMatchTimeout = time.Second

// CompiledPattern is a regular expression a `yaml-dsl.yml` or a schema writes, compiled.
type CompiledPattern struct {
	sourceText     string
	compiledRegexp *regexp2.Regexp
}

// CompilePattern compiles a pattern, or says why it is not a regular expression.
func CompilePattern(patternText string) (*CompiledPattern, error) {
	compiledRegexp, compileError := regexp2.Compile(patternText, regexp2.RE2)
	if compileError != nil {
		return nil, compileError
	}
	compiledRegexp.MatchTimeout = patternMatchTimeout
	return &CompiledPattern{sourceText: patternText, compiledRegexp: compiledRegexp}, nil
}

func (compiledPattern *CompiledPattern) String() string {
	return compiledPattern.sourceText
}

// MatchString tells whether a text holds a match of the pattern.
func (compiledPattern *CompiledPattern) MatchString(matchedText string) bool {
	isMatched, matchError := compiledPattern.compiledRegexp.MatchString(matchedText)
	return matchError == nil && isMatched
}

// GroupNames lists the pattern's named groups in the order it writes them.
func (compiledPattern *CompiledPattern) GroupNames() []string {
	var groupNames []string
	for _, groupName := range compiledPattern.compiledRegexp.GetGroupNames() {
		if _, numberError := strconv.Atoi(groupName); numberError != nil {
			groupNames = append(groupNames, groupName)
		}
	}
	return groupNames
}

// PatternMatch is a pattern's first match in a text, and the text each named group captures.
type PatternMatch struct {
	WholeMatch  string
	NamedGroups map[string]string
}

// FindMatch is the pattern's first match in a text; a group that captures nothing is left out.
func (compiledPattern *CompiledPattern) FindMatch(matchedText string) (PatternMatch, bool) {
	regexpMatch, matchError := compiledPattern.compiledRegexp.FindStringMatch(matchedText)
	if matchError != nil || regexpMatch == nil {
		return PatternMatch{}, false
	}
	foundMatch := PatternMatch{WholeMatch: regexpMatch.String(), NamedGroups: map[string]string{}}
	for _, groupName := range compiledPattern.GroupNames() {
		capturedGroup := regexpMatch.GroupByName(groupName)
		if capturedGroup != nil && len(capturedGroup.Captures) > 0 {
			foundMatch.NamedGroups[groupName] = capturedGroup.String()
		}
	}
	return foundMatch, true
}
