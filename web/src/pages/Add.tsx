import { useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Podcast, Preview, SearchResult } from '../lib/types'
import { Card, Empty, Field, Loading, PageHeader, Segmented, Tabs, useAsync, useDebounce, useToast } from '../components/ui'
import { Artwork } from '../components/Artwork'
import { fmtDate, fmtDur } from '../lib/format'

type Tab = 'search' | 'url' | 'youtube' | 'trending'

export default function Add() {
  const [sp] = useSearchParams()
  const [tab, setTab] = useState<Tab>((sp.get('tab') as Tab) || 'search')
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
          {r.subscribed_id ? <Link className="btn sm" to={`/podcasts/${r.subscribed_id}`}>✓ Following</Link> : <button className="btn primary sm" onClick={() => subscribe(r)} disabled={busy === r.feed_url}>{busy === r.feed_url ? 'Adding…' : '+ Follow'}</button>}
        </li>
      ))}
    </ul>
  )
}

function ByURL({ kind }: { kind: 'rss' | 'youtube' }) {
  const navigate = useNavigate()
  const { toast } = useToast()
  const { settings } = useApp()
  const [url, setUrl] = useState('')
  const [preview, setPreview] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  const [opts, setOpts] = useState({ download_limit: settings?.youtube_limit ?? 3, retention_days: settings?.retention_days ?? 0, min_duration: settings?.youtube_min_duration ?? 120, title_filter: '', auto_download: settings?.auto_download_default ?? false, notify: false, pub_date_mode: 'published' })
  useEffect(() => { setPreview(null) }, [url])
  const look = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setPreview(await api.post<Preview>(`${V1}/preview`, { url })) } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
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
              <ul className="mini-list mt">
                {preview.episodes.map((e, i) => <li key={i}><span className="grow">{e.title}</span><span className="muted small">{e.duration ? fmtDur(e.duration) : ''}{e.date && e.date.length >= 8 ? ` · ${e.date.length === 8 ? e.date.slice(0, 4) + '-' + e.date.slice(4, 6) + '-' + e.date.slice(6) : fmtDate(e.date)}` : ''}</span></li>)}
                {preview.episode_count && preview.episode_count > preview.episodes.length && <li className="muted small">… and {preview.episode_count - preview.episodes.length} more</li>}
              </ul>
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
                <li>Single videos go into a “YouTube picks” podcast.</li>
                <li>SponsorBlock segments are fetched with each video and skipped while you listen.</li>
                <li>Every subscription gets a public RSS feed (Settings → Feeds) for Apple Podcasts, Overcast or AntennaPod.</li>
              </> : <>
                <li>Most podcast pages link their RSS feed; Apple Podcasts links can be searched by name on the Search tab.</li>
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
