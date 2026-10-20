import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { usePlayer } from '../lib/player'
import type { Episode, Playlist as PlaylistT } from '../lib/types'
import { Empty, Loading, PageHeader, useAsync, useToast } from '../components/ui'
import { EpisodeList } from '../components/EpisodeRow'
import { PlaylistEditor, describe } from './Playlists'
import { fmtDur } from '../lib/format'

export default function PlaylistPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { toast } = useToast()
  const player = usePlayer()
  const { data, setData, loading, reload } = useAsync(() => api.get<{ playlist: PlaylistT; episodes: Episode[]; total: number }>(`${V1}/playlists/${id}/episodes?limit=200`), [id])
  const [edit, setEdit] = useState(false)
  if (loading && !data) return <Loading />
  if (!data) return <Empty title="Playlist not found" />
  const { playlist: p, episodes } = data
  const total = episodes.reduce((a, e) => a + Math.max(0, e.duration - e.play_position), 0)
  const playAll = () => { if (episodes[0]) player.play(episodes[0]) }
  const queueAll = async () => {
    let n = 0
    for (const e of episodes.slice(0, 50)) { if (!e.queue_pos) { await api.post(`${V1}/episodes/${e.id}/queue`); n++ } }
    toast(`${n} added to the queue`); window.dispatchEvent(new CustomEvent('pp:queue')); reload()
  }
  return (
    <>
      <PageHeader title={<>{p.icon} {p.name}</>} sub={`${data.total} episode${data.total === 1 ? '' : 's'} · ${fmtDur(total)} · ${p.description || describe(p)}`}
        actions={<><button className="btn primary" onClick={playAll} disabled={!episodes.length}>▶ Play</button><button className="btn" onClick={queueAll} disabled={!episodes.length}>+ Queue all</button><button className="btn" onClick={() => setEdit(true)}>✎ Edit</button></>} />
      <EpisodeList episodes={episodes} onChange={eps => setData({ ...data, episodes: eps })} empty={<Empty title="Nothing matches right now">The list fills itself as episodes arrive.</Empty>} />
      {edit && <PlaylistEditor pl={p} onClose={() => setEdit(false)} onSaved={() => { setEdit(false); api.get(`${V1}/playlists/${id}`).then(reload).catch(() => navigate('/playlists')) }} />}
    </>
  )
}
