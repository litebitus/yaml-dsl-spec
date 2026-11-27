package schema

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/litebitus/yaml-dsl-spec/go/core"
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
	case *core.OrderedObject:
		idText, isText := typedNode.FieldValues["$id"].(string)
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
		for _, schemaKeyword := range typedNode.KeyOrder {
			collectStatedLocations(typedNode.FieldValues[schemaKeyword], baseLocation, false, statedLocations)
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
		ancestorNode, _ := core.JSONPointerValue(wholeSchema, ancestorPointer)
		idValue, _ := core.ObjectField(ancestorNode, "$id")
		_, isIgnoredBesideReference := core.ObjectField(ancestorNode, "$ref")
		isIgnoredBesideReference = isIgnoredBesideReference && readDraft == draftSeven
		if idText, isText := idValue.(string); isText && !strings.HasPrefix(idText, "#") && !isIgnoredBesideReference {
			resourcePointer = ancestorPointer
		}
	}
	return resourcePointer
}
