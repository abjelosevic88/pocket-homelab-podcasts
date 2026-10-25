import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Episode, Podcast as PodcastT } from '../lib/types'
import { Confirm, Empty, Field, Loading, MenuButton, Modal, Segmented, Switch, useAsync, useDebounce, useToast } from '../components/ui'
import { EpisodeList } from '../components/EpisodeRow'
import { Artwork } from '../components/Artwork'
import { ago, bytes, fmtDate, plain } from '../lib/format'
import { safeHTML } from '../components/Player'
import { Icon } from '../components/icons'
import { useCrumb } from '../components/Layout'
import { useLive } from '../lib/realtime'

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
  useCrumb(p ? [{ label: 'Podcasts', to: '/podcasts' }, { label: p.title }] : [{ label: 'Podcasts', to: '/podcasts' }, { label: '…' }])
  const setEps = useCallback((list: Episode[]) => eps.setData(d => d ? { ...d, episodes: list } : d), [eps])
  // refreshes, new or removed episodes, downloads and settings changed anywhere show up at once
  const pid = Number(id)
  useLive(ev => (ev.type === 'refresh' && ev.podcast_id === pid) || (ev.type === 'podcasts' && ev.updated.includes(pid)), reloadP, { fallbackMs: p?.refreshing ? 3000 : 60_000 })
  useLive(ev => ((ev.type === 'episodes' || ev.type === 'download') && ev.podcast_id === pid) || (ev.type === 'refresh' && ev.podcast_id === pid && !ev.running) || (ev.type === 'history' && ev.episode_id === 0), eps.reload, { fallbackMs: p?.refreshing ? 3000 : 60_000, minGap: 1500 })
  useLive(ev => ev.type === 'podcasts' && ev.removed.includes(pid), () => navigate('/podcasts'), { fallbackMs: 0 })
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
            <span>{p.episode_count} episodes</span>{p.unplayed_count > 0 && <span>{p.unplayed_count} unplayed</span>}{p.downloaded_count > 0 && <span>{p.downloaded_count} downloaded · {bytes(p.downloaded_bytes)}</span>}
            {p.last_refresh_ok && <span title={p.last_refresh_ok}>checked {ago(p.last_refresh_ok)}</span>}{p.refreshing && <span className="row" style={{ gap: 6 }}><span className="spinner sm" /> refreshing</span>}
          </div>
          {p.last_error && <div className="callout danger small mt-sm"><Icon name="alert" size={15} /><span>Last refresh failed: {p.last_error}{p.failures > 1 && ` (${p.failures}× in a row — next try is spaced out)`}</span></div>}
          {desc && <div className={`pod-desc md ${more ? '' : 'clamped'}`}>{more ? <div dangerouslySetInnerHTML={{ __html: safeHTML(desc) }} /> : plain(desc, 320)}{plain(desc, 400).length > 300 && <button type="button" className="link-btn read-more" onClick={() => setMore(m => !m)}>{more ? 'less' : 'more'}</button>}</div>}
          <div className="row wrap mt-sm">
            <button type="button" className="btn primary" onClick={refresh} disabled={busy || p.refreshing}><Icon name="refresh" size={15} />{busy ? 'Refreshing…' : 'Refresh'}</button>
            <button type="button" className={`btn ${p.favorite ? 'on' : ''}`} onClick={fav} title="Favourites stay on top" aria-pressed={p.favorite}><Icon name={p.favorite ? 'star-fill' : 'star'} size={15} />Favourite</button>
            <button type="button" className="btn" onClick={() => setSettings(true)}><Icon name="settings" size={15} />Settings</button>
            {p.site_url && <a className="btn ghost" href={p.site_url} target="_blank" rel="noreferrer"><Icon name="external" size={15} />Website</a>}
            <MenuButton label="More" className="btn ghost icon-only" width={260} items={[
              { label: 'Mark all as played', icon: 'check-circle', onSelect: () => markAll(true) },
              { label: 'Mark all as unplayed', icon: 'circle', onSelect: () => markAll(false) },
              'sep',
              { label: p.kind === 'youtube' ? 'Download 5 more videos' : 'Download the newest 5', icon: 'download', onSelect: dlMore },
              { label: 'Remove files of played episodes', icon: 'trash', onSelect: () => rmFiles(true) },
              { label: 'Remove all files', icon: 'trash', onSelect: () => rmFiles(false) },
              'sep',
              { label: 'Unsubscribe', icon: 'x-circle', danger: true, onSelect: () => setUnsub(true) },
            ]}><Icon name="more" size={18} /></MenuButton>
          </div>
        </div>
      </div>
      <div className="toolbar mt">
        <label className="search"><Icon name="search" size={15} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search episodes…" aria-label="Search episodes" /></label>
        <Segmented size="sm" value={status} onChange={setStatus} options={[{ value: '', label: 'All' }, { value: 'unfinished', label: 'Unplayed' }, { value: 'partial', label: 'In progress' }, { value: 'played', label: 'Played' }]} />
        <Segmented size="sm" value={sort} onChange={setSort} options={[{ value: '', label: 'Newest' }, { value: 'oldest', label: 'Oldest' }, { value: 'longest', label: 'Longest' }, { value: 'shortest', label: 'Shortest' }]} />
      </div>
      {eps.loading && !eps.data ? <Loading /> : (
        <>
          <EpisodeList episodes={eps.data?.episodes || []} onChange={setEps} showPodcast={false} empty={<Empty title={p.kind === 'youtube' && p.episode_count === 0 ? 'Indexing…' : 'No episodes'}>{p.kind === 'youtube' && p.episode_count === 0 ? 'The channel is being read; this takes a few seconds.' : dq ? 'Nothing matches.' : 'This feed has no episodes yet.'}</Empty>} />
          {eps.data && eps.data.total > LIMIT && (
            <div className="row mt" style={{ justifyContent: 'center' }}>
              <button type="button" className="btn" disabled={page === 0} onClick={() => setPage(p => p - 1)}><Icon name="chevron-left" size={15} />Newer</button>
              <span className="muted small">{page * LIMIT + 1}–{Math.min((page + 1) * LIMIT, eps.data.total)} of {eps.data.total}</span>
              <button type="button" className="btn" disabled={(page + 1) * LIMIT >= eps.data.total} onClick={() => setPage(p => p + 1)}>Older<Icon name="chevron-right" size={15} /></button>
            </div>
          )}
        </>
      )}
      {settings && <PodcastSettings p={p} onClose={() => setSettings(false)} onSaved={np => { setP(np); setSettings(false) }} />}
      {unsub && <Confirm title={`Unsubscribe from ${p.title}?`} message="Its episodes, your progress on them and every downloaded file are deleted." confirmLabel="Unsubscribe" onCancel={() => setUnsub(false)} onConfirm={async () => { await api.del(`${V1}/podcasts/${p.id}`); toast('Unsubscribed'); navigate('/podcasts') }} />}
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
  const picks = p.feed_url.startsWith('pocket:')
  const feedURL = `${settings?.feed_url || ''}${p.id}.xml`
  const set = (k: keyof PodcastT, v: unknown) => setF(x => ({ ...x, [k]: v }))
  const num = (k: keyof PodcastT) => (e: React.ChangeEvent<HTMLInputElement>) => set(k, Number(e.target.value))
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
  const copy = (t: string) => { navigator.clipboard?.writeText(t); toast('Copied') }
  const check = (k: keyof PodcastT, label: string, help?: string) => (
    <div className="setting-row ps-switch"><div className="setting-text"><div className="setting-title">{label}</div>{help && <div className="setting-help">{help}</div>}</div><div className="setting-control"><Switch checked={Boolean(f[k])} onChange={v => set(k, v)} label={label} /></div></div>
  )
  return (
    <Modal title={`Settings — ${p.title}`} onClose={onClose} size="lg">
      <form onSubmit={save} className="ps">
        <section className="ps-section">
          <h3>Show</h3>
          <div className="ps-show">
            <Artwork src={p.artwork} title={p.title} size="md" />
            <div className="ps-fields">
              <Field label="Title"><input value={f.title} onChange={e => set('title', e.target.value)} /></Field>
              <div className="row wrap" style={{ gap: 8 }}>
                <label className="btn sm"><Icon name="upload" size={14} />Upload artwork…<input type="file" accept="image/*" hidden onChange={e => e.target.files?.[0] && uploadArt(e.target.files[0])} /></label>
                {p.artwork_file && !picks && <button type="button" className="btn ghost sm" onClick={resetArt}>Use the feed's artwork</button>}
              </div>
              <div className="ps-inline"><input value={artURL} onChange={e => setArtURL(e.target.value)} placeholder="…or paste an image URL" spellCheck={false} /><button type="button" className="btn sm" onClick={artFromURL} disabled={!artURL}>Use</button></div>
              <span className="muted small">Square, at least 1400×1400 px, for Apple Podcasts.</span>
            </div>
          </div>
        </section>

        <section className="ps-section">
          <h3>Playback</h3>
          <div className="ps-grid">
            <Field label="Speed" help={`0 = your default (${settings?.playback_speed || 1}×)`}><input type="number" step={0.05} min={0} max={3} value={f.playback_speed} onChange={num('playback_speed')} className="num" /></Field>
            <Field label="Skip intro (seconds)"><input type="number" min={0} value={f.start_skip} onChange={num('start_skip')} className="num" /></Field>
            <Field label="Skip outro (seconds)"><input type="number" min={0} value={f.end_skip} onChange={num('end_skip')} className="num" /></Field>
            <Field group label="Play the next queued episode"><Segmented size="sm" value={f.auto_play_next} onChange={v => set('auto_play_next', v)} options={[{ value: -1, label: 'Default' }, { value: 1, label: 'On' }, { value: 0, label: 'Off' }]} /></Field>
          </div>
          {(yt || p.youtube_kind === 'picks') && check('sponsorblock', 'Fetch SponsorBlock segments', 'Sponsor reads and reminders in YouTube videos are skipped while you listen.')}
        </section>

        <section className="ps-section">
          <h3>{picks ? 'Files' : yt ? 'New videos' : 'New episodes'}</h3>
          {!picks && !yt && check('auto_download', 'Download new episodes to the server', 'They play instantly and stay even if the publisher removes them.')}
          {!picks && check('notify', 'Notify me about new episodes', 'Through the channel set in Settings → Notifications.')}
          <div className="ps-grid">
            {yt && !picks && <Field label="Videos fetched when subscribing" help="Afterwards every new upload."><input type="number" min={0} max={500} value={f.download_limit} onChange={num('download_limit')} className="num" /></Field>}
            <Field label="Keep files for (days)" help="0 = until you delete them. Saved and queued episodes stay."><input type="number" min={0} value={f.retention_days} onChange={num('retention_days')} className="num" /></Field>
            {yt && !picks && <Field label="Skip videos shorter than (seconds)" help="Keeps Shorts out. 0 = everything."><input type="number" min={0} value={f.min_duration} onChange={num('min_duration')} className="num" /></Field>}
            {!yt && !picks && <Field label="Ignore episodes older than (days)" help="0 = the whole feed."><input type="number" min={0} value={f.feed_cutoff_days} onChange={num('feed_cutoff_days')} className="num" /></Field>}
            {!picks && <Field label="Check every (minutes)" help={`0 = your default (${settings?.refresh_minutes || settings?.refresh_default || 60} min)`}><input type="number" min={0} value={f.refresh_minutes} onChange={num('refresh_minutes')} className="num" /></Field>}
          </div>
          {yt && !picks && <Field label="Only titles matching" help="A regular expression, case-insensitive. Empty = all videos."><input value={f.title_filter} onChange={e => set('title_filter', e.target.value)} placeholder="e.g. Podcast|Interview" spellCheck={false} /></Field>}
        </section>

        <section className="ps-section">
          <h3>Public feed</h3>
          <p className="muted small" style={{ margin: 0 }}>Subscribe to this in Apple Podcasts, Overcast or any podcast app.</p>
          <div className="ps-inline"><code className="ps-url" title={feedURL}>{feedURL}</code><button type="button" className="btn sm" onClick={() => copy(feedURL)}><Icon name="copy" size={14} />Copy</button></div>
          <Field group label="Episode date in the feed" help="“Downloaded” puts old episodes you add later at the top instead of deep in the list.">
            <Segmented size="sm" value={f.pub_date_mode} onChange={v => set('pub_date_mode', v)} options={[{ value: 'published', label: 'Published' }, { value: 'downloaded', label: 'Downloaded' }]} />
          </Field>
          {!picks && !yt && <Field label="Source feed"><input value={f.feed_url} onChange={e => set('feed_url', e.target.value)} spellCheck={false} /></Field>}
          {!picks && yt && <p className="muted small" style={{ margin: 0 }}>Source: <a href={p.feed_url} target="_blank" rel="noreferrer">{p.feed_url}</a></p>}
        </section>

        <div className="ps-foot"><span className="muted small">{picks ? 'Your hand-picked episodes' : `Subscribed ${fmtDate(p.subscribed_at)}`}</span><span className="spacer" /><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="submit" className="btn primary" disabled={busy}>Save</button></div>
      </form>
    </Modal>
  )
}
