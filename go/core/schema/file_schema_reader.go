package schema

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/litebitus/yaml-dsl-spec/go/core"
)

const (
	noFileReason     = "no file is there"
	notUTF8Reason    = "stream did not contain valid UTF-8"
	fileSchemePrefix = "file://"
)

// fileSchemaReader is the core's reader of `file` URLs: a file on the machine, read each time.
type fileSchemaReader struct{}

func (fileSchemaReader) URLPrefixes() []string {
	return []string{fileSchemePrefix}
}

func (fileSchemaReader) ReadsRemoteSources() bool {
	return false
}

func (fileSchemaReader) ReadSchemaURL(_ context.Context, urlRequest SchemaURLRequest) SchemaURLAnswer {
	fileAddress, queryText, hasQuery := strings.Cut(urlRequest.FetchURL, "?")
	if hasQuery {
		firstParameter, _, _ := strings.Cut(queryText, "&")
		parameterName, _, _ := strings.Cut(firstParameter, "=")
		return FailedAnswer(core.CodeSpan(parameterName) + " is not a parameter of a " +
			core.CodeSpan(fileScheme) + " URL")
	}
	filePath := fileURLPath(fileAddress)
	if fileInfo, statError := os.Stat(filePath); statError != nil || !fileInfo.Mode().IsRegular() {
		return MissingAnswer(noFileReason)
	}
	fileBytes, readError := os.ReadFile(filePath)
	switch {
	case errors.Is(readError, fs.ErrNotExist):
		return MissingAnswer(noFileReason)
	case readError != nil:
		return FailedAnswer(readError.Error())
	case !utf8.Valid(fileBytes):
		return FailedAnswer(notUTF8Reason)
	}
	return OutcomeAnswer(SchemaURLOutcome{OutcomeKind: OutcomeTextSent, SchemaText: string(fileBytes)})
}
