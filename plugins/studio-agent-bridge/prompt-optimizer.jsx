import { useEffect, useRef, useState } from 'react'
import './prompt-optimizer.css'

export const optimizerLocales = {
  zh: {
    optimize: '优化提示词', optimizing: '优化中…', failed: '优化失败，已保留当前草稿。',
    invalid: '请输入不超过 8000 字符的提示词。', unavailable: '当前会话没有可用的模型。',
    changed: '优化期间草稿发生了变化，已保留当前草稿。',
  },
  en: {
    optimize: 'Optimize prompt', optimizing: 'Optimizing…', failed: 'Optimization failed. Your draft was kept.',
    invalid: 'Enter a prompt of at most 8,000 characters.', unavailable: 'No model is available for this session.',
    changed: 'The draft changed during optimization. Your current draft was kept.',
  },
}

export function PromptOptimizer({ sessionId, useInput, inputActions, rpc, t }) {
  const draft = useInput(state => state.draft)
  const current = useRef(draft)
  current.current = draft
  const outstanding = useRef(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => () => outstanding.current?.abort(), [])

  async function optimize() {
    if (outstanding.current || !draft.trim()) return
    if (draft.trim().length > 8000) {
      setError(t('invalid'))
      return
    }
    const controller = new AbortController()
    outstanding.current = controller
    setBusy(true)
    setError('')
    try {
      const result = await rpc('optimize-prompt', { sessionId, text: draft }, controller.signal)
      if (controller.signal.aborted) return
      if (!result.ok || typeof result.value?.prompt !== 'string' || !result.value.prompt.trim()) {
        const code = result.error?.code
        setError(t(code === 'horizon-studio/invalid_draft' ? 'invalid'
          : code === 'horizon-studio/model_unavailable' || code === 'horizon-studio/session_unavailable' ? 'unavailable' : 'failed'))
      } else if (current.current === draft) {
        inputActions.setDraft(result.value.prompt)
      } else {
        setError(t('changed'))
      }
    } catch {
      if (!controller.signal.aborted) setError(t('failed'))
    } finally {
      outstanding.current = null
      if (!controller.signal.aborted) setBusy(false)
    }
  }
  const label = t(busy ? 'optimizing' : 'optimize')
  return <div className="studio-prompt-opt">
    <button type="button" className="studio-prompt-opt-button" aria-label={label} title={label}
      aria-busy={busy} disabled={busy || !draft.trim()} onClick={() => void optimize()}>
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
        <path d="M10 3l1.8 5.2L17 10l-5.2 1.8L10 17l-1.8-5.2L3 10l5.2-1.8L10 3zm9 11v6m-3-3h6"/>
      </svg>
    </button>
    {error && <span role="status" className="studio-prompt-opt-error">{error}</span>}
  </div>
}
