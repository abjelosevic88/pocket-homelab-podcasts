import { api, V1 } from './api'
import type { YTChannel, YTItem } from './types'

export type YTKind = 'all' | 'channel' | 'video' | 'playlist'
export const YT_KINDS: { value: YTKind; label: string }[] = [
  { value: 'all', label: 'All of YouTube' },
  { value: 'channel', label: 'Channels' },
  { value: 'video', label: 'Videos' },
  { value: 'playlist', label: 'Playlists' },
]
export type YTHit = { kind: 'channel'; key: string; c: YTChannel } | { kind: 'video' | 'playlist'; key: string; v: YTItem }

const ytGet = <T,>(type: string, q: string, limit: number) => api.get<{ results: T[] }>(`${V1}/youtube/search?type=${type}&q=${encodeURIComponent(q)}&limit=${limit}`).then(r => r.results).catch(() => [] as T[])

/** Searches YouTube for one kind, or all three at once mixed into one list:
 *  the best channels first, then videos with playlists in between. */
export async function searchYouTube(q: string, kind: YTKind, small = false): Promise<YTHit[]> {
  const ch = (cs: YTChannel[]): YTHit[] => cs.map(c => ({ kind: 'channel', key: 'c' + c.id, c }))
  const it = (k: 'video' | 'playlist', vs: YTItem[]): YTHit[] => vs.map(v => ({ kind: k, key: k[0] + v.id, v }))
  if (kind === 'channel') return ch(await ytGet<YTChannel>('channel', q, 12))
  if (kind === 'video') return it('video', await ytGet<YTItem>('video', q, 20))
  if (kind === 'playlist') return it('playlist', await ytGet<YTItem>('playlist', q, 12))
  const [cs, vs, ps] = await Promise.all([ytGet<YTChannel>('channel', q, 12), ytGet<YTItem>('video', q, 20), ytGet<YTItem>('playlist', q, 12)])
  const channels = ch(cs), videos = it('video', vs), playlists = it('playlist', ps)
  const out: YTHit[] = [...channels.slice(0, small ? 1 : 2)]
  let pi = 0
  videos.slice(0, small ? 4 : 20).forEach((v, i) => { out.push(v); if (i % 4 === 2 && pi < (small ? 1 : 4) && playlists[pi]) out.push(playlists[pi++]) })
  if (!small) out.push(...channels.slice(2, 5), ...playlists.slice(pi, pi + 2))
  return out
}

