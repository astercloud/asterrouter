import { readFileSync } from 'node:fs'
import { createServer as createHTTPServer } from 'node:http'
import { createServer as createTCPServer } from 'node:net'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createSecureContext, TLSSocket } from 'node:tls'

const smtpPort = Number(process.env.ASTER_E2E_SMTP_PORT || 29001)
const apiPort = Number(process.env.ASTER_E2E_MAIL_API_PORT || 29002)
const keyPath = process.env.ASTER_E2E_SMTP_KEY
const certPath = process.env.ASTER_E2E_SMTP_CERT

export function decodeSMTPMessage(raw) {
  const separator = raw.match(/\r?\n\r?\n/)
  const headerText = separator ? raw.slice(0, separator.index) : raw
  const encodedBody = separator ? raw.slice((separator.index || 0) + separator[0].length) : ''
  const headers = new Map()
  let current = ''
  for (const line of headerText.split(/\r?\n/)) {
    if (/^[ \t]/.test(line) && current) {
      headers.set(current, `${headers.get(current) || ''} ${line.trim()}`)
      continue
    }
    const colon = line.indexOf(':')
    if (colon < 1) continue
    current = line.slice(0, colon).trim().toLowerCase()
    headers.set(current, line.slice(colon + 1).trim())
  }
  const body = (headers.get('content-transfer-encoding') || '').toLowerCase() === 'base64'
    ? Buffer.from(encodedBody.replace(/\s/g, ''), 'base64').toString('utf8')
    : encodedBody
  return {
    subject: headers.get('subject') || '',
    body
  }
}

function mailbox(command) {
  const match = command.match(/<([^>]+)>/)
  return (match?.[1] || '').trim().toLowerCase()
}

function start() {
  if (!keyPath || !certPath) throw new Error('ASTER_E2E_SMTP_KEY and ASTER_E2E_SMTP_CERT are required')
  const secureContext = createSecureContext({
    key: readFileSync(keyPath),
    cert: readFileSync(certPath)
  })
  const messages = []

  const smtpServer = createTCPServer((socket) => {
    let connection = socket
    let buffer = ''
    let tlsActive = false
    let dataMode = false
    let dataLines = []
    let sender = ''
    let recipients = []

    const reply = (value) => connection.write(`${value}\r\n`)
    const install = (nextConnection) => {
      connection = nextConnection
      buffer = ''
      connection.on('data', (chunk) => {
        buffer += chunk.toString('utf8')
        let lineEnd
        while ((lineEnd = buffer.indexOf('\n')) >= 0) {
          const line = buffer.slice(0, lineEnd).replace(/\r$/, '')
          buffer = buffer.slice(lineEnd + 1)
          handleLine(line)
        }
      })
      connection.on('error', () => {})
    }
    const handleLine = (line) => {
      if (dataMode) {
        if (line === '.') {
          const raw = dataLines.map((item) => item.startsWith('..') ? item.slice(1) : item).join('\r\n')
          const decoded = decodeSMTPMessage(raw)
          for (const recipient of recipients) {
            messages.push({
              id: messages.length + 1,
              from: sender,
              to: recipient,
              subject: decoded.subject,
              body: decoded.body,
              received_at: new Date().toISOString()
            })
          }
          dataMode = false
          dataLines = []
          reply('250 2.0.0 message accepted')
          return
        }
        dataLines.push(line)
        return
      }

      const command = line.trim()
      const verb = command.split(/\s+/, 1)[0].toUpperCase()
      if (verb === 'EHLO' || verb === 'HELO') {
        if (tlsActive) reply('250-fake-smtp\r\n250 8BITMIME')
        else reply('250-fake-smtp\r\n250 STARTTLS')
        return
      }
      if (verb === 'STARTTLS' && !tlsActive) {
        reply('220 2.0.0 ready to start TLS')
        connection.removeAllListeners('data')
        tlsActive = true
        sender = ''
        recipients = []
        install(new TLSSocket(connection, { isServer: true, secureContext }))
        return
      }
      if (!tlsActive) {
        reply('530 5.7.0 STARTTLS required')
        return
      }
      if (verb === 'MAIL') {
        sender = mailbox(command)
        recipients = []
        reply('250 2.1.0 sender accepted')
        return
      }
      if (verb === 'RCPT') {
        const recipient = mailbox(command)
        if (recipient) recipients.push(recipient)
        reply('250 2.1.5 recipient accepted')
        return
      }
      if (verb === 'DATA' && sender && recipients.length > 0) {
        dataMode = true
        dataLines = []
        reply('354 end data with <CR><LF>.<CR><LF>')
        return
      }
      if (verb === 'RSET') {
        sender = ''
        recipients = []
        dataMode = false
        dataLines = []
        reply('250 2.0.0 reset')
        return
      }
      if (verb === 'QUIT') {
        reply('221 2.0.0 bye')
        connection.end()
        return
      }
      reply('502 5.5.2 command not implemented')
    }

    install(socket)
    reply('220 fake-smtp ESMTP ready')
  })

  const apiServer = createHTTPServer((request, response) => {
    const url = new URL(request.url || '/', `http://${request.headers.host || '127.0.0.1'}`)
    response.setHeader('Content-Type', 'application/json')
    response.setHeader('Cache-Control', 'no-store')
    if (request.method === 'GET' && url.pathname === '/health') {
      response.writeHead(200)
      response.end(JSON.stringify({ status: 'ok' }))
      return
    }
    if (request.method === 'GET' && url.pathname === '/__test/messages') {
      const recipient = (url.searchParams.get('recipient') || '').trim().toLowerCase()
      const filtered = recipient ? messages.filter((message) => message.to === recipient) : messages
      response.writeHead(200)
      response.end(JSON.stringify({ messages: filtered }))
      return
    }
    response.writeHead(404)
    response.end(JSON.stringify({ error: 'not found' }))
  })

  smtpServer.listen(smtpPort, '127.0.0.1', () => {
    console.log(`Fake SMTP: 127.0.0.1:${smtpPort}`)
  })
  apiServer.listen(apiPort, '127.0.0.1', () => {
    console.log(`Fake SMTP API: http://127.0.0.1:${apiPort}`)
  })
  for (const signal of ['SIGINT', 'SIGTERM']) {
    process.on(signal, () => {
      smtpServer.close()
      apiServer.close(() => process.exit(0))
    })
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) start()
