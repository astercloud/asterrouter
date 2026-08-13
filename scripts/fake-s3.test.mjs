import assert from 'node:assert/strict'
import test from 'node:test'
import { decodeS3Payload, s3ObjectPath, validateS3Authorization } from './fake-s3.mjs'

test('decodeS3Payload preserves ordinary request bodies', () => {
  const payload = Buffer.from('synthetic-object')
  assert.deepEqual(decodeS3Payload(payload, ''), payload)
})

test('decodeS3Payload removes aws-chunked framing and trailers', () => {
  const payload = Buffer.from([
    '9;chunk-signature=first',
    'synthetic',
    '7;chunk-signature=second',
    '-object',
    '0;chunk-signature=last',
    'x-amz-checksum-crc32:AAAAAA==',
    '',
    ''
  ].join('\r\n'))
  assert.equal(decodeS3Payload(payload, 'aws-chunked').toString('utf8'), 'synthetic-object')
})

test('s3ObjectPath excludes AWS operation query parameters from object keys', () => {
  assert.equal(
    s3ObjectPath('/bucket/prefix/archive.tar.gz?x-id=PutObject'),
    '/bucket/prefix/archive.tar.gz'
  )
})

test('validateS3Authorization accepts the synthetic SigV4 contract', () => {
  const result = validateS3Authorization({
    authorization: 'AWS4-HMAC-SHA256 Credential=e2e-access-key/20260812/auto/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    host: '127.0.0.1:29003',
    'x-amz-content-sha256': 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    'x-amz-date': '20260812T120000Z'
  })
  assert.deepEqual(result, { valid: true, accessKey: 'e2e-access-key', errors: [] })
})

test('validateS3Authorization rejects unknown access keys and unsigned required headers', () => {
  const result = validateS3Authorization({
    authorization: 'AWS4-HMAC-SHA256 Credential=production-key/20260812/auto/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    host: '127.0.0.1:29003',
    'x-amz-content-sha256': 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    'x-amz-date': '20260812T120000Z'
  })
  assert.equal(result.valid, false)
  assert.deepEqual(result.errors.sort(), ['access_key', 'signed:x-amz-content-sha256'])
})
