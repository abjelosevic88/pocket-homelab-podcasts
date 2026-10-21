// The app shell: sidebar, top bar, search palette, shortcuts, the player bar — the Pocket Calendar layout.
import { createContext, useContext, useEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import { Icon } from './icons'
import { Sidebar } from './Sidebar'
import { Player } from './Player'
import { CommandPalette, isMac, openPalette } from './CommandPalette'
import { ShortcutsSheet } from './ShortcutsSheet'
import { Avatar, type MenuItem, MenuButton, modalOpen, useIsMobile, useMediaQuery } from './ui'

const SIDEBAR_KEY = 'pp-sidebar'

/** A page's place in the top bar: "Podcasts › The Show". */
export type Crumb = { label: string; to?: string }
const CrumbCtx = createContext<(c: Crumb[] | null) => void>(() => {})
export function useCrumb(c: Crumb[] | null) {
  const set = useContext(CrumbCtx)
  const key = JSON.stringify(c)
  useEffect(() => { set(c); return () => set(null) }, [key]) // eslint-disable-line react-hooks/exhaustive-deps
}

export default function Layout() {
  const navigate = useNavigate()
  const loc = useLocation()
  const mobile = useIsMobile()
  const narrow = useMediaQuery('(max-width: 1099px)')
  const drawer = mobile || narrow
  const [sidebarOpen, setSidebarOpen] = useState(() => !drawer && localStorage.getItem(SIDEBAR_KEY) !== '0')
  useEffect(() => { setSidebarOpen(!drawer && localStorage.getItem(SIDEBAR_KEY) !== '0') }, [drawer])
  const drawerOpenedAt = useRef(0)
  const toggleSidebar = () => setSidebarOpen(o => { drawerOpenedAt.current = performance.now(); const n = !o; if (!drawer) localStorage.setItem(SIDEBAR_KEY, n ? '1' : '0'); return n })
  const closeDrawer = () => { if (drawer) setSidebarOpen(false) }
  useEffect(() => { document.querySelector('.content')?.scrollTo({ top: 0 }) }, [loc.pathname])
  const [crumb, setCrumb] = useState<Crumb[] | null>(null)
  const [shortcuts, setShortcuts] = useState(false)

  useEffect(() => {
    const open = () => setShortcuts(true)
    window.addEventListener('pp:shortcuts', open)
    return () => window.removeEventListener('pp:shortcuts', open)
  }, [])
  // ⌘K and "/" belong to the search palette; the player has space, ← →, j k l, < > and shift+N
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if (e.metaKey || e.ctrlKey || e.altKey) return
      if (t.closest('input, textarea, select, [contenteditable="true"]') || modalOpen() || document.querySelector('.menu, .popover, .sel-pop, .cmdk-backdrop, .player-full')) return
      const go = (to: string) => { e.preventDefault(); navigate(to) }
      switch (e.key) {
        case '?': e.preventDefault(); setShortcuts(true); break
        case 'h': go('/home'); break
        case 'p': go('/podcasts'); break
        case 'e': go('/episodes'); break
        case 'u': go('/queue'); break
        case 'd': go('/downloads'); break
        case 'a': go('/add'); break
        case ',': go('/settings'); break
        case '[': e.preventDefault(); toggleSidebar(); break
      }
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <CrumbCtx.Provider value={setCrumb}>
      <div className={`app ${sidebarOpen ? 'with-sidebar' : 'no-sidebar'} ${mobile ? 'is-mobile' : ''} ${drawer ? 'drawer-mode' : ''}`}>
        {drawer && sidebarOpen && <div className="drawer-backdrop" onClick={e => { if (e.timeStamp - drawerOpenedAt.current > 400) setSidebarOpen(false) }} />}
        <div className={`sidebar-wrap ${sidebarOpen ? 'open' : ''}`}>
          <Sidebar onNavigate={closeDrawer} />
        </div>
        <div className="main">
          <TopBar onMenu={toggleSidebar} mobile={mobile} compact={narrow} sidebarOpen={sidebarOpen} crumb={crumb} />
          <main className="content"><div className="content-inner"><Outlet /></div></main>
          <Player />
        </div>
        <CommandPalette />
        {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
      </div>
    </CrumbCtx.Provider>
  )
}

const SECTION: Record<string, string> = { home: 'Home', podcasts: 'Podcasts', episodes: 'Episodes', queue: 'Up next', playlists: 'Playlists', saved: 'Saved', history: 'History', downloads: 'Downloads', add: 'Add', stats: 'Stats' }

/** The avatar menu: profile, settings, shortcuts, appearance, sign out (also on the Settings page). */
export function useAvatarMenu(): () => MenuItem[] {
  const { user, themeMode, setThemeMode, status, setUser } = useApp()
  const navigate = useNavigate()
  const signOut = async () => { await api.post(`${V1}/auth/logout`).catch(() => {}); setUser(null); navigate('/login') }
  return () => [
    { heading: user?.name || user?.email || '' },
    { label: 'Profile', icon: 'user', onSelect: () => navigate('/settings/profile') },
    { label: 'Settings', icon: 'settings', onSelect: () => navigate('/settings'), hint: ',' },
    { label: 'Keyboard shortcuts', icon: 'keyboard', onSelect: () => window.dispatchEvent(new CustomEvent('pp:shortcuts')), hint: '?' },
    'sep',
    { heading: 'Appearance' },
    { label: 'System', checked: themeMode === 'system', onSelect: () => setThemeMode('system') },
    { label: 'Light', checked: themeMode === 'light', onSelect: () => setThemeMode('light') },
    { label: 'Dark', checked: themeMode === 'dark', onSelect: () => setThemeMode('dark') },
    ...(status?.auth_mode === 'local' ? ['sep' as const, { label: 'Sign out', icon: 'log-out', onSelect: signOut }] : []),
  ]
}

function TopBar({ onMenu, mobile, compact, sidebarOpen, crumb }: { onMenu: () => void; mobile: boolean; compact: boolean; sidebarOpen: boolean; crumb: Crumb[] | null }) {
  const { user } = useApp()
  const navigate = useNavigate()
  const loc = useLocation()
  const avatarMenu = useAvatarMenu()
  const section = SECTION[loc.pathname.split('/')[1] || ''] || ''
  return (
    <header className="topbar">
      <button type="button" className="btn ghost icon-only" onClick={onMenu} aria-label={sidebarOpen ? 'Hide the sidebar' : 'Show the sidebar'} title="Menu ([)"><Icon name={mobile || compact ? 'menu' : 'sidebar'} size={18} /></button>
      <h1 className="range-title">
        {crumb && crumb.length > 0
          ? crumb.map((c, i) => i < crumb.length - 1
            ? <span key={i} className="row" style={{ gap: 4, minWidth: 0 }}><button type="button" className="crumb" onClick={() => c.to && navigate(c.to)}>{c.label}</button><Icon name="chevron-right" size={16} className="crumb-sep" /></span>
            : <span key={i} className="ellipsis">{c.label}</span>)
          : section}
      </h1>
      <span className="spacer" />
      <button type="button" className="btn ghost topbar-search" onClick={openPalette} aria-label="Search" title={`Search everything (${isMac ? '⌘' : 'Ctrl'}K or /)`}>
        <Icon name="search" size={17} /><span className="topbar-search-label">Search</span><kbd className="topbar-search-kbd">{isMac ? '⌘' : 'Ctrl'} K</kbd>
      </button>
      <MenuButton label="Account" className="avatar-btn" align="right" width={220} items={avatarMenu}>
        <Avatar name={user?.name} email={user?.email} size={30} />
      </MenuButton>
    </header>
  )
}
