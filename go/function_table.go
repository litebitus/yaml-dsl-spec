package yamldslspec

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// FunctionTablePointer is where a document schema publishes its function table.
const FunctionTablePointer = "/x-yaml-dsl-functions"

// FunctionTable is the functions and macros a document schema publishes.
type FunctionTable struct {
	functionEntries map[string]FunctionEntry
}

// FunctionEntry is a function's arguments and, for a macro, what the macro does.
type FunctionEntry struct {
	Arguments []FunctionArgument
	Macro     *MacroEntry
}

// FunctionArgument is an argument of a function: its name, whether it repeats, and its default where it has one.
type FunctionArgument struct {
	Name         string
	Repeated     bool
	DefaultValue any
	HasDefault   bool
}

// MacroKind is what a macro does.
type MacroKind string

const (
	MacroKindFunctionCall MacroKind = "function_call"
	MacroKindMerge        MacroKind = "merge"
	MacroKindLoop         MacroKind = "loop"
	MacroKindTransform    MacroKind = "transform"
)

// MergeBehavior is how a merge macro merges.
type MergeBehavior string

const (
	MergeBehaviorShallow MergeBehavior = "shallow"
	MergeBehaviorDeep    MergeBehavior = "deep"
	MergeBehaviorJoin    MergeBehavior = "join"
)

// MacroEntry is a macro's kind, merge behavior, name suffix and the arguments that play its roles.
type MacroEntry struct {
	Kind          MacroKind
	MergeBehavior MergeBehavior
	NameSuffix    *CompiledPattern
	roleArguments map[string]string
}

var macroRoleNames = []string{"callee", "callee_arguments", "content", "loop_variables", "collection", "body"}

// Role is the name of the macro's argument that plays a role.
func (macroEntry MacroEntry) Role(roleName string) (string, bool) {
	argumentName, isFound := macroEntry.roleArguments[roleName]
	return argumentName, isFound
}

// FunctionTableFromSchema reads the table a document schema publishes, empty for none, or says why it is none.
func FunctionTableFromSchema(documentSchema any) (FunctionTable, error) {
	functionTable := FunctionTable{functionEntries: map[string]FunctionEntry{}}
	tableValue, isPresent := jsonPointerValue(documentSchema, FunctionTablePointer)
	if !isPresent {
		return functionTable, nil
	}
	if !isJSONObject(tableValue) {
		return FunctionTable{}, fmt.Errorf("%s is not a mapping of function names", FunctionTablePointer)
	}
	for _, functionName := range objectKeys(tableValue) {
		entryValue, _ := objectField(tableValue, functionName)
		functionEntry, entryError := functionEntryOf(functionName, entryValue)
		if entryError != nil {
			return FunctionTable{}, entryError
		}
		functionTable.functionEntries[functionName] = functionEntry
	}
	return functionTable, nil
}

// Entry is the function or macro a call names, a suffixed macro name included.
func (functionTable FunctionTable) Entry(functionName string) (FunctionEntry, bool) {
	isSuffixed := func(functionEntry FunctionEntry) bool {
		return functionEntry.Macro != nil && functionEntry.Macro.NameSuffix != nil
	}
	if functionEntry, isFound := functionTable.functionEntries[functionName]; isFound && !isSuffixed(functionEntry) {
		return functionEntry, true
	}
	tableNames := make([]string, 0, len(functionTable.functionEntries))
	for tableName := range functionTable.functionEntries {
		tableNames = append(tableNames, tableName)
	}
	slices.Sort(tableNames)
	for _, tableName := range tableNames {
		functionEntry := functionTable.functionEntries[tableName]
		if !isSuffixed(functionEntry) {
			continue
		}
		nameSuffix, isPrefixed := strings.CutPrefix(functionName, tableName)
		if isPrefixed && functionEntry.Macro.NameSuffix.MatchString(nameSuffix) {
			return functionEntry, true
		}
	}
	return FunctionEntry{}, false
}

func functionEntryOf(functionName string, entryValue any) (FunctionEntry, error) {
	listedArguments, isList := entryValue.([]any)
	if !isList {
		argumentsValue, _ := objectField(entryValue, "arguments")
		listedArguments, isList = argumentsValue.([]any)
		if !isList {
			return FunctionEntry{}, fmt.Errorf("function `%s` has no arguments list", functionName)
		}
	}
	functionEntry := FunctionEntry{Arguments: []FunctionArgument{}}
	for _, argumentValue := range listedArguments {
		nameValue, _ := objectField(argumentValue, "name")
		argumentName, isText := nameValue.(string)
		if !isText {
			return FunctionEntry{}, fmt.Errorf("an argument of `%s` has no name", functionName)
		}
		repeatedValue, _ := objectField(argumentValue, "repeated")
		isRepeated, _ := repeatedValue.(bool)
		defaultValue, hasDefault := objectField(argumentValue, "default")
		functionEntry.Arguments = append(functionEntry.Arguments, FunctionArgument{
			Name:         argumentName,
			Repeated:     isRepeated,
			DefaultValue: defaultValue,
			HasDefault:   hasDefault,
		})
	}
	if macroValue, hasMacro := objectField(entryValue, "macro"); hasMacro {
		macroEntry, macroError := macroEntryOf(functionName, macroValue)
		if macroError != nil {
			return FunctionEntry{}, macroError
		}
		functionEntry.Macro = &macroEntry
	}
	return functionEntry, nil
}

func macroEntryOf(functionName string, macroValue any) (MacroEntry, error) {
	kindValue, _ := objectField(macroValue, "kind")
	kindText, isKindText := kindValue.(string)
	macroEntry := MacroEntry{roleArguments: map[string]string{}}
	switch MacroKind(kindText) {
	case MacroKindFunctionCall, MacroKindMerge, MacroKindLoop, MacroKindTransform:
		macroEntry.Kind = MacroKind(kindText)
	default:
		if !isKindText {
			return MacroEntry{}, fmt.Errorf("macro `%s` has an unknown kind None", functionName)
		}
		return MacroEntry{}, fmt.Errorf("macro `%s` has an unknown kind Some(%q)", functionName, kindText)
	}
	behaviorValue, _ := objectField(macroValue, "merge_behavior")
	behaviorText, isBehaviorText := behaviorValue.(string)
	switch {
	case !isBehaviorText || behaviorText == string(MergeBehaviorShallow):
		macroEntry.MergeBehavior = MergeBehaviorShallow
	case behaviorText == string(MergeBehaviorDeep) || behaviorText == string(MergeBehaviorJoin):
		macroEntry.MergeBehavior = MergeBehavior(behaviorText)
	default:
		return MacroEntry{}, fmt.Errorf("macro `%s` has an unknown merge_behavior `%s`", functionName, behaviorText)
	}
	suffixValue, _ := objectField(macroValue, "name_suffix_pattern")
	if suffixText, isSuffixText := suffixValue.(string); isSuffixText {
		suffixPattern, compileError := CompilePattern("^(?:" + suffixText + ")$")
		if compileError != nil {
			return MacroEntry{}, fmt.Errorf("macro `%s`: %v", functionName, compileError)
		}
		macroEntry.NameSuffix = suffixPattern
	}
	for _, roleName := range macroRoleNames {
		roleValue, _ := objectField(macroValue, roleName)
		if argumentName, isArgumentName := roleValue.(string); isArgumentName {
			macroEntry.roleArguments[roleName] = argumentName
		}
	}
	if _, hasLoopVariables := macroEntry.roleArguments["loop_variables"]; !hasLoopVariables {
		loopVariableValue, _ := objectField(macroValue, "loop_variable")
		if argumentName, isArgumentName := loopVariableValue.(string); isArgumentName {
			macroEntry.roleArguments["loop_variables"] = argumentName
		}
	}
	return macroEntry, nil
}

// CallKey is the call a key writes: its name where it has one, its function, and whether it splats.
type CallKey struct {
	Name     *string
	Function string
	Splat    bool
}

// ParseCallKey is the call a key writes, `<name> <marker><function>[splat]` or the marker alone.
func ParseCallKey(entryKey string, markerFunction MarkerFunction) (CallKey, bool) {
	trimmedKey := strings.TrimRightFunc(entryKey, unicode.IsSpace)
	callStart := strings.LastIndexByte(trimmedKey, ' ') + 1
	markedRest, isMarked := strings.CutPrefix(trimmedKey[callStart:], markerFunction.CallMarker)
	if !isMarked {
		return CallKey{}, false
	}
	functionName, isSplat := markedRest, false
	if splatOperator := markerFunction.SplatOperator; splatOperator != nil && strings.HasSuffix(markedRest, *splatOperator) {
		functionName, isSplat = markedRest[:len(markedRest)-len(*splatOperator)], true
	}
	if !isFunctionName(functionName) {
		return CallKey{}, false
	}
	writtenCall := CallKey{Function: functionName, Splat: isSplat}
	if callStart > 0 {
		callName := strings.TrimSpace(trimmedKey[:callStart])
		writtenCall.Name = &callName
	}
	return writtenCall, true
}

func isFunctionName(functionName string) bool {
	if functionName == "" {
		return false
	}
	for characterIndex, nameCharacter := range functionName {
		isLetter := (nameCharacter >= 'a' && nameCharacter <= 'z') || (nameCharacter >= 'A' && nameCharacter <= 'Z')
		isDigit := nameCharacter >= '0' && nameCharacter <= '9'
		if characterIndex == 0 && !isLetter && nameCharacter != '_' {
			return false
		}
		if !isLetter && !isDigit && nameCharacter != '_' && nameCharacter != ':' && nameCharacter != '-' {
			return false
		}
	}
	return true
}
