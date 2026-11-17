import { randomUUID } from 'node:crypto'
import { request as httpRequest } from 'node:http'

// Thin Horizon proxy: every Studio RPC forwards to one studio.* capability on
// the Horizon backend through the Capability Gateway. No local database and no
// process execution live here any more — the Horizon base owns entities,
// storage, the project CLI calls and the remote gateway sessions.

const mapping = {
  projects: { capability: 'studio.project.list', version: '1.0.0', pick: value => value.projects },
  link: { capability: 'studio.project.link', version: '1.0.0' },
  create: { capability: 'studio.project.create', version: '1.0.0' },
  control: { capability: 'studio.project.control', version: '1.0.0', pick: value => value.result ?? value },
  environment: { capability: 'studio.project.environment', version: '1.0.0', pick: value => value.result ?? value },
  log: { capability: 'studio.project.log', version: '1.0.0', pick: value => value.log },
  catalog: { capability: 'studio.catalog.inspect', version: '1.0.0' },
  summary: { capability: 'studio.summary', version: '1.0.0', pick: value =>
    ({ requirement: value.requirements ?? [], item: value.items ?? [], session: [], run: value.runs ?? [] }) },
  'requirement.add': { capability: 'studio.requirement.create', version: '1.0.0' },
  'requirement.archive': { capability: 'studio.requirement.archive', version: '1.0.0' },
  'item.add': { capability: 'studio.item.create', version: '1.0.0' },
  'item.bind': { capability: 'studio.item.bind', version: '1.0.0' },
  'item.transition': { capability: 'studio.item.transition', version: '1.0.0' },
  'gateway.login': { capability: 'studio.gateway.login', version: '1.0.0' },
  'gateway.logout': { capability: 'studio.gateway.logout', version: '1.0.0' },
  'gateway.run': {
    capability: 'studio.gateway.invoke', version: '1.0.0',
    transform: args => ({ project: args.project, capability: 'orchestration.workflow.get',
      version: '1.0.0', input: { runId: args.id } }),
    pick: value => value.result,
  },
  'gateway.invoke': { capability: 'studio.gateway.invoke', version: '1.0.0', pick: value => value.result },
}

export function createStudio({ api = process.env.HORIZONSTUDIO_API ?? 'http://127.0.0.1:8080', workspaceRegistry, invoke = httpInvoke }) {
  let session = null
  // HorizonStudio login: the browser sends credentials through the Studio RPC
  // channel; the proxy exchanges them for a Horizon API session held in
  // process memory only.
  const login = async (username, password) => {
    if (!username || !password) throw new Error('请输入用户名和密码')
    const response = await invoke(api, '/api/auth/login', { origin: api }, { username, password })
    const cookie = response.cookies.map(value => `${value.name}=${value.value}`).join('; ')
    const csrf = response.body?.profile?.csrfToken
    if (response.status !== 200 || !cookie || !csrf) {
      throw new Error(response.body?.error?.message ?? '用户名或密码不正确')
    }
    session = { cookie, csrf, username }
    return { username }
  }
  return async (operation, args = {}) => {
    if (operation === 'auth.status') {
      return { authenticated: !!session, username: session?.username ?? null }
    }
    if (operation === 'auth.login') return login(args.username, args.password)
    if (operation === 'auth.logout') {
      session = null
      return { ok: true }
    }
    if (operation === 'session.context') {
      // Session binding stays a DSH-side concern: the project root comes from
      // the Horizon backend, the workspace from the local registry.
      const target = await call('studio.project.get', '1.0.0', { id: args.kind === 'project' ? args.id : args.project })
      const workspace = await workspaceRegistry.resolveByPath(target.root) ?? await workspaceRegistry.create(target.root)
      return { workspaceId: workspace.id, subjectId: args.id ?? target.id, title: target.root.split('/').at(-1),
        prompt: `项目：${target.root.split('/').at(-1)}\n目标：${args.title ?? ''}` }
    }
    if (operation === 'session.link') return { id: args.sessionId }
    const route = mapping[operation]
    if (!route) throw new Error('未知的 Studio 操作')
    const value = await call(route.capability, route.version, route.transform ? route.transform(args) : args)
    return route.pick ? route.pick(value) : value
  }

  async function call(capability, version, input) {
    if (!session) {
      const error = new Error('尚未登录 HorizonStudio')
      error.code = 'unauthenticated'
      throw error
    }
    const key = randomUUID()
    const response = await invoke(api, `/api/capabilities/${encodeURIComponent(capability)}/invocations`,
      { origin: api, cookie: session.cookie, csrf: session.csrf },
      { capability, version, input: stripEmpty(input), idempotencyKey: key })
    if (response.status !== 200) {
      throw new Error(response.body?.error?.message ?? `Horizon 能力调用失败（${response.status}）`)
    }
    return response.body?.data ?? response.body ?? {}
  }
}

function stripEmpty(input) {
  const clean = {}
  for (const [key, value] of Object.entries(input ?? {})) {
    if (value !== undefined && value !== null && value !== '') clean[key] = value
  }
  return clean
}

// node:http keeps the proxy independent of any runtime-patched global fetch.
async function httpInvoke(api, path, context, body) {
  const payload = JSON.stringify(body)
  const headers = { 'content-type': 'application/json', 'content-length': Buffer.byteLength(payload) }
  if (context.origin) headers.origin = context.origin
  if (context.cookie) headers.cookie = context.cookie
  if (context.csrf) headers['x-csrf-token'] = context.csrf
  const target = new URL(path, api)
  const { status, cookies, text } = await new Promise((resolve, reject) => {
    const request = httpRequest({
      hostname: target.hostname, port: target.port || 80, path: target.pathname + target.search,
      method: 'POST', headers,
    }, response => {
      const chunks = []
      response.on('data', chunk => chunks.push(chunk))
      response.on('end', () => resolve({ status: response.statusCode,
        cookies: (response.headers['set-cookie'] ?? []).map(raw => {
          const [pair] = raw.split(';')
          const [name] = pair.split('=')
          return { name, value: pair.slice(name.length + 1) }
        }),
        text: Buffer.concat(chunks).toString() }))
    })
    request.on('error', reject)
    request.end(payload)
  })
  let parsed = null
  try { parsed = JSON.parse(text) } catch { /* empty body */ }
  return { status, cookies, body: parsed }
}
