ALTER TABLE listens ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';
UPDATE listens SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now');
ALTER TABLE listens ADD COLUMN first_at TEXT NOT NULL DEFAULT '';
ALTER TABLE listens ADD COLUMN last_at TEXT NOT NULL DEFAULT '';

-- A receipt survives retries and episode deletion; its ID belongs to one user.
CREATE TABLE listening_reports (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  report_id TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  PRIMARY KEY (user_id, report_id)
);
