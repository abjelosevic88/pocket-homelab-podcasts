import { api } from '../lib/api'
import { clock } from '../lib/format'
import { NeedsCore, useAsync } from './ui'
import { Icon } from './icons'

// Your notes about this episode, from Pocket Notes (notes there that link to its page), and a
// way to start one at the moment you are at. Greyed out with what Pocket Core is where Pocket
// Podcasts runs on its own; shows nothing where Core has no Pocket Notes; greyed out, saying
// what to switch on, where the link is off.
interface NotesBox {
  available: boolean
  missing?: boolean
  no_core?: boolean
  reason?: string
  notes: { id: number; title: string; folder: string; excerpt: string; modified: number; url: string }[]
  new_url?: string
}

const when = (unix: number) => new Date(unix * 1000).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })

// The note links to the episode at this second (?t=), and its link says so.
function atMoment(newURL: string, at: number): string {
  if (at < 5) return newURL
  const u = new URL(newURL)
  const item = u.searchParams.get('url') || ''
  u.searchParams.set('url', `${item}?t=${Math.floor(at)}`)
  u.searchParams.set('label', `${u.searchParams.get('label') || ''} at ${clock(at)}`)
  return u.toString()
}

export function NotesPanel({ src, at }: { src: string; at: number }) {
  const { data } = useAsync(() => api.get<NotesBox>(src), [src])
  if (!data || (data.missing && !data.no_core)) return null
  const label = <><Icon name="plus" size={14} />{at >= 5 ? `Note at ${clock(at)}` : 'New note'}</>
  return (
    <div className="epx-panel notes-panel">
      <h3>Notes {data.notes.length > 0 && <span className="muted">{data.notes.length}</span>}
        <span className="spacer" />
        {data.available && data.new_url
          ? <a className="btn ghost sm" href={atMoment(data.new_url, at)} target="_blank" rel="noreferrer" title="Opens Pocket Notes with a note linked to this episode, at this moment">{label}</a>
          : <span title={data.reason}><button type="button" className="btn ghost sm" disabled aria-disabled>{label}</button></span>}
      </h3>
      {data.no_core ? <NeedsCore what="Notes come from Pocket Notes, through Pocket Core." />
        : !data.available ? <p className="muted small row top"><Icon name="lock" size={14} />{data.reason}</p>
        : data.notes.length === 0 ? <p className="muted small">No notes about this episode yet. Start one at the moment you are at; any note that links to this page shows up here.</p>
        : <ul className="notes-list">{data.notes.map(n => (
            <li key={n.id}><a href={n.url} target="_blank" rel="noreferrer">
              <b>{n.title || 'Untitled'}</b>
              {n.excerpt && <span className="muted small clamp2">{n.excerpt}</span>}
              <span className="muted small">{n.folder || 'Notes'} · {when(n.modified)}</span>
            </a></li>))}
          </ul>}
    </div>
  )
}
