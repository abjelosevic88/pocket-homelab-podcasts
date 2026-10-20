// Takes the README screenshots over the Chrome DevTools protocol against a local instance on :18181.
// Run: docker run -d --name ppchrome --network host --shm-size=1g zenika/alpine-chrome:124 --no-sandbox --headless --disable-gpu --disable-dev-shm-usage --hide-scrollbars --remote-debugging-port=9222 --remote-debugging-address=127.0.0.1 --remote-allow-origins=* --autoplay-policy=no-user-gesture-required --window-size=1280,900 about:blank
//      EMAIL=… PASSWORD=… node scripts/screenshots.mjs   (SHOTS=home,player limits it); output in /tmp/ppcdp/out
import fs from 'node:fs'
const BASE = 'http://127.0.0.1:18181'
const log = (...a) => console.log(new Date().toISOString().slice(11,19), ...a), OUT = '/tmp/ppcdp/out', [email, password] = [process.env.EMAIL, process.env.PASSWORD]
fs.mkdirSync(OUT, { recursive: true })
const login = await fetch(BASE + '/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, password }) })
log('login', login.status)
const cookie = login.headers.get('set-cookie').match(/pp_session=([^;]+)/)[1]
const api = async p => (await fetch(BASE + p, { headers: { Cookie: 'pp_session=' + cookie } })).json()
const sb = (await api('/api/v1/episodes?downloaded=1&limit=200')).episodes.find(e => e.has_sponsors) || (await api('/api/v1/episodes?limit=1')).episodes[0]
const yt = (await api('/api/v1/podcasts')).find(p => p.kind === 'youtube' && p.downloaded_count > 0)
const pls = await api('/api/v1/playlists')

log('data ready', sb?.id, yt?.id, pls.length)
const ver = await (await fetch('http://127.0.0.1:9222/json/version')).json()
const ws = new WebSocket(ver.webSocketDebuggerUrl)
await new Promise(r => ws.onopen = r)
let id = 0; const waits = new Map(); const events = []
ws.onmessage = m => { const d = JSON.parse(m.data); if (d.id && waits.has(d.id)) { waits.get(d.id)(d); waits.delete(d.id) } else events.push(d) }
const send = (method, params = {}, sessionId) => new Promise((res, rej) => { const i = ++id; waits.set(i, res); const t = setTimeout(() => rej(new Error('timeout ' + method)), 30000); ws.send(JSON.stringify({ id: i, method, params, sessionId })); waits.set(i, d => { clearTimeout(t); res(d) }) })
const { result: { targetId } } = await send('Target.createTarget', { url: 'about:blank' })
const { result: { sessionId } } = await send('Target.attachToTarget', { targetId, flatten: true })
const s = (m, p) => send(m, p, sessionId)
log('attached', sessionId)
await s('Page.enable'); await s('Network.enable'); await s('Runtime.enable')
await s('Network.setCookie', { name: 'pp_session', value: cookie, url: BASE, path: '/' })
const sleep = ms => new Promise(r => setTimeout(r, ms))
const evalJS = async expr => (await s('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true })).result?.result?.value
const ONLY = (process.env.SHOTS || '').split(',').filter(Boolean)
let themeScript = null
async function shot(name, path, { w = 1280, h = 900, dark = false, mobile = false, before = null, wait = 2500 } = {}) {
  if (ONLY.length && !ONLY.includes(name)) return
  log('start', name)
  await s('Emulation.setDeviceMetricsOverride', { width: w, height: h, deviceScaleFactor: mobile ? 2 : 1, mobile })
  if (themeScript) await s('Page.removeScriptToEvaluateOnNewDocument', { identifier: themeScript })
  themeScript = (await s('Page.addScriptToEvaluateOnNewDocument', { source: `localStorage.setItem('pp-theme', '${dark ? 'dark' : 'light'}'); localStorage.removeItem('pp-last-episode')` })).result.identifier
  await s('Page.navigate', { url: BASE + path }); await sleep(wait)
  if (before) { await before(); await sleep(1200) }
  const cap = await s('Page.captureScreenshot', { format: 'png' })
  if (!cap.result) { log('capture failed', JSON.stringify(cap).slice(0, 300)); return }
  const { result: { data } } = cap
  fs.writeFileSync(`${OUT}/${name}.png`, Buffer.from(data, 'base64')); console.log('shot', name)
}
const click = async sel => evalJS(`(() => { const el = document.querySelector(${JSON.stringify(sel)}); if (!el) return 'missing ' + ${JSON.stringify(sel)}; el.click(); return 'ok' })()`)
await shot('home', '/home')
await shot('podcasts', '/podcasts')
await shot('podcast', `/podcasts/${yt.id}`, { wait: 3500 })
await shot('episode', `/episodes/${sb.id}`)
await shot('player', `/episodes/${sb.id}`, { before: async () => { console.log(await click('.ep-side .btn.primary')); await sleep(2500); console.log(await click('.pb-info')) }, wait: 2500 })
await shot('queue', '/queue')
await shot('playlists', '/playlists')
await shot('playlist', `/playlists/${pls[0].id}`)
await shot('add', '/add', { before: async () => { await click('.search-bar input'); await s('Input.insertText', { text: 'lex fridman' }); await sleep(3500) } })
await shot('add-youtube', '/add?tab=youtube')
await shot('add-pick', '/add?tab=url&url=' + encodeURIComponent('https://lexfridman.com/feed/podcast/'), { wait: 16000 })
await shot('downloads', '/downloads')
await shot('settings-feeds', '/settings?tab=feeds')
await shot('episodes-dark', '/episodes', { dark: true })
await shot('mobile-home', '/home', { w: 390, h: 844, mobile: true })
await shot('mobile-player', `/episodes/${sb.id}`, { w: 390, h: 844, mobile: true, before: async () => { await click('.ep-side .btn.primary'); await sleep(2500); await click('.pb-info') } })
ws.close(); process.exit(0)
