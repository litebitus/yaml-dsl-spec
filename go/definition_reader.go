package yamldslspec

import (
	"fmt"
	"slices"
	"strings"
)

const concernToken = "{concern}"

var standardSchemaCache = NewSchemaCache()

// DeclaredDSL is a DSL of a `yaml-dsl.yml`: its validated definition, and its block as written.
type DeclaredDSL struct {
	Definition DSLDefinition
	Block      Node
}

// ReadDSLDefinitions is each DSL of a `yaml-dsl.yml`, validated, or every problem at its field.
func ReadDSLDefinitions(fileName, fileText string) ([]DeclaredDSL, []Diagnostic) {
	rootNode, parseProblem := ParseYAML(fileName, fileText, nil)
	if parseProblem != nil {
		return nil, []Diagnostic{*parseProblem}
	}
	schemaFile, versionProblem := versionSchema(rootNode, fileName)
	if versionProblem != nil {
		return nil, []Diagnostic{*versionProblem}
	}
	loadedSchema, loadError := standardSchemaCache.Load(schemaFile.fileName, schemaFile.schemaText)
	if loadError != nil {
		return nil, []Diagnostic{UnplacedDiagnostic(loadError.Error())}
	}
	if schemaProblems := loadedSchema.Validate(ResolvedValueFromNode(rootNode), ValidationRules{}); len(schemaProblems) > 0 {
		return nil, schemaProblems
	}
	dslsNode, _ := rootNode.Get("dsls")
	var blockProblems []Diagnostic
	declaredDSLs := []DeclaredDSL{}
	for blockIndex, dslBlock := range dslsNode.Items {
		if repeatedProblem, isRepeated := repeatedID(dslBlock, dslsNode.Items[:blockIndex]); isRepeated {
			blockProblems = append(blockProblems, repeatedProblem)
		}
		blockCheck := blockChecks{dslBlock: dslBlock}
		if checkProblems := blockCheck.allProblems(); len(checkProblems) > 0 {
			blockProblems = append(blockProblems, checkProblems...)
			continue
		}
		dslDefinition, definitionError := DSLDefinitionFromJSON(dslBlock.ToJSON())
		if definitionError != nil {
			blockProblems = append(blockProblems, DiagnosticAt(dslBlock.Position, definitionError.Error()))
			continue
		}
		declaredDSLs = append(declaredDSLs, DeclaredDSL{Definition: dslDefinition, Block: dslBlock})
	}
	if len(blockProblems) > 0 {
		return nil, blockProblems
	}
	return declaredDSLs, nil
}

func versionSchema(rootNode Node, fileName string) (versionedSchema, *Diagnostic) {
	readVersions := YAMLDSLVersions()
	quotedVersions := make([]string, 0, len(readVersions))
	for _, readVersion := range readVersions {
		quotedVersions = append(quotedVersions, "`\""+readVersion+"\"`")
	}
	versionLabel := "`version` is one of " + strings.Join(quotedVersions, ", ")
	versionNode, hasVersion := rootNode.Get("version")
	if !hasVersion {
		missingProblem := DiagnosticAt(FileStartPosition(fileName), "`version` is required").WithLabel(versionLabel)
		return versionedSchema{}, &missingProblem
	}
	versionText, _ := versionNode.AsString()
	if foundSchema, isCarried := schemaAt(yamlDSLSchemaFiles, versionText); isCarried {
		return foundSchema, nil
	}
	shownVersion := "`" + versionText + "`"
	if versionNode.Kind != NodeString {
		shownVersion = "`" + jsonText(versionNode.ToJSON()) + "`"
	}
	var versionHelp *string
	if closestVersion, isClose := ClosestName(versionText, readVersions); isClose {
		suggestionText := "did you mean `\"" + closestVersion + "\"`?"
		versionHelp = &suggestionText
	}
	unknownProblem := DiagnosticAt(versionNode.Position, shownVersion+" is not a version this reader reads").
		WithLabel(versionLabel).
		WithHelp(versionHelp)
	return versionedSchema{}, &unknownProblem
}

func repeatedID(dslBlock Node, earlierBlocks []Node) (Diagnostic, bool) {
	idNode, hasID := dslBlock.Get("id")
	idText, isText := idNode.AsString()
	if !hasID || !isText {
		return Diagnostic{}, false
	}
	for _, earlierBlock := range earlierBlocks {
		if earlierID, isFound := earlierBlock.Get("id"); isFound && earlierID.Kind == NodeString && earlierID.Text == idText {
			return DiagnosticAt(idNode.Position, "DSL `"+idText+"` is declared more than once").
				WithLabel("an earlier DSL of this file has this `id`"), true
		}
	}
	return Diagnostic{}, false
}

type blockChecks struct {
	dslBlock      Node
	blockProblems []Diagnostic
}

type expectedGroup struct {
	groupName   string
	heldMeaning string
}

var pathEntryFields = [][]string{
	{"placeholder", "unscanned_paths"},
	{"function", "calls_not_allowed_at"},
	{"key_sort_orders"},
}

func (blockCheck *blockChecks) allProblems() []Diagnostic {
	blockCheck.checkPatterns()
	blockCheck.checkPaths()
	blockCheck.checkRepeatedPathEntries()
	blockCheck.checkKeySortOrders()
	blockCheck.checkCommonLayerFiles()
	blockCheck.checkScopeNames()
	blockCheck.checkLocalsAndLoops()
	blockCheck.checkTagKeys()
	return blockCheck.blockProblems
}

func (blockCheck *blockChecks) reportProblem(problemNode Node, problemMessage, problemLabel string) {
	labeledProblem := DiagnosticAt(problemNode.Position, problemMessage).WithLabel(problemLabel)
	blockCheck.blockProblems = append(blockCheck.blockProblems, labeledProblem)
}

func (blockCheck *blockChecks) checkPatterns() {
	if patternNode, isFound := nodeAt(blockCheck.dslBlock, "placeholder", "pattern"); isFound {
		blockCheck.checkPattern(patternNode, []expectedGroup{{groupName: "body", heldMeaning: "a placeholder's body"}})
	}
	for _, referenceRule := range nodeList(blockCheck.dslBlock, "references") {
		patternNode, isFound := referenceRule.Get("pattern")
		if !isFound {
			continue
		}
		var expectedGroups []expectedGroup
		if nameGroup, isNamed := referenceRule.Get("name_group"); isNamed && nameGroup.Kind == NodeString {
			expectedGroups = append(expectedGroups, expectedGroup{groupName: nameGroup.Text, heldMeaning: "the name, by `name_group`"})
		}
		if scopeGroup, isScoped := referenceRule.Get("scope_group"); isScoped && scopeGroup.Kind == NodeString {
			expectedGroups = append(expectedGroups, expectedGroup{groupName: scopeGroup.Text, heldMeaning: "the scope, by `scope_group`"})
		}
		blockCheck.checkPattern(patternNode, expectedGroups)
	}
	for _, childRule := range nodeList(blockCheck.dslBlock, "validator", "child_values_from_parent_key") {
		if patternNode, isFound := childRule.Get("pattern"); isFound {
			blockCheck.checkPattern(patternNode, []expectedGroup{
				{groupName: "field", heldMeaning: "the child field"},
				{groupName: "value", heldMeaning: "its value"},
			})
		}
	}
}

func (blockCheck *blockChecks) checkPattern(patternNode Node, expectedGroups []expectedGroup) {
	patternText, isText := patternNode.AsString()
	if !isText {
		return
	}
	compiledPattern, compileError := CompilePattern(patternText)
	if compileError != nil {
		blockCheck.reportProblem(patternNode, "the pattern is not a regular expression", "a regular expression the reader compiles")
		return
	}
	groupNames := compiledPattern.GroupNames()
	for _, wantedGroup := range expectedGroups {
		if !slices.Contains(groupNames, wantedGroup.groupName) {
			blockCheck.reportProblem(patternNode,
				fmt.Sprintf("the pattern has no `(?<%s>...)` group", wantedGroup.groupName),
				"the group holds "+wantedGroup.heldMeaning)
		}
	}
}

func (blockCheck *blockChecks) checkPaths() {
	var pathNodes []Node
	for _, declarationRule := range nodeList(blockCheck.dslBlock, "declarations") {
		if pathNode, isFound := declarationRule.Get("path"); isFound {
			pathNodes = append(pathNodes, pathNode)
		}
	}
	for _, scopeNode := range mapValues(blockCheck.dslBlock, "scopes") {
		pathNodes = append(pathNodes, nodeList(scopeNode, "regions")...)
	}
	for _, fieldSteps := range pathEntryFields {
		for _, pathEntry := range nodeList(blockCheck.dslBlock, fieldSteps...) {
			if pathNode, isFound := pathEntry.Get("path"); isFound {
				pathNodes = append(pathNodes, pathNode)
			}
		}
	}
	for _, overlayInclude := range nodeList(blockCheck.dslBlock, "layers", "overlay_includes") {
		pathNodes = append(pathNodes, nodeList(overlayInclude, "key_paths")...)
		if locationPath, isFound := overlayInclude.Get("location_path"); isFound {
			pathNodes = append(pathNodes, locationPath)
		}
	}
	for _, pathNode := range pathNodes {
		pathText, isText := pathNode.AsString()
		if !isText {
			continue
		}
		if _, pathProblem := ParsePathPattern(pathText, nil); pathProblem != nil {
			blockCheck.reportProblem(pathNode, pathProblem.Message, "a path of the standard's JSONPath subset")
		}
	}
}

func (blockCheck *blockChecks) checkRepeatedPathEntries() {
	for _, fieldSteps := range pathEntryFields {
		pathEntries := nodeList(blockCheck.dslBlock, fieldSteps...)
		for entryIndex, pathEntry := range pathEntries {
			pathNode, isFound := pathEntry.Get("path")
			if !isFound {
				continue
			}
			isRepeated := slices.ContainsFunc(pathEntries[:entryIndex], func(earlierEntry Node) bool {
				return sameFieldText(earlierEntry, "path", pathNode)
			})
			if isRepeated {
				pathText, _ := pathNode.AsString()
				blockCheck.reportProblem(pathNode,
					"path `"+pathText+"` is listed more than once",
					"an earlier entry of `"+strings.Join(fieldSteps, ".")+"` has this path")
			}
		}
	}
}

func (blockCheck *blockChecks) checkKeySortOrders() {
	for _, sortOrder := range nodeList(blockCheck.dslBlock, "key_sort_orders") {
		orderNode, _ := sortOrder.Get("order")
		isAlphabetical := orderNode.Kind == NodeString && orderNode.Text == "alphabetical"
		for _, keysField := range []string{"first_keys", "last_keys"} {
			listedKeys := nodeList(sortOrder, keysField)
			if isAlphabetical && len(listedKeys) > 0 {
				fieldNode, isFound := sortOrder.Get(keysField)
				if !isFound {
					fieldNode = sortOrder
				}
				blockCheck.reportProblem(fieldNode, "`"+keysField+"` is not read with alphabetical order", "`[]` under alphabetical order")
			}
			for keyIndex, listedKey := range listedKeys {
				if slices.ContainsFunc(listedKeys[:keyIndex], func(earlierKey Node) bool { return sameScalarText(earlierKey, listedKey) }) {
					blockCheck.reportProblem(listedKey, "`"+keysField+"` lists a key more than once", "an earlier item is this key")
				}
			}
		}
		lastKeys := nodeList(sortOrder, "last_keys")
		for _, firstKey := range nodeList(sortOrder, "first_keys") {
			if slices.ContainsFunc(lastKeys, func(lastKey Node) bool { return sameScalarText(lastKey, firstKey) }) {
				blockCheck.reportProblem(firstKey, "a key is in both `first_keys` and `last_keys`", "`last_keys` lists it too")
			}
		}
	}
}

func (blockCheck *blockChecks) checkCommonLayerFiles() {
	discoveryNode, _ := nodeAt(blockCheck.dslBlock, "layers", "common_layer_discovery")
	isParentDiscovery := discoveryNode.Kind == NodeString && discoveryNode.Text == "parent"
	for _, filesNode := range mapValues(blockCheck.dslBlock, "layers", "common_layer_files") {
		fileNames := filesNode.Items
		for nameIndex, nameNode := range fileNames {
			nameText, _ := nameNode.AsString()
			if nameText == "" || nameText == "." || nameText == ".." || strings.ContainsAny(nameText, "/\\") {
				blockCheck.reportProblem(nameNode, "`"+nameText+"` is not a file name", "a file name, with no folder")
			}
			if slices.ContainsFunc(fileNames[:nameIndex], func(earlierName Node) bool { return sameScalarText(earlierName, nameNode) }) {
				blockCheck.reportProblem(nameNode, "`"+nameText+"` is listed more than once", "an earlier item is this name")
			}
			if strings.Count(nameText, concernToken) > 1 {
				blockCheck.reportProblem(nameNode,
					"`"+nameText+"` holds `"+concernToken+"` more than once",
					"a name holds `"+concernToken+"` at most once")
			}
			if isParentDiscovery && strings.Contains(nameText, concernToken) {
				blockCheck.reportProblem(nameNode,
					"`"+nameText+"` names a concern's common layer",
					"only `common_layer_discovery: ancestor` reaches a concern below its overlays")
			}
		}
		everyNameHoldsConcern := len(fileNames) > 0 && !slices.ContainsFunc(fileNames, func(nameNode Node) bool {
			nameText, _ := nameNode.AsString()
			return !strings.Contains(nameText, concernToken)
		})
		if everyNameHoldsConcern {
			blockCheck.reportProblem(filesNode,
				"every name holds `"+concernToken+"`",
				"a name without it is the common layer of a concern above its overlays")
		}
	}
}

type declaredScope struct {
	scopeName        string
	namedByParentKey bool
}

func (blockCheck *blockChecks) checkScopeNames() {
	declaredScopes := blockCheck.declaredScopes()
	declaredNames := make([]string, 0, len(declaredScopes))
	for _, scopeEntry := range declaredScopes {
		declaredNames = append(declaredNames, scopeEntry.scopeName)
	}
	var namedNodes []Node
	ruleNodes := slices.Concat(nodeList(blockCheck.dslBlock, "declarations"), nodeList(blockCheck.dslBlock, "references"))
	for _, ruleNode := range ruleNodes {
		if scopeNode, isFound := ruleNode.Get("scope_name"); isFound {
			namedNodes = append(namedNodes, scopeNode)
		}
	}
	for _, fieldSteps := range [][]string{{"locals", "scope_name"}, {"loops", "scope_name"}} {
		if scopeNode, isFound := nodeAt(blockCheck.dslBlock, fieldSteps...); isFound {
			namedNodes = append(namedNodes, scopeNode)
		}
	}
	for _, scopeNode := range namedNodes {
		scopeName, _ := scopeNode.AsString()
		if slices.Contains(declaredNames, scopeName) {
			continue
		}
		var scopeHelp *string
		if closestScope, isClose := ClosestName(scopeName, declaredNames); isClose {
			suggestionText := "did you mean `" + closestScope + "`?"
			scopeHelp = &suggestionText
		}
		blockCheck.blockProblems = append(blockCheck.blockProblems,
			DiagnosticAt(scopeNode.Position, "scope `"+scopeName+"` is not declared").
				WithLabel("`scopes` declares no scope of this name").
				WithHelp(scopeHelp))
	}
	for _, referenceRule := range nodeList(blockCheck.dslBlock, "references") {
		scopeNode, isFound := referenceRule.Get("scope_name")
		if !isFound || scopeNode.Kind != NodeString {
			continue
		}
		scopeIndex := slices.IndexFunc(declaredScopes, func(scopeEntry declaredScope) bool {
			return scopeEntry.scopeName == scopeNode.Text
		})
		if scopeIndex < 0 {
			continue
		}
		groupNode, hasGroupField := referenceRule.Get("scope_group")
		hasGroup := hasGroupField && groupNode.Kind == NodeString
		reportedNode := referenceRule
		if hasGroupField {
			reportedNode = groupNode
		}
		if declaredScopes[scopeIndex].namedByParentKey && !hasGroup {
			blockCheck.reportProblem(reportedNode, "`scope_group` is required", "scope `"+scopeNode.Text+"` is named by a parent key")
		}
		if !declaredScopes[scopeIndex].namedByParentKey && hasGroup {
			blockCheck.reportProblem(reportedNode,
				"`scope_group` must be `null`",
				"scope `"+scopeNode.Text+"` is not named by a parent key")
		}
	}
}

func (blockCheck *blockChecks) checkLocalsAndLoops() {
	if scopeNode, isFound := nodeAt(blockCheck.dslBlock, "locals", "scope_name"); isFound {
		scopeName, _ := scopeNode.AsString()
		isDeclaredByKey := slices.ContainsFunc(nodeList(blockCheck.dslBlock, "declarations"), func(declarationRule Node) bool {
			everyNameNode, _ := declarationRule.Get("declares_every_name")
			return fieldTextIs(declarationRule, "scope_name", scopeName) && fieldTextIs(declarationRule, "name_source", "key") &&
				!(everyNameNode.Kind == NodeBool && everyNameNode.Flag)
		})
		if !isDeclaredByKey {
			blockCheck.reportProblem(scopeNode,
				"no declaration rule reads `"+scopeName+"` names from keys",
				"a local is declared by its key")
		}
		blockCheck.checkReadByReferenceRule(scopeNode, scopeName, "a local")
	}
	if scopeNode, isFound := nodeAt(blockCheck.dslBlock, "loops", "scope_name"); isFound {
		scopeName, _ := scopeNode.AsString()
		blockCheck.checkReadByReferenceRule(scopeNode, scopeName, "a loop variable")
	}
}

func (blockCheck *blockChecks) checkReadByReferenceRule(scopeNode Node, scopeName, readerName string) {
	isRead := slices.ContainsFunc(nodeList(blockCheck.dslBlock, "references"), func(referenceRule Node) bool {
		return fieldTextIs(referenceRule, "scope_name", scopeName)
	})
	if !isRead {
		blockCheck.reportProblem(scopeNode,
			"no reference rule reads `"+scopeName+"`",
			readerName+" is read by a reference rule of its scope")
	}
}

func (blockCheck *blockChecks) checkTagKeys() {
	tagEntries := nodeList(blockCheck.dslBlock, "tag_keys")
	for entryIndex, tagEntry := range tagEntries {
		for _, entryField := range []string{"tag", "key"} {
			fieldNode, isFound := tagEntry.Get(entryField)
			if !isFound {
				continue
			}
			isRepeated := slices.ContainsFunc(tagEntries[:entryIndex], func(earlierEntry Node) bool {
				return sameFieldText(earlierEntry, entryField, fieldNode)
			})
			if isRepeated {
				fieldText, _ := fieldNode.AsString()
				blockCheck.reportProblem(fieldNode,
					entryField+" `"+fieldText+"` is listed more than once",
					"an earlier entry of `tag_keys` has this "+entryField)
			}
		}
	}
}

func (blockCheck *blockChecks) declaredScopes() []declaredScope {
	scopesNode, _ := blockCheck.dslBlock.Get("scopes")
	var declaredScopes []declaredScope
	for _, scopeEntry := range scopesNode.Entries {
		parentKeyNode, _ := scopeEntry.Value.Get("named_by_parent_key")
		declaredScopes = append(declaredScopes, declaredScope{
			scopeName:        scopeEntry.Key,
			namedByParentKey: parentKeyNode.Kind == NodeBool && parentKeyNode.Flag,
		})
	}
	return declaredScopes
}

func nodeAt(startNode Node, fieldSteps ...string) (Node, bool) {
	currentNode := startNode
	for _, fieldStep := range fieldSteps {
		nextNode, isFound := currentNode.Get(fieldStep)
		if !isFound {
			return Node{}, false
		}
		currentNode = nextNode
	}
	return currentNode, true
}

func nodeList(startNode Node, fieldSteps ...string) []Node {
	listNode, isFound := nodeAt(startNode, fieldSteps...)
	if !isFound || listNode.Kind != NodeList {
		return nil
	}
	return listNode.Items
}

func mapValues(startNode Node, fieldSteps ...string) []Node {
	mapNode, isFound := nodeAt(startNode, fieldSteps...)
	if !isFound || mapNode.Kind != NodeMap {
		return nil
	}
	entryValues := make([]Node, 0, len(mapNode.Entries))
	for _, mapEntry := range mapNode.Entries {
		entryValues = append(entryValues, mapEntry.Value)
	}
	return entryValues
}

func sameFieldText(holderNode Node, holderField string, otherNode Node) bool {
	fieldNode, _ := holderNode.Get(holderField)
	return sameScalarText(fieldNode, otherNode)
}

func fieldTextIs(holderNode Node, holderField, expectedText string) bool {
	fieldNode, isFound := holderNode.Get(holderField)
	return isFound && fieldNode.Kind == NodeString && fieldNode.Text == expectedText
}

func sameScalarText(firstNode, secondNode Node) bool {
	firstText, firstIsText := firstNode.AsString()
	secondText, secondIsText := secondNode.AsString()
	return firstIsText == secondIsText && (!firstIsText || firstText == secondText)
}
