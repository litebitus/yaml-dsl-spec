package schema_reader_s3

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	yamldslspec "github.com/litebitus/yaml-dsl-spec/go"
)

const mockSchemaText = `{"type": "object"}`

// motoEndpoint is the Moto server the tests read from.
func motoEndpoint(testingContext *testing.T) string {
	endpointURL := os.Getenv("AWS_ENDPOINT_URL")
	if endpointURL == "" {
		testingContext.Fatal("the S3 tests read from a Moto server: set `AWS_ENDPOINT_URL` to its address")
	}
	if !strings.HasPrefix(endpointURL, "http://127.0.0.1:") && !strings.HasPrefix(endpointURL, "http://localhost:") {
		testingContext.Fatalf("the S3 tests read only from a local Moto server, not %s", endpointURL)
	}
	return endpointURL
}

func motoReader(testingContext *testing.T) *S3SchemaReader {
	motoEndpoint(testingContext)
	return NewS3SchemaReader("mock-session")
}

// motoRecorderCall is a call of the Moto server's request recorder, such as `start-recording`, and its answer.
func motoRecorderCall(testingContext *testing.T, httpMethod, recorderAction string) string {
	recorderContext, cancelRecorder := context.WithTimeout(context.Background(), time.Second)
	defer cancelRecorder()
	recorderRequest, requestError := http.NewRequestWithContext(recorderContext, httpMethod,
		motoEndpoint(testingContext)+"/moto-api/recorder/"+recorderAction, nil)
	if requestError != nil {
		testingContext.Fatal(requestError)
	}
	recorderResponse, responseError := http.DefaultClient.Do(recorderRequest)
	if responseError != nil {
		testingContext.Fatal(responseError)
	}
	defer recorderResponse.Body.Close()
	recorderAnswer, readError := io.ReadAll(recorderResponse.Body)
	if readError != nil || recorderResponse.StatusCode != http.StatusOK {
		testingContext.Fatalf("%s answered %d: %v", recorderAction, recorderResponse.StatusCode, readError)
	}
	return string(recorderAnswer)
}

// seedBucket creates bucketName holding seededObjects in the Moto server, anyone allowed to read the objects
// publicKeys names, and removes both when the test ends.
func seedBucket(
	testingContext *testing.T, bucketName string, seededObjects map[string]string, publicKeys ...string,
) {
	endpointURL := motoEndpoint(testingContext)
	seedContext, cancelSeed := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelSeed()
	machineConfig, configError := config.LoadDefaultConfig(seedContext)
	if configError != nil {
		testingContext.Fatal(configError)
	}
	seedClient := s3.NewFromConfig(machineConfig, func(clientOptions *s3.Options) {
		clientOptions.BaseEndpoint = aws.String(endpointURL)
		clientOptions.UsePathStyle = true
	})
	if _, createError := seedClient.CreateBucket(seedContext, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	}); createError != nil {
		testingContext.Fatal(createError)
	}
	for objectKey, objectBody := range seededObjects {
		putInput := &s3.PutObjectInput{
			Bucket: aws.String(bucketName), Key: aws.String(objectKey), Body: strings.NewReader(objectBody),
		}
		for _, publicKey := range publicKeys {
			if publicKey == objectKey {
				putInput.ACL = s3types.ObjectCannedACLPublicRead
			}
		}
		if _, putError := seedClient.PutObject(seedContext, putInput); putError != nil {
			testingContext.Fatal(putError)
		}
	}
	testingContext.Cleanup(func() {
		cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelCleanup()
		for objectKey := range seededObjects {
			_, _ = seedClient.DeleteObject(cleanupContext, &s3.DeleteObjectInput{
				Bucket: aws.String(bucketName), Key: aws.String(objectKey),
			})
		}
		_, _ = seedClient.DeleteBucket(cleanupContext, &s3.DeleteBucketInput{Bucket: aws.String(bucketName)})
	})
}

func readObject(
	testingContext *testing.T, reader *S3SchemaReader, objectURL string, machineCredentials bool, keptVersion any,
) yamldslspec.SchemaURLAnswer {
	readContext, cancelRead := context.WithTimeout(context.Background(), 4*time.Second)
	testingContext.Cleanup(cancelRead)
	return reader.ReadSchemaURL(readContext, yamldslspec.SchemaURLRequest{
		FetchURL: objectURL, MachineCredentials: machineCredentials, KeptCopyVersion: keptVersion,
	})
}

func TestAnObjectIsSentWithItsETagAndACurrentCopyIsConfirmed(testingContext *testing.T) {
	reader := motoReader(testingContext)
	seedBucket(testingContext, "mock-sent-bucket", map[string]string{
		"schemas/mock.schema.json":   mockSchemaText,
		"schemas/public.schema.json": mockSchemaText,
	}, "schemas/public.schema.json")
	readCases := []struct {
		objectURL          string
		machineCredentials bool
	}{
		{objectURL: "s3://mock-sent-bucket/schemas/public.schema.json", machineCredentials: false},
		{objectURL: "s3://mock-sent-bucket/schemas/mock.schema.json", machineCredentials: true},
	}
	for _, readCase := range readCases {
		objectURL, machineCredentials := readCase.objectURL, readCase.machineCredentials
		sentAnswer := readObject(testingContext, reader, objectURL, machineCredentials, nil)
		sentOutcome := sentAnswer.ReadOutcome
		versionFields, _ := sentOutcome.CopyVersion.(map[string]any)
		if entityTag, _ := versionFields["etag"].(string); sentOutcome.OutcomeKind != yamldslspec.OutcomeTextSent ||
			sentOutcome.SchemaText != mockSchemaText || entityTag == "" {
			testingContext.Fatalf("sent %+v", sentAnswer)
		}
		confirmedAnswer := readObject(testingContext, reader, objectURL, machineCredentials, sentOutcome.CopyVersion)
		if confirmedAnswer.ReadOutcome.OutcomeKind != yamldslspec.OutcomeCopyConfirmed {
			testingContext.Fatalf("confirmed %+v", confirmedAnswer)
		}
	}
}

func TestAnObjectIsReadByItsPathWhereTheEnvironmentNamesTheEndpoint(testingContext *testing.T) {
	hostEndpoint := strings.Replace(motoEndpoint(testingContext), "http://127.0.0.1:", "http://localhost:", 1)
	testingContext.Setenv("AWS_ENDPOINT_URL", hostEndpoint)
	seedBucket(testingContext, "mock-path-bucket", map[string]string{"mock.schema.json": mockSchemaText},
		"mock.schema.json")
	motoRecorderCall(testingContext, http.MethodPost, "reset-recording")
	motoRecorderCall(testingContext, http.MethodPost, "start-recording")
	testingContext.Cleanup(func() {
		motoRecorderCall(testingContext, http.MethodPost, "stop-recording")
		motoRecorderCall(testingContext, http.MethodPost, "reset-recording")
	})
	sentAnswer := readObject(testingContext, NewS3SchemaReader("mock-session"),
		"s3://mock-path-bucket/mock.schema.json", false, nil)
	if sentAnswer.ReadOutcome.OutcomeKind != yamldslspec.OutcomeTextSent {
		testingContext.Fatalf("sent %+v", sentAnswer)
	}
	motoRecorderCall(testingContext, http.MethodPost, "stop-recording")
	pathURL := hostEndpoint + "/mock-path-bucket/mock.schema.json"
	recordedRequests := motoRecorderCall(testingContext, http.MethodGet, "download-recording")
	for recordedLine := range strings.SplitSeq(recordedRequests, "\n") {
		var recordedRequest struct {
			RequestURL string `json:"url"`
		}
		if json.Unmarshal([]byte(recordedLine), &recordedRequest) == nil &&
			strings.HasPrefix(recordedRequest.RequestURL, pathURL) {
			return
		}
	}
	testingContext.Fatalf("no request to %s in %s", pathURL, recordedRequests)
}

func TestAPrivateObjectReadWithoutCredentialsIsRefused(testingContext *testing.T) {
	reader := motoReader(testingContext)
	seedBucket(testingContext, "mock-private-bucket", map[string]string{"mock.schema.json": mockSchemaText})
	refusedAnswer := readObject(testingContext, reader, "s3://mock-private-bucket/mock.schema.json", false, nil)
	if refusedAnswer.ReadOutcome.OutcomeKind != yamldslspec.OutcomeReadRefused ||
		len(refusedAnswer.ReadRefusals) != 1 || refusedAnswer.ReadRefusals[0].CredentialOrigin != nil ||
		refusedAnswer.ReadRefusals[0].RefusedBy != "AWS" || refusedAnswer.ReadRefusals[0].RefusalReason == "" {
		testingContext.Fatalf("refused %+v", refusedAnswer)
	}
}

func TestAnObjectThatIsNotUTF8Fails(testingContext *testing.T) {
	reader := motoReader(testingContext)
	seedBucket(testingContext, "mock-binary-bucket", map[string]string{"mock.schema.json": "\xff\xfe"})
	binaryAnswer := readObject(testingContext, reader, "s3://mock-binary-bucket/mock.schema.json", true, nil)
	if binaryAnswer.ReadOutcome != (yamldslspec.SchemaURLOutcome{
		OutcomeKind: yamldslspec.OutcomeReadFailed, FailureReason: notUTF8Reason,
	}) {
		testingContext.Fatalf("binary %+v", binaryAnswer)
	}
}

func TestAnObjectIsReadAsTheRoleItsURLNames(testingContext *testing.T) {
	reader := motoReader(testingContext)
	seedBucket(testingContext, "mock-role-bucket", map[string]string{"mock.schema.json": mockSchemaText})
	objectURL := "s3://mock-role-bucket/mock.schema.json?assume_role=arn:aws:iam::000000000000:role/mock-reader"
	if sentAnswer := readObject(testingContext, reader, objectURL, true, nil); sentAnswer.ReadOutcome.OutcomeKind !=
		yamldslspec.OutcomeTextSent {
		testingContext.Fatalf("sent %+v", sentAnswer)
	}
}

func TestAKeyOrABucketThatIsNotThereIsMissing(testingContext *testing.T) {
	reader := motoReader(testingContext)
	seedBucket(testingContext, "mock-missing-bucket", map[string]string{})
	missingKey := readObject(testingContext, reader, "s3://mock-missing-bucket/absent.json", false, nil)
	if missingKey.ReadOutcome != (yamldslspec.SchemaURLOutcome{
		OutcomeKind:   yamldslspec.OutcomeSourceMissing,
		MissingReason: "no key `absent.json` in bucket `mock-missing-bucket`",
	}) {
		testingContext.Fatalf("missing key %+v", missingKey)
	}
	missingBucket := readObject(testingContext, reader, "s3://mock-absent-bucket/absent.json", true, nil)
	if missingBucket.ReadOutcome != (yamldslspec.SchemaURLOutcome{
		OutcomeKind: yamldslspec.OutcomeSourceMissing, MissingReason: "no bucket `mock-absent-bucket`",
	}) {
		testingContext.Fatalf("missing bucket %+v", missingBucket)
	}
}

func TestAnS3URLTakesItsBucketItsKeyAndOnlyTheAssumeRoleParameter(testingContext *testing.T) {
	namedObject, parseError := parseS3Object("s3://mock-bucket/a/b.json?assume_role=arn:mock")
	if parseError != nil ||
		namedObject != (s3Object{bucketName: "mock-bucket", objectKey: "a/b.json", assumeRole: "arn:mock"}) {
		testingContext.Fatalf("parsed %+v, %v", namedObject, parseError)
	}
	expectedErrors := map[string]string{
		"s3://mock-bucket/a.json?region=x": "`region` is not a parameter of an `s3` URL; it takes " +
			"`assume_role=<role ARN>`",
		"s3://mock-bucket": "an S3 URL is `s3://<bucket>/<key>`",
	}
	for objectURL, expectedError := range expectedErrors {
		if _, parseError := parseS3Object(objectURL); parseError == nil || parseError.Error() != expectedError {
			testingContext.Fatalf("%s parses to %v", objectURL, parseError)
		}
	}
	failedAnswer := readObject(testingContext, motoReader(testingContext),
		"s3://mock-bucket/a.json?mock=1", false, nil)
	if failedAnswer.ReadOutcome.OutcomeKind != yamldslspec.OutcomeReadFailed {
		testingContext.Fatalf("failed %+v", failedAnswer)
	}
	reader := NewS3SchemaReader("mock-session")
	if len(reader.URLPrefixes()) != 1 || reader.URLPrefixes()[0] != "s3://" || !reader.ReadsRemoteSources() {
		testingContext.Fatal("the reader reads other URLs")
	}
}

func TestARefusalNamesItsCodeTheIdentityRefusedAndWhatRenewsIt(testingContext *testing.T) {
	deniedReason := refusalReasonOf("AccessDenied",
		"User: arn:aws:iam::000000000000:user/mock is not authorized to perform: s3:GetObject")
	if deniedReason != "AccessDenied for arn:aws:iam::000000000000:user/mock" ||
		refusalReasonOf("ExpiredToken", "mock") != "ExpiredToken" || refusalReasonOf("", "mock") != "mock" {
		testingContext.Fatalf("reason %s", deniedReason)
	}
	roleReader := readIdentity{identityKind: assumedRoleIdentity, roleARN: "arn:mock-role"}
	deniedAnswer, isRefused := credentialsAnswerOf("AccessDenied: User: arn:mock-user is not authorized", roleReader)
	if !isRefused ||
		*deniedAnswer.ReadRefusals[0].RenewalFix !=
			"use a profile allowed to assume arn:mock-role, such as `AWS_PROFILE=<profile>`" ||
		deniedAnswer.ReadRefusals[0].RefusalReason != "AccessDenied for arn:mock-user" {
		testingContext.Fatalf("denied %+v", deniedAnswer)
	}
	machineReader := readIdentity{identityKind: machineIdentity}
	absentAnswer, isRefused := credentialsAnswerOf(
		"get identity: get credentials: failed to refresh cached credentials, no EC2 IMDS role found", machineReader)
	if !isRefused || *absentAnswer.ReadRefusals[0].RenewalFix != credentialsFix {
		testingContext.Fatalf("absent %+v", absentAnswer)
	}
	if _, isRefused := credentialsAnswerOf("dispatch failure: io error", machineReader); isRefused {
		testingContext.Fatal("an error short of credentials was refused")
	}
}

// s3ServiceError is an error S3 answered a GetObject with: its status, its code and message, and its headers.
func s3ServiceError(statusCode int, errorCode, errorMessage string, responseHeaders http.Header) error {
	return &smithy.OperationError{
		ServiceID:     "S3",
		OperationName: "GetObject",
		Err: &awshttp.ResponseError{ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: statusCode, Header: responseHeaders}},
			Err:      &smithy.GenericAPIError{Code: errorCode, Message: errorMessage},
		}},
	}
}

func TestWhatS3AnswersComesToItsOutcomeAndRenewalFix(testingContext *testing.T) {
	namedObject := s3Object{bucketName: "mock-bucket", objectKey: "mock.json"}
	roleReader := readIdentity{identityKind: assumedRoleIdentity, roleARN: "arn:mock-role"}
	machineReader := readIdentity{identityKind: machineIdentity}
	unsignedReader := readIdentity{identityKind: unsignedIdentity}
	expectedFixes := map[readIdentity]string{
		roleReader:    "allow arn:mock-role `s3:GetObject` on it",
		machineReader: "use a profile allowed `s3:GetObject` on it, such as `AWS_PROFILE=<profile>`",
	}
	for identity, expectedFix := range expectedFixes {
		deniedAnswer := errorAnswer(s3ServiceError(403, "AccessDenied", "mock", nil), namedObject, identity)
		if *deniedAnswer.ReadRefusals[0].RenewalFix != expectedFix {
			testingContext.Fatalf("denied %+v", deniedAnswer)
		}
	}
	unsignedAnswer := errorAnswer(s3ServiceError(403, "", "", nil), namedObject, unsignedReader)
	if unsignedAnswer.ReadRefusals[0].CredentialOrigin != nil || unsignedAnswer.ReadRefusals[0].RenewalFix != nil {
		testingContext.Fatalf("unsigned %+v", unsignedAnswer)
	}
	expiredAnswer := errorAnswer(s3ServiceError(400, "ExpiredToken", "mock", nil), namedObject, machineReader)
	if *expiredAnswer.ReadRefusals[0].CredentialOrigin != machineCredentialsOrigin {
		testingContext.Fatalf("expired %+v", expiredAnswer)
	}
	expectedFailures := map[error]string{
		s3ServiceError(500, "", "", nil):                      "S3 answered 500",
		s3ServiceError(500, "InternalError", "mock", nil):     "InternalError",
		errors.New("dispatch failure: mock connection error"): "dispatch failure: mock connection error",
	}
	for readError, expectedReason := range expectedFailures {
		if failedAnswer := errorAnswer(readError, namedObject, machineReader); failedAnswer.ReadOutcome.FailureReason !=
			expectedReason {
			testingContext.Fatalf("%v fails with %+v", readError, failedAnswer)
		}
	}
	redirectHeaders := http.Header{"X-Amz-Bucket-Region": {"mock-region"}}
	redirectError := s3ServiceError(301, "PermanentRedirect", "", redirectHeaders)
	if bucketRegion, isRedirected := redirectedRegionOf(redirectError); !isRedirected || bucketRegion != "mock-region" {
		testingContext.Fatalf("redirected to %s", bucketRegion)
	}
	credentialsError := &smithy.OperationError{ServiceID: "S3", OperationName: "GetObject", Err: &smithy.OperationError{
		ServiceID: "STS", OperationName: "AssumeRole", Err: s3ServiceError(403, "AccessDenied", "mock", nil),
	}}
	if _, isRedirected := redirectedRegionOf(credentialsError); isRedirected {
		testingContext.Fatal("an STS error was read as S3's")
	}
}
