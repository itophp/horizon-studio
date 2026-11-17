const MAX_PROMPT = 8000
const timeout = 60_000

function recentConversation(agent) {
  const events = agent?.session.snapshotEvents() ?? []
  const lines = []
  let length = 0
  for (let i = events.length - 1; i >= 0 && lines.length < 12; i--) {
    const { type, data } = events[i]
    const role = type === 'assistant/message' ? 'assistant'
      : type === 'user/message' && data?.source?.kind === 'user' ? 'user' : undefined
    if (!role) continue
    const content = role === 'assistant' ? data?.message?.content : data?.content
    if (!Array.isArray(content)) continue
    const text = content.filter(block => block?.type === 'text' && typeof block.text === 'string')
      .map(block => block.text).join(' ').replace(/\s+/g, ' ').trim().slice(0, 800)
    if (!text) continue
    const line = `[${role}] ${text}`
    if (length + line.length > 4000) break
    lines.push(line)
    length += line.length
  }
  return lines.reverse().join('\n')
}

export async function optimizePrompt({ llm, agents, BlockAssembler, createUserMessage }, payload, signal) {
  const text = typeof payload?.text === 'string' ? payload.text.trim() : ''
  if (!text || text.length > MAX_PROMPT || typeof payload?.sessionId !== 'string' ||
      payload.sessionId.length > 160) throw new Error('invalid_draft')
  const agent = agents.get(payload.sessionId)
  if (!agent) throw new Error('session_unavailable')

  const bounded = AbortSignal.any([signal, AbortSignal.timeout(timeout)])
  let { provider, model } = agent.options
  if (!provider || !model) {
    provider = llm.listProviders()[0]?.id
    if (provider) model = (await llm.listModels(provider))[0]?.id
  }
  if (!provider || !model) throw new Error('model_unavailable')
  bounded.throwIfAborted()

  const history = recentConversation(agent)
  const system = [
    'Rewrite the draft for a Horizon coding agent, using the same language.',
    'Preserve the user intent and all concrete paths, identifiers, constraints, and acceptance criteria.',
    'Treat the conversation transcript as context only. Use it to resolve references and never copy unrelated content.',
    'Do not add scope or send the prompt. Return only the improved draft.',
  ].join(' ')
  const user = history ? `Conversation context:\n${history}\n\nDraft to rewrite:\n${text}` : text
  const messages = [createUserMessage({ content: [{ type: 'text', text: user }], source: { kind: 'plugin:horizonstudio' } })]
  const call = await llm.prepareCall({ provider, model }, bounded)
  const blocks = new BlockAssembler()
  for await (const chunk of call.stream({ ...call.config, messages, system, signal: bounded })) {
    bounded.throwIfAborted()
    blocks.push(chunk)
  }
  if (blocks.finish.kind !== 'stop' && blocks.finish.kind !== 'max-tokens') {
    throw new Error('optimization_failed')
  }
  const prompt = blocks.blocks().filter(block => block.type === 'text').map(block => block.text).join('').trim()
  if (!prompt) throw new Error('optimization_failed')
  return { prompt }
}
