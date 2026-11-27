// Package schema_reader_s3 is the YAML DSL standard's reader of schema URLs on S3, `s3://<bucket>/<key>`, read with
// the AWS SDK: unsigned without the machine's credentials, else with the SDK's default chain, as the role a URL's
// `assume_role` names where it names one.
package schema_reader_s3

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"

	yamldslspec "github.com/litebitus/yaml-dsl-spec/go"
)

const (
	s3Prefix                 = "s3://"
	assumeRoleParameter      = "assume_role"
	defaultRegion            = "us-east-1"
	bucketRegionHeader       = "x-amz-bucket-region"
	awsRefuser               = "AWS"
	machineCredentialsOrigin = "the machine's AWS credentials"
	s3ServiceID              = "S3"
	credentialsFix           = "run `aws sso login`, or set `AWS_PROFILE` to a profile that reads it"
	notUTF8Reason            = "the answer is not valid UTF-8"
)

var (
	credentialsRefusedCodes = []string{
		"ExpiredToken",
		"InvalidAccessKeyId",
		"InvalidClientTokenId",
		"SignatureDoesNotMatch",
	}
	regionRedirectCodes = []string{
		"PermanentRedirect",
		"AuthorizationHeaderMalformed",
		"IllegalLocationConstraintException",
	}
	credentialsMissingPhrases = []string{
		"no providers",
		"expired",
		"failed to load",
		"failed to refresh",
		"failed to retrieve",
	}
)

// S3SchemaReader is the URL reader of `s3://<bucket>/<key>`.
type S3SchemaReader struct {
	roleSessionName string
	stateLock       sync.Mutex
	machineConfig   *aws.Config
	roleCredentials map[string]aws.CredentialsProvider
	bucketRegions   map[string]string
}

// s3Object is an `s3://<bucket>/<key>` URL: where its object is, and the role it is read as.
type s3Object struct {
	bucketName string
	objectKey  string
	assumeRole string
}

// readIdentityKind is who a read is made as.
type readIdentityKind int

const (
	unsignedIdentity readIdentityKind = iota + 1
	machineIdentity
	assumedRoleIdentity
)

type readIdentity struct {
	identityKind readIdentityKind
	roleARN      string
}

// NewS3SchemaReader is a URL reader of `s3://` URLs, a role assumed under roleSessionName, the consumer's name for
// itself.
func NewS3SchemaReader(roleSessionName string) *S3SchemaReader {
	return &S3SchemaReader{
		roleSessionName: roleSessionName,
		roleCredentials: map[string]aws.CredentialsProvider{},
		bucketRegions:   map[string]string{},
	}
}

// URLPrefixes are the prefixes of the URLs the reader reads, `s3://`.
func (*S3SchemaReader) URLPrefixes() []string {
	return []string{s3Prefix}
}

// ReadsRemoteSources is true: S3 is remote.
func (*S3SchemaReader) ReadsRemoteSources() bool {
	return true
}

// ReadSchemaURL is the object an `s3://` URL names, read unsigned, or with the machine's credentials when asked.
func (reader *S3SchemaReader) ReadSchemaURL(
	readContext context.Context, urlRequest yamldslspec.SchemaURLRequest,
) yamldslspec.SchemaURLAnswer {
	namedObject, parseError := parseS3Object(urlRequest.FetchURL)
	if parseError != nil {
		return yamldslspec.FailedAnswer(parseError.Error())
	}
	identity := readIdentity{identityKind: unsignedIdentity}
	if urlRequest.MachineCredentials {
		identity.identityKind = machineIdentity
		if namedObject.assumeRole != "" {
			identity = readIdentity{identityKind: assumedRoleIdentity, roleARN: namedObject.assumeRole}
		}
	}
	versionFields, _ := urlRequest.KeptCopyVersion.(map[string]any)
	keptEntityTag, _ := versionFields["etag"].(string)
	answerChannel := make(chan yamldslspec.SchemaURLAnswer, 1)
	go func() {
		answerChannel <- reader.objectAnswer(readContext, namedObject, identity, keptEntityTag)
	}()
	select {
	case objectAnswer := <-answerChannel:
		return objectAnswer
	case <-readContext.Done():
		return yamldslspec.FailedAnswer("S3 did not answer in time")
	}
}

func (reader *S3SchemaReader) objectAnswer(
	readContext context.Context, namedObject s3Object, identity readIdentity, keptEntityTag string,
) yamldslspec.SchemaURLAnswer {
	readConfig, configError := reader.configOf(readContext, identity)
	var objectAnswer yamldslspec.SchemaURLAnswer
	if configError != nil {
		objectAnswer = errorAnswer(configError, namedObject, identity)
	} else {
		bucketRegion := reader.bucketRegion(namedObject.bucketName, readConfig)
		objectOutput, readError := reader.getObject(readContext, readConfig, bucketRegion, namedObject, keptEntityTag)
		if redirectedRegion, isRedirected := redirectedRegionOf(readError); isRedirected &&
			redirectedRegion != bucketRegion {
			reader.stateLock.Lock()
			reader.bucketRegions[namedObject.bucketName] = redirectedRegion
			reader.stateLock.Unlock()
			objectOutput, readError = reader.getObject(readContext, readConfig, redirectedRegion, namedObject,
				keptEntityTag)
		}
		if readError != nil {
			objectAnswer = errorAnswer(readError, namedObject, identity)
		} else {
			objectAnswer = sentAnswer(objectOutput)
		}
	}
	if objectAnswer.ReadOutcome.OutcomeKind == yamldslspec.OutcomeReadRefused &&
		identity.identityKind != unsignedIdentity {
		reader.forgetCredentials()
	}
	return objectAnswer
}

func (reader *S3SchemaReader) getObject(
	readContext context.Context, readConfig aws.Config, bucketRegion string, namedObject s3Object, keptEntityTag string,
) (*s3.GetObjectOutput, error) {
	s3Client := s3.NewFromConfig(readConfig, func(clientOptions *s3.Options) {
		clientOptions.Region = bucketRegion
		if clientOptions.BaseEndpoint != nil {
			clientOptions.UsePathStyle = true
		}
	})
	objectInput := &s3.GetObjectInput{
		Bucket: aws.String(namedObject.bucketName), Key: aws.String(namedObject.objectKey),
	}
	if keptEntityTag != "" {
		objectInput.IfNoneMatch = aws.String(keptEntityTag)
	}
	return s3Client.GetObject(readContext, objectInput)
}

// configOf is the configuration a read as identity is made with.
func (reader *S3SchemaReader) configOf(readContext context.Context, identity readIdentity) (aws.Config, error) {
	switch identity.identityKind {
	case unsignedIdentity:
		return config.LoadDefaultConfig(readContext, config.WithCredentialsProvider(aws.AnonymousCredentials{}))
	case machineIdentity:
		return reader.machineConfigOf(readContext)
	}
	machineConfig, configError := reader.machineConfigOf(readContext)
	if configError != nil {
		return aws.Config{}, configError
	}
	roleConfig := machineConfig.Copy()
	roleConfig.Credentials = reader.roleProvider(identity.roleARN, machineConfig)
	return roleConfig, nil
}

func (reader *S3SchemaReader) machineConfigOf(readContext context.Context) (aws.Config, error) {
	reader.stateLock.Lock()
	heldConfig := reader.machineConfig
	reader.stateLock.Unlock()
	if heldConfig != nil {
		return *heldConfig, nil
	}
	loadedConfig, loadError := config.LoadDefaultConfig(readContext)
	if loadError != nil {
		return aws.Config{}, loadError
	}
	reader.stateLock.Lock()
	reader.machineConfig = &loadedConfig
	reader.stateLock.Unlock()
	return loadedConfig, nil
}

// roleProvider is the credentials of roleARN, assumed with the machine's credentials and renewed as they expire.
func (reader *S3SchemaReader) roleProvider(roleARN string, machineConfig aws.Config) aws.CredentialsProvider {
	reader.stateLock.Lock()
	defer reader.stateLock.Unlock()
	if heldProvider, isHeld := reader.roleCredentials[roleARN]; isHeld {
		return heldProvider
	}
	setSessionName := func(assumeOptions *stscreds.AssumeRoleOptions) {
		assumeOptions.RoleSessionName = reader.roleSessionName
	}
	assumedProvider := aws.NewCredentialsCache(
		stscreds.NewAssumeRoleProvider(sts.NewFromConfig(machineConfig), roleARN, setSessionName))
	reader.roleCredentials[roleARN] = assumedProvider
	return assumedProvider
}

func (reader *S3SchemaReader) bucketRegion(bucketName string, readConfig aws.Config) string {
	reader.stateLock.Lock()
	knownRegion, isKnown := reader.bucketRegions[bucketName]
	reader.stateLock.Unlock()
	switch {
	case isKnown:
		return knownRegion
	case readConfig.Region != "":
		return readConfig.Region
	}
	return defaultRegion
}

// forgetCredentials reads the machine's credentials again when next asked, after a refusal.
func (reader *S3SchemaReader) forgetCredentials() {
	reader.stateLock.Lock()
	defer reader.stateLock.Unlock()
	reader.machineConfig = nil
	clear(reader.roleCredentials)
}

func parseS3Object(objectURL string) (s3Object, error) {
	objectLink := ""
	if len(objectURL) >= len(s3Prefix) {
		objectLink = objectURL[len(s3Prefix):]
	}
	objectPath, queryText, hasQuery := strings.Cut(objectLink, "?")
	bucketName, objectKey, hasKey := strings.Cut(objectPath, "/")
	if !hasKey || bucketName == "" || objectKey == "" {
		return s3Object{}, errors.New("an S3 URL is " + yamldslspec.CodeSpan("s3://<bucket>/<key>"))
	}
	namedObject := s3Object{bucketName: bucketName, objectKey: objectKey}
	if !hasQuery {
		return namedObject, nil
	}
	for _, queryParameter := range strings.Split(queryText, "&") {
		parameterName, roleARN, hasValue := strings.Cut(queryParameter, "=")
		if !hasValue || parameterName != assumeRoleParameter || roleARN == "" {
			return s3Object{}, errors.New(yamldslspec.CodeSpan(parameterName) + " is not a parameter of an " +
				yamldslspec.CodeSpan("s3") + " URL; it takes " + yamldslspec.CodeSpan("assume_role=<role ARN>"))
		}
		namedObject.assumeRole = roleARN
	}
	return namedObject, nil
}

func sentAnswer(objectOutput *s3.GetObjectOutput) yamldslspec.SchemaURLAnswer {
	defer objectOutput.Body.Close()
	var entityTag any
	if objectOutput.ETag != nil {
		entityTag = *objectOutput.ETag
	}
	bodyBytes, readError := io.ReadAll(objectOutput.Body)
	if readError != nil {
		return yamldslspec.FailedAnswer(readError.Error())
	}
	if !utf8.Valid(bodyBytes) {
		return yamldslspec.FailedAnswer(notUTF8Reason)
	}
	return yamldslspec.OutcomeAnswer(yamldslspec.SchemaURLOutcome{
		OutcomeKind: yamldslspec.OutcomeTextSent,
		SchemaText:  string(bodyBytes),
		CopyVersion: map[string]any{"etag": entityTag},
	})
}

// serviceErrorOf is the answer S3 itself gave a read, where the read reached S3: its status, its headers and its
// error's code and message.
func serviceErrorOf(readError error) (*awshttp.ResponseError, string, string, bool) {
	var operationError *smithy.OperationError
	if errors.As(readError, &operationError) {
		var nestedOperation *smithy.OperationError
		if operationError.ServiceID != s3ServiceID || errors.As(operationError.Err, &nestedOperation) {
			return nil, "", "", false
		}
	}
	var responseError *awshttp.ResponseError
	if !errors.As(readError, &responseError) || responseError.Response == nil {
		return nil, "", "", false
	}
	var apiError smithy.APIError
	if errors.As(readError, &apiError) {
		return responseError, apiError.ErrorCode(), apiError.ErrorMessage(), true
	}
	return responseError, "", "", true
}

// redirectedRegionOf is the region a refused or redirected read names its bucket in, where it names one.
func redirectedRegionOf(readError error) (string, bool) {
	responseError, errorCode, _, isServiceError := serviceErrorOf(readError)
	if !isServiceError {
		return "", false
	}
	statusCode := responseError.HTTPStatusCode()
	isRedirected := slices.Contains(regionRedirectCodes, errorCode) ||
		statusCode == http.StatusMovedPermanently || statusCode == http.StatusTemporaryRedirect
	bucketRegion := responseError.Response.Header.Get(bucketRegionHeader)
	return bucketRegion, isRedirected && bucketRegion != ""
}

// errorAnswer is what an S3 error comes to.
func errorAnswer(readError error, namedObject s3Object, identity readIdentity) yamldslspec.SchemaURLAnswer {
	responseError, errorCode, errorMessage, isServiceError := serviceErrorOf(readError)
	if !isServiceError {
		errorText := readError.Error()
		if credentialsAnswer, isRefused := credentialsAnswerOf(errorText, identity); isRefused {
			return credentialsAnswer
		}
		return yamldslspec.FailedAnswer(errorText)
	}
	statusCode := responseError.HTTPStatusCode()
	if statusCode == http.StatusNotModified {
		return yamldslspec.OutcomeAnswer(yamldslspec.SchemaURLOutcome{OutcomeKind: yamldslspec.OutcomeCopyConfirmed})
	}
	switch errorCode {
	case "NoSuchKey":
		return yamldslspec.MissingAnswer("no key " + yamldslspec.CodeSpan(namedObject.objectKey) + " in bucket " +
			yamldslspec.CodeSpan(namedObject.bucketName))
	case "NoSuchBucket":
		return yamldslspec.MissingAnswer("no bucket " + yamldslspec.CodeSpan(namedObject.bucketName))
	}
	reasonText := refusalReasonOf(errorCode, errorMessage)
	if slices.Contains(credentialsRefusedCodes, errorCode) {
		return refusedAnswer(identity, reasonText, new(credentialsFix))
	}
	if errorCode == "AccessDenied" || statusCode == http.StatusForbidden {
		var renewalFix *string
		switch identity.identityKind {
		case machineIdentity:
			renewalFix = new("use a profile allowed `s3:GetObject` on it, such as `AWS_PROFILE=<profile>`")
		case assumedRoleIdentity:
			renewalFix = new("allow " + identity.roleARN + " `s3:GetObject` on it")
		}
		return refusedAnswer(identity, reasonText, renewalFix)
	}
	if errorCode == "" {
		return yamldslspec.FailedAnswer("S3 answered " + strconv.Itoa(statusCode))
	}
	return yamldslspec.FailedAnswer(reasonText)
}

// credentialsAnswerOf is the refusal an error short of S3 comes to: credentials the chain does not give, or a role
// the machine is not allowed to assume.
func credentialsAnswerOf(errorText string, identity readIdentity) (yamldslspec.SchemaURLAnswer, bool) {
	machineReader := readIdentity{identityKind: machineIdentity}
	if identity.identityKind == assumedRoleIdentity &&
		(strings.Contains(errorText, "AccessDenied") || strings.Contains(errorText, "not authorized")) {
		return refusedAnswer(machineReader, refusalReasonOf("AccessDenied", errorText),
			new("use a profile allowed to assume "+identity.roleARN+", such as `AWS_PROFILE=<profile>`")), true
	}
	if !strings.Contains(errorText, "credentials") {
		return yamldslspec.SchemaURLAnswer{}, false
	}
	for _, missingPhrase := range credentialsMissingPhrases {
		if strings.Contains(errorText, missingPhrase) {
			return refusedAnswer(machineReader, "the machine's AWS credentials are absent or expired",
				new(credentialsFix)), true
		}
	}
	return yamldslspec.SchemaURLAnswer{}, false
}

func refusedAnswer(identity readIdentity, refusalReason string, renewalFix *string) yamldslspec.SchemaURLAnswer {
	var credentialOrigin *string
	switch identity.identityKind {
	case machineIdentity:
		credentialOrigin = new(machineCredentialsOrigin)
	case assumedRoleIdentity:
		credentialOrigin = new("role " + identity.roleARN)
	}
	return yamldslspec.RefusedAnswer([]yamldslspec.ReadRefusal{{
		RefusedBy:        awsRefuser,
		CredentialOrigin: credentialOrigin,
		RefusalReason:    refusalReason,
		RenewalFix:       renewalFix,
	}})
}

// refusalReasonOf is an S3 error cut to its code and the identity it names, such as
// `AccessDenied for arn:aws:iam::<account>:user/<name>`.
func refusalReasonOf(errorCode, errorMessage string) string {
	if errorCode == "" {
		return errorMessage
	}
	if _, afterUser, namesUser := strings.Cut(errorMessage, "User: "); namesUser {
		if namedIdentity := strings.Fields(afterUser); len(namedIdentity) > 0 {
			return errorCode + " for " + namedIdentity[0]
		}
	}
	return errorCode
}
