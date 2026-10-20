import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Episode, Podcast } from '../lib/types'
import { Empty, Loading, PageHeader, Segmented, useAsync, useDebounce } from '../components/ui'
import { EpisodeList } from '../components/EpisodeRow'
import { Select } from '../components/Select'

const SORTS = [
  { value: '', label: 'Newest first' },
  { value: 'oldest', label: 'Oldest first' },
  { value: 'longest', label: 'Longest first' },
  { value: 'shortest', label: 'Shortest first' },
]
const STATUSES = [
  { value: '', label: 'All' },
  { value: 'unplayed', label: 'Unplayed' },
  { value: 'partial', label: 'In progress' },
  { value: 'played', label: 'Played' },
]
const SortIcon = () => <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden><path d="M5 3v10M5 13l-2.5-2.5M5 13l2.5-2.5M11 13V3M11 3L8.5 5.5M11 3l2.5 2.5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" /></svg>
const FilterIcon = () => <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden><path d="M2.5 3.5h11L9.2 8.6v3.6l-2.4 1.3V8.6z" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" /></svg>
const DAYS: Record<string, string> = { '7': 'Last 7 days', '30': 'Last 30 days' }
const KINDS: Record<string, string> = { rss: 'Podcasts only', youtube: 'YouTube only' }

/** Pops open under its button and closes on a click elsewhere or Escape. */
function usePopover() {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const click = (e: MouseEvent) => { if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false) }
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', click)
    document.addEventListener('keydown', key)
    return () => { document.removeEventListener('mousedown', click); document.removeEventListener('keydown', key) }
  }, [open])
  return { open, setOpen, ref }
}

/** Every episode across subscriptions with search, status tabs, sort and a filter panel. */
export default function Episodes() {
  const [sp, setSp] = useSearchParams()
  const status = sp.get('status') || ''
  const kind = sp.get('kind') || ''
  const days = sp.get('days') || ''
  const downloaded = sp.get('downloaded') || ''
  const sort = sp.get('sort') || ''
  const podcast = sp.get('podcast') || ''
  const [q, setQ] = useState(sp.get('q') || '')
  const dq = useDebounce(q)
  const [page, setPage] = useState(0)
  const LIMIT = 50
  const sortPop = usePopover()
  const filterPop = usePopover()
  const pods = useAsync(() => api.get<Podcast[]>(`${V1}/podcasts?sort=title`), [])
  const set = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n, { replace: true }); setPage(0) }
  const clearFilters = () => { const n = new URLSearchParams(sp); ['kind', 'days', 'downloaded', 'podcast'].forEach(k => n.delete(k)); setSp(n, { replace: true }); setPage(0) }
  const url = `${V1}/episodes?status=${status}&kind=${kind}&days=${days}&downloaded=${downloaded}&sort=${sort}&podcast=${podcast}&q=${encodeURIComponent(dq)}&limit=${LIMIT}&offset=${page * LIMIT}`
  const { data, setData, loading } = useAsync(() => api.get<{ episodes: Episode[]; total: number }>(url), [url])
  useEffect(() => { setPage(0) }, [dq])
  const setEps = useCallback((eps: Episode[]) => setData(d => d ? { ...d, episodes: eps } : d), [setData])

  const podName = (pods.data || []).find(p => String(p.id) === podcast)?.title || 'One podcast'
  const chips = [
    podcast && { key: 'podcast', label: podName },
    days && { key: 'days', label: DAYS[days] || `Last ${days} days` },
    kind && { key: 'kind', label: KINDS[kind] || kind },
    downloaded && { key: 'downloaded', label: downloaded === '1' ? 'Downloaded' : 'Not downloaded' },
  ].filter(Boolean) as { key: string; label: string }[]
  const sortLabel = SORTS.find(s => s.value === sort)?.label || SORTS[0].label

  return (
    <>
      <PageHeader title="Episodes" sub={data ? `${data.total.toLocaleString()} episode${data.total === 1 ? '' : 's'}${chips.length || status || dq ? ' match' : ''}` : undefined} />
      <div className="epf">
        <div className="epf-top">
          <label className="epf-search"><span className="ico">⌕</span><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search titles and show notes…" />{q && <button className="epf-x" onClick={() => setQ('')} aria-label="Clear search">✕</button>}</label>
          <div className="menu-wrap" ref={sortPop.ref}>
            <button className="btn epf-btn" onClick={() => sortPop.setOpen(o => !o)} aria-haspopup="menu" aria-expanded={sortPop.open}><SortIcon /> {sortLabel}</button>
            {sortPop.open && (
              <div className="menu" role="menu">
                {SORTS.map(s => <button key={s.value} role="menuitemradio" aria-checked={s.value === sort} className={s.value === sort ? 'cur' : ''} onClick={() => { set('sort', s.value); sortPop.setOpen(false) }}>{s.value === sort ? '✓ ' : ''}{s.label}</button>)}
              </div>
            )}
          </div>
          <div className="menu-wrap" ref={filterPop.ref}>
            <button className={`btn epf-btn ${chips.length ? 'on' : ''}`} onClick={() => filterPop.setOpen(o => !o)} aria-haspopup="dialog" aria-expanded={filterPop.open}>
              <FilterIcon /> Filters{chips.length > 0 && <span className="epf-count">{chips.length}</span>}
            </button>
            {filterPop.open && (
              <div className="epf-panel" role="dialog" aria-label="Filters">
                <div className="epf-group">
                  <span className="label">Podcast</span>
                  <Select value={podcast} onChange={v => set('podcast', v)} placeholder="All podcasts" clearLabel="All podcasts"
                    options={(pods.data || []).map(p => ({ value: String(p.id), label: p.title, hint: p.kind === 'youtube' ? 'YouTube' : undefined }))} />
                </div>
                <div className="epf-group">
                  <span className="label">Published</span>
                  <Segmented size="sm" value={days} onChange={v => set('days', v)} options={[{ value: '', label: 'Any time' }, { value: '7', label: 'Last week' }, { value: '30', label: 'Last month' }]} />
                </div>
                <div className="epf-group">
                  <span className="label">Source</span>
                  <Segmented size="sm" value={kind} onChange={v => set('kind', v)} options={[{ value: '', label: 'All' }, { value: 'rss', label: 'Podcasts' }, { value: 'youtube', label: 'YouTube' }]} />
                </div>
                <div className="epf-group">
                  <span className="label">Download</span>
                  <Segmented size="sm" value={downloaded} onChange={v => set('downloaded', v)} options={[{ value: '', label: 'Any' }, { value: '1', label: 'Downloaded' }, { value: '0', label: 'Not downloaded' }]} />
                </div>
                <div className="epf-panel-foot">
                  <button className="btn ghost sm" onClick={clearFilters} disabled={!chips.length}>Clear filters</button>
                  <button className="btn primary sm" onClick={() => filterPop.setOpen(false)}>Done</button>
                </div>
              </div>
            )}
          </div>
        </div>
        <div className="epf-tabs" role="tablist">
          {STATUSES.map(s => <button key={s.value} role="tab" aria-selected={s.value === status} className={s.value === status ? 'active' : ''} onClick={() => set('status', s.value)}>{s.label}</button>)}
        </div>
        {chips.length > 0 && (
          <div className="epf-chips">
            {chips.map(c => <button key={c.key} className="epf-chip" onClick={() => set(c.key, '')} title="Remove this filter">{c.label} <span aria-hidden>✕</span></button>)}
            <button className="btn ghost sm" onClick={clearFilters}>Clear all</button>
          </div>
        )}
      </div>
      {loading && !data ? <Loading /> : (
        <>
          <EpisodeList episodes={data?.episodes || []} onChange={setEps} empty={<Empty title="No episodes">{dq ? 'Nothing matches your search.' : chips.length || status ? 'Nothing here with these filters.' : 'Subscribe to something under Add.'}</Empty>} />
          {data && data.total > LIMIT && (
            <div className="row mt" style={{ justifyContent: 'center' }}>
              <button className="btn" disabled={page === 0} onClick={() => setPage(p => p - 1)}>← Previous</button>
              <span className="muted small">{page * LIMIT + 1}–{Math.min((page + 1) * LIMIT, data.total)} of {data.total}</span>
              <button className="btn" disabled={(page + 1) * LIMIT >= data.total} onClick={() => setPage(p => p + 1)}>Next →</button>
            </div>
          )}
        </>
      )}
    </>
  )
}
