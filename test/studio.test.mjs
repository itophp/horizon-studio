import { readFile } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { fileURLToPath } from 'node:url'
import assert from 'node:assert/strict'
import test from 'node:test'
import { createStudio } from '../plugins/studio-host/service.mjs'

const exec = promisify(execFile)

test('bundled CLI matches its manifest', async () => {
  const path = new URL('../resources/horizon-cli/', import.meta.url)
  const source = JSON.parse(await readFile(new URL('source.json', path), 'utf8'))
  const binary = await readFile(new URL('horizon-cli', path))
  assert.equal(createHash('sha256').update(binary).digest('hex'), source.sha256)
})

test('bundled CLI lists the offline definition catalogue', async () => {
  const cli = fileURLToPath(new URL('../resources/horizon-cli/horizon-cli', import.meta.url))
  const { stdout } = await exec(cli, ['capability', '--json'])
  const catalogue = JSON.parse(stdout)
  assert.ok(catalogue.items.some(row => row.id === 'orchestration.workflow.run'))
  const schema = JSON.parse((await exec(cli, ['capability', 'schema', 'orchestration.workflow.run@1.0.0'])).stdout)
  assert.equal(schema.entry.version, '1.0.0')
})

test('studio proxy forwards every RPC to its studio.* capability', async t => {
  const calls = []
  const invoke = async (api, path, context, body) => {
    calls.push({ api, path, context, body })
    if (path === '/api/auth/login') {
      return { status: 200, cookies: [{ name: 'baseline_session', value: 'session-test' }],
        body: { profile: { csrfToken: 'csrf-test' } } }
    }
    if (body.capability === 'studio.project.list') {
      return { status: 200, cookies: [], body: { data: { projects: [{ id: 1, root: '/tmp/project' }] } } }
    }
    if (body.capability === 'studio.summary') {
      return { status: 200, cookies: [], body: { data: { requirements: [1], items: [2], runs: [3] } } }
    }
    return { status: 200, cookies: [], body: { data: { echo: body } } }
  }
  const studio = createStudio({
    api: 'http://127.0.0.1:18080',
    workspaceRegistry: {
      resolveByPath: () => { throw new Error('should not resolve for capability calls') },
      create: () => { throw new Error('should not create for capability calls') },
    },
    invoke,
  })
  // Capability calls are refused before login and the login screen owns the
  // credentials exchange.
  await assert.rejects(studio('projects'), error => error.code === 'unauthenticated')
  assert.deepEqual(await studio('auth.status'), { authenticated: false, username: null })
  assert.deepEqual(await studio('auth.login', { username: 'service', password: 'secret' }), { username: 'service' })
  assert.deepEqual(await studio('auth.status'), { authenticated: true, username: 'service' })
  const projects = await studio('projects')
  assert.deepEqual(projects, [{ id: 1, root: '/tmp/project' }])
  const summary = await studio('summary', { project: 1 })
  assert.deepEqual(summary, { requirement: [1], item: [2], session: [], run: [3] })
  const link = await studio('link', { path: '/tmp/project', api: 'http://127.0.0.1:8080', web: 'http://127.0.0.1:5173' })
  assert.equal(link.echo.capability, 'studio.project.link')
  assert.equal(link.echo.idempotencyKey.length, 36)
  // Login happened once and every capability call carries the session.
  assert.equal(calls.filter(call => call.path === '/api/auth/login').length, 1)
  for (const call of calls.filter(call => call.path !== '/api/auth/login')) {
    assert.equal(call.context.cookie, 'baseline_session=session-test')
    assert.equal(call.context.csrf, 'csrf-test')
    assert.equal(call.context.origin, 'http://127.0.0.1:18080')
  }
  await assert.rejects(studio('unknown.op'), /未知的 Studio 操作/)
})

test('session context resolves the project root and local workspace', async () => {
  const studio = createStudio({
    api: 'http://127.0.0.1:18080',
    workspaceRegistry: {
      resolveByPath: root => (root === '/Users/demo/project' ? { id: 'workspace-two' } : null),
      create: () => { throw new Error('should reuse') },
    },
    invoke: async (api, path) => path === '/api/auth/login'
      ? { status: 200, cookies: [{ name: 's', value: '1' }], body: { profile: { csrfToken: 'c' } } }
      : { status: 200, cookies: [], body: { data: { id: 1, root: '/Users/demo/project' } } },
  })
  await studio('auth.login', { username: 'demo', password: 'secret' })
  const context = await studio('session.context', { project: 1, kind: 'item', id: 9 })
  assert.equal(context.workspaceId, 'workspace-two')
  assert.equal(context.subjectId, 9)
  assert.match(context.prompt, /project/)
  await studio('session.link', { sessionId: 'session-x' }).then(value => assert.equal(value.id, 'session-x'))
})
