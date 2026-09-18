import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { API, api, type Batch, type RateLimitState } from './api'

interface JobChange { job_id: string; batch_id: string | null; status: string; attempt_count: number }

interface Live {
  connected: boolean
  batches: Record<string, Batch>
  rateLimits: RateLimitState[]
  /** increments on every coalesced jobs event — subscribe to refetch lists */
  tick: number
  lastChanges: JobChange[]
  maxAttempts: number
}

const Ctx = createContext<Live>({ connected: false, batches: {}, rateLimits: [], tick: 0, lastChanges: [], maxAttempts: 3 })

export function LiveProvider({ children }: { children: ReactNode }) {
  const [connected, setConnected] = useState(false)
  const [batches, setBatches] = useState<Record<string, Batch>>({})
  const [rateLimits, setRateLimits] = useState<RateLimitState[]>([])
  const [tick, setTick] = useState(0)
  const [lastChanges, setLastChanges] = useState<JobChange[]>([])
  const [maxAttempts, setMaxAttempts] = useState(3)
  const esRef = useRef<EventSource | null>(null)

  useEffect(() => { api.config().then((c) => setMaxAttempts(c.maxAttempts)).catch(() => {}) }, [])
  useEffect(() => {
    let closed = false
    const connect = () => {
      if (closed) return
      const es = new EventSource(API + '/api/events')
      esRef.current = es
      es.addEventListener('hello', () => setConnected(true))
      es.addEventListener('batches', (e) => {
        const list = JSON.parse((e as MessageEvent).data) as Batch[]
        setBatches((prev) => {
          const next = { ...prev }
          list.forEach((b) => { next[b.id] = b })
          return next
        })
      })
      es.addEventListener('jobs', (e) => {
        setLastChanges(JSON.parse((e as MessageEvent).data) as JobChange[])
        setTick((t) => t + 1)
      })
      es.addEventListener('ratelimit', (e) => setRateLimits(JSON.parse((e as MessageEvent).data) as RateLimitState[]))
      es.onerror = () => {
        setConnected(false)
        es.close()
        setTimeout(connect, 2000)
      }
    }
    connect()
    return () => { closed = true; esRef.current?.close() }
  }, [])

  const value = useMemo(() => ({ connected, batches, rateLimits, tick, lastChanges, maxAttempts }), [connected, batches, rateLimits, tick, lastChanges, maxAttempts])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export const useLive = () => useContext(Ctx)

/** Re-run `fn` when live job changes arrive (debounced) and on mount. */
export function useLiveRefresh(fn: () => void, deps: unknown[] = [], debounceMs = 400) {
  const { tick } = useLive()
  const fnRef = useRef(fn)
  fnRef.current = fn
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => { fnRef.current() }, deps)
  useEffect(() => {
    if (tick === 0) return
    const t = setTimeout(() => fnRef.current(), debounceMs)
    return () => clearTimeout(t)
  }, [tick, debounceMs])
}
