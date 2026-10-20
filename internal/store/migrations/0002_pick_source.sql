-- A picked episode remembers the show it came from, so its page can link to
-- that show's other episodes and offer to follow it.
ALTER TABLE episodes ADD COLUMN source_feed TEXT NOT NULL DEFAULT '';
ALTER TABLE episodes ADD COLUMN source_title TEXT NOT NULL DEFAULT '';

-- Picks made before this migration carry the feed in their guid ("<feed>#<guid>")
-- and the show in their title ("<episode> — <show>").
UPDATE episodes SET
  source_feed = substr(guid, 1, instr(guid, '#') - 1),
  source_title = CASE WHEN instr(title, ' — ') > 0 THEN substr(title, instr(title, ' — ') + 3) ELSE '' END
WHERE podcast_id IN (SELECT id FROM podcasts WHERE feed_url IN ('pocket:picks', 'pocket:youtube-picks'))
  AND instr(guid, '#') > 0 AND guid NOT LIKE 'yt:%';
