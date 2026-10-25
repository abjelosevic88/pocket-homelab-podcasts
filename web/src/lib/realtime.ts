import { useEffect, useRef, useState } from 'react'
import { V1 } from './api'
import type { RemoteSession } from './player'

/** Live updates from the server (GET /api/v1/events, Server-Sent Events): what
 *  changed on the person's other tabs, the iPhone app, feed refreshes and the
 *  download workers. Events carry ids; pages refetch what they show. One
 *  EventSource is shared by the whole app and reconnects with backoff; while
 *  it is down, pages fall back to a slow poll (see `useLive`). */

export type DownloadState = 'none' | 'queued' | 'downloading' | 'done' | 'failed' | 'culled'
export type RealtimeEvent =
  | { type: 'hello'; server_time: number }
  | ({ type: 'now_playing' } & RemoteSession)
  | { type: 'progress'; episode_id: number; podcast_id: number; position: number; completed: boolean; client: string; updated_at: number }
  | { type: 'history'; episode_id: number }
  | { type: 'episodes'; podcast_id: number; added: number[]; updated: number[]; removed: number[] }
  | { type: 'download'; episode_id: number; podcast_id: number; state: DownloadState; error?: string }
  | { type: 'podcasts'; added: number[]; updated: number[]; removed: number[] }
  | { type: 'refresh'; podcast_id: number; running: boolean; new?: number; error?: string }
  | { type: 'queue' }
  | { type: 'saved'; episode_id: number; saved: boolean }
  | { type: 'playlists' }
  | { type: 'settings' }
  /** not from the server: the stream came back after being down, so anything may have changed */
  | { type: 'reconnected' }

type Listener = (e: RealtimeEvent) => void
const listeners = new Set<Listener>()
const statusListeners = new Set<(up: boolean) => void>()
let source: EventSource | null = null
let retry: number | undefined
let backoff = 1000
let users = 0
let up = false
let wasUp = false

function setUp(v: boolean) {
  if (up === v) return
  up = v
  statusListeners.forEach(f => f(v))
}

function emit(ev: RealtimeEvent) {
  listeners.forEach(f => { try { f(ev) } catch { /* one listener must not stop the rest */ } })
}

function connect() {
  clearTimeout(retry)
  source?.close()
  const es = new EventSource(`${V1}/events`)
  source = es
  es.onopen = () => {
    backoff = 1000
    setUp(true)
    // events sent while the stream was down are lost: have every page reload
    if (wasUp) emit({ type: 'reconnected' })
    wasUp = true
  }
  es.onmessage = m => {
    let ev: RealtimeEvent
    try { ev = JSON.parse(m.data) } catch { return }
    emit(ev)
  }
  es.addEventListener('unauthorized', () => {
    es.close(); setUp(false)
    window.dispatchEvent(new CustomEvent('pp:unauthorized'))
  })
  es.onerror = () => {
    setUp(false)
    // the browser retries a dropped stream by itself; a refused one (401, 429, 5xx) is closed
    if (es.readyState === EventSource.CLOSED && source === es) {
      retry = window.setTimeout(() => { if (users > 0) connect() }, backoff)
      backoff = Math.min(30_000, backoff * 2)
    }
  }
}

/** Keeps the stream open while at least one caller holds it (signed in). */
export function holdRealtime(): () => void {
  if (typeof EventSource === 'undefined') return () => {}
  users++
  if (users === 1) { backoff = 1000; wasUp = false; connect() }
  let released = false
  return () => {
    if (released) return
    released = true
    users--
    if (users === 0) { clearTimeout(retry); source?.close(); source = null; setUp(false) }
  }
}

export function onRealtime(f: Listener): () => void {
  listeners.add(f)
  return () => { listeners.delete(f) }
}

export function realtimeConnected() { return up }

export function useRealtimeConnected() {
  const [v, setV] = useState(up)
  useEffect(() => {
    statusListeners.add(setV)
    setV(up)
    return () => { statusListeners.delete(setV) }
  }, [])
  return v
}

/** Reloads when an event the page cares about arrives, after the stream
 *  reconnects, and — only while the stream is down — every `fallbackMs` as a
 *  slow poll (0 = no poll). A burst of events is one fetch, and fetches are at
 *  least `minGap` ms apart (the phone reports every few seconds while it plays). */
export function useLive(match: (e: RealtimeEvent) => boolean, reload: () => unknown, opts: { fallbackMs?: number; minGap?: number } = {}) {
  const { fallbackMs = 60_000, minGap = 1000 } = opts
  const m = useRef(match), r = useRef(reload), gap = useRef(minGap)
  m.current = match; r.current = reload; gap.current = minGap
  const live = useRealtimeConnected()
  useEffect(() => {
    let t: number | undefined
    let last = 0
    const off = onRealtime(ev => {
      if (ev.type !== 'reconnected' && !m.current(ev)) return
      if (t !== undefined) return // one is already on its way
      const wait = Math.max(250, last + gap.current - Date.now())
      t = window.setTimeout(() => { t = undefined; last = Date.now(); r.current() }, wait)
    })
    return () => { off(); clearTimeout(t) }
  }, [])
  useEffect(() => {
    if (live || !fallbackMs) return
    const t = window.setInterval(() => { if (document.visibilityState === 'visible') r.current() }, fallbackMs)
    return () => clearInterval(t)
  }, [live, fallbackMs])
}

/** Does an event touch one of these episodes (or their subscriptions)? */
export function touchesEpisodes(ev: RealtimeEvent, ids: Set<number>, podcastIds?: Set<number>): boolean {
  switch (ev.type) {
    case 'progress': case 'download': case 'saved': return ids.has(ev.episode_id)
    case 'history': return ev.episode_id === 0 || ids.has(ev.episode_id)
    case 'episodes':
      return [...ev.updated, ...ev.removed].some(id => ids.has(id)) || (!!podcastIds && podcastIds.has(ev.podcast_id))
    case 'podcasts': return !!podcastIds && [...ev.updated, ...ev.removed].some(id => podcastIds.has(id))
  }
  return false
}
