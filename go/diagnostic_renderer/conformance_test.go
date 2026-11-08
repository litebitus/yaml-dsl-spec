package diagnostic_renderer

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	yamldslspec "github.com/litebitus/yaml-dsl-spec/go"
)

const (
	renderingCasesFolder = "../../conformance/diagnostic-rendering"
	positionFileName     = "mock-position-file.yml"
)

type caseDiagnostic struct {
	Line    *int    `json:"line"`
	Column  *int    `json:"column"`
	Message string  `json:"message"`
	Label   *string `json:"label"`
	Rule    *string `json:"rule"`
	Schema  *string `json:"schema"`
	Help    *string `json:"help"`
}

type caseRendering struct {
	Severity   string `json:"severity"`
	SourceName string `json:"source_name"`
	Color      bool   `json:"color"`
	Width      *int   `json:"width"`
}

type caseExpectation struct {
	Lines []string `json:"lines"`
}

func readCaseJSON(testingContext *testing.T, jsonPath string, decodedValue any) {
	fileContent, readError := os.ReadFile(jsonPath)
	if readError != nil {
		testingContext.Fatal(readError)
	}
	if decodeError := json.Unmarshal(fileContent, decodedValue); decodeError != nil {
		testingContext.Fatalf("%s: %v", jsonPath, decodeError)
	}
}

func diagnosticOfCase(fields caseDiagnostic) yamldslspec.Diagnostic {
	renderedProblem := yamldslspec.UnplacedDiagnostic(fields.Message)
	if fields.Line != nil && fields.Column != nil {
		renderedProblem = yamldslspec.DiagnosticAt(yamldslspec.NewPosition(positionFileName, *fields.Line, *fields.Column), fields.Message)
	}
	if fields.Label != nil {
		renderedProblem = renderedProblem.WithLabel(*fields.Label)
	}
	if fields.Rule != nil {
		renderedProblem = renderedProblem.WithRule(*fields.Rule)
	}
	return renderedProblem.WithSchema(fields.Schema).WithHelp(fields.Help)
}

func renderedCase(testingContext *testing.T, caseFolder string) string {
	var fields caseDiagnostic
	readCaseJSON(testingContext, filepath.Join(caseFolder, "diagnostic.json"), &fields)
	var rendering caseRendering
	readCaseJSON(testingContext, filepath.Join(caseFolder, "rendering.json"), &rendering)
	sourceText, readError := os.ReadFile(filepath.Join(caseFolder, "source.yml"))
	if readError != nil && !errors.Is(readError, fs.ErrNotExist) {
		testingContext.Fatal(readError)
	}
	severity := SeverityError
	if rendering.Severity == "warning" {
		severity = SeverityWarning
	}
	style := RenderStyle{Color: rendering.Color}
	if rendering.Width != nil {
		style.Width = *rendering.Width
	}
	return RenderDiagnostic(diagnosticOfCase(fields), severity, DiagnosticSource{Name: rendering.SourceName, Text: string(sourceText)}, style)
}

func TestEveryDiagnosticRenderingCaseRendersItsExpectedText(testingContext *testing.T) {
	directoryEntries, readError := os.ReadDir(renderingCasesFolder)
	if readError != nil {
		testingContext.Fatal(readError)
	}
	caseCount := 0
	for _, directoryEntry := range directoryEntries {
		caseFolder := filepath.Join(renderingCasesFolder, directoryEntry.Name())
		if _, statError := os.Stat(filepath.Join(caseFolder, "diagnostic.json")); statError != nil {
			continue
		}
		caseCount++
		testingContext.Run(directoryEntry.Name(), func(caseContext *testing.T) {
			var expectation caseExpectation
			readCaseJSON(caseContext, filepath.Join(caseFolder, "expected.json"), &expectation)
			actualText := renderedCase(caseContext, caseFolder)
			if expectedText := strings.Join(expectation.Lines, "\n"); actualText != expectedText {
				actualLines, _ := json.MarshalIndent(caseExpectation{Lines: strings.Split(actualText, "\n")}, "", "  ")
				caseContext.Errorf("rendered:\n%s", actualLines)
			}
		})
	}
	if caseCount == 0 {
		testingContext.Fatal("no diagnostic rendering cases")
	}
}

type corpusRendering struct {
	StyleIndex   int    `json:"style"`
	ExpectedText string `json:"expected"`
}

type corpusCase struct {
	CaseText   string            `json:"text"`
	Renderings []corpusRendering `json:"renderings"`
}

type corpusStyle struct {
	Color bool `json:"color"`
	Width *int `json:"width"`
}

type subsetCorpus struct {
	SourceName string        `json:"source_name"`
	Styles     []corpusStyle `json:"styles"`
	Cases      []corpusCase  `json:"cases"`
}

func TestEverySubsetCorpusTextRendersItsExpectedTextAtEachStyle(testingContext *testing.T) {
	var corpus subsetCorpus
	readCaseJSON(testingContext, filepath.Join(renderingCasesFolder, "subset-corpus.json"), &corpus)
	if len(corpus.Cases) == 0 {
		testingContext.Fatal("no subset corpus cases")
	}
	for _, corpusEntry := range corpus.Cases {
		renderedProblem := yamldslspec.DiagnosticAt(yamldslspec.NewPosition(positionFileName, 1, 1), corpusEntry.CaseText)
		source := DiagnosticSource{Name: corpus.SourceName, Text: corpusEntry.CaseText + "\n"}
		for _, rendering := range corpusEntry.Renderings {
			renderingStyle := corpus.Styles[rendering.StyleIndex]
			style := RenderStyle{Color: renderingStyle.Color}
			if renderingStyle.Width != nil {
				style.Width = *renderingStyle.Width
			}
			if actualText := RenderDiagnostic(renderedProblem, SeverityError, source, style); actualText != rendering.ExpectedText {
				testingContext.Errorf("%q at style %d: %q", corpusEntry.CaseText, rendering.StyleIndex, actualText)
			}
		}
	}
}
