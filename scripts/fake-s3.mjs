import { readFileSync } from 'node:fs'
import { createServer as createHTTPServer } from 'node:http'
import { createServer as createHTTPSServer } from 'node:https'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const s3Port = Number(process.env.ASTER_E2E_S3_PORT || 29003)
const apiPort = Number(process.env.ASTER_E2E_S3_API_PORT || 29004)
const keyPath = process.env.ASTER_E2E_S3_KEY
const certPath = process.env.ASTER_E2E_S3_CERT

async function readBody(request) {
  const chunks = []
  for await (const chunk of request) chunks.push(chunk)
  return Buffer.concat(chunks)
}

function chunkLineEnd(payload, offset) {
  const index = payload.indexOf('\r\n', offset, 'ascii')
  if (index < 0) throw new Error('invalid aws-chunked payload')
  return index
}

export function decodeS3Payload(payload, contentEncoding = '') {
  if (!String(contentEncoding).toLowerCase().split(',').map((value) => value.trim()).includes('aws-chunked')) {
    return Buffer.from(payload)
  }
  const chunks = []
  let offset = 0
  while (offset < payload.length) {
    const lineEnd = chunkLineEnd(payload, offset)
    const header = payload.subarray(offset, lineEnd).toString('ascii')
    const sizeText = header.split(';', 1)[0]
    if (!/^[0-9a-f]+$/i.test(sizeText)) throw new Error('invalid aws-chunked size')
    const size = Number.parseInt(sizeText, 16)
    offset = lineEnd + 2
    if (size === 0) break
    if (!Number.isSafeInteger(size) || size < 0 || offset + size + 2 > payload.length) {
      throw new Error('invalid aws-chunked body length')
    }
    chunks.push(payload.subarray(offset, offset + size))
    offset += size
    if (payload.subarray(offset, offset + 2).toString('ascii') !== '\r\n') {
      throw new Error('invalid aws-chunked delimiter')
    }
    offset += 2
  }
  return Buffer.concat(chunks)
}

export function s3ObjectPath(requestURL) {
  return new URL(requestURL || '/', 'https://127.0.0.1').pathname
}

function requestHeader(headers, name) {
  if (typeof headers?.get === 'function') return String(headers.get(name) || '')
  return String(headers?.[name.toLowerCase()] || headers?.[name] || '')
}

export function escapeXMLText(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;')
}

function allowedAccessKey(value) {
  const configured = String(process.env.ASTER_E2E_S3_ALLOWED_ACCESS_KEYS || '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
  if (configured.length > 0) return configured.includes(value)
  return value === 'e2e-access-key'
    || /^access-[A-Za-z0-9._:-]+$/.test(value)
    || /^synthetic-access-[A-Za-z0-9._:-]+$/.test(value)
}

export function validateS3Authorization(headers) {
  const authorization = requestHeader(headers, 'authorization')
  const match = authorization.match(/^AWS4-HMAC-SHA256\s+Credential=([^/,\s]+)\/(\d{8})\/([^/,\s]+)\/s3\/aws4_request,\s*SignedHeaders=([^,\s]+),\s*Signature=([a-fA-F0-9]{64})$/)
  const errors = []
  if (!match) return { valid: false, accessKey: '', errors: ['authorization'] }
  const [, accessKey, scopeDate, region, signedHeaderText] = match
  const signedHeaders = signedHeaderText.split(';')
  const amzDate = requestHeader(headers, 'x-amz-date')
  const contentSHA = requestHeader(headers, 'x-amz-content-sha256')
  if (!allowedAccessKey(accessKey)) errors.push('access_key')
  if (!region) errors.push('credential_region')
  if (!/^\d{8}T\d{6}Z$/.test(amzDate) || !amzDate.startsWith(scopeDate)) errors.push('x-amz-date')
  if (!contentSHA || (!/^[a-fA-F0-9]{64}$/.test(contentSHA) && !/^STREAMING-[A-Z0-9-]+$/.test(contentSHA) && contentSHA !== 'UNSIGNED-PAYLOAD')) {
    errors.push('x-amz-content-sha256')
  }
  for (const name of ['host', 'x-amz-content-sha256', 'x-amz-date']) {
    if (!signedHeaders.includes(name)) errors.push(`signed:${name}`)
  }
  if (requestHeader(headers, 'x-amz-security-token') && !signedHeaders.includes('x-amz-security-token')) {
    errors.push('signed:x-amz-security-token')
  }
  return { valid: errors.length === 0, accessKey, errors }
}

function json(response, status, body) {
  response.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' })
  response.end(JSON.stringify(body))
}

function s3Error(response, status, code, message) {
  response.writeHead(status, { 'Content-Type': 'application/xml', 'x-amz-request-id': `fake-s3-${Date.now()}` })
  response.end(`<Error><Code>${escapeXMLText(code)}</Code><Message>${escapeXMLText(message)}</Message></Error>`)
}

function start() {
  if (!keyPath || !certPath) throw new Error('ASTER_E2E_S3_KEY and ASTER_E2E_S3_CERT are required')
  let mode = 'normal'
  const requests = []
  const objects = new Map()
  const tls = { key: readFileSync(keyPath), cert: readFileSync(certPath) }

  const s3Server = createHTTPSServer(tls, async (request, response) => {
    const received = await readBody(request)
    let body = received
    let decodeError = ''
    try {
      body = decodeS3Payload(received, request.headers['content-encoding'])
    } catch (error) {
      decodeError = error instanceof Error ? error.message : 'invalid encoded body'
    }
    const record = {
      id: requests.length + 1,
      method: request.method || '',
      path: request.url || '',
      content_type: String(request.headers['content-type'] || ''),
      content_length: Number(request.headers['x-amz-decoded-content-length'] || body.length),
      authorization_present: Boolean(request.headers.authorization),
      sigv4_valid: false,
      sigv4_errors: [],
      access_key_hint: '',
      body_base64: body.toString('base64'),
      decode_error: decodeError,
      mode,
      outcome: 'rejected'
    }
    requests.push(record)

    if (decodeError) {
      s3Error(response, 400, 'InvalidRequest', decodeError)
      return
    }
    const authorization = validateS3Authorization(request.headers)
    record.sigv4_valid = authorization.valid
    record.sigv4_errors = authorization.errors
    record.access_key_hint = authorization.accessKey ? `${authorization.accessKey.slice(0, 4)}...` : ''
    if (!authorization.valid) {
      s3Error(response, 403, 'SignatureDoesNotMatch', 'invalid synthetic SigV4 contract')
      return
    }
    if (request.method === 'HEAD' && /^\/[^/]+\/?$/.test(request.url || '')) {
      record.outcome = 'bucket-found'
      response.writeHead(200, { 'x-amz-request-id': `fake-s3-${record.id}` })
      response.end()
      return
    }
    if (request.method === 'GET' && (request.url || '').includes('list-type=2')) {
      const url = new URL(request.url || '/', 'https://127.0.0.1')
      const bucketPath = url.pathname.replace(/\/$/, '')
      const prefix = url.searchParams.get('prefix') || ''
      const contents = [...objects]
        .filter(([path]) => path.startsWith(`${bucketPath}/${prefix}`))
        .map(([path, value]) => {
          const key = path.slice(bucketPath.length + 1)
          return `<Contents><Key>${escapeXMLText(key)}</Key><LastModified>${new Date().toISOString()}</LastModified><ETag>&quot;synthetic-etag&quot;</ETag><Size>${value.body.length}</Size><StorageClass>STANDARD</StorageClass></Contents>`
        }).join('')
      record.outcome = 'listed'
      response.writeHead(200, { 'Content-Type': 'application/xml', 'x-amz-request-id': `fake-s3-${record.id}` })
      response.end(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult><Name>${escapeXMLText(bucketPath.slice(1))}</Name><Prefix>${escapeXMLText(prefix)}</Prefix><KeyCount>${objects.size}</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated>${contents}</ListBucketResult>`)
      return
    }
    if (request.method === 'GET') {
      const object = objects.get(s3ObjectPath(request.url))
      if (!object) {
        s3Error(response, 404, 'NoSuchKey', 'not found')
        return
      }
      record.outcome = 'downloaded'
      response.writeHead(200, { 'Content-Type': object.contentType || 'application/octet-stream', 'Content-Length': object.body.length, ETag: '"synthetic-etag"', 'x-amz-request-id': `fake-s3-${record.id}` })
      response.end(object.body)
      return
    }
    if (request.method === 'PUT') {
      if (mode === 'fail-put') {
        s3Error(response, 503, 'ServiceUnavailable', 'synthetic object storage failure')
        return
      }
      objects.set(s3ObjectPath(request.url), { body: Buffer.from(body), contentType: record.content_type })
      record.outcome = 'stored'
      response.writeHead(200, { ETag: '"synthetic-etag"', 'x-amz-request-id': `fake-s3-${record.id}` })
      response.end()
      return
    }
    if (request.method === 'DELETE') {
      objects.delete(s3ObjectPath(request.url))
      record.outcome = 'deleted'
      response.writeHead(204, { 'x-amz-request-id': `fake-s3-${record.id}` })
      response.end()
      return
    }
    s3Error(response, 404, 'NoSuchKey', 'not found')
  })

  const apiServer = createHTTPServer(async (request, response) => {
    const url = new URL(request.url || '/', `http://${request.headers.host || '127.0.0.1'}`)
    if (request.method === 'GET' && url.pathname === '/health') {
      json(response, 200, { status: 'ok', mode })
      return
    }
    if (request.method === 'POST' && url.pathname === '/__test/mode') {
      const payload = JSON.parse((await readBody(request)).toString('utf8') || '{}')
      mode = String(payload.mode || 'normal')
      json(response, 200, { mode })
      return
    }
    if (request.method === 'GET' && url.pathname === '/__test/requests') {
      const artifactID = (url.searchParams.get('artifact_id') || '').trim()
      const filtered = artifactID ? requests.filter((record) => record.path.includes(artifactID)) : requests
      json(response, 200, { requests: filtered })
      return
    }
    if (request.method === 'GET' && url.pathname === '/__test/objects') {
      json(response, 200, {
        objects: [...objects].map(([path, value]) => ({
          path,
          content_type: value.contentType,
          body_base64: value.body.toString('base64')
        }))
      })
      return
    }
    json(response, 404, { error: 'not found' })
  })

  s3Server.listen(s3Port, '127.0.0.1', () => {
    console.log(`Fake S3: https://127.0.0.1:${s3Port}`)
  })
  apiServer.listen(apiPort, '127.0.0.1', () => {
    console.log(`Fake S3 API: http://127.0.0.1:${apiPort}`)
  })
  for (const signal of ['SIGINT', 'SIGTERM']) {
    process.on(signal, () => {
      s3Server.close()
      apiServer.close(() => process.exit(0))
    })
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) start()
