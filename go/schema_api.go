package yamldslspec

import (
	"context"
	"net/http"

	"github.com/litebitus/yaml-dsl-spec/go/core/schema"
)

// The types of package schema, read as it defines them.
type (
	CandidateNamer       = schema.CandidateNamer
	CandidateNamerKind   = schema.CandidateNamerKind
	FoundSchema          = schema.FoundSchema
	HTTPReply            = schema.HTTPReply
	HostLogin            = schema.HostLogin
	LoadedSchema         = schema.LoadedSchema
	ReadRefusal          = schema.ReadRefusal
	SchemaCache          = schema.SchemaCache
	SchemaCacheOption    = schema.SchemaCacheOption
	SchemaCandidate      = schema.SchemaCandidate
	SchemaFinding        = schema.SchemaFinding
	SchemaFolderStore    = schema.SchemaFolderStore
	SchemaProblem        = schema.SchemaProblem
	SchemaSearchPath     = schema.SchemaSearchPath
	SchemaStore          = schema.SchemaStore
	SchemaURLAnswer      = schema.SchemaURLAnswer
	SchemaURLOutcome     = schema.SchemaURLOutcome
	SchemaURLOutcomeKind = schema.SchemaURLOutcomeKind
	SchemaURLReader      = schema.SchemaURLReader
	SchemaURLRequest     = schema.SchemaURLRequest
	ValidationRules      = schema.ValidationRules
)

// The constants of package schema, read as it defines them.
const (
	NamedByCaller        = schema.NamedByCaller
	NamedByModeline      = schema.NamedByModeline
	NamedBySearchPath    = schema.NamedBySearchPath
	OutcomeCopyConfirmed = schema.OutcomeCopyConfirmed
	OutcomeReadFailed    = schema.OutcomeReadFailed
	OutcomeReadRefused   = schema.OutcomeReadRefused
	OutcomeSourceMissing = schema.OutcomeSourceMissing
	OutcomeTextSent      = schema.OutcomeTextSent
)

// ChildValue is schema.ChildValue.
func ChildValue(entryKey string, childPatterns []*CompiledPattern) (string, string, bool) {
	return schema.ChildValue(entryKey, childPatterns)
}

// ClosestName is schema.ClosestName.
func ClosestName(misspeltName string, candidateNames []string) (string, bool) {
	return schema.ClosestName(misspeltName, candidateNames)
}

// FailedAnswer is schema.FailedAnswer.
func FailedAnswer(failureReason string) SchemaURLAnswer {
	return schema.FailedAnswer(failureReason)
}

// HTTPConditionalHeaders is schema.HTTPConditionalHeaders.
func HTTPConditionalHeaders(keptCopyVersion any) http.Header {
	return schema.HTTPConditionalHeaders(keptCopyVersion)
}

// HTTPGet is schema.HTTPGet.
func HTTPGet(readContext context.Context, linkURL string, requestHeaders http.Header) (HTTPReply, error) {
	return schema.HTTPGet(readContext, linkURL, requestHeaders)
}

// JSONPointerSteps is schema.JSONPointerSteps.
func JSONPointerSteps(jsonPointer string) ([]string, error) {
	return schema.JSONPointerSteps(jsonPointer)
}

// MissingAnswer is schema.MissingAnswer.
func MissingAnswer(missingReason string) SchemaURLAnswer {
	return schema.MissingAnswer(missingReason)
}

// NetrcLogin is schema.NetrcLogin.
func NetrcLogin(hostName string) (HostLogin, bool) {
	return schema.NetrcLogin(hostName)
}

// NewSchemaCache is schema.NewSchemaCache.
func NewSchemaCache(cacheOptions ...SchemaCacheOption) *SchemaCache {
	return schema.NewSchemaCache(cacheOptions...)
}

// NewSchemaFolderStore is schema.NewSchemaFolderStore.
func NewSchemaFolderStore(storeFolder string, entryCapacity int) *SchemaFolderStore {
	return schema.NewSchemaFolderStore(storeFolder, entryCapacity)
}

// OutcomeAnswer is schema.OutcomeAnswer.
func OutcomeAnswer(readOutcome SchemaURLOutcome) SchemaURLAnswer {
	return schema.OutcomeAnswer(readOutcome)
}

// RefusedAnswer is schema.RefusedAnswer.
func RefusedAnswer(readRefusals []ReadRefusal) SchemaURLAnswer {
	return schema.RefusedAnswer(readRefusals)
}

// ValidationRulesFromValidator is schema.ValidationRulesFromValidator.
func ValidationRulesFromValidator(dslValidator *Validator) (ValidationRules, error) {
	return schema.ValidationRulesFromValidator(dslValidator)
}

// WithCapacity is schema.WithCapacity.
func WithCapacity(heldCapacity int) SchemaCacheOption {
	return schema.WithCapacity(heldCapacity)
}

// WithStore is schema.WithStore.
func WithStore(schemaStore SchemaStore) SchemaCacheOption {
	return schema.WithStore(schemaStore)
}

// WithURLReader is schema.WithURLReader.
func WithURLReader(urlReader SchemaURLReader) SchemaCacheOption {
	return schema.WithURLReader(urlReader)
}
