import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Playlist, Podcast } from '../lib/types'
import { Card, Confirm, Empty, Field, Loading, Modal, PageHeader, Segmented, useAsync, useToast } from '../components/ui'
import { fmtDur } from '../lib/format'

export default function Playlists() {
  const { data, loading, reload } = useAsync(() => api.get<Playlist[]>(`${V1}/playlists`), [])
  const [edit, setEdit] = useState<Partial<Playlist> | null>(null)
  if (loading && !data) return <Loading />
  return (
    <>
      <PageHeader title="Playlists" sub="Smart lists: a filter over your episodes that is always up to date." actions={<button className="btn primary" onClick={() => setEdit({})}>+ New playlist</button>} />
      {!data || data.length === 0 ? <Empty title="No playlists" /> : (
        <div className="pl-grid">
          {data.map(p => (
            <Link key={p.id} to={`/playlists/${p.id}`} className="card pl-card">
              <span className="pl-icon">{p.icon || '✦'}</span>
              <div className="info"><div className="nm">{p.name}</div><div className="muted small">{p.description || describe(p)}</div></div>
              <span className="count-pill inline">{p.count}</span>
              <button className="btn ghost sm" onClick={e => { e.preventDefault(); setEdit(p) }} title="Edit">✎</button>
            </Link>
          ))}
        </div>
      )}
      {edit && <PlaylistEditor pl={edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); reload() }} />}
    </>
  )
}

export function describe(p: Playlist): string {
  const parts: string[] = []
  const st = [p.include_unplayed && 'unplayed', p.include_partial && 'in progress', p.include_played && 'played'].filter(Boolean)
  if (st.length && st.length < 3) parts.push(st.join(' + '))
  if (p.podcast_ids.length) parts.push(`${p.podcast_ids.length} podcast${p.podcast_ids.length === 1 ? '' : 's'}`)
  if (p.min_duration) parts.push(`over ${fmtDur(p.min_duration)}`)
  if (p.max_duration) parts.push(`under ${fmtDur(p.max_duration)}`)
  if (p.max_age_days) parts.push(`last ${p.max_age_days} days`)
  if (p.downloaded_only) parts.push('on disk')
  return parts.join(' · ') || 'everything'
}

export function PlaylistEditor({ pl, onClose, onSaved }: { pl: Partial<Playlist>; onClose: () => void; onSaved: () => void }) {
  const { toast } = useToast()
  const pods = useAsync(() => api.get<Podcast[]>(`${V1}/podcasts?sort=title`), [])
  const [f, setF] = useState<Playlist>({ id: 0, user_id: 0, name: '', description: '', icon: '✦', podcast_ids: [], include_unplayed: true, include_partial: true, include_played: false, min_duration: 0, max_duration: 0, max_age_days: 0, downloaded_only: false, sort: 'newest', limit: 0, pos: 0, created_at: '', count: 0, ...pl })
  const [busy, setBusy] = useState(false)
  const [del, setDel] = useState(false)
  const set = (k: keyof Playlist, v: unknown) => setF(x => ({ ...x, [k]: v }))
  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true)
    try { if (f.id) await api.put(`${V1}/playlists/${f.id}`, f); else await api.post(`${V1}/playlists`, f); toast('Saved'); onSaved() } catch (err) { toast((err as Error).message, 'err') } finally { setBusy(false) }
  }
  const togglePod = (id: number) => set('podcast_ids', f.podcast_ids.includes(id) ? f.podcast_ids.filter(x => x !== id) : [...f.podcast_ids, id])
  const mins = (v: number) => Math.round(v / 60)
  return (
    <Modal title={f.id ? 'Edit playlist' : 'New playlist'} onClose={onClose} size="lg">
      <form onSubmit={save} className="grid" style={{ gap: 14 }}>
        <div className="row"><input value={f.icon} onChange={e => set('icon', e.target.value.slice(0, 2))} style={{ width: 56, textAlign: 'center', fontSize: 18 }} aria-label="Icon" /><input value={f.name} onChange={e => set('name', e.target.value)} placeholder="Name" required autoFocus /></div>
        <Field label="Description (optional)"><input value={f.description} onChange={e => set('description', e.target.value)} /></Field>
        <div className="two-col">
          <Card title="Include">
            <div className="grid" style={{ gap: 8 }}>
              <label className="row"><input type="checkbox" checked={f.include_unplayed} onChange={e => set('include_unplayed', e.target.checked)} style={{ width: 'auto' }} /> Unplayed</label>
              <label className="row"><input type="checkbox" checked={f.include_partial} onChange={e => set('include_partial', e.target.checked)} style={{ width: 'auto' }} /> In progress</label>
              <label className="row"><input type="checkbox" checked={f.include_played} onChange={e => set('include_played', e.target.checked)} style={{ width: 'auto' }} /> Played</label>
              <label className="row"><input type="checkbox" checked={f.downloaded_only} onChange={e => set('downloaded_only', e.target.checked)} style={{ width: 'auto' }} /> Only episodes on disk</label>
              <div className="two-col tight">
                <Field label="Min length (min)"><input type="number" min={0} value={mins(f.min_duration)} onChange={e => set('min_duration', Number(e.target.value) * 60)} className="num" /></Field>
                <Field label="Max length (min)"><input type="number" min={0} value={mins(f.max_duration)} onChange={e => set('max_duration', Number(e.target.value) * 60)} className="num" /></Field>
              </div>
              <Field label="Published in the last (days)" help="0 = any time"><input type="number" min={0} value={f.max_age_days} onChange={e => set('max_age_days', Number(e.target.value))} className="num" /></Field>
              <Field group label="Order"><Segmented size="sm" value={f.sort} onChange={v => set('sort', v)} options={[{ value: 'newest', label: 'Newest' }, { value: 'oldest', label: 'Oldest' }, { value: 'shortest', label: 'Shortest' }, { value: 'longest', label: 'Longest' }]} /></Field>
              <Field label="At most (episodes)" help="0 = no limit"><input type="number" min={0} value={f.limit} onChange={e => set('limit', Number(e.target.value))} className="num" /></Field>
            </div>
          </Card>
          <Card title={f.podcast_ids.length ? `Podcasts (${f.podcast_ids.length} chosen)` : 'Podcasts (all)'}>
            <p className="muted small">Leave all unticked for every subscription.</p>
            <div className="pick-list">
              {(pods.data || []).map(p => <label key={p.id} className="row"><input type="checkbox" checked={f.podcast_ids.includes(p.id)} onChange={() => togglePod(p.id)} style={{ width: 'auto' }} /> <span className="grow" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{p.title}</span></label>)}
            </div>
          </Card>
        </div>
        <div className="form-actions">
          {f.id > 0 && <button type="button" className="btn ghost danger-text" style={{ marginRight: 'auto' }} onClick={() => setDel(true)}>Delete</button>}
          <button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn primary" disabled={busy}>Save</button>
        </div>
      </form>
      {del && <Confirm title={`Delete “${f.name}”?`} message="Only the list goes; episodes stay." onCancel={() => setDel(false)} onConfirm={async () => { await api.del(`${V1}/playlists/${f.id}`); onSaved() }} />}
    </Modal>
  )
}
