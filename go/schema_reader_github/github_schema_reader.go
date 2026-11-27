// Package schema_reader_github is the YAML DSL standard's reader of schema URLs on GitHub: a link read as written,
// and, with the machine's credentials, a release asset read through GitHub's API with each of the machine's tokens
// in turn.
package schema_reader_github

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	yamldslspec "github.com/litebitus/yaml-dsl-spec/go"
)

const (
	githubAPI           = "https://api.github.com"
	githubAPIHost       = "api.github.com"
	githubRefuser       = "GitHub"
	githubJSON          = "application/vnd.github+json"
	ghLoginTimeout      = 10 * time.Second
	authorizationHeader = "Authorization"
	noTokenFix          = "set `GH_TOKEN`, run `gh auth login`, or add a `.netrc` entry for api.github.com"
)

var githubPrefixes = []string{"https://github.com/", "https://www.github.com/"}

// githubToken is a token, where it came from, and how its owner renews it.
type githubToken struct {
	credentialOrigin string
	authorization    string
	renewalFix       string
}

// tokenSource is where the tokens come from.
type tokenSource interface {
	// githubTokens are the tokens, in the order they are tried.
	githubTokens() []githubToken
	// forgetTokens reads the tokens again when next asked, after every one was refused.
	forgetTokens()
}

// machineTokens are the machine's tokens: `GH_TOKEN`, `GITHUB_TOKEN`, the `gh` command's login, then the `.netrc`
// entry for api.github.com, read when first needed.
type machineTokens struct {
	tokensLock  sync.Mutex
	readTokens  []githubToken
	areTokensIn bool
}

func (tokens *machineTokens) githubTokens() []githubToken {
	tokens.tokensLock.Lock()
	defer tokens.tokensLock.Unlock()
	if !tokens.areTokensIn {
		tokens.readTokens, tokens.areTokensIn = machineTokenList(), true
	}
	return tokens.readTokens
}

func (tokens *machineTokens) forgetTokens() {
	tokens.tokensLock.Lock()
	defer tokens.tokensLock.Unlock()
	tokens.areTokensIn = false
}

func machineTokenList() []githubToken {
	var tokenList []githubToken
	for _, variableName := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if tokenText := os.Getenv(variableName); tokenText != "" {
			tokenList = append(tokenList, githubToken{
				credentialOrigin: "`" + variableName + "`",
				authorization:    "Bearer " + tokenText,
				renewalFix:       "replace `" + variableName + "`",
			})
		}
	}
	if tokenText, isLoggedIn := ghLoginToken(); isLoggedIn {
		tokenList = append(tokenList, githubToken{
			credentialOrigin: "`gh auth token`",
			authorization:    "Bearer " + tokenText,
			renewalFix:       "run `gh auth login`",
		})
	}
	if hostLogin, hasLogin := yamldslspec.NetrcLogin(githubAPIHost); hasLogin {
		tokenList = append(tokenList, githubToken{
			credentialOrigin: hostLogin.CredentialOrigin,
			authorization:    hostLogin.Authorization(),
			renewalFix:       hostLogin.RenewalFix,
		})
	}
	return tokenList
}

// ghLoginToken is the token of the account `gh auth login` logged in to github.com, where the `gh` command is
// installed and answers within its time.
func ghLoginToken() (string, bool) {
	loginContext, cancelLogin := context.WithTimeout(context.Background(), ghLoginTimeout)
	defer cancelLogin()
	tokenOutput, commandError := exec.CommandContext(loginContext, "gh", "auth", "token", "--hostname", "github.com").
		Output()
	if commandError != nil {
		return "", false
	}
	tokenText := strings.TrimSpace(string(tokenOutput))
	return tokenText, tokenText != ""
}

// GithubSchemaReader is the URL reader of GitHub links.
type GithubSchemaReader struct {
	tokens       tokenSource
	githubAPI    string
	linkPrefixes []string
}

// NewGithubSchemaReader is a URL reader of GitHub links, `https://github.com/` and `https://www.github.com/`.
func NewGithubSchemaReader() *GithubSchemaReader {
	return newGithubReader(&machineTokens{}, githubAPI, githubPrefixes)
}

func newGithubReader(tokens tokenSource, apiAddress string, linkPrefixes []string) *GithubSchemaReader {
	return &GithubSchemaReader{tokens: tokens, githubAPI: apiAddress, linkPrefixes: linkPrefixes}
}

// githubRelease is a link under `<owner>/<repo>/releases/`: its repository, and the release and asset its path
// reads, where it reads one.
type githubRelease struct {
	ownerName      string
	repositoryName string
	releaseAsset   *releaseAsset
}

type releaseAsset struct {
	isLatestRelease bool
	releaseTag      string
	assetName       string
}

func (asset releaseAsset) apiPath() string {
	if asset.isLatestRelease {
		return "latest"
	}
	return "tags/" + strings.ReplaceAll(asset.releaseTag, "/", "%2F")
}

func (asset releaseAsset) shownName() string {
	if asset.isLatestRelease {
		return "the latest release"
	}
	return "release `" + asset.releaseTag + "`"
}

// URLPrefixes are the GitHub link prefixes the reader reads.
func (reader *GithubSchemaReader) URLPrefixes() []string {
	return reader.linkPrefixes
}

// ReadsRemoteSources is true: GitHub is remote.
func (*GithubSchemaReader) ReadsRemoteSources() bool {
	return true
}

// ReadSchemaURL is a link read as written, or, with the machine's credentials, a release asset read through
// GitHub's API.
func (reader *GithubSchemaReader) ReadSchemaURL(
	readContext context.Context, urlRequest yamldslspec.SchemaURLRequest,
) yamldslspec.SchemaURLAnswer {
	if urlRequest.MachineCredentials {
		if linkRelease, isRelease := reader.githubReleaseOf(urlRequest.FetchURL); isRelease &&
			linkRelease.releaseAsset != nil {
			return reader.releaseAnswer(readContext, linkRelease, urlRequest)
		}
	}
	return reader.linkAnswer(readContext, urlRequest)
}

// linkAnswer is the link read as written, the machine's credentials never sent.
func (*GithubSchemaReader) linkAnswer(
	readContext context.Context, urlRequest yamldslspec.SchemaURLRequest,
) yamldslspec.SchemaURLAnswer {
	linkReply, requestError := yamldslspec.HTTPGet(readContext, urlRequest.FetchURL,
		yamldslspec.HTTPConditionalHeaders(urlRequest.KeptCopyVersion))
	if requestError != nil {
		return yamldslspec.FailedAnswer(requestError.Error())
	}
	switch linkReply.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return yamldslspec.RefusedAnswer([]yamldslspec.ReadRefusal{{
			RefusedBy: githubRefuser, RefusalReason: strconv.Itoa(linkReply.StatusCode),
		}})
	}
	return sentAnswer(linkReply)
}

// githubReleaseOf is the release a link names after one of the reader's prefixes, matched without regard to ASCII
// case.
func (reader *GithubSchemaReader) githubReleaseOf(linkURL string) (githubRelease, bool) {
	for _, linkPrefix := range reader.linkPrefixes {
		if len(linkURL) >= len(linkPrefix) && strings.EqualFold(linkURL[:len(linkPrefix)], linkPrefix) {
			return githubReleaseAt(linkURL[len(linkPrefix):])
		}
	}
	return githubRelease{}, false
}

// releaseAnswer is a release asset read through GitHub's API, each token tried in turn until one is let in, each
// one refused before it listed.
func (reader *GithubSchemaReader) releaseAnswer(
	readContext context.Context, linkRelease githubRelease, urlRequest yamldslspec.SchemaURLRequest,
) yamldslspec.SchemaURLAnswer {
	tokenList := reader.tokens.githubTokens()
	if len(tokenList) == 0 {
		renewalFix := noTokenFix
		return yamldslspec.RefusedAnswer([]yamldslspec.ReadRefusal{{
			RefusedBy: githubRefuser, RefusalReason: "no GitHub token is set", RenewalFix: &renewalFix,
		}})
	}
	asset := *linkRelease.releaseAsset
	var readRefusals []yamldslspec.ReadRefusal
	answerWith := func(readOutcome yamldslspec.SchemaURLOutcome) yamldslspec.SchemaURLAnswer {
		return yamldslspec.SchemaURLAnswer{ReadOutcome: readOutcome, ReadRefusals: readRefusals}
	}
	failedWith := func(failureReason string) yamldslspec.SchemaURLAnswer {
		return answerWith(yamldslspec.SchemaURLOutcome{
			OutcomeKind: yamldslspec.OutcomeReadFailed, FailureReason: failureReason,
		})
	}
	shownRepository := "`" + linkRelease.ownerName + "/" + linkRelease.repositoryName + "`"
	repositoryURL := reader.githubAPI + "/repos/" + linkRelease.ownerName + "/" + linkRelease.repositoryName
	releaseURL := repositoryURL + "/releases/" + asset.apiPath()
	missingReason := ""
	for _, token := range tokenList {
		apiHeaders := http.Header{}
		apiHeaders.Set(authorizationHeader, token.authorization)
		apiHeaders.Set("Accept", githubJSON)
		releaseReply, requestError := yamldslspec.HTTPGet(readContext, releaseURL, apiHeaders)
		if requestError != nil {
			return failedWith(requestError.Error())
		}
		releaseStatus := releaseReply.StatusCode
		if releaseStatus == http.StatusUnauthorized || releaseStatus == http.StatusForbidden {
			readRefusals = append(readRefusals, tokenRefusal(token, releaseStatus))
			continue
		}
		if releaseStatus != http.StatusOK {
			repositoryReply, repositoryError := yamldslspec.HTTPGet(readContext, repositoryURL, apiHeaders)
			if repositoryError == nil && repositoryReply.StatusCode == http.StatusOK {
				missingReason = shownRepository + " has no " + asset.shownName()
				break
			}
			continue
		}
		assetURL, listingError := listedAssetURL(releaseReply.ReplyText, asset.assetName)
		if listingError != nil {
			return failedWith(listingError.Error())
		}
		if assetURL == "" {
			missingReason = asset.shownName() + " of " + shownRepository + " has no asset `" + asset.assetName + "`"
			break
		}
		downloadHeaders := yamldslspec.HTTPConditionalHeaders(urlRequest.KeptCopyVersion)
		downloadHeaders.Set(authorizationHeader, token.authorization)
		downloadHeaders.Set("Accept", "application/octet-stream")
		downloadReply, downloadError := yamldslspec.HTTPGet(readContext, assetURL, downloadHeaders)
		if downloadError != nil {
			return failedWith(downloadError.Error())
		}
		downloadStatus := downloadReply.StatusCode
		if downloadStatus == http.StatusUnauthorized || downloadStatus == http.StatusForbidden {
			readRefusals = append(readRefusals, tokenRefusal(token, downloadStatus))
			continue
		}
		return answerWith(sentAnswer(downloadReply).ReadOutcome)
	}
	if missingReason != "" {
		return answerWith(yamldslspec.SchemaURLOutcome{
			OutcomeKind: yamldslspec.OutcomeSourceMissing, MissingReason: missingReason,
		})
	}
	if len(readRefusals) == len(tokenList) {
		reader.tokens.forgetTokens()
		return yamldslspec.RefusedAnswer(readRefusals)
	}
	return failedWith("no credential can see " + shownRepository)
}

// listedAssetURL is the API URL of the asset a release's listing names assetName, empty where it lists none.
func listedAssetURL(listingText, assetName string) (string, error) {
	var releaseListing any
	if decodeError := json.Unmarshal([]byte(listingText), &releaseListing); decodeError != nil {
		return "", decodeError
	}
	listingFields, _ := releaseListing.(map[string]any)
	listedAssets, _ := listingFields["assets"].([]any)
	for _, listedAsset := range listedAssets {
		assetFields, _ := listedAsset.(map[string]any)
		if listedName, _ := assetFields["name"].(string); listedName == assetName {
			assetURL, _ := assetFields["url"].(string)
			return assetURL, nil
		}
	}
	return "", nil
}

func sentAnswer(githubReply yamldslspec.HTTPReply) yamldslspec.SchemaURLAnswer {
	switch githubReply.StatusCode {
	case http.StatusOK:
		return yamldslspec.OutcomeAnswer(yamldslspec.SchemaURLOutcome{
			OutcomeKind: yamldslspec.OutcomeTextSent,
			SchemaText:  githubReply.ReplyText,
			CopyVersion: githubReply.CopyVersion,
		})
	case http.StatusNotModified:
		return yamldslspec.OutcomeAnswer(yamldslspec.SchemaURLOutcome{OutcomeKind: yamldslspec.OutcomeCopyConfirmed})
	}
	return yamldslspec.FailedAnswer("GitHub answered " + strconv.Itoa(githubReply.StatusCode))
}

func tokenRefusal(token githubToken, statusCode int) yamldslspec.ReadRefusal {
	answeredStatus := strconv.Itoa(statusCode)
	if statusCode == http.StatusUnauthorized {
		answeredStatus = "401 Bad credentials"
	}
	return yamldslspec.ReadRefusal{
		RefusedBy:        githubRefuser,
		CredentialOrigin: &token.credentialOrigin,
		RefusalReason:    answeredStatus,
		RenewalFix:       &token.renewalFix,
	}
}

// githubReleaseAt is the repository a link's path, after its host, names under `<owner>/<repo>/releases/`, and the
// release asset that path reads: `download/<tag>/<asset>` or `latest/download/<asset>`.
func githubReleaseAt(linkPath string) (githubRelease, bool) {
	if pathEnd := strings.IndexAny(linkPath, "?#"); pathEnd >= 0 {
		linkPath = linkPath[:pathEnd]
	}
	pathSteps := strings.Split(linkPath, "/")
	if len(pathSteps) < 3 || pathSteps[2] != "releases" || pathSteps[0] == "" || pathSteps[1] == "" {
		return githubRelease{}, false
	}
	linkRelease := githubRelease{ownerName: pathSteps[0], repositoryName: pathSteps[1]}
	releaseSteps := pathSteps[3:]
	switch {
	case len(releaseSteps) == 3 && releaseSteps[0] == "latest" && releaseSteps[1] == "download":
		linkRelease.releaseAsset = &releaseAsset{isLatestRelease: true, assetName: releaseSteps[2]}
	case len(releaseSteps) >= 3 && releaseSteps[0] == "download":
		linkRelease.releaseAsset = &releaseAsset{
			releaseTag: strings.Join(releaseSteps[1:len(releaseSteps)-1], "/"),
			assetName:  releaseSteps[len(releaseSteps)-1],
		}
	}
	return linkRelease, true
}
