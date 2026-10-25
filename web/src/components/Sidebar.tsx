// Left column: Add button, sections, your shows and playlists, settings — the Pocket Calendar sidebar.
import { type ReactNode, useEffect, useRef, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Dashboard, Playlist, Podcast } from '../lib/types'
import { artColor } from '../lib/format'
import { Icon } from './icons'
import { type Anchor, type MenuItem, Menu, rectOf } from './ui'
import { useLive } from '../lib/realtime'

const SHOWN = 10

interface SidebarData { dash: Dashboard | null; podcasts: Podcast[]; playlists: Playlist[] }

/** Counts, shows and playlists for the sidebar; refetched when something changes. */
function useSidebarData(): SidebarData {
  const [data, setData] = useState<SidebarData>({ dash: null, podcasts: [], playlists: [] })
  const loc = useLocation()
  const last = useRef(0)
  const load = useRef(() => {})
  load.current = () => {
    last.current = Date.now()
    Promise.all([
      api.get<Dashboard>(`${V1}/dashboard`).catch(() => null),
      api.get<Podcast[]>(`${V1}/podcasts?sort=recent`).catch(() => [] as Podcast[]),
      api.get<Playlist[]>(`${V1}/playlists`).catch(() => [] as Playlist[]),
    ]).then(([dash, podcasts, playlists]) => setData({ dash, podcasts: podcasts || [], playlists: playlists || [] }))
  }
  useEffect(() => {
    load.current()
    const h = () => load.current()
    window.addEventListener('pp:queue', h)
    window.addEventListener('pp:changed', h)
    return () => { window.removeEventListener('pp:queue', h); window.removeEventListener('pp:changed', h) }
  }, [])
  // counts and lists change on other devices too: live events (a slow poll while the stream is down)
  useLive(ev => ev.type === 'podcasts' || ev.type === 'playlists' || ev.type === 'queue' || ev.type === 'episodes' || ev.type === 'history'
    || (ev.type === 'download' && (ev.state === 'done' || ev.state === 'failed' || ev.state === 'queued'))
    || (ev.type === 'progress' && ev.completed) || ev.type === 'saved', () => load.current(), { minGap: 5000 })
  // a page may have subscribed, played or downloaded something: refresh on navigation, at most every few seconds
  useEffect(() => { if (Date.now() - last.current > 4000) load.current() }, [loc.pathname])
  return data
}

export function Sidebar({ onNavigate }: { onNavigate: () => void }) {
  const { status, settings } = useApp()
  const navigate = useNavigate()
  const { dash, podcasts, playlists } = useSidebarData()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const title = settings?.title || status?.title || 'Pocket Podcasts'
  const go = (to: string) => { onNavigate(); navigate(to) }
  const addItems: MenuItem[] = [
    { label: 'Search directories', icon: 'search', hint: 'A', onSelect: () => go('/add') },
    { label: 'Feed URL', icon: 'rss', onSelect: () => go('/add?tab=url') },
    ...(settings?.yt_dlp ? [{ label: 'YouTube channel or video', icon: 'youtube', onSelect: () => go('/add?tab=youtube') }] : []),
    ...(settings?.podcastindex_key || settings?.podcastindex_env ? [{ label: 'Trending', icon: 'activity', onSelect: () => go('/add?tab=trending') }] : []),
    'sep',
    { label: 'Import OPML or JSON', icon: 'upload', onSelect: () => go('/settings/data') },
    { label: 'New smart playlist', icon: 'list-music', onSelect: () => go('/playlists?new=1') },
  ]
  const nav = [
    { to: '/home', label: 'Home', icon: 'home' },
    { to: '/podcasts', label: 'Podcasts', icon: 'podcast', count: dash?.podcasts },
    { to: '/episodes', label: 'Episodes', icon: 'list', count: dash?.unplayed, countTitle: 'unplayed' },
    { to: '/queue', label: 'Up next', icon: 'queue', badge: dash?.queued, badgeTitle: 'in the queue' },
    { to: '/playlists', label: 'Playlists', icon: 'list-music', end: true },
    { to: '/saved', label: 'Saved', icon: 'heart' },
    { to: '/history', label: 'History', icon: 'history' },
    { to: '/downloads', label: 'Downloads', icon: 'download', badge: dash?.downloads_pending, badgeTitle: 'downloading' },
    { to: '/stats', label: 'Stats', icon: 'bar-chart' },
  ]
  const shows = podcasts.slice(0, SHOWN)
  return (
    <aside className="sidebar" aria-label="Podcasts">
      <div className="sb-brand">
        <img src="/favicon.svg" alt="" className="sb-logo" />
        <span className="sb-title">{title !== 'Pocket Podcasts' ? title : <>Pocket <b>Podcasts</b></>}</span>
        {status?.version && <span className="sb-version" title={`Version ${status.version}`}>{/^\d/.test(status.version) ? 'v' + status.version : status.version}</span>}
      </div>
      <button type="button" className="btn primary create-btn" aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(menu ? null : rectOf(e.currentTarget))}>
        <Icon name="plus" size={17} strokeWidth={2.2} />Add<Icon name="chevron-down" size={15} className="create-caret" />
      </button>
      {menu && <Menu anchor={menu} items={addItems} onClose={() => setMenu(null)} width={248} />}

      <nav className="sb-nav" aria-label="Sections">
        {nav.map(n => (
          <NavLink key={n.to} to={n.to} end={n.end} onClick={onNavigate} className={({ isActive }) => `sb-nav-item ${isActive ? 'active' : ''}`}>
            <Icon name={n.icon} size={16} />{n.label}
            {!!n.badge && <span className="badge accent" title={`${n.badge} ${n.badgeTitle}`}>{n.badge}</span>}
            {!n.badge && !!n.count && <span className="sb-count" title={n.countTitle ? `${n.count} ${n.countTitle}` : undefined}>{n.count}</span>}
          </NavLink>
        ))}
      </nav>

      <Group title="Your podcasts" add={() => go('/add')} addLabel="Add a podcast (a)">
        {podcasts.length === 0
          ? <p className="sb-empty">Nothing followed yet. <button type="button" className="link-btn" onClick={() => go('/add')}>Add one</button></p>
          : <>
              <ul className="cal-list">{shows.map(p => <PodcastRow key={p.id} p={p} onNavigate={onNavigate} />)}</ul>
              {podcasts.length > SHOWN && <Link to="/podcasts" className="sb-more" onClick={onNavigate}><Icon name="arrow-right" size={13} />All {podcasts.length} podcasts</Link>}
            </>}
      </Group>

      {playlists.length > 0 && (
        <Group title="Playlists" add={() => go('/playlists?new=1')} addLabel="New smart playlist" closed={playlists.length > 6}>
          <ul className="cal-list">
            {playlists.map(pl => (
              <li key={pl.id} className="cal-row pod-row-sb">
                <NavLink to={`/playlists/${pl.id}`} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`} title={pl.description || pl.name}>
                  <span className="sb-pl-ico" aria-hidden>{pl.icon || '✦'}</span>
                  <span className="cal-name ellipsis">{pl.name}</span>
                  {pl.count > 0 && <span className="sb-meta">{pl.count}</span>}
                </NavLink>
              </li>
            ))}
          </ul>
        </Group>
      )}

      <div className="sb-foot">
        <Link to="/settings" className="sb-foot-link" onClick={onNavigate}><Icon name="settings" size={15} />Settings</Link>
        <button type="button" className="sb-foot-link" onClick={() => { onNavigate(); window.dispatchEvent(new CustomEvent('pp:shortcuts')) }}><Icon name="keyboard" size={15} />Shortcuts</button>
      </div>
    </aside>
  )
}

function Group({ title, children, add, addLabel, closed }: { title: string; children: ReactNode; add?: () => void; addLabel?: string; closed?: boolean }) {
  const key = `pp-sb-${title}`
  const [open, setOpen] = useState(() => { const v = localStorage.getItem(key); return v ? v === '1' : !closed })
  const toggle = () => setOpen(o => { localStorage.setItem(key, o ? '0' : '1'); return !o })
  return (
    <section className="sb-sec">
      <h4 className="sb-h">
        <button type="button" className="sb-h-btn" aria-expanded={open} onClick={toggle}><Icon name={open ? 'chevron-down' : 'chevron-right'} size={13} />{title}</button>
        {add && <button type="button" className="btn ghost icon-only xs" aria-label={addLabel} title={addLabel} onClick={add}><Icon name="plus" size={14} /></button>}
      </h4>
      {open && children}
    </section>
  )
}

function PodcastRow({ p, onNavigate }: { p: Podcast; onNavigate: () => void }) {
  const navigate = useNavigate()
  const [menu, setMenu] = useState<Anchor | null>(null)
  const [broken, setBroken] = useState(false)
  const go = (to: string) => { onNavigate(); navigate(to) }
  const refresh = () => api.post(`${V1}/podcasts/${p.id}/refresh`).catch(() => {})
  const items: MenuItem[] = [
    { label: 'Open', icon: 'podcast', onSelect: () => go(`/podcasts/${p.id}`) },
    { label: 'Unplayed episodes', icon: 'list', onSelect: () => go(`/episodes?podcast=${p.id}&status=unplayed`) },
    'sep',
    ...(p.feed_url.startsWith('pocket:') ? [] : [{ label: 'Check for new episodes', icon: 'refresh', onSelect: refresh }]),
    ...(p.site_url ? [{ label: 'Website', icon: 'external', onSelect: () => window.open(p.site_url, '_blank', 'noreferrer') }] : []),
  ]
  return (
    <li className="cal-row pod-row-sb">
      <NavLink to={`/podcasts/${p.id}`} onClick={onNavigate} className={({ isActive }) => `cal-check ${isActive ? 'is-active' : ''}`} title={[p.title, p.author].filter(Boolean).join(' · ')}>
        {p.artwork && !broken
          ? <img className="sb-art" src={p.artwork} alt="" loading="lazy" onError={() => setBroken(true)} />
          : <span className="sb-art" style={{ background: artColor(p.title) }} aria-hidden>{p.title.slice(0, 1).toUpperCase()}</span>}
        <span className="cal-name ellipsis">{p.title}</span>
        {p.unplayed_count > 0 ? <span className="sb-meta new" title={`${p.unplayed_count} unplayed`}>{p.unplayed_count}</span> : p.kind === 'youtube' ? <span className="sb-meta"><Icon name="youtube" size={13} /></span> : null}
      </NavLink>
      <button type="button" className="btn ghost icon-only xs cal-more" aria-label={`Options for ${p.title}`} aria-haspopup="menu" aria-expanded={!!menu} onClick={e => setMenu(rectOf(e.currentTarget))}><Icon name="more" size={15} /></button>
      {menu && <Menu anchor={menu} items={items} onClose={() => setMenu(null)} width={220} />}
    </li>
  )
}
