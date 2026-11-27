package store

import (
	"context"
	"strings"
)

// DailyListen is actual listening on a date, independent of the latest resume
// position. Old rows have no clock time: do not invent one from last_played_at.
type DailyListen struct {
	EpisodeID int64  `json:"episode_id"`
	Title     string `json:"title"`
	Podcast   string `json:"podcast"`
	Seconds   int    `json:"seconds"`
	Duration  int    `json:"duration"`
	FirstAt   string `json:"first_at,omitempty"`
	LastAt    string `json:"last_at,omitempty"`
}

func (s *Store) DailyListens(ctx context.Context, userID int64, day string) ([]DailyListen, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id,e.title,p.title,l.seconds,e.duration,l.first_at,l.last_at
 FROM listens l JOIN episodes e ON e.id=l.episode_id JOIN podcasts p ON p.id=e.podcast_id
 WHERE l.user_id=? AND p.user_id=? AND l.day=? AND l.seconds>0 ORDER BY l.first_at,e.id`, userID, userID, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DailyListen{}
	for rows.Next() {
		var v DailyListen
		if err := rows.Scan(&v.EpisodeID, &v.Title, &v.Podcast, &v.Seconds, &v.Duration, &v.FirstAt, &v.LastAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListeningDates lets consumers refresh old days after an offline upload.
func (s *Store) ListeningDates(ctx context.Context, userID int64) ([]map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT day,MAX(updated_at) FROM listens WHERE user_id=? GROUP BY day ORDER BY day`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var day, updated string
		if err := rows.Scan(&day, &updated); err != nil {
			return nil, err
		}
		out = append(out, map[string]string{"date": day, "updated_at": updated})
	}
	return out, rows.Err()
}

// FinishedAt tells when a person finished each of these episodes (completed_at
// of the ones marked played; the others are left out).
func (s *Store) FinishedAt(ctx context.Context, userID int64, episodeIDs []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(episodeIDs) == 0 {
		return out, nil
	}
	args := []any{userID}
	for _, id := range episodeIDs {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT episode_id,completed_at FROM progress WHERE user_id=? AND completed=1 AND completed_at<>''
 AND episode_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(episodeIDs)), ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
	}
	return out, rows.Err()
}
