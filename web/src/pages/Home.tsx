import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Episode, Home as HomeT } from '../lib/types'
import { Card, Empty, Loading, PageHeader, useAsync, useToast } from '../components/ui'
import { EpisodeList } from '../components/EpisodeRow'
import { PodcastCard } from '../components/PodcastCard'

export default function Home() {
  const { user } = useApp()
  const { toast } = useToast()
  const { data, setData, loading, reload } = useAsync(() => api.get<HomeT>(`${V1}/home`), [])
  const [refreshing, setRefreshing] = useState(false)
  useEffect(() => { const h = () => reload(); window.addEventListener('pp:queue', h); return () => window.removeEventListener('pp:queue', h) }, [reload])
  const setList = useCallback((key: 'continue' | 'queue' | 'fresh' | 'recent') => (eps: Episode[]) => setData(d => d ? { ...d, [key]: eps } : d), [setData])
  const refreshAll = async () => {
    setRefreshing(true)
    try { await api.post(`${V1}/refresh`); toast('Refreshing all feeds in the background'); setTimeout(reload, 4000) } catch (e) { toast((e as Error).message, 'err') } finally { setTimeout(() => setRefreshing(false), 3000) }
  }
  if (loading && !data) return <Loading />
  if (!data) return <Empty title="Could not load" />
  const d = data.dashboard
  const hour = new Date().getHours()
  const greet = hour < 5 ? 'Still up' : hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening'
  const first = (user?.name || '').split(' ')[0]
  return (
    <>
      <PageHeader title={`${greet}${first ? ', ' + first : ''}`} sub={d.podcasts === 0 ? 'Follow a podcast or a YouTube channel to get started.' : `${d.unplayed} unplayed · ${d.queued} queued · ${d.listened_today_min} min today`}
        actions={<><button className="btn" onClick={refreshAll} disabled={refreshing}>{refreshing ? 'Refreshing…' : '↻ Refresh'}</button><Link className="btn primary" to="/add">+ Add</Link></>} />
      {d.podcasts === 0 && (
        <Card title="Welcome">
          <p>Pocket Podcasts keeps your subscriptions, remembers where you stopped, downloads episodes to this box and turns YouTube channels into audio podcasts.</p>
          <div className="row wrap mt-sm"><Link className="btn primary" to="/add">Find a podcast</Link><Link className="btn" to="/settings?tab=data">Import OPML</Link></div>
        </Card>
      )}
      {data.continue.length > 0 && (
        <section className="home-section">
          <h2>Continue listening</h2>
          <EpisodeList episodes={data.continue} onChange={setList('continue')} compact />
        </section>
      )}
      {data.queue.length > 0 && (
        <section className="home-section">
          <h2>Up next <Link to="/queue" className="muted small">all {d.queued} →</Link></h2>
          <EpisodeList episodes={data.queue} onChange={setList('queue')} compact />
        </section>
      )}
      {data.podcasts.length > 0 && (
        <section className="home-section">
          <h2>Podcasts <Link to="/podcasts" className="muted small">all {d.podcasts} →</Link></h2>
          <div className="pod-strip">{data.podcasts.map(p => <PodcastCard key={p.id} p={p} />)}</div>
        </section>
      )}
      {data.fresh.length > 0 && (
        <section className="home-section">
          <h2>New episodes <Link to="/episodes?status=unplayed" className="muted small">all →</Link></h2>
          <EpisodeList episodes={data.fresh} onChange={setList('fresh')} />
        </section>
      )}
      {data.recent.length > 0 && (
        <section className="home-section">
          <h2>Recently played <Link to="/history" className="muted small">history →</Link></h2>
          <EpisodeList episodes={data.recent} onChange={setList('recent')} compact />
        </section>
      )}
    </>
  )
}
