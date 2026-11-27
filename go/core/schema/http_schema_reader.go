package schema

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

const httpNotFoundReason = "the server answered 404"

// httpSchemaReader is the core's reader of `http` and `https` links: what a server answers a link with, read with
// the `.netrc` login of its host when asked for the machine's credentials.
type httpSchemaReader struct {
	credentials credentialSource
}

func newHTTPSchemaReader(credentials credentialSource) *httpSchemaReader {
	return &httpSchemaReader{credentials: credentials}
}

func (*httpSchemaReader) URLPrefixes() []string {
	return []string{"http://", "https://"}
}

func (*httpSchemaReader) ReadsRemoteSources() bool {
	return true
}

func (reader *httpSchemaReader) ReadSchemaURL(
	readContext context.Context, urlRequest SchemaURLRequest,
) SchemaURLAnswer {
	hostName := urlHost(urlRequest.FetchURL)
	requestHeaders := HTTPConditionalHeaders(urlRequest.KeptCopyVersion)
	var hostLogin *HostLogin
	if urlRequest.MachineCredentials {
		foundLogin, hasLogin := reader.credentials.hostLogin(hostName)
		if !hasLogin {
			renewalFix := "add a `.netrc` entry for " + hostName
			return RefusedAnswer([]ReadRefusal{{
				RefusedBy:     hostName,
				RefusalReason: "no `.netrc` entry for " + hostName,
				RenewalFix:    &renewalFix,
			}})
		}
		requestHeaders.Set(authorizationHeader, foundLogin.Authorization())
		hostLogin = &foundLogin
	}
	httpReply, requestError := HTTPGet(readContext, urlRequest.FetchURL, requestHeaders)
	if requestError != nil {
		return FailedAnswer(requestError.Error())
	}
	if httpReply.StatusCode == http.StatusUnauthorized || httpReply.StatusCode == http.StatusForbidden {
		if hostLogin != nil {
			reader.credentials.forgetCredentials()
		}
		return RefusedAnswer([]ReadRefusal{hostRefusal(hostName, hostLogin, httpReply.StatusCode)})
	}
	return httpSentAnswer(httpReply)
}

// httpSentAnswer is a server's answer to a request for the schema itself.
func httpSentAnswer(httpReply HTTPReply) SchemaURLAnswer {
	switch httpReply.StatusCode {
	case http.StatusOK:
		return OutcomeAnswer(SchemaURLOutcome{
			OutcomeKind: OutcomeTextSent,
			SchemaText:  httpReply.ReplyText,
			CopyVersion: httpReply.CopyVersion,
		})
	case http.StatusNotModified:
		return OutcomeAnswer(SchemaURLOutcome{OutcomeKind: OutcomeCopyConfirmed})
	case http.StatusNotFound:
		return MissingAnswer(httpNotFoundReason)
	}
	return FailedAnswer("the server answered " + strconv.Itoa(httpReply.StatusCode))
}

func hostRefusal(hostName string, hostLogin *HostLogin, statusCode int) ReadRefusal {
	hostRefusal := ReadRefusal{RefusedBy: hostName, RefusalReason: strconv.Itoa(statusCode)}
	if hostLogin != nil {
		hostRefusal.CredentialOrigin = &hostLogin.CredentialOrigin
		hostRefusal.RenewalFix = &hostLogin.RenewalFix
	}
	return hostRefusal
}

// urlHost is the host a URL names, empty where it names none.
func urlHost(linkURL string) string {
	_, afterScheme, hasScheme := strings.Cut(linkURL, "://")
	if !hasScheme {
		return ""
	}
	authorityText := afterScheme
	if authorityEnd := strings.IndexAny(afterScheme, "/?#"); authorityEnd >= 0 {
		authorityText = afterScheme[:authorityEnd]
	}
	authorityText = authorityText[strings.LastIndexByte(authorityText, '@')+1:]
	hostName, _, _ := strings.Cut(authorityText, ":")
	return hostName
}
