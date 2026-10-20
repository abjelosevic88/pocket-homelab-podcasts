import { Link } from 'react-router-dom'
import { api, V1 } from '../lib/api'
import type { Stats as StatsT } from '../lib/types'
import { Card, Loading, PageHeader, useAsync } from '../components/ui'
import { fmtDate, hours } from '../lib/format'

export default function Stats() {
  const { data: s, loading } = useAsync(() => api.get<StatsT>(`${V1}/stats`), [])
  if (loading || !s) return <Loading />
  const maxDay = Math.max(1, ...s.days.map(d => d.seconds))
  const maxMonth = Math.max(1, ...s.months.map(d => d.seconds))
  const maxHour = Math.max(1, ...s.hours)
  const maxWd = Math.max(1, ...s.weekdays)
  const dayNames = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
  return (
    <>
      <PageHeader title="Stats" sub={s.first_listen ? `Listening since ${fmtDate(s.first_listen)}` : 'Nothing played yet'} />
      <div className="stats">
        <div className="stat"><div className="n">{hours(s.total_seconds)}</div><div className="l">listened in total</div></div>
        <div className="stat"><div className="n">{hours(s.year_seconds)}</div><div className="l">this year</div></div>
        <div className="stat"><div className="n">{hours(s.week_seconds)}</div><div className="l">last 7 days</div></div>
        <div className="stat"><div className="n">{s.completed}</div><div className="l">episodes finished{s.completed_year ? ` · ${s.completed_year} this year` : ''}</div></div>
        <div className="stat"><div className="n">{s.streak}</div><div className="l">day streak</div></div>
        <div className="stat"><div className="n">{s.active_days}</div><div className="l">days with listening</div></div>
        <div className="stat"><div className="n">{s.podcasts}</div><div className="l">podcasts · {s.episodes} episodes</div></div>
        <div className="stat"><div className="n">{s.downloaded}</div><div className="l">files on disk · {(s.downloaded_mb / 1024).toFixed(1)} GB</div></div>
      </div>
      <div className="two-col">
        <Card title="Last 90 days">
          <div className="spark">{s.days.map(d => <span key={d.day} title={`${d.day}: ${Math.round(d.seconds / 60)} min`} style={{ height: `${Math.max(2, (d.seconds / maxDay) * 100)}%` }} className={d.seconds ? 'on' : ''} />)}</div>
        </Card>
        <Card title="Last 12 months">
          <div className="month-bars">{s.months.map(m => <div key={m.day} className="mb-col"><span className="v">{m.seconds ? hours(m.seconds).replace(' h', '') : ''}</span><span className="b" style={{ height: `${Math.max(2, (m.seconds / maxMonth) * 100)}%` }} /><span className="l">{m.day.slice(5)}</span></div>)}</div>
        </Card>
        <Card title="Time of day">
          <div className="hour-bars">{s.hours.map((n, h) => <div key={h} className="mb-col" title={`${h}:00 — ${n} plays`}><span className="b" style={{ height: `${Math.max(2, (n / maxHour) * 100)}%` }} />{h % 6 === 0 && <span className="l">{h}h</span>}</div>)}</div>
        </Card>
        <Card title="Day of week">
          <div className="rating-bars">{s.weekdays.map((n, i) => <div key={i} className="rb-row"><span className="l">{dayNames[i]}</span><span className="track"><span className="b" style={{ width: `${(n / maxWd) * 100}%` }} /></span><span className="n">{hours(n)}</span></div>)}</div>
        </Card>
        <Card title="Most listened podcasts">
          {s.top_podcasts.length === 0 ? <p className="muted">—</p> : <ol className="rank">{s.top_podcasts.map(p => <li key={p.id}><Link to={`/podcasts/${p.id}`}>{p.title}</Link><span className="muted">{hours(p.seconds)} · {p.episodes} ep.</span></li>)}</ol>}
        </Card>
        <Card title="Most listened episodes">
          {s.top_episodes.length === 0 ? <p className="muted">—</p> : <ol className="rank">{s.top_episodes.map(e => <li key={e.id}><Link to={`/episodes/${e.id}`} className="grow" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{e.title}</Link><span className="muted small" style={{ whiteSpace: 'nowrap' }}>{hours(e.seconds)}</span></li>)}</ol>}
        </Card>
      </div>
    </>
  )
}
