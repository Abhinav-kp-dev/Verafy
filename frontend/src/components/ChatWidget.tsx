import { useEffect, useRef, useState } from 'react'
import { api, type ChatMessage } from '../api'
import { Icon } from './ui'

const GREETING: ChatMessage = {
  role: 'assistant',
  text: "Hi! I can answer questions about your patients, verifications, manual review, or how to use Verafy. Try \"what's in manual review right now?\" or \"how much does Falcon Dent owe for tomorrow's visit?\"",
}

export function ChatWidget() {
  const [open, setOpen] = useState(false)
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [messages, setMessages] = useState<ChatMessage[]>([GREETING])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => { api.config().then((c) => setEnabled(!!c.chatEnabled)).catch(() => setEnabled(false)) }, [])
  useEffect(() => { listRef.current?.scrollTo({ top: listRef.current.scrollHeight, behavior: 'smooth' }) }, [messages, busy])

  const send = async () => {
    const text = input.trim()
    if (!text || busy) return
    setInput(''); setErr('')
    const next = [...messages, { role: 'user', text } as ChatMessage]
    setMessages(next)
    setBusy(true)
    try {
      const reply = await api.chat(next)
      setMessages([...next, { role: 'assistant', text: reply.text }])
    } catch (e) {
      setErr((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <button className="chat-fab" onClick={() => setOpen(!open)} aria-label={open ? 'Close assistant' : 'Open assistant'}>
        <Icon name={open ? 'close' : 'chat'} size={22} />
      </button>
      {open && (
        <div className="chat-panel">
          <div className="chat-head">
            <div className="chat-avatar"><Icon name="tooth" size={16} /></div>
            <div><b>Verafy Assistant</b><div className="cell-sub">{enabled === false ? 'Not configured' : 'Read-only · looks up real data'}</div></div>
            <button className="x" onClick={() => setOpen(false)} aria-label="Close">×</button>
          </div>

          {enabled === false && (
            <div className="notice warn" style={{ margin: 12 }}>
              The assistant needs a Gemini API key. Add <code>GEMINI_API_KEY</code> to <code>backend/.env</code> and restart the backend.
            </div>
          )}

          <div className="chat-body" ref={listRef}>
            {messages.map((m, i) => (
              <div key={i} className={`chat-msg ${m.role}`}>
                <div className="chat-bubble">{m.text}</div>
              </div>
            ))}
            {busy && (
              <div className="chat-msg assistant"><div className="chat-bubble chat-typing"><span /><span /><span /></div></div>
            )}
            {err && <div className="notice error" style={{ margin: '8px 4px' }}>{err}</div>}
          </div>

          <div className="chat-input-row">
            <input
              className="input" placeholder={enabled === false ? 'Assistant not configured' : 'Ask about a patient, a check, or the app…'}
              value={input} disabled={busy || enabled === false}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() } }}
            />
            <button className="btn btn-primary btn-sm" disabled={busy || !input.trim() || enabled === false} onClick={send}>
              <Icon name="send" size={15} />
            </button>
          </div>
        </div>
      )}
    </>
  )
}
