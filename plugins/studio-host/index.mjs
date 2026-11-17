import { createStudio } from './service.mjs'

export const name = 'horizon-studio-host'
export const inject = ['connection', 'webServer', 'workspaceRegistry']

const channel = '/horizon-studio'
const bodyLimit = 1024 * 1024

export function apply(ctx) {
  const studio = createStudio({ workspaceRegistry: ctx.workspaceRegistry })
  ctx.effect(() => ctx.webServer.register({
    kind: 'prefix',
    path: channel,
    handler: createHandler(ctx.connection, studio),
  }), 'HorizonStudio RPC')
}

export function createHandler(connection, studio) {
  return async (req, res) => {
    const { rejection } = connection.admit(req)
    if (rejection) return res.writeHead(rejection).end()
    const prefix = channel + '/'
    const path = new URL(req.url, 'http://localhost').pathname
    const method = path.slice(prefix.length)
    if (req.method !== 'POST' || !path.startsWith(prefix) || !/^[\w.-]+$/u.test(method)) {
      return res.writeHead(404).end()
    }
    if (req.headers['content-type']?.split(';', 1)[0].trim() !== 'application/json') {
      return res.writeHead(415).end()
    }
    let envelope
    try {
      const chunks = []
      let size = 0
      for await (const chunk of req.iterator({ destroyOnReturn: false })) {
        size += chunk.length
        if (size > bodyLimit) {
          req.resume()
          return res.writeHead(413).end()
        }
        chunks.push(chunk)
      }
      envelope = JSON.parse(Buffer.concat(chunks))
    } catch {
      return res.writeHead(400).end()
    }
    if (envelope?.type !== 'client-request' || envelope.method !== method ||
        typeof envelope.rpcId !== 'string' || envelope.rpcId.length > 200) {
      return res.writeHead(400).end()
    }
    let result
    try {
      const value = await studio(method, envelope.payload)
      result = { ok: true, value: value ?? null }
    } catch (error) {
      result = { ok: false, error: { code: `horizon-studio/${error.code ?? 'operation'}`, message: error.message, details: {} } }
    }
    res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'no-store' })
    res.end(JSON.stringify({ type: 'server-response', rpcId: envelope.rpcId, result }))
  }
}
