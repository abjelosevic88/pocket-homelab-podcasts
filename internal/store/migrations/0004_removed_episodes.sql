-- Episodes a person removed: a feed refresh does not add them back. Getting the
-- same video or episode again on purpose clears the mark.
CREATE TABLE IF NOT EXISTS removed_episodes (
  podcast_id INTEGER NOT NULL REFERENCES podcasts(id) ON DELETE CASCADE,
  guid       TEXT NOT NULL,
  removed_at TEXT NOT NULL,
  PRIMARY KEY (podcast_id, guid)
);
