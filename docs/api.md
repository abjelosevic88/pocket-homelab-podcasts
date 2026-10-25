# API

Base path `/api/v1`. JSON in and out. Authenticate with the browser session or `Authorization: Bearer <token>` (Settings → API tokens; a token acts as the person who made it). Pocket Core device tokens (`pcd_`) and link tokens (`pcx_`, scopes `podcasts:read`, `history:read`, `queue:write`) are accepted when `CORE_URL` is set.

## Discovery
- `GET /search?q=&source=all|itunes|podcastindex` → `{results[], warnings[]}`; results carry `subscribed_id` when already followed.
- `GET /search/trending?lang=` (Podcast Index key needed).
- `POST /preview {url}` → what a feed / YouTube channel / playlist / video is before subscribing.

## Subscriptions
- `GET /podcasts?q=&kind=rss|youtube&sort=title|recent|unplayed|added`
- `POST /podcasts {url, title?, auto_download?, download_limit?, retention_days?, min_duration?, title_filter?, notify?, playback_speed?, pub_date_mode?}` — a feed URL or a YouTube channel/playlist URL. Indexes synchronously, returns the podcast.
- `GET /podcasts/{id}` · `PUT /podcasts/{id}` (any subset of the settings) · `DELETE /podcasts/{id}`
- `POST /podcasts/{id}/refresh` → `{new, podcast}` · `POST /refresh` (all, in the background)
- `POST /podcasts/{id}/favorite {favorite}` · `POST /podcasts/{id}/played {played}` · `POST /podcasts/{id}/download {count}` · `DELETE /podcasts/{id}/files?played=1`
- `GET /podcasts/{id}/episodes?…` (same filters as `/episodes`)
- `GET|POST|DELETE /podcasts/{id}/artwork` (POST: multipart `file` or JSON `{url}`)
- `GET /youtube/search?type=channel|video|playlist&q=&limit=` (yt-dlp, cached 10 min; `limit` 1–100, default 12; no offset: for more, ask again with a larger limit). Channels: `{id, title, handle, url, followers, description, thumbnail, subscribed_id?}`; videos and playlists: `{kind, id, title, url, channel, duration?, views?, date? (YYYYMMDD, approximate: YouTube only says "2 years ago"), thumbnail, subscribed_id? (playlist followed), episode_id? (video already in your library)}`. Subscribe with `POST /podcasts {url}`, take a video with `POST /youtube/video {url}`.
- `GET /youtube/listing?url=&offset=&limit=` → the next videos of a channel or playlist `{episodes, more, next}` (the Videos window loads these as you scroll).
- `POST /youtube/video {url}` → downloads one video into your "Picks" show.
- `POST /picks {feed_url, guid|title, download?}` → one episode of any feed into your "Picks" show (no subscription), downloaded unless `download: false`. `guid` comes from `/preview`.

## Episodes
- `GET /episodes?q=&status=unplayed|partial|played|unfinished&downloaded=1|0&days=&kind=&podcast=&min_duration=&max_duration=&sort=newest|oldest|shortest|longest&limit=&offset=` → `{episodes[], total}`
- `GET /episodes/{id}` → episode with `chapters[]` and `sponsors[]`
- `GET /episodes/{id}/stream` — the episode's file with range support (`?download=1` as attachment). When it is not on the server yet it is downloaded right then and the response waits for it (a YouTube video takes a few seconds), so a phone or player can simply ask for the file; concurrent requests share one download.
- `GET /episodes/{id}/artwork`
- `POST /episodes/{id}/progress {position, listened, duration?, completed?}` — called by the player every ~10 s; `listened` seconds feed the stats.
- `POST|DELETE /episodes/{id}/played` · `DELETE /episodes/{id}/history`
- `POST|DELETE /episodes/{id}/download` · `POST|DELETE /episodes/{id}/queue` (`{front: true}` = play next) · `POST|DELETE /episodes/{id}/save`
- `DELETE /episodes/{id}`: removes the episode and its file; remembered per subscription (`removed_episodes`), so a feed refresh does not add it back. `POST /youtube/video` or `/picks` for the same item clears that.

## Lists
- `GET /home` — everything the Home page shows.
- `GET /now-playing` → what is playing right now on any of your devices `{active, episode, position, duration, playing, rate, device, client, updated_at, server_time}`; `GET /now-playing/events` streams the same as server-sent events (each also carries `type: "now_playing"`; `GET /events` below has them too). Fed by `POST /episodes/{id}/progress` (send `client` from a browser tab so it can tell its own session apart); the device name comes from the Pocket Core device, the API token's name ("Iphone app" → iPhone) or the User-Agent.
- `GET /queue` · `PUT /queue {ids[]}` · `DELETE /queue` · `GET /queue/next?after=`
- `GET /history?limit=&offset=` · `DELETE /history`
- `GET /saved` · `GET /downloads` → `{pending[], done[], bytes, yt_dlp}`
- `GET|POST /playlists` · `GET|PUT|DELETE /playlists/{id}` · `GET /playlists/{id}/episodes`

## Live events
`GET /events` is a server-sent event stream of everything that changes for the signed-in person, wherever it happened (another tab, the iPhone app, a feed refresh, a download worker). Each `data:` line is JSON with a `type`; events carry ids and a little state, not whole objects, so clients refetch what they show:

| type | fields | when |
|---|---|---|
| `hello` | `server_time` (ms) | on connect |
| `now_playing` | same as `GET /now-playing` | a progress report moved the now-playing session, or it went stale |
| `progress` | `episode_id, podcast_id, position, completed, client, updated_at` | a progress report (`client` = the reporting tab's id, empty for apps), played / unplayed, forgotten |
| `history` | `episode_id` (0 = all) | history cleared or forgotten |
| `episodes` | `podcast_id, added[], updated[], removed[]` | refresh found episodes, picks, mark-all, members-only videos retired, episode removed |
| `download` | `episode_id, podcast_id, state, error?` | queued, downloading, done, failed, culled, file deleted |
| `podcasts` | `added[], updated[], removed[]` | subscribe, settings, favourite, artwork, refresh finished, unsubscribe |
| `refresh` | `podcast_id, running, new?, error?` | a feed or channel refresh started / finished |
| `queue` · `playlists` · `settings` | — | changed |
| `saved` | `episode_id, saved` | saved / unsaved |

Only the person's own events are sent; every subscription has one owner, so two people following the same feed never see each other's refreshes or downloads. Keep-alive comments every 25 s; a revoked sign-in ends the stream with `event: unauthorized`. A client that cannot keep up (64 events buffered) is disconnected and should reconnect and reload; at most 16 streams per person (429 beyond). The web app reconnects with backoff, reloads on reconnect, and polls slowly only while the stream is down.

## You
- `GET /dashboard` — compact numbers for widgets (Homepage `customapi`): `podcasts, unplayed, queued, downloaded, downloaded_mb, listened_today_min, listened_week_min, now_playing, next_up, new_today, downloads_pending`.
- `GET /stats` · `GET /activity?kind=&limit=`
- `GET|PUT /settings` · `GET /settings/system` · `POST /settings/notify/test` · `POST /settings/feed-key` · `POST /settings/ytdlp/update` (admin)
- `GET /auth/me` · `PUT /auth/me` · `GET /tokens` · `POST /tokens {name}` · `DELETE /tokens/{id}`
- `GET /export.opml` · `GET /export.json` · `POST /import/opml` (multipart `file` or raw body) · `POST /import` (the JSON export) · `GET /backup` (admin)

## Public feeds (no login)
- `GET /feed/{key}/` — HTML index of your feeds
- `GET /feed/{key}/{podcast}.xml` — RSS 2.0 with itunes tags; YouTube feeds list downloaded episodes only
- `GET /feed/{key}/e/{episode}.m4a` (any extension), `/feed/{key}/e/{episode}/chapters.json`, `/feed/{key}/art/{podcast}.jpg`, `/feed/{key}/art/e/{episode}.jpg`

The key is in Settings → Public feeds. Feed URLs use `feed_base_url` (Settings) or `BASE_URL`.

## Health
`GET /healthz`, `GET /readyz`, `GET /metrics`.
