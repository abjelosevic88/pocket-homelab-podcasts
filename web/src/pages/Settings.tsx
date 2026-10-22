import { AskButton } from '../components/AskButton'
import React, { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, upload, V1 } from '../lib/api'
import { useApp } from '../lib/app-context'
import type { Activity, APIToken, Podcast, Settings as SettingsT, User } from '../lib/types'
import { Avatar, Confirm, CopyField, Field, Loading, Modal, Segmented, SettingRow, Switch, Tabs, copyText, useAsync, useIsMobile, useToast } from '../components/ui'
import { Icon } from '../components/icons'
import { Player } from '../components/Player'
import { ShortcutsSheet } from '../components/ShortcutsSheet'
import { ago, bytes, SB_CATEGORIES } from '../lib/format'

type TabId = 'general' | 'playback' | 'downloads' | 'search' | 'notifications' | 'feeds' | 'profile' | 'people' | 'tokens' | 'data' | 'activity' | 'about'

const TABS: { id: TabId; label: string; icon: string; sub: string }[] = [
  { id: 'general', label: 'General', icon: 'settings', sub: 'Where the app opens, and its title.' },
  { id: 'playback', label: 'Playback', icon: 'headphones', sub: 'Speed, skip lengths, what plays next, and SponsorBlock.' },
  { id: 'downloads', label: 'Feeds & downloads', icon: 'download', sub: 'How often feeds are checked, what is downloaded and kept, and YouTube.' },
  { id: 'search', label: 'Search', icon: 'search', sub: 'The podcast directories the Add page and ⌘K search.' },
  { id: 'notifications', label: 'Notifications', icon: 'bell', sub: 'Where a message goes when a podcast you marked has new episodes.' },
  { id: 'feeds', label: 'Public feeds', icon: 'rss', sub: 'Every subscription as a podcast feed for Apple Podcasts, Overcast or AntennaPod.' },
  { id: 'profile', label: 'Profile', icon: 'user', sub: 'Your name, email and password.' },
  { id: 'people', label: 'People', icon: 'users', sub: 'Everyone who has an account on this box.' },
  { id: 'tokens', label: 'API tokens', icon: 'key', sub: 'Personal tokens for widgets, Shortcuts and scripts.' },
  { id: 'data', label: 'Backup & data', icon: 'database', sub: 'Download the database, export and import subscriptions.' },
  { id: 'activity', label: 'Activity', icon: 'activity', sub: 'What the app did on its own: new episodes, downloads, clean-ups and errors.' },
  { id: 'about', label: 'About', icon: 'info', sub: 'Version, numbers and the server.' },
]
// the old ?tab= names
const ALIAS: Record<string, TabId> = { notify: 'notifications', users: 'people', api: 'tokens' }

export default function Settings() {
  const { tab: raw } = useParams()
  const navigate = useNavigate()
  const mobile = useIsMobile()
  const legacy = new URLSearchParams(window.location.search).get('tab') || ''
  const want = raw || legacy
  const tab: TabId = (TABS.find(t => t.id === (ALIAS[want] || want))?.id) || 'general'
  const current = TABS.find(t => t.id === tab)!
  const go = (id: TabId) => navigate(`/settings/${id}`, { replace: true })
  const [shortcuts, setShortcuts] = useState(false)

  useEffect(() => { document.querySelector('.set-content')?.scrollTo({ top: 0 }) }, [tab])
  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape' && !document.querySelector('.modal-backdrop, .menu, .sel-pop, .player-full')) navigate('/') }
    const open = () => setShortcuts(true)
    window.addEventListener('keydown', h)
    window.addEventListener('pp:shortcuts', open)
    return () => { window.removeEventListener('keydown', h); window.removeEventListener('pp:shortcuts', open) }
  }, [navigate])

  return (
    <div className="set-page">
      <header className="set-header">
        <button type="button" className="btn ghost icon-only" aria-label="Back to the podcasts" title="Back to the podcasts (esc)" onClick={() => navigate('/')}><Icon name="chevron-left" size={18} /></button>
        <img src="/favicon.svg" alt="" width={24} height={24} className="set-logo" />
        <h1>Settings</h1>
        <span style={{ flex: 1 }} />
        <AskButton />
      </header>
      {mobile && <div className="set-tabs-mobile"><Tabs tabs={TABS.map(t => ({ id: t.id, label: t.label, icon: t.icon }))} value={tab} onChange={go} /></div>}
      <div className="set-body">
        {!mobile && (
          <nav className="set-nav" aria-label="Settings sections">
            {TABS.map(t => (
              <button key={t.id} type="button" className={`set-nav-item ${t.id === tab ? 'active' : ''}`} aria-current={t.id === tab ? 'page' : undefined} onClick={() => go(t.id)}>
                <Icon name={t.icon} size={16} />{t.label}
              </button>
            ))}
          </nav>
        )}
        <main className="set-content">
          <div className="set-inner" key={tab}>
            <div className="set-title">
              <h2>{current.label}</h2>
              <p>{current.sub}</p>
            </div>
            {tab === 'general' && <General />}
            {tab === 'playback' && <Playback />}
            {tab === 'downloads' && <Downloads />}
            {tab === 'search' && <Search />}
            {tab === 'notifications' && <Notify />}
            {tab === 'feeds' && <Feeds />}
            {tab === 'profile' && <Profile />}
            {tab === 'people' && <Users />}
            {tab === 'tokens' && <Tokens />}
            {tab === 'data' && <Data />}
            {tab === 'activity' && <ActivityTab />}
            {tab === 'about' && <About />}
          </div>
        </main>
      </div>
      <Player />
      {shortcuts && <ShortcutsSheet onClose={() => setShortcuts(false)} />}
    </div>
  )
}

/** A settings card: title, explanation, actions on the right — Pocket Calendar's section. */
function Section({ title, sub, icon, actions, children }: { title: React.ReactNode; sub?: React.ReactNode; icon?: string; actions?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <section className="set-section">
      <div className="set-section-head">
        {icon && <span className="set-app-ico set-ico"><Icon name={icon} size={17} /></span>}
        <div className="grow"><h3>{title}</h3>{sub && <p>{sub}</p>}</div>
        {actions && <div className="row">{actions}</div>}
      </div>
      {children && <div className="set-section-body">{children}</div>}
    </section>
  )
}

function useSettingsForm() {
  const { settings, setSettings, user } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState<SettingsT | null>(settings)
  const [busy, setBusy] = useState(false)
  useEffect(() => { if (!f && settings) setF(settings) }, [settings]) // eslint-disable-line react-hooks/exhaustive-deps
  const save = async (extra: Record<string, unknown> = {}) => {
    if (!f) return
    setBusy(true)
    try { const s = await api.put<SettingsT>(`${V1}/settings`, { ...f, ...extra }); setSettings(s); setF(s); toast('Saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return { f, setF, save, busy, isAdmin: user?.role === 'admin' }
}

const SaveBar = ({ onSave, busy, children }: { onSave: () => void; busy: boolean; children?: React.ReactNode }) => (
  <div className="row wrap"><button type="button" className="btn primary" onClick={onSave} disabled={busy}>{busy ? 'Saving…' : 'Save changes'}</button>{children}</div>
)

function General() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  if (!f) return <Loading />
  return (
    <div className="stack loose">
      <Section title="You" sub="Everyone on this box has their own subscriptions, queue, history and settings. Only the title is shared.">
        <SettingRow title="Open on" help="The page you land on when you open the app.">
          <Segmented size="sm" value={f.landing} onChange={v => setF({ ...f, landing: v })} options={[{ value: 'home', label: 'Home' }, { value: 'podcasts', label: 'Podcasts' }, { value: 'episodes', label: 'Episodes' }, { value: 'queue', label: 'Queue' }]} />
        </SettingRow>
      </Section>
      <Section title="This box" sub={isAdmin ? 'This applies to everyone on this box.' : 'Set by an admin for everyone.'}>
        <SettingRow title="App title" help="Shown in the sidebar and the browser tab."><input value={f.title} onChange={e => setF({ ...f, title: e.target.value })} maxLength={40} disabled={!isAdmin} style={{ width: 220 }} /></SettingRow>
      </Section>
      <SaveBar onSave={() => save()} busy={busy} />
    </div>
  )
}

function Playback() {
  const { f, setF, save, busy } = useSettingsForm()
  if (!f) return <Loading />
  const cats = new Set(f.skip_categories.split(',').map(s => s.trim()).filter(Boolean))
  const toggle = (id: string) => { if (cats.has(id)) cats.delete(id); else cats.add(id); setF({ ...f, skip_categories: Array.from(cats).join(',') }) }
  return (
    <div className="stack loose">
      <Section title="Player" icon="headphones">
        <SettingRow title="Default speed" help="Each podcast can override it."><input type="number" step={0.05} min={0.5} max={3} value={f.playback_speed} onChange={e => setF({ ...f, playback_speed: Number(e.target.value) })} className="num" aria-label="Default speed" /></SettingRow>
        <SettingRow title="Skip back" help="Seconds, for the back button, ← and the lock screen."><input type="number" min={5} max={300} value={f.skip_back} onChange={e => setF({ ...f, skip_back: Number(e.target.value) })} className="num" aria-label="Skip back (s)" /></SettingRow>
        <SettingRow title="Skip forward" help="Seconds, for the forward button and →."><input type="number" min={5} max={300} value={f.skip_forward} onChange={e => setF({ ...f, skip_forward: Number(e.target.value) })} className="num" aria-label="Skip forward (s)" /></SettingRow>
        <SettingRow title="Count as played at" help="Percent. Reaching this far marks the episode played and drops it from the queue."><input type="number" min={50} max={100} value={f.complete_at} onChange={e => setF({ ...f, complete_at: Number(e.target.value) })} className="num" aria-label="Count as played at (%)" /></SettingRow>
        <SettingRow title="Play the next one in the queue" help="When an episode ends."><Switch checked={f.auto_play_next} onChange={v => setF({ ...f, auto_play_next: v })} label="Play the next one in the queue" /></SettingRow>
        <p className="muted small mt-s">Keyboard: <kbd>⌘</kbd>/<kbd>Ctrl</kbd>+<kbd>K</kbd> or <kbd>/</kbd> search everything · <kbd>space</kbd> play/pause · <kbd>←</kbd> <kbd>→</kbd> skip · <kbd>shift</kbd>+<kbd>→</kbd> a minute · <kbd>&lt;</kbd> <kbd>&gt;</kbd> speed · <kbd>shift</kbd>+<kbd>N</kbd> next · <button type="button" className="link-btn" onClick={() => window.dispatchEvent(new CustomEvent('pp:shortcuts'))}>all shortcuts</button></p>
      </Section>
      <Section title="SponsorBlock" icon="youtube" sub="YouTube episodes come with the segments the SponsorBlock community marked. The player jumps over the categories you switch on; seek back into one and it stays.">
        <SettingRow title="Skip automatically"><Switch checked={f.auto_skip_sponsors} onChange={v => setF({ ...f, auto_skip_sponsors: v })} label="Skip automatically" /></SettingRow>
        {SB_CATEGORIES.map(c => (
          <SettingRow key={c.id} title={c.label} help={c.hint}><Switch size="sm" checked={cats.has(c.id)} onChange={() => toggle(c.id)} label={c.label} /></SettingRow>
        ))}
      </Section>
      <SaveBar onSave={() => save()} busy={busy} />
    </div>
  )
}

function Downloads() {
  const { f, setF, save, busy, isAdmin } = useSettingsForm()
  const { toast } = useToast()
  const [updating, setUpdating] = useState(false)
  if (!f) return <Loading />
  const updateYt = async () => {
    setUpdating(true)
    try { const r = await api.post<{ version: string }>(`${V1}/settings/ytdlp/update`); toast('yt-dlp is now ' + r.version); setF({ ...f, yt_dlp: r.version }) } catch (e) { toast((e as Error).message, 'err') } finally { setUpdating(false) }
  }
  return (
    <div className="stack loose">
      <Section title="Feeds" icon="rss">
        <SettingRow title="Check feeds every" help={`Minutes. 0 = the box default (${f.refresh_default} min). A podcast can set its own.`}><input type="number" min={0} value={f.refresh_minutes} onChange={e => setF({ ...f, refresh_minutes: Number(e.target.value) })} className="num" aria-label="Check feeds every (minutes)" /></SettingRow>
        <SettingRow title="Download new episodes of new subscriptions" help="Each podcast has its own switch afterwards."><Switch checked={f.auto_download_default} onChange={v => setF({ ...f, auto_download_default: v })} label="Download new episodes of new subscriptions" /></SettingRow>
        <SettingRow title="Keep files for" help="Days; the default for new subscriptions. 0 = keep. Saved and queued episodes are never removed."><input type="number" min={0} value={f.retention_days} onChange={e => setF({ ...f, retention_days: Number(e.target.value) })} className="num" aria-label="Keep files for (days)" /></SettingRow>
        <SettingRow title="Keep podcast files after playing" help="Otherwise a played podcast download is removed; YouTube files follow retention."><Switch checked={f.keep_played} onChange={v => setF({ ...f, keep_played: v })} label="Keep podcast files after playing" /></SettingRow>
      </Section>
      <Section title="YouTube" icon="youtube" sub="Channels and playlists are read and downloaded with yt-dlp on this box. YouTube changes often; when downloads start failing, update it here — no rebuild needed.">
        <SettingRow title="yt-dlp" help={f.yt_dlp ? <span className="mono">{f.yt_dlp}</span> : <span className="bad">not available</span>}>
          {isAdmin && f.yt_dlp_updatable && <button type="button" className="btn sm" onClick={updateYt} disabled={updating}><Icon name="refresh" size={14} />{updating ? 'Updating…' : 'Update now'}</button>}
        </SettingRow>
        <SettingRow title="Videos fetched when subscribing to a channel" help="The newest N; afterwards every upload."><input type="number" min={0} max={200} value={f.youtube_limit} onChange={e => setF({ ...f, youtube_limit: Number(e.target.value) })} className="num" aria-label="Videos fetched when subscribing" /></SettingRow>
        <SettingRow title="Skip videos shorter than" help="Seconds. Keeps Shorts out of new subscriptions."><input type="number" min={0} value={f.youtube_min_duration} onChange={e => setF({ ...f, youtube_min_duration: Number(e.target.value) })} className="num" aria-label="Skip videos shorter than (s)" /></SettingRow>
        <div className="callout mt-s"><Icon name="info" size={15} /><div className="stack tight">
          <span>{f.youtube_check_minutes > 0 ? <>New uploads are spotted within about <b>{f.youtube_check_minutes} minutes</b> through YouTube's RSS feed; a full check with yt-dlp also runs at the feed interval above, so nothing is missed when the RSS is down.</> : <>New uploads are found at the feed interval above.</>}</span>
          <span>{f.ytdlp_cookies ? <>A YouTube login is configured, so <b>members-only</b> videos of channels you are a paying member of are downloaded too.</> : <><b>Members-only</b> (“Premium”) videos are skipped: only a channel's paying members can get them. If you are a member, give the app your YouTube login cookies (<code>YTDLP_COOKIES</code>) and they download too.</>}</span>
        </div></div>
      </Section>
      <SaveBar onSave={() => save()} busy={busy} />
    </div>
  )
}

function Search() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const [secret, setSecret] = useState('')
  const [testing, setTesting] = useState(false)
  if (!f) return <Loading />
  const test = async () => {
    setTesting(true)
    try { await api.post(`${V1}/search/test`, { key: f.podcastindex_key, secret }); toast('Podcast Index answers') } catch (e) { toast((e as Error).message, 'err') } finally { setTesting(false) }
  }
  return (
    <div className="stack loose">
      <Section title="Directories" icon="search" sub={<>Apple's directory needs no key. <a href="https://api.podcastindex.org/" target="_blank" rel="noreferrer">Podcast Index</a> is free and open; with a key you also get trending podcasts.</>}>
        <SettingRow title="Search in by default"><Segmented size="sm" value={f.search_source} onChange={v => setF({ ...f, search_source: v })} options={[{ value: 'all', label: 'Both' }, { value: 'itunes', label: 'Apple' }, { value: 'podcastindex', label: 'Podcast Index' }]} /></SettingRow>
        <SettingRow title="Podcast Index API key" help={f.podcastindex_env && !f.podcastindex_key ? 'A key is set through PODCASTINDEX_KEY.' : undefined} stack><input value={f.podcastindex_key} onChange={e => setF({ ...f, podcastindex_key: e.target.value })} spellCheck={false} autoComplete="off" aria-label="Podcast Index API key" /></SettingRow>
        <SettingRow title="API secret" stack><input type="password" value={secret} onChange={e => setSecret(e.target.value)} placeholder={f.podcastindex_secret_set ? 'saved — type to replace' : ''} autoComplete="off" aria-label="API secret" /></SettingRow>
      </Section>
      <SaveBar onSave={() => save(secret ? { podcastindex_secret: secret } : {}).then(() => setSecret(''))} busy={busy}>
        <button type="button" className="btn" onClick={test} disabled={testing || !f.podcastindex_key}>{testing ? 'Testing…' : 'Test'}</button>
      </SaveBar>
    </div>
  )
}

function Notify() {
  const { f, setF, save, busy } = useSettingsForm()
  const { toast } = useToast()
  const [token, setToken] = useState('')
  const [testing, setTesting] = useState(false)
  if (!f) return <Loading />
  const hints: Record<string, string> = { apprise: 'Apprise API route, e.g. http://apprise:8000/notify/podcasts', ntfy: 'ntfy topic URL, e.g. https://ntfy.example.com/podcasts', webhook: 'Any URL that accepts a JSON POST {title, body, url}' }
  const test = async () => {
    setTesting(true)
    try { await api.post(`${V1}/settings/notify/test`, { kind: f.notify_kind, url: f.notify_url, token }); toast('Sent — check your phone') } catch (e) { toast((e as Error).message, 'err') } finally { setTesting(false) }
  }
  const on = !!(f.notify_url || f.notify_env)
  return (
    <div className="stack loose">
      <Section title="New episodes" icon="bell" actions={<span className={`badge ${on ? 'ok' : ''}`}>{on ? 'on' : 'not set'}</span>}
        sub={<>Switch on “Notify me” in a podcast's settings and a message goes here whenever it has new episodes.{f.notify_env && !f.notify_url && ' An address is set through NOTIFY_URL.'}</>}>
        <SettingRow title="Send through"><Segmented size="sm" value={f.notify_kind} onChange={v => setF({ ...f, notify_kind: v })} options={[{ value: 'apprise', label: 'Apprise' }, { value: 'ntfy', label: 'ntfy' }, { value: 'webhook', label: 'Webhook' }]} /></SettingRow>
        <SettingRow title="Address" help={hints[f.notify_kind]} stack><input value={f.notify_url} onChange={e => setF({ ...f, notify_url: e.target.value })} placeholder="http://…" spellCheck={false} aria-label="Address" /></SettingRow>
        <SettingRow title="Token (optional)" help="Sent as a bearer token (ntfy access token, Apprise key)." stack><input type="password" value={token} onChange={e => setToken(e.target.value)} placeholder={f.notify_token_set ? 'saved — type to replace' : ''} autoComplete="off" aria-label="Token" /></SettingRow>
      </Section>
      <SaveBar onSave={() => save(token ? { notify_token: token } : {}).then(() => setToken(''))} busy={busy}>
        <button type="button" className="btn" onClick={test} disabled={testing || !on}><Icon name="mail" size={15} />{testing ? 'Sending…' : 'Send a test'}</button>
      </SaveBar>
    </div>
  )
}

function Feeds() {
  const { f, setF, save, busy } = useSettingsForm()
  const { setSettings } = useApp()
  const { toast } = useToast()
  const pods = useAsync(() => api.get<Podcast[]>(`${V1}/podcasts?sort=title`), [])
  const [rotate, setRotate] = useState(false)
  if (!f) return <Loading />
  const copy = async (t: string) => toast(await copyText(t) ? 'Copied' : 'Could not copy', 'ok')
  return (
    <div className="stack loose">
      <Section title="Your feeds" icon="rss" sub="Every subscription is also a podcast feed served by this box — including YouTube channels as audio, with the files, artwork and chapters. Subscribe to them in Apple Podcasts, Overcast, AntennaPod or any app. The key in the URL is your secret: anyone with it can listen.">
        <div className="stack">
          <div className="row"><div className="grow"><CopyField value={f.feed_url} label="Your feed address" /></div><a className="btn sm" href={f.feed_url} target="_blank" rel="noreferrer"><Icon name="external" size={14} />Open</a></div>
          <Field label="Public address (optional)" help="If feeds are reached through another hostname (a public tunnel), put its base URL here; it is used inside the feeds."><input value={f.feed_base_url} onChange={e => setF({ ...f, feed_base_url: e.target.value })} placeholder="https://feeds.example.com" spellCheck={false} /></Field>
        </div>
      </Section>
      <SaveBar onSave={() => save()} busy={busy}>
        <button type="button" className="btn ghost danger-text" onClick={() => setRotate(true)}><Icon name="refresh" size={14} />New key…</button>
      </SaveBar>
      <Section title="Feed URLs" sub="One per subscription.">
        {pods.data ? (pods.data.length === 0 ? <p className="muted">No subscriptions yet.</p> : (
          <div className="set-people">{pods.data.map(p => (
            <div key={p.id} className="set-person feed-row">
              <div className="grow"><div className="set-person-name ellipsis">{p.title}</div><code className="mono small muted feed-url">{f.feed_url}{p.id}.xml</code></div>
              <button type="button" className="btn ghost sm" onClick={() => copy(`${f.feed_url}${p.id}.xml`)}><Icon name="copy" size={14} />Copy</button>
            </div>))}
          </div>)) : <Loading />}
      </Section>
      {rotate && <Confirm title="Make a new feed key?" message="Every feed URL you gave to a podcast app stops working; you will have to subscribe again with the new ones." confirmLabel="Make a new key" onCancel={() => setRotate(false)} onConfirm={async () => { const s = await api.post<SettingsT>(`${V1}/settings/feed-key`); setSettings(s); setF(s); setRotate(false); toast('New key made') }} />}
    </div>
  )
}

function Users() {
  const { user, users, reloadMeta, status } = useApp()
  const { toast } = useToast()
  const [edit, setEdit] = useState<Partial<User> | null>(null)
  const [del, setDel] = useState<User | null>(null)
  const isAdmin = user?.role === 'admin'
  const run = async (fn: () => Promise<unknown>, msg: string) => { try { await fn(); await reloadMeta(); toast(msg) } catch (e) { toast((e as Error).message, 'err') } }
  return (
    <div className="stack loose">
      <Section title="Accounts" icon="users" sub="Everyone signs in with their own account and keeps their own subscriptions, queue, history, playlists, files and settings — admins included. Admins manage the accounts and the app title."
        actions={isAdmin && <button type="button" className="btn primary sm" onClick={() => setEdit({ role: 'member' })}><Icon name="plus" size={14} />Add person</button>}>
        <div className="set-people">
          {users.map(u => (
            <div key={u.id} className="set-person">
              <Avatar name={u.name} email={u.email} size={34} />
              <div className="grow">
                <div className="set-person-name">{u.name || <span className="muted">—</span>}{u.id === user?.id && <span className="badge">you</span>}<span className={`badge ${u.role === 'admin' ? 'accent' : ''}`}>{u.role}</span></div>
                <div className="muted small ellipsis">{u.email}</div>
              </div>
              {isAdmin && <div className="row">
                <button type="button" className="btn ghost sm" onClick={() => setEdit(u)}><Icon name="edit" size={14} />Edit</button>
                {u.id !== user?.id && <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(u)}>Remove</button>}
              </div>}
            </div>
          ))}
        </div>
        {status?.auth_mode !== 'local' && <p className="muted small mt">Auth mode is <code>{status?.auth_mode}</code>: people are created automatically on first visit; passwords are not used here.</p>}
      </Section>
      {edit && <UserModal user={edit} local={status?.auth_mode === 'local'} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reloadMeta() }} />}
      {del && <Confirm title={`Remove ${del.name || del.email}?`} message="Their account is deleted together with their subscriptions, history and downloaded files." confirmLabel="Remove" onCancel={() => setDel(null)} onConfirm={() => { const d = del; setDel(null); run(() => api.del(`${V1}/users/${d.id}`), 'Person removed') }} />}
    </div>
  )
}

function UserModal({ user, local, onClose, onSaved }: { user: Partial<User>; local: boolean; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const [f, setF] = useState({ name: user.name || '', email: user.email || '', password: '', role: user.role || 'member' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (user.id) await api.put(`${V1}/users/${user.id}`, f); else await api.post(`${V1}/users`, f); toast(user.id ? 'Saved' : 'Person added'); onSaved() } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <Modal title={user.id ? 'Edit person' : 'Add a person'} onClose={onClose}>
      <form onSubmit={save} className="stack">
        <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} autoFocus required maxLength={60} /></Field>
        <Field label="Email" help="Used to sign in."><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} required /></Field>
        {local && <Field label={user.id ? 'New password (leave empty to keep)' : 'Password'} help="At least 8 characters."><input type="password" value={f.password} onChange={e => setF({ ...f, password: e.target.value })} minLength={user.id ? 0 : 8} required={!user.id} autoComplete="new-password" /></Field>}
        <Field group label="Role"><Segmented value={f.role} onChange={v => setF({ ...f, role: v })} options={[{ value: 'member', label: 'Member' }, { value: 'admin', label: 'Admin' }]} /></Field>
        <div className="form-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button type="submit" className="btn primary" disabled={busy}>{user.id ? 'Save' : 'Add person'}</button></div>
      </form>
    </Modal>
  )
}

function Profile() {
  const { user, setUser, status } = useApp()
  const { toast } = useToast()
  const [f, setF] = useState({ name: user?.name || '', email: user?.email || '', current_password: '', new_password: '' })
  const [busy, setBusy] = useState(false)
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { setUser(await api.put(`${V1}/auth/me`, f)); setF(x => ({ ...x, current_password: '', new_password: '' })); toast('Profile saved') } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <form onSubmit={save} className="stack loose">
      <Section title="You" icon="user">
        <div className="set-profile">
          <Avatar name={f.name || user?.name} email={user?.email} size={56} />
          <div className="stack grow">
            <Field label="Name"><input value={f.name} onChange={e => setF({ ...f, name: e.target.value })} /></Field>
            <Field label="Email"><input type="email" value={f.email} onChange={e => setF({ ...f, email: e.target.value })} /></Field>
          </div>
        </div>
      </Section>
      {status?.auth_mode === 'local' && (
        <Section title="Password" icon="lock" sub="Leave the new password empty to keep the current one.">
          <div className="two-col">
            <Field label="Current password"><input type="password" value={f.current_password} onChange={e => setF({ ...f, current_password: e.target.value })} autoComplete="current-password" /></Field>
            <Field label="New password" help="At least 8 characters."><input type="password" value={f.new_password} onChange={e => setF({ ...f, new_password: e.target.value })} autoComplete="new-password" /></Field>
          </div>
        </Section>
      )}
      <div className="row"><button type="submit" className="btn primary" disabled={busy}>{busy ? 'Saving…' : 'Save changes'}</button></div>
    </form>
  )
}

function Tokens() {
  const { toast } = useToast()
  const { data, reload } = useAsync(() => api.get<APIToken[]>(`${V1}/tokens`), [])
  const [name, setName] = useState('')
  const [fresh, setFresh] = useState('')
  const [del, setDel] = useState<APIToken | null>(null)
  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    try { const r = await api.post<{ token: string }>(`${V1}/tokens`, { name: name || 'token' }); setFresh(r.token); setName(''); reload() } catch (e) { toast((e as Error).message, 'err') }
  }
  return (
    <div className="stack loose">
      <Section title="Your tokens" icon="key" sub={<>Use as <code>Authorization: Bearer &lt;token&gt;</code>. A token acts as you. Good for a Homepage widget (<code>GET /api/v1/dashboard</code>), an iOS Shortcut that queues an episode, or the PinePods importer. See <code>docs/api.md</code>.</>}>
        <form onSubmit={create} className="row"><input value={name} onChange={e => setName(e.target.value)} placeholder="Token name (e.g. Homepage)" aria-label="Token name" /><button type="submit" className="btn primary">Create</button></form>
        {fresh && <div className="set-fresh"><div className="row"><Icon name="check-circle" size={16} /><b>Copy it now — it is shown once.</b></div><CopyField value={fresh} label="New token" /></div>}
        <div className="set-people mt">
          {data && data.length > 0 ? data.map(t => (
            <div key={t.id} className="set-person">
              <span className="set-token-ico"><Icon name="key" size={15} /></span>
              <div className="grow"><div className="set-person-name">{t.name}<code className="mono small muted">{t.prefix}…</code></div><div className="muted small">Last used {t.last_used_at ? ago(t.last_used_at) : 'never'} · created {t.created_at.slice(0, 10)}</div></div>
              <button type="button" className="btn ghost sm danger-text" onClick={() => setDel(t)}>Revoke</button>
            </div>
          )) : data ? <p className="muted">No tokens yet.</p> : <Loading />}
        </div>
      </Section>
      {del && <Confirm title={`Revoke “${del.name}”?`} message="Anything using this token stops working immediately." confirmLabel="Revoke" onCancel={() => setDel(null)} onConfirm={async () => { await api.del(`${V1}/tokens/${del.id}`); setDel(null); reload() }} />}
    </div>
  )
}

function Data() {
  const { toast } = useToast()
  const { user } = useApp()
  const [busy, setBusy] = useState(false)
  const importOPML = async (file: File) => {
    setBusy(true)
    try { const r = await upload<{ added: number; skipped: number; failed: string[] }>(`${V1}/import/opml`, 'file', file); toast(`${r.added} added, ${r.skipped} already followed${r.failed.length ? `, ${r.failed.length} failed` : ''}`); if (r.failed.length) console.warn(r.failed) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  const importJSON = async (file: File) => {
    setBusy(true)
    try { const r = await api.post<{ added: number; existing: number; progress: number; failed: string[] }>(`${V1}/import`, JSON.parse(await file.text())); toast(`${r.added} subscriptions added, ${r.progress} episode positions restored${r.failed.length ? `, ${r.failed.length} failed` : ''}`) } catch (e) { toast((e as Error).message, 'err') } finally { setBusy(false) }
  }
  return (
    <div className="stack loose">
      {user?.role === 'admin' && (
        <Section title="Backup" icon="database" sub={<>A consistent copy of the SQLite database (<code>VACUUM INTO</code>) — every account, so only admins can take it. Media files live next to it in <code>media/</code>, artwork in <code>artwork/</code>. Restore by stopping the app and replacing <code>pocket-podcasts.db</code>.</>}
          actions={<a className="btn primary sm" href={`${V1}/backup`}><Icon name="download" size={14} />Download database</a>} />
      )}
      <Section title="Export" icon="upload" sub="OPML carries subscriptions and works with every podcast app. JSON also carries your progress, saved and queued episodes and playlists, and can be imported again here.">
        <div className="row wrap"><a className="btn" href={`${V1}/export.opml`}><Icon name="download" size={15} />Export OPML</a><a className="btn" href={`${V1}/export.json`}><Icon name="download" size={15} />Export JSON</a></div>
      </Section>
      <Section title="Import" icon="download" sub={<>Coming from PinePods? <code>scripts/import-pinepods.py</code> reads its database and produces a JSON file for this import, with your history and positions.</>}>
        <div className="row wrap">
          <label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Import OPML…'}<input type="file" accept=".opml,.xml,text/xml,text/x-opml" hidden onChange={e => e.target.files?.[0] && importOPML(e.target.files[0])} /></label>
          <label className="btn"><Icon name="upload" size={15} />{busy ? 'Importing…' : 'Import JSON…'}<input type="file" accept="application/json" hidden onChange={e => e.target.files?.[0] && importJSON(e.target.files[0])} /></label>
        </div>
      </Section>
    </div>
  )
}

function ActivityTab() {
  const [kind, setKind] = useState('')
  const { data, loading } = useAsync(() => api.get<Activity[]>(`${V1}/activity?kind=${kind}&limit=200`), [kind])
  return (
    <Section title="Last 90 days" icon="activity" actions={<Segmented size="sm" value={kind} onChange={setKind} options={[{ value: '', label: 'All' }, { value: 'refresh', label: 'New episodes' }, { value: 'download', label: 'Downloads' }, { value: 'error', label: 'Errors' }]} />}
      sub="Feed refreshes with new episodes, downloads, files removed by retention, notifications and errors.">
      {loading && !data ? <Loading /> : !data || data.length === 0 ? <p className="muted">Nothing yet.</p> : (
        <ul className="activity">{data.map(a => <li key={a.id}><span className={`k ${a.kind}`}>{a.kind}</span><span className="grow">{a.message}</span><span className="when" title={a.created_at}>{ago(a.created_at)}</span></li>)}</ul>
      )}
    </Section>
  )
}

function About() {
  const { data } = useAsync(() => api.get<Record<string, unknown>>(`${V1}/settings/system`), [])
  const counts = (data?.counts || {}) as Record<string, number>
  const core = (data?.core || {}) as { configured: boolean; url: string; registered: boolean; error: string }
  const yc = data?.youtube_check as { every_minutes: number; ok: number; failing: number; last_ok: string } | undefined
  return (
    <div className="stack loose">
      <section className="set-section">
        <div className="set-section-body stack" style={{ paddingTop: 18 }}>
        <div className="set-about-hero"><img src="/favicon.svg" alt="" width={44} height={44} /><div><h3>Pocket Podcasts</h3><p className="muted small">{data ? `Version ${String(data.version)}` : ' '}</p></div></div>
        <p>A self-hosted podcast server: subscriptions, a player that remembers where you were, smart playlists, downloads on this box, YouTube channels as audio podcasts with SponsorBlock, and public feeds for any podcast app. One binary, one SQLite file, one volume.</p>
        <p className="muted small">Open source (MIT) — <a href="https://github.com/abjelosevic88/pocket-homelab-podcasts" target="_blank" rel="noreferrer">github.com/abjelosevic88/pocket-homelab-podcasts</a>. One of the Pocket apps, alongside Bookshelf, Day, Habits, Kitchen and the rest.</p>
        </div>
      </section>
      {data && (
        <div className="set-stats">
          <div className="set-stat"><Icon name="podcast" size={17} /><span className="set-stat-n">{counts.podcasts ?? 0}</span><span className="set-stat-l">podcasts</span></div>
          <div className="set-stat"><Icon name="list" size={17} /><span className="set-stat-n">{counts.episodes ?? 0}</span><span className="set-stat-l">episodes</span></div>
          <div className="set-stat"><Icon name="download" size={17} /><span className="set-stat-n">{counts.downloaded ?? 0}</span><span className="set-stat-l">downloaded</span></div>
          <div className="set-stat"><Icon name="check-circle" size={17} /><span className="set-stat-n">{counts.played ?? 0}</span><span className="set-stat-l">played</span></div>
        </div>
      )}
      <Section title="The server" icon="server">
        {!data ? <Loading /> : (
          <dl className="set-dl">
            <div><dt>Version</dt><dd className="mono">{String(data.version)}</dd></div>
            <div><dt>Runtime</dt><dd className="mono">{String(data.go)} · {String(data.os)}/{String(data.arch)}</dd></div>
            <div><dt>yt-dlp</dt><dd className="mono">{data.yt_dlp ? String(data.yt_dlp) : <span className="bad">not available</span>}</dd></div>
            {yc && yc.every_minutes > 0 && <div><dt>New uploads</dt><dd className="mono">RSS check every {yc.every_minutes} min · {yc.ok} channel{yc.ok === 1 ? '' : 's'} OK{yc.failing > 0 && ` · ${yc.failing} failing (covered by the full check)`}{yc.last_ok && ` · last ${ago(yc.last_ok)}`}</dd></div>}
            <div><dt>Auth mode</dt><dd className="mono">{String(data.auth_mode)}</dd></div>
            <div><dt>Database</dt><dd className="mono">{String(data.db_path)} · {bytes(Number(data.db_bytes))}</dd></div>
            <div><dt>Media</dt><dd className="mono">{String(data.media_dir)} · {String(data.media_files)} files · {bytes(Number(data.media_bytes))}</dd></div>
            <div><dt>Feeds</dt><dd className="mono">checked every {String(data.refresh_minutes)} min by default · {String(data.download_workers)} download worker{Number(data.download_workers) === 1 ? '' : 's'}</dd></div>
            <div><dt>Timezone</dt><dd className="mono">{String(data.timezone)}</dd></div>
            <div><dt>Uptime</dt><dd className="mono">{String(data.uptime)}</dd></div>
            <div><dt>Pocket Core</dt><dd className="mono">{core.configured ? (core.registered ? `registered at ${core.url}` : `not registered: ${core.error}`) : 'not configured'}</dd></div>
            {counts.users !== undefined && <div><dt>Accounts</dt><dd className="mono">{counts.users} on this box</dd></div>}
          </dl>
        )}
      </Section>
    </div>
  )
}
