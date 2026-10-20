import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Podcast } from '../lib/types'
import { Empty, Loading, PageHeader, Segmented, useAsync, useDebounce } from '../components/ui'
import { PodcastCard } from '../components/PodcastCard'
import { Artwork } from '../components/Artwork'
import { ago, bytes } from '../lib/format'

export default function Podcasts() {
  const [q, setQ] = useState('')
  const [sort, setSort] = useState<string>(localStorage.getItem('pp-pod-sort') || 'title')
  const [view, setView] = useState<'grid' | 'list'>((localStorage.getItem('pp-pod-view') as 'grid' | 'list') || 'grid')
  const [kind, setKind] = useState('')
  const dq = useDebounce(q)
  const { data, loading } = useAsync(() => api.get<Podcast[]>(`${V1}/podcasts?q=${encodeURIComponent(dq)}&sort=${sort}&kind=${kind}`), [dq, sort, kind])
  const pick = (s: string) => { setSort(s); localStorage.setItem('pp-pod-sort', s) }
  const pickView = (v: 'grid' | 'list') => { setView(v); localStorage.setItem('pp-pod-view', v) }
  return (
    <>
      <PageHeader title="Podcasts" sub={data ? `${data.length} subscription${data.length === 1 ? '' : 's'}` : undefined} actions={<Link className="btn primary" to="/add">+ Add</Link>} />
      <div className="toolbar">
        <label className="search"><span className="ico">⌕</span><input value={q} onChange={e => setQ(e.target.value)} placeholder="Filter podcasts…" /></label>
        <Segmented size="sm" value={kind} onChange={setKind} options={[{ value: '', label: 'All' }, { value: 'rss', label: 'Podcasts' }, { value: 'youtube', label: 'YouTube' }]} />
        <Segmented size="sm" value={sort} onChange={pick} options={[{ value: 'title', label: 'A–Z' }, { value: 'recent', label: 'Newest episode' }, { value: 'unplayed', label: 'Unplayed' }, { value: 'added', label: 'Added' }]} />
        <Segmented size="sm" value={view} onChange={pickView} options={[{ value: 'grid', label: '▦' }, { value: 'list', label: '☰' }]} />
      </div>
      {loading && !data ? <Loading /> : !data || data.length === 0 ? (
        <Empty title={dq ? 'Nothing matches' : 'No podcasts yet'}>{!dq && <>Search a directory, paste a feed URL or a YouTube channel under <Link to="/add">Add</Link>.</>}</Empty>
      ) : view === 'grid' ? (
        <div className="pod-grid">{data.map(p => <PodcastCard key={p.id} p={p} />)}</div>
      ) : (
        <div className="pod-list">
          {data.map(p => (
            <Link key={p.id} to={`/podcasts/${p.id}`} className="pod-row">
              <Artwork src={p.artwork} title={p.title} size="sm" />
              <div className="info">
                <div className="nm">{p.favorite && <span className="fav-inline">★</span>}{p.title}{p.kind === 'youtube' && <span className="badge" style={{ marginLeft: 6 }}>YouTube</span>}</div>
                <div className="meta"><span>{p.author}</span>{p.latest_pub_date && <span>latest {ago(p.latest_pub_date)}</span>}{p.downloaded_count > 0 && <span>{p.downloaded_count} on disk · {bytes(p.downloaded_bytes)}</span>}{p.last_error && <span className="bad">⚠ {p.last_error}</span>}</div>
              </div>
              <div className="side">{p.unplayed_count > 0 ? <span className="count-pill inline">{p.unplayed_count} new</span> : <span className="muted small">{p.episode_count} episodes</span>}</div>
            </Link>
          ))}
        </div>
      )}
    </>
  )
}
