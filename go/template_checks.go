package yamldslspec

import (
	"slices"

	"github.com/litebitus/yaml-dsl-spec/go/core"
)

type templateChecks struct {
	blockChecks
	templateBlock Node
	dslBlocks     []Node
}

func (templateCheck *templateChecks) allProblems(earlierTemplates []Node) []Diagnostic {
	templateCheck.checkRepeatedID(earlierTemplates)
	templateCheck.checkTemplatedDSL()
	if startNode, isFound := nodeAt(templateCheck.templateBlock, "loop", "start"); isFound {
		templateCheck.checkPattern(startNode, []expectedGroup{
			{groupName: core.LoopVariablesGroup, heldMeaning: "the loop variables a loop binds"},
		})
	}
	if variableNode, isFound := nodeAt(templateCheck.templateBlock, "loop", "variable"); isFound {
		templateCheck.checkPattern(variableNode, nil)
	}
	if variableNode, isFound := templateCheck.templateBlock.Get("variable"); isFound {
		templateCheck.checkPattern(variableNode, nil)
	}
	return templateCheck.blockProblems
}

func (templateCheck *templateChecks) checkRepeatedID(earlierTemplates []Node) {
	idNode, hasID := templateCheck.templateBlock.Get("id")
	idText, isText := idNode.AsString()
	if !hasID || !isText {
		return
	}
	repeatedLabel := ""
	if slices.ContainsFunc(templateCheck.dslBlocks, func(dslBlock Node) bool { return fieldTextIs(dslBlock, "id", idText) }) {
		repeatedLabel = "a DSL of this file has this `id`"
	} else if slices.ContainsFunc(earlierTemplates, func(earlierBlock Node) bool { return fieldTextIs(earlierBlock, "id", idText) }) {
		repeatedLabel = "an earlier template language of this file has this `id`"
	}
	if repeatedLabel != "" {
		templateCheck.reportProblem(idNode, "template language `"+idText+"` is declared more than once", repeatedLabel)
	}
}

func (templateCheck *templateChecks) checkTemplatedDSL() {
	dslNode, hasDSL := templateCheck.templateBlock.Get("dsl")
	dslText, isText := dslNode.AsString()
	if !hasDSL || !isText {
		return
	}
	var declaredIDs []string
	for _, dslBlock := range templateCheck.dslBlocks {
		if idNode, isFound := dslBlock.Get("id"); isFound && idNode.Kind == NodeString {
			declaredIDs = append(declaredIDs, idNode.Text)
		}
	}
	if slices.Contains(declaredIDs, dslText) {
		return
	}
	var dslHelp *string
	if closestID, isClose := ClosestName(dslText, declaredIDs); isClose {
		suggestionText := "did you mean `" + closestID + "`?"
		dslHelp = &suggestionText
	}
	templateCheck.blockProblems = append(templateCheck.blockProblems,
		DiagnosticAt(dslNode.Position, "DSL `"+dslText+"` is not declared").
			WithLabel("`dsls` declares no DSL of this name").
			WithHelp(dslHelp))
}
