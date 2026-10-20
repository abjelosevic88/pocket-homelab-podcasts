import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { api, V1 } from './api'
import { useApp } from './app-context'
import { useToast } from '../components/ui'
import type { Chapter, Episode, EpisodeDetail, Segment } from './types'

/** The one audio element of the app and everything around it: what plays,
 *  where it is, progress reports, sponsor skipping, chapters, sleep timer,
 *  lock-screen controls and keyboard shortcuts. */

/** What another device (the phone, another browser) is playing right now. */
export interface RemoteSession {
  active: boolean; episode?: EpisodeDetail; position: number; duration: number; playing: boolean; rate: number
  device: string; client: string; updated_at: number; server_time: number
}

/** This tab's id: its own progress reports are not shown back to it as "another device". */
export const CLIENT_ID = (() => {
  try { let id = sessionStorage.getItem('pp-client'); if (!id) { id = Math.random().toString(36).slice(2, 12); sessionStorage.setItem('pp-client', id) } return id }
  catch { return Math.random().toString(36).slice(2, 12) }
})()

interface PlayerState {
  current: EpisodeDetail | null
  playing: boolean
  loading: boolean
  time: number
  duration: number
  rate: number
  sleepAt: number | null
  chapter: Chapter | null
  chapters: Chapter[]
  sponsors: Segment[]
  expanded: boolean
  play: (e: Episode | EpisodeDetail, opts?: { fromStart?: boolean }) => void
  toggle: () => void
  pause: () => void
  seek: (t: number) => void
  skip: (delta: number) => void
  setRate: (r: number) => void
  setSleep: (minutes: number | null) => void
  next: () => void
  stop: () => void
  setExpanded: (v: boolean) => void
  isCurrent: (id: number) => boolean
  remote: RemoteSession | null
  remoteVisible: boolean
  clockOffset: number
}

const Ctx = createContext<PlayerState>(null!)
export function usePlayer() { return useContext(Ctx) }

const LAST_KEY = 'pp-last-episode'
const REPORT_EVERY = 10 // seconds

export function PlayerProvider({ children }: { children: React.ReactNode }) {
  const { settings } = useApp()
  const { toast } = useToast()
  const audio = useMemo(() => { const a = new Audio(); a.preload = 'metadata'; return a }, [])
  const [current, setCurrent] = useState<EpisodeDetail | null>(null)
  const [playing, setPlaying] = useState(false)
  const [loading, setLoading] = useState(false)
  const [time, setTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [rate, setRateState] = useState(1)
  const [sleepAt, setSleepAt] = useState<number | null>(null)
  const [expanded, setExpanded] = useState(false)
  const cur = useRef<EpisodeDetail | null>(null)
  const listened = useRef(0)       // seconds since the last report
  const lastT = useRef(0)          // last timeupdate position
  const lastReport = useRef(0)     // time of the last report
  const skippedAt = useRef<Map<number, number>>(new Map()) // segment start → when we skipped it
  const sleepTimer = useRef<number | null>(null)
  const settingsRef = useRef(settings)
  useEffect(() => { settingsRef.current = settings }, [settings])

  const report = useCallback(async (opts: { completed?: boolean; force?: boolean } = {}) => {
    const e = cur.current
    if (!e) return
    const pos = Math.floor(audio.currentTime)
    const l = Math.floor(listened.current)
    if (!opts.force && !opts.completed && l === 0) return
    listened.current = 0
    lastReport.current = Date.now()
    try {
      const r = await api.post<{ position: number; completed: boolean }>(`${V1}/episodes/${e.id}/progress`, { position: pos, listened: l, duration: Math.floor(audio.duration) || e.duration, completed: opts.completed, client: CLIENT_ID })
      if (cur.current?.id === e.id) {
        cur.current = { ...cur.current, play_position: pos, completed: r.completed }
        window.dispatchEvent(new CustomEvent('pp:progress', { detail: { id: e.id, position: pos, completed: r.completed } }))
      }
    } catch { /* offline: the next report carries the seconds */ }
  }, [audio])

  const applyMediaSession = useCallback((e: EpisodeDetail | null) => {
    if (!('mediaSession' in navigator)) return
    if (!e) { navigator.mediaSession.metadata = null; return }
    const art = e.artwork_url || e.podcast_artwork
    navigator.mediaSession.metadata = new MediaMetadata({ title: e.title, artist: e.podcast_title, album: e.podcast_title, artwork: art ? [{ src: art, sizes: '512x512' }] : [] })
  }, [])

  // ---- loading an episode ----
  const play = useCallback((e: Episode | EpisodeDetail, opts: { fromStart?: boolean } = {}) => {
    if (!e.stream_url) {
      if (e.download === 'queued' || e.download === 'downloading') toast('Still downloading — it will be ready soon')
      else { api.post(`${V1}/episodes/${e.id}/download`).then(() => toast('Downloading first — press play again when it is ready')).catch(err => toast((err as Error).message, 'err')) }
      return
    }
    const same = cur.current?.id === e.id
    if (same && !opts.fromStart) { audio.play().catch(() => {}); return }
    if (cur.current) report({ force: true })
    const detail: EpisodeDetail = { ...(e as EpisodeDetail), chapters: (e as EpisodeDetail).chapters || [], sponsors: (e as EpisodeDetail).sponsors || [] }
    cur.current = detail
    setCurrent(detail)
    applyMediaSession(detail)
    localStorage.setItem(LAST_KEY, String(e.id))
    skippedAt.current.clear()
    listened.current = 0
    const start = opts.fromStart || e.completed ? 0 : e.play_position || 0
    const startAt = start > 0 ? start : (e.start_skip || 0)
    setLoading(true)
    audio.src = e.stream_url
    const r = e.playback_speed || settingsRef.current?.playback_speed || 1
    audio.playbackRate = r
    setRateState(r)
    const onMeta = () => { if (startAt > 0 && startAt < (audio.duration || Infinity) - 2) audio.currentTime = startAt; lastT.current = audio.currentTime; audio.removeEventListener('loadedmetadata', onMeta) }
    audio.addEventListener('loadedmetadata', onMeta)
    audio.play().catch(err => { setLoading(false); toast('Could not play: ' + (err as Error).message, 'err') })
    // chapters and sponsor segments come with the detail
    api.get<EpisodeDetail>(`${V1}/episodes/${e.id}`).then(d => {
      if (cur.current?.id !== e.id) return
      cur.current = { ...cur.current!, chapters: d.chapters || [], sponsors: d.sponsors || [], description: d.description }
      setCurrent(cur.current)
    }).catch(() => {})
  }, [audio, applyMediaSession, report, toast])

  const pause = useCallback(() => audio.pause(), [audio])
  const toggle = useCallback(() => { if (!cur.current) return; if (audio.paused) audio.play().catch(() => {}); else audio.pause() }, [audio])
  const seek = useCallback((t: number) => { audio.currentTime = Math.max(0, Math.min(t, audio.duration || t)); lastT.current = audio.currentTime; setTime(audio.currentTime) }, [audio])
  const skip = useCallback((d: number) => seek(audio.currentTime + d), [audio, seek])
  const setRate = useCallback((r: number) => { r = Math.round(r * 100) / 100; audio.playbackRate = r; setRateState(r) }, [audio])
  const stop = useCallback(() => { report({ force: true }); audio.pause(); audio.removeAttribute('src'); audio.load(); cur.current = null; setCurrent(null); setPlaying(false); applyMediaSession(null); localStorage.removeItem(LAST_KEY) }, [audio, report, applyMediaSession])

  const next = useCallback(async () => {
    const e = cur.current
    const st = settingsRef.current
    if (!e) return
    try {
      const nx = await api.get<EpisodeDetail | null>(`${V1}/queue/next?after=${e.id}`)
      if (nx && (st?.queue_next ?? true)) { play(nx); return }
    } catch { /* ignore */ }
    toast('Queue is empty')
  }, [play, toast])

  const setSleep = useCallback((minutes: number | null) => {
    if (sleepTimer.current) { clearTimeout(sleepTimer.current); sleepTimer.current = null }
    if (!minutes) { setSleepAt(null); return }
    const at = Date.now() + minutes * 60000
    setSleepAt(at)
    sleepTimer.current = window.setTimeout(() => { audio.pause(); setSleepAt(null); sleepTimer.current = null }, minutes * 60000)
  }, [audio])

  // ---- audio events ----
  useEffect(() => {
    const onPlay = () => { setPlaying(true); setLoading(false); lastT.current = audio.currentTime }
    const onPause = () => { setPlaying(false); report({ force: true }) }
    const onWaiting = () => setLoading(true)
    const onPlaying = () => setLoading(false)
    const onDur = () => setDuration(audio.duration || 0)
    const onEnded = async () => {
      const e = cur.current
      if (!e) return
      setPlaying(false)
      await report({ completed: true, force: true })
      api.del(`${V1}/episodes/${e.id}/queue`).catch(() => {})
      window.dispatchEvent(new CustomEvent('pp:queue'))
      if (settingsRef.current?.auto_play_next ?? true) next()
    }
    const onTime = () => {
      const e = cur.current
      const t = audio.currentTime
      setTime(t)
      if (!audio.paused) {
        const d = t - lastT.current
        if (d > 0 && d < 5) listened.current += d // seconds of content heard
      }
      lastT.current = t
      if (!e) return
      // end skip: the last N seconds are credits
      if (e.end_skip > 0 && audio.duration && t >= audio.duration - e.end_skip) { audio.pause(); audio.currentTime = audio.duration; onEnded(); return }
      // SponsorBlock
      const st = settingsRef.current
      if (st?.auto_skip_sponsors && e.sponsors?.length) {
        const cats = new Set((st.skip_categories || '').split(',').map(s => s.trim()).filter(Boolean))
        for (const s of e.sponsors) {
          if (!cats.has(s.category)) continue
          if (t >= s.start && t < s.end - 0.3) {
            const when = skippedAt.current.get(s.start)
            if (when && Date.now() - when < 8000) break // the person seeked back on purpose
            skippedAt.current.set(s.start, Date.now())
            audio.currentTime = s.end
            toast(`Skipped ${labelFor(s.category)} (${Math.round(s.end - s.start)} s)`)
            break
          }
        }
      }
      if (Date.now() - lastReport.current > REPORT_EVERY * 1000) report()
    }
    audio.addEventListener('play', onPlay); audio.addEventListener('pause', onPause); audio.addEventListener('waiting', onWaiting); audio.addEventListener('playing', onPlaying)
    audio.addEventListener('durationchange', onDur); audio.addEventListener('ended', onEnded); audio.addEventListener('timeupdate', onTime)
    audio.addEventListener('error', () => { setLoading(false); if (cur.current) toast('Playback error — the file may be missing or the host refused', 'err') })
    return () => {
      audio.removeEventListener('play', onPlay); audio.removeEventListener('pause', onPause); audio.removeEventListener('waiting', onWaiting); audio.removeEventListener('playing', onPlaying)
      audio.removeEventListener('durationchange', onDur); audio.removeEventListener('ended', onEnded); audio.removeEventListener('timeupdate', onTime)
    }
  }, [audio, report, next, toast])

  // ---- lock screen / keyboard ----
  useEffect(() => {
    if (!('mediaSession' in navigator)) return
    const ms = navigator.mediaSession
    const back = settings?.skip_back || 10, fwd = settings?.skip_forward || 30
    try {
      ms.setActionHandler('play', () => audio.play())
      ms.setActionHandler('pause', () => audio.pause())
      ms.setActionHandler('seekbackward', d => skip(-(d.seekOffset || back)))
      ms.setActionHandler('seekforward', d => skip(d.seekOffset || fwd))
      ms.setActionHandler('seekto', d => { if (d.seekTime !== undefined) seek(d.seekTime) })
      ms.setActionHandler('nexttrack', () => next())
      ms.setActionHandler('previoustrack', () => seek(0))
    } catch { /* older browsers */ }
  }, [audio, settings?.skip_back, settings?.skip_forward, skip, seek, next])
  useEffect(() => {
    if (!('mediaSession' in navigator) || !current || !duration) return
    try { navigator.mediaSession.setPositionState({ duration, playbackRate: rate, position: Math.min(time, duration) }) } catch { /* ignore */ }
    try { navigator.mediaSession.playbackState = playing ? 'playing' : 'paused' } catch { /* ignore */ }
  }, [current, duration, time, rate, playing])
  useEffect(() => {
    const h = (ev: KeyboardEvent) => {
      const t = ev.target as HTMLElement
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return
      if (!cur.current) return
      if (ev.code === 'Space' || ev.key === 'k') { ev.preventDefault(); toggle() }
      else if (ev.key === 'ArrowRight' || ev.key === 'l') { ev.preventDefault(); skip(ev.shiftKey ? 60 : (settingsRef.current?.skip_forward || 30)) }
      else if (ev.key === 'ArrowLeft' || ev.key === 'j') { ev.preventDefault(); skip(-(ev.shiftKey ? 60 : (settingsRef.current?.skip_back || 10))) }
      else if (ev.key === 'n' && ev.shiftKey) next()
      else if (ev.key === '>' ) setRate(Math.min(3, (audio.playbackRate || 1) + 0.1))
      else if (ev.key === '<' ) setRate(Math.max(0.5, (audio.playbackRate || 1) - 0.1))
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [audio, toggle, skip, next, setRate])

  // report before the tab goes away
  useEffect(() => {
    const h = () => { if (cur.current && listened.current > 0) { const e = cur.current; const body = JSON.stringify({ position: Math.floor(audio.currentTime), listened: Math.floor(listened.current), duration: Math.floor(audio.duration) || e.duration, client: CLIENT_ID }); navigator.sendBeacon?.(`${V1}/episodes/${e.id}/progress`, new Blob([body], { type: 'application/json' })) } }
    window.addEventListener('pagehide', h)
    document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'hidden') report({ force: true }) })
    return () => window.removeEventListener('pagehide', h)
  }, [audio, report])

  // restore the last episode (paused) after a reload
  useEffect(() => {
    const id = localStorage.getItem(LAST_KEY)
    if (!id || cur.current) return
    api.get<EpisodeDetail>(`${V1}/episodes/${id}`).then(d => {
      if (cur.current || d.completed) return
      cur.current = d; setCurrent(d); setDuration(d.duration); setTime(d.play_position || 0)
      setRateState(d.playback_speed || settingsRef.current?.playback_speed || 1)
      applyMediaSession(d)
    }).catch(() => localStorage.removeItem(LAST_KEY))
  }, [applyMediaSession])

  // what other devices play, pushed live by the server
  const [remote, setRemote] = useState<RemoteSession | null>(null)
  const [clockOffset, setClockOffset] = useState(0)
  useEffect(() => {
    let es: EventSource | null = null, retry: number | undefined, closed = false
    const connect = () => {
      es = new EventSource(`${V1}/now-playing/events`)
      es.onmessage = ev => { try { const d = JSON.parse(ev.data) as RemoteSession; setClockOffset(d.server_time - Date.now()); setRemote(d.active ? d : null) } catch { /* ignore */ } }
      es.onerror = () => { es?.close(); if (!closed) retry = window.setTimeout(connect, 5000) }
    }
    connect()
    return () => { closed = true; es?.close(); if (retry) clearTimeout(retry) }
  }, [])
  const fromElsewhere = !!remote && remote.client !== CLIENT_ID
  // another device wins while it plays and this tab does not; a paused one shows only when this tab has nothing loaded
  const remoteVisible = fromElsewhere && !playing && (remote!.playing || !current)

  const chapter = useMemo(() => {
    const ch = current?.chapters || []
    let c: Chapter | null = null
    for (const x of ch) if (x.start <= time) c = x
    return c
  }, [current, time])

  const value: PlayerState = {
    current, playing, loading, time, duration: duration || current?.duration || 0, rate, sleepAt, chapter, chapters: current?.chapters || [], sponsors: current?.sponsors || [], expanded,
    play, toggle, pause, seek, skip, setRate, setSleep, next, stop, setExpanded, isCurrent: id => cur.current?.id === id,
    remote: fromElsewhere ? remote : null, remoteVisible, clockOffset,
  }
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function labelFor(cat: string): string {
  return ({ sponsor: 'sponsor', selfpromo: 'self-promotion', interaction: 'reminder', intro: 'intro', outro: 'outro', preview: 'preview', music_offtopic: 'non-music', filler: 'filler' } as Record<string, string>)[cat] || cat
}
