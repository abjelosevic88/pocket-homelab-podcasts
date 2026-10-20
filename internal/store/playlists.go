package store

import (
	"context"
	"encoding/json"
	"time"
)

const playlistCols = `id, user_id, name, description, icon, podcast_ids, include_unplayed, include_partial, include_played, min_duration, max_duration, max_age_days, downloaded_only, sort, limit_count, pos, created_at`

func scanPlaylist(row interface{ Scan(...any) error }) (*Playlist, error) {
	var p Playlist
	var ids string
	var unplayed, partial, played, dl int
	if err := row.Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.Icon, &ids, &unplayed, &partial, &played, &p.MinDuration, &p.MaxDuration, &p.MaxAgeDays, &dl, &p.Sort, &p.Limit, &p.Pos, &p.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	p.IncludeUnplayed, p.IncludePartial, p.IncludePlayed, p.DownloadedOnly = unplayed == 1, partial == 1, played == 1, dl == 1
	p.PodcastIDs = []int64{}
	_ = json.Unmarshal([]byte(ids), &p.PodcastIDs)
	return &p, nil
}

// CreatePlaylist inserts a smart playlist.
func (s *Store) CreatePlaylist(ctx context.Context, p *Playlist) error {
	if p.PodcastIDs == nil {
		p.PodcastIDs = []int64{}
	}
	ids, _ := json.Marshal(p.PodcastIDs)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO playlists (user_id, name, description, icon, podcast_ids, include_unplayed, include_partial, include_played, min_duration, max_duration, max_age_days, downloaded_only, sort, limit_count, pos)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, (SELECT COALESCE(MAX(pos), 0) + 1 FROM playlists WHERE user_id = ?))`,
		p.UserID, p.Name, p.Description, p.Icon, string(ids), boolInt(p.IncludeUnplayed), boolInt(p.IncludePartial), boolInt(p.IncludePlayed), p.MinDuration, p.MaxDuration, p.MaxAgeDays, boolInt(p.DownloadedOnly), p.Sort, p.Limit, p.UserID)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

// UpdatePlaylist stores a playlist's fields.
func (s *Store) UpdatePlaylist(ctx context.Context, p *Playlist) error {
	if p.PodcastIDs == nil {
		p.PodcastIDs = []int64{}
	}
	ids, _ := json.Marshal(p.PodcastIDs)
	res, err := s.DB.ExecContext(ctx, `UPDATE playlists SET name = ?, description = ?, icon = ?, podcast_ids = ?, include_unplayed = ?, include_partial = ?, include_played = ?, min_duration = ?, max_duration = ?, max_age_days = ?, downloaded_only = ?, sort = ?, limit_count = ? WHERE id = ? AND user_id = ?`,
		p.Name, p.Description, p.Icon, string(ids), boolInt(p.IncludeUnplayed), boolInt(p.IncludePartial), boolInt(p.IncludePlayed), p.MinDuration, p.MaxDuration, p.MaxAgeDays, boolInt(p.DownloadedOnly), p.Sort, p.Limit, p.ID, p.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetPlaylist returns one of a person's playlists.
func (s *Store) GetPlaylist(ctx context.Context, userID, id int64) (*Playlist, error) {
	return scanPlaylist(s.DB.QueryRowContext(ctx, `SELECT `+playlistCols+` FROM playlists WHERE id = ? AND user_id = ?`, id, userID))
}

// ListPlaylists lists a person's playlists in order.
func (s *Store) ListPlaylists(ctx context.Context, userID int64) ([]Playlist, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+playlistCols+` FROM playlists WHERE user_id = ? ORDER BY pos, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Playlist{}
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// DeletePlaylist removes a playlist.
func (s *Store) DeletePlaylist(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM playlists WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Filter turns a playlist into an episode filter.
func (p *Playlist) Filter() EpisodeFilter {
	f := EpisodeFilter{UserID: p.UserID, PodcastIDs: p.PodcastIDs, MinDuration: p.MinDuration, MaxDuration: p.MaxDuration, Sort: p.Sort, Limit: p.Limit}
	switch {
	case p.IncludeUnplayed && p.IncludePartial && !p.IncludePlayed:
		f.Status = "unfinished"
	case p.IncludeUnplayed && !p.IncludePartial && !p.IncludePlayed:
		f.Status = "unplayed"
	case !p.IncludeUnplayed && p.IncludePartial && !p.IncludePlayed:
		f.Status = "partial"
	case !p.IncludeUnplayed && !p.IncludePartial && p.IncludePlayed:
		f.Status = "played"
	}
	if p.DownloadedOnly {
		f.Downloaded = 1
	}
	if p.MaxAgeDays > 0 {
		f.Since = time.Now().AddDate(0, 0, -p.MaxAgeDays).UTC().Format(time.RFC3339)
	}
	return f
}

// DefaultPlaylists are created for every new person.
func DefaultPlaylists(userID int64) []Playlist {
	return []Playlist{
		{UserID: userID, Name: "Fresh", Icon: "✦", Description: "Unplayed episodes from the last week", IncludeUnplayed: true, MaxAgeDays: 7, Sort: "newest"},
		{UserID: userID, Name: "In progress", Icon: "◐", Description: "Episodes you started but did not finish", IncludePartial: true, Sort: "played"},
		{UserID: userID, Name: "Quick listens", Icon: "⚡", Description: "Unplayed, under 20 minutes", IncludeUnplayed: true, IncludePartial: true, MaxDuration: 20 * 60, Sort: "newest"},
		{UserID: userID, Name: "Long form", Icon: "☕", Description: "Unplayed, over an hour", IncludeUnplayed: true, IncludePartial: true, MinDuration: 60 * 60, Sort: "newest"},
		{UserID: userID, Name: "Downloaded", Icon: "⤓", Description: "Everything on disk, unplayed first", IncludeUnplayed: true, IncludePartial: true, DownloadedOnly: true, Sort: "newest"},
	}
}
