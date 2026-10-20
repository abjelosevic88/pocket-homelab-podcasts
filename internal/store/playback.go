package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// SaveProgress stores where a person is in an episode and adds listened
// seconds to today's listening. completed marks the episode played.
func (s *Store) SaveProgress(ctx context.Context, userID, episodeID int64, position, listened int, completed bool, after ...func(*sql.Tx, bool, bool) error) error {
	if !s.ownsEpisode(ctx, userID, episodeID) {
		return ErrNotFound
	}
	now := Now()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	var wasPlayed string
	var wasDone int
	err = tx.QueryRowContext(ctx, `SELECT last_played_at,completed FROM progress WHERE user_id=? AND episode_id=?`, userID, episodeID).Scan(&wasPlayed, &wasDone)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	var played int
	if listened > 0 {
		played = 1
	}
	completedAt := ""
	if completed {
		completedAt = now
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO progress (user_id, episode_id, position, completed, listened, play_count, last_played_at, completed_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(user_id, episode_id) DO UPDATE SET position = excluded.position,
		  completed = CASE WHEN excluded.completed = 1 THEN 1 ELSE progress.completed END,
		  completed_at = CASE WHEN excluded.completed = 1 AND progress.completed = 0 THEN excluded.completed_at ELSE progress.completed_at END,
		  listened = progress.listened + excluded.listened,
		  play_count = progress.play_count + CASE WHEN progress.last_played_at < strftime('%Y-%m-%dT%H:%M:%fZ','now','-30 minutes') THEN ? ELSE 0 END,
		  last_played_at = excluded.last_played_at`,
		userID, episodeID, position, boolInt(completed), listened, now, completedAt, played); err != nil {
		return err
	}
	if listened > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO listens (user_id, episode_id, day, seconds) VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id, episode_id, day) DO UPDATE SET seconds = listens.seconds + excluded.seconds`, userID, episodeID, Today(), listened); err != nil {
			return err
		}
	}
	for _, hook := range after {
		if hook != nil {
			if err := hook(tx, wasPlayed == "" && listened > 0, completed && wasDone == 0); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// SetCompleted marks an episode played (position at the end) or unplayed.
func (s *Store) SetCompleted(ctx context.Context, userID, episodeID int64, completed bool, after ...func(*sql.Tx, bool, bool) error) error {
	if !s.ownsEpisode(ctx, userID, episodeID) {
		return ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var wasPlayed string
	var wasDone int
	err = tx.QueryRowContext(ctx, `SELECT last_played_at,completed FROM progress WHERE user_id=? AND episode_id=?`, userID, episodeID).Scan(&wasPlayed, &wasDone)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	finish := func() error {
		for _, hook := range after {
			if hook != nil {
				if err := hook(tx, false, completed && wasDone == 0); err != nil {
					return err
				}
			}
		}
		return tx.Commit()
	}

	if completed {
		_, err := tx.ExecContext(ctx, `INSERT INTO progress (user_id, episode_id, position, completed, completed_at, last_played_at)
			VALUES (?, ?, (SELECT duration FROM episodes WHERE id = ?), 1, ?, ?)
			ON CONFLICT(user_id, episode_id) DO UPDATE SET completed = 1, completed_at = excluded.completed_at,
			  last_played_at = CASE WHEN progress.last_played_at = '' THEN excluded.last_played_at ELSE progress.last_played_at END`,
			userID, episodeID, episodeID, Now(), Now())
		if err != nil {
			return err
		}
		return finish()
	}
	_, err = tx.ExecContext(ctx, `UPDATE progress SET completed = 0, completed_at = '', position = 0 WHERE user_id = ? AND episode_id = ?`, userID, episodeID)
	if err != nil {
		return err
	}
	return finish()
}

// ClearHistory forgets a person's plays of one episode (or all when 0).
func (s *Store) ClearHistory(ctx context.Context, userID, episodeID int64) error {
	if episodeID > 0 {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM progress WHERE user_id = ? AND episode_id = ?`, userID, episodeID)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM progress WHERE user_id = ?`, userID)
	return err
}

func (s *Store) ownsEpisode(ctx context.Context, userID, episodeID int64) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM episodes e JOIN podcasts p ON p.id = e.podcast_id WHERE e.id = ? AND p.user_id = ?`, episodeID, userID).Scan(&n)
	return n == 1
}

// ---- queue ----

// AddToQueue appends (or, with front, prepends) an episode to a person's queue.
func (s *Store) AddToQueue(ctx context.Context, userID, episodeID int64, front bool) error {
	if !s.ownsEpisode(ctx, userID, episodeID) {
		return ErrNotFound
	}
	if front {
		if _, err := s.DB.ExecContext(ctx, `UPDATE queue SET pos = pos + 1 WHERE user_id = ?`, userID); err != nil {
			return err
		}
		_, err := s.DB.ExecContext(ctx, `INSERT INTO queue (user_id, episode_id, pos) VALUES (?, ?, 1) ON CONFLICT(user_id, episode_id) DO UPDATE SET pos = 1`, userID, episodeID)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO queue (user_id, episode_id, pos) VALUES (?, ?, (SELECT COALESCE(MAX(pos), 0) + 1 FROM queue WHERE user_id = ?))
		ON CONFLICT(user_id, episode_id) DO NOTHING`, userID, episodeID, userID)
	return err
}

// RemoveFromQueue drops an episode from the queue.
func (s *Store) RemoveFromQueue(ctx context.Context, userID, episodeID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM queue WHERE user_id = ? AND episode_id = ?`, userID, episodeID)
	if err != nil {
		return err
	}
	return s.renumberQueue(ctx, userID)
}

// ClearQueue empties the queue.
func (s *Store) ClearQueue(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM queue WHERE user_id = ?`, userID)
	return err
}

// ReorderQueue sets the order to ids (episodes not listed keep their relative order after them).
func (s *Store) ReorderQueue(ctx context.Context, userID int64, ids []int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `UPDATE queue SET pos = pos + 100000 WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE queue SET pos = ? WHERE user_id = ? AND episode_id = ?`, i+1, userID, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.renumberQueue(ctx, userID)
}

func (s *Store) renumberQueue(ctx context.Context, userID int64) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT episode_id FROM queue WHERE user_id = ? ORDER BY pos, added_at`, userID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for i, id := range ids {
		if _, err := s.DB.ExecContext(ctx, `UPDATE queue SET pos = ? WHERE user_id = ? AND episode_id = ?`, i+1, userID, id); err != nil {
			return err
		}
	}
	return nil
}

// NextInQueue returns the first queued episode other than current (0 = none).
func (s *Store) NextInQueue(ctx context.Context, userID, current int64) (*Episode, error) {
	eps, err := s.ListEpisodes(ctx, EpisodeFilter{UserID: userID, Queued: true, Sort: "queue", Limit: 2})
	if err != nil {
		return nil, err
	}
	for i := range eps {
		if eps[i].ID != current {
			return &eps[i], nil
		}
	}
	return nil, nil
}

// ---- saved ----

// SetSaved bookmarks an episode or removes the bookmark.
func (s *Store) SetSaved(ctx context.Context, userID, episodeID int64, saved bool) error {
	if !s.ownsEpisode(ctx, userID, episodeID) {
		return ErrNotFound
	}
	if saved {
		_, err := s.DB.ExecContext(ctx, `INSERT INTO saved (user_id, episode_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, userID, episodeID)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM saved WHERE user_id = ? AND episode_id = ?`, userID, episodeID)
	return err
}

// ---- activity ----

// AddActivity logs something the app did for a person.
func (s *Store) AddActivity(ctx context.Context, userID int64, kind, message string, podcastID, episodeID int64) {
	if len(message) > 400 {
		message = message[:400]
	}
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO activity (user_id, kind, message, podcast_id, episode_id) VALUES (?, ?, ?, ?, ?)`, userID, kind, message, podcastID, episodeID)
}

// ListActivity returns a person's newest activity lines.
func (s *Store) ListActivity(ctx context.Context, userID int64, kind string, limit int) ([]Activity, error) {
	where := "user_id = ?"
	args := []any{userID}
	if kind != "" {
		where += " AND kind = ?"
		args = append(args, kind)
	}
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, kind, message, podcast_id, episode_id, created_at FROM activity WHERE `+where+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.UserID, &a.Kind, &a.Message, &a.PodcastID, &a.EpisodeID, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PruneActivity keeps the newest 2000 lines per person and drops the rest.
func (s *Store) PruneActivity(ctx context.Context) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM activity WHERE created_at < ?`, time.Now().AddDate(0, 0, -90).UTC().Format(time.RFC3339))
}

// ---- dashboard ----

// Dashboard summarises a person's listening for widgets.
func (s *Store) Dashboard(ctx context.Context, userID int64) (*Dashboard, error) {
	d := &Dashboard{}
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM podcasts WHERE user_id = ?`, userID).Scan(&d.Podcasts)
	d.Unplayed, _ = s.CountEpisodes(ctx, EpisodeFilter{UserID: userID, Status: "unplayed"})
	d.Queued, _ = s.CountEpisodes(ctx, EpisodeFilter{UserID: userID, Queued: true})
	var bytes sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1), SUM(e.file_bytes) FROM episodes e JOIN podcasts p ON p.id = e.podcast_id WHERE p.user_id = ? AND e.download = 'done'`, userID).Scan(&d.Downloaded, &bytes)
	d.DownloadedMB = int(bytes.Int64 >> 20)
	q, r := s.CountDownloads(ctx, userID)
	d.Pending = q + r
	var today, week int
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(seconds), 0) FROM listens WHERE user_id = ? AND day = ?`, userID, Today()).Scan(&today)
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(seconds), 0) FROM listens WHERE user_id = ? AND day >= ?`, userID, time.Now().AddDate(0, 0, -6).Format("2006-01-02")).Scan(&week)
	d.ListenedToday, d.ListenedWeek = today/60, week/60
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM episodes e JOIN podcasts p ON p.id = e.podcast_id WHERE p.user_id = ? AND substr(e.pub_date, 1, 10) = ?`, userID, time.Now().UTC().Format("2006-01-02")).Scan(&d.NewToday)
	if eps, err := s.ListEpisodes(ctx, EpisodeFilter{UserID: userID, History: true, Status: "partial", Sort: "played", Limit: 1}); err == nil && len(eps) > 0 {
		d.NowPlaying = line(eps[0])
	}
	if nx, err := s.NextInQueue(ctx, userID, 0); err == nil && nx != nil {
		d.NextUp = line(*nx)
	}
	return d, nil
}

func line(e Episode) *EpisodeLine {
	l := &EpisodeLine{ID: e.ID, Title: e.Title, Podcast: e.PodcastTitle, Date: strings.SplitN(e.PubDate, "T", 2)[0]}
	if e.Duration > 0 {
		l.Progress = e.PlayPosition * 100 / e.Duration
	}
	return l
}
