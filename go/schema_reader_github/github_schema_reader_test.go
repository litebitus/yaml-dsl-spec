package schema_reader_github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	yamldslspec "github.com/litebitus/yaml-dsl-spec/go"
)

const (
	mockSchemaText = `{"type": "object"}`
	goodToken      = "Bearer mock-good-token"
	blindToken     = "Bearer mock-blind-token"
	privateRelease = "mock-org/mock-repo/releases/download/mock/v1.0/mock.schema.json"
	publicRelease  = "mock-org/public-repo/releases/download/v1/mock.schema.json"
)

// mockGithub is GitHub for one public and one private repository: its pages answer the public release and 404 for
// the private one, and its API lets the good token see the private release, by tag and as the latest, whose asset
// redirects to a file store that refuses any request still carrying a token. The blind token sees nothing.
type mockGithub struct {
	apiAddress string
}

func startMockGithub(testingContext *testing.T) mockGithub {
	var apiAddress string
	githubServer := httptest.NewServer(http.HandlerFunc(
		func(responseWriter http.ResponseWriter, githubRequest *http.Request) {
			authorization, isAuthorized := githubRequest.Header[authorizationHeader]
			answer := func(statusCode int, bodyText string) {
				responseWriter.WriteHeader(statusCode)
				_, _ = responseWriter.Write([]byte(bodyText))
			}
			requestPath := githubRequest.RequestURI
			switch {
			case strings.HasPrefix(requestPath, "/mock-org/public-repo/"):
				answer(http.StatusOK, mockSchemaText)
			case strings.HasPrefix(requestPath, "/mock-org/"):
				answer(http.StatusNotFound, "")
			case requestPath == "/stored/7" && !isAuthorized:
				answer(http.StatusOK, mockSchemaText)
			case requestPath == "/stored/7":
				answer(http.StatusBadRequest, "")
			case isAuthorized && authorization[0] == blindToken:
				answer(http.StatusNotFound, "")
			case !isAuthorized || authorization[0] != goodToken:
				answer(http.StatusUnauthorized, "")
			case requestPath == "/repos/mock-org/mock-repo":
				answer(http.StatusOK, "{}")
			case requestPath == "/repos/mock-org/mock-repo/releases/tags/mock%2Fv1.0",
				requestPath == "/repos/mock-org/mock-repo/releases/latest":
				answer(http.StatusOK, `{"assets": [{"name": "mock.schema.json", "url": "`+apiAddress+`/assets/7"}]}`)
			case requestPath == "/assets/7":
				http.Redirect(responseWriter, githubRequest, apiAddress+"/stored/7", http.StatusFound)
			default:
				answer(http.StatusNotFound, "")
			}
		}))
	testingContext.Cleanup(githubServer.Close)
	apiAddress = githubServer.URL
	return mockGithub{apiAddress: apiAddress}
}

func (github mockGithub) link(linkPath string) string {
	return github.apiAddress + "/" + linkPath
}

// givenTokens are tokens a test names in place of the machine's, counting how often they are forgotten.
type givenTokens struct {
	namedTokens []githubToken
	forgetCount atomic.Int64
}

func (tokens *givenTokens) githubTokens() []githubToken {
	return tokens.namedTokens
}

func (tokens *givenTokens) forgetTokens() {
	tokens.forgetCount.Add(1)
}

// reader is the reader of this GitHub's links, reading with the tokens given.
func (github mockGithub) reader(tokens *givenTokens) *GithubSchemaReader {
	return newGithubReader(tokens, github.apiAddress, []string{github.apiAddress + "/"})
}

func namedToken(originName, authorization, renewalFix string) githubToken {
	return githubToken{credentialOrigin: "`" + originName + "`", authorization: authorization, renewalFix: renewalFix}
}

func staleToken() githubToken {
	return namedToken("GH_TOKEN", "Bearer mock-stale-token", "replace `GH_TOKEN`")
}

func workingToken() githubToken {
	return namedToken("gh auth token", goodToken, "run `gh auth login`")
}

func readLink(
	testingContext *testing.T, reader *GithubSchemaReader, linkURL string, machineCredentials bool,
) yamldslspec.SchemaURLAnswer {
	readContext, cancelRead := context.WithTimeout(context.Background(), 4*time.Second)
	testingContext.Cleanup(cancelRead)
	return reader.ReadSchemaURL(readContext, yamldslspec.SchemaURLRequest{
		FetchURL: linkURL, MachineCredentials: machineCredentials,
	})
}

func TestALinkNamesItsRepositoryAndTheReleaseAssetItsPathReads(testingContext *testing.T) {
	taggedRelease, isRelease := githubReleaseAt(privateRelease)
	expectedTagged := releaseAsset{releaseTag: "mock/v1.0", assetName: "mock.schema.json"}
	if !isRelease || taggedRelease.ownerName != "mock-org" || taggedRelease.repositoryName != "mock-repo" ||
		*taggedRelease.releaseAsset != expectedTagged {
		testingContext.Fatalf("tagged %+v", taggedRelease)
	}
	latestRelease, _ := githubReleaseAt("mock-org/mock-repo/releases/latest/download/a.json?x=1")
	if *latestRelease.releaseAsset != (releaseAsset{isLatestRelease: true, assetName: "a.json"}) {
		testingContext.Fatalf("latest %+v", latestRelease.releaseAsset)
	}
	if pageRelease, _ := githubReleaseAt("mock-org/mock-repo/releases/tag/v1"); pageRelease.releaseAsset != nil {
		testingContext.Fatalf("page %+v", pageRelease.releaseAsset)
	}
	if _, isRelease := githubReleaseAt("mock-org/mock-repo/blob/main/a.json"); isRelease {
		testingContext.Fatal("a blob reads as a release")
	}
}

func TestALinkWithoutCredentialsIsReadAsWrittenAPrivateOneRefused(testingContext *testing.T) {
	github := startMockGithub(testingContext)
	reader := github.reader(&givenTokens{namedTokens: []githubToken{workingToken()}})
	publicAnswer := readLink(testingContext, reader, github.link(publicRelease), false)
	if publicAnswer.ReadOutcome.OutcomeKind != yamldslspec.OutcomeTextSent {
		testingContext.Fatalf("public %+v", publicAnswer)
	}
	privateAnswer := readLink(testingContext, reader, github.link(privateRelease), false)
	if privateAnswer.ReadOutcome.OutcomeKind != yamldslspec.OutcomeReadRefused ||
		privateAnswer.ReadRefusals[0].RefusalReason != "404" || privateAnswer.ReadRefusals[0].RefusedBy != "GitHub" {
		testingContext.Fatalf("private %+v", privateAnswer)
	}
	pageAnswer := readLink(testingContext, reader, github.link("mock-org/mock-repo/releases/tag/v1"), true)
	if pageAnswer.ReadOutcome.OutcomeKind != yamldslspec.OutcomeReadRefused {
		testingContext.Fatalf("page %+v", pageAnswer)
	}
}

func TestAPrivateReleaseIsReadWithTheFirstTokenLetInEachRefusedOneListed(testingContext *testing.T) {
	github := startMockGithub(testingContext)
	tokens := &givenTokens{namedTokens: []githubToken{staleToken(), workingToken()}}
	reader := github.reader(tokens)
	expectedRefusals := []yamldslspec.ReadRefusal{{
		RefusedBy:        "GitHub",
		CredentialOrigin: new("`GH_TOKEN`"),
		RefusalReason:    "401 Bad credentials",
		RenewalFix:       new("replace `GH_TOKEN`"),
	}}
	latestRelease := "mock-org/mock-repo/releases/latest/download/mock.schema.json"
	for _, releaseLink := range []string{privateRelease, latestRelease} {
		readAnswer := readLink(testingContext, reader, github.link(releaseLink), true)
		if readAnswer.ReadOutcome.OutcomeKind != yamldslspec.OutcomeTextSent ||
			!reflect.DeepEqual(readAnswer.ReadRefusals, expectedRefusals) {
			testingContext.Fatalf("%s reads to %+v", releaseLink, readAnswer)
		}
	}
	shoutedLink := strings.Replace(github.link(privateRelease), "http://", "HTTP://", 1)
	if shoutedAnswer := readLink(testingContext, reader, shoutedLink, true); shoutedAnswer.ReadOutcome.OutcomeKind !=
		yamldslspec.OutcomeTextSent {
		testingContext.Fatalf("%s reads to %+v", shoutedLink, shoutedAnswer)
	}
	if tokens.forgetCount.Load() != 0 {
		testingContext.Fatal("a token let in was forgotten")
	}
}

func TestAReleaseOrAnAssetTheRepositoryLacksIsMissing(testingContext *testing.T) {
	github := startMockGithub(testingContext)
	reader := github.reader(&givenTokens{namedTokens: []githubToken{workingToken()}})
	expectedReasons := map[string]string{
		"mock-org/mock-repo/releases/download/v9/mock.schema.json": "`mock-org/mock-repo` has no release `v9`",
		"mock-org/mock-repo/releases/latest/download/other.json": "the latest release of `mock-org/mock-repo` " +
			"has no asset `other.json`",
	}
	for releaseLink, expectedReason := range expectedReasons {
		readAnswer := readLink(testingContext, reader, github.link(releaseLink), true)
		if readAnswer.ReadOutcome != (yamldslspec.SchemaURLOutcome{
			OutcomeKind: yamldslspec.OutcomeSourceMissing, MissingReason: expectedReason,
		}) {
			testingContext.Fatalf("%s reads to %+v", releaseLink, readAnswer)
		}
	}
}

func TestEveryTokenRefusedIsListedAndTheTokensReadAgainNextTime(testingContext *testing.T) {
	github := startMockGithub(testingContext)
	staleTokens := &givenTokens{namedTokens: []githubToken{staleToken()}}
	refusedAnswer := readLink(testingContext, github.reader(staleTokens), github.link(privateRelease), true)
	if refusedAnswer.ReadOutcome.OutcomeKind != yamldslspec.OutcomeReadRefused ||
		len(refusedAnswer.ReadRefusals) != 1 || staleTokens.forgetCount.Load() != 1 {
		testingContext.Fatalf("refused %+v", refusedAnswer)
	}
	blindTokens := &givenTokens{namedTokens: []githubToken{namedToken("GITHUB_TOKEN", blindToken, "x")}}
	unseenAnswer := readLink(testingContext, github.reader(blindTokens), github.link(privateRelease), true)
	if unseenAnswer.ReadOutcome != (yamldslspec.SchemaURLOutcome{
		OutcomeKind: yamldslspec.OutcomeReadFailed, FailureReason: "no credential can see `mock-org/mock-repo`",
	}) {
		testingContext.Fatalf("unseen %+v", unseenAnswer)
	}
	unsetAnswer := readLink(testingContext, github.reader(&givenTokens{}), github.link(privateRelease), true)
	if *unsetAnswer.ReadRefusals[0].RenewalFix != noTokenFix {
		testingContext.Fatalf("unset %+v", unsetAnswer)
	}
}

func TestTheMachineTokensAreTheVariablesThenTheNetrcEntry(testingContext *testing.T) {
	netrcPath := filepath.Join(testingContext.TempDir(), ".netrc")
	netrcText := "machine api.github.com login mock-user password mock-secret\n"
	if writeError := os.WriteFile(netrcPath, []byte(netrcText), 0o600); writeError != nil {
		testingContext.Fatal(writeError)
	}
	testingContext.Setenv("NETRC", netrcPath)
	testingContext.Setenv("GH_TOKEN", "mock-gh-token")
	testingContext.Setenv("GITHUB_TOKEN", "")
	testingContext.Setenv("PATH", "")
	tokens := &machineTokens{}
	expectedTokens := []githubToken{
		{credentialOrigin: "`GH_TOKEN`", authorization: "Bearer mock-gh-token", renewalFix: "replace `GH_TOKEN`"},
		{
			credentialOrigin: "the `.netrc` entry for api.github.com",
			authorization:    "Basic bW9jay11c2VyOm1vY2stc2VjcmV0",
			renewalFix:       "update the `.netrc` entry for api.github.com",
		},
	}
	if readTokens := tokens.githubTokens(); !reflect.DeepEqual(readTokens, expectedTokens) {
		testingContext.Fatalf("tokens %+v", readTokens)
	}
	testingContext.Setenv("GH_TOKEN", "")
	if readTokens := tokens.githubTokens(); len(readTokens) != 2 {
		testingContext.Fatal("the tokens were read again before they were forgotten")
	}
	tokens.forgetTokens()
	if readTokens := tokens.githubTokens(); len(readTokens) != 1 {
		testingContext.Fatalf("tokens %+v", readTokens)
	}
	reader := NewGithubSchemaReader()
	if !reflect.DeepEqual(reader.URLPrefixes(), githubPrefixes) || !reader.ReadsRemoteSources() {
		testingContext.Fatal("the reader reads other links")
	}
}
