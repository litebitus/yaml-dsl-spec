package resolution

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

const (
	httpRequestTimeout  = 30 * time.Second
	httpRedirectLimit   = 10
	authorizationHeader = "Authorization"
)

var (
	errTooManyRedirects = errors.New("stopped after 10 redirects")
	errReplyNotUTF8     = errors.New("the answer is not valid UTF-8")
	sharedHTTPClient    = &http.Client{
		Timeout: httpRequestTimeout,
		CheckRedirect: func(redirectedRequest *http.Request, previousRequests []*http.Request) error {
			if len(previousRequests) >= httpRedirectLimit {
				return errTooManyRedirects
			}
			redirectedRequest.Header.Del(authorizationHeader)
			return nil
		},
	}
)

// HTTPReply is a server's answer: its status, the copy version its `ETag` and `Last-Modified` give, and, for a
// `200`, its text.
type HTTPReply struct {
	StatusCode  int
	CopyVersion any
	ReplyText   string
}

// HTTPGet is a server's answer to a GET of linkURL with requestHeaders, due by readContext's deadline and within
// the limit for one request, a redirect followed without the `Authorization` header; else why there is none.
func HTTPGet(readContext context.Context, linkURL string, requestHeaders http.Header) (HTTPReply, error) {
	getRequest, requestError := http.NewRequestWithContext(readContext, http.MethodGet, linkURL, nil)
	if requestError != nil {
		return HTTPReply{}, requestError
	}
	for headerName, headerValues := range requestHeaders {
		getRequest.Header[headerName] = headerValues
	}
	httpResponse, responseError := sharedHTTPClient.Do(getRequest)
	if responseError != nil {
		return HTTPReply{}, responseError
	}
	defer httpResponse.Body.Close()
	httpReply := HTTPReply{
		StatusCode: httpResponse.StatusCode,
		CopyVersion: map[string]any{
			"etag":          responseHeader(httpResponse, "ETag"),
			"last_modified": responseHeader(httpResponse, "Last-Modified"),
		},
	}
	if httpResponse.StatusCode != http.StatusOK {
		return httpReply, nil
	}
	bodyBytes, readError := io.ReadAll(httpResponse.Body)
	if readError != nil {
		return HTTPReply{}, readError
	}
	if !utf8.Valid(bodyBytes) {
		return HTTPReply{}, errReplyNotUTF8
	}
	httpReply.ReplyText = string(bodyBytes)
	return httpReply, nil
}

// HTTPConditionalHeaders are the headers asking a server whether a kept copy is current: `If-None-Match` with its
// `etag`, else `If-Modified-Since` with its `last_modified`.
func HTTPConditionalHeaders(keptCopyVersion any) http.Header {
	conditionalHeaders := http.Header{}
	versionFields, _ := keptCopyVersion.(map[string]any)
	if entityTag, isText := versionFields["etag"].(string); isText {
		conditionalHeaders.Set("If-None-Match", entityTag)
	} else if lastModified, isText := versionFields["last_modified"].(string); isText {
		conditionalHeaders.Set("If-Modified-Since", lastModified)
	}
	return conditionalHeaders
}

// responseHeader is a header's value, or nil where the answer has none.
func responseHeader(httpResponse *http.Response, headerName string) any {
	headerValues := httpResponse.Header.Values(headerName)
	if len(headerValues) == 0 {
		return nil
	}
	return headerValues[0]
}
