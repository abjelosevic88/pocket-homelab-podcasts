package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

// Settings are the box title (admins change it) plus the caller's own preferences.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r, st))
}

// publicSettings adds what the UI needs to know about the environment.
func (s *Server) publicSettings(r *http.Request, st store.Settings) map[string]any {
	ctx, uid := r.Context(), me(r)
	if st.FeedKey == "" {
		st.FeedKey, _ = s.store.EnsureFeedKey(ctx, uid)
	}
	ytv, _ := s.ytVersion.Load().(string)
	return map[string]any{
		"title": st.Title, "setup_complete": st.SetupComplete, "landing": st.Landing,
		"playback_speed": st.PlaybackSpeed, "skip_forward": st.SkipForward, "skip_back": st.SkipBack, "auto_play_next": st.AutoPlayNext,
		"complete_at": st.CompleteAt, "auto_skip_sponsors": st.AutoSkipSponsors, "skip_categories": st.SkipCategories, "queue_next": st.QueueNext,
		"refresh_minutes": st.RefreshMinutes, "refresh_default": s.cfg.RefreshMinutes, "auto_download_default": st.AutoDownloadDefault,
		"retention_days": st.RetentionDays, "youtube_limit": st.YouTubeLimit, "youtube_min_duration": st.YouTubeMinDuration, "keep_played": st.KeepPlayed,
		"search_source": st.SearchSource, "podcastindex_key": st.PodcastIndexKey, "podcastindex_secret_set": st.PodcastIndexSecret != "",
		"podcastindex_env": s.cfg.PodcastIndexKey != "" && s.isOwner(ctx, uid),
		"notify_kind":      st.NotifyKind, "notify_url": st.NotifyURL, "notify_token_set": st.NotifyToken != "",
		"notify_env": s.cfg.NotifyURL != "" && s.isOwner(ctx, uid),
		"feed_key":   st.FeedKey, "feed_base_url": st.FeedBaseURL, "feed_url": s.feedBase(r, uid) + "/feed/" + st.FeedKey + "/",
		"yt_dlp": ytv, "yt_dlp_updatable": s.yt.SiteDir != "", "youtube_check_minutes": s.cfg.YouTubeCheckMinutes, "ytdlp_cookies": s.yt.HasCookies(),
	}
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cur, err := s.store.GetSettings(ctx, me(r))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	var in struct {
		store.Settings
		PodcastIndexSecret *string `json:"podcastindex_secret"`
		NotifyToken        *string `json:"notify_token"`
	}
	in.Settings = cur
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	st := in.Settings
	if strings.TrimSpace(st.Title) == "" {
		st.Title = store.DefaultTitle
	}
	if st.Landing != "podcasts" && st.Landing != "queue" && st.Landing != "episodes" {
		st.Landing = "home"
	}
	if st.PlaybackSpeed < 0.5 || st.PlaybackSpeed > 3 {
		st.PlaybackSpeed = 1
	}
	st.SkipForward = clampInt(st.SkipForward, 5, 300)
	st.SkipBack = clampInt(st.SkipBack, 5, 300)
	st.CompleteAt = clampInt(st.CompleteAt, 50, 100)
	var cats []string
	for _, c := range strings.Split(strings.ToLower(st.SkipCategories), ",") {
		c = strings.TrimSpace(c)
		switch c {
		case "sponsor", "selfpromo", "interaction", "intro", "outro", "preview", "music_offtopic", "filler":
			cats = append(cats, c)
		}
	}
	st.SkipCategories = strings.Join(cats, ",")
	st.RefreshMinutes = clampInt(st.RefreshMinutes, 0, 10080)
	if st.RefreshMinutes > 0 && st.RefreshMinutes < 5 {
		st.RefreshMinutes = 5
	}
	st.RetentionDays = clampInt(st.RetentionDays, 0, 3650)
	st.YouTubeLimit = clampInt(st.YouTubeLimit, 0, 500)
	st.YouTubeMinDuration = clampInt(st.YouTubeMinDuration, 0, 86400)
	if st.SearchSource != "itunes" && st.SearchSource != "podcastindex" {
		st.SearchSource = "all"
	}
	st.PodcastIndexKey = strings.TrimSpace(st.PodcastIndexKey)
	st.PodcastIndexSecret = cur.PodcastIndexSecret
	if in.PodcastIndexSecret != nil {
		st.PodcastIndexSecret = strings.TrimSpace(*in.PodcastIndexSecret)
	}
	if st.PodcastIndexKey == "" {
		st.PodcastIndexSecret = ""
	}
	if st.NotifyKind != "ntfy" && st.NotifyKind != "webhook" {
		st.NotifyKind = "apprise"
	}
	st.NotifyURL = strings.TrimSpace(st.NotifyURL)
	st.NotifyToken = cur.NotifyToken
	if in.NotifyToken != nil {
		st.NotifyToken = strings.TrimSpace(*in.NotifyToken)
	}
	st.FeedBaseURL = strings.TrimRight(strings.TrimSpace(st.FeedBaseURL), "/")
	st.SetupComplete = cur.SetupComplete
	if u := userFrom(ctx); u == nil || u.Role != "admin" {
		st.Title = cur.Title // the title is the box's: only admins change it
	}
	if err := s.store.SaveSettings(ctx, me(r), st); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if st.Title != cur.Title {
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	st.FeedKey = cur.FeedKey
	writeJSON(w, http.StatusOK, s.publicSettings(r, st))
}

func (s *Server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind, URL, Token string
	}
	_ = decode(r, &in)
	st, _ := s.store.GetSettings(r.Context(), me(r))
	kind, u, tok := st.NotifyKind, st.NotifyURL, st.NotifyToken
	if in.URL != "" {
		kind, u, tok = in.Kind, in.URL, in.Token
		if tok == "" {
			tok = st.NotifyToken
		}
	}
	if u == "" && s.cfg.NotifyURL != "" && s.isOwner(r.Context(), me(r)) {
		kind, u = s.cfg.NotifyKind, s.cfg.NotifyURL
	}
	if err := notify.Send(r.Context(), kind, u, tok, "Pocket Podcasts", "Notifications work. New episodes of the shows you flag will arrive here.", s.cfg.BaseURL); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRotateFeedKey gives the person a new feed key; old feed URLs stop working.
func (s *Server) handleRotateFeedKey(w http.ResponseWriter, r *http.Request) {
	key := store.RandomToken(16)
	if err := s.store.SetUserSetting(r.Context(), me(r), "feed_key", key); err != nil {
		s.fail(w, err, "feed key")
		return
	}
	st, _ := s.store.GetSettings(r.Context(), me(r))
	writeJSON(w, http.StatusOK, s.publicSettings(r, st))
}

// handleUpdateYtDlp installs the newest yt-dlp (admin only, one at a time).
func (s *Server) handleUpdateYtDlp(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if !s.ytBusy.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, "an update is already running")
		return
	}
	defer s.ytBusy.Store(false)
	v, err := s.yt.Update(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.ytVersion.Store(v)
	s.store.AddActivity(r.Context(), me(r), "system", "yt-dlp updated to "+v, 0, 0)
	writeJSON(w, http.StatusOK, map[string]string{"version": v})
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(queryInt(r, "limit"), 0, 500)
	if limit == 0 {
		limit = 100
	}
	items, err := s.store.ListActivity(r.Context(), me(r), r.URL.Query().Get("kind"), limit)
	if err != nil {
		s.fail(w, err, "activity")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	var mediaBytes int64
	var mediaFiles int
	_ = filepath.Walk(s.cfg.MediaDir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			mediaBytes += info.Size()
			mediaFiles++
		}
		return nil
	})
	counts := s.store.UserCounts(r.Context(), me(r))
	if u := userFrom(r.Context()); u != nil && u.Role == "admin" {
		if n, err := s.store.CountUsers(r.Context()); err == nil {
			counts["users"] = n
		}
	}
	ytv, _ := s.ytVersion.Load().(string)
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"media_dir": s.cfg.MediaDir, "media_files": mediaFiles, "media_bytes": mediaBytes,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": counts, "yt_dlp": ytv, "youtube_check": s.quickCheckStatus(), "ytdlp_cookies": s.yt.HasCookies(), "refresh_minutes": s.cfg.RefreshMinutes, "download_workers": s.cfg.DownloadWorkers,
	})
}

// ---------- backup / export / import ----------

// handleBackup downloads the whole database — every account — so only an
// admin, as the operator of the box, may take it.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if u := userFrom(r.Context()); u == nil || u.Role != "admin" {
		writeErr(w, http.StatusForbidden, "only an admin can download the database; export your subscriptions instead")
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-podcasts-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	defer os.Remove(dst)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	http.ServeFile(w, r, dst)
}

// Export is the portable JSON shape of one person's data (also accepted by /import).
type Export struct {
	Version    string           `json:"version"`
	ExportedAt string           `json:"exported_at"`
	Podcasts   []ExportPodcast  `json:"podcasts"`
	Playlists  []store.Playlist `json:"playlists"`
	Settings   store.Settings   `json:"settings"`
}

// ExportPodcast is a subscription with the person's state on its episodes.
type ExportPodcast struct {
	store.Podcast
	Episodes []ExportEpisode `json:"episodes"`
}

// ExportEpisode is an episode identified by guid with progress and flags.
type ExportEpisode struct {
	GUID         string `json:"guid"`
	Title        string `json:"title"`
	PubDate      string `json:"pub_date,omitempty"`
	Position     int    `json:"position,omitempty"`
	Completed    bool   `json:"completed,omitempty"`
	LastPlayedAt string `json:"last_played_at,omitempty"`
	PlayCount    int    `json:"play_count,omitempty"`
	Saved        bool   `json:"saved,omitempty"`
	QueuePos     int    `json:"queue_pos,omitempty"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	pods, err := s.store.ListPodcasts(ctx, uid, "", "", "title")
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	ex := Export{Version: Version, ExportedAt: store.Now(), Podcasts: []ExportPodcast{}}
	for _, p := range pods {
		ep := ExportPodcast{Podcast: p, Episodes: []ExportEpisode{}}
		eps, err := s.store.ListEpisodes(ctx, store.EpisodeFilter{UserID: uid, PodcastID: p.ID, Limit: 0})
		if err != nil {
			s.fail(w, err, "export")
			return
		}
		for _, e := range eps {
			if e.LastPlayedAt == "" && !e.Saved && e.QueuePos == 0 && !e.Completed {
				continue
			}
			ep.Episodes = append(ep.Episodes, ExportEpisode{GUID: e.GUID, Title: e.Title, PubDate: e.PubDate, Position: e.PlayPosition, Completed: e.Completed, LastPlayedAt: e.LastPlayedAt, PlayCount: e.PlayCount, Saved: e.Saved, QueuePos: e.QueuePos})
		}
		ex.Podcasts = append(ex.Podcasts, ep)
	}
	ex.Playlists, _ = s.store.ListPlaylists(ctx, uid)
	ex.Settings, _ = s.store.GetSettings(ctx, uid)
	ex.Settings.PodcastIndexSecret, ex.Settings.NotifyToken, ex.Settings.FeedKey = "", "", ""
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-podcasts-export.json"`)
	writeJSON(w, http.StatusOK, ex)
}

// handleImportJSON subscribes to the podcasts of an export (or of the
// PinePods importer script) and restores progress, saved and queued episodes.
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	var ex Export
	if err := decode(r, &ex); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx, uid := r.Context(), me(r)
	added, existing, progress := 0, 0, 0
	var failed []string
	for _, ep := range ex.Podcasts {
		if ep.FeedURL == "" || ep.FeedURL == PicksFeedURL {
			continue
		}
		p, _ := s.store.FindPodcastByFeed(ctx, uid, ep.FeedURL)
		if p == nil {
			in := subscribeInput{URL: ep.FeedURL, Title: ep.Title, Notify: ep.Notify, PlaybackSpeed: ep.PlaybackSpeed, PubDateMode: ep.PubDateMode, TitleFilter: ep.TitleFilter}
			ad, dl, rd, md := ep.AutoDownload, ep.DownloadLimit, ep.RetentionDays, ep.MinDuration
			in.AutoDownload, in.DownloadLimit, in.RetentionDays, in.MinDuration = &ad, &dl, &rd, &md
			var err error
			if p, err = s.subscribe(ctx, uid, in); err != nil {
				failed = append(failed, ep.Title+": "+err.Error())
				continue
			}
			p.StartSkip, p.EndSkip, p.Favorite, p.FeedCutoffDays = ep.StartSkip, ep.EndSkip, ep.Favorite, ep.FeedCutoffDays
			_ = s.store.UpdatePodcast(ctx, p)
			added++
		} else {
			existing++
		}
		if len(ep.Episodes) == 0 {
			continue
		}
		eps, _ := s.store.ListEpisodes(ctx, store.EpisodeFilter{UserID: uid, PodcastID: p.ID, Limit: 0})
		byGUID := map[string]store.Episode{}
		byTitle := map[string]store.Episode{}
		for _, e := range eps {
			byGUID[e.GUID] = e
			byTitle[strings.ToLower(strings.TrimSpace(e.Title))] = e
		}
		for _, x := range ep.Episodes {
			e, ok := byGUID[x.GUID]
			if !ok {
				e, ok = byTitle[strings.ToLower(strings.TrimSpace(x.Title))]
			}
			if !ok {
				continue
			}
			if x.Position > 0 || x.Completed || x.LastPlayedAt != "" {
				_ = s.store.SaveProgress(ctx, uid, e.ID, x.Position, 0, x.Completed)
				if x.LastPlayedAt != "" {
					_, _ = s.store.DB.ExecContext(ctx, `UPDATE progress SET last_played_at = ?, play_count = MAX(play_count, ?) WHERE user_id = ? AND episode_id = ?`, x.LastPlayedAt, x.PlayCount, uid, e.ID)
				}
				progress++
			}
			if x.Saved {
				_ = s.store.SetSaved(ctx, uid, e.ID, true)
			}
			if x.QueuePos > 0 {
				_ = s.store.AddToQueue(ctx, uid, e.ID, false)
			}
		}
	}
	for _, pl := range ex.Playlists {
		pl.ID, pl.UserID = 0, uid
		_ = s.store.CreatePlaylist(ctx, &pl)
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "existing": existing, "progress": progress, "failed": failed})
}
