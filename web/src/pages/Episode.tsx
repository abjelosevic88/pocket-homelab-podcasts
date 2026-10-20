import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { usePlayer, labelFor } from '../lib/player'
import type { EpisodeDetail, Podcast } from '../lib/types'
import { Card, Loading, useAsync, useToast } from '../components/ui'
import { useEpisodeActions } from '../components/EpisodeRow'
import { Artwork } from '../components/Artwork'
import { safeHTML } from '../components/Player'
import { bytes, clock, fmtDate, fmtDur, left, progressPct, DL_LABEL } from '../lib/format'

export default function EpisodePage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const player = usePlayer()
  const act = useEpisodeActions()
  const { data: e, setData, reload } = useAsync(() => api.get<EpisodeDetail>(`${V1}/episodes/${id}`), [id])
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!e || (e.download !== 'queued' && e.download !== 'downloading')) return
    const t = setInterval(reload, 3000)
    return () => clearInterval(t)
  }, [e?.download]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const h = (ev: Event) => { const d = (ev as CustomEvent).detail as { id: number; position: number; completed: boolean }; if (e && d.id === e.id) setData({ ...e, play_position: d.position, completed: d.completed }) }
    window.addEventListener('pp:progress', h)
    return () => window.removeEventListener('pp:progress', h)
  }, [e, setData])
  if (!e) return <Loading />
  const upd = (x: Partial<EpisodeDetail> | null) => { if (x === null) navigate(`/podcasts/${e.podcast_id}`); else setData({ ...e, ...x }) }
  const isCur = player.isCurrent(e.id)
  const playing = isCur && player.playing
  const pct = isCur && player.duration ? Math.round((player.time / player.duration) * 100) : progressPct(e)
  const run = async (fn: () => Promise<unknown>) => { setBusy(true); try { await fn() } finally { setBusy(false) } }
  const src = e.source
  const follow = () => run(async () => {
    try { const p = await api.post<Podcast>(`${V1}/podcasts`, { url: src!.feed_url }); toast(`Following ${p.title}`); setData({ ...e, source: { ...src!, subscribed_id: p.id } }) } catch (err) { toast((err as Error).message, 'err') }
  })
  return (
    <div className="ep-page">
      <div className="ep-side">
        <Artwork src={e.artwork_url || e.podcast_artwork} title={e.podcast_title} size="lg" />
        <div className="grid" style={{ gap: 8 }}>
          <button className="btn primary lg" onClick={() => playing ? player.pause() : player.play(e)} disabled={!e.stream_url && e.download !== 'none' && e.download !== 'failed' && e.download !== 'culled'}>
            {playing ? '❚❚ Pause' : e.stream_url ? (e.play_position > 0 && !e.completed ? '▶ Resume' : '▶ Play') : e.download === 'queued' || e.download === 'downloading' ? 'Downloading…' : '↓ Download & play'}
          </button>
          {pct > 0 && !e.completed && <div className="bar lg"><span className="fill" style={{ width: `${pct}%` }} /></div>}
          <div className="row wrap" style={{ gap: 6 }}>
            <button className={`btn sm ${e.queue_pos ? 'on' : ''}`} onClick={() => run(async () => upd(await act.queue(e)))} disabled={busy}>{e.queue_pos ? `✓ #${e.queue_pos} in queue` : '+ Queue'}</button>
            <button className="btn sm" onClick={() => run(async () => upd(await act.queue(e, true)))} disabled={busy || e.queue_pos === 1}>Play next</button>
            <button className={`btn sm ${e.saved ? 'on' : ''}`} onClick={() => run(async () => upd(await act.save(e)))} disabled={busy}>{e.saved ? '♥ Saved' : '♡ Save'}</button>
            <button className={`btn sm ${e.download === 'done' ? 'on' : ''}`} title={e.download === 'done' ? 'The audio file is stored on your Pocket Podcasts server, so it plays instantly and stays even if the publisher removes it. Click to delete the file.' : 'Store the audio file on your Pocket Podcasts server'} onClick={() => run(async () => upd(await act.download(e)))} disabled={busy || e.download === 'queued' || e.download === 'downloading'}>{e.download === 'done' ? '✓ On your server' : e.download === 'queued' || e.download === 'downloading' ? 'Downloading…' : '↓ Download to server'}</button>
            <button className="btn sm" onClick={() => run(async () => upd(await act.played(e, !e.completed)))} disabled={busy}>{e.completed ? 'Mark unplayed' : 'Mark played'}</button>
          </div>
        </div>
        {src && (
          <div className="source-card">
            <div className="muted small">Picked from</div>
            <div className="nm">{src.title || (src.kind === 'youtube' ? 'a YouTube channel' : 'a podcast')}</div>
            <div className="row wrap" style={{ gap: 6 }}>
              {src.subscribed_id
                ? <Link className="btn sm primary" to={`/podcasts/${src.subscribed_id}`}>All episodes →</Link>
                : <>
                    <Link className="btn sm" to={`/add?tab=${src.kind === 'youtube' ? 'youtube' : 'url'}&url=${encodeURIComponent(src.feed_url)}`}>All episodes →</Link>
                    <button className="btn sm primary" onClick={follow} disabled={busy}>+ Follow show</button>
                  </>}
            </div>
            {!src.subscribed_id && <div className="muted small">You don't follow this show — only this episode is in your Picks.</div>}
          </div>
        )}
        <dl className="facts">
          <dt>Podcast</dt><dd><Link to={`/podcasts/${e.podcast_id}`}>{e.podcast_title}</Link></dd>
          {e.pub_date && <><dt>Published</dt><dd>{fmtDate(e.pub_date)}</dd></>}
          {e.duration > 0 && <><dt>Length</dt><dd>{fmtDur(e.duration)}{left(e) && <span className="muted"> · {left(e)}</span>}</dd></>}
          {e.season > 0 && <><dt>Season</dt><dd>{e.season}{e.episode_no > 0 && ` · episode ${e.episode_no}`}</dd></>}
          {e.last_played_at && <><dt>Last played</dt><dd>{fmtDate(e.last_played_at)}{e.play_count > 1 && ` · ${e.play_count}×`}</dd></>}
          {e.download !== 'none' && <><dt>File</dt><dd>{e.download === 'done' ? `On your server · ${bytes(e.file_bytes)}` : DL_LABEL[e.download]}{e.download_error && <div className="bad small">{e.download_error}</div>}{e.download === 'done' && <div><a className="small" href={`${e.stream_url}?download=1`}>Save a copy to this device…</a></div>}</dd></>}
          {e.link && <><dt>Link</dt><dd><a href={e.link} target="_blank" rel="noreferrer">{e.podcast_kind === 'youtube' ? 'Watch on YouTube ↗' : 'Open ↗'}</a></dd></>}
          {e.transcript_url && <><dt>Transcript</dt><dd><a href={e.transcript_url} target="_blank" rel="noreferrer">Open ↗</a></dd></>}
        </dl>
        <button className="btn ghost sm danger-text" onClick={() => { if (confirm('Remove this episode?')) act.remove(e).then(() => { toast('Removed'); navigate(`/podcasts/${e.podcast_id}`) }) }}>Remove episode</button>
      </div>
      <div className="ep-main">
        <h1 className="ep-h1">{e.title}</h1>
        {e.chapters.length > 0 && (
          <Card title={`Chapters (${e.chapters.length})`}>
            <ul className="chapters">
              {e.chapters.map((c, i) => <li key={i} className={isCur && player.chapter === c ? 'cur' : ''}><button onClick={() => { if (isCur) player.seek(c.start); else { player.play({ ...e, play_position: Math.floor(c.start) }, {}); } }}><span className="mono">{clock(c.start)}</span><span className="grow">{c.title}</span>{c.url && <a href={c.url} target="_blank" rel="noreferrer" onClick={ev => ev.stopPropagation()}>↗</a>}</button></li>)}
            </ul>
          </Card>
        )}
        {e.sponsors.length > 0 && (
          <Card title={`SponsorBlock (${e.sponsors.length})`} className="mt">
            <p className="muted small">Segments reported by the SponsorBlock community. The categories you chose in Settings → Playback are skipped automatically.</p>
            <ul className="chapters">
              {e.sponsors.map((s, i) => <li key={i}><button onClick={() => isCur && player.seek(s.start)}><span className="mono">{clock(s.start)}–{clock(s.end)}</span><span className="grow">{labelFor(s.category)}</span><span className="muted small">{Math.round(s.end - s.start)} s</span></button></li>)}
            </ul>
          </Card>
        )}
        <Card title="Notes" className="mt">
          {e.description ? <div className="md notes" dangerouslySetInnerHTML={{ __html: safeHTML(e.description) }} /> : <p className="muted">No show notes.</p>}
        </Card>
      </div>
    </div>
  )
}
