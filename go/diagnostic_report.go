package yamldslspec

// Diagnostic is a problem found in a file, at the value it is about where there is one.
type Diagnostic struct {
	Position *Position
	Message  string
	Notes    *DiagnosticNotes
}

// DiagnosticNotes is what a report writes beside a problem: its label, rule, schema and help.
type DiagnosticNotes struct {
	Label  *string
	Rule   *string
	Schema *string
	Help   *string
}

// DiagnosticAt is a problem placed at a position.
func DiagnosticAt(problemPosition Position, problemMessage string) Diagnostic {
	return Diagnostic{Position: &problemPosition, Message: problemMessage}
}

// UnplacedDiagnostic is a problem with no position.
func UnplacedDiagnostic(problemMessage string) Diagnostic {
	return Diagnostic{Message: problemMessage}
}

// WithLabel notes what is wrong with the text at the position.
func (reportedProblem Diagnostic) WithLabel(labelText string) Diagnostic {
	copiedNotes := reportedProblem.copiedNotes()
	copiedNotes.Label = &labelText
	reportedProblem.Notes = copiedNotes
	return reportedProblem
}

// WithRule notes the schema rule broken: its keyword and where the schema holds it.
func (reportedProblem Diagnostic) WithRule(ruleText string) Diagnostic {
	copiedNotes := reportedProblem.copiedNotes()
	copiedNotes.Rule = &ruleText
	reportedProblem.Notes = copiedNotes
	return reportedProblem
}

// WithSchema notes the schema's own words, as written, where there are any.
func (reportedProblem Diagnostic) WithSchema(schemaText *string) Diagnostic {
	if schemaText == nil {
		return reportedProblem
	}
	copiedNotes := reportedProblem.copiedNotes()
	copiedNotes.Schema = schemaText
	reportedProblem.Notes = copiedNotes
	return reportedProblem
}

// WithHelp notes how the problem may be fixed, where there is a way.
func (reportedProblem Diagnostic) WithHelp(helpText *string) Diagnostic {
	if helpText == nil {
		return reportedProblem
	}
	copiedNotes := reportedProblem.copiedNotes()
	copiedNotes.Help = helpText
	reportedProblem.Notes = copiedNotes
	return reportedProblem
}

func (reportedProblem Diagnostic) copiedNotes() *DiagnosticNotes {
	if reportedProblem.Notes == nil {
		return &DiagnosticNotes{}
	}
	notesCopy := *reportedProblem.Notes
	return &notesCopy
}

func (reportedProblem *Diagnostic) Error() string {
	if reportedProblem.Position == nil {
		return reportedProblem.Message
	}
	return reportedProblem.Position.String() + ": " + reportedProblem.Message
}

func (reportedProblem Diagnostic) String() string {
	return reportedProblem.Error()
}
