import { Link } from 'react-router-dom'
import type { Podcast } from '../lib/types'
import { Artwork } from './Artwork'
import { Icon } from './icons'

export function PodcastCard({ p }: { p: Podcast }) {
  return (
    <Link to={`/podcasts/${p.id}`} className="pod-card" title={p.title}>
      <div className="art-wrap">
        <Artwork src={p.artwork} title={p.title} size="fill" />
        {p.unplayed_count > 0 && <span className="count-pill">{p.unplayed_count}</span>}
        {p.kind === 'youtube' && <span className="yt-pill" title="YouTube"><Icon name="play" size={9} /></span>}
        {p.favorite && <span className="fav-pill" title="Favourite"><Icon name="star-fill" size={13} /></span>}
      </div>
      <div className="nm">{p.title}</div>
      <div className="au">{p.author || (p.kind === 'youtube' ? 'YouTube' : '')}</div>
    </Link>
  )
}
