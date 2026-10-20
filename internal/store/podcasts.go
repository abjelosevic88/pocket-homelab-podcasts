package store

import (
	"context"
	"database/sql"
	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"strings"
)

const podcastCols = `p.id, p.user_id, p.kind, p.title, p.feed_url, p.site_url, p.youtube_id, p.youtube_kind, p.author, p.description,
	p.artwork_url, p.artwork_file, p.language, p.categories, p.explicit, p.favorite, p.playback_speed, p.start_skip, p.end_skip,
	p.auto_play_next, p.notify, p.auto_download, p.download_limit, p.retention_days, p.min_duration, p.title_filter,
	p.feed_cutoff_days, p.pub_date_mode, p.sponsorblock, p.refresh_minutes, p.etag, p.last_modified, p.last_refresh_at,
	p.last_refresh_ok, p.last_error, p.failures, p.subscribed_at, p.created_at, p.updated_at`

const podcastDerived = `,
	(SELECT COUNT(1) FROM episodes e WHERE e.podcast_id = p.id) AS episode_count,
	(SELECT COUNT(1) FROM episodes e LEFT JOIN progress pr ON pr.episode_id = e.id AND pr.user_id = p.user_id
	   WHERE e.podcast_id = p.id AND COALESCE(pr.completed, 0) = 0) AS unplayed_count,
	(SELECT COUNT(1) FROM episodes e WHERE e.podcast_id = p.id AND e.download = 'done') AS downloaded_count,
	(SELECT COALESCE(SUM(e.file_bytes), 0) FROM episodes e WHERE e.podcast_id = p.id AND e.download = 'done') AS downloaded_bytes,
	(SELECT COALESCE(MAX(e.pub_date), '') FROM episodes e WHERE e.podcast_id = p.id) AS latest_pub_date`

func scanPodcast(row interface{ Scan(...any) error }, derived bool) (*Podcast, error) {
	var p Podcast
	var explicit, favorite, notify, autoDL, sb int
	dest := []any{&p.ID, &p.UserID, &p.Kind, &p.Title, &p.FeedURL, &p.SiteURL, &p.YouTubeID, &p.YouTubeKind, &p.Author, &p.Description,
		&p.ArtworkURL, &p.ArtworkFile, &p.Language, &p.Categories, &explicit, &favorite, &p.PlaybackSpeed, &p.StartSkip, &p.EndSkip,
		&p.AutoPlayNext, &notify, &autoDL, &p.DownloadLimit, &p.RetentionDays, &p.MinDuration, &p.TitleFilter,
		&p.FeedCutoffDays, &p.PubDateMode, &sb, &p.RefreshMinutes, &p.ETag, &p.LastModified, &p.LastRefreshAt,
		&p.LastRefreshOK, &p.LastError, &p.Failures, &p.SubscribedAt, &p.CreatedAt, &p.UpdatedAt}
	if derived {
		dest = append(dest, &p.EpisodeCount, &p.UnplayedCount, &p.DownloadedCount, &p.DownloadedBytes, &p.LatestPubDate)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, notFound(err)
	}
	p.Explicit, p.Favorite, p.Notify, p.AutoDownload, p.SponsorBlock = explicit == 1, favorite == 1, notify == 1, autoDL == 1, sb == 1
	return &p, nil
}

// CreatePodcast inserts a subscription.
func (s *Store) CreatePodcast(ctx context.Context, p *Podcast, after ...core.CommitHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if p.Kind == "" {
		p.Kind = KindRSS
	}
	if p.PubDateMode == "" {
		p.PubDateMode = "published"
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO podcasts (user_id, kind, title, feed_url, site_url, youtube_id, youtube_kind, author, description,
		artwork_url, artwork_file, language, categories, explicit, favorite, playback_speed, start_skip, end_skip, auto_play_next, notify,
		auto_download, download_limit, retention_days, min_duration, title_filter, feed_cutoff_days, pub_date_mode, sponsorblock, refresh_minutes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.UserID, p.Kind, p.Title, p.FeedURL, p.SiteURL, p.YouTubeID, p.YouTubeKind, p.Author, p.Description,
		p.ArtworkURL, p.ArtworkFile, p.Language, p.Categories, boolInt(p.Explicit), boolInt(p.Favorite), p.PlaybackSpeed, p.StartSkip, p.EndSkip, p.AutoPlayNext, boolInt(p.Notify),
		boolInt(p.AutoDownload), p.DownloadLimit, p.RetentionDays, p.MinDuration, p.TitleFilter, p.FeedCutoffDays, p.PubDateMode, boolInt(p.SponsorBlock), p.RefreshMinutes)
	if err != nil {
		return conflict(err)
	}
	p.ID, _ = res.LastInsertId()
	return core.Commit(tx, after...)
}

// UpdatePodcast stores every editable field.
func (s *Store) UpdatePodcast(ctx context.Context, p *Podcast) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE podcasts SET title = ?, feed_url = ?, site_url = ?, youtube_id = ?, youtube_kind = ?, author = ?, description = ?,
		artwork_url = ?, artwork_file = ?, language = ?, categories = ?, explicit = ?, favorite = ?, playback_speed = ?, start_skip = ?, end_skip = ?,
		auto_play_next = ?, notify = ?, auto_download = ?, download_limit = ?, retention_days = ?, min_duration = ?, title_filter = ?,
		feed_cutoff_days = ?, pub_date_mode = ?, sponsorblock = ?, refresh_minutes = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		p.Title, p.FeedURL, p.SiteURL, p.YouTubeID, p.YouTubeKind, p.Author, p.Description,
		p.ArtworkURL, p.ArtworkFile, p.Language, p.Categories, boolInt(p.Explicit), boolInt(p.Favorite), p.PlaybackSpeed, p.StartSkip, p.EndSkip,
		p.AutoPlayNext, boolInt(p.Notify), boolInt(p.AutoDownload), p.DownloadLimit, p.RetentionDays, p.MinDuration, p.TitleFilter,
		p.FeedCutoffDays, p.PubDateMode, boolInt(p.SponsorBlock), p.RefreshMinutes, Now(), p.ID, p.UserID)
	return conflict(err)
}

// UpdateFeedState records the outcome of a refresh.
func (s *Store) UpdateFeedState(ctx context.Context, id int64, etag, lastModified string, refreshErr error) error {
	now := Now()
	if refreshErr != nil {
		_, err := s.DB.ExecContext(ctx, `UPDATE podcasts SET last_refresh_at = ?, last_error = ?, failures = failures + 1 WHERE id = ?`, now, refreshErr.Error(), id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE podcasts SET last_refresh_at = ?, last_refresh_ok = ?, last_error = '', failures = 0, etag = ?, last_modified = ? WHERE id = ?`, now, now, etag, lastModified, id)
	return err
}

// GetPodcast returns one of a person's subscriptions.
func (s *Store) GetPodcast(ctx context.Context, userID, id int64) (*Podcast, error) {
	return scanPodcast(s.DB.QueryRowContext(ctx, `SELECT `+podcastCols+podcastDerived+` FROM podcasts p WHERE p.id = ? AND p.user_id = ?`, id, userID), true)
}

// GetPodcastAny returns a subscription by id regardless of owner (for workers).
func (s *Store) GetPodcastAny(ctx context.Context, id int64) (*Podcast, error) {
	return scanPodcast(s.DB.QueryRowContext(ctx, `SELECT `+podcastCols+` FROM podcasts p WHERE p.id = ?`, id), false)
}

// FindPodcastByFeed finds a person's subscription to a feed URL.
func (s *Store) FindPodcastByFeed(ctx context.Context, userID int64, feedURL string) (*Podcast, error) {
	return scanPodcast(s.DB.QueryRowContext(ctx, `SELECT `+podcastCols+podcastDerived+` FROM podcasts p WHERE p.user_id = ? AND p.feed_url = ?`, userID, feedURL), true)
}

// ListPodcasts lists a person's subscriptions. sort: title | recent | added | unplayed.
func (s *Store) ListPodcasts(ctx context.Context, userID int64, q, kind, sort string) ([]Podcast, error) {
	where := []string{"p.user_id = ?"}
	args := []any{userID}
	if q = strings.TrimSpace(q); q != "" {
		where = append(where, "(p.title LIKE ? OR p.author LIKE ?)")
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	if kind == KindRSS || kind == KindYouTube {
		where = append(where, "p.kind = ?")
		args = append(args, kind)
	}
	order := "p.favorite DESC, LOWER(p.title)"
	switch sort {
	case "recent":
		order = "p.favorite DESC, latest_pub_date DESC"
	case "added":
		order = "p.id DESC"
	case "unplayed":
		order = "p.favorite DESC, unplayed_count DESC, LOWER(p.title)"
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+podcastCols+podcastDerived+` FROM podcasts p WHERE `+strings.Join(where, " AND ")+` ORDER BY `+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Podcast{}
	for rows.Next() {
		p, err := scanPodcast(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// ListAllPodcasts lists every subscription on the box (for the scheduler).
func (s *Store) ListAllPodcasts(ctx context.Context) ([]Podcast, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+podcastCols+` FROM podcasts p ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Podcast
	for rows.Next() {
		p, err := scanPodcast(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// DeletePodcast removes a subscription with its episodes and returns the
// files that belonged to it, for the caller to delete.
func (s *Store) DeletePodcast(ctx context.Context, userID, id int64, after ...core.CommitHook) ([]string, error) {
	files, err := s.PodcastFiles(ctx, id)
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM podcasts WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return files, core.Commit(tx, after...)
}

// PodcastFiles lists the media files of a subscription (relative to MEDIA_DIR).
func (s *Store) PodcastFiles(ctx context.Context, id int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT file_path FROM episodes WHERE podcast_id = ? AND file_path <> ''`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetPodcastArtworkFile records a cached or custom artwork file.
func (s *Store) SetPodcastArtworkFile(ctx context.Context, id int64, file string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE podcasts SET artwork_file = ?, updated_at = ? WHERE id = ?`, file, Now(), id)
	return err
}

// SetPodcastFavorite flips the star.
func (s *Store) SetPodcastFavorite(ctx context.Context, userID, id int64, fav bool) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE podcasts SET favorite = ?, updated_at = ? WHERE id = ? AND user_id = ?`, boolInt(fav), Now(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePodcastMeta stores what a feed refresh learned about the show (never
// the person's own title or artwork choice when they set one by hand).
func (s *Store) UpdatePodcastMeta(ctx context.Context, id int64, title, author, description, artworkURL, siteURL, language, categories string, explicit bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE podcasts SET
		title = CASE WHEN title = '' THEN ? ELSE title END,
		author = ?, description = ?, artwork_url = CASE WHEN ? = '' THEN artwork_url ELSE ? END,
		site_url = CASE WHEN ? = '' THEN site_url ELSE ? END, language = ?, categories = ?, explicit = ?, updated_at = ? WHERE id = ?`,
		title, author, description, artworkURL, artworkURL, siteURL, siteURL, language, categories, boolInt(explicit), Now(), id)
	return err
}

// OwnerOfPodcast returns who a subscription belongs to.
func (s *Store) OwnerOfPodcast(ctx context.Context, id int64) (int64, error) {
	var uid sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT user_id FROM podcasts WHERE id = ?`, id).Scan(&uid); err != nil {
		return 0, notFound(err)
	}
	return uid.Int64, nil
}
