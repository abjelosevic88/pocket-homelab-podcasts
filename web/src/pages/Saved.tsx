import { api, V1 } from '../lib/api'
import type { Episode } from '../lib/types'
import { Empty, Loading, PageHeader, useAsync } from '../components/ui'
import { EpisodeList } from '../components/EpisodeRow'

export default function Saved() {
  const { data, setData, loading } = useAsync(() => api.get<Episode[]>(`${V1}/saved`), [])
  if (loading && !data) return <Loading />
  return (
    <>
      <PageHeader title="Saved" sub="Episodes you keep. Their files are never removed by retention." />
      <EpisodeList episodes={(data || []).filter(e => e.saved)} onChange={setData} empty={<Empty title="Nothing saved">♡ on an episode keeps it here.</Empty>} />
    </>
  )
}
