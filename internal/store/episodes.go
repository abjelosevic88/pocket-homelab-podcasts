package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const episodeCols = `e.id, e.podcast_id, e.guid, e.title, e.description, e.link, e.enclosure_url, e.enclosure_type, e.enclosure_bytes,
	e.duration, e.pub_date, e.artwork_url, e.season, e.episode_no, e.episode_type, e.explicit, e.is_video, e.chapters_url, e.transcript_url,
	e.youtube_id, e.position, e.file_path, e.file_bytes, e.file_type, e.downloaded_at, e.download, e.download_error, e.download_tries, e.created_at,
	p.title, p.kind, p.artwork_url, p.artwork_file, p.playback_speed, p.start_skip, p.end_skip,
	COALESCE(pr.position, 0), COALESCE(pr.completed, 0), COALESCE(pr.last_played_at, ''), COALESCE(pr.play_count, 0),
	(sv.episode_id IS NOT NULL), COALESCE(q.pos, 0), e.chapters <> '', e.sponsors <> ''`

const episodeFrom = ` FROM episodes e JOIN podcasts p ON p.id = e.podcast_id
	LEFT JOIN progress pr ON pr.episode_id = e.id AND pr.user_id = p.user_id
	LEFT JOIN saved sv ON sv.episode_id = e.id AND sv.user_id = p.user_id
	LEFT JOIN queue q ON q.episode_id = e.id AND q.user_id = p.user_id`

func scanEpisode(row interface{ Scan(...any) error }) (*Episode, error) {
	var e Episode
	var explicit, video, completed, saved, hasCh, hasSb int
	var artFile string
	if err := row.Scan(&e.ID, &e.PodcastID, &e.GUID, &e.Title, &e.Description, &e.Link, &e.EnclosureURL, &e.EnclosureType, &e.EnclosureBytes,
		&e.Duration, &e.PubDate, &e.ArtworkURL, &e.Season, &e.EpisodeNo, &e.EpisodeType, &explicit, &video, &e.ChaptersURL, &e.TranscriptURL,
		&e.YouTubeID, &e.Position, &e.FilePath, &e.FileBytes, &e.FileType, &e.DownloadedAt, &e.Download, &e.DownloadError, &e.DownloadTries, &e.CreatedAt,
		&e.PodcastTitle, &e.PodcastKind, &e.PodcastArtwork, &artFile, &e.PlaybackSpeed, &e.StartSkip, &e.EndSkip,
		&e.PlayPosition, &completed, &e.LastPlayedAt, &e.PlayCount, &saved, &e.QueuePos, &hasCh, &hasSb); err != nil {
		return nil, notFound(err)
	}
	e.Explicit, e.IsVideo, e.Completed, e.Saved, e.HasChapters, e.HasSponsors = explicit == 1, video == 1, completed == 1, saved == 1, hasCh == 1, hasSb == 1
	if artFile != "" {
		e.PodcastArtwork = "file:" + artFile
	}
	return &e, nil
}

// EpisodeFilter selects episodes of one person.
type EpisodeFilter struct {
	UserID      int64
	PodcastID   int64
	PodcastIDs  []int64
	Query       string
	Status      string // "" | unplayed | partial | played | unfinished (unplayed+partial)
	Downloaded  int    // 0 any, 1 only downloaded, -1 not downloaded
	Saved       bool
	Queued      bool
	History     bool   // played at least once, newest play first
	Pending     bool   // download queued, running or failed
	Since       string // pub_date >= Since
	MinDuration int
	MaxDuration int
	Kind        string
	Sort        string // newest | oldest | shortest | longest | played | queue | listing
	Limit       int
	Offset      int
}

func (f EpisodeFilter) build() (string, []any) {
	where := []string{"p.user_id = ?"}
	args := []any{f.UserID}
	if f.PodcastID > 0 {
		where = append(where, "e.podcast_id = ?")
		args = append(args, f.PodcastID)
	}
	if len(f.PodcastIDs) > 0 {
		ph := make([]string, len(f.PodcastIDs))
		for i, id := range f.PodcastIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		where = append(where, "e.podcast_id IN ("+strings.Join(ph, ",")+")")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "(e.title LIKE ? OR e.description LIKE ? OR p.title LIKE ?)")
		args = append(args, "%"+q+"%", "%"+q+"%", "%"+q+"%")
	}
	switch f.Status {
	case "unplayed":
		where = append(where, "COALESCE(pr.completed, 0) = 0 AND COALESCE(pr.position, 0) = 0")
	case "partial":
		where = append(where, "COALESCE(pr.completed, 0) = 0 AND COALESCE(pr.position, 0) > 0")
	case "played":
		where = append(where, "COALESCE(pr.completed, 0) = 1")
	case "unfinished":
		where = append(where, "COALESCE(pr.completed, 0) = 0")
	}
	switch f.Downloaded {
	case 1:
		where = append(where, "e.download = 'done'")
	case -1:
		where = append(where, "e.download <> 'done'")
	}
	if f.Saved {
		where = append(where, "sv.episode_id IS NOT NULL")
	}
	if f.Queued {
		where = append(where, "q.episode_id IS NOT NULL")
	}
	if f.History {
		where = append(where, "COALESCE(pr.last_played_at, '') <> ''")
	}
	if f.Pending {
		where = append(where, "e.download IN ('queued', 'downloading', 'failed')")
	}
	if f.Since != "" {
		where = append(where, "e.pub_date >= ?")
		args = append(args, f.Since)
	}
	if f.MinDuration > 0 {
		where = append(where, "e.duration >= ?")
		args = append(args, f.MinDuration)
	}
	if f.MaxDuration > 0 {
		where = append(where, "e.duration > 0 AND e.duration <= ?")
		args = append(args, f.MaxDuration)
	}
	if f.Kind != "" {
		where = append(where, "p.kind = ?")
		args = append(args, f.Kind)
	}
	order := "e.pub_date DESC, e.position ASC, e.id DESC"
	switch f.Sort {
	case "oldest":
		order = "e.pub_date ASC, e.position DESC, e.id ASC"
	case "shortest":
		order = "e.duration ASC, e.pub_date DESC"
	case "longest":
		order = "e.duration DESC, e.pub_date DESC"
	case "played":
		order = "pr.last_played_at DESC"
	case "queue":
		order = "q.pos ASC"
	case "listing":
		order = "CASE WHEN e.pub_date = '' THEN 1 ELSE 0 END, e.pub_date DESC, e.position ASC"
	case "saved":
		order = "sv.created_at DESC"
	case "downloaded":
		order = "e.downloaded_at DESC"
	case "pending":
		order = "CASE e.download WHEN 'downloading' THEN 0 WHEN 'queued' THEN 1 ELSE 2 END, e.id"
	}
	sql := episodeFrom + " WHERE " + strings.Join(where, " AND ") + " ORDER BY " + order
	if f.Limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, f.Offset)
	}
	return sql, args
}

// ListEpisodes returns the episodes matching f with the person's own state.
func (s *Store) ListEpisodes(ctx context.Context, f EpisodeFilter) ([]Episode, error) {
	q, args := f.build()
	rows, err := s.DB.QueryContext(ctx, `SELECT `+episodeCols+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Episode{}
	for rows.Next() {
		e, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// CountEpisodes counts the episodes matching f.
func (s *Store) CountEpisodes(ctx context.Context, f EpisodeFilter) (int, error) {
	f.Limit, f.Offset = 0, 0
	q, args := f.build()
	q = q[:strings.LastIndex(q, " ORDER BY ")]
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1)`+q, args...).Scan(&n)
	return n, err
}

// GetEpisode returns one episode of one person's subscription.
func (s *Store) GetEpisode(ctx context.Context, userID, id int64) (*Episode, error) {
	return scanEpisode(s.DB.QueryRowContext(ctx, `SELECT `+episodeCols+episodeFrom+` WHERE e.id = ? AND p.user_id = ?`, id, userID))
}

// GetEpisodeAny returns an episode regardless of owner (for workers).
func (s *Store) GetEpisodeAny(ctx context.Context, id int64) (*Episode, error) {
	return scanEpisode(s.DB.QueryRowContext(ctx, `SELECT `+episodeCols+episodeFrom+` WHERE e.id = ?`, id))
}

// SetEpisodeSource records the show a picked episode came from.
func (s *Store) SetEpisodeSource(ctx context.Context, id int64, feed, title string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE episodes SET source_feed = ?, source_title = ? WHERE id = ?`, feed, title, id)
	return err
}

// EpisodeSource returns the show a picked episode came from (” when none).
func (s *Store) EpisodeSource(ctx context.Context, id int64) (feed, title string) {
	_ = s.DB.QueryRowContext(ctx, `SELECT source_feed, source_title FROM episodes WHERE id = ?`, id).Scan(&feed, &title)
	return
}

// FindPodcastBySource finds a person's subscription to a feed, or to a
// YouTube channel by its UC… id (a channel followed by @handle has another URL).
func (s *Store) FindPodcastBySource(ctx context.Context, userID int64, feed, youtubeID string) (*Podcast, error) {
	if p, err := s.FindPodcastByFeed(ctx, userID, feed); err == nil {
		return p, nil
	}
	if youtubeID == "" {
		return nil, ErrNotFound
	}
	return scanPodcast(s.DB.QueryRowContext(ctx, `SELECT `+podcastCols+podcastDerived+` FROM podcasts p WHERE p.user_id = ? AND p.youtube_id = ?`, userID, youtubeID), true)
}

// EpisodeExtras loads the chapter and SponsorBlock JSON of an episode.
func (s *Store) EpisodeExtras(ctx context.Context, id int64) (chapters, sponsors string, err error) {
	err = notFound(s.DB.QueryRowContext(ctx, `SELECT chapters, sponsors FROM episodes WHERE id = ?`, id).Scan(&chapters, &sponsors))
	return
}

// UpsertEpisode inserts an episode or refreshes its feed metadata, keeping
// the file and download state. It reports whether the row is new.
func (s *Store) UpsertEpisode(ctx context.Context, e *Episode) (bool, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM episodes WHERE podcast_id = ? AND guid = ?`, e.PodcastID, e.GUID).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if id > 0 {
		e.ID = id
		_, err := s.DB.ExecContext(ctx, `UPDATE episodes SET title = ?, description = ?, link = ?,
			enclosure_url = CASE WHEN ? = '' THEN enclosure_url ELSE ? END, enclosure_type = CASE WHEN ? = '' THEN enclosure_type ELSE ? END,
			enclosure_bytes = CASE WHEN ? = 0 THEN enclosure_bytes ELSE ? END,
			duration = CASE WHEN ? = 0 THEN duration ELSE ? END, pub_date = CASE WHEN ? = '' THEN pub_date ELSE ? END,
			artwork_url = CASE WHEN ? = '' THEN artwork_url ELSE ? END, season = ?, episode_no = ?, episode_type = ?, explicit = ?, is_video = ?,
			chapters_url = ?, transcript_url = ?, position = CASE WHEN ? < 0 THEN position ELSE ? END WHERE id = ?`,
			e.Title, e.Description, e.Link, e.EnclosureURL, e.EnclosureURL, e.EnclosureType, e.EnclosureType, e.EnclosureBytes, e.EnclosureBytes,
			e.Duration, e.Duration, e.PubDate, e.PubDate, e.ArtworkURL, e.ArtworkURL, e.Season, e.EpisodeNo, e.EpisodeType, boolInt(e.Explicit), boolInt(e.IsVideo),
			e.ChaptersURL, e.TranscriptURL, e.Position, e.Position, id)
		return false, err
	}
	if e.Download == "" {
		e.Download = DownloadNone
	}
	if e.Position < 0 {
		e.Position = 0
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO episodes (podcast_id, guid, title, description, link, enclosure_url, enclosure_type, enclosure_bytes,
		duration, pub_date, artwork_url, season, episode_no, episode_type, explicit, is_video, chapters_url, transcript_url, chapters, sponsors, youtube_id, position, download)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.PodcastID, e.GUID, e.Title, e.Description, e.Link, e.EnclosureURL, e.EnclosureType, e.EnclosureBytes,
		e.Duration, e.PubDate, e.ArtworkURL, e.Season, e.EpisodeNo, e.EpisodeType, boolInt(e.Explicit), boolInt(e.IsVideo), e.ChaptersURL, e.TranscriptURL,
		e.ChaptersJSON, e.SponsorsJSON, e.YouTubeID, e.Position, e.Download)
	if err != nil {
		return false, conflict(err)
	}
	e.ID, _ = res.LastInsertId()
	return true, nil
}

// SetEpisodeMeta stores what a download learned (YouTube: real date, duration, description).
func (s *Store) SetEpisodeMeta(ctx context.Context, id int64, title, description, pubDate string, duration int, artworkURL, link string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE episodes SET title = CASE WHEN ? = '' THEN title ELSE ? END, description = CASE WHEN ? = '' THEN description ELSE ? END,
		pub_date = CASE WHEN ? = '' THEN pub_date ELSE ? END, duration = CASE WHEN ? = 0 THEN duration ELSE ? END,
		artwork_url = CASE WHEN ? = '' THEN artwork_url ELSE ? END, link = CASE WHEN ? = '' THEN link ELSE ? END WHERE id = ?`,
		title, title, description, description, pubDate, pubDate, duration, duration, artworkURL, artworkURL, link, link, id)
	return err
}

// SetEpisodeExtras stores chapters and SponsorBlock segments as JSON.
func (s *Store) SetEpisodeExtras(ctx context.Context, id int64, chapters, sponsors string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE episodes SET chapters = CASE WHEN ? = '' THEN chapters ELSE ? END, sponsors = CASE WHEN ? = '' THEN sponsors ELSE ? END WHERE id = ?`, chapters, chapters, sponsors, sponsors, id)
	return err
}

// SetDownloadState moves an episode's file state.
func (s *Store) SetDownloadState(ctx context.Context, id int64, state, errMsg string) error {
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	tries := ""
	if state == DownloadFailed {
		tries = ", download_tries = download_tries + 1"
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE episodes SET download = ?, download_error = ?`+tries+` WHERE id = ?`, state, errMsg, id)
	return err
}

// SetEpisodeFile records a finished download.
func (s *Store) SetEpisodeFile(ctx context.Context, id int64, path string, bytes int64, mime string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE episodes SET file_path = ?, file_bytes = ?, file_type = ?, downloaded_at = ?, download = 'done', download_error = '', download_tries = 0 WHERE id = ?`, path, bytes, mime, Now(), id)
	return err
}

// ClearEpisodeFile forgets a file (deleted by hand or culled).
func (s *Store) ClearEpisodeFile(ctx context.Context, id int64, state string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE episodes SET file_path = '', file_bytes = 0, file_type = '', download = ?, download_error = '' WHERE id = ?`, state, id)
	return err
}

// GiveUpDownload stops the hourly retries of a failed download (a person can
// still queue it again by hand).
func (s *Store) GiveUpDownload(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE episodes SET download_tries = 99 WHERE id = ? AND download = 'failed'`, id)
	return err
}

// QueueDownload marks an episode for the download worker (idempotent).
func (s *Store) QueueDownload(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE episodes SET download = 'queued', download_error = '' WHERE id = ? AND download IN ('none', 'failed', 'culled')`, id)
	return err
}

// ClaimDownload takes the oldest queued episode for a worker; nil when none.
func (s *Store) ClaimDownload(ctx context.Context) (*Episode, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM episodes WHERE download = 'queued' ORDER BY id LIMIT 1`).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE episodes SET download = 'downloading' WHERE id = ? AND download = 'queued'`, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil
	}
	return s.GetEpisodeAny(ctx, id)
}

// ResetStaleDownloads puts episodes left "downloading" by a crash back in the queue.
func (s *Store) ResetStaleDownloads(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE episodes SET download = 'queued' WHERE download = 'downloading'`)
	return err
}

// CountDownloads reports queued + running downloads on the box.
func (s *Store) CountDownloads(ctx context.Context, userID int64) (queued, running int) {
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(CASE WHEN e.download = 'queued' THEN 1 END), COUNT(CASE WHEN e.download = 'downloading' THEN 1 END)
		FROM episodes e JOIN podcasts p ON p.id = e.podcast_id WHERE p.user_id = ?`, userID).Scan(&queued, &running)
	return
}

// CullCandidate is a downloaded file older than its subscription's retention.
type CullCandidate struct {
	EpisodeID int64
	UserID    int64
	FilePath  string
	Title     string
}

// ListCullCandidates finds downloaded episodes past their retention. Episodes
// in the person's queue or saved list are kept; played ones go first.
func (s *Store) ListCullCandidates(ctx context.Context, now time.Time) ([]CullCandidate, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id, p.user_id, e.file_path, e.title FROM episodes e JOIN podcasts p ON p.id = e.podcast_id
		LEFT JOIN queue q ON q.episode_id = e.id AND q.user_id = p.user_id
		LEFT JOIN saved sv ON sv.episode_id = e.id AND sv.user_id = p.user_id
		WHERE e.download = 'done' AND p.retention_days > 0 AND e.downloaded_at <> ''
		  AND e.downloaded_at < strftime('%Y-%m-%dT%H:%M:%SZ', ?, '-' || p.retention_days || ' days')
		  AND q.episode_id IS NULL AND sv.episode_id IS NULL`, now.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CullCandidate
	for rows.Next() {
		var c CullCandidate
		if err := rows.Scan(&c.EpisodeID, &c.UserID, &c.FilePath, &c.Title); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TrimEpisodes keeps at most keep episodes per RSS subscription (oldest,
// never played, never downloaded ones go). 0 = keep all.
func (s *Store) TrimEpisodes(ctx context.Context, podcastID int64, keep int) error {
	if keep <= 0 {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM episodes WHERE podcast_id = ? AND download = 'none'
		AND id NOT IN (SELECT episode_id FROM progress WHERE episode_id IN (SELECT id FROM episodes WHERE podcast_id = ?))
		AND id NOT IN (SELECT id FROM episodes WHERE podcast_id = ? ORDER BY pub_date DESC, id DESC LIMIT ?)`, podcastID, podcastID, podcastID, keep)
	return err
}

// DeleteEpisode removes one episode (YouTube videos the person does not want).
func (s *Store) DeleteEpisode(ctx context.Context, userID, id int64) (string, error) {
	var file string
	if err := s.DB.QueryRowContext(ctx, `SELECT e.file_path FROM episodes e JOIN podcasts p ON p.id = e.podcast_id WHERE e.id = ? AND p.user_id = ?`, id, userID).Scan(&file); err != nil {
		return "", notFound(err)
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM episodes WHERE id = ?`, id)
	return file, err
}

// LatestEpisodeDate is the newest pub_date of a subscription.
func (s *Store) LatestEpisodeDate(ctx context.Context, podcastID int64) string {
	var d string
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(pub_date), '') FROM episodes WHERE podcast_id = ?`, podcastID).Scan(&d)
	return d
}

// EpisodeIDsByYouTube maps a subscription's known video ids to episode ids.
func (s *Store) EpisodeIDsByYouTube(ctx context.Context, podcastID int64) (map[string]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT youtube_id, id FROM episodes WHERE podcast_id = ? AND youtube_id <> ''`, podcastID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var yid string
		var id int64
		if err := rows.Scan(&yid, &id); err != nil {
			return nil, err
		}
		out[yid] = id
	}
	return out, rows.Err()
}
