import { useEffect } from 'react'
import { api, V1 } from '../lib/api'
import type { Episode } from '../lib/types'
import { Card, Empty, Loading, PageHeader, useAsync } from '../components/ui'
import { EpisodeList } from '../components/EpisodeRow'
import { bytes } from '../lib/format'

export default function Downloads() {
  const { data, setData, loading, reload } = useAsync(() => api.get<{ pending: Episode[]; done: Episode[]; bytes: number; yt_dlp: string }>(`${V1}/downloads`), [])
  useEffect(() => {
    if (!data || data.pending.every(e => e.download === 'failed')) return
    const t = setInterval(reload, 4000)
    return () => clearInterval(t)
  }, [data, reload])
  if (loading && !data) return <Loading />
  if (!data) return null
  return (
    <>
      <PageHeader title="Downloads" sub={`${data.done.length} file${data.done.length === 1 ? '' : 's'} on your server · ${bytes(data.bytes)}${data.yt_dlp ? ` · yt-dlp ${data.yt_dlp}` : ' · yt-dlp not available'}`} />
      {data.pending.length > 0 && (
        <Card title={`In progress (${data.pending.length})`} className="mb">
          <EpisodeList episodes={data.pending} onChange={eps => setData({ ...data, pending: eps })} compact />
        </Card>
      )}
      <h2 className="mb-sm">On your server</h2>
      <EpisodeList episodes={data.done} onChange={eps => setData({ ...data, done: eps.filter(e => e.download === 'done') })} compact empty={<Empty title="Nothing downloaded">Episodes you download, auto-downloads and YouTube videos show up here.</Empty>} />
    </>
  )
}
