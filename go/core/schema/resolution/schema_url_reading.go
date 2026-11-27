// Package resolution is the YAML DSL standard's reading of schema URLs: locations, the readers of their prefixes and
// the machine's credentials they read with.
package resolution

import "context"

// SchemaURLReader reads the URLs of its prefixes for a schema cache.
type SchemaURLReader interface {
	// URLPrefixes are the prefixes of the URLs it reads, such as `s3://`.
	URLPrefixes() []string
	// ReadsRemoteSources is whether its sources are remote, so a cache keeps a copy of what it reads.
	ReadsRemoteSources() bool
	// ReadSchemaURL is its answer to a request, due by readContext's deadline.
	ReadSchemaURL(readContext context.Context, urlRequest SchemaURLRequest) SchemaURLAnswer
}

// SchemaURLRequest is what a schema cache asks a reader for.
type SchemaURLRequest struct {
	// FetchURL is the URL without the cache's parameters, every other parameter as written.
	FetchURL string
	// MachineCredentials is whether to read the URL with the machine's credentials.
	MachineCredentials bool
	// KeptCopyVersion is the version the reader gave the kept copy; nil where no copy is kept.
	KeptCopyVersion any
}

// SchemaURLAnswer is what a read came to, and each refusal the source gave in it.
type SchemaURLAnswer struct {
	ReadOutcome  SchemaURLOutcome
	ReadRefusals []ReadRefusal
}

// SchemaURLOutcomeKind is which outcome a read came to.
type SchemaURLOutcomeKind int

const (
	// OutcomeTextSent is the text with its version.
	OutcomeTextSent SchemaURLOutcomeKind = iota + 1
	// OutcomeCopyConfirmed is the kept copy confirmed current.
	OutcomeCopyConfirmed
	// OutcomeSourceMissing is a source that is not there.
	OutcomeSourceMissing
	// OutcomeReadRefused is a read the source refused.
	OutcomeReadRefused
	// OutcomeReadFailed is a read that failed.
	OutcomeReadFailed
)

// SchemaURLOutcome is what a read came to, with the fields of its kind.
type SchemaURLOutcome struct {
	OutcomeKind SchemaURLOutcomeKind
	// SchemaText is a sent text.
	SchemaText string
	// CopyVersion is a sent text's version, the reader's own, a JSON value.
	CopyVersion any
	// MissingReason is why a missing source is not there.
	MissingReason string
	// FailureReason is why a failed read failed.
	FailureReason string
}

// ReadRefusal is a refusal a source gave.
type ReadRefusal struct {
	// RefusedBy is who refused, such as `GitHub`.
	RefusedBy string
	// CredentialOrigin is where the refused credential came from; nil for a read without credentials.
	CredentialOrigin *string
	// RefusalReason is what the source answered.
	RefusalReason string
	// RenewalFix is what renews the credential, if it has one.
	RenewalFix *string
}

// OutcomeAnswer is an answer of readOutcome, with no refusal.
func OutcomeAnswer(readOutcome SchemaURLOutcome) SchemaURLAnswer {
	return SchemaURLAnswer{ReadOutcome: readOutcome}
}

// RefusedAnswer is a read refused, with each refusal the source gave.
func RefusedAnswer(readRefusals []ReadRefusal) SchemaURLAnswer {
	return SchemaURLAnswer{
		ReadOutcome:  SchemaURLOutcome{OutcomeKind: OutcomeReadRefused},
		ReadRefusals: readRefusals,
	}
}

// FailedAnswer is a read that failed, and why.
func FailedAnswer(failureReason string) SchemaURLAnswer {
	return OutcomeAnswer(SchemaURLOutcome{OutcomeKind: OutcomeReadFailed, FailureReason: failureReason})
}

// MissingAnswer is a source that is not there, and why.
func MissingAnswer(missingReason string) SchemaURLAnswer {
	return OutcomeAnswer(SchemaURLOutcome{OutcomeKind: OutcomeSourceMissing, MissingReason: missingReason})
}
