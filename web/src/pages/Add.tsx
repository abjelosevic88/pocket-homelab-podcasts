import { useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Podcast, Preview, PreviewEpisode, SearchResult } from '../lib/types'
import { safeHTML } from '../components/Player'
import { Card, Empty, Field, Loading, PageHeader, Segmented, Tabs, useAsync, useDebounce, useToast } from '../components/ui'
import { Artwork } from '../components/Artwork'
import { fmtDate, fmtDur } from '../lib/format'

type Tab = 'search' | 'url' | 'youtube' | 'trending'

export default function Add() {
  const [sp] = useSearchParams()
  const [tab, setTab] = useState<Tab>((sp.get('tab') as Tab) || 'search')
  // links like /add?tab=url&url=… land on this same page: follow the query string
  useEffect(() => { const t = sp.get('tab') as Tab | null; if (t) setTab(t) }, [sp])
  const { settings } = useApp()
  const tabs: { id: Tab; label: string }[] = [{ id: 'search', label: 'Search' }, { id: 'url', label: 'Feed URL' }, { id: 'youtube', label: 'YouTube' }]
  if (settings?.podcastindex_key || settings?.podcastindex_env) tabs.push({ id: 'trending', label: 'Trending' })
  return (
    <>
      <PageHeader title="Add" sub="Follow a podcast from a directory, by feed URL, or turn a YouTube channel into a podcast." />
      <Tabs tabs={tabs} value={tab} onChange={setTab} />
      <div className="mt">
        {tab === 'search' && <Search />}
        {tab === 'url' && <ByURL kind="rss" />}
        {tab === 'youtube' && <ByURL kind="youtube" />}
        {tab === 'trending' && <Trending />}
      </div>
    </>
  )
}

function Search() {
  const { settings } = useApp()
  const [q, setQ] = useState('')
  const [source, setSource] = useState<string>(settings?.search_source || 'all')
  const dq = useDebounce(q, 400)
  const { data, loading } = useAsync(async () => dq.trim().length < 2 ? null : api.get<{ results: SearchResult[]; warnings: string[] | null }>(`${V1}/search?q=${encodeURIComponent(dq)}&source=${source}`), [dq, source])
  const hasPI = settings?.podcastindex_key || settings?.podcastindex_env
  return (
    <>
      <div className="search-bar mb"><span className="ico">⌕</span><input value={q} onChange={e => setQ(e.target.value)} placeholder="Podcast name, host, topic…" autoFocus />{loading && <span className="spinner" />}</div>
      <div className="row mb"><Segmented size="sm" value={source} onChange={setSource} options={[{ value: 'all', label: 'All' }, { value: 'itunes', label: 'Apple' }, ...(hasPI ? [{ value: 'podcastindex', label: 'Podcast Index' }] : [])]} />{!hasPI && <span className="muted small">Add a free Podcast Index key under Settings → Search for a second directory.</span>}</div>
      {data?.warnings?.map(w => <div key={w} className="callout danger small mb">{w}</div>)}
      {data && data.results.length === 0 && <Empty title="Nothing found">Try fewer words, or paste the feed URL.</Empty>}
      {data && <Results results={data.results} />}
    </>
  )
}

function Trending() {
  const { data, loading, error } = useAsync(() => api.get<{ results: SearchResult[] }>(`${V1}/search/trending`), [])
  if (loading) return <Loading />
  if (error) return <div className="callout danger">{error}</div>
  return <Results results={data?.results || []} />
}

function Results({ results }: { results: SearchResult[] }) {
  const navigate = useNavigate()
  const { toast } = useToast()
  const [busy, setBusy] = useState('')
  const subscribe = async (r: SearchResult) => {
    setBusy(r.feed_url)
    try { const p = await api.post<Podcast>(`${V1}/podcasts`, { url: r.feed_url, title: r.title }); toast(`Subscribed to ${p.title}`); navigate(`/podcasts/${p.id}`) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy('') }
  }
  return (
    <ul className="results">
      {results.map(r => (
        <li key={r.feed_url} className="result">
          <Artwork src={r.image} title={r.title} size="sm" />
          <div className="info">
            <div className="nm">{r.title}</div>
            <div className="meta">{r.author}{r.episodes > 0 && ` · ${r.episodes} episodes`}{r.last_update && ` · ${fmtDate(r.last_update)}`}{r.categories?.length > 0 && ` · ${r.categories.slice(0, 2).join(', ')}`}</div>
            {r.description && <div className="muted small clamp2">{r.description}</div>}
          </div>
          <div className="row" style={{ gap: 6, flexShrink: 0 }}>
            <Link className="btn sm" to={`/add?tab=url&url=${encodeURIComponent(r.feed_url)}`} title="See the episodes and take just one">Episodes</Link>
            {r.subscribed_id ? <Link className="btn sm" to={`/podcasts/${r.subscribed_id}`}>✓ Following</Link> : <button className="btn primary sm" onClick={() => subscribe(r)} disabled={busy === r.feed_url}>{busy === r.feed_url ? 'Adding…' : '+ Follow'}</button>}
          </div>
        </li>
      ))}
    </ul>
  )
}

function ByURL({ kind }: { kind: 'rss' | 'youtube' }) {
  const navigate = useNavigate()
  const { toast } = useToast()
  const { settings } = useApp()
  const [sp] = useSearchParams()
  const [url, setUrl] = useState(sp.get('url') || '')
  const [preview, setPreview] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  const [picked, setPicked] = useState<Record<string, 'busy' | 'done'>>({})
    const [opts, setOpts] = useState({ download_limit: settings?.youtube_limit ?? 3, retention_days: settings?.retention_days ?? 0, min_duration: settings?.youtube_min_duration ?? 120, title_filter: '', auto_download: settings?.auto_download_default ?? false, notify: false, pub_date_mode: 'published' })
  useEffect(() => { setPreview(null); setPicked({}) }, [url])
  const lookup = async (u: string) => {
    setBusy(true)
    try { setPreview(await api.post<Preview>(`${V1}/preview`, { url: u })) } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  const look = async (e: React.FormEvent) => { e.preventDefault(); lookup(url) }
  const spURL = sp.get('url') || ''
  useEffect(() => { if (spURL) { setUrl(spURL); lookup(spURL) } }, [spURL]) // eslint-disable-line react-hooks/exhaustive-deps
  const pick = async (guid: string, title: string) => {
    if (!preview) return
    setPicked(p => ({ ...p, [guid]: 'busy' }))
    try { const ep = guid.startsWith('yt:') ? await api.post<{ id: number }>(`${V1}/youtube/video`, { url: `https://www.youtube.com/watch?v=${guid.slice(3)}` }) : await api.post<{ id: number }>(`${V1}/picks`, { feed_url: preview.url, guid, title }); setPicked(p => ({ ...p, [guid]: 'done' })); toast(`“${title}” is in your Picks and downloading`); if (!preview.subscribed_id) toast('Find it under Podcasts → Picks', 'ok'); void ep } catch (err) { setPicked(p => { const n = { ...p }; delete n[guid]; return n }); toast((err as Error).message, 'err') }
  }
  const subscribe = async () => {
    if (!preview) return
    setBusy(true)
    try {
      if (preview.kind === 'video') { const ep = await api.post<{ id: number; title: string }>(`${V1}/youtube/video`, { url: preview.url }); toast(`Downloading “${ep.title}”`); navigate(`/episodes/${ep.id}`); return }
      const p = await api.post<Podcast>(`${V1}/podcasts`, { url: preview.url, ...opts, title_filter: opts.title_filter || undefined })
      toast(`Subscribed to ${p.title}`); navigate(`/podcasts/${p.id}`)
    } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  const yt = kind === 'youtube'
  return (
    <div className="two-col">
      <div className="grid" style={{ gap: 16 }}>
        <Card title={yt ? 'YouTube channel, playlist or video' : 'Feed URL'}>
          <form onSubmit={look} className="grid" style={{ gap: 12 }}>
            <p className="muted">{yt ? 'Paste a channel (@handle or /channel/UC…), a playlist (also unlisted) or a single video. Audio is downloaded on this box with yt-dlp; the newest uploads keep coming.' : 'Paste the RSS feed address of a podcast. Episodes stream from the publisher unless you download them.'}{!yt && ' A YouTube URL works here too.'}</p>
            <div className="row"><input value={url} onChange={e => setUrl(e.target.value)} placeholder={yt ? 'https://www.youtube.com/@Fireship' : 'https://example.com/feed.xml'} spellCheck={false} autoFocus /><button className="btn primary" disabled={busy || !url.trim()}>{busy ? 'Looking…' : 'Look up'}</button></div>
            {yt && !settings?.yt_dlp && <div className="callout danger small">yt-dlp is not available on this box, so YouTube cannot be added. See Settings → YouTube.</div>}
          </form>
        </Card>
        {preview && (preview.kind === 'youtube' || preview.kind === 'rss') && (
          <Card title="Options">
            <div className="grid" style={{ gap: 12 }}>
              {preview.kind === 'youtube' ? <>
                <Field label="Videos to fetch now" help="The newest N. Every upload from now on is fetched automatically."><input type="number" min={0} max={200} value={opts.download_limit} onChange={e => setOpts({ ...opts, download_limit: Number(e.target.value) })} className="num" /></Field>
                <Field label="Skip videos shorter than (s)" help="Keeps Shorts and clips out. 0 = everything."><input type="number" min={0} value={opts.min_duration} onChange={e => setOpts({ ...opts, min_duration: Number(e.target.value) })} className="num" /></Field>
                <Field label="Only titles matching" help="Regular expression, optional."><input value={opts.title_filter} onChange={e => setOpts({ ...opts, title_filter: e.target.value })} spellCheck={false} /></Field>
              </> : <label className="row"><input type="checkbox" checked={opts.auto_download} onChange={e => setOpts({ ...opts, auto_download: e.target.checked })} style={{ width: 'auto' }} /> Download new episodes automatically</label>}
              <Field label="Keep files for (days)" help="0 = until you delete them. Saved and queued episodes always stay."><input type="number" min={0} value={opts.retention_days} onChange={e => setOpts({ ...opts, retention_days: Number(e.target.value) })} className="num" /></Field>
              <label className="row"><input type="checkbox" checked={opts.notify} onChange={e => setOpts({ ...opts, notify: e.target.checked })} style={{ width: 'auto' }} /> Notify me about new episodes</label>
            </div>
          </Card>
        )}
      </div>
      <div>
        {preview ? (
          <Card title={preview.kind === 'video' ? 'Video' : preview.kind === 'youtube' ? `YouTube ${preview.youtube_kind}` : 'Podcast'}>
            <div className="pick-head">
              <Artwork src={preview.image} title={preview.title} size="md" />
              <div className="info">
                <h3>{preview.title}</h3>
                {preview.author && <div className="authors">{preview.author}</div>}
                {preview.description && <div className="desc">{preview.description.slice(0, 500)}</div>}
                {preview.kind === 'video' && preview.duration ? <div className="muted small mt-sm">{fmtDur(preview.duration)}</div> : null}
              </div>
            </div>
            {preview.episodes && preview.episodes.length > 0 && (
              <PreviewEpisodes preview={preview} picked={picked} onPick={pick} />
            )}
            <div className="form-actions mt">
              {preview.subscribed_id ? <Link className="btn" to={`/podcasts/${preview.subscribed_id}`}>✓ Already following — open</Link> : <button className="btn primary lg" onClick={subscribe} disabled={busy}>{busy ? 'Adding…' : preview.kind === 'video' ? '↓ Download as episode' : '+ Subscribe'}</button>}
            </div>
          </Card>
        ) : (
          <Card title="Tips">
            <ul className="muted" style={{ paddingLeft: 18, margin: 0, display: 'grid', gap: 6 }}>
              {yt ? <>
                <li>A channel subscription fetches the newest videos now and every upload after that.</li>
                <li>Unlisted playlists work: make a “Watch later” playlist, paste its link, and whatever you add appears here as audio.</li>
                <li>Single videos go into your “Picks” show — no need to follow the channel.</li>
                <li>SponsorBlock segments are fetched with each video and skipped while you listen.</li>
                <li>Every subscription gets a public RSS feed (Settings → Feeds) for Apple Podcasts, Overcast or AntennaPod.</li>
              </> : <>
                <li>Most podcast pages link their RSS feed; Apple Podcasts links can be searched by name on the Search tab.</li>
                <li>Want one episode only (a guest you like, one talk)? Look the feed up and press <b>↓ Get</b> next to it: it lands in your “Picks” show, downloaded, without following the whole podcast. Search results have an <b>Episodes</b> button for the same.</li>
                <li>Private feeds with a token in the URL work as they are.</li>
                <li>Import a whole OPML file under Settings → Backup & data.</li>
              </>}
            </ul>
          </Card>
        )}
      </div>
    </div>
  )
}

/** Episodes of a looked-up show: thumbnail rows that expand in place with
 *  the notes, a listen sample and "Get" (one episode into Picks). */
function PreviewEpisodes({ preview, picked, onPick }: { preview: Preview; picked: Record<string, 'busy' | 'done'>; onPick: (guid: string, title: string) => void }) {
  const [open, setOpen] = useState<string | null>(null)
  const [showAll, setShowAll] = useState(false)
  const [q, setQ] = useState('')
  const eps = preview.episodes || []
  const list = eps.filter(e => !q || e.title.toLowerCase().includes(q.toLowerCase())).slice(0, showAll || q ? eps.length : 8)
  const yt = preview.kind === 'youtube'
  const dateOf = (d: string) => !d ? '' : d.length === 8 ? fmtDate(`${d.slice(0, 4)}-${d.slice(4, 6)}-${d.slice(6)}`) : fmtDate(d)
  const key = (e: PreviewEpisode, i: number) => e.guid || String(i)
  const get = (e: PreviewEpisode) => {
    if (!e.guid || e.has_audio === false) return null
    if (picked[e.guid] === 'done') return <span className="badge ok-badge">✓ in Picks</span>
    return <button className="btn sm" onClick={ev => { ev.stopPropagation(); onPick(e.guid!, e.title) }} disabled={picked[e.guid] === 'busy'} title="Download just this one into your Picks">{picked[e.guid] === 'busy' ? '…' : '↓ Get'}</button>
  }
  return (
    <div className="pv mt">
      <div className="pv-head"><b>Episodes</b><span className="muted small">take one without following the show</span><span className="spacer" />{eps.length > 8 && <input value={q} onChange={e => setQ(e.target.value)} placeholder="Filter…" className="pv-filter" />}</div>
      <ul className="pv-list">
        {list.map((e, i) => {
          const k = key(e, i), isOpen = open === k
          const meta = [dateOf(e.date), e.duration ? fmtDur(e.duration) : '', e.season ? `S${e.season}${e.episode ? ` E${e.episode}` : ''}` : e.episode ? `#${e.episode}` : ''].filter(Boolean).join(' · ')
          return (
            <li key={k} className={isOpen ? 'open' : ''}>
              <div className="pv-row" role="button" tabIndex={0} aria-expanded={isOpen} onClick={() => setOpen(isOpen ? null : k)} onKeyDown={ev => { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); setOpen(isOpen ? null : k) } }}>
                <Artwork src={e.image || preview.image} title={e.title} size="xs" />
                <span className="pv-info"><span className="pv-title" title={e.title}>{e.title}</span><span className="muted small">{meta}</span></span>
                {get(e)}
                <span className={`pv-chev ${isOpen ? 'up' : ''}`} aria-hidden>⌄</span>
              </div>
              {isOpen && (
                <div className="pv-detail">
                  <div className="pv-detail-top">
                    <Artwork src={e.image || preview.image} title={e.title} size="md" />
                    <div className="pv-detail-head">
                      {e.link && <a className="small" href={e.link} target="_blank" rel="noreferrer">{yt ? 'Watch on YouTube ↗' : 'Open the episode page ↗'}</a>}
                      {!yt && e.audio && <audio className="pv-audio" controls preload="none" src={e.audio} />}
                      {!yt && e.audio && <span className="muted small">Listen to a bit before you get it — streams from the publisher.</span>}
                    </div>
                  </div>
                  {e.description ? <div className="md pv-notes" dangerouslySetInnerHTML={{ __html: safeHTML(e.description) }} /> : <p className="muted small" style={{ margin: 0 }}>{yt ? 'The video description arrives when you get it.' : 'No show notes.'}</p>}
                </div>
              )}
            </li>
          )
        })}
      </ul>
      {!showAll && !q && eps.length > 8 && <button className="btn ghost sm" onClick={() => setShowAll(true)}>Show {eps.length - 8} more{preview.episode_count && preview.episode_count > eps.length ? ` (of ${preview.episode_count})` : ''}</button>}
    </div>
  )
}
