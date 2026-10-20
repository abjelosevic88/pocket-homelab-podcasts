import { Link } from 'react-router-dom'
import type { Podcast } from '../lib/types'
import { Artwork } from './Artwork'

export function PodcastCard({ p }: { p: Podcast }) {
  return (
    <Link to={`/podcasts/${p.id}`} className="pod-card" title={p.title}>
      <div className="art-wrap">
        <Artwork src={p.artwork} title={p.title} size="fill" />
        {p.unplayed_count > 0 && <span className="count-pill">{p.unplayed_count}</span>}
        {p.kind === 'youtube' && <span className="yt-pill">▶</span>}
        {p.favorite && <span className="fav-pill">★</span>}
      </div>
      <div className="nm">{p.title}</div>
      <div className="au">{p.author || (p.kind === 'youtube' ? 'YouTube' : '')}</div>
    </Link>
  )
}
