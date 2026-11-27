package schema

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/litebitus/yaml-dsl-spec/go/core"
	"github.com/litebitus/yaml-dsl-spec/go/core/schema/resolution"
)

const (
	readLimit              = 60 * time.Second
	daySeconds             = 24 * 60 * 60
	noMatchReason          = "no file matches"
	neverKeptCopyReason    = "the reader confirmed a copy never kept"
	machineCredentialsHelp = "add `yaml_dsl_machine_credentials=true` to read it with the machine's credentials"
)

// SchemaSearchPath is a `schema_search_paths` entry, and the position it is written at, if it has one.
type SchemaSearchPath struct {
	PathText  string
	WrittenAt *core.Position
}

// SchemaFinding is each candidate tried, in order; the schema found, if one is; and the finding's problems, its
// warnings among them.
type SchemaFinding struct {
	TriedCandidates []SchemaCandidate
	FoundSchema     *FoundSchema
	FindingProblems []core.Diagnostic
}

// SchemaCandidate is a location as resolved, and what named it.
type SchemaCandidate struct {
	SchemaLocation string
	NamedBy        CandidateNamer
}

// CandidateNamerKind is what named a candidate.
type CandidateNamerKind int

const (
	// NamedByModeline is a candidate the document's modeline named.
	NamedByModeline CandidateNamerKind = iota + 1
	// NamedBySearchPath is a candidate a search path named.
	NamedBySearchPath
	// NamedByCaller is a candidate the caller named, outside a document.
	NamedByCaller
)

// CandidateNamer is what named a candidate: the modeline at its position, the search path at its index, or the
// caller.
type CandidateNamer struct {
	NamerKind        CandidateNamerKind
	ModelinePosition core.Position
	SearchPathIndex  int
}

// FoundSchema is the name the schema loads under, its text, the fragment its location names, if it names one, and
// the candidate it was read from.
type FoundSchema struct {
	SchemaName     string
	SchemaText     string
	SchemaFragment *string
	FoundCandidate SchemaCandidate
}

// keptCopy is a copy of a remote source's text, in the form every language keeps it in.
type keptCopy struct {
	CopyURL     string `json:"url"`
	CopyVersion any    `json:"copy_version"`
	ConfirmedAt uint64 `json:"confirmed_at"`
	CopyBody    string `json:"body"`
}

// storedCopy is a kept copy as a store holds it, each field nil where the entry lacks it.
type storedCopy struct {
	CopyURL     *string         `json:"url"`
	CopyVersion json.RawMessage `json:"copy_version"`
	ConfirmedAt *uint64         `json:"confirmed_at"`
	CopyBody    *string         `json:"body"`
}

// candidateReadKind is what reading one candidate came to.
type candidateReadKind int

const (
	candidateTextRead candidateReadKind = iota + 1
	candidateSourceMissing
	candidateReadProblem
)

type candidateRead struct {
	readKind       candidateReadKind
	schemaText     string
	missingReason  string
	schemaLocation string
	readProblem    core.Diagnostic
}

// missingCandidate is a candidate whose source is not there, as written, and why.
type missingCandidate struct {
	writtenLocation string
	missingReason   string
}

// namedLocation is a candidate before it is read: what it says, where it says it, and what named it.
type namedLocation struct {
	resolvedAt resolution.ResolvedLocation
	writtenAt  *core.Position
	namedBy    CandidateNamer
}

// DocumentSchema is the schema of the document documentFile, whose text is documentText: its modeline's location
// tried, then each search path in order, a path resolved against the document's folder, or configRoot where it
// starts with `{config_root}`.
func (schemaCache *SchemaCache) DocumentSchema(
	findContext context.Context, documentFile, documentText string, searchPaths []SchemaSearchPath, configRoot string,
) SchemaFinding {
	documentFolder := absolutePath(filepath.Dir(documentFile))
	rootFolder := absolutePath(configRoot)
	foundModeline, hasModeline := resolution.DocumentModeline(documentText)
	fallbackLine := 1
	if hasModeline {
		fallbackLine = foundModeline.LineNumber
	}
	fallbackPosition := core.NewPosition(documentFile, fallbackLine, 1)
	var namedLocations []namedLocation
	if hasModeline {
		modelinePosition := core.NewPosition(documentFile, foundModeline.LineNumber, foundModeline.ColumnNumber)
		namedLocations = append(namedLocations, namedLocation{
			resolvedAt: resolution.ResolveLocation(foundModeline.SchemaURI, documentFolder, rootFolder),
			writtenAt:  &modelinePosition,
			namedBy:    CandidateNamer{NamerKind: NamedByModeline, ModelinePosition: modelinePosition},
		})
	}
	for pathIndex, searchPath := range searchPaths {
		resolvedPath := resolution.ResolveLocation(searchPath.PathText, documentFolder, rootFolder)
		resolvedPath.SchemaFragment = nil
		writtenAt := fallbackPosition
		if searchPath.WrittenAt != nil {
			writtenAt = *searchPath.WrittenAt
		}
		namedLocations = append(namedLocations, namedLocation{
			resolvedAt: resolvedPath,
			writtenAt:  &writtenAt,
			namedBy:    CandidateNamer{NamerKind: NamedBySearchPath, SearchPathIndex: pathIndex},
		})
	}
	schemaFinding, missingCandidates, everyMissing := schemaCache.findingOf(findContext, namedLocations)
	if everyMissing {
		helpLines := make([]string, 0, len(missingCandidates))
		for _, missing := range missingCandidates {
			helpLines = append(helpLines, core.CodeSpan(missing.writtenLocation)+": "+missing.missingReason)
		}
		noSchemaProblem := core.DiagnosticAt(fallbackPosition, "no schema found")
		if len(helpLines) > 0 {
			helpText := strings.Join(helpLines, "\n")
			noSchemaProblem = noSchemaProblem.WithHelp(&helpText)
		}
		schemaFinding.FindingProblems = append(schemaFinding.FindingProblems, noSchemaProblem)
	}
	return schemaFinding
}

// ReadSchema is the schema at a location named outside a document: a path resolved against the working directory,
// a problem placed at writtenAt, or with no position where it is nil.
func (schemaCache *SchemaCache) ReadSchema(
	findContext context.Context, schemaLocation string, writtenAt *core.Position,
) SchemaFinding {
	workingDirectory := absolutePath("")
	callerLocation := namedLocation{
		resolvedAt: resolution.ResolveLocation(schemaLocation, workingDirectory, workingDirectory),
		writtenAt:  writtenAt,
		namedBy:    CandidateNamer{NamerKind: NamedByCaller},
	}
	schemaFinding, missingCandidates, everyMissing := schemaCache.findingOf(
		findContext, []namedLocation{callerLocation})
	if everyMissing && len(missingCandidates) > 0 {
		missingProblem := problemAt(writtenAt, core.CodeSpan(schemaLocation)+" is missing").
			WithLabel(missingCandidates[0].missingReason)
		schemaFinding.FindingProblems = append(schemaFinding.FindingProblems, missingProblem)
	}
	return schemaFinding
}

// findingOf is each location read in turn until one is there, or one's read ends the finding; and, where every one
// is missing, why each is.
func (schemaCache *SchemaCache) findingOf(
	findContext context.Context, namedLocations []namedLocation,
) (SchemaFinding, []missingCandidate, bool) {
	var missingCandidates []missingCandidate
	schemaFinding := SchemaFinding{TriedCandidates: []SchemaCandidate{}, FindingProblems: []core.Diagnostic{}}
	for _, location := range namedLocations {
		readCandidate := schemaCache.candidateReadOf(findContext, location, &schemaFinding.FindingProblems)
		schemaLocation := readCandidate.schemaLocation
		if readCandidate.readKind == candidateReadProblem {
			schemaLocation = location.resolvedAt.LocationURL
		}
		triedCandidate := SchemaCandidate{SchemaLocation: schemaLocation, NamedBy: location.namedBy}
		schemaFinding.TriedCandidates = append(schemaFinding.TriedCandidates, triedCandidate)
		switch readCandidate.readKind {
		case candidateTextRead:
			schemaName := location.resolvedAt.WrittenLocation
			if location.resolvedAt.URLScheme == resolution.FileScheme {
				schemaName = fileNameOf(readCandidate.schemaLocation)
			}
			schemaFinding.FoundSchema = &FoundSchema{
				SchemaName:     schemaName,
				SchemaText:     readCandidate.schemaText,
				SchemaFragment: location.resolvedAt.SchemaFragment,
				FoundCandidate: triedCandidate,
			}
			return schemaFinding, nil, false
		case candidateSourceMissing:
			missingCandidates = append(missingCandidates, missingCandidate{
				writtenLocation: location.resolvedAt.WrittenLocation,
				missingReason:   readCandidate.missingReason,
			})
		default:
			schemaFinding.FindingProblems = append(schemaFinding.FindingProblems, readCandidate.readProblem)
			return schemaFinding, nil, false
		}
	}
	return schemaFinding, missingCandidates, true
}

// candidateReadOf is one location read by the reader of its prefix, a remote source through its copy.
func (schemaCache *SchemaCache) candidateReadOf(
	findContext context.Context, location namedLocation, findingWarnings *[]core.Diagnostic,
) candidateRead {
	resolvedAt := location.resolvedAt
	if resolvedAt.CacheParameterProblem != nil {
		return problemRead(problemAt(location.writtenAt, *resolvedAt.CacheParameterProblem))
	}
	urlReader, hasReader := schemaCache.urlReaderOf(resolvedAt.LocationURL)
	if !hasReader {
		return problemRead(problemAt(location.writtenAt, "no reader reads "+core.CodeSpan(resolvedAt.WrittenLocation)))
	}
	if !urlReader.ReadsRemoteSources() {
		if len(resolvedAt.CacheParameterNames) > 0 {
			return problemRead(problemAt(location.writtenAt, core.CodeSpan(resolvedAt.CacheParameterNames[0])+
				" is not a parameter of a "+core.CodeSpan(resolvedAt.URLScheme)+" URL"))
		}
		return localRead(findContext, urlReader, location)
	}
	return schemaCache.remoteRead(findContext, urlReader, location, findingWarnings)
}

// localRead is a source on the machine, read each time; a `file` pattern's first match.
func localRead(
	findContext context.Context, urlReader resolution.SchemaURLReader, location namedLocation,
) candidateRead {
	resolvedAt := location.resolvedAt
	fetchURL, schemaLocation := resolvedAt.FetchURL, resolvedAt.LocationURL
	if resolvedAt.FilePath != "" && resolution.HasPattern(resolvedAt.FilePath) {
		matchedFile, hasMatch := resolution.FirstMatchingFile(resolvedAt.FilePath)
		if !hasMatch {
			return candidateRead{
				readKind: candidateSourceMissing, missingReason: noMatchReason, schemaLocation: schemaLocation,
			}
		}
		schemaLocation = resolution.FileURLOf(matchedFile)
		fetchURL = schemaLocation
		if _, queryText, hasQuery := strings.Cut(resolvedAt.FetchURL, "?"); hasQuery {
			fetchURL = schemaLocation + "?" + queryText
		}
	}
	readContext, cancelRead := context.WithTimeout(findContext, readLimit)
	defer cancelRead()
	urlAnswer := urlReader.ReadSchemaURL(readContext, resolution.SchemaURLRequest{FetchURL: fetchURL})
	switch readOutcome := urlAnswer.ReadOutcome; readOutcome.OutcomeKind {
	case resolution.OutcomeTextSent:
		return candidateRead{
			readKind: candidateTextRead, schemaText: readOutcome.SchemaText, schemaLocation: schemaLocation,
		}
	case resolution.OutcomeSourceMissing:
		return candidateRead{
			readKind: candidateSourceMissing, missingReason: readOutcome.MissingReason, schemaLocation: schemaLocation,
		}
	case resolution.OutcomeReadRefused:
		return problemRead(
			refusedProblem(location.writtenAt, resolvedAt.WrittenLocation, urlAnswer.ReadRefusals, false))
	case resolution.OutcomeReadFailed:
		return problemRead(failedProblem(location.writtenAt, resolvedAt.WrittenLocation, readOutcome.FailureReason))
	}
	return problemRead(failedProblem(location.writtenAt, resolvedAt.WrittenLocation, neverKeptCopyReason))
}

// remoteRead is a remote source: its kept copy while it is younger than its max age, else what the reader answers,
// read again with the machine's credentials where it is refused and the URL asks for them.
func (schemaCache *SchemaCache) remoteRead(
	findContext context.Context, urlReader resolution.SchemaURLReader, location namedLocation,
	findingWarnings *[]core.Diagnostic,
) candidateRead {
	resolvedAt := location.resolvedAt
	schemaLocation := resolvedAt.LocationURL
	urlLock := schemaCache.fetchURLLock(resolvedAt.FetchURL)
	urlLock.Lock()
	defer urlLock.Unlock()
	storeKey := copyKey(resolvedAt.FetchURL)
	storedKept, hasKept := schemaCache.keptCopyOf(storeKey, resolvedAt.FetchURL)
	secondsNow := uint64(max(time.Now().Unix(), 0))
	textRead := func(schemaText string) candidateRead {
		return candidateRead{readKind: candidateTextRead, schemaText: schemaText, schemaLocation: schemaLocation}
	}
	if hasKept && resolvedAt.MaxAgeDays > 0 &&
		secondsNow-min(secondsNow, storedKept.ConfirmedAt) < resolvedAt.MaxAgeDays*daySeconds {
		return textRead(storedKept.CopyBody)
	}
	askReader := func(machineCredentials bool) resolution.SchemaURLAnswer {
		urlRequest := resolution.SchemaURLRequest{FetchURL: resolvedAt.FetchURL, MachineCredentials: machineCredentials}
		if hasKept {
			urlRequest.KeptCopyVersion = storedKept.CopyVersion
		}
		readContext, cancelRead := context.WithTimeout(findContext, readLimit)
		defer cancelRead()
		return urlReader.ReadSchemaURL(readContext, urlRequest)
	}
	urlAnswer := askReader(false)
	credentialsAsked := urlAnswer.ReadOutcome.OutcomeKind == resolution.OutcomeReadRefused &&
		resolvedAt.MachineCredentials
	if credentialsAsked {
		urlAnswer = askReader(true)
	}
	readOutcome := urlAnswer.ReadOutcome
	if readOutcome.OutcomeKind != resolution.OutcomeReadRefused {
		for _, readRefusal := range urlAnswer.ReadRefusals {
			if readRefusal.CredentialOrigin != nil {
				*findingWarnings = append(*findingWarnings, refusalWarning(location.writtenAt, readRefusal))
			}
		}
	}
	writtenLocation := resolvedAt.WrittenLocation
	switch readOutcome.OutcomeKind {
	case resolution.OutcomeTextSent:
		schemaCache.keepCopy(storeKey, keptCopy{
			CopyURL:     resolvedAt.FetchURL,
			CopyVersion: readOutcome.CopyVersion,
			ConfirmedAt: secondsNow,
			CopyBody:    readOutcome.SchemaText,
		})
		return textRead(readOutcome.SchemaText)
	case resolution.OutcomeCopyConfirmed:
		if !hasKept {
			return problemRead(failedProblem(location.writtenAt, writtenLocation, neverKeptCopyReason))
		}
		storedKept.ConfirmedAt = secondsNow
		schemaCache.keepCopy(storeKey, storedKept)
		return textRead(storedKept.CopyBody)
	case resolution.OutcomeSourceMissing:
		return candidateRead{
			readKind: candidateSourceMissing, missingReason: readOutcome.MissingReason, schemaLocation: schemaLocation,
		}
	case resolution.OutcomeReadRefused:
		readRefused := refusedProblem(location.writtenAt, writtenLocation, urlAnswer.ReadRefusals, credentialsAsked)
		if !hasKept {
			return problemRead(readRefused)
		}
		readRefused.Severity = core.SeverityWarning
		readRefused.Message += ", so its kept copy was read"
		*findingWarnings = append(*findingWarnings, readRefused)
		return textRead(storedKept.CopyBody)
	}
	if hasKept {
		return textRead(storedKept.CopyBody)
	}
	return problemRead(failedProblem(location.writtenAt, writtenLocation, readOutcome.FailureReason))
}

func (schemaCache *SchemaCache) keptCopyOf(storeKey, fetchURL string) (keptCopy, bool) {
	if schemaCache.schemaStore == nil {
		return keptCopy{}, false
	}
	storedText, isStored := schemaCache.schemaStore.Read(storeKey)
	if !isStored {
		return keptCopy{}, false
	}
	var storedEntry storedCopy
	if json.Unmarshal([]byte(storedText), &storedEntry) != nil || storedEntry.CopyURL == nil ||
		storedEntry.CopyVersion == nil || storedEntry.ConfirmedAt == nil || storedEntry.CopyBody == nil ||
		*storedEntry.CopyURL != fetchURL {
		return keptCopy{}, false
	}
	var copyVersion any
	if json.Unmarshal(storedEntry.CopyVersion, &copyVersion) != nil {
		return keptCopy{}, false
	}
	return keptCopy{
		CopyURL:     *storedEntry.CopyURL,
		CopyVersion: copyVersion,
		ConfirmedAt: *storedEntry.ConfirmedAt,
		CopyBody:    *storedEntry.CopyBody,
	}, true
}

func (schemaCache *SchemaCache) keepCopy(storeKey string, sourceCopy keptCopy) {
	if schemaCache.schemaStore == nil {
		return
	}
	if copyText, encodeError := json.Marshal(sourceCopy); encodeError == nil {
		schemaCache.schemaStore.Write(storeKey, string(copyText))
	}
}

func problemRead(readProblem core.Diagnostic) candidateRead {
	return candidateRead{readKind: candidateReadProblem, readProblem: readProblem}
}

// problemAt is a problem placed at writtenAt, or with no position where it is nil.
func problemAt(writtenAt *core.Position, problemMessage string) core.Diagnostic {
	if writtenAt == nil {
		return core.UnplacedDiagnostic(problemMessage)
	}
	return core.DiagnosticAt(*writtenAt, problemMessage)
}

func refusedProblem(
	writtenAt *core.Position, writtenLocation string, readRefusals []resolution.ReadRefusal, credentialsAsked bool,
) core.Diagnostic {
	refusedBy := "the source"
	if len(readRefusals) > 0 {
		refusedBy = readRefusals[0].RefusedBy
	}
	var refusalReasons, renewalFixes []string
	for _, readRefusal := range readRefusals {
		if readRefusal.CredentialOrigin != nil {
			refusalReasons = append(refusalReasons, *readRefusal.CredentialOrigin+": "+readRefusal.RefusalReason)
		} else {
			refusalReasons = append(refusalReasons, readRefusal.RefusalReason)
		}
		if readRefusal.RenewalFix != nil && !slices.Contains(renewalFixes, *readRefusal.RenewalFix) {
			renewalFixes = append(renewalFixes, *readRefusal.RenewalFix)
		}
	}
	if !credentialsAsked {
		renewalFixes = append(renewalFixes, machineCredentialsHelp)
	}
	readRefused := problemAt(writtenAt, refusedBy+" refused to read "+core.CodeSpan(writtenLocation))
	if len(refusalReasons) > 0 {
		readRefused = readRefused.WithLabel(strings.Join(refusalReasons, ", "))
	}
	if len(renewalFixes) > 0 {
		helpText := strings.Join(renewalFixes, "; ")
		readRefused = readRefused.WithHelp(&helpText)
	}
	return readRefused
}

func failedProblem(writtenAt *core.Position, writtenLocation, failureReason string) core.Diagnostic {
	return problemAt(writtenAt, "reading "+core.CodeSpan(writtenLocation)+" failed").WithLabel(failureReason)
}

func refusalWarning(writtenAt *core.Position, readRefusal resolution.ReadRefusal) core.Diagnostic {
	credentialOrigin := ""
	if readRefusal.CredentialOrigin != nil {
		credentialOrigin = *readRefusal.CredentialOrigin
	}
	refusedCredential := problemAt(writtenAt, readRefusal.RefusedBy+" refused "+credentialOrigin)
	refusedCredential.Severity = core.SeverityWarning
	return refusedCredential.WithLabel(readRefusal.RefusalReason).WithHelp(readRefusal.RenewalFix)
}

// fileNameOf is the name of the file a `file` URL names, else the URL.
func fileNameOf(fileURL string) string {
	lastSegment := fileURL[strings.LastIndexByte(fileURL, '/')+1:]
	segmentPath := resolution.FileURLPath("file:///" + lastSegment)
	fileName := filepath.Base(segmentPath)
	if fileName == "." || fileName == ".." || fileName == string(filepath.Separator) {
		return fileURL
	}
	return fileName
}

// copyKey is the key a remote source's copy is kept under: a SHA-256 of its URL without the cache's parameters.
func copyKey(fetchURL string) string {
	urlDigest := sha256.Sum256([]byte(fetchURL))
	return hex.EncodeToString(urlDigest[:])
}

// absolutePath is machinePath made absolute, its `.` and `..` steps read out.
func absolutePath(machinePath string) string {
	if absoluteForm, absoluteError := filepath.Abs(machinePath); absoluteError == nil {
		return absoluteForm
	}
	return filepath.Clean(machinePath)
}
