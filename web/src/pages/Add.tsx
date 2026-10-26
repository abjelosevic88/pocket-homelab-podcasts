import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Podcast, Preview, PreviewEpisode, SearchResult, VideoHave, YTChannel, YTItem } from '../lib/types'
import { safeHTML } from '../components/Player'
import { searchYouTube, YT_KINDS, type YTHit, type YTKind } from '../lib/youtube'
import { Card, Empty, Field, Loading, MenuButton, Modal, PageHeader, Segmented, SettingRow, Switch, Tabs, useAsync, useDebounce, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Artwork } from '../components/Artwork'
import { clock, compact, fmtDate, fmtDur } from '../lib/format'

type Tab = 'search' | 'url' | 'youtube' | 'trending'

export default function Add() {
  const [sp] = useSearchParams()
  const [tab, setTab] = useState<Tab>((sp.get('tab') as Tab) || 'search')
  // links like /add?tab=url&url=… land on this same page: follow the query string
  useEffect(() => { const t = sp.get('tab') as Tab | null; if (t) setTab(t) }, [sp])
  const { settings } = useApp()
  const tabs: { id: Tab; label: string; icon: string }[] = [{ id: 'search', label: 'Search', icon: 'search' }, { id: 'url', label: 'Feed URL', icon: 'rss' }, { id: 'youtube', label: 'YouTube', icon: 'youtube' }]
  if (settings?.podcastindex_key || settings?.podcastindex_env) tabs.push({ id: 'trending', label: 'Trending', icon: 'activity' })
  return (
    <>
      <PageHeader title="Add" sub="Follow a podcast from a directory, by feed URL, or turn a YouTube channel into a podcast." />
      <Tabs tabs={tabs} value={tab} onChange={setTab} />
      <div className="mt">
        {tab === 'search' && <Search />}
        {tab === 'url' && <ByURL kind="rss" />}
        {tab === 'youtube' && <><YouTubeSearch /><ByURL kind="youtube" /></>}
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
      <div className="search-bar mb"><Icon name="search" size={17} /><input value={q} onChange={e => setQ(e.target.value)} placeholder="Podcast name, host, topic…" aria-label="Search podcasts" autoFocus />{loading && <span className="spinner" />}</div>
      <div className="row mb"><Segmented size="sm" value={source} onChange={setSource} options={[{ value: 'all', label: 'All' }, { value: 'itunes', label: 'Apple' }, ...(hasPI ? [{ value: 'podcastindex', label: 'Podcast Index' }] : [])]} />{!hasPI && <span className="muted small">Add a free Podcast Index key under Settings → Search for a second directory.</span>}</div>
      {data?.warnings?.map(w => <div key={w} className="callout warn small mb"><Icon name="alert" size={15} /><span>{w}</span></div>)}
      {data && data.results.length === 0 && <Empty title="Nothing found" icon="search">Try fewer words, or paste the feed URL.</Empty>}
      {data && <Results results={data.results} />}
      {dq.trim().length >= 2 && settings?.yt_dlp && <YouTubeMatches q={dq} />}
    </>
  )
}

function Trending() {
  const { data, loading, error } = useAsync(() => api.get<{ results: SearchResult[] }>(`${V1}/search/trending`), [])
  if (loading) return <Loading />
  if (error) return <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div>
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
            {r.subscribed_id ? <Link className="btn sm" to={`/podcasts/${r.subscribed_id}`}><Icon name="check" size={14} />Following</Link> : <button type="button" className="btn primary sm" onClick={() => subscribe(r)} disabled={busy === r.feed_url}>{busy === r.feed_url ? 'Adding…' : <><Icon name="plus" size={14} />Follow</>}</button>}
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
  const top = useRef<HTMLDivElement>(null)
  useEffect(() => { if (spURL) { setUrl(spURL); lookup(spURL); setTimeout(() => top.current?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 50) } }, [spURL]) // eslint-disable-line react-hooks/exhaustive-deps
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
    <div className="two-col" ref={top} style={{ scrollMarginTop: 16 }}>
      <div className="grid" style={{ gap: 16 }}>
        <Card title={yt ? 'YouTube channel, playlist or video' : 'Feed URL'}>
          <form onSubmit={look} className="grid" style={{ gap: 12 }}>
            <p className="muted">{yt ? 'Paste a channel (@handle or /channel/UC…), a playlist (also unlisted) or a single video. Audio is downloaded on this box with yt-dlp; the newest uploads keep coming.' : 'Paste the RSS feed address of a podcast. Episodes stream from the publisher unless you download them.'}{!yt && ' A YouTube URL works here too.'}</p>
            <div className="row"><input value={url} onChange={e => setUrl(e.target.value)} placeholder={yt ? 'https://www.youtube.com/@Fireship' : 'https://example.com/feed.xml'} spellCheck={false} autoFocus aria-label={yt ? 'YouTube URL' : 'Feed URL'} /><button type="submit" className="btn primary" disabled={busy || !url.trim()}>{busy ? 'Looking…' : 'Look up'}</button></div>
            {yt && !settings?.yt_dlp && <div className="callout danger small"><Icon name="alert" size={15} /><span>yt-dlp is not available on this box, so YouTube cannot be added. See Settings → Feeds & downloads.</span></div>}
          </form>
        </Card>
        {preview && (preview.kind === 'youtube' || preview.kind === 'rss') && (
          <Card title="Options">
            <div className="grid" style={{ gap: 12 }}>
              {preview.kind === 'youtube' ? <>
                <Field label="Videos to fetch now" help="The newest N. Every upload from now on is fetched automatically."><input type="number" min={0} max={200} value={opts.download_limit} onChange={e => setOpts({ ...opts, download_limit: Number(e.target.value) })} className="num" /></Field>
                <Field label="Skip videos shorter than (s)" help="Keeps Shorts and clips out. 0 = everything."><input type="number" min={0} value={opts.min_duration} onChange={e => setOpts({ ...opts, min_duration: Number(e.target.value) })} className="num" /></Field>
                <Field label="Only titles matching" help="Regular expression, optional."><input value={opts.title_filter} onChange={e => setOpts({ ...opts, title_filter: e.target.value })} spellCheck={false} /></Field>
              </> : <SettingRow title="Download new episodes automatically"><Switch checked={opts.auto_download} onChange={v => setOpts({ ...opts, auto_download: v })} label="Download new episodes automatically" /></SettingRow>}
              <Field label="Keep files for (days)" help="0 = until you delete them. Saved and queued episodes always stay."><input type="number" min={0} value={opts.retention_days} onChange={e => setOpts({ ...opts, retention_days: Number(e.target.value) })} className="num" /></Field>
              <SettingRow title="Notify me about new episodes"><Switch checked={opts.notify} onChange={v => setOpts({ ...opts, notify: v })} label="Notify me about new episodes" /></SettingRow>
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
              {preview.subscribed_id ? <Link className="btn" to={`/podcasts/${preview.subscribed_id}`}><Icon name="check" size={15} />Already following — open</Link> : <button type="button" className="btn primary lg" onClick={subscribe} disabled={busy}>{busy ? 'Adding…' : preview.kind === 'video' ? <><Icon name="download" size={16} />Download as episode</> : <><Icon name="plus" size={16} />Subscribe</>}</button>}
            </div>
          </Card>
        ) : (
          <Card title="Tips">
            <ul className="tips">
              {yt ? <>
                <li>A channel subscription fetches the newest videos now and every upload after that.</li>
                <li>Unlisted playlists work: make a “Watch later” playlist, paste its link, and whatever you add appears here as audio.</li>
                <li>Single videos go into your “Picks” show — no need to follow the channel.</li>
                <li>SponsorBlock segments are fetched with each video and skipped while you listen.</li>
                <li>Every subscription gets a public RSS feed (Settings → Feeds) for Apple Podcasts, Overcast or AntennaPod.</li>
              </> : <>
                <li>Most podcast pages link their RSS feed; Apple Podcasts links can be searched by name on the Search tab.</li>
                <li>Want one episode only (a guest you like, one talk)? Look the feed up and press <b>Get</b> next to it: it lands in your “Picks” show, downloaded, without following the whole podcast. Search results have an <b>Episodes</b> button for the same.</li>
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
interface InfiniteList { more: boolean; loading: boolean; error: string; onMore: () => void }

/** Where a person is with a video they already have: watched, part-played,
 *  downloaded (the file may have been cleaned up since) or just listed. */
function HaveChip({ have, always = false }: { have: VideoHave; always?: boolean }) {
  const pct = have.duration && have.position ? Math.min(99, Math.round(have.position / have.duration * 100)) : 0
  if (have.watched) return <span className="badge ok have-chip" title={have.downloaded ? 'Downloaded and watched' : 'Watched'}><Icon name="check" size={12} />{have.downloaded ? 'Downloaded · watched' : 'Watched'}</span>
  if (have.position) return <span className="badge have-chip started" title={have.downloaded ? 'Downloaded, part played' : 'Part played'}>{have.downloaded ? 'Downloaded · ' : ''}{pct ? `${pct}% played` : 'Started'}</span>
  if (have.downloaded) return <span className="badge have-chip dl" title={have.on_server ? 'The file is on your server' : 'Downloaded before; the file has been cleaned up since'}><Icon name="download" size={12} />Downloaded{have.on_server ? '' : ' · file removed'}</span>
  return always ? <span className="badge have-chip" title="In your library, not downloaded">In library</span> : null
}

/** YouTube-style progress line along the bottom of a thumbnail. */
function WatchBar({ have }: { have: VideoHave }) {
  const pct = have.watched ? 100 : have.duration && have.position ? Math.min(100, have.position / have.duration * 100) : 0
  return pct > 0 ? <span className="yt-watchbar" aria-hidden><i style={{ width: `${pct}%` }} /></span> : null
}

function PreviewEpisodes({ preview, picked, onPick, infinite }: { preview: Preview; picked: Record<string, 'busy' | 'done'>; onPick: (guid: string, title: string) => void; infinite?: InfiniteList }) {
  const sentinel = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!infinite || !infinite.more || infinite.loading || infinite.error || !sentinel.current) return
    const io = new IntersectionObserver(es => { if (es.some(e => e.isIntersecting)) infinite.onMore() }, { rootMargin: '400px 0px' })
    io.observe(sentinel.current)
    return () => io.disconnect()
  }, [infinite])
  const [open, setOpen] = useState<string | null>(null)
  const [showAll, setShowAll] = useState(false)
  const [q, setQ] = useState('')
  const eps = preview.episodes || []
  const list = eps.filter(e => !q || e.title.toLowerCase().includes(q.toLowerCase())).slice(0, infinite || showAll || q ? eps.length : 8)
  const yt = preview.kind === 'youtube'
  const dateOf = (d: string) => !d ? '' : d.length === 8 ? fmtDate(`${d.slice(0, 4)}-${d.slice(4, 6)}-${d.slice(6)}`) : fmtDate(d)
  const key = (e: PreviewEpisode, i: number) => e.guid || String(i)
  const get = (e: PreviewEpisode) => {
    if (e.have) return <Link to={`/episodes/${e.have.episode_id}`} onClick={ev => ev.stopPropagation()} title="Open it in your library"><HaveChip have={e.have} always /></Link>
    if (!e.guid || e.has_audio === false) return null
    if (picked[e.guid] === 'done') return <span className="badge ok">in Picks</span>
    return <button type="button" className="btn sm" onClick={ev => { ev.stopPropagation(); onPick(e.guid!, e.title) }} disabled={picked[e.guid] === 'busy'} title="Download just this one into your Picks">{picked[e.guid] === 'busy' ? '…' : <><Icon name="download" size={14} />Get</>}</button>
  }
  return (
    <div className="pv mt">
      <div className="pv-head"><b>{yt ? 'Videos' : 'Episodes'}</b><span className="muted small pv-hint">take one without following the {yt ? 'channel' : 'show'}</span><span className="spacer" />{eps.length > 8 && <input value={q} onChange={e => setQ(e.target.value)} placeholder="Filter…" className="pv-filter" />}</div>
      <ul className="pv-list">
        {list.map((e, i) => {
          const k = key(e, i), isOpen = open === k
          const meta = [dateOf(e.date), e.duration ? fmtDur(e.duration) : '', e.season ? `S${e.season}${e.episode ? ` E${e.episode}` : ''}` : e.episode ? `#${e.episode}` : ''].filter(Boolean).join(' · ')
          return (
            <li key={k} className={isOpen ? 'open' : ''}>
              <div className={`pv-row ${e.have?.watched ? 'watched' : ''}`} role="button" tabIndex={0} aria-expanded={isOpen} onClick={() => setOpen(isOpen ? null : k)} onKeyDown={ev => { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); setOpen(isOpen ? null : k) } }}>
                <Artwork src={e.image || preview.image} title={e.title} size="xs" />
                <span className="pv-info"><span className="pv-title" title={e.title}>{e.title}</span><span className="muted small">{meta}</span></span>
                {get(e)}
                <span className={`pv-chev ${isOpen ? 'up' : ''}`} aria-hidden><Icon name="chevron-down" size={16} /></span>
              </div>
              {isOpen && (
                <div className="pv-detail">
                  <div className="pv-detail-top">
                    <Artwork src={e.image || preview.image} title={e.title} size="md" />
                    <div className="pv-detail-head">
                      {e.link && <a className="small row" style={{ gap: 5 }} href={e.link} target="_blank" rel="noreferrer"><Icon name="external" size={13} />{yt ? 'Watch on YouTube' : 'Open the episode page'}</a>}
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
      {!infinite && !showAll && !q && eps.length > 8 && <button type="button" className="btn ghost sm" onClick={() => setShowAll(true)}>Show {eps.length - 8} more{preview.episode_count && preview.episode_count > eps.length ? ` (of ${preview.episode_count})` : ''}</button>}
      {infinite && !q && (
        <div ref={sentinel} className="pv-more">
          {infinite.error ? <><span className="bad small">{infinite.error}</span> <button type="button" className="btn ghost sm" onClick={infinite.onMore}>Try again</button></>
            : infinite.loading ? <><span className="spinner sm" /> Loading older videos…</>
            : infinite.more ? <button type="button" className="btn ghost sm" onClick={infinite.onMore}>Load more</button>
            : <span className="muted small">That's all {eps.length} videos.</span>}
        </div>
      )}
    </div>
  )
}

/** One search box for YouTube: everything by default, a type filter in a dropdown. */
function YouTubeSearch() {
  const [sp, setSp] = useSearchParams()
  const { settings } = useApp()
  const kind = (YT_KINDS.some(k => k.value === sp.get('yt')) ? sp.get('yt') : 'all') as YTKind
  const [q, setQ] = useState(sp.get('q') || '')
  const dq = useDebounce(q, 450)
  const { data, loading } = useAsync(async () => dq.trim().length < 2 ? null : searchYouTube(dq.trim(), kind), [dq, kind])
  const setKind = (k: YTKind) => { const n = new URLSearchParams(sp); if (k === 'all') n.delete('yt'); else n.set('yt', k); n.delete('url'); setSp(n, { replace: true }) }
  if (!settings?.yt_dlp) return null
  const current = YT_KINDS.find(k => k.value === kind)!
  return (
    <div className="mb">
      <div className="yts-top">
        <div className="search-bar grow"><Icon name="search" size={17} /><input aria-label="Search YouTube" value={q} onChange={e => setQ(e.target.value)} placeholder={kind === 'all' ? 'Search YouTube…' : `Search YouTube ${current.label.toLowerCase()}…`} autoFocus={!sp.get('url')} />{loading && dq.trim().length >= 2 && <span className="spinner" />}</div>
        <MenuButton label={`Show: ${current.label}`} className={`btn yts-kind ${kind !== 'all' ? 'on' : ''}`} align="right" width={180} items={YT_KINDS.map(k => ({ label: k.label, checked: k.value === kind, onSelect: () => setKind(k.value) }))}>{current.label}<Icon name="chevron-down" size={14} className="muted" /></MenuButton>
      </div>
      {data && data.length === 0 && <p className="muted">Nothing found.</p>}
      {data && data.length > 0 && <YouTubeResults hits={data} />}
      {!data && <p className="muted small">Find channels, videos and playlists. Subscribe to a channel or playlist and new uploads download automatically; <b>Get</b> takes a single video as audio into your Picks. Or paste a link below.</p>}
    </div>
  )
}

const YT_LABEL = { channel: 'Channel', video: 'Video', playlist: 'Playlist' }

/** Mixed YouTube results; every row says what it is and offers the matching action. */
function YouTubeResults({ hits, compactList = false }: { hits: YTHit[]; compactList?: boolean }) {
  const { toast } = useToast()
  const [done, setDone] = useState<Record<string, number | 'busy'>>({})
  const [peek, setPeek] = useState<{ key: string; url: string; title: string } | null>(null)
  const run = async (key: string, fn: () => Promise<{ id: number }>, ok: (id: number) => string) => {
    setDone(d => ({ ...d, [key]: 'busy' }))
    try { const r = await fn(); setDone(d => ({ ...d, [key]: r.id })); toast(ok(r.id)) }
    catch (e) { setDone(d => { const n = { ...d }; delete n[key]; return n }); toast((e as Error).message, 'err') }
  }
  return (
    <ul className={`yt-videos ${compactList ? 'compact' : ''}`}>
      {hits.map(h => {
        const state = done[h.key]
        if (h.kind === 'channel') {
          const c = h.c, sub = state ?? c.subscribed_id
          return (
            <li key={h.key} className="yt-video">
              <a className="yt-thumb channel" href={c.url} target="_blank" rel="noreferrer" title="Open on YouTube"><img src={c.thumbnail} alt="" loading="lazy" /></a>
              <div className="info">
                <div className="yt-kind-row"><span className="yt-badge channel">Channel</span></div>
                <div className="nm">{c.title}</div>
                <div className="meta">{[c.handle, c.followers ? `${compact(c.followers)} subscribers` : ''].filter(Boolean).join(' · ')}</div>
                {!compactList && c.description && <div className="muted small clamp2">{c.description}</div>}
              </div>
              <div className="row yt-actions">
                <button type="button" className="btn sm" onClick={() => setPeek({ key: h.key, url: c.url, title: c.title })} title="See the latest videos first">Videos</button>
                {typeof sub === 'number'
                  ? <Link className="btn sm" to={`/podcasts/${sub}`}><Icon name="check" size={14} />Following</Link>
                  : <button type="button" className="btn primary sm" disabled={sub === 'busy'} title="New uploads will download automatically"
                      onClick={() => run(h.key, () => api.post<Podcast>(`${V1}/podcasts`, { url: c.url }), () => `Subscribed to ${c.title} — the newest videos are downloading`)}>{sub === 'busy' ? 'Subscribing…' : <><Icon name="plus" size={14} />Subscribe</>}</button>}
              </div>
            </li>
          )
        }
        const v = h.v
        const isVideo = h.kind === 'video'
        const have = state ?? (isVideo ? v.episode_id : v.subscribed_id)
        return (
          <li key={h.key} className={`yt-video ${isVideo && v.watched ? 'watched' : ''}`}>
            <a className="yt-thumb" href={v.url} target="_blank" rel="noreferrer" title="Open on YouTube">
              <img src={v.thumbnail} alt="" loading="lazy" />
              {isVideo && v.episode_id ? <WatchBar have={{ episode_id: v.episode_id, watched: v.watched, position: v.position, duration: v.duration }} /> : null}
              {isVideo ? (v.duration ? <span className="yt-dur">{clock(v.duration)}</span> : v.live ? <span className="yt-dur live">LIVE</span> : null) : <span className="yt-dur"><Icon name="list" size={12} /></span>}
            </a>
            <div className="info">
              <div className="yt-kind-row"><span className={`yt-badge ${h.kind}`}>{YT_LABEL[h.kind]}</span>{isVideo && v.episode_id ? <HaveChip have={{ episode_id: v.episode_id, downloaded: v.downloaded, on_server: v.on_server, watched: v.watched, position: v.position, duration: v.duration }} /> : null}</div>
              <div className="nm" title={v.title}>{v.title}</div>
              <div className="meta">{[v.channel, v.views ? `${compact(v.views)} views` : '', isVideo && v.duration ? fmtDur(v.duration) : ''].filter(Boolean).join(' · ')}</div>
              {!compactList && isVideo && v.description && <div className="muted small clamp2">{v.description}</div>}
            </div>
            <div className="row yt-actions">
              {isVideo
                ? (typeof have === 'number'
                    ? <Link className="btn sm" to={`/episodes/${have}`}><Icon name="check" size={14} />{v.watched || v.downloaded || v.position ? 'Open' : 'In library'}</Link>
                    : <button type="button" className="btn primary sm" disabled={have === 'busy' || v.live} title="Download as audio into your Picks"
                        onClick={() => run(h.key, () => api.post<{ id: number }>(`${V1}/youtube/video`, { url: v.url }), () => `“${v.title}” is downloading into your Picks`)}>{have === 'busy' ? 'Adding…' : <><Icon name="download" size={14} />Get</>}</button>)
                : <>
                    <button type="button" className="btn sm" onClick={() => setPeek({ key: h.key, url: v.url, title: v.title })} title="See its videos first">Videos</button>
                    {typeof have === 'number'
                      ? <Link className="btn sm" to={`/podcasts/${have}`}><Icon name="check" size={14} />Following</Link>
                      : <button type="button" className="btn primary sm" disabled={have === 'busy'}
                          onClick={() => run(h.key, () => api.post<Podcast>(`${V1}/podcasts`, { url: v.url }), () => `Subscribed to ${v.title} — the newest videos are downloading`)}>{have === 'busy' ? 'Subscribing…' : <><Icon name="plus" size={14} />Subscribe</>}</button>}
                  </>}
            </div>
          </li>
        )
      })}
      {peek && <SourcePeek url={peek.url} title={peek.title} subscribedID={typeof done[peek.key] === 'number' ? done[peek.key] as number : (() => { const h = hits.find(x => x.key === peek.key); return h ? (h.kind === 'channel' ? h.c.subscribed_id : h.v.subscribed_id) : undefined })()}
        onSubscribed={id => setDone(d => ({ ...d, [peek.key]: id }))} onClose={() => setPeek(null)} />}
    </ul>
  )
}

/** A channel's or playlist's latest videos in a window over the search, so
 *  the results stay where they were: subscribe, or take single videos. */
export function SourcePeek({ url, title, subscribedID, onSubscribed, onClose }: { url: string; title: string; subscribedID?: number; onSubscribed: (id: number) => void; onClose: () => void }) {
  const { toast } = useToast()
  const { data: preview, loading, error } = useAsync(() => api.post<Preview>(`${V1}/preview`, { url }), [url])
  const [picked, setPicked] = useState<Record<string, 'busy' | 'done'>>({})
  const [extra, setExtra] = useState<PreviewEpisode[]>([])
  const [page, setPage] = useState<{ next: number; more: boolean; loading: boolean; error: string } | null>(null)
  useEffect(() => { if (preview) { setExtra([]); setPage({ next: preview.next ?? 30, more: preview.more ?? false, loading: false, error: '' }) } }, [preview])
  const loadMore = useCallback(async () => {
    if (!page || page.loading || !page.more) return
    setPage(p => p && { ...p, loading: true, error: '' })
    try {
      const r = await api.get<{ episodes: PreviewEpisode[]; more: boolean; next: number }>(`${V1}/youtube/listing?url=${encodeURIComponent(url)}&offset=${page.next}&limit=30`)
      setExtra(x => { const seen = new Set([...(preview?.episodes || []), ...x].map(e => e.guid)); return [...x, ...r.episodes.filter(e => !seen.has(e.guid))] })
      setPage({ next: r.next, more: r.more, loading: false, error: '' })
    } catch (e) { setPage(p => p && { ...p, loading: false, error: (e as Error).message }) }
  }, [page, url, preview])
  const all = preview ? { ...preview, episodes: [...(preview.episodes || []), ...extra] } : null
  const [sub, setSub] = useState<number | 'busy' | undefined>(subscribedID)
  const pick = async (guid: string, t: string) => {
    setPicked(p => ({ ...p, [guid]: 'busy' }))
    try {
      if (guid.startsWith('yt:')) await api.post(`${V1}/youtube/video`, { url: `https://www.youtube.com/watch?v=${guid.slice(3)}` })
      else await api.post(`${V1}/picks`, { feed_url: url, guid, title: t })
      setPicked(p => ({ ...p, [guid]: 'done' })); toast(`“${t}” is downloading into your Picks`)
    } catch (e) { setPicked(p => { const n = { ...p }; delete n[guid]; return n }); toast((e as Error).message, 'err') }
  }
  const subscribe = async () => {
    setSub('busy')
    try { const p = await api.post<Podcast>(`${V1}/podcasts`, { url }); setSub(p.id); onSubscribed(p.id); toast(`Subscribed to ${p.title} — the newest videos are downloading`) }
    catch (e) { setSub(undefined); toast((e as Error).message, 'err') }
  }
  return (
    <Modal title={preview?.title || title} onClose={onClose} size="lg">
      {loading && !preview ? <Loading /> : error ? <div className="callout danger"><Icon name="alert" size={15} /><span>{error}</span></div> : preview && (
        <div className="grid" style={{ gap: 14 }}>
          <div className="peek-head">
            <Artwork src={preview.image} title={preview.title} size="md" className={preview.youtube_kind === 'channel' ? 'round' : ''} />
            <div className="grid grow" style={{ gap: 4, minWidth: 0 }}>
              <span className={`yt-badge ${preview.youtube_kind === 'playlist' ? 'playlist' : 'channel'}`} style={{ justifySelf: 'start' }}>{preview.youtube_kind === 'playlist' ? 'Playlist' : 'Channel'}</span>
              <b className="peek-title">{preview.title}</b>
              {preview.author && preview.author !== preview.title && <span className="muted small">{preview.author}</span>}
              {preview.description && <span className="muted small clamp2">{preview.description}</span>}
            </div>
            <div className="row" style={{ gap: 6, flexShrink: 0, alignSelf: 'center' }}>
              <a className="btn sm ghost" href={url} target="_blank" rel="noreferrer"><Icon name="external" size={14} />YouTube</a>
              {typeof sub === 'number'
                ? <Link className="btn sm" to={`/podcasts/${sub}`} onClick={onClose}><Icon name="check" size={14} />Following</Link>
                : <button type="button" className="btn primary sm" onClick={subscribe} disabled={sub === 'busy'} title="New uploads will download automatically">{sub === 'busy' ? 'Subscribing…' : <><Icon name="plus" size={14} />Subscribe</>}</button>}
            </div>
          </div>
          {all && all.episodes.length > 0
            ? <PreviewEpisodes preview={all} picked={picked} onPick={pick} infinite={page ? { more: page.more, loading: page.loading, error: page.error, onMore: loadMore } : undefined} />
            : <p className="muted">No videos found.</p>}
        </div>
      )}
    </Modal>
  )
}

/** The best YouTube matches, mixed, under the podcast search results. */
function YouTubeMatches({ q }: { q: string }) {
  const { data, loading } = useAsync(() => searchYouTube(q, 'all', true), [q])
  if (loading && !data) return <p className="muted small mt"><span className="spinner sm" /> Looking on YouTube…</p>
  if (!data || data.length === 0) return null
  return (
    <section className="mt">
      <h2 className="yt-head"><span className="yt-dot"><Icon name="play" size={9} /></span> On YouTube <Link className="see-all" to={`/add?tab=youtube&q=${encodeURIComponent(q)}`}>More<Icon name="chevron-right" size={14} /></Link></h2>
      <YouTubeResults hits={data} compactList />
    </section>
  )
}
