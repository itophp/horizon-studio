import { useEffect, useRef } from 'react'
import { PromptOptimizer, optimizerLocales } from './prompt-optimizer.jsx'

// Bridge between the HorizonStudio shell (parent frame) and this DSH surface.
// The shell owns tasks; DSH owns Sessions. The shell asks for a Session to be
// opened/created and the bridge drives DSH's own session page — the shell never
// re-implements the conversation view.
export const name = 'horizonstudio-agent-bridge'
export const inject = ['sessions', 'uiWorkspace', 'workspaces', 'slots', 'locale', 'connection', 'conversation', 'layout']

const preset = 'horizon-development'
const PROTOCOL = 'horizonstudio/agent'
const studioOrigin = process.env.HORIZONSTUDIO_WEB_ORIGIN

// The Studio shell renders the session list outside DSH, so it asks this bridge
// for the runtime facts DSH already owns: running, the completion reminder and
// live Agent errors. DSH remains the only owner of these facts.
function createStatusReporter(ctx, protocol, origin) {
  const errors = new Set()
  let last = ''
  const collect = () => {
    const state = ctx.sessions.list.getSnapshot()
    const statuses = ctx.get('uiSession')?.sessionStatus?.getSnapshot?.()
    const rows = Object.values(state.byId ?? {}).map(summary => {
      const ui = statuses?.get(summary.id)
      return {
        sessionId: summary.id,
        running: Boolean(ui?.running ?? summary.running),
        completed: Boolean(ui?.completionUnread),
        error: errors.has(summary.id),
      }
    })
    const live = new Set(rows.map(row => row.sessionId))
    for (const id of errors) if (!live.has(id)) errors.delete(id)
    return rows
  }
  const publish = () => {
    if (window.parent === window) return
    const rows = collect()
    const key = JSON.stringify(rows)
    if (key === last) return
    last = key
    window.parent.postMessage({ protocol, event: 'session-status', statuses: rows }, origin)
  }
  return {
    collect,
    publish,
    clear(sessionId) {
      if (errors.delete(sessionId)) publish()
    },
    fail(sessionId) {
      errors.add(sessionId)
      publish()
    },
  }
}

// The official settings shell owns opening, closing and editing its sections.
function EmbeddedSettingsLauncher({ settingsOpen, openSettings }) {
  const opened = useRef(false)

  useEffect(() => {
    if (settingsOpen) {
      opened.current = true
      window.parent.postMessage({ protocol: PROTOCOL, event: 'settings-opened' }, studioOrigin)
    } else if (opened.current) {
      window.parent.postMessage({ protocol: PROTOCOL, event: 'settings-closed' }, studioOrigin)
    } else {
      openSettings()
    }
  }, [settingsOpen, openSettings])

  return null
}

export function apply(ctx) {
  if (window.parent !== window && window.location.hash === '#horizonstudio-mcp') {
    const parent = window.parent
    const send = payload => parent.postMessage({ protocol: PROTOCOL, ...payload }, studioOrigin)
    const announce = () => send({ event: 'mcp-pair-ready' })
    const receive = event => {
      if (event.origin !== studioOrigin || event.source !== parent) return
      const data = event.data
      if (data?.protocol !== PROTOCOL) return
      if (data.op === 'mcp-pair-probe') return announce()
      if (typeof data.requestId !== 'string') return
      if (data.op === 'mcp-pair-check') {
        const respond = (ok, connected) => send({ event: 'mcp-pair-result', requestId: data.requestId, ok, connected })
        void ctx.connection.rpc.call('/horizon-studio-agent', 'check-mcp', data.credential).then(
          result => respond(result.ok, result.ok && result.value?.connected === true),
          () => respond(false, false),
        )
        return
      }
      if (data.op !== 'mcp-pair') return
      const respond = ok => send({ event: 'mcp-pair-result', requestId: data.requestId, ok })
      void ctx.connection.rpc.call('/horizon-studio-agent', 'pair-mcp', data.credential).then(
        result => respond(result.ok && result.value?.saved === true),
        () => respond(false),
      )
    }
    ctx.effect(() => {
      window.addEventListener('message', receive)
      announce()
      return () => window.removeEventListener('message', receive)
    }, 'Studio MCP pairing')
    return
  }

  if (window.parent !== window && window.location.hash === '#horizonstudio-settings') {
    // Shadow the sidebar launcher in this frame alone. Settings and its
    // authorization are still provided by the pinned DSH profile.
    ctx.slots.inject('settings.launcher', () => ctx.slots.register({
      name: 'settings.launcher', priority: -1,
    }, EmbeddedSettingsLauncher))
    const style = document.createElement('style')
    style.textContent = `
      html, body { background: transparent !important; }
      #root { opacity: 0; pointer-events: none; }
    `
    document.head.append(style)
    ctx.effect(() => () => style.remove(), 'Studio settings: hide workspace behind the official modal')
    return
  }

  const reply = (source, requestId, payload) => {
    source?.postMessage({ protocol: PROTOCOL, requestId, ...payload }, studioOrigin)
  }
  const statuses = createStatusReporter(ctx, PROTOCOL, studioOrigin)

  ctx.effect(() => ctx.locale.register('horizonstudio.promptOptimizer', optimizerLocales), 'Studio prompt optimizer locale')
  const slot = 'conversation.input.right'
  ctx.slots.inject(slot, () => ctx.slots.register({
    name: slot, id: 'horizonstudio-prompt-optimizer', locale: 'horizonstudio.promptOptimizer',
    inject: () => ({ rpc: (method, payload, signal) => ctx.connection.rpc.call('/horizon-studio-agent', method, payload, signal) }),
  }, PromptOptimizer))

  const catalogsReady = () => ctx.sessions.list.getSnapshot().phase === 'ready' &&
    ctx.workspaces.list.getSnapshot().phase === 'ready'
  const announceReady = () => {
    if (!catalogsReady()) return
    window.parent.postMessage({ protocol: PROTOCOL, event: 'ready' }, studioOrigin)
    statuses.publish()
  }

  async function open(sessionId, signal, prompt) {
    const session = ctx.sessions.list.getSnapshot().byId?.[sessionId]
    if (session?.agentPreset && session.agentPreset !== preset) throw new Error('This session does not use the Horizon development preset')
    await ctx.sessions.using(sessionId, { source: 'controllerOperation', signal }, ({ binding }) => {
      signal.throwIfAborted()
      if (typeof prompt === 'string' && prompt.trim() !== '') {
        const result = ctx.conversation.input.requestDraftInitialization(binding, { prompt })
        if (result === 'blocked') throw new Error('The task draft could not be prepared')
      }
      // The main view retains this binding before the temporary reference is released.
      ctx.uiWorkspace.openSession(sessionId)
    })
  }

  async function create(projectRoot, prompt, signal) {
    if (typeof projectRoot !== 'string' || projectRoot.length === 0) throw new Error('The project workspace is required')
    const items = ctx.workspaces.list.getSnapshot().items
    const workspace = items.find(row => row.path === projectRoot) ?? await ctx.workspaces.create({ path: projectRoot })
    signal.throwIfAborted()
    const sessionId = await ctx.sessions.create({ agentPreset: preset, workspaceId: workspace.workspaceId })
    await open(sessionId, signal, prompt)
    return sessionId
  }

  const onMessage = event => {
    if (event.origin !== studioOrigin || event.source !== window.parent) return
    const data = event.data
    if (!data || data.protocol !== PROTOCOL) return
    const { requestId, op } = data
    if (op === 'probe') return announceReady()
    if (op === 'ping') return reply(event.source, requestId, { ok: true })
    if (op === 'statuses') return reply(event.source, requestId, { ok: true, statuses: statuses.collect() })
    if (op === 'open' && typeof data.sessionId === 'string') {
      return void open(data.sessionId, ctx.layout.beginNavigation()).then(
        () => {
          statuses.clear(data.sessionId)
          statuses.publish()
          reply(event.source, requestId, { ok: true, sessionId: data.sessionId })
        },
        error => reply(event.source, requestId, { ok: false, message: String(error?.message ?? error) }),
      )
    }
    if (op === 'create') {
      return void create(data.projectRoot, data.prompt, ctx.layout.beginNavigation()).then(
        sessionId => {
          statuses.publish()
          reply(event.source, requestId, { ok: true, sessionId })
        },
        error => reply(event.source, requestId, { ok: false, message: String(error?.message ?? error) }),
      )
    }
    reply(event.source, requestId, { ok: false, message: `unknown op ${String(op)}` })
  }
  window.addEventListener('message', onMessage)
  ctx.effect(() => () => window.removeEventListener('message', onMessage), 'agent bridge: message channel')
  ctx.effect(() => {
    const dispose = ctx.sessions.list.subscribe(statuses.publish)
    return () => dispose()
  }, 'agent bridge: session catalog statuses')
  ctx.inject(['uiSession'], (scope) => {
    scope.effect(() => scope.uiSession.sessionStatus.subscribe(statuses.publish), 'agent bridge: session completion statuses')
  })
  ctx.inject(['remote'], (scope) => {
    try {
      scope.effect(() => {
        const offs = [
          scope.remote.$on('api-session/error', sessionId => statuses.fail(sessionId)),
          scope.remote.$on('api-session/status', (sessionId, running) => {
            if (running) statuses.clear(sessionId)
          }),
        ]
        return () => {
          for (const off of offs) off?.()
        }
      }, 'agent bridge: live agent errors')
    } catch {
      // Forwarded events are optional; without them the catalog still reports
      // running and completion, and only live Agent errors are unavailable.
    }
  })
  ctx.effect(() => {
    let announced = false
    const onCatalogChange = () => {
      if (announced || !catalogsReady()) return
      announced = true
      announceReady()
    }
    const disposeSessions = ctx.sessions.list.subscribe(onCatalogChange)
    const disposeWorkspaces = ctx.workspaces.list.subscribe(onCatalogChange)
    onCatalogChange()
    return () => {
      disposeSessions()
      disposeWorkspaces()
    }
  }, 'agent bridge: wait for DSH catalogs')
}
