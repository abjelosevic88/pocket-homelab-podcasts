import { api, V1 } from '../lib/api'
import type { Episode } from '../lib/types'
import { Empty, Loading, PageHeader, useAsync } from '../components/ui'
import { EpisodeList } from '../components/EpisodeRow'
import { useLive } from '../lib/realtime'

export default function Saved() {
  const { data, setData, loading, reload } = useAsync(() => api.get<Episode[]>(`${V1}/saved`), [])
  useLive(ev => ev.type === 'saved' || (ev.type === 'episodes' && ev.removed.length > 0) || (ev.type === 'podcasts' && ev.removed.length > 0) || ev.type === 'download', reload)
  if (loading && !data) return <Loading />
  return (
    <>
      <PageHeader title="Saved" sub="Episodes you keep. Their files are never removed by retention." />
      <EpisodeList episodes={(data || []).filter(e => e.saved)} onChange={setData} empty={<Empty title="Nothing saved" icon="heart">“Save for later” in an episode's menu keeps it here.</Empty>} />
    </>
  )
}
