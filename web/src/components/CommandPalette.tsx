import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { usePlayer } from '../lib/player'
import { useDebounce, useToast } from './ui'
import { Artwork } from './Artwork'
import { Icon } from './icons'
import { searchYouTube, type YTHit } from '../lib/youtube'
import { SourcePeek } from '../pages/Add'
import type { Episode, Podcast, SearchResult } from '../lib/types'
import { compact, fmtDate, fmtDur } from '../lib/format'

/** ⌘K / Ctrl+K: one box that searches your library, the podcast directories
 *  and YouTube, and jumps anywhere in the app. */

type Scope = 'all' | 'library' | 'podcasts' | 'youtube'
const SCOPES: { value: Scope; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'library', label: 'Library' },
  { value: 'podcasts', label: 'Podcasts' },
  { value: 'youtube', label: 'YouTube' },
]

interface Action { label: string; run: () => void; primary?: boolean; disabled?: boolean }
interface Item {
  key: string
  section: string
  title: string
  sub?: string
  thumb?: string
  round?: boolean
  wide?: boolean
  icon?: string
  badge?: { text: string; cls: string }
  open: () => void
  actions?: Action[]
}

export const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)
export const openPalette = () => window.dispatchEvent(new CustomEvent('pp:palette'))

const fold = (s: string) => s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase()

export function CommandPalette() {
  const navigate = useNavigate()
  const { toggleTheme, settings } = useApp()
  const player = usePlayer()
  const { toast } = useToast()
  const [open, setOpen] = useState(false)
  const [q, setQ] = useState('')
  const [scope, setScope] = useState<Scope>('all')
  const [active, setActive] = useState(0)
  const [pods, setPods] = useState<Podcast[]>([])
  const [eps, setEps] = useState<Episode[] | null>(null)
  const [dir, setDir] = useState<SearchResult[] | null>(null)
  const [yt, setYt] = useState<YTHit[] | null>(null)
  const [busy, setBusy] = useState<Record<string, boolean>>({})
  const [done, setDone] = useState<Record<string, number>>({})
  const [peek, setPeek] = useState<{ key: string; url: string; title: string; subscribed?: number } | null>(null)
  const input = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const dq = useDebounce(q.trim(), 350)
  const seq = useRef(0)

  // open with ⌘K / Ctrl+K anywhere, or "/" when not typing; the sidebar button sends pp:palette
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') { e.preventDefault(); setOpen(o => !o); return }
      const t = e.target as HTMLElement
      if (e.key === '/' && !open && !(t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable))) { e.preventDefault(); setOpen(true) }
    }
    const ev = () => setOpen(true)
    window.addEventListener('keydown', key)
    window.addEventListener('pp:palette', ev)
    return () => { window.removeEventListener('keydown', key); window.removeEventListener('pp:palette', ev) }
  }, [open])
  useEffect(() => {
    if (!open) return
    setActive(0)
    setTimeout(() => { input.current?.focus(); input.current?.select() }, 0)
    api.get<Podcast[]>(`${V1}/podcasts?sort=recent`).then(setPods).catch(() => {})
    document.body.style.overflow = 'hidden'
    return () => { document.body.style.overflow = '' }
  }, [open])

  // remote searches; each section fills in as its answer arrives
  useEffect(() => {
    const n = ++seq.current
    setEps(null); setDir(null); setYt(null); setActive(0)
    if (dq.length < 2 || !open) return
    if (scope === 'all' || scope === 'library')
      api.get<{ episodes: Episode[] }>(`${V1}/episodes?q=${encodeURIComponent(dq)}&limit=${scope === 'library' ? 15 : 5}`).then(r => { if (n === seq.current) setEps(r.episodes) }).catch(() => n === seq.current && setEps([]))
    if (scope === 'all' || scope === 'podcasts')
      api.get<{ results: SearchResult[] }>(`${V1}/search?q=${encodeURIComponent(dq)}&source=${settings?.search_source || 'all'}`).then(r => { if (n === seq.current) setDir(r.results.slice(0, scope === 'podcasts' ? 15 : 5)) }).catch(() => n === seq.current && setDir([]))
    if ((scope === 'all' || scope === 'youtube') && settings?.yt_dlp)
      searchYouTube(dq, 'all', scope === 'all').then(r => { if (n === seq.current) setYt(scope === 'all' ? r.slice(0, 6) : r) }).catch(() => n === seq.current && setYt([]))
  }, [dq, scope, open, settings?.search_source, settings?.yt_dlp])

  const close = useCallback(() => { setOpen(false) }, [])
  const go = useCallback((to: string) => { close(); navigate(to) }, [close, navigate])
  const act = useCallback(async (key: string, fn: () => Promise<{ id: number }>, ok: string) => {
    setBusy(b => ({ ...b, [key]: true }))
    try { const r = await fn(); setDone(d => ({ ...d, [key]: r.id })); toast(ok) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(b => ({ ...b, [key]: false })) }
  }, [toast])

  const pages: Item[] = useMemo(() => {
    const p = (title: string, to: string, icon: string, sub?: string): Item => ({ key: 'go:' + to, section: 'Go to', title, icon, sub, open: () => go(to) })
    return [
      p('Home', '/home', 'home'), p('Podcasts', '/podcasts', 'podcast'), p('Episodes', '/episodes', 'list'), p('Up next (queue)', '/queue', 'queue'),
      p('Playlists', '/playlists', 'list-music'), p('Add a podcast', '/add', 'plus', 'Search directories or paste a feed'), p('Add from YouTube', '/add?tab=youtube', 'youtube', 'Channels, videos, playlists'),
      p('Saved', '/saved', 'heart'), p('History', '/history', 'history'), p('Downloads', '/downloads', 'download'), p('Stats', '/stats', 'bar-chart'), p('Settings', '/settings', 'settings'),
      p('Public feeds', '/settings/feeds', 'rss', 'Feed URLs for Apple Podcasts & co.'),
      { key: 'do:refresh', section: 'Go to', title: 'Refresh all feeds', icon: 'refresh', open: () => { close(); api.post(`${V1}/refresh`).then(() => toast('Refreshing all feeds in the background')).catch(e => toast((e as Error).message, 'err')) } },
      { key: 'do:theme', section: 'Go to', title: 'Toggle dark mode', icon: 'moon', open: () => { toggleTheme(); close() } },
      { key: 'do:keys', section: 'Go to', title: 'Keyboard shortcuts', icon: 'keyboard', open: () => { close(); window.dispatchEvent(new CustomEvent('pp:shortcuts')) } },
    ]
  }, [go, close, toast, toggleTheme])

  const items: Item[] = useMemo(() => {
    const fq = fold(dq)
    const out: Item[] = []
    if (scope === 'all' || scope === 'library') {
      const pg = dq ? pages.filter(p => fold(p.title + ' ' + (p.sub || '')).includes(fq)).slice(0, 4) : (scope === 'all' ? pages : [])
      out.push(...pg)
      const mine = (dq ? pods.filter(p => fold(p.title + ' ' + p.author).includes(fq)) : (scope === 'library' ? pods : pods.slice(0, 0))).slice(0, dq ? (scope === 'library' ? 10 : 4) : 12)
      out.push(...mine.map((p): Item => ({
        key: 'pod:' + p.id, section: 'Your podcasts', title: p.title, sub: [p.author, p.unplayed_count ? `${p.unplayed_count} unplayed` : ''].filter(Boolean).join(' · '),
        thumb: p.artwork, badge: p.kind === 'youtube' ? { text: 'YouTube', cls: 'video' } : undefined, open: () => go(`/podcasts/${p.id}`),
      })))
      out.push(...(eps || []).map((e): Item => ({
        key: 'ep:' + e.id, section: 'Your episodes', title: e.title, sub: [e.podcast_title, fmtDate(e.pub_date), e.duration ? fmtDur(e.duration) : ''].filter(Boolean).join(' · '),
        thumb: e.artwork_url || e.podcast_artwork, open: () => go(`/episodes/${e.id}`),
        actions: [{ label: player.isCurrent(e.id) && player.playing ? 'Pause' : 'Play', run: () => { player.play(e); close() } }],
      })))
    }
    if (scope === 'all' || scope === 'podcasts') {
      out.push(...(dir || []).map((r): Item => {
        const key = 'dir:' + r.feed_url, sub = done[key] ?? r.subscribed_id
        return {
          key, section: 'Podcasts', title: r.title, sub: [r.author, r.episodes ? `${r.episodes} episodes` : ''].filter(Boolean).join(' · '), thumb: r.image,
          open: () => sub ? go(`/podcasts/${sub}`) : go(`/add?tab=url&url=${encodeURIComponent(r.feed_url)}`),
          actions: sub ? [{ label: '✓ Following', run: () => go(`/podcasts/${sub}`) }]
            : [{ label: 'Episodes', run: () => go(`/add?tab=url&url=${encodeURIComponent(r.feed_url)}`) }, { label: busy[key] ? 'Adding…' : '+ Follow', primary: true, disabled: busy[key], run: () => act(key, () => api.post<Podcast>(`${V1}/podcasts`, { url: r.feed_url, title: r.title }), `Following ${r.title}`) }],
        }
      }))
    }
    if (scope === 'all' || scope === 'youtube') {
      out.push(...(yt || []).map((h): Item => {
        if (h.kind === 'channel') {
          const c = h.c, sub = done[h.key] ?? c.subscribed_id
          return {
            key: h.key, section: 'YouTube', title: c.title, sub: [c.handle, c.followers ? `${compact(c.followers)} subscribers` : ''].filter(Boolean).join(' · '), thumb: c.thumbnail, round: true,
            badge: { text: 'Channel', cls: 'channel' }, open: () => { setPeek({ key: h.key, url: c.url, title: c.title, subscribed: sub }); close() },
            actions: sub ? [{ label: '✓ Following', run: () => go(`/podcasts/${sub}`) }] : [{ label: busy[h.key] ? 'Subscribing…' : '+ Subscribe', primary: true, disabled: busy[h.key], run: () => act(h.key, () => api.post<Podcast>(`${V1}/podcasts`, { url: c.url }), `Subscribed to ${c.title}`) }],
          }
        }
        const v = h.v, isVideo = h.kind === 'video', have = done[h.key] ?? (isVideo ? v.episode_id : v.subscribed_id)
        const get = () => act(h.key, () => api.post<{ id: number }>(`${V1}/youtube/video`, { url: v.url }), `“${v.title}” is downloading into your Picks`)
        return {
          key: h.key, section: 'YouTube', title: v.title, sub: [v.channel, v.views ? `${compact(v.views)} views` : '', isVideo && v.duration ? fmtDur(v.duration) : ''].filter(Boolean).join(' · '),
          thumb: v.thumbnail, wide: true, badge: { text: isVideo ? 'Video' : 'Playlist', cls: h.kind },
          open: isVideo ? (have ? () => go(`/episodes/${have}`) : get) : () => { setPeek({ key: h.key, url: v.url, title: v.title, subscribed: have }); close() },
          actions: isVideo
            ? [have ? { label: '✓ In library', run: () => go(`/episodes/${have}`) } : { label: busy[h.key] ? 'Adding…' : '↓ Get', primary: true, disabled: busy[h.key], run: get }]
            : (have ? [{ label: '✓ Following', run: () => go(`/podcasts/${have}`) }] : [{ label: busy[h.key] ? 'Subscribing…' : '+ Subscribe', primary: true, disabled: busy[h.key], run: () => act(h.key, () => api.post<Podcast>(`${V1}/podcasts`, { url: v.url }), `Subscribed to ${v.title}`) }]),
        }
      }))
    }
    return out
  }, [dq, scope, pages, pods, eps, dir, yt, done, busy, go, close, act, player])

  useEffect(() => { if (active >= items.length) setActive(Math.max(0, items.length - 1)) }, [items.length, active])
  useEffect(() => { listRef.current?.querySelector<HTMLElement>(`[data-i="${active}"]`)?.scrollIntoView({ block: 'nearest' }) }, [active])

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setActive(a => Math.min(items.length - 1, a + 1)) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setActive(a => Math.max(0, a - 1)) }
    else if (e.key === 'Enter') { e.preventDefault(); items[active]?.open() }
    else if (e.key === 'Tab') { e.preventDefault(); const i = SCOPES.findIndex(s => s.value === scope); setScope(SCOPES[(i + (e.shiftKey ? SCOPES.length - 1 : 1)) % SCOPES.length].value) }
    else if (e.key === 'Escape') { e.preventDefault(); close() }
  }

  const loading = dq.length >= 2 && (((scope === 'all' || scope === 'library') && eps === null) || ((scope === 'all' || scope === 'podcasts') && dir === null) || ((scope === 'all' || scope === 'youtube') && !!settings?.yt_dlp && yt === null))
  let section = ''
  return (
    <>
      {open && (
        <div className="cmdk-backdrop" onMouseDown={e => { if (e.target === e.currentTarget) close() }}>
          <div className="cmdk" role="dialog" aria-modal="true" aria-label="Search everything">
            <div className="cmdk-input">
              <Icon name="search" size={18} />
              <input ref={input} value={q} onChange={e => setQ(e.target.value)} onKeyDown={onKey} placeholder={scope === 'youtube' ? 'Search YouTube…' : scope === 'podcasts' ? 'Search podcast directories…' : scope === 'library' ? 'Search your podcasts and episodes…' : 'Search everything…'} aria-label="Search" spellCheck={false} />
              {loading && <span className="spinner sm" />}
              <kbd onClick={close} role="button" aria-label="Close">esc</kbd>
            </div>
            <div className="cmdk-scopes" role="tablist">
              {SCOPES.map(s => <button key={s.value} type="button" role="tab" aria-selected={s.value === scope} className={s.value === scope ? 'active' : ''} onClick={() => { setScope(s.value); input.current?.focus() }}>{s.label}</button>)}
            </div>
            <div className="cmdk-list" ref={listRef}>
              {items.length === 0 && !loading && <div className="cmdk-empty">{dq.length < 2 ? 'Type to search your library, podcast directories and YouTube.' : 'Nothing found.'}</div>}
              {items.map((it, i) => {
                const head = it.section !== section ? (section = it.section) : null
                return (
                  <div key={it.key}>
                    {head && <div className="cmdk-section">{head}{head === 'YouTube' && yt === null && <span className="spinner sm" />}</div>}
                    <div data-i={i} className={`cmdk-item ${i === active ? 'active' : ''}`} onMouseMove={() => setActive(i)} onClick={() => it.open()} role="option" aria-selected={i === active}>
                      {it.icon ? <span className="cmdk-icon"><Icon name={it.icon} size={16} /></span>
                        : it.wide ? <img className="cmdk-wide" src={it.thumb} alt="" loading="lazy" />
                        : <Artwork src={it.thumb} title={it.title} size="xs" className={it.round ? 'round' : ''} />}
                      <span className="cmdk-text">
                        <span className="cmdk-title">{it.badge && <span className={`yt-badge ${it.badge.cls}`}>{it.badge.text}</span>}{it.title}</span>
                        {it.sub && <span className="cmdk-sub">{it.sub}</span>}
                      </span>
                      {it.actions && <span className="cmdk-actions" onClick={e => e.stopPropagation()}>{it.actions.map(a => <button key={a.label} type="button" className={`btn sm ${a.primary ? 'primary' : ''}`} disabled={a.disabled} onClick={a.run}>{a.label}</button>)}</span>}
                    </div>
                  </div>
                )
              })}
              {(() => {
                const waitDir = dq.length >= 2 && (scope === 'all' || scope === 'podcasts') && dir === null
                const waitYT = dq.length >= 2 && (scope === 'all' || scope === 'youtube') && !!settings?.yt_dlp && yt === null
                if (!items.length || (!waitDir && !waitYT)) return null
                return <div className="cmdk-section muted"><span className="spinner sm" /> Still looking{waitDir ? ' in podcast directories' : ''}{waitDir && waitYT ? ' and' : ''}{waitYT ? ' on YouTube' : ''}…</div>
              })()}
            </div>
            <div className="cmdk-foot"><span><kbd>↑</kbd><kbd>↓</kbd> move</span><span><kbd>↵</kbd> open</span><span><kbd>tab</kbd> scope</span><span><kbd>esc</kbd> close</span></div>
          </div>
        </div>
      )}
      {peek && <SourcePeek url={peek.url} title={peek.title} subscribedID={peek.subscribed} onSubscribed={id => setDone(d => ({ ...d, [peek.key]: id }))} onClose={() => setPeek(null)} />}
    </>
  )
}
