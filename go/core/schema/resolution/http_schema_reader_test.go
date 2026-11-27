package resolution

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

const (
	servedSchemaText = `{"type": "object"}`
	mockEntityTag    = `"mock-etag"`
	mockLastModified = "Wed, 01 Jan 2026 00:00:00 GMT"
	mockLoginHeader  = "Basic bW9jay11c2VyOm1vY2stc2VjcmV0"
)

// mockSchemaServer answers `/schema.json` with the schema, or `304` to a request carrying its ETag; `/private.json`
// only to the mock login; `/broken.json` with `500`; `/binary.json` with a body that is not UTF-8; `/moved.json` by
// a redirect to `/landing.json`, which answers only a request without a login; and anything else with `404`.
func mockSchemaServer(testingContext *testing.T) *httptest.Server {
	schemaServer := httptest.NewServer(http.HandlerFunc(
		func(responseWriter http.ResponseWriter, servedRequest *http.Request) {
			switch servedRequest.URL.Path {
			case "/schema.json":
				if servedRequest.Header.Get("If-None-Match") == mockEntityTag {
					responseWriter.WriteHeader(http.StatusNotModified)
					return
				}
				responseWriter.Header().Set("ETag", mockEntityTag)
				responseWriter.Header().Set("Last-Modified", mockLastModified)
				_, _ = responseWriter.Write([]byte(servedSchemaText))
			case "/private.json":
				if servedRequest.Header.Get(authorizationHeader) != mockLoginHeader {
					responseWriter.WriteHeader(http.StatusUnauthorized)
					return
				}
				_, _ = responseWriter.Write([]byte(servedSchemaText))
			case "/moved.json":
				http.Redirect(responseWriter, servedRequest, "/landing.json", http.StatusFound)
			case "/landing.json":
				if servedRequest.Header.Get(authorizationHeader) != "" {
					responseWriter.WriteHeader(http.StatusBadRequest)
					return
				}
				_, _ = responseWriter.Write([]byte(servedSchemaText))
			case "/binary.json":
				_, _ = responseWriter.Write([]byte{0xff, 0xfe})
			case "/broken.json":
				responseWriter.WriteHeader(http.StatusInternalServerError)
			default:
				responseWriter.WriteHeader(http.StatusNotFound)
			}
		}))
	testingContext.Cleanup(schemaServer.Close)
	return schemaServer
}

// givenLogin is a login a test names in place of the machine's `.netrc`, counting how often it is forgotten.
type givenLogin struct {
	namedLogin  *HostLogin
	forgetCount atomic.Int64
}

func (credentials *givenLogin) hostLogin(string) (HostLogin, bool) {
	if credentials.namedLogin == nil {
		return HostLogin{}, false
	}
	return *credentials.namedLogin, true
}

func (credentials *givenLogin) forgetCredentials() {
	credentials.forgetCount.Add(1)
}

func mockLogin(loginPassword string) *HostLogin {
	return &HostLogin{
		CredentialOrigin: "the `.netrc` entry for 127.0.0.1",
		LoginName:        "mock-user",
		LoginPassword:    loginPassword,
		RenewalFix:       "update the `.netrc` entry for 127.0.0.1",
	}
}

func readLink(
	testingContext *testing.T, reader *HTTPSchemaReader, linkURL string, machineCredentials bool, keptVersion any,
) SchemaURLAnswer {
	readContext, cancelRead := context.WithTimeout(context.Background(), 4*time.Second)
	testingContext.Cleanup(cancelRead)
	return reader.ReadSchemaURL(readContext, SchemaURLRequest{
		FetchURL: linkURL, MachineCredentials: machineCredentials, KeptCopyVersion: keptVersion,
	})
}

func TestALinkIsSentWithItsVersionAndACurrentCopyIsConfirmed(testingContext *testing.T) {
	schemaServer := mockSchemaServer(testingContext)
	reader := httpSchemaReaderWith(&givenLogin{})
	sentAnswer := readLink(testingContext, reader, schemaServer.URL+"/schema.json", false, nil)
	sentOutcome := sentAnswer.ReadOutcome
	expectedVersion := map[string]any{"etag": mockEntityTag, "last_modified": mockLastModified}
	if sentOutcome.OutcomeKind != OutcomeTextSent || sentOutcome.SchemaText != servedSchemaText ||
		!reflect.DeepEqual(sentOutcome.CopyVersion, expectedVersion) {
		testingContext.Fatalf("sent %+v", sentOutcome)
	}
	confirmedAnswer := readLink(testingContext, reader, schemaServer.URL+"/schema.json", false, sentOutcome.CopyVersion)
	if confirmedAnswer.ReadOutcome.OutcomeKind != OutcomeCopyConfirmed {
		testingContext.Fatalf("confirmed %+v", confirmedAnswer)
	}
	modifiedHeaders := HTTPConditionalHeaders(map[string]any{"etag": nil, "last_modified": mockLastModified})
	if !reflect.DeepEqual(modifiedHeaders, http.Header{"If-Modified-Since": {mockLastModified}}) {
		testingContext.Fatalf("headers %v", modifiedHeaders)
	}
	if unversionedHeaders := HTTPConditionalHeaders(nil); len(unversionedHeaders) != 0 {
		testingContext.Fatalf("headers %v", unversionedHeaders)
	}
}

func TestA404IsMissingAndAnotherStatusOrNoAnswerFails(testingContext *testing.T) {
	schemaServer := mockSchemaServer(testingContext)
	reader := httpSchemaReaderWith(&givenLogin{})
	missingAnswer := readLink(testingContext, reader, schemaServer.URL+"/absent.json", false, nil)
	if missingAnswer.ReadOutcome != (SchemaURLOutcome{
		OutcomeKind: OutcomeSourceMissing, MissingReason: "the server answered 404",
	}) {
		testingContext.Fatalf("missing %+v", missingAnswer)
	}
	brokenAnswer := readLink(testingContext, reader, schemaServer.URL+"/broken.json", false, nil)
	if brokenAnswer.ReadOutcome != (SchemaURLOutcome{
		OutcomeKind: OutcomeReadFailed, FailureReason: "the server answered 500",
	}) {
		testingContext.Fatalf("broken %+v", brokenAnswer)
	}
	unansweredAnswer := readLink(testingContext, reader, "http://127.0.0.1:9/schema.json", false, nil)
	if unansweredAnswer.ReadOutcome.OutcomeKind != OutcomeReadFailed {
		testingContext.Fatalf("unanswered %+v", unansweredAnswer)
	}
	binaryAnswer := readLink(testingContext, reader, schemaServer.URL+"/binary.json", false, nil)
	if binaryAnswer.ReadOutcome != (SchemaURLOutcome{
		OutcomeKind: OutcomeReadFailed, FailureReason: "the answer is not valid UTF-8",
	}) {
		testingContext.Fatalf("binary %+v", binaryAnswer)
	}
}

func TestARedirectIsFollowedWithoutTheLogin(testingContext *testing.T) {
	schemaServer := mockSchemaServer(testingContext)
	loggedInReader := httpSchemaReaderWith(&givenLogin{namedLogin: mockLogin("mock-secret")})
	movedAnswer := readLink(testingContext, loggedInReader, schemaServer.URL+"/moved.json", true, nil)
	if movedAnswer.ReadOutcome.OutcomeKind != OutcomeTextSent {
		testingContext.Fatalf("moved %+v", movedAnswer)
	}
}

func TestARefusedLinkIsReadWithTheNetrcLoginOfItsHost(testingContext *testing.T) {
	schemaServer := mockSchemaServer(testingContext)
	anonymousReader := httpSchemaReaderWith(&givenLogin{})
	refusedAnswer := readLink(testingContext, anonymousReader, schemaServer.URL+"/private.json", false, nil)
	if refusedAnswer.ReadOutcome.OutcomeKind != OutcomeReadRefused ||
		refusedAnswer.ReadRefusals[0].RefusalReason != "401" || refusedAnswer.ReadRefusals[0].CredentialOrigin != nil {
		testingContext.Fatalf("refused %+v", refusedAnswer)
	}
	loggedInReader := httpSchemaReaderWith(&givenLogin{namedLogin: mockLogin("mock-secret")})
	sentAnswer := readLink(testingContext, loggedInReader, schemaServer.URL+"/private.json", true, nil)
	if sentAnswer.ReadOutcome.OutcomeKind != OutcomeTextSent {
		testingContext.Fatalf("sent %+v", sentAnswer)
	}
}

func TestARefusedLoginIsListedAndTheCredentialsReadAgainNextTime(testingContext *testing.T) {
	schemaServer := mockSchemaServer(testingContext)
	staleLogin := &givenLogin{namedLogin: mockLogin("mock-stale")}
	privateLink := schemaServer.URL + "/private.json"
	refusedAnswer := readLink(testingContext, httpSchemaReaderWith(staleLogin), privateLink, true, nil)
	expectedRefusals := []ReadRefusal{{
		RefusedBy:        "127.0.0.1",
		CredentialOrigin: new("the `.netrc` entry for 127.0.0.1"),
		RefusalReason:    "401",
		RenewalFix:       new("update the `.netrc` entry for 127.0.0.1"),
	}}
	if refusedAnswer.ReadOutcome.OutcomeKind != OutcomeReadRefused ||
		!reflect.DeepEqual(refusedAnswer.ReadRefusals, expectedRefusals) || staleLogin.forgetCount.Load() != 1 {
		testingContext.Fatalf("refused %+v", refusedAnswer)
	}
	unsetAnswer := readLink(testingContext, httpSchemaReaderWith(&givenLogin{}), privateLink, true, nil)
	if *unsetAnswer.ReadRefusals[0].RenewalFix != "add a `.netrc` entry for 127.0.0.1" {
		testingContext.Fatalf("unset %+v", unsetAnswer)
	}
}

func TestAHostIsTheAuthorityWithoutItsLoginAndPort(testingContext *testing.T) {
	expectedHosts := map[string]string{
		"https://mock-user:mock-secret@mock.example:8443/a?b#c": "mock.example",
		"https://mock.example": "mock.example",
		"mock.example/a":       "",
	}
	for linkURL, expectedHost := range expectedHosts {
		if hostName := urlHost(linkURL); hostName != expectedHost {
			testingContext.Fatalf("%s names %s", linkURL, hostName)
		}
	}
}

func TestAFileIsReadEachTimeAndAFolderOrAQueryIsNotOne(testingContext *testing.T) {
	scratchFolder := testingContext.TempDir()
	invalidFile := filepath.Join(scratchFolder, "invalid.json")
	if writeError := os.WriteFile(invalidFile, []byte{0xff}, 0o644); writeError != nil {
		testingContext.Fatal(writeError)
	}
	expectedOutcomes := map[string]SchemaURLOutcome{
		FileURLOf(scratchFolder): {OutcomeKind: OutcomeSourceMissing, MissingReason: noFileReason},
		FileURLOf(invalidFile):   {OutcomeKind: OutcomeReadFailed, FailureReason: notUTF8Reason},
		FileURLOf(invalidFile) + "?mock=1&b": {
			OutcomeKind:   OutcomeReadFailed,
			FailureReason: "`mock` is not a parameter of a `file` URL",
		},
	}
	for fileURL, expectedOutcome := range expectedOutcomes {
		fileAnswer := FileSchemaReader{}.ReadSchemaURL(context.Background(), SchemaURLRequest{FetchURL: fileURL})
		if fileAnswer.ReadOutcome != expectedOutcome {
			testingContext.Fatalf("%s reads to %+v", fileURL, fileAnswer.ReadOutcome)
		}
	}
}
