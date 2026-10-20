CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'admin',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup_complete). Everything a person chooses
-- lives in user_settings under their id.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

-- A subscription. Every person has their own: the same feed followed by two
-- people is two rows (each with their own settings, episodes and files).
--   kind:         rss | youtube
--   feed_url:     the RSS feed, or the YouTube channel / playlist URL
--   youtube_id:   UC… channel id or PL… playlist id (kind = youtube)
--   pub_date_mode published | downloaded — what the public feed uses as pubDate
CREATE TABLE podcasts (
  id               INTEGER PRIMARY KEY,
  user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind             TEXT NOT NULL DEFAULT 'rss',
  title            TEXT NOT NULL,
  feed_url         TEXT NOT NULL,
  site_url         TEXT NOT NULL DEFAULT '',
  youtube_id       TEXT NOT NULL DEFAULT '',
  youtube_kind     TEXT NOT NULL DEFAULT '',   -- channel | playlist
  author           TEXT NOT NULL DEFAULT '',
  description      TEXT NOT NULL DEFAULT '',
  artwork_url      TEXT NOT NULL DEFAULT '',   -- remote artwork
  artwork_file     TEXT NOT NULL DEFAULT '',   -- cached / custom file under ARTWORK_DIR
  language         TEXT NOT NULL DEFAULT '',
  categories       TEXT NOT NULL DEFAULT '',   -- comma separated
  explicit         INTEGER NOT NULL DEFAULT 0,
  favorite         INTEGER NOT NULL DEFAULT 0,
  -- listening preferences
  playback_speed   REAL NOT NULL DEFAULT 0,    -- 0 = use the person's default
  start_skip       INTEGER NOT NULL DEFAULT 0, -- seconds skipped at the start
  end_skip         INTEGER NOT NULL DEFAULT 0, -- seconds skipped at the end
  auto_play_next   INTEGER NOT NULL DEFAULT -1,-- -1 = person's default, 0 off, 1 on
  notify           INTEGER NOT NULL DEFAULT 0, -- send a notification for new episodes
  -- downloads
  auto_download    INTEGER NOT NULL DEFAULT 0, -- rss: download every new episode
  download_limit   INTEGER NOT NULL DEFAULT 3, -- youtube: how many of the newest to fetch on subscribe
  retention_days   INTEGER NOT NULL DEFAULT 0, -- delete files this many days after download (0 = keep)
  min_duration     INTEGER NOT NULL DEFAULT 0, -- youtube: skip videos shorter than this (seconds)
  title_filter     TEXT NOT NULL DEFAULT '',   -- youtube: regex a title must match
  feed_cutoff_days INTEGER NOT NULL DEFAULT 0, -- ignore episodes older than this when refreshing (0 = all)
  pub_date_mode    TEXT NOT NULL DEFAULT 'published',
  sponsorblock     INTEGER NOT NULL DEFAULT 1, -- youtube: fetch SponsorBlock segments
  -- feed state
  refresh_minutes  INTEGER NOT NULL DEFAULT 0, -- 0 = person's default
  etag             TEXT NOT NULL DEFAULT '',
  last_modified    TEXT NOT NULL DEFAULT '',
  last_refresh_at  TEXT NOT NULL DEFAULT '',
  last_refresh_ok  TEXT NOT NULL DEFAULT '',
  last_error       TEXT NOT NULL DEFAULT '',
  failures         INTEGER NOT NULL DEFAULT 0,
  subscribed_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (user_id, feed_url)
);
CREATE INDEX podcasts_user ON podcasts(user_id);

-- One episode of one subscription. Everything reachable through podcasts.user_id.
--   download: none | queued | downloading | done | failed | culled
--   pub_date: RFC 3339 (UTC); '' when unknown (a YouTube video only indexed)
CREATE TABLE episodes (
  id              INTEGER PRIMARY KEY,
  podcast_id      INTEGER NOT NULL REFERENCES podcasts(id) ON DELETE CASCADE,
  guid            TEXT NOT NULL,
  title           TEXT NOT NULL,
  description     TEXT NOT NULL DEFAULT '',
  link            TEXT NOT NULL DEFAULT '',
  enclosure_url   TEXT NOT NULL DEFAULT '',
  enclosure_type  TEXT NOT NULL DEFAULT '',
  enclosure_bytes INTEGER NOT NULL DEFAULT 0,
  duration        INTEGER NOT NULL DEFAULT 0,  -- seconds
  pub_date        TEXT NOT NULL DEFAULT '',
  artwork_url     TEXT NOT NULL DEFAULT '',
  season          INTEGER NOT NULL DEFAULT 0,
  episode_no      INTEGER NOT NULL DEFAULT 0,
  episode_type    TEXT NOT NULL DEFAULT '',   -- full | trailer | bonus
  explicit        INTEGER NOT NULL DEFAULT 0,
  is_video        INTEGER NOT NULL DEFAULT 0,
  chapters_url    TEXT NOT NULL DEFAULT '',   -- podcast:chapters JSON
  transcript_url  TEXT NOT NULL DEFAULT '',
  chapters        TEXT NOT NULL DEFAULT '',   -- JSON [{start,end,title,url}]
  sponsors        TEXT NOT NULL DEFAULT '',   -- JSON [{start,end,category}] (SponsorBlock)
  youtube_id      TEXT NOT NULL DEFAULT '',
  position        INTEGER NOT NULL DEFAULT 0,-- youtube: index in the channel listing (0 = newest)
  file_path       TEXT NOT NULL DEFAULT '',  -- relative to MEDIA_DIR
  file_bytes      INTEGER NOT NULL DEFAULT 0,
  file_type       TEXT NOT NULL DEFAULT '',
  downloaded_at   TEXT NOT NULL DEFAULT '',
  download        TEXT NOT NULL DEFAULT 'none',
  download_error  TEXT NOT NULL DEFAULT '',
  download_tries  INTEGER NOT NULL DEFAULT 0,
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (podcast_id, guid)
);
CREATE INDEX episodes_podcast_date ON episodes(podcast_id, pub_date DESC);
CREATE INDEX episodes_download ON episodes(download);

-- Where a person is in an episode. One row per (person, episode); a row with
-- last_played_at set is their history.
CREATE TABLE progress (
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  episode_id      INTEGER NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
  position        INTEGER NOT NULL DEFAULT 0,  -- seconds
  completed       INTEGER NOT NULL DEFAULT 0,
  listened        INTEGER NOT NULL DEFAULT 0,  -- seconds of listening recorded in total
  play_count      INTEGER NOT NULL DEFAULT 0,
  last_played_at  TEXT NOT NULL DEFAULT '',
  completed_at    TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, episode_id)
);
CREATE INDEX progress_user_played ON progress(user_id, last_played_at DESC);

-- Listening sessions, for the stats: who listened how long on which day.
CREATE TABLE listens (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  episode_id INTEGER NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
  day        TEXT NOT NULL,                  -- YYYY-MM-DD local
  seconds    INTEGER NOT NULL DEFAULT 0,
  UNIQUE (user_id, episode_id, day)
);

CREATE TABLE queue (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  episode_id INTEGER NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
  pos        INTEGER NOT NULL DEFAULT 0,
  added_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, episode_id)
);

CREATE TABLE saved (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  episode_id INTEGER NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, episode_id)
);

-- Smart playlists: a filter over a person's episodes.
--   podcast_ids: JSON array, [] = every subscription
--   sort: newest | oldest | shortest | longest
CREATE TABLE playlists (
  id              INTEGER PRIMARY KEY,
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name            TEXT NOT NULL,
  description     TEXT NOT NULL DEFAULT '',
  icon            TEXT NOT NULL DEFAULT '',
  podcast_ids     TEXT NOT NULL DEFAULT '[]',
  include_unplayed INTEGER NOT NULL DEFAULT 1,
  include_partial  INTEGER NOT NULL DEFAULT 1,
  include_played   INTEGER NOT NULL DEFAULT 0,
  min_duration    INTEGER NOT NULL DEFAULT 0,
  max_duration    INTEGER NOT NULL DEFAULT 0,
  max_age_days    INTEGER NOT NULL DEFAULT 0,
  downloaded_only INTEGER NOT NULL DEFAULT 0,
  sort            TEXT NOT NULL DEFAULT 'newest',
  limit_count     INTEGER NOT NULL DEFAULT 0,
  pos             INTEGER NOT NULL DEFAULT 0,
  created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX playlists_user ON playlists(user_id);

-- Things the app did on its own, shown under Settings → Activity.
CREATE TABLE activity (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL DEFAULT 0,
  kind       TEXT NOT NULL,                  -- refresh | download | cull | notify | error
  message    TEXT NOT NULL DEFAULT '',
  podcast_id INTEGER NOT NULL DEFAULT 0,
  episode_id INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX activity_user_time ON activity(user_id, id DESC);
