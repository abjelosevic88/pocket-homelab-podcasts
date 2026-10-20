import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Episode, Podcast as PodcastT } from '../lib/types'
import { Card, Confirm, Empty, Field, Loading, Modal, PageHeader, Segmented, useAsync, useDebounce, useToast } from '../components/ui'
import { EpisodeList } from '../components/EpisodeRow'
import { Artwork } from '../components/Artwork'
import { ago, bytes, fmtDate, plain } from '../lib/format'
import { safeHTML } from '../components/Player'

export default function Podcast() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const { data: p, setData: setP, reload: reloadP } = useAsync(() => api.get<PodcastT>(`${V1}/podcasts/${id}`), [id])
  const [status, setStatus] = useState<string>('')
  const [sort, setSort] = useState('')
  const [q, setQ] = useState('')
  const dq = useDebounce(q)
  const [page, setPage] = useState(0)
  const LIMIT = 50
  const eps = useAsync(() => api.get<{ episodes: Episode[]; total: number }>(`${V1}/podcasts/${id}/episodes?status=${status}&sort=${sort}&q=${encodeURIComponent(dq)}&limit=${LIMIT}&offset=${page * LIMIT}`), [id, status, sort, dq, page])
  const [settings, setSettings] = useState(false)
  const [unsub, setUnsub] = useState(false)
  const [busy, setBusy] = useState(false)
  const [more, setMore] = useState(false)
  useEffect(() => { setPage(0) }, [status, sort, dq, id])
  const setEps = useCallback((list: Episode[]) => eps.setData(d => d ? { ...d, episodes: list } : d), [eps])
  useEffect(() => {
    if (!p?.refreshing) return
    const t = setInterval(() => { reloadP(); eps.reload() }, 3000)
    return () => clearInterval(t)
  }, [p?.refreshing]) // eslint-disable-line react-hooks/exhaustive-deps
  if (!p) return <Loading />
  const refresh = async () => {
    setBusy(true)
    try { const r = await api.post<{ new: number; podcast: PodcastT }>(`${V1}/podcasts/${p.id}/refresh`); setP(r.podcast); toast(r.new ? `${r.new} new episode${r.new === 1 ? '' : 's'}` : 'Nothing new'); eps.reload() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const fav = async () => { const r = await api.post<{ favorite: boolean }>(`${V1}/podcasts/${p.id}/favorite`, { favorite: !p.favorite }); setP({ ...p, favorite: r.favorite }) }
  const markAll = async (played: boolean) => { await api.post(`${V1}/podcasts/${p.id}/played`, { played }); toast(played ? 'All marked as played' : 'All marked as unplayed'); eps.reload(); reloadP() }
  const dlMore = async () => { const r = await api.post<{ queued: number }>(`${V1}/podcasts/${p.id}/download`, { count: 5 }); toast(`${r.queued} download${r.queued === 1 ? '' : 's'} queued`); setTimeout(eps.reload, 1500) }
  const rmFiles = async (played: boolean) => { const r = await api.del<{ removed: number }>(`${V1}/podcasts/${p.id}/files${played ? '?played=1' : ''}`); toast(`${r.removed} file${r.removed === 1 ? '' : 's'} removed`); eps.reload(); reloadP() }
  const desc = p.description || ''
  return (
    <>
      <div className="pod-head">
        <Artwork src={p.artwork} title={p.title} size="lg" />
        <div className="pod-head-info">
          <div className="row wrap" style={{ gap: 6 }}>
            {p.kind === 'youtube' && <span className="badge cat">YouTube {p.youtube_kind}</span>}
            {p.categories && p.categories.split(',').slice(0, 3).map(c => <span key={c} className="badge">{c.trim()}</span>)}
            {p.explicit && <span className="badge warn">explicit</span>}
          </div>
          <h1 className="pod-title">{p.title}</h1>
          {p.author && <div className="pod-author">{p.author}</div>}
          <div className="muted small pod-facts">
            <span>{p.episode_count} episodes</span>{p.unplayed_count > 0 && <span>{p.unplayed_count} unplayed</span>}{p.downloaded_count > 0 && <span>{p.downloaded_count} on disk · {bytes(p.downloaded_bytes)}</span>}
            {p.last_refresh_ok && <span title={p.last_refresh_ok}>checked {ago(p.last_refresh_ok)}</span>}{p.refreshing && <span className="row"><span className="spinner sm" /> refreshing</span>}
          </div>
          {p.last_error && <div className="callout danger small mt-sm">Last refresh failed: {p.last_error}{p.failures > 1 && ` (${p.failures}× in a row — next try is spaced out)`}</div>}
          {desc && <div className={`pod-desc md ${more ? '' : 'clamped'}`}>{more ? <div dangerouslySetInnerHTML={{ __html: safeHTML(desc) }} /> : plain(desc, 320)}{plain(desc, 400).length > 300 && <button className="btn ghost sm read-more" onClick={() => setMore(m => !m)}>{more ? 'less' : 'more'}</button>}</div>}
          <div className="row wrap mt-sm">
            <button className="btn primary" onClick={refresh} disabled={busy || p.refreshing}>{busy ? 'Refreshing…' : '↻ Refresh'}</button>
            <button className={`btn ${p.favorite ? 'on' : ''}`} onClick={fav} title="Favourites stay on top">{p.favorite ? '★ Favourite' : '☆ Favourite'}</button>
            <button className="btn" onClick={() => setSettings(true)}>⚙ Settings</button>
            {p.site_url && <a className="btn ghost" href={p.site_url} target="_blank" rel="noreferrer">Website ↗</a>}
            <div className="menu-wrap"><MoreMenu onMarkAll={markAll} onDownload={dlMore} onRemoveFiles={rmFiles} onUnsubscribe={() => setUnsub(true)} kind={p.kind} /></div>
          </div>
        </div>
      </div>
      <div className="toolbar mt">
        <label className="search"><span className="ico">⌕</span><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search episodes…" /></label>
        <Segmented size="sm" value={status} onChange={setStatus} options={[{ value: '', label: 'All' }, { value: 'unfinished', label: 'Unplayed' }, { value: 'partial', label: 'In progress' }, { value: 'played', label: 'Played' }]} />
        <Segmented size="sm" value={sort} onChange={setSort} options={[{ value: '', label: 'Newest' }, { value: 'oldest', label: 'Oldest' }, { value: 'longest', label: 'Longest' }, { value: 'shortest', label: 'Shortest' }]} />
      </div>
      {eps.loading && !eps.data ? <Loading /> : (
        <>
          <EpisodeList episodes={eps.data?.episodes || []} onChange={setEps} showPodcast={false} empty={<Empty title={p.kind === 'youtube' && p.episode_count === 0 ? 'Indexing…' : 'No episodes'}>{p.kind === 'youtube' && p.episode_count === 0 ? 'The channel is being read; this takes a few seconds.' : dq ? 'Nothing matches.' : 'This feed has no episodes yet.'}</Empty>} />
          {eps.data && eps.data.total > LIMIT && (
            <div className="row mt" style={{ justifyContent: 'center' }}>
              <button className="btn" disabled={page === 0} onClick={() => setPage(p => p - 1)}>← Newer</button>
              <span className="muted small">{page * LIMIT + 1}–{Math.min((page + 1) * LIMIT, eps.data.total)} of {eps.data.total}</span>
              <button className="btn" disabled={(page + 1) * LIMIT >= eps.data.total} onClick={() => setPage(p => p + 1)}>Older →</button>
            </div>
          )}
        </>
      )}
      {settings && <PodcastSettings p={p} onClose={() => setSettings(false)} onSaved={np => { setP(np); setSettings(false) }} />}
      {unsub && <Confirm title={`Unsubscribe from ${p.title}?`} message="Its episodes, your progress on them and every downloaded file are deleted." onCancel={() => setUnsub(false)} onConfirm={async () => { await api.del(`${V1}/podcasts/${p.id}`); toast('Unsubscribed'); navigate('/podcasts') }} />}
    </>
  )
}

function MoreMenu({ onMarkAll, onDownload, onRemoveFiles, onUnsubscribe, kind }: { onMarkAll: (v: boolean) => void; onDownload: () => void; onRemoveFiles: (played: boolean) => void; onUnsubscribe: () => void; kind: string }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button className="btn ghost" onClick={() => setOpen(o => !o)}>⋯</button>
      {open && <div className="menu" onClick={() => setOpen(false)} onMouseLeave={() => setOpen(false)}>
        <button onClick={() => onMarkAll(true)}>Mark all as played</button>
        <button onClick={() => onMarkAll(false)}>Mark all as unplayed</button>
        <button onClick={onDownload}>{kind === 'youtube' ? 'Download 5 more videos' : 'Download the newest 5'}</button>
        <button onClick={() => onRemoveFiles(true)}>Remove files of played episodes</button>
        <button onClick={() => onRemoveFiles(false)}>Remove all files</button>
        <button className="danger-text" onClick={onUnsubscribe}>Unsubscribe</button>
      </div>}
    </>
  )
}

/** Per-podcast settings: playback, downloads, feed, artwork. */
function PodcastSettings({ p, onClose, onSaved }: { p: PodcastT; onClose: () => void; onSaved: (p: PodcastT) => void }) {
  const { toast } = useToast()
  const { settings } = useApp()
  const [f, setF] = useState({ ...p })
  const [busy, setBusy] = useState(false)
  const [artURL, setArtURL] = useState('')
  const yt = p.kind === 'youtube'
  const set = (k: keyof PodcastT, v: unknown) => setF(x => ({ ...x, [k]: v }))
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { onSaved(await api.put<PodcastT>(`${V1}/podcasts/${p.id}`, f)); toast('Saved') } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  const uploadArt = async (file: File) => {
    try { onSaved(await upload<PodcastT>(`${V1}/podcasts/${p.id}/artwork`, 'file', file)); toast('Artwork replaced') } catch (err) { toast((err as Error).message, 'err') }
  }
  const artFromURL = async () => {
    try { onSaved(await api.post<PodcastT>(`${V1}/podcasts/${p.id}/artwork`, { url: artURL })); toast('Artwork replaced') } catch (err) { toast((err as Error).message, 'err') }
  }
  const resetArt = async () => { onSaved(await api.del<PodcastT>(`${V1}/podcasts/${p.id}/artwork`)); toast('Back to the feed artwork') }
  return (
    <Modal title={`Settings — ${p.title}`} onClose={onClose} size="lg">
      <form onSubmit={save} className="grid" style={{ gap: 18 }}>
        <div className="two-col">
          <Card title="Playback">
            <div className="grid" style={{ gap: 12 }}>
              <Field label="Title"><input value={f.title} onChange={e => set('title', e.target.value)} /></Field>
              <Field label="Playback speed" help={`0 = your default (${settings?.playback_speed || 1}×)`}><input type="number" step={0.05} min={0} max={3} value={f.playback_speed} onChange={e => set('playback_speed', Number(e.target.value))} className="num" /></Field>
              <div className="two-col tight">
                <Field label="Skip intro (s)"><input type="number" min={0} value={f.start_skip} onChange={e => set('start_skip', Number(e.target.value))} className="num" /></Field>
                <Field label="Skip outro (s)"><input type="number" min={0} value={f.end_skip} onChange={e => set('end_skip', Number(e.target.value))} className="num" /></Field>
              </div>
              <Field group label="Auto-play next in queue"><Segmented size="sm" value={f.auto_play_next} onChange={v => set('auto_play_next', v)} options={[{ value: -1, label: 'Default' }, { value: 1, label: 'On' }, { value: 0, label: 'Off' }]} /></Field>
              <label className="row"><input type="checkbox" checked={f.notify} onChange={e => set('notify', e.target.checked)} style={{ width: 'auto' }} /> Notify me about new episodes</label>
              {yt && <label className="row"><input type="checkbox" checked={f.sponsorblock} onChange={e => set('sponsorblock', e.target.checked)} style={{ width: 'auto' }} /> Fetch SponsorBlock segments</label>}
            </div>
          </Card>
          <Card title={yt ? 'Downloads (YouTube)' : 'Downloads'}>
            <div className="grid" style={{ gap: 12 }}>
              {!yt && <label className="row"><input type="checkbox" checked={f.auto_download} onChange={e => set('auto_download', e.target.checked)} style={{ width: 'auto' }} /> Download new episodes automatically</label>}
              {yt && <Field label="Videos fetched when subscribing" help="Afterwards every new upload is downloaded. Older ones can be fetched by hand."><input type="number" min={0} max={500} value={f.download_limit} onChange={e => set('download_limit', Number(e.target.value))} className="num" /></Field>}
              <Field label="Keep files for (days)" help="0 = keep until you delete them. Saved and queued episodes are never removed."><input type="number" min={0} value={f.retention_days} onChange={e => set('retention_days', Number(e.target.value))} className="num" /></Field>
              {yt && <>
                <Field label="Skip videos shorter than (s)" help="Shorts and clips. 0 = take everything."><input type="number" min={0} value={f.min_duration} onChange={e => set('min_duration', Number(e.target.value))} className="num" /></Field>
                <Field label="Only titles matching" help="Regular expression, case-insensitive. Empty = all."><input value={f.title_filter} onChange={e => set('title_filter', e.target.value)} placeholder="e.g. ^Podcast|Interview" spellCheck={false} /></Field>
              </>}
              {!yt && <Field label="Ignore episodes older than (days)" help="0 = the whole feed."><input type="number" min={0} value={f.feed_cutoff_days} onChange={e => set('feed_cutoff_days', Number(e.target.value))} className="num" /></Field>}
              <Field label="Check for new episodes every (minutes)" help={`0 = your default (${settings?.refresh_minutes || settings?.refresh_default || 60} min)`}><input type="number" min={0} value={f.refresh_minutes} onChange={e => set('refresh_minutes', Number(e.target.value))} className="num" /></Field>
            </div>
          </Card>
        </div>
        <div className="two-col">
          <Card title="Public feed">
            <div className="grid" style={{ gap: 12 }}>
              <Field group label="Episode date in the feed" help="Apple Podcasts sorts by this. “Downloaded” puts old videos you add later at the top instead of a year deep.">
                <Segmented size="sm" value={f.pub_date_mode} onChange={v => set('pub_date_mode', v)} options={[{ value: 'published', label: 'Published' }, { value: 'downloaded', label: 'Downloaded' }]} />
              </Field>
              <p className="muted small">Feed URL: <code className="mono">{settings?.feed_url}{p.id}.xml</code></p>
              {!yt && <Field label="Feed URL"><input value={f.feed_url} onChange={e => set('feed_url', e.target.value)} spellCheck={false} /></Field>}
              {yt && <p className="muted small">Source: <a href={p.feed_url} target="_blank" rel="noreferrer">{p.feed_url}</a></p>}
            </div>
          </Card>
          <Card title="Artwork">
            <div className="row" style={{ alignItems: 'flex-start', gap: 14 }}>
              <Artwork src={p.artwork} title={p.title} size="md" />
              <div className="grid grow" style={{ gap: 8 }}>
                <label className="btn">Upload image…<input type="file" accept="image/*" hidden onChange={e => e.target.files?.[0] && uploadArt(e.target.files[0])} /></label>
                <div className="row"><input value={artURL} onChange={e => setArtURL(e.target.value)} placeholder="https://… image URL" spellCheck={false} /><button type="button" className="btn" onClick={artFromURL} disabled={!artURL}>Use</button></div>
                {p.artwork_file && <button type="button" className="btn ghost sm" onClick={resetArt}>Back to the feed's artwork</button>}
                <p className="muted small" style={{ margin: 0 }}>Apple Podcasts wants square artwork of at least 1400×1400 px.</p>
              </div>
            </div>
          </Card>
        </div>
        <div className="form-actions"><span className="muted small" style={{ marginRight: 'auto' }}>Subscribed {fmtDate(p.subscribed_at)}</span><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>Save</button></div>
      </form>
    </Modal>
  )
}

