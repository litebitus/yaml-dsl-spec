package yamldslspec

import (
	"context"
	"net/http"

	"github.com/litebitus/yaml-dsl-spec/go/core/schema"
	"github.com/litebitus/yaml-dsl-spec/go/core/schema/resolution"
)

// The types of package schema, read as it defines them.
type (
	CandidateNamer     = schema.CandidateNamer
	CandidateNamerKind = schema.CandidateNamerKind
	FoundSchema        = schema.FoundSchema
	LoadedSchema       = schema.LoadedSchema
	SchemaCache        = schema.SchemaCache
	SchemaCacheOption  = schema.SchemaCacheOption
	SchemaCandidate    = schema.SchemaCandidate
	SchemaFinding      = schema.SchemaFinding
	SchemaFolderStore  = schema.SchemaFolderStore
	SchemaProblem      = schema.SchemaProblem
	SchemaSearchPath   = schema.SchemaSearchPath
	SchemaStore        = schema.SchemaStore
	ValidationRules    = schema.ValidationRules
)

// The types of package resolution, read as it defines them.
type (
	HTTPReply            = resolution.HTTPReply
	HostLogin            = resolution.HostLogin
	ReadRefusal          = resolution.ReadRefusal
	SchemaModeline       = resolution.SchemaModeline
	SchemaURLAnswer      = resolution.SchemaURLAnswer
	SchemaURLOutcome     = resolution.SchemaURLOutcome
	SchemaURLOutcomeKind = resolution.SchemaURLOutcomeKind
	SchemaURLReader      = resolution.SchemaURLReader
	SchemaURLRequest     = resolution.SchemaURLRequest
)

// The constants of package schema, read as it defines them.
const (
	NamedByCaller     = schema.NamedByCaller
	NamedByModeline   = schema.NamedByModeline
	NamedBySearchPath = schema.NamedBySearchPath
)

// The constants of package resolution, read as it defines them.
const (
	OutcomeCopyConfirmed = resolution.OutcomeCopyConfirmed
	OutcomeReadFailed    = resolution.OutcomeReadFailed
	OutcomeReadRefused   = resolution.OutcomeReadRefused
	OutcomeSourceMissing = resolution.OutcomeSourceMissing
	OutcomeTextSent      = resolution.OutcomeTextSent
)

// ChildValue is schema.ChildValue.
func ChildValue(entryKey string, childPatterns []*CompiledPattern) (string, string, bool) {
	return schema.ChildValue(entryKey, childPatterns)
}

// ClosestName is schema.ClosestName.
func ClosestName(misspeltName string, candidateNames []string) (string, bool) {
	return schema.ClosestName(misspeltName, candidateNames)
}

// DocumentModeline is resolution.DocumentModeline.
func DocumentModeline(documentText string) (SchemaModeline, bool) {
	return resolution.DocumentModeline(documentText)
}

// FailedAnswer is resolution.FailedAnswer.
func FailedAnswer(failureReason string) SchemaURLAnswer {
	return resolution.FailedAnswer(failureReason)
}

// HTTPConditionalHeaders is resolution.HTTPConditionalHeaders.
func HTTPConditionalHeaders(keptCopyVersion any) http.Header {
	return resolution.HTTPConditionalHeaders(keptCopyVersion)
}

// HTTPGet is resolution.HTTPGet.
func HTTPGet(readContext context.Context, linkURL string, requestHeaders http.Header) (HTTPReply, error) {
	return resolution.HTTPGet(readContext, linkURL, requestHeaders)
}

// JSONPointerSteps is schema.JSONPointerSteps.
func JSONPointerSteps(jsonPointer string) ([]string, error) {
	return schema.JSONPointerSteps(jsonPointer)
}

// MissingAnswer is resolution.MissingAnswer.
func MissingAnswer(missingReason string) SchemaURLAnswer {
	return resolution.MissingAnswer(missingReason)
}

// NetrcLogin is resolution.NetrcLogin.
func NetrcLogin(hostName string) (HostLogin, bool) {
	return resolution.NetrcLogin(hostName)
}

// NewSchemaCache is schema.NewSchemaCache.
func NewSchemaCache(cacheOptions ...SchemaCacheOption) *SchemaCache {
	return schema.NewSchemaCache(cacheOptions...)
}

// NewSchemaFolderStore is schema.NewSchemaFolderStore.
func NewSchemaFolderStore(
	storeFolder string, entryCapacity int, reportFailure func(failureMessage string),
) *SchemaFolderStore {
	return schema.NewSchemaFolderStore(storeFolder, entryCapacity, reportFailure)
}

// OutcomeAnswer is resolution.OutcomeAnswer.
func OutcomeAnswer(readOutcome SchemaURLOutcome) SchemaURLAnswer {
	return resolution.OutcomeAnswer(readOutcome)
}

// RefusedAnswer is resolution.RefusedAnswer.
func RefusedAnswer(readRefusals []ReadRefusal) SchemaURLAnswer {
	return resolution.RefusedAnswer(readRefusals)
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
