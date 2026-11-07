package yamldslspec

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

var readsBelowKeywords = []string{"anyOf", "oneOf", "not", "contains", "unevaluatedItems", "unevaluatedProperties", "uniqueItems"}

type failedCheck struct {
	failedKeyword   string
	instancePointer string
	schemaPointer   string
	errorKind       jsonschema.ErrorKind
	missingField    *string
	fieldNames      []string
	itemCount       int
}

type failureCollector struct {
	loadedSchema     *LoadedSchema
	documentJSON     any
	documentRecord   *instanceRecord
	failedChecks     []failedCheck
	usedHolders      map[string]bool
	unevaluatedNames map[string]map[string]bool
	unevaluatedAt    map[string]failedCheck
}

func newFailureCollector(loadedSchema *LoadedSchema, documentJSON any, documentRecord *instanceRecord) *failureCollector {
	return &failureCollector{
		loadedSchema:     loadedSchema,
		documentJSON:     documentJSON,
		documentRecord:   documentRecord,
		usedHolders:      map[string]bool{},
		unevaluatedNames: map[string]map[string]bool{},
		unevaluatedAt:    map[string]failedCheck{},
	}
}

func (loadedSchema *LoadedSchema) withQuotedValuesRead(documentJSON any, validationRules ValidationRules) any {
	if len(validationRules.QuotedValueTypes) == 0 {
		return documentJSON
	}
	for {
		validationError, _ := loadedSchema.compiledSchema.Validate(documentJSON).(*jsonschema.ValidationError)
		if validationError == nil {
			return documentJSON
		}
		quotedReadings := map[string]any{}
		var readingOrder []string
		loadedSchema.collectQuotedReadings(validationError, documentJSON, validationRules, quotedReadings, &readingOrder)
		if len(quotedReadings) == 0 {
			return documentJSON
		}
		for _, readingPointer := range readingOrder {
			documentJSON = withValueAt(documentJSON, readingPointer, quotedReadings[readingPointer])
		}
	}
}

func (loadedSchema *LoadedSchema) collectQuotedReadings(
	errorNode *jsonschema.ValidationError,
	documentJSON any,
	validationRules ValidationRules,
	quotedReadings map[string]any,
	readingOrder *[]string,
) {
	switch errorNode.ErrorKind.(type) {
	case *kind.Type:
		instancePointer := pointerOfLocation(errorNode.InstanceLocation)
		instanceValue, _ := jsonPointerValue(documentJSON, instancePointer)
		quotedText, isText := instanceValue.(string)
		if !isText {
			return
		}
		expectedTypes := loadedSchema.expectedTypes(loadedSchema.keywordPointer(errorNode))
		readValue, isSpelled := validationRules.quotedValue(quotedText, expectedTypes)
		if _, isRead := quotedReadings[instancePointer]; isSpelled && !isRead {
			quotedReadings[instancePointer] = readValue
			*readingOrder = append(*readingOrder, instancePointer)
		}
	case *kind.Not, *kind.Contains, *kind.MinContains, *kind.PropertyNames:
	default:
		for _, causeNode := range errorNode.Causes {
			loadedSchema.collectQuotedReadings(causeNode, documentJSON, validationRules, quotedReadings, readingOrder)
		}
	}
}

func withValueAt(documentJSON any, valuePointer string, replacementValue any) any {
	if valuePointer == "" {
		return replacementValue
	}
	lastSlash := strings.LastIndexByte(valuePointer, '/')
	holderValue, isFound := jsonPointerValue(documentJSON, valuePointer[:lastSlash])
	if !isFound {
		return documentJSON
	}
	lastStep := unescapePointer(valuePointer[lastSlash+1:])
	switch typedHolder := holderValue.(type) {
	case map[string]any:
		typedHolder[lastStep] = replacementValue
	case []any:
		if itemIndex, indexError := strconv.Atoi(lastStep); indexError == nil && itemIndex >= 0 && itemIndex < len(typedHolder) {
			typedHolder[itemIndex] = replacementValue
		}
	}
	return documentJSON
}

func pointerOfLocation(instanceLocation []string) string {
	var pointerBuilder strings.Builder
	for _, locationStep := range instanceLocation {
		pointerBuilder.WriteByte('/')
		pointerBuilder.WriteString(escapePointer(locationStep))
	}
	return pointerBuilder.String()
}

func (loadedSchema *LoadedSchema) nodePointer(errorNode *jsonschema.ValidationError) string {
	_, urlFragment, _ := strings.Cut(errorNode.SchemaURL, "#")
	if decodedFragment, decodeError := url.PathUnescape(urlFragment); decodeError == nil {
		urlFragment = decodedFragment
	}
	return loadedSchema.realPointer(urlFragment)
}

func (loadedSchema *LoadedSchema) keywordPointer(errorNode *jsonschema.ValidationError) string {
	keywordPointer := loadedSchema.nodePointer(errorNode)
	switch errorNode.ErrorKind.(type) {
	case *kind.Not:
		return loadedSchema.realPointer(keywordPointer + "/not")
	case *kind.FalseSchema:
		return keywordPointer
	}
	for _, keywordStep := range errorNode.ErrorKind.KeywordPath() {
		keywordPointer += "/" + escapePointer(keywordStep)
	}
	return loadedSchema.realPointer(keywordPointer)
}

func (loadedSchema *LoadedSchema) failuresOf(documentJSON any, documentRecord *instanceRecord) []failedCheck {
	validationError, _ := loadedSchema.compiledSchema.Validate(documentJSON).(*jsonschema.ValidationError)
	if validationError == nil {
		return nil
	}
	checkCollector := newFailureCollector(loadedSchema, documentJSON, documentRecord)
	checkCollector.collectNode(validationError, nil)
	unevaluatedKeys := make([]string, 0, len(checkCollector.unevaluatedAt))
	for unevaluatedKey := range checkCollector.unevaluatedAt {
		unevaluatedKeys = append(unevaluatedKeys, unevaluatedKey)
	}
	slices.Sort(unevaluatedKeys)
	for _, unevaluatedKey := range unevaluatedKeys {
		unevaluatedCheck := checkCollector.unevaluatedAt[unevaluatedKey]
		for unexpectedName := range checkCollector.unevaluatedNames[unevaluatedKey] {
			unevaluatedCheck.fieldNames = append(unevaluatedCheck.fieldNames, unexpectedName)
		}
		slices.Sort(unevaluatedCheck.fieldNames)
		unevaluatedCheck.itemCount = len(unevaluatedCheck.fieldNames)
		checkCollector.failedChecks = append(checkCollector.failedChecks, unevaluatedCheck)
	}
	return checkCollector.failedChecks
}

func (checkCollector *failureCollector) addCheck(addedCheck failedCheck) {
	checkCollector.failedChecks = append(checkCollector.failedChecks, addedCheck)
}

func (checkCollector *failureCollector) collectNode(errorNode *jsonschema.ValidationError, reliableLocation []string) {
	loadedSchema := checkCollector.loadedSchema
	if _, isPropertyNames := errorNode.ErrorKind.(*kind.PropertyNames); !isPropertyNames {
		reliableLocation = errorNode.InstanceLocation
		if checkCollector.collectUnevaluated(errorNode) {
			return
		}
	}
	nodeCheck := failedCheck{
		instancePointer: pointerOfLocation(errorNode.InstanceLocation),
		schemaPointer:   loadedSchema.keywordPointer(errorNode),
		errorKind:       errorNode.ErrorKind,
	}
	switch errorKind := errorNode.ErrorKind.(type) {
	case *kind.Schema, *kind.Group, *kind.Reference, *kind.AllOf:
		for _, causeNode := range errorNode.Causes {
			checkCollector.collectNode(causeNode, reliableLocation)
		}
	case *kind.PropertyNames:
		checkCollector.collectPropertyName(errorNode, errorKind.Property, reliableLocation)
	case *kind.Required:
		for _, missingName := range errorKind.Missing {
			nodeCheck.failedKeyword, nodeCheck.missingField = "required", &missingName
			checkCollector.addCheck(nodeCheck)
		}
	case *kind.Dependency:
		dependencyPointer := loadedSchema.nodePointer(errorNode) + "/dependencies/" + escapePointer(errorKind.Prop)
		checkCollector.addDependency(nodeCheck, dependencyPointer, errorKind.Missing)
	case *kind.DependentRequired:
		dependencyPointer := loadedSchema.nodePointer(errorNode) + "/dependentRequired/" + escapePointer(errorKind.Prop)
		checkCollector.addDependency(nodeCheck, dependencyPointer, errorKind.Missing)
	case *kind.AdditionalProperties:
		nodeCheck.failedKeyword, nodeCheck.fieldNames = "additionalProperties", errorKind.Properties
		checkCollector.addCheck(nodeCheck)
	case *kind.Contains, *kind.MinContains, *kind.MaxContains:
		nodeCheck.failedKeyword, nodeCheck.schemaPointer = "contains", loadedSchema.nodePointer(errorNode)+"/contains"
		checkCollector.addCheck(nodeCheck)
	case *kind.Not:
		nodeCheck.failedKeyword = "not"
		checkCollector.addCheck(nodeCheck)
	case *kind.FalseSchema:
		checkCollector.collectFalseSchema(nodeCheck, errorNode.InstanceLocation)
	case *kind.RefCycle:
	default:
		nodeCheck.failedKeyword = failureKeyword(errorNode.ErrorKind)
		checkCollector.addCheck(nodeCheck)
	}
}

func (checkCollector *failureCollector) addDependency(nodeCheck failedCheck, dependencyPointer string, missingNames []string) {
	for _, missingName := range missingNames {
		nodeCheck.failedKeyword, nodeCheck.schemaPointer, nodeCheck.missingField = "required", dependencyPointer, &missingName
		checkCollector.addCheck(nodeCheck)
	}
}

func failureKeyword(errorKind jsonschema.ErrorKind) string {
	if keywordPath := errorKind.KeywordPath(); len(keywordPath) > 0 {
		return keywordPath[0]
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", errorKind), "*kind.")
}

func (checkCollector *failureCollector) collectFalseSchema(nodeCheck failedCheck, instanceLocation []string) {
	nodeCheck.failedKeyword = "falseSchema"
	for _, listKeyword := range []string{"items", "additionalItems"} {
		if strings.HasSuffix(nodeCheck.schemaPointer, "/"+listKeyword) {
			nodeCheck.failedKeyword = listKeyword
			nodeCheck.instancePointer = pointerOfLocation(instanceLocation[:max(len(instanceLocation)-1, 0)])
		}
	}
	checkCollector.addCheck(nodeCheck)
}

func (checkCollector *failureCollector) collectUnevaluated(errorNode *jsonschema.ValidationError) bool {
	unevaluatedKeyword, holderPointer, stepsBelow, isFound := unevaluatedStep(checkCollector.loadedSchema.nodePointer(errorNode))
	if !isFound || len(errorNode.InstanceLocation) < stepsBelow+1 {
		return false
	}
	holderDepth := len(errorNode.InstanceLocation) - stepsBelow - 1
	instancePointer := pointerOfLocation(errorNode.InstanceLocation[:holderDepth])
	collectorKey := instancePointer + "\x00" + holderPointer + "\x00" + unevaluatedKeyword
	if _, isSeen := checkCollector.unevaluatedAt[collectorKey]; !isSeen {
		checkCollector.unevaluatedAt[collectorKey] = failedCheck{
			failedKeyword:   unevaluatedKeyword,
			instancePointer: instancePointer,
			schemaPointer:   holderPointer + "/" + unevaluatedKeyword,
			errorKind:       errorNode.ErrorKind,
		}
		checkCollector.unevaluatedNames[collectorKey] = map[string]bool{}
	}
	checkCollector.unevaluatedNames[collectorKey][errorNode.InstanceLocation[holderDepth]] = true
	return true
}

func unevaluatedStep(nodePointer string) (string, string, int, bool) {
	if nodePointer == "" {
		return "", "", 0, false
	}
	pointerSteps := strings.Split(nodePointer[1:], "/")
	for stepIndex := 0; stepIndex < len(pointerSteps); stepIndex++ {
		switch pointerStep := pointerSteps[stepIndex]; pointerStep {
		case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependencies":
			stepIndex++
		case "unevaluatedProperties", "unevaluatedItems":
			holderPointer := ""
			if stepIndex > 0 {
				holderPointer = "/" + strings.Join(pointerSteps[:stepIndex], "/")
			}
			return pointerStep, holderPointer, instanceStepsOf(pointerSteps[stepIndex+1:]), true
		}
	}
	return "", "", 0, false
}

func instanceStepsOf(schemaSteps []string) int {
	instanceSteps := 0
	for stepIndex := 0; stepIndex < len(schemaSteps); stepIndex++ {
		switch schemaSteps[stepIndex] {
		case "properties", "patternProperties", "prefixItems":
			instanceSteps++
			stepIndex++
		case "items":
			instanceSteps++
			if stepIndex+1 < len(schemaSteps) && schemaSteps[stepIndex+1] != "" && onlyDigits(schemaSteps[stepIndex+1]) {
				stepIndex++
			}
		case "additionalProperties", "additionalItems", "unevaluatedProperties", "unevaluatedItems", "contains":
			instanceSteps++
		case "allOf", "anyOf", "oneOf", "$defs", "definitions", "dependentSchemas", "dependencies":
			stepIndex++
		}
	}
	return instanceSteps
}

func (checkCollector *failureCollector) collectPropertyName(
	errorNode *jsonschema.ValidationError,
	propertyName string,
	reliableLocation []string,
) {
	propertyNamesPointer := checkCollector.loadedSchema.nodePointer(errorNode)
	holderSchemaPointer := strings.TrimSuffix(propertyNamesPointer, "/propertyNames")
	holderDepth := len(errorNode.InstanceLocation)
	holderPointer, isLocated := checkCollector.propertyNamesHolder(propertyName, holderDepth, reliableLocation, holderSchemaPointer)
	if !isLocated {
		return
	}
	innerCollector := newFailureCollector(checkCollector.loadedSchema, propertyName, checkCollector.documentRecord)
	for _, causeNode := range errorNode.Causes {
		innerCollector.collectNode(causeNode, nil)
	}
	innerPointer := propertyNamesPointer
	if len(innerCollector.failedChecks) > 0 {
		innerPointer = innerCollector.failedChecks[0].schemaPointer
	}
	checkCollector.addCheck(failedCheck{
		failedKeyword:   "propertyNames",
		instancePointer: holderPointer,
		schemaPointer:   innerPointer,
		errorKind:       errorNode.ErrorKind,
		fieldNames:      []string{propertyName},
	})
}

func (checkCollector *failureCollector) propertyNamesHolder(
	propertyName string,
	holderDepth int,
	reliableLocation []string,
	holderSchemaPointer string,
) (string, bool) {
	basePointer := pointerOfLocation(reliableLocation)
	if len(reliableLocation) >= holderDepth {
		return basePointer, true
	}
	baseValue, isFound := jsonPointerValue(checkCollector.documentJSON, basePointer)
	if !isFound {
		return "", false
	}
	var holderCandidates []string
	collectHolders(baseValue, basePointer, holderDepth-len(reliableLocation), propertyName, &holderCandidates)
	hintedStep := ""
	if schemaSteps := strings.Split(holderSchemaPointer, "/"); len(schemaSteps) >= 2 {
		switch schemaSteps[len(schemaSteps)-2] {
		case "properties", "items", "prefixItems":
			hintedStep = unescapePointer(schemaSteps[len(schemaSteps)-1])
		}
	}
	slices.SortFunc(holderCandidates, func(firstCandidate, secondCandidate string) int {
		return comparePositions(
			checkCollector.documentRecord.valuePosition(firstCandidate),
			checkCollector.documentRecord.valuePosition(secondCandidate),
		)
	})
	for _, holderCandidate := range holderCandidates {
		lastStep := unescapePointer(holderCandidate[strings.LastIndexByte(holderCandidate, '/')+1:])
		usedKey := holderSchemaPointer + "\x00" + propertyName + "\x00" + holderCandidate
		if (hintedStep != "" && lastStep != hintedStep) || checkCollector.usedHolders[usedKey] {
			continue
		}
		checkCollector.usedHolders[usedKey] = true
		return holderCandidate, true
	}
	return "", false
}

func collectHolders(currentValue any, currentPointer string, depthLeft int, propertyName string, holderCandidates *[]string) {
	if depthLeft == 0 {
		if holderFields, isObject := currentValue.(map[string]any); isObject {
			if _, holdsName := holderFields[propertyName]; holdsName {
				*holderCandidates = append(*holderCandidates, currentPointer)
			}
		}
		return
	}
	switch typedValue := currentValue.(type) {
	case map[string]any:
		for fieldKey, fieldValue := range typedValue {
			collectHolders(fieldValue, currentPointer+"/"+escapePointer(fieldKey), depthLeft-1, propertyName, holderCandidates)
		}
	case []any:
		for itemIndex, itemValue := range typedValue {
			collectHolders(itemValue, currentPointer+"/"+strconv.Itoa(itemIndex), depthLeft-1, propertyName, holderCandidates)
		}
	}
}
