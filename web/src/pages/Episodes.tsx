import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Episode } from '../lib/types'
import { Empty, Loading, PageHeader, Segmented, useAsync, useDebounce } from '../components/ui'
import { EpisodeList } from '../components/EpisodeRow'

/** Every episode across subscriptions, newest first, with filters. */
export default function Episodes() {
  const [sp, setSp] = useSearchParams()
  const status = sp.get('status') || ''
  const kind = sp.get('kind') || ''
  const days = sp.get('days') || ''
  const downloaded = sp.get('downloaded') || ''
  const [q, setQ] = useState(sp.get('q') || '')
  const dq = useDebounce(q)
  const [page, setPage] = useState(0)
  const LIMIT = 50
  const set = (k: string, v: string) => { const n = new URLSearchParams(sp); if (v) n.set(k, v); else n.delete(k); setSp(n, { replace: true }); setPage(0) }
  const url = `${V1}/episodes?status=${status}&kind=${kind}&days=${days}&downloaded=${downloaded}&q=${encodeURIComponent(dq)}&limit=${LIMIT}&offset=${page * LIMIT}`
  const { data, setData, loading } = useAsync(() => api.get<{ episodes: Episode[]; total: number }>(url), [url])
  useEffect(() => { setPage(0) }, [dq])
  const setEps = useCallback((eps: Episode[]) => setData(d => d ? { ...d, episodes: eps } : d), [setData])
  return (
    <>
      <PageHeader title="Episodes" sub={data ? `${data.total} episode${data.total === 1 ? '' : 's'}` : undefined} />
      <div className="toolbar">
        <label className="search"><span className="ico">⌕</span><input value={q} onChange={e => setQ(e.target.value)} placeholder="Search titles and notes…" /></label>
        <Segmented size="sm" value={status} onChange={v => set('status', v)} options={[{ value: '', label: 'All' }, { value: 'unplayed', label: 'Unplayed' }, { value: 'partial', label: 'In progress' }, { value: 'played', label: 'Played' }]} />
        <Segmented size="sm" value={days} onChange={v => set('days', v)} options={[{ value: '', label: 'Any time' }, { value: '7', label: 'Week' }, { value: '30', label: 'Month' }]} />
        <Segmented size="sm" value={kind} onChange={v => set('kind', v)} options={[{ value: '', label: 'All sources' }, { value: 'rss', label: 'Podcasts' }, { value: 'youtube', label: 'YouTube' }]} />
        <Segmented size="sm" value={downloaded} onChange={v => set('downloaded', v)} options={[{ value: '', label: 'Any' }, { value: '1', label: 'On disk' }]} />
      </div>
      {loading && !data ? <Loading /> : (
        <>
          <EpisodeList episodes={data?.episodes || []} onChange={setEps} empty={<Empty title="No episodes">{dq ? 'Nothing matches.' : 'Nothing here with these filters.'}</Empty>} />
          {data && data.total > LIMIT && (
            <div className="row mt" style={{ justifyContent: 'center' }}>
              <button className="btn" disabled={page === 0} onClick={() => setPage(p => p - 1)}>← Newer</button>
              <span className="muted small">{page * LIMIT + 1}–{Math.min((page + 1) * LIMIT, data.total)} of {data.total}</span>
              <button className="btn" disabled={(page + 1) * LIMIT >= data.total} onClick={() => setPage(p => p + 1)}>Older →</button>
            </div>
          )}
        </>
      )}
    </>
  )
}
