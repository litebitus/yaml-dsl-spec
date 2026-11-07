package yamldslspec

import "testing"

func parsedNode(testingContext *testing.T, yamlText string, tagKeys []TagKey) Node {
	rootNode, readProblem := ParseYAML("mock.yml", yamlText, tagKeys)
	if readProblem != nil {
		testingContext.Fatal(readProblem)
	}
	return rootNode
}

func TestKeysKeepTheirOrderAndPositions(testingContext *testing.T) {
	rootNode := parsedNode(testingContext, "zeta: 1\nalpha:\n  nested: true\n", nil)
	if rootNode.Entries[0].Key != "zeta" || rootNode.Entries[1].Key != "alpha" || rootNode.Entries[1].KeyPosition.Line != 2 {
		testingContext.Errorf("entries %+v", rootNode.Entries)
	}
	nestedNode, _ := rootNode.Entries[1].Value.Get("nested")
	if nestedNode.Kind != NodeBool || !nestedNode.Flag || nestedNode.Position.Column != 11 {
		testingContext.Errorf("nested %+v", nestedNode)
	}
}

func TestPlainScalarsReadByTheCoreSchema(testingContext *testing.T) {
	rootNode := parsedNode(testingContext, "a: 30\nb: 1.5\nc: \"30\"\nd: ~\ne: mock\nf: yes\n", nil)
	expectedKinds := map[string]NodeKind{
		"a": NodeNumber,
		"b": NodeNumber,
		"c": NodeString,
		"d": NodeNull,
		"e": NodeString,
		"f": NodeString,
	}
	for entryKey, expectedKind := range expectedKinds {
		if entryValue, _ := rootNode.Get(entryKey); entryValue.Kind != expectedKind {
			testingContext.Errorf("%s reads as %v", entryKey, entryValue.Kind)
		}
	}
}

func TestAListedTagReadsAsAMapHoldingItsKey(testingContext *testing.T) {
	rootNode := parsedNode(testingContext, "bucket: !Ref MockBucket\n", []TagKey{{Tag: "!Ref", Key: "Ref"}})
	bucketNode, _ := rootNode.Get("bucket")
	referenceNode, isFound := bucketNode.Get("Ref")
	if !isFound || referenceNode.Text != "MockBucket" {
		testingContext.Errorf("bucket %+v", bucketNode)
	}
}

func TestADuplicateKeyIsRefusedBeforeALaterSyntaxError(testingContext *testing.T) {
	_, readProblem := ParseYAML("mock.yml", "a: 1\na: 2\nb: [1\n", nil)
	if readProblem == nil || readProblem.Message != "duplicate key `a`" || readProblem.Position.Line != 2 {
		testingContext.Errorf("problem %v", readProblem)
	}
}

func TestAnEmptyValueStandsAtItsIndicator(testingContext *testing.T) {
	rootNode := parsedNode(testingContext, "a:\nlist:\n  - \n", nil)
	emptyValue, _ := rootNode.Get("a")
	listValue, _ := rootNode.Get("list")
	if emptyValue.Position.Column != 2 || listValue.Items[0].Position.Column != 5 {
		testingContext.Errorf("empty value at %v, empty item at %v", emptyValue.Position, listValue.Items[0].Position)
	}
}

func TestABlockScalarStandsAtItsContent(testingContext *testing.T) {
	rootNode := parsedNode(testingContext, "m:\n  x: |1\n    a\n  y: >\n\n     b\n", nil)
	holderNode, _ := rootNode.Get("m")
	explicitValue, _ := holderNode.Get("x")
	detectedValue, _ := holderNode.Get("y")
	if explicitValue.Position != NewPosition("mock.yml", 3, 4) || detectedValue.Position != NewPosition("mock.yml", 6, 6) {
		testingContext.Errorf("block scalars at %v and %v", explicitValue.Position, detectedValue.Position)
	}
}

func TestAnyYAMLVersionDirectiveIsRead(testingContext *testing.T) {
	rootNode := parsedNode(testingContext, "%YAML 1.2\n---\na: 1\n", nil)
	if entryValue, _ := rootNode.Get("a"); entryValue.Text != "1" || entryValue.Position.Line != 3 {
		testingContext.Errorf("node %+v", rootNode)
	}
}
