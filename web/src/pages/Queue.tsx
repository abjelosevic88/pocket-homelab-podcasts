import { useEffect, useState } from 'react'
import { api, V1 } from '../lib/api'
import type { Episode } from '../lib/types'
import { Confirm, Empty, Loading, PageHeader, useAsync, useToast } from '../components/ui'
import { EpisodeRow } from '../components/EpisodeRow'
import { fmtDur } from '../lib/format'
import { Icon } from '../components/icons'
import { useLive } from '../lib/realtime'

export default function Queue() {
  const { toast } = useToast()
  const { data, setData, loading, reload } = useAsync(() => api.get<Episode[]>(`${V1}/queue`), [])
  const [clear, setClear] = useState(false)
  const [drag, setDrag] = useState<number | null>(null)
  useEffect(() => { const h = () => reload(); window.addEventListener('pp:queue', h); return () => window.removeEventListener('pp:queue', h) }, [reload])
  useLive(ev => ev.type === 'queue' || (ev.type === 'episodes' && ev.removed.length > 0) || (ev.type === 'podcasts' && ev.removed.length > 0) || ev.type === 'download', reload)
  if (loading && !data) return <Loading />
  const eps = data || []
  const total = eps.reduce((a, e) => a + Math.max(0, e.duration - (e.completed ? e.duration : e.play_position)), 0)
  const move = async (from: number, to: number) => {
    if (from === to || to < 0 || to >= eps.length) return
    const l = [...eps]; const [x] = l.splice(from, 1); l.splice(to, 0, x)
    setData(l.map((e, i) => ({ ...e, queue_pos: i + 1 })))
    try { setData(await api.put<Episode[]>(`${V1}/queue`, { ids: l.map(e => e.id) })) } catch (e) { toast((e as Error).message, 'err'); reload() }
  }
  return (
    <>
      <PageHeader title="Up next" sub={eps.length ? `${eps.length} episode${eps.length === 1 ? '' : 's'} · ${fmtDur(total)} left` : 'Nothing queued'} actions={eps.length > 0 && <button type="button" className="btn" onClick={() => setClear(true)}><Icon name="trash" size={15} />Clear</button>} />
      {eps.length === 0 ? <Empty title="The queue is empty" icon="queue">Use + on any episode. When an episode ends, the next one here plays.</Empty> : (
        <div className="ep-list">
          {eps.map((e, i) => (
            <div key={e.id} draggable onDragStart={() => setDrag(i)} onDragOver={ev => ev.preventDefault()} onDrop={() => { if (drag !== null) move(drag, i); setDrag(null) }} className={drag === i ? 'dragging' : ''}>
              <EpisodeRow ep={e} compact onChange={x => x ? (x.queue_pos ? setData(eps.map(o => o.id === x.id ? x : o)) : setData(eps.filter(o => o.id !== e.id))) : setData(eps.filter(o => o.id !== e.id))}
                dragHandle={<span className="order"><button type="button" className="btn ghost icon-only xs" onClick={() => move(i, i - 1)} disabled={i === 0} aria-label="Move up" title="Move up"><Icon name="chevron-up" size={14} /></button><button type="button" className="btn ghost icon-only xs" onClick={() => move(i, i + 1)} disabled={i === eps.length - 1} aria-label="Move down" title="Move down"><Icon name="chevron-down" size={14} /></button></span>} />
            </div>
          ))}
        </div>
      )}
      {clear && <Confirm title="Clear the queue?" message="Episodes stay in your library; only the order is forgotten." confirmLabel="Clear" onCancel={() => setClear(false)} onConfirm={async () => { await api.del(`${V1}/queue`); setClear(false); setData([]) }} />}
    </>
  )
}
