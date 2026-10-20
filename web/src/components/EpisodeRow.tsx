import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { usePlayer } from '../lib/player'
import { useToast } from './ui'
import { Artwork } from './Artwork'
import type { Episode } from '../lib/types'
import { DL_LABEL, fmtDate, fmtDur, left, plain, progressPct } from '../lib/format'

/** Actions on an episode that every list and page shares. Each returns the
 *  patched episode so the caller can update its own copy. */
export function useEpisodeActions() {
  const { toast } = useToast()
  const run = async <T,>(fn: () => Promise<T>, ok?: string): Promise<T | null> => {
    try { const r = await fn(); if (ok) toast(ok); return r } catch (e) { toast((e as Error).message, 'err'); return null }
  }
  return {
    queue: async (e: Episode, front = false) => {
      if (e.queue_pos) { await run(() => api.del(`${V1}/episodes/${e.id}/queue`), 'Removed from queue'); window.dispatchEvent(new CustomEvent('pp:queue')); return { ...e, queue_pos: 0 } }
      const q = await run(() => api.post<Episode[]>(`${V1}/episodes/${e.id}/queue`, { front }), front ? 'Playing next' : 'Added to queue')
      window.dispatchEvent(new CustomEvent('pp:queue'))
      return { ...e, queue_pos: q ? (q.find(x => x.id === e.id)?.queue_pos || 1) : 1 }
    },
    save: async (e: Episode) => {
      if (e.saved) { await run(() => api.del(`${V1}/episodes/${e.id}/save`)); return { ...e, saved: false } }
      await run(() => api.post(`${V1}/episodes/${e.id}/save`), 'Saved'); return { ...e, saved: true }
    },
    download: async (e: Episode) => {
      if (e.download === 'done') { const r = await run(() => api.del<Episode>(`${V1}/episodes/${e.id}/download`), 'File removed'); return r ? { ...e, ...r } : e }
      const r = await run(() => api.post<Episode>(`${V1}/episodes/${e.id}/download`), 'Download queued')
      return r ? { ...e, ...r } : e
    },
    played: async (e: Episode, played: boolean) => {
      if (played) { await run(() => api.post(`${V1}/episodes/${e.id}/played`), 'Marked as played'); return { ...e, completed: true, queue_pos: 0 } }
      await run(() => api.del(`${V1}/episodes/${e.id}/played`), 'Marked as unplayed'); return { ...e, completed: false, play_position: 0 }
    },
    remove: async (e: Episode) => { await run(() => api.del(`${V1}/episodes/${e.id}`), 'Episode removed'); return null },
  }
}

interface Props {
  ep: Episode
  onChange?: (e: Episode | null) => void
  showPodcast?: boolean
  compact?: boolean
  index?: number
  dragHandle?: React.ReactNode
}

export function EpisodeRow({ ep, onChange, showPodcast = true, compact = false, dragHandle }: Props) {
  const player = usePlayer()
  const act = useEpisodeActions()
  const [menu, setMenu] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const isCur = player.isCurrent(ep.id)
  const playing = isCur && player.playing
  const pct = isCur && player.duration ? Math.round((player.time / player.duration) * 100) : progressPct(ep)
  const upd = (e: Episode | null) => onChange?.(e)
  useEffect(() => {
    if (!menu) return
    const h = (e: MouseEvent) => { if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenu(false) }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [menu])
  const dl = ep.download
  const dlIcon = dl === 'done' ? '✓' : dl === 'queued' || dl === 'downloading' ? <span className="spinner sm" /> : dl === 'failed' ? '!' : '↓'
  const canPlay = Boolean(ep.stream_url) || dl === 'queued' || dl === 'downloading'
  return (
    <div className={`ep-row ${compact ? 'compact' : ''} ${ep.completed ? 'played' : ''} ${isCur ? 'current' : ''}`}>
      {dragHandle}
      <button className={`play-btn ${playing ? 'on' : ''}`} onClick={() => playing ? player.pause() : player.play(ep)} title={playing ? 'Pause' : canPlay ? 'Play' : 'Download and play'} aria-label={playing ? 'Pause' : 'Play'}>
        {playing ? '❚❚' : canPlay ? '▶' : '↓'}
      </button>
      <Link to={`/episodes/${ep.id}`} className="ep-art"><Artwork src={ep.artwork_url || ep.podcast_artwork} title={ep.podcast_title || ep.title} size={compact ? 'xs' : 'sm'} /></Link>
      <div className="ep-info">
        <Link to={`/episodes/${ep.id}`} className="ep-title">{ep.title}</Link>
        <div className="ep-meta">
          {showPodcast && <Link to={`/podcasts/${ep.podcast_id}`} className="ep-pod">{ep.podcast_title}</Link>}
          {ep.pub_date && <span>{fmtDate(ep.pub_date)}</span>}
          {ep.duration > 0 && <span>{left(ep) || fmtDur(ep.duration)}</span>}
          {ep.completed && <span className="badge ok-badge">played</span>}
          {dl !== 'none' && dl !== 'culled' && <span className={`badge ${dl === 'failed' ? 'warn' : ''}`} title={ep.download_error || undefined}>{DL_LABEL[dl]}</span>}
          {ep.queue_pos > 0 && <span className="badge cat">#{ep.queue_pos} in queue</span>}
          {ep.saved && <span className="badge">saved</span>}
          {ep.has_sponsors && <span className="badge" title="SponsorBlock segments known: they are skipped while playing">SB</span>}
        </div>
        {!compact && ep.description && <div className="ep-desc">{plain(ep.description, 180)}</div>}
        {pct > 0 && !ep.completed && <div className="bar"><span className="fill" style={{ width: `${pct}%` }} /></div>}
      </div>
      <div className="ep-actions">
        <button className={`btn ghost sm ${ep.queue_pos ? 'on' : ''}`} onClick={async () => upd(await act.queue(ep))} title={ep.queue_pos ? 'Remove from queue' : 'Add to queue'}>{ep.queue_pos ? '✓' : '+'}</button>
        <button className={`btn ghost sm hide-sm ${dl === 'done' ? 'on' : ''}`} onClick={async () => upd(await act.download(ep))} title={dl === 'done' ? 'Remove the downloaded file' : 'Download'}>{dlIcon}</button>
        <div className="menu-wrap" ref={menuRef}>
          <button className="btn ghost sm" onClick={() => setMenu(m => !m)} aria-label="More">⋯</button>
          {menu && (
            <div className="menu" onClick={() => setMenu(false)}>
              <button onClick={async () => upd(await act.queue(ep, true))}>Play next</button>
              <button onClick={() => player.play(ep, { fromStart: true })}>Play from the start</button>
              <button onClick={async () => upd(await act.save(ep))}>{ep.saved ? 'Unsave' : 'Save for later'}</button>
              <button onClick={async () => upd(await act.played(ep, !ep.completed))}>{ep.completed ? 'Mark as unplayed' : 'Mark as played'}</button>
              <button onClick={async () => upd(await act.download(ep))}>{dl === 'done' ? 'Remove download' : 'Download'}</button>
              {ep.link && <a href={ep.link} target="_blank" rel="noreferrer">Open the original ↗</a>}
              {ep.stream_url && dl === 'done' && <a href={`${ep.stream_url}?download=1`}>Save the file…</a>}
              <button className="danger-text" onClick={async () => { if (confirm('Remove this episode from the list? It comes back on the next refresh unless the feed dropped it.')) upd(await act.remove(ep)) }}>Remove episode</button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

/** A list of episodes that keeps its own copies up to date as rows change. */
export function EpisodeList({ episodes, onChange, showPodcast = true, compact = false, empty }: { episodes: Episode[]; onChange: (eps: Episode[]) => void; showPodcast?: boolean; compact?: boolean; empty?: React.ReactNode }) {
  useEffect(() => {
    const h = (ev: Event) => {
      const d = (ev as CustomEvent).detail as { id: number; position: number; completed: boolean }
      if (episodes.some(e => e.id === d.id)) onChange(episodes.map(e => e.id === d.id ? { ...e, play_position: d.position, completed: d.completed } : e))
    }
    window.addEventListener('pp:progress', h)
    return () => window.removeEventListener('pp:progress', h)
  }, [episodes, onChange])
  if (episodes.length === 0) return <>{empty}</>
  return (
    <div className="ep-list">
      {episodes.map(e => <EpisodeRow key={e.id} ep={e} showPodcast={showPodcast} compact={compact} onChange={x => onChange(x ? episodes.map(o => o.id === x.id ? x : o) : episodes.filter(o => o.id !== e.id))} />)}
    </div>
  )
}
