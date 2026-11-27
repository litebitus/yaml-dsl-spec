'use strict';

/*
 * The YAML DSL standard's reader of schema URLs on S3, `s3://<bucket>/<key>`,
 * read with the AWS SDK: unsigned without the machine's credentials, else with
 * the SDK's default chain, as the role a URL's `assume_role` names where it
 * names one.
 */

const { S3Client, GetObjectCommand, S3ServiceException } = require('@aws-sdk/client-s3');
const { fromNodeProviderChain, fromTemporaryCredentials } = require('@aws-sdk/credential-providers');
const { codeSpan, schemaResolution } = require('yaml-dsl-spec');

const { answerOf, refusedAnswer, failedAnswer, missingAnswer } = schemaResolution;

const S3_PREFIX = 's3://';
const ASSUME_ROLE_PARAMETER = 'assume_role';
const DEFAULT_REGION = 'us-east-1';
const BUCKET_REGION_HEADER = 'x-amz-bucket-region';
const AWS_REFUSER = 'AWS';
const MACHINE_CREDENTIALS_ORIGIN = "the machine's AWS credentials";
const STATUS_NOT_MODIFIED = 304;
const STATUS_FORBIDDEN = 403;
const CREDENTIALS_REFUSED_CODES = [
  'ExpiredToken',
  'InvalidAccessKeyId',
  'InvalidClientTokenId',
  'SignatureDoesNotMatch',
];
const REGION_REDIRECT_CODES = [
  'PermanentRedirect',
  'AuthorizationHeaderMalformed',
  'IllegalLocationConstraintException',
];
const REGION_REDIRECT_STATUSES = [301, 307];
const CREDENTIALS_PROVIDER_ERRORS = ['CredentialsProviderError', 'TokenProviderError'];
const CREDENTIALS_FIX = 'run `aws sso login`, or set `AWS_PROFILE` to a profile that reads it';
const UNSIGNED_IDENTITY = { identityKind: 'unsigned' };
const MACHINE_IDENTITY = { identityKind: 'machine' };
const UTF8_TEXT = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });

/** The URL reader of `s3://<bucket>/<key>`. */
class S3SchemaReader {
  /** The reader, a role assumed under `roleSessionName`, the consumer's name for itself. */
  constructor(roleSessionName) {
    this.urlPrefixes = [S3_PREFIX];
    this.readsRemoteSources = true;
    this.roleSessionName = roleSessionName;
    this.endpointOverride = null;
    this.machineCredentials = null;
    this.machineRegion = undefined;
    this.identityClients = new Map();
    this.bucketRegions = new Map();
  }

  async readSchemaUrl(request) {
    let s3Object;
    try {
      s3Object = parsedS3Object(request.fetchUrl);
    } catch (error) {
      return failedAnswer(error.message);
    }
    let identity = UNSIGNED_IDENTITY;
    if (request.machineCredentials) {
      identity = s3Object.assumedRole === null
        ? MACHINE_IDENTITY
        : { identityKind: 'assumedRole', assumedRole: s3Object.assumedRole };
    }
    const keptVersion = request.keptCopyVersion;
    const keptEtag = keptVersion !== null && typeof keptVersion === 'object' && typeof keptVersion.etag === 'string'
      ? keptVersion.etag
      : null;
    const answerSignal = request.signal || new AbortController().signal;
    const lateAnswer = new Promise((resolve) => {
      const answerLate = () => resolve(failedAnswer('S3 did not answer in time'));
      if (answerSignal.aborted) answerLate();
      else answerSignal.addEventListener('abort', answerLate, { once: true });
    });
    return Promise.race([this.answerOf(s3Object, identity, keptEtag, answerSignal), lateAnswer]);
  }

  async answerOf(s3Object, identity, keptEtag, answerSignal) {
    let read;
    try {
      const region = await this.bucketRegion(s3Object.bucketName);
      read = await this.objectRead(identity, region, s3Object, keptEtag, answerSignal);
      const bucketRegion = read.readError ? redirectedRegion(read.readError) : null;
      if (bucketRegion !== null && bucketRegion !== region) {
        this.bucketRegions.set(s3Object.bucketName, bucketRegion);
        read = await this.objectRead(identity, bucketRegion, s3Object, keptEtag, answerSignal);
      }
    } catch (error) {
      read = { readError: error };
    }
    const answer = read.readError
      ? errorAnswer(read.readError, s3Object, identity)
      : await sentAnswer(read.objectOutput);
    if (answer.readOutcome.outcomeKind === 'readRefused' && identity.identityKind !== 'unsigned') {
      this.forgetCredentials();
    }
    return answer;
  }

  /** The object as `identity` reads it in `region`: `{ objectOutput }`, or `{ readError }`. */
  async objectRead(identity, region, s3Object, keptEtag, answerSignal) {
    const command = new GetObjectCommand({
      Bucket: s3Object.bucketName,
      Key: s3Object.objectKey,
      ...(keptEtag === null ? {} : { IfNoneMatch: keptEtag }),
    });
    try {
      return { objectOutput: await this.identityClient(identity, region).send(command, { abortSignal: answerSignal }) };
    } catch (error) {
      return { readError: error };
    }
  }

  /** The client a read as `identity` in `region` is made with, kept with its credentials until they are refused. */
  identityClient(identity, region) {
    const clientKey = JSON.stringify([identity.identityKind, identity.assumedRole || null, region]);
    if (!this.identityClients.has(clientKey)) {
      const clientConfig = { region, credentials: this.credentialsOf(identity) };
      if (identity.identityKind === 'unsigned') {
        clientConfig.signer = { sign: async (unsignedRequest) => unsignedRequest };
      }
      if (this.endpointOverride !== null) {
        clientConfig.endpoint = this.endpointOverride;
        clientConfig.forcePathStyle = true;
      }
      this.identityClients.set(clientKey, new S3Client(clientConfig));
    }
    return this.identityClients.get(clientKey);
  }

  credentialsOf(identity) {
    if (identity.identityKind === 'unsigned') return async () => ({ accessKeyId: '', secretAccessKey: '' });
    if (this.machineCredentials === null) this.machineCredentials = fromNodeProviderChain();
    if (identity.identityKind === 'machine') return this.machineCredentials;
    return fromTemporaryCredentials({
      params: { RoleArn: identity.assumedRole, RoleSessionName: this.roleSessionName },
      masterCredentials: this.machineCredentials,
    });
  }

  /** The region `bucketName` is read in: where it was last found, else the machine's region, else the default. */
  async bucketRegion(bucketName) {
    if (this.bucketRegions.has(bucketName)) return this.bucketRegions.get(bucketName);
    if (this.machineRegion === undefined) {
      try {
        this.machineRegion = await new S3Client({}).config.region();
      } catch {
        this.machineRegion = null;
      }
    }
    return this.machineRegion || DEFAULT_REGION;
  }

  /** Reads the machine's credentials again when next asked, after a refusal. */
  forgetCredentials() {
    this.machineCredentials = null;
    this.machineRegion = undefined;
    this.identityClients.clear();
  }
}

/** The reader reading from `endpoint`, its bucket in the path. */
function s3SchemaReaderAt(roleSessionName, endpoint) {
  const reader = new S3SchemaReader(roleSessionName);
  reader.endpointOverride = endpoint;
  return reader;
}

/** An `s3://<bucket>/<key>` URL: where its object is, and the role it is read as; else throws why it is none. */
function parsedS3Object(url) {
  const link = url.slice(S3_PREFIX.length);
  const queryStart = link.indexOf('?');
  const objectPath = queryStart === -1 ? link : link.slice(0, queryStart);
  const keyStart = objectPath.indexOf('/');
  const bucketName = keyStart === -1 ? '' : objectPath.slice(0, keyStart);
  const objectKey = keyStart === -1 ? '' : objectPath.slice(keyStart + 1);
  if (bucketName === '' || objectKey === '') throw new Error(`an S3 URL is ${codeSpan('s3://<bucket>/<key>')}`);
  let assumedRole = null;
  for (const parameter of queryStart === -1 ? [] : link.slice(queryStart + 1).split('&')) {
    const equalsAt = parameter.indexOf('=');
    const parameterName = equalsAt === -1 ? parameter : parameter.slice(0, equalsAt);
    const parameterValue = equalsAt === -1 ? '' : parameter.slice(equalsAt + 1);
    if (equalsAt === -1 || parameterName !== ASSUME_ROLE_PARAMETER || parameterValue === '') {
      throw new Error(`${codeSpan(parameterName)} is not a parameter of an ${codeSpan('s3')} URL; `
        + `it takes ${codeSpan('assume_role=<role ARN>')}`);
    }
    assumedRole = parameterValue;
  }
  return { bucketName, objectKey, assumedRole };
}

async function sentAnswer(objectOutput) {
  const copyVersion = { etag: objectOutput.ETag === undefined ? null : objectOutput.ETag };
  try {
    const schemaText = UTF8_TEXT.decode(await objectOutput.Body.transformToByteArray());
    return answerOf({ outcomeKind: 'textSent', schemaText, copyVersion });
  } catch (error) {
    return failedAnswer(error.message);
  }
}

/** The status S3 answered a read with. */
function statusOf(serviceError) {
  return serviceError.$metadata.httpStatusCode;
}

/** The code S3 named an error by, empty where it named none. */
function codeOf(serviceError) {
  return typeof serviceError.Code === 'string' ? serviceError.Code : '';
}

/** The region a refused or redirected read names its bucket in, `null` where it names none. */
function redirectedRegion(readError) {
  if (!(readError instanceof S3ServiceException)) return null;
  const redirected = REGION_REDIRECT_CODES.includes(codeOf(readError))
    || REGION_REDIRECT_STATUSES.includes(statusOf(readError));
  const headers = readError.$response && readError.$response.headers ? readError.$response.headers : {};
  const bucketRegion = headers[BUCKET_REGION_HEADER];
  return redirected && typeof bucketRegion === 'string' ? bucketRegion : null;
}

/** What an error comes to. */
function errorAnswer(readError, s3Object, identity) {
  if (!(readError instanceof S3ServiceException)) {
    const said = `${readError.name}: ${readError.message}`;
    return credentialsAnswer(said, readError.name, identity) || failedAnswer(said);
  }
  const status = statusOf(readError);
  const code = codeOf(readError);
  if (status === STATUS_NOT_MODIFIED) return answerOf({ outcomeKind: 'copyConfirmed' });
  if (code === 'NoSuchKey') {
    return missingAnswer(`no key ${codeSpan(s3Object.objectKey)} in bucket ${codeSpan(s3Object.bucketName)}`);
  }
  if (code === 'NoSuchBucket') return missingAnswer(`no bucket ${codeSpan(s3Object.bucketName)}`);
  const reason = refusalReason(code, typeof readError.message === 'string' ? readError.message : '');
  if (CREDENTIALS_REFUSED_CODES.includes(code)) return refused(identity, reason, CREDENTIALS_FIX);
  if (code === 'AccessDenied' || status === STATUS_FORBIDDEN) {
    let renewalFix = null;
    if (identity.identityKind === 'machine') {
      renewalFix = 'use a profile allowed `s3:GetObject` on it, such as `AWS_PROFILE=<profile>`';
    } else if (identity.identityKind === 'assumedRole') {
      renewalFix = `allow ${identity.assumedRole} \`s3:GetObject\` on it`;
    }
    return refused(identity, reason, renewalFix);
  }
  return failedAnswer(code === '' ? `S3 answered ${status}` : reason);
}

/**
 * The refusal an error short of S3 comes to: credentials the chain does not give,
 * or a role the machine is not allowed to assume; `null` for any other.
 */
function credentialsAnswer(said, errorName, identity) {
  if (identity.identityKind === 'assumedRole' && (said.includes('AccessDenied') || said.includes('not authorized'))) {
    return refused(
      MACHINE_IDENTITY,
      refusalReason('AccessDenied', said),
      `use a profile allowed to assume ${identity.assumedRole}, such as \`AWS_PROFILE=<profile>\``,
    );
  }
  if (!CREDENTIALS_PROVIDER_ERRORS.includes(errorName)) return null;
  return refused(MACHINE_IDENTITY, "the machine's AWS credentials are absent or expired", CREDENTIALS_FIX);
}

function refused(identity, refusalReasonText, renewalFix) {
  let credentialOrigin = null;
  if (identity.identityKind === 'machine') credentialOrigin = MACHINE_CREDENTIALS_ORIGIN;
  if (identity.identityKind === 'assumedRole') credentialOrigin = `role ${identity.assumedRole}`;
  return refusedAnswer([{ refusedBy: AWS_REFUSER, credentialOrigin, refusalReason: refusalReasonText, renewalFix }]);
}

/**
 * An S3 error cut to its code and the identity it names, such as
 * `AccessDenied for arn:aws:iam::<account>:user/<name>`.
 */
function refusalReason(code, message) {
  const userAt = message.indexOf('User: ');
  const namedWords = userAt === -1 ? [] : message.slice(userAt + 'User: '.length).split(/\p{White_Space}+/u);
  const namedIdentity = namedWords.find((namedWord) => namedWord !== '') || null;
  if (code === '') return message;
  return namedIdentity === null ? code : `${code} for ${namedIdentity}`;
}

module.exports = {
  S3SchemaReader,
  s3SchemaReaderAt,
  parsedS3Object,
  errorAnswer,
  redirectedRegion,
  credentialsAnswer,
  refusalReason,
};
