import assert from 'node:assert/strict'
import test from 'node:test'
import { optimizePrompt } from '../plugins/studio-agent-bridge/prompt-optimizer.mjs'

test('optimizes only the draft with the session model and bounded recent human context', async () => {
  const events = [
    { type: 'user/message', data: { source: { kind: 'user' }, content: [{ type: 'text', text: 'Use the existing contract.' }] } },
    { type: 'user/message', data: { source: { kind: 'plugin:other' }, content: [{ type: 'text', text: 'Do not export this seeded context.' }] } },
  ]
  class Assembler {
    finish = { kind: 'stop' }
    push(text) { this.text = text }
    blocks() { return [{ type: 'text', text: this.text }] }
  }
  let requested
  const deps = {
    llm: {
      async prepareCall(config) {
        assert.deepEqual(config, { provider: 'session-provider', model: 'session-model' })
        return { config: {}, async *stream(request) { requested = request; yield 'Improved draft.' } }
      },
    },
    agents: { get: () => ({ options: { provider: 'session-provider', model: 'session-model' }, session: { snapshotEvents: () => events } }) },
    BlockAssembler: Assembler,
    createUserMessage: message => message,
  }
  const signal = new AbortController().signal
  assert.deepEqual(await optimizePrompt(deps, { sessionId: 'task-1', text: 'Implement it.' }, signal), { prompt: 'Improved draft.' })
  const context = requested.messages[0].content[0].text
  assert.match(context, /Use the existing contract\./)
  assert.match(context, /Implement it\./)
  assert.doesNotMatch(context, /seeded context/)
  assert.ok(!requested.tools)
  await assert.rejects(optimizePrompt(deps, { sessionId: 'task-1', text: 'a'.repeat(8001) }, signal), /invalid_draft/)
  await assert.rejects(optimizePrompt({ ...deps, agents: { get: () => undefined } }, { sessionId: 'unknown', text: 'Draft' }, signal), /session_unavailable/)
})
