import assert from 'node:assert/strict'
import test from 'node:test'
import { decodeSMTPMessage } from './fake-smtp.mjs'

test('decodeSMTPMessage decodes the base64 verification body without exposing headers', () => {
  const actionURL = 'https://router.example.test/verify-email?token=synthetic-token'
  const body = `<p><a href="${actionURL}">Verify email</a></p>`
  const encoded = Buffer.from(body).toString('base64')
  const message = decodeSMTPMessage([
    'From: sender@example.test',
    'To: user@example.test',
    'Subject: Verify account',
    'Content-Type: text/html; charset=UTF-8',
    'Content-Transfer-Encoding: base64',
    '',
    encoded.slice(0, 20),
    encoded.slice(20)
  ].join('\r\n'))

  assert.equal(message.subject, 'Verify account')
  assert.equal(message.body, body)
})
