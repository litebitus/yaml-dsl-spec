package yamldslspec

import (
	"net/url"
	"strconv"
	"strings"
)

func unstatedSchemaLocation(schemaValue any) string {
	for candidateIndex := 0; ; candidateIndex++ {
		candidateLocation := compiledSchemaDirectory + "document-schema-" + strconv.Itoa(candidateIndex) + ".json"
		statedLocations := map[string]bool{}
		collectStatedLocations(schemaValue, candidateLocation, true, statedLocations)
		if !statedLocations[candidateLocation] {
			return candidateLocation
		}
	}
}

func collectStatedLocations(schemaNode any, baseLocation string, isDocumentRoot bool, statedLocations map[string]bool) {
	switch typedNode := schemaNode.(type) {
	case []any:
		for _, listedNode := range typedNode {
			collectStatedLocations(listedNode, baseLocation, false, statedLocations)
		}
	case *orderedObject:
		idText, isText := typedNode.fieldValues["$id"].(string)
		baseURL, baseError := url.Parse(baseLocation)
		idURL, idError := url.Parse(idText)
		if isText && idText != "" && !strings.HasPrefix(idText, "#") && baseError == nil && idError == nil {
			resolvedURL := baseURL.ResolveReference(idURL)
			resolvedURL.Fragment = ""
			baseLocation = resolvedURL.String()
			if !isDocumentRoot {
				statedLocations[baseLocation] = true
			}
		}
		for _, schemaKeyword := range typedNode.keyOrder {
			collectStatedLocations(typedNode.fieldValues[schemaKeyword], baseLocation, false, statedLocations)
		}
	}
}

func referenceTargetPointer(wholeSchema any, holderPointer, referenceText string) (string, bool) {
	fragmentPointer, isFragment := strings.CutPrefix(referenceText, "#")
	if !isFragment || (fragmentPointer != "" && !strings.HasPrefix(fragmentPointer, "/")) {
		return "", false
	}
	if decodedPointer, decodeError := url.PathUnescape(fragmentPointer); decodeError == nil {
		fragmentPointer = decodedPointer
	}
	return resourceRootPointer(wholeSchema, holderPointer) + fragmentPointer, true
}

func resourceRootPointer(wholeSchema any, nodePointer string) string {
	readDraft, _ := schemaDraftOf(wholeSchema)
	resourcePointer := ""
	for stepEnd := 0; stepEnd <= len(nodePointer); stepEnd++ {
		if stepEnd < len(nodePointer) && nodePointer[stepEnd] != '/' {
			continue
		}
		ancestorPointer := nodePointer[:stepEnd]
		ancestorNode, _ := jsonPointerValue(wholeSchema, ancestorPointer)
		idValue, _ := objectField(ancestorNode, "$id")
		_, isIgnoredBesideReference := objectField(ancestorNode, "$ref")
		isIgnoredBesideReference = isIgnoredBesideReference && readDraft == draftSeven
		if idText, isText := idValue.(string); isText && !strings.HasPrefix(idText, "#") && !isIgnoredBesideReference {
			resourcePointer = ancestorPointer
		}
	}
	return resourcePointer
}
