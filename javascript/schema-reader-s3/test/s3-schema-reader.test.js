'use strict';

const assert = require('node:assert');
const test = require('node:test');

const {
  S3Client, S3ServiceException, CreateBucketCommand, PutObjectCommand, ListObjectsV2Command, DeleteObjectCommand,
  DeleteBucketCommand,
} = require('@aws-sdk/client-s3');

const { S3SchemaReader } = require('..');
const {
  s3SchemaReaderAt, parsedS3Object, errorAnswer, redirectedRegion, credentialsAnswer, refusalReason,
} = require('../lib/s3-schema-reader');

const MOCK_SCHEMA = '{"type": "object"}';
const CREDENTIALS_FIX = 'run `aws sso login`, or set `AWS_PROFILE` to a profile that reads it';
const MOCK_ROLE = 'arn:aws:iam::000000000000:role/mock-reader';

/** The Moto server the tests read from. */
function motoEndpoint() {
  const endpoint = process.env.AWS_ENDPOINT_URL;
  assert.ok(endpoint, 'the S3 tests read from a Moto server: set AWS_ENDPOINT_URL to it');
  assert.ok(
    /^http:\/\/(127\.0\.0\.1|localhost):[0-9]+\/?$/u.test(endpoint),
    `the S3 tests read only from a local Moto server, not ${endpoint}`,
  );
  return endpoint;
}

function mockReader() {
  return s3SchemaReaderAt('mock-session', motoEndpoint());
}

function seedingClient() {
  return new S3Client({ endpoint: motoEndpoint(), forcePathStyle: true });
}

function mockRequest(fetchUrl, machineCredentials, keptCopyVersion = null) {
  return { fetchUrl, machineCredentials, keptCopyVersion, signal: AbortSignal.timeout(4000) };
}

/**
 * A bucket in the Moto server holding `objects`, anyone allowed to read those `publicKeys` names, created
 * before each test of a suite and deleted after it.
 */
function seededBucket(bucketName, objects, publicKeys = []) {
  test.beforeEach(async () => {
    const client = seedingClient();
    await client.send(new CreateBucketCommand({ Bucket: bucketName }));
    for (const [objectKey, objectBody] of Object.entries(objects)) {
      const objectAcl = publicKeys.includes(objectKey) ? 'public-read' : undefined;
      await client.send(new PutObjectCommand({ Bucket: bucketName, Key: objectKey, Body: objectBody, ACL: objectAcl }));
    }
  });
  test.afterEach(async () => {
    const client = seedingClient();
    const listing = await client.send(new ListObjectsV2Command({ Bucket: bucketName }));
    for (const listed of listing.Contents || []) {
      await client.send(new DeleteObjectCommand({ Bucket: bucketName, Key: listed.Key }));
    }
    await client.send(new DeleteBucketCommand({ Bucket: bucketName }));
  });
}

test.describe('an object in a bucket', () => {
  seededBucket(
    'mock-sent-bucket',
    { 'schemas/mock.schema.json': MOCK_SCHEMA, 'schemas/public.schema.json': MOCK_SCHEMA },
    ['schemas/public.schema.json'],
  );

  test('an object is sent with its etag and a current copy is confirmed', async () => {
    const reader = mockReader();
    const readCases = [
      { url: 's3://mock-sent-bucket/schemas/public.schema.json', machineCredentials: false },
      { url: 's3://mock-sent-bucket/schemas/mock.schema.json', machineCredentials: true },
    ];
    for (const { url, machineCredentials } of readCases) {
      const sent = await reader.readSchemaUrl(mockRequest(url, machineCredentials));
      assert.strictEqual(sent.readOutcome.outcomeKind, 'textSent', JSON.stringify(sent));
      assert.strictEqual(sent.readOutcome.schemaText, MOCK_SCHEMA);
      assert.strictEqual(typeof sent.readOutcome.copyVersion.etag, 'string');
      const confirmed = await reader.readSchemaUrl(mockRequest(url, machineCredentials, sent.readOutcome.copyVersion));
      assert.deepStrictEqual(confirmed.readOutcome, { outcomeKind: 'copyConfirmed' });
    }
  });

  test('a private object read without credentials is refused', async () => {
    const privateUrl = 's3://mock-sent-bucket/schemas/mock.schema.json';
    const refused = await mockReader().readSchemaUrl(mockRequest(privateUrl, false));
    assert.strictEqual(refused.readOutcome.outcomeKind, 'readRefused', JSON.stringify(refused));
    assert.strictEqual(refused.readRefusals[0].credentialOrigin, null);
    assert.strictEqual(refused.readRefusals[0].refusedBy, 'AWS');
    assert.notStrictEqual(refused.readRefusals[0].refusalReason, '');
  });
});

test.describe('an object read as a role', () => {
  seededBucket('mock-role-bucket', { 'mock.schema.json': MOCK_SCHEMA });

  test('an object is read as the role its url names', async () => {
    const url = `s3://mock-role-bucket/mock.schema.json?assume_role=${MOCK_ROLE}`;
    const sent = await mockReader().readSchemaUrl(mockRequest(url, true));
    assert.strictEqual(sent.readOutcome.outcomeKind, 'textSent', JSON.stringify(sent));
  });
});

test.describe('an empty bucket', () => {
  seededBucket('mock-missing-bucket', {});

  test('a key or a bucket that is not there is missing', async () => {
    const reader = mockReader();
    const missingKey = await reader.readSchemaUrl(mockRequest('s3://mock-missing-bucket/absent.json', false));
    assert.deepStrictEqual(missingKey.readOutcome, {
      outcomeKind: 'sourceMissing',
      missingReason: 'no key `absent.json` in bucket `mock-missing-bucket`',
    });
    const missingBucket = await reader.readSchemaUrl(mockRequest('s3://mock-absent-bucket/absent.json', true));
    assert.deepStrictEqual(missingBucket.readOutcome, {
      outcomeKind: 'sourceMissing',
      missingReason: 'no bucket `mock-absent-bucket`',
    });
  });
});

test('an s3 url takes its bucket its key and only the assume role parameter', async () => {
  assert.deepStrictEqual(
    parsedS3Object('s3://mock-bucket/a/b.json?assume_role=arn:mock'),
    { bucketName: 'mock-bucket', objectKey: 'a/b.json', assumedRole: 'arn:mock' },
  );
  assert.throws(
    () => parsedS3Object('s3://mock-bucket/a.json?region=x'),
    { message: '`region` is not a parameter of an `s3` URL; it takes `assume_role=<role ARN>`' },
  );
  assert.throws(() => parsedS3Object('s3://mock-bucket'), { message: 'an S3 URL is `s3://<bucket>/<key>`' });
  const reader = new S3SchemaReader('mock-session');
  assert.deepStrictEqual(reader.urlPrefixes, ['s3://']);
  assert.strictEqual(reader.readsRemoteSources, true);
  const failed = await reader.readSchemaUrl(mockRequest('s3://mock-bucket/a.json?mock=1', false));
  assert.strictEqual(failed.readOutcome.outcomeKind, 'readFailed');
});

test('an answer not ready when it is due fails', async () => {
  const late = await mockReader().readSchemaUrl({
    fetchUrl: 's3://mock-bucket/a.json',
    machineCredentials: false,
    keptCopyVersion: null,
    signal: AbortSignal.abort(),
  });
  assert.deepStrictEqual(late.readOutcome, { outcomeKind: 'readFailed', failureReason: 'S3 did not answer in time' });
});

/** An error S3 answers with `status`, its code and message, and its bucket's region where it names one. */
function mockServiceError(status, code, message, bucketRegion = null) {
  const serviceError = new S3ServiceException({
    name: code || 'UnknownError',
    $fault: 'client',
    message,
    $metadata: { httpStatusCode: status },
  });
  serviceError.$response = { headers: bucketRegion === null ? {} : { 'x-amz-bucket-region': bucketRegion } };
  if (code) serviceError.Code = code;
  return serviceError;
}

test('an s3 error is refused, redirected or failed by its code and status', () => {
  const s3Object = { bucketName: 'mock-bucket', objectKey: 'a.json', assumedRole: null };
  const denied = 'User: arn:aws:iam::000000000000:user/mock is not authorized';
  const machine = { identityKind: 'machine' };
  const machineDenied = errorAnswer(mockServiceError(403, 'AccessDenied', denied), s3Object, machine);
  assert.deepStrictEqual(machineDenied.readRefusals, [{
    refusedBy: 'AWS',
    credentialOrigin: "the machine's AWS credentials",
    refusalReason: 'AccessDenied for arn:aws:iam::000000000000:user/mock',
    renewalFix: 'use a profile allowed `s3:GetObject` on it, such as `AWS_PROFILE=<profile>`',
  }]);
  const role = { identityKind: 'assumedRole', assumedRole: 'arn:mock-role' };
  const roleDenied = errorAnswer(mockServiceError(403, 'AccessDenied', 'mock'), s3Object, role);
  assert.strictEqual(roleDenied.readRefusals[0].renewalFix, 'allow arn:mock-role `s3:GetObject` on it');
  assert.strictEqual(roleDenied.readRefusals[0].credentialOrigin, 'role arn:mock-role');
  const unsignedDenied = errorAnswer(mockServiceError(403, '', 'mock'), s3Object, { identityKind: 'unsigned' });
  assert.deepStrictEqual(unsignedDenied.readRefusals[0], {
    refusedBy: 'AWS', credentialOrigin: null, refusalReason: 'mock', renewalFix: null,
  });
  const expired = errorAnswer(mockServiceError(400, 'ExpiredToken', 'mock'), s3Object, { identityKind: 'machine' });
  assert.strictEqual(expired.readRefusals[0].renewalFix, CREDENTIALS_FIX);
  assert.deepStrictEqual(
    errorAnswer(mockServiceError(500, '', ''), s3Object, { identityKind: 'unsigned' }).readOutcome,
    { outcomeKind: 'readFailed', failureReason: 'S3 answered 500' },
  );
  assert.deepStrictEqual(
    errorAnswer(mockServiceError(500, 'InternalError', 'mock'), s3Object, { identityKind: 'unsigned' }).readOutcome,
    { outcomeKind: 'readFailed', failureReason: 'InternalError' },
  );
  assert.strictEqual(redirectedRegion(mockServiceError(301, 'PermanentRedirect', '', 'eu-west-1')), 'eu-west-1');
  assert.strictEqual(redirectedRegion(mockServiceError(403, 'AccessDenied', '', 'eu-west-1')), null);
  assert.strictEqual(redirectedRegion(new Error('mock')), null);
});

test('a refusal names its code the identity refused and what renews it', () => {
  assert.strictEqual(
    refusalReason(
      'AccessDenied',
      'User: arn:aws:iam::000000000000:user/mock is not authorized to perform: s3:GetObject',
    ),
    'AccessDenied for arn:aws:iam::000000000000:user/mock',
  );
  assert.strictEqual(refusalReason('ExpiredToken', 'mock'), 'ExpiredToken');
  const role = { identityKind: 'assumedRole', assumedRole: 'arn:mock-role' };
  const denied = credentialsAnswer('AccessDenied: User: arn:mock-user is not authorized', 'AccessDenied', role);
  assert.strictEqual(
    denied.readRefusals[0].renewalFix,
    'use a profile allowed to assume arn:mock-role, such as `AWS_PROFILE=<profile>`',
  );
  assert.strictEqual(denied.readRefusals[0].refusalReason, 'AccessDenied for arn:mock-user');
  assert.strictEqual(denied.readRefusals[0].credentialOrigin, "the machine's AWS credentials");
  const machine = { identityKind: 'machine' };
  const absent = credentialsAnswer(
    'CredentialsProviderError: Could not load credentials from any providers',
    'CredentialsProviderError',
    machine,
  );
  assert.strictEqual(absent.readRefusals[0].renewalFix, CREDENTIALS_FIX);
  assert.strictEqual(credentialsAnswer('TypeError: fetch failed', 'TypeError', machine), null);
});
