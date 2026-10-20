import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { usePlayer, labelFor } from '../lib/player'
import type { EpisodeDetail, Podcast } from '../lib/types'
import { Loading, useAsync, useToast } from '../components/ui'
import { useEpisodeActions } from '../components/EpisodeRow'
import { Artwork } from '../components/Artwork'
import { safeHTML } from '../components/Player'
import { bytes, clock, fmtDate, fmtDur, progressPct, DL_LABEL } from '../lib/format'

export default function EpisodePage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const player = usePlayer()
  const act = useEpisodeActions()
  const { data: e, setData, reload } = useAsync(() => api.get<EpisodeDetail>(`${V1}/episodes/${id}`), [id])
  const [busy, setBusy] = useState(false)
  const [menu, setMenu] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
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
  useEffect(() => {
    if (!menu) return
    const h = (ev: MouseEvent) => { if (menuRef.current && !menuRef.current.contains(ev.target as Node)) setMenu(false) }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [menu])
  if (!e) return <Loading />

  const upd = (x: Partial<EpisodeDetail> | null) => { if (x === null) navigate(`/podcasts/${e.podcast_id}`); else setData({ ...e, ...x }) }
  const run = async (fn: () => Promise<unknown>) => { setBusy(true); try { await fn() } finally { setBusy(false) } }
  const isCur = player.isCurrent(e.id)
  const playing = isCur && player.playing
  const dur = (isCur && player.duration) || e.duration
  const pos = isCur ? player.time : e.play_position
  const pct = isCur && player.duration ? Math.round((player.time / player.duration) * 100) : progressPct(e)
  const started = pct > 0 && !e.completed
  const art = e.artwork_url || e.podcast_artwork
  const src = e.source
  const showName = src?.title || e.podcast_title
  // picks carry " — <show>" in their title for lists; the page names the show above it
  const title = src?.title && e.title.endsWith(' — ' + src.title) ? e.title.slice(0, -(src.title.length + 3)) : e.title
  const downloading = e.download === 'queued' || e.download === 'downloading'
  const canPlay = Boolean(e.stream_url)
  const follow = () => run(async () => {
    try { const p = await api.post<Podcast>(`${V1}/podcasts`, { url: src!.feed_url }); toast(`Following ${p.title}`); setData({ ...e, source: { ...src!, subscribed_id: p.id } }) } catch (err) { toast((err as Error).message, 'err') }
  })
  const showLink = src?.subscribed_id ? `/podcasts/${src.subscribed_id}` : src ? `/add?tab=${src.kind === 'youtube' ? 'youtube' : 'url'}&url=${encodeURIComponent(src.feed_url)}` : `/podcasts/${e.podcast_id}`
  const meta = [
    e.pub_date && fmtDate(e.pub_date),
    e.duration > 0 && fmtDur(e.duration),
    e.season > 0 && `S${e.season}${e.episode_no ? ` · E${e.episode_no}` : ''}`,
    !e.season && e.episode_no > 0 && `Episode ${e.episode_no}`,
  ].filter(Boolean) as string[]
  const playLabel = playing ? 'Pause' : !canPlay ? (downloading ? 'Downloading…' : 'Download & play') : e.completed ? 'Play again' : started ? 'Resume' : 'Play'

  return (
    <div className="epx">
      <section className="epx-hero">
        <div className="epx-hero-bg" aria-hidden>{art && <div className="epx-backdrop" style={{ backgroundImage: `url("${art}")` }} />}</div>
        <div className="epx-hero-inner">
          <Artwork src={art} title={showName} size="lg" className="epx-art" />
          <div className="epx-head">
            <Link to={showLink} className="epx-show">{showName}{src && <span className="epx-via"> · in your Picks</span>}</Link>
            <h1 className="epx-title">{title}</h1>
            <div className="epx-meta">
              {meta.map(m => <span key={m}>{m}</span>)}
              {e.completed && <span className="badge ok-badge">played</span>}
              {e.queue_pos > 0 && <span className="badge cat">#{e.queue_pos} in queue</span>}
              {e.download === 'done' && <span className="badge" title="The audio file is stored on your Pocket Podcasts server">on your server</span>}
              {e.has_sponsors && <span className="badge" title="SponsorBlock segments are skipped while playing">SponsorBlock</span>}
            </div>
            <div className="epx-play">
              <button className="epx-play-btn" onClick={() => playing ? player.pause() : player.play(e)} disabled={!canPlay && downloading} aria-label={playLabel}>
                {playing ? '❚❚' : downloading && !canPlay ? <span className="spinner" style={{ borderTopColor: '#fff' }} /> : !canPlay ? '↓' : '▶'}
              </button>
              <div className="epx-play-info">
                <b>{playLabel}</b>
                {started ? (
                  <div className="epx-progress"><div className="bar"><span className="fill" style={{ width: `${pct}%` }} /></div><span className="muted small">{clock(pos)} / {clock(dur)} · {fmtDur(Math.max(0, dur - pos))} left</span></div>
                ) : e.completed ? <span className="muted small">You finished this one</span> : !canPlay ? <span className="muted small">{downloading ? 'Fetching the audio to your server' : 'Fetches the audio first'}</span> : null}
              </div>
            </div>
            <div className="epx-actions">
              <button className={`pill ${e.queue_pos ? 'on' : ''}`} onClick={() => run(async () => upd(await act.queue(e)))} disabled={busy}>{e.queue_pos ? '✓ Queued' : '＋ Queue'}</button>
              <button className="pill" onClick={() => run(async () => upd(await act.queue(e, true)))} disabled={busy || e.queue_pos === 1}>↱ Play next</button>
              <button className={`pill ${e.saved ? 'on' : ''}`} onClick={() => run(async () => upd(await act.save(e)))} disabled={busy}>{e.saved ? '♥ Saved' : '♡ Save'}</button>
              <button className={`pill ${e.completed ? 'on' : ''}`} onClick={() => run(async () => upd(await act.played(e, !e.completed)))} disabled={busy}>{e.completed ? '✓ Played' : '○ Mark played'}</button>
              <div className="menu-wrap" ref={menuRef}>
                <button className="pill" onClick={() => setMenu(m => !m)} aria-label="More">⋯</button>
                {menu && (
                  <div className="menu" onClick={() => setMenu(false)}>
                    <button onClick={() => player.play(e, { fromStart: true })}>Play from the start</button>
                    {e.download === 'done'
                      ? <><a href={`${e.stream_url}?download=1`}>Save a copy to this device</a><button onClick={() => run(async () => upd(await act.download(e)))}>Delete the server copy</button></>
                      : !downloading && <button onClick={() => run(async () => upd(await act.download(e)))}>Keep a copy on the server</button>}
                    {e.link && <a href={e.link} target="_blank" rel="noreferrer">{e.podcast_kind === 'youtube' || e.youtube_id ? 'Watch on YouTube ↗' : 'Open the episode page ↗'}</a>}
                    {e.transcript_url && <a href={e.transcript_url} target="_blank" rel="noreferrer">Transcript ↗</a>}
                    {e.last_played_at && <button onClick={() => run(async () => { await api.del(`${V1}/episodes/${e.id}/history`); setData({ ...e, play_position: 0, completed: false, last_played_at: '' }) })}>Forget my progress</button>}
                    <button className="danger-text" onClick={() => { if (confirm('Remove this episode?')) act.remove(e).then(() => { toast('Removed'); navigate(`/podcasts/${e.podcast_id}`) }) }}>Remove episode</button>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      </section>

      {src && (
        <section className={`epx-source ${src.subscribed_id ? 'following' : ''}`}>
          <span className="epx-source-text">{src.subscribed_id
            ? <>You follow <b>{src.title || 'this show'}</b>.</>
            : <>Picked from <b>{src.title || (src.kind === 'youtube' ? 'a YouTube channel' : 'a podcast')}</b> — only this episode is in your library.</>}</span>
          <span className="row" style={{ gap: 6, flexShrink: 0 }}>
            <Link className="btn sm" to={showLink}>All episodes →</Link>
            {!src.subscribed_id && <button className="btn sm primary" onClick={follow} disabled={busy}>+ Follow show</button>}
          </span>
        </section>
      )}

      <div className="epx-body">
        <article className="epx-notes">
          <h2>Show notes</h2>
          {e.description ? <div className="md notes" dangerouslySetInnerHTML={{ __html: safeHTML(e.description) }} /> : <p className="muted">This episode has no show notes.</p>}
        </article>
        <aside className="epx-aside">
          {e.chapters.length > 0 && (
            <div className="epx-panel">
              <h3>Chapters <span className="muted">{e.chapters.length}</span></h3>
              <ul className="chapters">
                {e.chapters.map((c, i) => <li key={i} className={isCur && player.chapter === c ? 'cur' : ''}><button onClick={() => { if (isCur) player.seek(c.start); else player.play({ ...e, play_position: Math.floor(c.start) }) }}><span className="mono">{clock(c.start)}</span><span className="grow">{c.title}</span></button></li>)}
              </ul>
            </div>
          )}
          {e.sponsors.length > 0 && (
            <div className="epx-panel">
              <h3>SponsorBlock <span className="muted">{e.sponsors.length}</span></h3>
              <ul className="chapters">
                {e.sponsors.map((s, i) => <li key={i}><button onClick={() => isCur && player.seek(s.start)}><span className="mono">{clock(s.start)}</span><span className="grow">{labelFor(s.category)}</span><span className="muted small">{Math.round(s.end - s.start)} s</span></button></li>)}
              </ul>
              <p className="muted small" style={{ margin: '8px 0 0' }}>Skipped automatically for the categories in Settings → Playback.</p>
            </div>
          )}
          <div className="epx-panel">
            <h3>Details</h3>
            <dl className="facts">
              <dt>Show</dt><dd><Link to={showLink}>{showName}</Link></dd>
              {src && <><dt>Library</dt><dd><Link to={`/podcasts/${e.podcast_id}`}>{e.podcast_title}</Link></dd></>}
              {e.pub_date && <><dt>Published</dt><dd>{fmtDate(e.pub_date)}</dd></>}
              {e.duration > 0 && <><dt>Length</dt><dd>{fmtDur(e.duration)}</dd></>}
              {e.last_played_at && <><dt>Last played</dt><dd>{fmtDate(e.last_played_at)}{e.play_count > 1 && ` · ${e.play_count}×`}</dd></>}
              <dt>Audio</dt><dd>{e.download === 'done' ? <>On your server · {bytes(e.file_bytes)}</> : e.download === 'none' ? 'Streams from the publisher' : DL_LABEL[e.download]}{e.download_error && <div className="bad small">{e.download_error}</div>}</dd>
            </dl>
          </div>
        </aside>
      </div>
    </div>
  )
}
