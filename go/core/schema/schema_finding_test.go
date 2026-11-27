package schema

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/litebitus/yaml-dsl-spec/go/core"
	"github.com/litebitus/yaml-dsl-spec/go/core/schema/resolution"
)

const (
	mockSchemaURL  = "mock://mock-host/mock.schema.json"
	keptSchemaText = `{"title": "kept"}`
)

// scriptedReader is a reader of `mock://` URLs answering what a test scripts: one answer for a read without
// credentials and one for a read with them, each request kept.
type scriptedReader struct {
	answerWithout   resolution.SchemaURLAnswer
	answerWith      resolution.SchemaURLAnswer
	requestLock     sync.Mutex
	askedRequests   []resolution.SchemaURLRequest
	readCount       atomic.Int64
	requestDeadline atomic.Bool
}

func answeringReader(answerWithout, answerWith resolution.SchemaURLAnswer) *scriptedReader {
	return &scriptedReader{answerWithout: answerWithout, answerWith: answerWith}
}

func sendingReader(schemaText string) *scriptedReader {
	sentAnswer := resolution.OutcomeAnswer(resolution.SchemaURLOutcome{
		OutcomeKind: resolution.OutcomeTextSent,
		SchemaText:  schemaText,
		CopyVersion: map[string]any{"mock_version": 1},
	})
	return answeringReader(sentAnswer, sentAnswer)
}

func (*scriptedReader) URLPrefixes() []string {
	return []string{"mock://"}
}

func (*scriptedReader) ReadsRemoteSources() bool {
	return true
}

func (reader *scriptedReader) ReadSchemaURL(
	readContext context.Context, urlRequest resolution.SchemaURLRequest,
) resolution.SchemaURLAnswer {
	reader.readCount.Add(1)
	_, hasDeadline := readContext.Deadline()
	reader.requestDeadline.Store(hasDeadline)
	reader.requestLock.Lock()
	reader.askedRequests = append(reader.askedRequests, urlRequest)
	reader.requestLock.Unlock()
	if urlRequest.MachineCredentials {
		return reader.answerWith
	}
	return reader.answerWithout
}

func newMockStore() *mockSchemaStore {
	return &mockSchemaStore{storedEntries: map[string]string{}}
}

func scriptedCache(reader *scriptedReader, store *mockSchemaStore) *SchemaCache {
	return NewSchemaCache(WithStore(store), WithURLReader(reader))
}

func mockWrittenAt() *core.Position {
	writtenAt := core.NewPosition("mock-tfgen.yml", 4, 7)
	return &writtenAt
}

func mockRefusal(credentialOrigin, renewalFix *string) resolution.ReadRefusal {
	return resolution.ReadRefusal{
		RefusedBy:        "mock-host",
		CredentialOrigin: credentialOrigin,
		RefusalReason:    "403",
		RenewalFix:       renewalFix,
	}
}

func keepMockCopy(testingContext *testing.T, store *mockSchemaStore, fetchURL string, confirmedAt uint64) {
	copyText, encodeError := json.Marshal(keptCopy{
		CopyURL:     fetchURL,
		CopyVersion: map[string]any{"mock_version": 0},
		ConfirmedAt: confirmedAt,
		CopyBody:    keptSchemaText,
	})
	if encodeError != nil {
		testingContext.Fatal(encodeError)
	}
	store.Write(copyKey(fetchURL), string(copyText))
}

func storedCopyFields(testingContext *testing.T, store *mockSchemaStore, fetchURL string) map[string]any {
	storedText, isStored := store.Read(copyKey(fetchURL))
	if !isStored {
		testingContext.Fatalf("no copy kept for %s", fetchURL)
	}
	var copyFields map[string]any
	if decodeError := json.Unmarshal([]byte(storedText), &copyFields); decodeError != nil {
		testingContext.Fatal(decodeError)
	}
	return copyFields
}

func TestARemoteTextIsKeptAsACopyWithItsVersionAndReadBackWithinItsMaxAge(testingContext *testing.T) {
	reader, store := sendingReader("{}"), newMockStore()
	schemaCache := scriptedCache(reader, store)
	schemaLocation := mockSchemaURL + "?b=1&yaml_dsl_max_age_days=3"
	firstFinding := schemaCache.ReadSchema(context.Background(), schemaLocation, mockWrittenAt())
	if firstFinding.FoundSchema == nil || firstFinding.FoundSchema.SchemaText != "{}" {
		testingContext.Fatalf("first finding %+v", firstFinding)
	}
	copyFields := storedCopyFields(testingContext, store, mockSchemaURL+"?b=1")
	if !reflect.DeepEqual(copyFields["copy_version"], map[string]any{"mock_version": float64(1)}) ||
		copyFields["url"] != mockSchemaURL+"?b=1" {
		testingContext.Fatalf("kept %v", copyFields)
	}
	secondFinding := schemaCache.ReadSchema(context.Background(), schemaLocation, mockWrittenAt())
	if secondFinding.FoundSchema == nil || secondFinding.FoundSchema.SchemaName != schemaLocation {
		testingContext.Fatalf("second finding %+v", secondFinding)
	}
	if readCount := reader.readCount.Load(); readCount != 1 {
		testingContext.Fatalf("reads = %d, want 1", readCount)
	}
	if !reader.requestDeadline.Load() {
		testingContext.Fatal("the request carries no deadline")
	}
}

func TestACopyPastItsMaxAgeIsAskedAboutWithItsVersionAndAConfirmationKeepsIt(testingContext *testing.T) {
	confirmedAnswer := resolution.OutcomeAnswer(
		resolution.SchemaURLOutcome{OutcomeKind: resolution.OutcomeCopyConfirmed})
	reader, store := answeringReader(confirmedAnswer, confirmedAnswer), newMockStore()
	keepMockCopy(testingContext, store, mockSchemaURL, 0)
	schemaFinding := scriptedCache(reader, store).ReadSchema(context.Background(), mockSchemaURL, nil)
	if schemaFinding.FoundSchema == nil || schemaFinding.FoundSchema.SchemaText != keptSchemaText {
		testingContext.Fatalf("finding %+v", schemaFinding)
	}
	if !reflect.DeepEqual(reader.askedRequests[0].KeptCopyVersion, map[string]any{"mock_version": float64(0)}) {
		testingContext.Fatalf("asked with %v", reader.askedRequests[0].KeptCopyVersion)
	}
	copyFields := storedCopyFields(testingContext, store, mockSchemaURL)
	if confirmedAt, _ := copyFields["confirmed_at"].(float64); confirmedAt <= 0 {
		testingContext.Fatal("the confirmation did not restart the copy's age")
	}
}

func TestARefusedReadIsAskedAgainWithCredentialsOnlyWhereTheURLOptsIn(testingContext *testing.T) {
	refusedAnswer := resolution.RefusedAnswer([]resolution.ReadRefusal{mockRefusal(nil, nil)})
	sentAnswer := resolution.SchemaURLAnswer{
		ReadOutcome: resolution.SchemaURLOutcome{OutcomeKind: resolution.OutcomeTextSent, SchemaText: "{}"},
		ReadRefusals: []resolution.ReadRefusal{
			mockRefusal(new("`MOCK_TOKEN`"), new("replace `MOCK_TOKEN`")),
		},
	}
	reader := answeringReader(refusedAnswer, sentAnswer)
	schemaCache := scriptedCache(reader, newMockStore())
	withoutFinding := schemaCache.ReadSchema(context.Background(), mockSchemaURL, mockWrittenAt())
	if withoutFinding.FoundSchema != nil {
		testingContext.Fatal("a refused read found a schema")
	}
	refusedProblem := withoutFinding.FindingProblems[0]
	if refusedProblem.Message != "mock-host refused to read `"+mockSchemaURL+"`" ||
		*refusedProblem.Notes.Label != "403" || *refusedProblem.Notes.Help != machineCredentialsHelp {
		testingContext.Fatalf("problem %+v %+v", refusedProblem, *refusedProblem.Notes)
	}
	optedIn := mockSchemaURL + "?yaml_dsl_machine_credentials=true"
	withFinding := schemaCache.ReadSchema(context.Background(), optedIn, mockWrittenAt())
	if withFinding.FoundSchema == nil || withFinding.FoundSchema.SchemaText != "{}" {
		testingContext.Fatalf("finding %+v", withFinding)
	}
	refusedWarning := withFinding.FindingProblems[0]
	if refusedWarning.Severity != core.SeverityWarning || refusedWarning.Message != "mock-host refused `MOCK_TOKEN`" ||
		*refusedWarning.Notes.Help != "replace `MOCK_TOKEN`" {
		testingContext.Fatalf("warning %+v", refusedWarning)
	}
	var askedCredentials []bool
	for _, askedRequest := range reader.askedRequests {
		askedCredentials = append(askedCredentials, askedRequest.MachineCredentials)
	}
	if !reflect.DeepEqual(askedCredentials, []bool{false, false, true}) {
		testingContext.Fatalf("asked %v", askedCredentials)
	}
}

func TestEveryCredentialRefusedReadsTheKeptCopyWithAWarningElseEndsTheFinding(testingContext *testing.T) {
	refusedAnswer := resolution.RefusedAnswer([]resolution.ReadRefusal{
		mockRefusal(new("`MOCK_TOKEN`"), new("replace `MOCK_TOKEN`")),
		mockRefusal(new("`OTHER_TOKEN`"), new("replace `MOCK_TOKEN`")),
	})
	reader := answeringReader(refusedAnswer, refusedAnswer)
	optedIn := mockSchemaURL + "?yaml_dsl_machine_credentials=true"
	store := newMockStore()
	unkeptFinding := scriptedCache(reader, store).ReadSchema(context.Background(), optedIn, mockWrittenAt())
	unkeptNotes := unkeptFinding.FindingProblems[0].Notes
	if *unkeptNotes.Label != "`MOCK_TOKEN`: 403, `OTHER_TOKEN`: 403" || *unkeptNotes.Help != "replace `MOCK_TOKEN`" {
		testingContext.Fatalf("notes %+v", *unkeptNotes)
	}
	keepMockCopy(testingContext, store, mockSchemaURL, 0)
	keptFinding := scriptedCache(reader, store).ReadSchema(context.Background(), optedIn, mockWrittenAt())
	if keptFinding.FoundSchema == nil || keptFinding.FoundSchema.SchemaText != keptSchemaText {
		testingContext.Fatalf("finding %+v", keptFinding)
	}
	keptWarning := keptFinding.FindingProblems[0]
	if keptWarning.Severity != core.SeverityWarning ||
		keptWarning.Message != "mock-host refused to read `"+optedIn+"`, so its kept copy was read" {
		testingContext.Fatalf("warning %+v", keptWarning)
	}
}

func TestAFailedReadGivesTheKeptCopyAndAMissingSourceNeverDoes(testingContext *testing.T) {
	failedAnswer := resolution.FailedAnswer("mock failure")
	failingReader, store := answeringReader(failedAnswer, failedAnswer), newMockStore()
	unkeptFinding := scriptedCache(failingReader, store).ReadSchema(context.Background(), mockSchemaURL, nil)
	if unkeptFinding.FindingProblems[0].Message != "reading `"+mockSchemaURL+"` failed" ||
		unkeptFinding.FindingProblems[0].Position != nil {
		testingContext.Fatalf("problem %+v", unkeptFinding.FindingProblems[0])
	}
	keepMockCopy(testingContext, store, mockSchemaURL, 0)
	keptFinding := scriptedCache(failingReader, store).ReadSchema(context.Background(), mockSchemaURL, nil)
	if keptFinding.FoundSchema == nil || len(keptFinding.FindingProblems) != 0 {
		testingContext.Fatalf("finding %+v", keptFinding)
	}
	missingAnswer := resolution.MissingAnswer("mock reason")
	absentReader := answeringReader(missingAnswer, missingAnswer)
	missingFinding := scriptedCache(absentReader, store).ReadSchema(
		context.Background(), mockSchemaURL, mockWrittenAt())
	missingProblem := missingFinding.FindingProblems[0]
	if missingFinding.FoundSchema != nil || missingProblem.Message != "`"+mockSchemaURL+"` is missing" ||
		*missingProblem.Notes.Label != "mock reason" || *missingProblem.Position != *mockWrittenAt() {
		testingContext.Fatalf("finding %+v", missingFinding)
	}
}

func TestACopyConfirmedButNeverKeptFails(testingContext *testing.T) {
	confirmedAnswer := resolution.OutcomeAnswer(
		resolution.SchemaURLOutcome{OutcomeKind: resolution.OutcomeCopyConfirmed})
	reader := answeringReader(confirmedAnswer, confirmedAnswer)
	schemaFinding := scriptedCache(reader, newMockStore()).ReadSchema(context.Background(), mockSchemaURL, nil)
	if *schemaFinding.FindingProblems[0].Notes.Label != neverKeptCopyReason {
		testingContext.Fatalf("finding %+v", schemaFinding)
	}
}

func TestAStoredCopyThatDoesNotReadIsFetchedAgain(testingContext *testing.T) {
	reader, store := sendingReader("{}"), newMockStore()
	unreadEntries := []string{
		"not a copy",
		`{"url": "` + mockSchemaURL + `", "confirmed_at": 1, "body": ""}`,
	}
	schemaLocation := mockSchemaURL + "?yaml_dsl_max_age_days=3"
	for _, storedText := range unreadEntries {
		store.Write(copyKey(mockSchemaURL), storedText)
		schemaFinding := scriptedCache(reader, store).ReadSchema(context.Background(), schemaLocation, nil)
		if schemaFinding.FoundSchema == nil || schemaFinding.FoundSchema.SchemaText != "{}" {
			testingContext.Fatalf("finding %+v", schemaFinding)
		}
	}
	if readCount := reader.readCount.Load(); readCount != 2 {
		testingContext.Fatalf("reads = %d, want 2", readCount)
	}
}

func TestARelativePathIsReadAgainstTheWorkingDirectory(testingContext *testing.T) {
	workingDirectory, directoryError := os.Getwd()
	if directoryError != nil {
		testingContext.Fatal(directoryError)
	}
	expectedLocation := resolution.FileURLOf(filepath.Join(workingDirectory, "schema_finding.go"))
	callerFinding := NewSchemaCache().ReadSchema(context.Background(), "schema_finding.go", nil)
	modelineText := "# yaml-language-server: $schema=schema_finding.go\n"
	documentFinding := NewSchemaCache().DocumentSchema(context.Background(), "mock-dsl.yml", modelineText, nil, "")
	for _, schemaFinding := range []SchemaFinding{callerFinding, documentFinding} {
		if schemaFinding.FoundSchema == nil || schemaFinding.FoundSchema.SchemaName != "schema_finding.go" ||
			schemaFinding.FoundSchema.FoundCandidate.SchemaLocation != expectedLocation {
			testingContext.Fatalf("finding %+v", schemaFinding)
		}
	}
}

func TestACacheWithoutAStoreAsksTheReaderOnEveryRead(testingContext *testing.T) {
	reader := sendingReader("{}")
	schemaCache := NewSchemaCache(WithURLReader(reader))
	schemaLocation := mockSchemaURL + "?yaml_dsl_max_age_days=3"
	schemaCache.ReadSchema(context.Background(), schemaLocation, nil)
	schemaCache.ReadSchema(context.Background(), schemaLocation, nil)
	if readCount := reader.readCount.Load(); readCount != 2 {
		testingContext.Fatalf("reads = %d, want 2", readCount)
	}
}

func TestReadsOfOneURLAtOnceFetchItOnceAndTheOthersReadItsCopy(testingContext *testing.T) {
	reader, store := sendingReader("{}"), newMockStore()
	schemaCache := scriptedCache(reader, store)
	schemaLocation := mockSchemaURL + "?yaml_dsl_max_age_days=1"
	var readersDone sync.WaitGroup
	foundCount := atomic.Int64{}
	for range 4 {
		readersDone.Go(func() {
			if schemaCache.ReadSchema(context.Background(), schemaLocation, nil).FoundSchema != nil {
				foundCount.Add(1)
			}
		})
	}
	readersDone.Wait()
	if foundCount.Load() != 4 || reader.readCount.Load() != 1 {
		testingContext.Fatalf("found %d, reads %d", foundCount.Load(), reader.readCount.Load())
	}
}

// prefixReader is a reader of its prefixes answering every URL missing, its first prefix the reason.
type prefixReader struct {
	readPrefixes []string
}

func (reader prefixReader) URLPrefixes() []string {
	return reader.readPrefixes
}

func (prefixReader) ReadsRemoteSources() bool {
	return true
}

func (reader prefixReader) ReadSchemaURL(context.Context, resolution.SchemaURLRequest) resolution.SchemaURLAnswer {
	return resolution.MissingAnswer(reader.readPrefixes[0])
}

func TestAURLIsReadByTheReaderOfItsLongestPrefix(testingContext *testing.T) {
	schemaCache := NewSchemaCache(WithURLReader(prefixReader{readPrefixes: []string{"https://mock.example/releases/"}}))
	schemaFinding := schemaCache.ReadSchema(context.Background(), "HTTPS://mock.example/releases/a.json", nil)
	if *schemaFinding.FindingProblems[0].Notes.Label != "https://mock.example/releases/" {
		testingContext.Fatalf("finding %+v", schemaFinding)
	}
	defer func() {
		if recover() == nil {
			testingContext.Fatal("a second reader of `https://` was registered")
		}
	}()
	NewSchemaCache(WithURLReader(prefixReader{readPrefixes: []string{"HTTPS://"}}))
}

func TestALocalReaderRefusalOrConfirmationIsAProblemWhereTheLocationIsWritten(testingContext *testing.T) {
	answerCases := []struct {
		localAnswer     resolution.SchemaURLAnswer
		expectedMessage string
	}{
		{
			localAnswer:     resolution.RefusedAnswer([]resolution.ReadRefusal{mockRefusal(nil, nil)}),
			expectedMessage: "mock-host refused to read `local://mock.schema.json`",
		},
		{
			localAnswer: resolution.OutcomeAnswer(
				resolution.SchemaURLOutcome{OutcomeKind: resolution.OutcomeCopyConfirmed}),
			expectedMessage: "reading `local://mock.schema.json` failed",
		},
	}
	for _, answerCase := range answerCases {
		schemaCache := NewSchemaCache(WithURLReader(localReader{localAnswer: answerCase.localAnswer}))
		schemaFinding := schemaCache.ReadSchema(context.Background(), "local://mock.schema.json", mockWrittenAt())
		if schemaFinding.FindingProblems[0].Message != answerCase.expectedMessage {
			testingContext.Fatalf("finding %+v", schemaFinding)
		}
	}
}

// localReader is a reader of `local://` sources on the machine, answering what a test scripts.
type localReader struct {
	localAnswer resolution.SchemaURLAnswer
}

func (localReader) URLPrefixes() []string {
	return []string{"local://"}
}

func (localReader) ReadsRemoteSources() bool {
	return false
}

func TestAFileNameIsTheLastStepOfItsURLElseTheURL(testingContext *testing.T) {
	expectedNames := map[string]string{
		"file:///mock/mock%20schema.json": "mock schema.json",
		"file:///mock/":                   "file:///mock/",
	}
	for fileURL, expectedName := range expectedNames {
		if fileName := fileNameOf(fileURL); fileName != expectedName {
			testingContext.Fatalf("%s names %s", fileURL, fileName)
		}
	}
}

func (reader localReader) ReadSchemaURL(context.Context, resolution.SchemaURLRequest) resolution.SchemaURLAnswer {
	return reader.localAnswer
}
