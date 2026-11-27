'use strict';

/*
 * An HTTP read for a schema URL reader: the answer to a GET, within the time a
 * request's answer is due, a redirect followed without the `Authorization`
 * header, and the headers asking whether a kept copy is current.
 */

const REQUEST_TIMEOUT_MILLISECONDS = 30 * 1000;
const STATUS_OK = 200;
const REDIRECT_STATUSES = [301, 302, 303, 307, 308];
const MAXIMUM_REDIRECTS = 10;
const UTF8_TEXT = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });

/**
 * A promise of a server's answer to a GET of `url` with `headers`, due by
 * `signal`: `{ statusCode, copyVersion, replyText }`, the text a `200`'s; it
 * rejects with why there is none.
 */
async function httpGet(url, headers, signal) {
  const requestLimit = AbortSignal.timeout(REQUEST_TIMEOUT_MILLISECONDS);
  const requestSignal = signal ? AbortSignal.any([signal, requestLimit]) : requestLimit;
  try {
    const response = await responseWithoutLoginOnRedirect(url, headers, requestSignal);
    const copyVersion = {
      etag: response.headers.get('etag'),
      last_modified: response.headers.get('last-modified'),
    };
    if (response.status !== STATUS_OK) {
      await response.body?.cancel();
      return { statusCode: response.status, copyVersion, replyText: '' };
    }
    const replyText = UTF8_TEXT.decode(await response.arrayBuffer());
    return { statusCode: response.status, copyVersion, replyText };
  } catch (error) {
    throw new Error(failureReasonOf(error));
  }
}

/** The answer to `url`, each redirect followed without the `Authorization` header. */
async function responseWithoutLoginOnRedirect(url, headers, signal) {
  let currentUrl = url;
  let currentHeaders = headers;
  for (let redirectCount = 0; ; redirectCount += 1) {
    const response = await fetch(currentUrl, { headers: currentHeaders, redirect: 'manual', signal });
    const location = response.headers.get('location');
    if (!REDIRECT_STATUSES.includes(response.status) || location === null || redirectCount === MAXIMUM_REDIRECTS) {
      return response;
    }
    await response.body?.cancel();
    currentUrl = new URL(location, currentUrl).href;
    const { Authorization: _droppedAuthorization, ...withoutLogin } = currentHeaders;
    currentHeaders = withoutLogin;
  }
}

/** Why a request got no answer, with the cause the request names. */
function failureReasonOf(error) {
  return error.cause && error.cause.message ? `${error.message}: ${error.cause.message}` : error.message;
}

/**
 * The headers asking a server whether the kept copy is current: `If-None-Match`
 * with its `etag`, else `If-Modified-Since` with its `last_modified`.
 */
function httpConditionalHeaders(keptCopyVersion) {
  const versionField = (fieldName) => {
    const fieldValue = keptCopyVersion !== null && typeof keptCopyVersion === 'object'
      ? keptCopyVersion[fieldName]
      : undefined;
    return typeof fieldValue === 'string' ? fieldValue : null;
  };
  const etag = versionField('etag');
  if (etag !== null) return { 'If-None-Match': etag };
  const lastModified = versionField('last_modified');
  return lastModified === null ? {} : { 'If-Modified-Since': lastModified };
}

module.exports = { httpGet, httpConditionalHeaders };
