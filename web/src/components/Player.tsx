import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { usePlayer, labelFor } from '../lib/player'
import { useApp } from '../lib/app-context'
import { Artwork } from './Artwork'
import { clock } from '../lib/format'

const RATES = [0.8, 1, 1.1, 1.2, 1.3, 1.5, 1.7, 2, 2.5]

/** The persistent bar at the bottom and its expanded view. */
export function Player() {
  const p = usePlayer()
  const { settings } = useApp()
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
        <button className="pb-art" onClick={() => p.setExpanded(true)} aria-label="Open player"><Artwork src={e.artwork_url || e.podcast_artwork} title={e.podcast_title || e.title} size="xs" /></button>
        <div className="pb-info" onClick={() => p.setExpanded(true)}>
          <div className="t">{e.title}</div>
          <div className="s muted">{p.chapter ? p.chapter.title : e.podcast_title} · {clock(p.time)} / {clock(dur)}{p.rate !== 1 ? ` · ${p.rate}×` : ''}</div>
        </div>
        <div className="pb-controls">
          <button className="btn ghost icon hide-sm" onClick={() => p.skip(-back)} title={`Back ${back} s`}>↺</button>
          <button className="btn primary icon round" onClick={p.toggle} title={p.playing ? 'Pause' : 'Play'}>{p.loading ? <span className="spinner sm" style={{ borderTopColor: '#fff' }} /> : p.playing ? '❚❚' : '▶'}</button>
          <button className="btn ghost icon" onClick={() => p.skip(fwd)} title={`Forward ${fwd} s`}>↻</button>
          <button className="btn ghost icon hide-sm" onClick={p.next} title="Next in queue">⏭</button>
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
  const [sleepOpen, setSleepOpen] = useState(false)
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
        <button className="btn ghost icon" onClick={() => p.setExpanded(false)} aria-label="Close">⌄</button>
        <div className="tabs" style={{ border: 0 }}>
          <button className={tab === 'now' ? 'active' : ''} onClick={() => setTab('now')}>Now playing</button>
          {p.chapters.length > 0 && <button className={tab === 'chapters' ? 'active' : ''} onClick={() => setTab('chapters')}>Chapters</button>}
          <button className={tab === 'notes' ? 'active' : ''} onClick={() => setTab('notes')}>Notes</button>
        </div>
        <button className="btn ghost icon" onClick={() => { p.stop(); p.setExpanded(false) }} title="Stop and clear">✕</button>
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
            {p.chapters.map((c, i) => <li key={i} className={p.chapter === c ? 'cur' : ''}><button onClick={() => p.seek(c.start)}><span className="mono">{clock(c.start)}</span><span className="grow">{c.title}</span></button></li>)}
          </ul>
        )}
        {tab === 'notes' && <div className="pf-notes md" dangerouslySetInnerHTML={{ __html: safeHTML(e.description) }} />}
      </div>
      <div className="pf-controls">
        <div className="pf-seek">
          <input type="range" min={0} max={dur || 1} step={1} value={Math.min(p.time, dur || 1)} onChange={ev => p.seek(Number(ev.target.value))} aria-label="Position" />
          <div className="row small muted"><span>{clock(p.time)}</span><span className="spacer" />{p.sponsors.length > 0 && settings?.auto_skip_sponsors && <span title="SponsorBlock on">SB ✓</span>}<span>-{clock(Math.max(0, dur - p.time))}</span></div>
        </div>
        <div className="pf-buttons">
          <button className="btn ghost" onClick={() => p.seek(0)} title="Restart">⏮</button>
          <button className="btn ghost lg" onClick={() => p.skip(-back)} title={`Back ${back} s`}>↺<small>{back}</small></button>
          <button className="btn primary round xl" onClick={p.toggle}>{p.loading ? <span className="spinner" style={{ borderTopColor: '#fff' }} /> : p.playing ? '❚❚' : '▶'}</button>
          <button className="btn ghost lg" onClick={() => p.skip(fwd)} title={`Forward ${fwd} s`}>↻<small>{fwd}</small></button>
          <button className="btn ghost" onClick={p.next} title="Next in queue">⏭</button>
        </div>
        <div className="pf-extra">
          <div className="segmented sm">{RATES.map(r => <button key={r} className={Math.abs(p.rate - r) < 0.01 ? 'active' : ''} onClick={() => p.setRate(r)}>{r}×</button>)}</div>
          <div className="menu-wrap">
            <button className={`btn sm ${p.sleepAt ? 'on' : ''}`} onClick={() => setSleepOpen(o => !o)}>{p.sleepAt ? `☾ ${sleepLeft} min` : '☾ Sleep'}</button>
            {sleepOpen && <div className="menu up" onClick={() => setSleepOpen(false)}>
              {[5, 10, 15, 30, 45, 60].map(m => <button key={m} onClick={() => p.setSleep(m)}>{m} minutes</button>)}
              {dur > 0 && <button onClick={() => p.setSleep(Math.ceil((dur - p.time) / p.rate / 60))}>End of episode</button>}
              {p.sleepAt && <button onClick={() => p.setSleep(null)}>Off</button>}
            </div>}
          </div>
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
