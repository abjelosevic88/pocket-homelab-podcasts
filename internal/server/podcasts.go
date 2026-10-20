package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/feed"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/search"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/ytdlp"
)

// podcastJSON is a subscription as the UI sees it (artwork resolved).
type podcastJSON struct {
	*store.Podcast
	Artwork    string `json:"artwork"`
	Refreshing bool   `json:"refreshing"`
}

func (s *Server) podcastOut(p *store.Podcast) podcastJSON {
	_, busy := s.refreshing.Load(p.ID)
	return podcastJSON{Podcast: p, Artwork: s.podcastArt(p.ID, p.ArtworkFile, p.ArtworkURL), Refreshing: busy}
}

func (s *Server) podcastArt(id int64, file, remote string) string {
	if file != "" {
		return fmt.Sprintf("/api/v1/podcasts/%d/artwork?v=%s", id, url.QueryEscape(file))
	}
	return remote
}

// ---------- search / preview ----------

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []search.Result{}, "warnings": nil})
		return
	}
	source := r.URL.Query().Get("source")
	st, _ := s.store.GetSettings(r.Context(), me(r))
	if source == "" {
		source = st.SearchSource
	}
	results, warnings := s.searchFor(r.Context(), me(r)).Search(r.Context(), q, source, 25)
	writeJSON(w, http.StatusOK, map[string]any{"results": s.markSubscribed(r.Context(), me(r), results), "warnings": warnings})
}

func (s *Server) handleTrending(w http.ResponseWriter, r *http.Request) {
	results, err := s.searchFor(r.Context(), me(r)).Trending(r.Context(), r.URL.Query().Get("lang"), 20)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": s.markSubscribed(r.Context(), me(r), results)})
}

type resultJSON struct {
	search.Result
	SubscribedID int64 `json:"subscribed_id,omitempty"`
}

func (s *Server) markSubscribed(ctx context.Context, uid int64, rs []search.Result) []resultJSON {
	pods, _ := s.store.ListPodcasts(ctx, uid, "", "", "")
	byURL := map[string]int64{}
	for _, p := range pods {
		byURL[strings.ToLower(strings.TrimSuffix(p.FeedURL, "/"))] = p.ID
	}
	out := make([]resultJSON, 0, len(rs))
	for _, r := range rs {
		out = append(out, resultJSON{Result: r, SubscribedID: byURL[strings.ToLower(strings.TrimSuffix(r.FeedURL, "/"))]})
	}
	return out
}

func (s *Server) handleTestPodcastIndex(w http.ResponseWriter, r *http.Request) {
	var in struct{ Key, Secret string }
	_ = decode(r, &in)
	c := s.searchFor(r.Context(), me(r))
	if in.Key != "" {
		c = s.search.With(strings.TrimSpace(in.Key), strings.TrimSpace(in.Secret))
	}
	if err := c.Test(r.Context()); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handlePreview looks at a URL before subscribing: a feed or a YouTube
// channel / playlist / video.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.URL) == "" {
		writeErr(w, http.StatusBadRequest, "url is required")
		return
	}
	in.URL = strings.TrimSpace(in.URL)
	if kind, norm, _, err := ytdlp.Classify(in.URL); err == nil {
		if !s.ytAvailable() {
			writeErr(w, http.StatusBadRequest, "yt-dlp is not available on this box, so YouTube cannot be added")
			return
		}
		if kind == ytdlp.KindVideo {
			info, err := s.yt.VideoInfo(r.Context(), norm)
			if err != nil {
				writeErr(w, http.StatusBadGateway, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"kind": "video", "url": norm, "title": info.Title, "author": info.Channel, "image": info.Thumbnail, "duration": info.Duration, "description": info.Description})
			return
		}
		l, err := s.yt.Index(r.Context(), norm, 30)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		items := []map[string]any{}
		for _, e := range l.Entries {
			items = append(items, map[string]any{"title": e.Title, "duration": e.Duration, "date": e.UploadDate, "guid": "yt:" + e.ID, "image": e.Thumbnail, "link": e.URL, "has_audio": true})
		}
		existing, _ := s.store.FindPodcastByFeed(r.Context(), me(r), norm)
		out := map[string]any{"kind": "youtube", "youtube_kind": kind, "url": norm, "title": l.Title, "author": l.Uploader, "image": l.Thumbnail, "description": l.Description, "episodes": items}
		if existing != nil {
			out["subscribed_id"] = existing.ID
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	u := in.URL
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	res, err := feed.Fetch(r.Context(), s.http, u, "", "", s.userAgent())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "could not read the feed: "+err.Error())
		return
	}
	f := res.Feed
	items := []map[string]any{}
	for i, it := range f.Items {
		if i == 60 {
			break
		}
		items = append(items, map[string]any{"title": it.Title, "duration": it.Duration, "date": store.FormatTime(it.PubDate), "guid": it.GUID, "description": it.Description, "image": it.Image,
			"has_audio": it.EnclosureURL != "", "audio": it.EnclosureURL, "link": it.Link, "season": it.Season, "episode": it.Episode})
	}
	existing, _ := s.store.FindPodcastByFeed(r.Context(), me(r), u)
	out := map[string]any{"kind": "rss", "url": u, "title": f.Title, "author": f.Author, "image": f.Image, "description": feed.StripHTML(f.Description), "episodes": items, "episode_count": len(f.Items)}
	if existing != nil {
		out["subscribed_id"] = existing.ID
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) ytAvailable() bool {
	v, _ := s.ytVersion.Load().(string)
	if v != "" {
		return true
	}
	if ver, err := s.yt.Version(context.Background()); err == nil {
		s.ytVersion.Store(ver)
		return true
	}
	return false
}

// ---------- subscriptions ----------

func (s *Server) handleListPodcasts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pods, err := s.store.ListPodcasts(r.Context(), me(r), q.Get("q"), q.Get("kind"), q.Get("sort"))
	if err != nil {
		s.fail(w, err, "list podcasts")
		return
	}
	out := make([]podcastJSON, 0, len(pods))
	for i := range pods {
		out = append(out, s.podcastOut(&pods[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// subscribeInput is what the add flow sends: a URL (feed or YouTube) plus optional overrides.
type subscribeInput struct {
	URL           string  `json:"url"`
	Title         string  `json:"title"`
	AutoDownload  *bool   `json:"auto_download"`
	DownloadLimit *int    `json:"download_limit"`
	RetentionDays *int    `json:"retention_days"`
	MinDuration   *int    `json:"min_duration"`
	TitleFilter   string  `json:"title_filter"`
	Notify        bool    `json:"notify"`
	PlaybackSpeed float64 `json:"playback_speed"`
	PubDateMode   string  `json:"pub_date_mode"`
}

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	var in subscribeInput
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.URL) == "" {
		writeErr(w, http.StatusBadRequest, "url is required")
		return
	}
	p, err := s.subscribe(r.Context(), me(r), in)
	if err != nil {
		var se *subscribeError
		if errors.As(err, &se) {
			writeErr(w, se.status, se.msg)
			return
		}
		s.fail(w, err, "subscribe")
		return
	}
	writeJSON(w, http.StatusCreated, s.podcastOut(p))
}

type subscribeError struct {
	status int
	msg    string
}

func (e *subscribeError) Error() string { return e.msg }

// subscribe adds a feed or YouTube source for a person and indexes it.
func (s *Server) subscribe(ctx context.Context, uid int64, in subscribeInput) (*store.Podcast, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	in.URL = strings.TrimSpace(in.URL)
	p := &store.Podcast{UserID: uid, Title: strings.TrimSpace(in.Title), Notify: in.Notify, PlaybackSpeed: in.PlaybackSpeed, AutoPlayNext: -1,
		RetentionDays: st.RetentionDays, PubDateMode: "published", SponsorBlock: true, AutoDownload: st.AutoDownloadDefault}
	if in.RetentionDays != nil {
		p.RetentionDays = *in.RetentionDays
	}
	if in.AutoDownload != nil {
		p.AutoDownload = *in.AutoDownload
	}
	if in.PubDateMode == "downloaded" {
		p.PubDateMode = "downloaded"
	}
	var listing *ytdlp.Listing
	if kind, norm, id, err := ytdlp.Classify(in.URL); err == nil {
		if kind == ytdlp.KindVideo {
			return nil, &subscribeError{http.StatusBadRequest, "that is a single video: use “Add a video” instead"}
		}
		if !s.ytAvailable() {
			return nil, &subscribeError{http.StatusBadRequest, "yt-dlp is not available on this box, so YouTube cannot be added"}
		}
		if ex, _ := s.store.FindPodcastByFeed(ctx, uid, norm); ex != nil {
			return nil, &subscribeError{http.StatusConflict, "you already follow " + ex.Title}
		}
		l, err := s.yt.Index(ctx, norm, s.cfg.YouTubeIndexLimit)
		if err != nil {
			return nil, &subscribeError{http.StatusBadGateway, err.Error()}
		}
		listing = l
		p.Kind, p.FeedURL, p.YouTubeID, p.YouTubeKind = store.KindYouTube, norm, firstNonEmpty(l.ID, id), kind
		if p.Title == "" {
			p.Title = firstNonEmpty(l.Title, l.Uploader, "YouTube")
		}
		p.Author, p.Description, p.ArtworkURL, p.SiteURL = l.Uploader, l.Description, l.Thumbnail, firstNonEmpty(l.URL, norm)
		p.DownloadLimit, p.MinDuration, p.TitleFilter = st.YouTubeLimit, st.YouTubeMinDuration, strings.TrimSpace(in.TitleFilter)
		if in.DownloadLimit != nil {
			p.DownloadLimit = *in.DownloadLimit
		}
		if in.MinDuration != nil {
			p.MinDuration = *in.MinDuration
		}
		if p.TitleFilter != "" {
			if _, err := regexp.Compile("(?i)" + p.TitleFilter); err != nil {
				return nil, &subscribeError{http.StatusBadRequest, "title filter is not a valid regular expression"}
			}
		}
		p.AutoDownload = true
	} else {
		u := in.URL
		if !strings.Contains(u, "://") {
			u = "https://" + u
		}
		if ex, _ := s.store.FindPodcastByFeed(ctx, uid, u); ex != nil {
			return nil, &subscribeError{http.StatusConflict, "you already follow " + ex.Title}
		}
		res, err := feed.Fetch(ctx, s.http, u, "", "", s.userAgent())
		if err != nil {
			return nil, &subscribeError{http.StatusBadGateway, "could not read the feed: " + err.Error()}
		}
		f := res.Feed
		p.Kind, p.FeedURL = store.KindRSS, u
		if p.Title == "" {
			p.Title = firstNonEmpty(f.Title, u)
		}
		p.Author, p.Description, p.ArtworkURL, p.SiteURL, p.Language, p.Categories, p.Explicit = f.Author, f.Description, f.Image, f.Link, f.Language, strings.Join(f.Categories, ", "), f.Explicit
	}
	if err := s.store.CreatePodcast(ctx, p); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, &subscribeError{http.StatusConflict, "you already follow this one"}
		}
		return nil, err
	}
	n, err := s.refreshPodcast(ctx, p, listing)
	if err != nil {
		s.log.Warn("first refresh failed", "podcast", p.Title, "err", err)
	}
	s.store.AddActivity(ctx, uid, "subscribe", fmt.Sprintf("Subscribed to %s (%d episode%s)", p.Title, n, plural(n)), p.ID, 0)
	s.publish(uid, "podcasts.subscribed", p.Title, map[string]any{"podcast_id": p.ID, "title": p.Title, "kind": p.Kind, "url": p.FeedURL})
	if p.ArtworkFile == "" {
		go s.cacheArtwork(context.Background(), p) // the first refresh failed before caching it
	}
	return s.store.GetPodcast(ctx, uid, p.ID)
}

func (s *Server) handleGetPodcast(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPodcast(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get podcast")
		return
	}
	writeJSON(w, http.StatusOK, s.podcastOut(p))
}

func (s *Server) handleUpdatePodcast(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.store.GetPodcast(ctx, me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get podcast")
		return
	}
	var in struct {
		Title          *string  `json:"title"`
		FeedURL        *string  `json:"feed_url"`
		Favorite       *bool    `json:"favorite"`
		PlaybackSpeed  *float64 `json:"playback_speed"`
		StartSkip      *int     `json:"start_skip"`
		EndSkip        *int     `json:"end_skip"`
		AutoPlayNext   *int     `json:"auto_play_next"`
		Notify         *bool    `json:"notify"`
		AutoDownload   *bool    `json:"auto_download"`
		DownloadLimit  *int     `json:"download_limit"`
		RetentionDays  *int     `json:"retention_days"`
		MinDuration    *int     `json:"min_duration"`
		TitleFilter    *string  `json:"title_filter"`
		FeedCutoffDays *int     `json:"feed_cutoff_days"`
		PubDateMode    *string  `json:"pub_date_mode"`
		SponsorBlock   *bool    `json:"sponsorblock"`
		RefreshMinutes *int     `json:"refresh_minutes"`
		Description    *string  `json:"description"`
		Author         *string  `json:"author"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) != "" {
		p.Title = strings.TrimSpace(*in.Title)
	}
	if in.FeedURL != nil && p.Kind == store.KindRSS && strings.TrimSpace(*in.FeedURL) != "" {
		p.FeedURL = strings.TrimSpace(*in.FeedURL)
		p.ETag, p.LastModified = "", ""
	}
	if in.Favorite != nil {
		p.Favorite = *in.Favorite
	}
	if in.PlaybackSpeed != nil {
		if *in.PlaybackSpeed != 0 && (*in.PlaybackSpeed < 0.5 || *in.PlaybackSpeed > 3) {
			writeErr(w, http.StatusBadRequest, "playback speed must be between 0.5 and 3")
			return
		}
		p.PlaybackSpeed = *in.PlaybackSpeed
	}
	if in.StartSkip != nil {
		p.StartSkip = clampInt(*in.StartSkip, 0, 3600)
	}
	if in.EndSkip != nil {
		p.EndSkip = clampInt(*in.EndSkip, 0, 3600)
	}
	if in.AutoPlayNext != nil {
		p.AutoPlayNext = clampInt(*in.AutoPlayNext, -1, 1)
	}
	if in.Notify != nil {
		p.Notify = *in.Notify
	}
	if in.AutoDownload != nil {
		p.AutoDownload = *in.AutoDownload
	}
	if in.DownloadLimit != nil {
		p.DownloadLimit = clampInt(*in.DownloadLimit, 0, 500)
	}
	if in.RetentionDays != nil {
		p.RetentionDays = clampInt(*in.RetentionDays, 0, 3650)
	}
	if in.MinDuration != nil {
		p.MinDuration = clampInt(*in.MinDuration, 0, 86400)
	}
	if in.TitleFilter != nil {
		tf := strings.TrimSpace(*in.TitleFilter)
		if tf != "" {
			if _, err := regexp.Compile("(?i)" + tf); err != nil {
				writeErr(w, http.StatusBadRequest, "title filter is not a valid regular expression")
				return
			}
		}
		p.TitleFilter = tf
	}
	if in.FeedCutoffDays != nil {
		p.FeedCutoffDays = clampInt(*in.FeedCutoffDays, 0, 36500)
	}
	if in.PubDateMode != nil && (*in.PubDateMode == "published" || *in.PubDateMode == "downloaded") {
		p.PubDateMode = *in.PubDateMode
	}
	if in.SponsorBlock != nil {
		p.SponsorBlock = *in.SponsorBlock
	}
	if in.RefreshMinutes != nil {
		p.RefreshMinutes = clampInt(*in.RefreshMinutes, 0, 10080)
		if p.RefreshMinutes > 0 && p.RefreshMinutes < 5 {
			p.RefreshMinutes = 5
		}
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	if in.Author != nil {
		p.Author = strings.TrimSpace(*in.Author)
	}
	if err := s.store.UpdatePodcast(ctx, p); err != nil {
		s.fail(w, err, "update podcast")
		return
	}
	p, _ = s.store.GetPodcast(ctx, me(r), p.ID)
	writeJSON(w, http.StatusOK, s.podcastOut(p))
}

func (s *Server) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.store.GetPodcast(ctx, me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get podcast")
		return
	}
	files, err := s.store.DeletePodcast(ctx, me(r), p.ID)
	if err != nil {
		s.fail(w, err, "unsubscribe")
		return
	}
	for _, f := range files {
		s.removeEpisodeFile(f)
	}
	_ = os.RemoveAll(filepath.Join(s.cfg.MediaDir, fmt.Sprintf("%d", p.ID)))
	if p.ArtworkFile != "" {
		_ = os.Remove(filepath.Join(s.cfg.ArtworkDir, p.ArtworkFile))
	}
	s.store.AddActivity(ctx, me(r), "unsubscribe", "Unsubscribed from "+p.Title, 0, 0)
	s.publish(me(r), "podcasts.unsubscribed", p.Title, map[string]any{"podcast_id": p.ID, "title": p.Title, "kind": p.Kind})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRefreshPodcast(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPodcast(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get podcast")
		return
	}
	n, err := s.refreshPodcast(r.Context(), p, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	p, _ = s.store.GetPodcast(r.Context(), me(r), p.ID)
	writeJSON(w, http.StatusOK, map[string]any{"new": n, "podcast": s.podcastOut(p)})
}

// handleRefreshAll refreshes every subscription of the caller in the background.
func (s *Server) handleRefreshAll(w http.ResponseWriter, r *http.Request) {
	pods, err := s.store.ListPodcasts(r.Context(), me(r), "", "", "")
	if err != nil {
		s.fail(w, err, "list podcasts")
		return
	}
	go func() {
		ctx := context.Background()
		for i := range pods {
			p := pods[i]
			_, _ = s.refreshPodcast(ctx, &p, nil)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]int{"podcasts": len(pods)})
}

func (s *Server) handleFavoritePodcast(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Favorite bool `json:"favorite"`
	}
	_ = decode(r, &in)
	if err := s.store.SetPodcastFavorite(r.Context(), me(r), idParam(r, "id"), in.Favorite); err != nil {
		s.fail(w, err, "favorite")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"favorite": in.Favorite})
}

func (s *Server) handlePodcastEpisodes(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPodcast(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get podcast")
		return
	}
	f := filterFromQuery(r, me(r))
	f.PodcastID = p.ID
	if f.Sort == "" && p.Kind == store.KindYouTube {
		f.Sort = "listing"
	}
	eps, err := s.store.ListEpisodes(r.Context(), f)
	if err != nil {
		s.fail(w, err, "list episodes")
		return
	}
	total, _ := s.store.CountEpisodes(r.Context(), f)
	writeJSON(w, http.StatusOK, map[string]any{"episodes": s.episodesOut(eps), "total": total})
}

func (s *Server) handlePodcastMarkAll(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPodcast(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get podcast")
		return
	}
	var in struct {
		Played bool `json:"played"`
	}
	_ = decode(r, &in)
	eps, err := s.store.ListEpisodes(r.Context(), store.EpisodeFilter{UserID: me(r), PodcastID: p.ID})
	if err != nil {
		s.fail(w, err, "list episodes")
		return
	}
	for _, e := range eps {
		if e.Completed != in.Played {
			_ = s.store.SetCompleted(r.Context(), me(r), e.ID, in.Played)
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"episodes": len(eps)})
}

// handlePodcastDownloadAll queues the newest n episodes (default 5) that have no file.
func (s *Server) handlePodcastDownloadAll(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPodcast(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get podcast")
		return
	}
	var in struct {
		Count int `json:"count"`
	}
	_ = decode(r, &in)
	if in.Count <= 0 {
		in.Count = 5
	}
	eps, err := s.store.ListEpisodes(r.Context(), store.EpisodeFilter{UserID: me(r), PodcastID: p.ID, Downloaded: -1, Status: "unfinished", Sort: "listing", Limit: clampInt(in.Count, 1, 200)})
	if err != nil {
		s.fail(w, err, "list episodes")
		return
	}
	n := 0
	for _, e := range eps {
		if e.Download == store.DownloadQueued || e.Download == store.DownloadDownloading {
			continue
		}
		if err := s.store.QueueDownload(r.Context(), e.ID); err == nil {
			n++
		}
	}
	s.kickDownloads()
	writeJSON(w, http.StatusAccepted, map[string]int{"queued": n})
}

func (s *Server) handlePodcastDeleteFiles(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPodcast(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get podcast")
		return
	}
	eps, err := s.store.ListEpisodes(r.Context(), store.EpisodeFilter{UserID: me(r), PodcastID: p.ID, Downloaded: 1})
	if err != nil {
		s.fail(w, err, "list episodes")
		return
	}
	onlyPlayed := queryBool(r, "played")
	n := 0
	for _, e := range eps {
		if onlyPlayed && !e.Completed {
			continue
		}
		s.removeEpisodeFile(e.FilePath)
		_ = s.store.ClearEpisodeFile(r.Context(), e.ID, store.DownloadNone)
		n++
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": n})
}

// ---------- artwork ----------

func (s *Server) handlePodcastArtwork(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPodcast(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get podcast")
		return
	}
	s.serveArtwork(w, r, p)
}

func (s *Server) serveArtwork(w http.ResponseWriter, r *http.Request, p *store.Podcast) {
	if p.ArtworkFile != "" {
		if full := filepath.Join(s.cfg.ArtworkDir, filepath.Base(p.ArtworkFile)); fileExists(full) {
			w.Header().Set("Cache-Control", "public, max-age=604800")
			http.ServeFile(w, r, full)
			return
		}
	}
	if p.ArtworkURL != "" {
		http.Redirect(w, r, p.ArtworkURL, http.StatusFound)
		return
	}
	writeErr(w, http.StatusNotFound, "no artwork")
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// handleSetPodcastArtwork accepts an uploaded image (multipart "file") or a
// JSON {url} to use as the show's artwork.
func (s *Server) handleSetPodcastArtwork(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.store.GetPodcast(ctx, me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get podcast")
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var in struct {
			URL string `json:"url"`
		}
		if err := decode(r, &in); err != nil || strings.TrimSpace(in.URL) == "" {
			writeErr(w, http.StatusBadRequest, "url is required")
			return
		}
		p.ArtworkURL = strings.TrimSpace(in.URL)
		_ = s.store.UpdatePodcast(ctx, p)
		s.cacheArtwork(ctx, p)
		writeJSON(w, http.StatusOK, s.podcastOut(p))
		return
	}
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "image too large (12 MB max)")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		writeErr(w, http.StatusBadRequest, "use a JPEG, PNG or WebP image")
		return
	}
	name := fmt.Sprintf("p%d-%d%s", p.ID, time.Now().Unix(), ext)
	dst, err := os.Create(filepath.Join(s.cfg.ArtworkDir, name))
	if err != nil {
		s.fail(w, err, "save artwork")
		return
	}
	_, err = io.Copy(dst, file)
	dst.Close()
	if err != nil {
		s.fail(w, err, "save artwork")
		return
	}
	if p.ArtworkFile != "" {
		_ = os.Remove(filepath.Join(s.cfg.ArtworkDir, p.ArtworkFile))
	}
	p.ArtworkFile = name
	_ = s.store.SetPodcastArtworkFile(ctx, p.ID, name)
	writeJSON(w, http.StatusOK, s.podcastOut(p))
}

func (s *Server) handleDeletePodcastArtwork(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPodcast(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get podcast")
		return
	}
	if p.ArtworkFile != "" {
		_ = os.Remove(filepath.Join(s.cfg.ArtworkDir, p.ArtworkFile))
		_ = s.store.SetPodcastArtworkFile(r.Context(), p.ID, "")
		p.ArtworkFile = ""
	}
	go s.cacheArtwork(context.Background(), p) // back to the feed's own
	writeJSON(w, http.StatusOK, s.podcastOut(p))
}

// ---------- OPML ----------

func (s *Server) handleExportOPML(w http.ResponseWriter, r *http.Request) {
	pods, err := s.store.ListPodcasts(r.Context(), me(r), "", "", "title")
	if err != nil {
		s.fail(w, err, "list podcasts")
		return
	}
	var entries []feed.OPMLEntry
	for _, p := range pods {
		entries = append(entries, feed.OPMLEntry{Title: p.Title, FeedURL: p.FeedURL, SiteURL: p.SiteURL})
	}
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-podcasts.opml"`)
	_, _ = w.Write(feed.BuildOPML("Pocket Podcasts subscriptions", entries))
}

// handleImportOPML subscribes to every feed in an OPML file (multipart "file"
// or raw body). YouTube URLs in it become YouTube subscriptions.
func (s *Server) handleImportOPML(w http.ResponseWriter, r *http.Request) {
	var data []byte
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			writeErr(w, http.StatusBadRequest, "file too large")
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, http.StatusBadRequest, "file is required")
			return
		}
		defer f.Close()
		data, _ = io.ReadAll(io.LimitReader(f, 8<<20))
	} else {
		data, _ = io.ReadAll(io.LimitReader(r.Body, 8<<20))
	}
	entries, err := feed.ParseOPML(data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "not an OPML file: "+err.Error())
		return
	}
	uid := me(r)
	added, skipped := 0, 0
	var failed []string
	for _, e := range entries {
		if ex, _ := s.store.FindPodcastByFeed(r.Context(), uid, e.FeedURL); ex != nil {
			skipped++
			continue
		}
		if _, err := s.subscribe(r.Context(), uid, subscribeInput{URL: e.FeedURL, Title: e.Title}); err != nil {
			failed = append(failed, e.Title+": "+err.Error())
			continue
		}
		added++
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "skipped": skipped, "failed": failed})
}
