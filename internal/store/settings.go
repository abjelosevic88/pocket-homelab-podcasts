package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// DefaultTitle is the app title until someone changes it.
const DefaultTitle = "Pocket Podcasts"

// DefaultSkipCategories are the SponsorBlock categories skipped out of the box.
const DefaultSkipCategories = "sponsor,selfpromo,interaction"

func defaults() Settings {
	return Settings{
		Title: DefaultTitle, Landing: "home",
		PlaybackSpeed: 1, SkipForward: 30, SkipBack: 10, AutoPlayNext: true, CompleteAt: 95,
		AutoSkipSponsors: true, SkipCategories: DefaultSkipCategories, QueueNext: true,
		RefreshMinutes: 0, RetentionDays: 0, YouTubeLimit: 3, YouTubeMinDuration: 120,
		SearchSource: "all", NotifyKind: "apprise",
	}
}

// GetSettings loads the box settings plus the preferences of userID, with
// defaults applied. userID 0 gives the box settings with default preferences.
func (s *Store) GetSettings(ctx context.Context, userID int64) (Settings, error) {
	st := defaults()
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM settings WHERE key IN ('title', 'setup_complete')
		UNION ALL SELECT key, value FROM user_settings WHERE user_id = ?`, userID)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	atoi := func(v string, def int) int {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		return def
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return st, err
		}
		switch k {
		case "title":
			if v != "" {
				st.Title = v
			}
		case "setup_complete":
			st.SetupComplete = v == "1"
		case "landing":
			if v == "home" || v == "podcasts" || v == "queue" || v == "episodes" {
				st.Landing = v
			}
		case "playback_speed":
			if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0.5 && f <= 3 {
				st.PlaybackSpeed = f
			}
		case "skip_forward":
			st.SkipForward = atoi(v, st.SkipForward)
		case "skip_back":
			st.SkipBack = atoi(v, st.SkipBack)
		case "auto_play_next":
			st.AutoPlayNext = v != "0"
		case "complete_at":
			st.CompleteAt = atoi(v, st.CompleteAt)
		case "auto_skip_sponsors":
			st.AutoSkipSponsors = v != "0"
		case "skip_categories":
			st.SkipCategories = v
		case "queue_next":
			st.QueueNext = v != "0"
		case "refresh_minutes":
			st.RefreshMinutes = atoi(v, 0)
		case "auto_download_default":
			st.AutoDownloadDefault = v == "1"
		case "retention_days":
			st.RetentionDays = atoi(v, 0)
		case "youtube_limit":
			st.YouTubeLimit = atoi(v, st.YouTubeLimit)
		case "youtube_min_duration":
			st.YouTubeMinDuration = atoi(v, st.YouTubeMinDuration)
		case "keep_played":
			st.KeepPlayed = v == "1"
		case "search_source":
			if v == "all" || v == "itunes" || v == "podcastindex" {
				st.SearchSource = v
			}
		case "podcastindex_key":
			st.PodcastIndexKey = v
		case "podcastindex_secret":
			st.PodcastIndexSecret = v
		case "notify_kind":
			if v == "apprise" || v == "ntfy" || v == "webhook" {
				st.NotifyKind = v
			}
		case "notify_url":
			st.NotifyURL = v
		case "notify_token":
			st.NotifyToken = v
		case "feed_key":
			st.FeedKey = v
		case "feed_base_url":
			st.FeedBaseURL = strings.TrimRight(v, "/")
		}
	}
	return st, rows.Err()
}

func b2s(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// SaveSettings stores the personal preferences of userID (not the box title).
func (s *Store) SaveSettings(ctx context.Context, userID int64, st Settings) error {
	return s.putUserSettings(ctx, userID, map[string]string{
		"landing":               st.Landing,
		"playback_speed":        strconv.FormatFloat(st.PlaybackSpeed, 'f', -1, 64),
		"skip_forward":          strconv.Itoa(st.SkipForward),
		"skip_back":             strconv.Itoa(st.SkipBack),
		"auto_play_next":        b2s(st.AutoPlayNext),
		"complete_at":           strconv.Itoa(st.CompleteAt),
		"auto_skip_sponsors":    b2s(st.AutoSkipSponsors),
		"skip_categories":       st.SkipCategories,
		"queue_next":            b2s(st.QueueNext),
		"refresh_minutes":       strconv.Itoa(st.RefreshMinutes),
		"auto_download_default": b2s(st.AutoDownloadDefault),
		"retention_days":        strconv.Itoa(st.RetentionDays),
		"youtube_limit":         strconv.Itoa(st.YouTubeLimit),
		"youtube_min_duration":  strconv.Itoa(st.YouTubeMinDuration),
		"keep_played":           b2s(st.KeepPlayed),
		"search_source":         st.SearchSource,
		"podcastindex_key":      st.PodcastIndexKey,
		"podcastindex_secret":   st.PodcastIndexSecret,
		"notify_kind":           st.NotifyKind,
		"notify_url":            st.NotifyURL,
		"notify_token":          st.NotifyToken,
		"feed_base_url":         st.FeedBaseURL,
	})
}

// SetUserSetting stores one key for a person.
func (s *Store) SetUserSetting(ctx context.Context, userID int64, key, value string) error {
	return s.putUserSettings(ctx, userID, map[string]string{key: value})
}

// SaveBoxSettings stores the settings of the box itself: title and setup state.
func (s *Store) SaveBoxSettings(ctx context.Context, st Settings) error {
	kv := map[string]string{"title": st.Title, "setup_complete": b2s(st.SetupComplete)}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) putUserSettings(ctx context.Context, userID int64, kv map[string]string) error {
	if userID <= 0 {
		return ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?)
			ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`, userID, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// OwnerID is the account the box was set up with (the first user): keys
// given to the whole box through the environment are theirs.
func (s *Store) OwnerID(ctx context.Context) int64 {
	var id sql.NullInt64
	_ = s.DB.QueryRowContext(ctx, `SELECT MIN(id) FROM users`).Scan(&id)
	return id.Int64
}

// UserByFeedKey finds the person whose public feeds use key.
func (s *Store) UserByFeedKey(ctx context.Context, key string) (*User, error) {
	if key == "" {
		return nil, ErrNotFound
	}
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users u WHERE u.id = (SELECT user_id FROM user_settings WHERE key = 'feed_key' AND value = ?)`, key))
}

// EnsureFeedKey returns the person's feed key, creating one when missing.
func (s *Store) EnsureFeedKey(ctx context.Context, userID int64) (string, error) {
	var key string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id = ? AND key = 'feed_key'`, userID).Scan(&key)
	if err == nil && key != "" {
		return key, nil
	}
	key = RandomToken(16)
	return key, s.SetUserSetting(ctx, userID, "feed_key", key)
}
