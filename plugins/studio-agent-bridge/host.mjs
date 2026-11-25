import { BlockAssembler, createUserMessage } from '@deepseek-ai/dsh-llm'
import { optimizePrompt } from './prompt-optimizer.mjs'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'

export const inject = ['connection', 'webServer', 'agents', 'llm', 'credentials']

const route = '/horizon-studio-agent'
const optimize = 'optimize-prompt'
const pair = 'pair-mcp'
const check = 'check-mcp'
const tokenRef = 'HORIZON_STUDIO_MCP_TOKEN'
const knownErrors = new Set(['invalid_draft', 'session_unavailable', 'model_unavailable'])

export function apply(ctx) {
  ctx.effect(() => ctx.webServer.register({
    kind: 'prefix',
    path: route,
    async handler(req, res) {
      const { rejection } = ctx.connection.admit(req)
      if (rejection) return res.writeHead(rejection).end()
      const path = new URL(req.url, 'http://localhost').pathname
      const endpoint = path.slice(route.length + 1)
      if (req.method !== 'POST' || path !== `${route}/${endpoint}` || (endpoint !== optimize && endpoint !== pair && endpoint !== check)) {
        return res.writeHead(404).end()
      }
      if (endpoint !== optimize && req.headers.origin !== `http://${req.headers.host}`) {
        return res.writeHead(403).end()
      }
      if (req.headers['content-type']?.split(';')[0]?.trim() !== 'application/json') {
        return res.writeHead(415).end()
      }
      let envelope
      try {
        const chunks = []
        let size = 0
        for await (const chunk of req.iterator({ destroyOnReturn: false })) {
          size += chunk.length
          if (size > (endpoint === optimize ? 64 * 1024 : 1024)) {
            req.resume()
            return res.writeHead(413).end()
          }
          chunks.push(chunk)
        }
        envelope = JSON.parse(Buffer.concat(chunks))
      } catch {
        return res.writeHead(400).end()
      }
      if (envelope?.type !== 'client-request' || envelope.method !== endpoint ||
          typeof envelope.rpcId !== 'string' || envelope.rpcId.length > 200) {
        return res.writeHead(400).end()
      }
      const controller = new AbortController()
      res.on('close', () => controller.abort())
      let result
      try {
        const value = endpoint === pair
          ? await pairStudioMcp(ctx.credentials, envelope.payload, controller.signal)
          : endpoint === check
          ? await checkStudioMcp(ctx.credentials, envelope.payload)
          : await optimizePrompt({
            llm: ctx.llm, agents: ctx.agents, BlockAssembler, createUserMessage,
          }, envelope.payload, controller.signal)
        result = { ok: true, value }
      } catch (error) {
        const code = endpoint !== optimize ? 'pairing_failed' : knownErrors.has(error?.message) ? error.message : 'optimization_failed'
        result = { ok: false, error: { code: `horizon-studio/${code}`, message: code, details: {} } }
      }
      if (res.destroyed) return
      res.writeHead(200, { 'content-type': 'application/json', 'cache-control': 'no-store' })
      res.end(JSON.stringify({ type: 'server-response', rpcId: envelope.rpcId, result }))
    },
  }), 'Studio prompt optimizer')
}


async function verifyStudioMcp(token, { accountId, tenantId, identityBindingId } = {}) {
  if (typeof token !== 'string' || !/^bmcp_[a-f0-9]{64}$/.test(token) ||
      !Number.isSafeInteger(accountId) || accountId <= 0 ||
      typeof tenantId !== 'string' || !tenantId || typeof identityBindingId !== 'string') {
    throw new Error('Invalid Studio MCP credential')
  }
  const root = process.env.HORIZONSTUDIO_ROOT
  const fromStudio = file => import(pathToFileURL(join(root, 'scripts', file)).href)
  const [{ backendURL }, { diagnose }] = await Promise.all([
    fromStudio('studio-mcp.mjs'),
    fromStudio('mcp-business-helper.mjs'),
  ])
  await diagnose((await backendURL()).href, token, { accountId, tenantId, identityBindingId }, 'studio_session_context')
}

async function checkStudioMcp(credentials, identity) {
  const current = await credentials.resolve(tokenRef)
  if (current) {
    try {
      await verifyStudioMcp(current.value, identity)
      return { connected: true }
    } catch {
      // An expired or mismatched credential needs to be replaced.
    }
  }
  return { connected: false }
}

async function pairStudioMcp(credentials, payload, signal) {
  await verifyStudioMcp(payload?.token, payload)
  signal.throwIfAborted()
  await credentials.set(tokenRef, payload.token)
  return { saved: true }
}
