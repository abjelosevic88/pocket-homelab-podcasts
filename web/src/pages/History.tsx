import { useState } from 'react'
import { api, V1 } from '../lib/api'
import type { Episode } from '../lib/types'
import { Confirm, Empty, Loading, PageHeader, useAsync } from '../components/ui'
import { EpisodeList } from '../components/EpisodeRow'
import { Icon } from '../components/icons'
import { useLive } from '../lib/realtime'

export default function History() {
  const [page, setPage] = useState(0)
  const LIMIT = 50
  const { data, setData, loading, reload } = useAsync(() => api.get<{ episodes: Episode[]; total: number }>(`${V1}/history?limit=${LIMIT}&offset=${page * LIMIT}`), [page])
  // something played on another device moves to the top (positions update in place meanwhile)
  useLive(ev => ev.type === 'history' || ev.type === 'progress' || (ev.type === 'episodes' && ev.removed.length > 0) || (ev.type === 'podcasts' && ev.removed.length > 0), reload, { minGap: 10_000 })
  const [clear, setClear] = useState(false)
  if (loading && !data) return <Loading />
  return (
    <>
      <PageHeader title="History" sub={data ? `${data.total} episode${data.total === 1 ? '' : 's'} played` : undefined} actions={data && data.total > 0 && <button type="button" className="btn" onClick={() => setClear(true)}><Icon name="trash" size={15} />Clear history</button>} />
      <EpisodeList episodes={data?.episodes || []} onChange={eps => setData(d => d ? { ...d, episodes: eps } : d)} compact empty={<Empty title="Nothing played yet" icon="history" />} />
      {data && data.total > LIMIT && (
        <div className="row mt" style={{ justifyContent: 'center' }}>
          <button type="button" className="btn" disabled={page === 0} onClick={() => setPage(p => p - 1)}><Icon name="chevron-left" size={15} />Newer</button>
          <span className="muted small">{page * LIMIT + 1}–{Math.min((page + 1) * LIMIT, data.total)} of {data.total}</span>
          <button type="button" className="btn" disabled={(page + 1) * LIMIT >= data.total} onClick={() => setPage(p => p + 1)}>Older<Icon name="chevron-right" size={15} /></button>
        </div>
      )}
      {clear && <Confirm title="Clear your history?" message="Positions and play counts are forgotten; downloads and subscriptions stay." confirmLabel="Clear history" onCancel={() => setClear(false)} onConfirm={async () => { await api.del(`${V1}/history`); setClear(false); setData({ episodes: [], total: 0 }) }} />}
    </>
  )
}
