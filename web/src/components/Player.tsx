import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { usePlayer, labelFor } from '../lib/player'
import { useApp } from '../lib/app-context'
import { Artwork } from './Artwork'
import { clock } from '../lib/format'
import { Icon } from './icons'
import { MenuButton, type MenuItem } from './ui'

const RATES = [0.8, 1, 1.1, 1.2, 1.3, 1.5, 1.7, 2, 2.5]

/** The persistent bar at the bottom and its expanded view. */
export function Player() {
  const p = usePlayer()
  const { settings } = useApp()
  if (p.remoteVisible && p.remote) return <RemoteBar />
  if (!p.current) return null
  const e = p.current
  const dur = p.duration || e.duration || 0
  const pct = dur ? Math.min(100, (p.time / dur) * 100) : 0
  const back = settings?.skip_back || 10, fwd = settings?.skip_forward || 30
  return (
    <>
      <div className="player-bar" role="region" aria-label="Player">
        <div className="pb-progress" onClick={ev => { const r = (ev.currentTarget as HTMLElement).getBoundingClientRect(); p.seek(((ev.clientX - r.left) / r.width) * dur) }}>
          <span className="fill" style={{ width: `${pct}%` }} />
          {p.sponsors.map(s => dur > 0 && <span key={s.start} className="seg" style={{ left: `${(s.start / dur) * 100}%`, width: `${((s.end - s.start) / dur) * 100}%` }} title={labelFor(s.category)} />)}
        </div>
        <button type="button" className="pb-art" onClick={() => p.setExpanded(true)} aria-label="Open player"><Artwork src={e.artwork_url || e.podcast_artwork} title={e.podcast_title || e.title} size="xs" /></button>
        <div className="pb-info" onClick={() => p.setExpanded(true)}>
          <div className="t">{e.title}</div>
          <div className="s muted">{p.chapter ? p.chapter.title : e.podcast_title} · {clock(p.time)} / {clock(dur)}{p.rate !== 1 ? ` · ${p.rate}×` : ''}</div>
        </div>
        <div className="pb-controls">
          <button type="button" className="btn ghost icon-only hide-sm pb-skip" onClick={() => p.skip(-back)} title={`Back ${back} s`} aria-label={`Back ${back} seconds`}><Icon name="rotate-ccw" size={18} /></button>
          <button type="button" className="btn primary round pb-play" onClick={p.toggle} title={p.playing ? 'Pause' : 'Play'} aria-label={p.playing ? 'Pause' : 'Play'}>{p.loading ? <span className="spinner sm on-accent" /> : <Icon name={p.playing ? 'pause' : 'play'} size={18} />}</button>
          <button type="button" className="btn ghost icon-only pb-skip" onClick={() => p.skip(fwd)} title={`Forward ${fwd} s`} aria-label={`Forward ${fwd} seconds`}><Icon name="rotate-cw" size={18} /></button>
          <button type="button" className="btn ghost icon-only hide-sm" onClick={p.next} title="Next in queue" aria-label="Next in queue"><Icon name="skip-next" size={16} /></button>
          <button type="button" className="btn ghost icon-only hide-sm" onClick={() => p.setExpanded(true)} title="Open the player" aria-label="Open the player"><Icon name="chevron-up" size={18} /></button>
        </div>
      </div>
      {p.expanded && <Expanded />}
    </>
  )
}

function Expanded() {
  const p = usePlayer()
  const { settings } = useApp()
  const [tab, setTab] = useState<'now' | 'chapters' | 'notes'>('now')
  const e = p.current!
  const dur = p.duration || e.duration || 0
  const back = settings?.skip_back || 10, fwd = settings?.skip_forward || 30
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const h = (ev: KeyboardEvent) => { if (ev.key === 'Escape') p.setExpanded(false) }
    window.addEventListener('keydown', h)
    document.body.style.overflow = 'hidden'
    return () => { window.removeEventListener('keydown', h); document.body.style.overflow = '' }
  }, [p])
  const sleepLeft = p.sleepAt ? Math.max(0, Math.round((p.sleepAt - Date.now()) / 60000)) : 0
  return (
    <div className="player-full" ref={ref}>
      <div className="pf-head">
        <button type="button" className="btn ghost icon-only" onClick={() => p.setExpanded(false)} aria-label="Close the player" title="Close (esc)"><Icon name="chevron-down" size={20} /></button>
        <div className="tabs" role="tablist">
          <button type="button" role="tab" aria-selected={tab === 'now'} className={tab === 'now' ? 'active' : ''} onClick={() => setTab('now')}>Now playing</button>
          {p.chapters.length > 0 && <button type="button" role="tab" aria-selected={tab === 'chapters'} className={tab === 'chapters' ? 'active' : ''} onClick={() => setTab('chapters')}>Chapters</button>}
          <button type="button" role="tab" aria-selected={tab === 'notes'} className={tab === 'notes' ? 'active' : ''} onClick={() => setTab('notes')}>Notes</button>
        </div>
        <button type="button" className="btn ghost icon-only" onClick={() => { p.stop(); p.setExpanded(false) }} title="Stop and clear" aria-label="Stop and clear"><Icon name="x" size={18} /></button>
      </div>
      <div className="pf-body">
        {tab === 'now' && (
          <div className="pf-now">
            <Artwork src={e.artwork_url || e.podcast_artwork} title={e.podcast_title || e.title} size="xl" />
            <div className="pf-title"><Link to={`/episodes/${e.id}`} onClick={() => p.setExpanded(false)}>{e.title}</Link></div>
            <div className="muted"><Link to={`/podcasts/${e.podcast_id}`} onClick={() => p.setExpanded(false)}>{e.podcast_title}</Link>{p.chapter && <> · {p.chapter.title}</>}</div>
          </div>
        )}
        {tab === 'chapters' && (
          <ul className="chapters">
            {p.chapters.map((c, i) => <li key={i} className={p.chapter === c ? 'cur' : ''}><button type="button" onClick={() => p.seek(c.start)}><span className="mono">{clock(c.start)}</span><span className="grow">{c.title}</span></button></li>)}
          </ul>
        )}
        {tab === 'notes' && <div className="pf-notes md" dangerouslySetInnerHTML={{ __html: safeHTML(e.description) }} />}
      </div>
      <div className="pf-controls">
        <div className="pf-seek">
          <input type="range" min={0} max={dur || 1} step={1} value={Math.min(p.time, dur || 1)} onChange={ev => p.seek(Number(ev.target.value))} aria-label="Position" />
          <div className="row small muted"><span>{clock(p.time)}</span><span className="spacer" />{p.sponsors.length > 0 && settings?.auto_skip_sponsors && <span className="badge ok" title="SponsorBlock segments are skipped">SponsorBlock</span>}<span>-{clock(Math.max(0, dur - p.time))}</span></div>
        </div>
        <div className="pf-buttons">
          <button type="button" className="btn ghost icon-only lg" onClick={() => p.seek(0)} title="Restart" aria-label="Restart"><Icon name="skip-prev" size={18} /></button>
          <button type="button" className="btn ghost lg pf-skip" onClick={() => p.skip(-back)} title={`Back ${back} s`} aria-label={`Back ${back} seconds`}><Icon name="rotate-ccw" size={22} /><small>{back}</small></button>
          <button type="button" className="btn primary round xl" onClick={p.toggle} aria-label={p.playing ? 'Pause' : 'Play'}>{p.loading ? <span className="spinner on-accent" /> : <Icon name={p.playing ? 'pause' : 'play'} size={26} />}</button>
          <button type="button" className="btn ghost lg pf-skip" onClick={() => p.skip(fwd)} title={`Forward ${fwd} s`} aria-label={`Forward ${fwd} seconds`}><Icon name="rotate-cw" size={22} /><small>{fwd}</small></button>
          <button type="button" className="btn ghost icon-only lg" onClick={p.next} title="Next in queue" aria-label="Next in queue"><Icon name="skip-next" size={18} /></button>
        </div>
        <div className="pf-extra">
          <div className="segmented sm pf-rates" role="radiogroup" aria-label="Speed">{RATES.map(r => <button key={r} type="button" role="radio" aria-checked={Math.abs(p.rate - r) < 0.01} className={Math.abs(p.rate - r) < 0.01 ? 'active' : ''} onClick={() => p.setRate(r)}>{r}×</button>)}</div>
          <MenuButton label="Sleep timer" className={`btn sm ${p.sleepAt ? 'on' : ''}`} align="right" width={190} items={() => [
            { heading: 'Stop playing after' },
            ...[5, 10, 15, 30, 45, 60].map(m => ({ label: `${m} minutes`, onSelect: () => p.setSleep(m) })),
            ...(dur > 0 ? [{ label: 'End of episode', onSelect: () => p.setSleep(Math.ceil((dur - p.time) / p.rate / 60)) }] : []),
            ...(p.sleepAt ? ['sep' as const, { label: 'Off', icon: 'x', onSelect: () => p.setSleep(null) }] : []),
          ] as MenuItem[]}><Icon name="moon" size={14} />{p.sleepAt ? `${sleepLeft} min` : 'Sleep'}</MenuButton>
        </div>
      </div>
    </div>
  )
}

/** Episode notes are publisher HTML: keep the harmless tags, drop the rest. */
export function safeHTML(html: string): string {
  if (!html) return ''
  const d = document.createElement('div')
  d.innerHTML = html
  const ok = new Set(['P', 'BR', 'B', 'STRONG', 'I', 'EM', 'U', 'A', 'UL', 'OL', 'LI', 'H1', 'H2', 'H3', 'H4', 'BLOCKQUOTE', 'CODE', 'PRE', 'HR', 'SPAN', 'DIV', 'SMALL'])
  const walk = (n: Element) => {
    for (const c of Array.from(n.children)) {
      if (!ok.has(c.tagName)) { c.replaceWith(document.createTextNode(c.textContent || '')); continue }
      for (const a of Array.from(c.attributes)) if (!(c.tagName === 'A' && a.name === 'href' && /^https?:/i.test(a.value))) c.removeAttribute(a.name)
      if (c.tagName === 'A') { c.setAttribute('target', '_blank'); c.setAttribute('rel', 'noreferrer') }
      walk(c)
    }
  }
  walk(d)
  // bare URLs and timestamps become links
  return d.innerHTML.replace(/(^|[\s>])(https?:\/\/[^\s<]+)/g, '$1<a href="$2" target="_blank" rel="noreferrer">$2</a>')
}

/** Another device is playing (the phone, another browser): its episode with a
 *  clock that keeps ticking between reports, and "Continue here". */
function RemoteBar() {
  const p = usePlayer()
  const r = p.remote!
  const e = r.episode!
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!r.playing) { setNow(Date.now()); return }
    const t = setInterval(() => setNow(Date.now()), 500)
    return () => clearInterval(t)
  }, [r.playing, r.updated_at])
  const dur = r.duration || e.duration || 0
  const elapsed = r.playing ? Math.max(0, (now + p.clockOffset - r.updated_at) / 1000) * (r.rate || 1) : 0
  const pos = Math.min(dur || Infinity, r.position + elapsed)
  const pct = dur ? Math.min(100, (pos / dur) * 100) : 0
  const ago = Math.round((now + p.clockOffset - r.updated_at) / 60000)
  const here = () => p.play({ ...e, play_position: Math.floor(pos), completed: false })
  return (
    <div className="player-bar remote" role="region" aria-label={`Playing on ${r.device}`}>
      <div className="pb-progress remote"><span className="fill" style={{ width: `${pct}%` }} /></div>
      <Link to={`/episodes/${e.id}`} className="pb-art"><Artwork src={e.artwork_url || e.podcast_artwork} title={e.podcast_title || e.title} size="xs" /></Link>
      <Link to={`/episodes/${e.id}`} className="pb-info">
        <div className="t">{e.title}</div>
        <div className="s">
          <span className={`remote-dot ${r.playing ? 'live' : ''}`} aria-hidden />
          <span className="remote-where">{r.playing ? 'Playing' : 'Paused'} on {r.device}</span>
          <span className="muted"> · {clock(pos)} / {clock(dur)}{!r.playing && ago >= 1 ? ` · ${ago} min ago` : ''}</span>
        </div>
      </Link>
      <div className="pb-controls">
        <button type="button" className="btn sm primary remote-here" onClick={here} title={`Continue in this browser from ${clock(pos)}`}><Icon name="play" size={13} />Continue here</button>
      </div>
    </div>
  )
}
