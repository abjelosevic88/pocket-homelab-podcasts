package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/feed"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/ytdlp"
)

// ---------- refresh scheduler ----------

func (s *Server) kickRefresh() {
	select {
	case s.refreshKick <- struct{}{}:
	default:
	}
}

func (s *Server) kickDownloads() {
	select {
	case s.downloadKick <- struct{}{}:
	default:
	}
}

// refreshLoop refreshes every subscription that is due, one minute at a time.
func (s *Server) refreshLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	first := time.After(10 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-first:
		case <-t.C:
		case <-s.refreshKick:
		}
		s.refreshDue(ctx)
	}
}

func (s *Server) refreshInterval(ctx context.Context, p *store.Podcast, cache map[int64]int) time.Duration {
	if p.RefreshMinutes > 0 {
		return time.Duration(p.RefreshMinutes) * time.Minute
	}
	m, ok := cache[p.UserID]
	if !ok {
		st, _ := s.store.GetSettings(ctx, p.UserID)
		m = st.RefreshMinutes
		if m <= 0 {
			m = s.cfg.RefreshMinutes
		}
		cache[p.UserID] = m
	}
	return time.Duration(m) * time.Minute
}

func (s *Server) refreshDue(ctx context.Context) {
	pods, err := s.store.ListAllPodcasts(ctx)
	if err != nil {
		s.log.Error("list podcasts", "err", err)
		return
	}
	cache := map[int64]int{}
	now := time.Now()
	sem := make(chan struct{}, 3)
	for i := range pods {
		p := pods[i]
		iv := s.refreshInterval(ctx, &p, cache)
		if p.Failures > 0 {
			// back off a failing feed: 2^failures × interval, at most a day
			for n := 0; n < p.Failures && iv < 24*time.Hour; n++ {
				iv *= 2
			}
		}
		if last := store.ParseTime(p.LastRefreshAt); !last.IsZero() && now.Sub(last) < iv {
			continue
		}
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			s.refreshPodcast(ctx, &p, nil)
		}()
	}
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}
}

// refreshPodcast fetches new episodes of one subscription. For YouTube a
// listing fetched by the caller (at subscribe time) can be passed in.
func (s *Server) refreshPodcast(ctx context.Context, p *store.Podcast, listing *ytdlp.Listing) (newCount int, err error) {
	if p.FeedURL == PicksFeedURL {
		return 0, nil // hand-picked episodes: nothing to fetch
	}
	if _, busy := s.refreshing.LoadOrStore(p.ID, true); busy {
		return 0, errors.New("already refreshing")
	}
	defer s.refreshing.Delete(p.ID)
	first := p.LastRefreshOK == ""
	var created []store.Episode
	if p.Kind == store.KindYouTube {
		created, err = s.refreshYouTube(ctx, p, listing, first)
	} else {
		created, err = s.refreshRSS(ctx, p, first)
	}
	if err != nil {
		_ = s.store.UpdateFeedState(ctx, p.ID, p.ETag, p.LastModified, err)
		s.store.AddActivity(ctx, p.UserID, "error", fmt.Sprintf("%s: refresh failed: %s", p.Title, err.Error()), p.ID, 0)
		s.log.Warn("refresh failed", "podcast", p.Title, "err", err)
		return 0, err
	}
	if len(created) > 0 {
		s.store.AddActivity(ctx, p.UserID, "refresh", fmt.Sprintf("%s: %d new episode%s", p.Title, len(created), plural(len(created))), p.ID, 0)
		if !first && p.Notify {
			s.notifyNew(ctx, p, created)
		}
	}
	if p.ArtworkFile == "" && p.ArtworkURL != "" {
		s.cacheArtwork(ctx, p)
	}
	return len(created), nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (s *Server) refreshRSS(ctx context.Context, p *store.Podcast, first bool) ([]store.Episode, error) {
	res, err := feed.Fetch(ctx, s.http, p.FeedURL, p.ETag, p.LastModified, s.userAgent())
	if err != nil {
		return nil, err
	}
	if res.NotModified {
		return nil, s.store.UpdateFeedState(ctx, p.ID, res.ETag, res.LastModified, nil)
	}
	f := res.Feed
	_ = s.store.UpdatePodcastMeta(ctx, p.ID, f.Title, f.Author, f.Description, f.Image, f.Link, f.Language, strings.Join(f.Categories, ", "), f.Explicit)
	if p.ArtworkURL == "" && f.Image != "" {
		p.ArtworkURL = f.Image
	}
	cutoff := time.Time{}
	if p.FeedCutoffDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -p.FeedCutoffDays)
	}
	subscribed := store.ParseTime(p.SubscribedAt)
	var created []store.Episode
	for i, it := range f.Items {
		if it.EnclosureURL == "" {
			continue
		}
		if !cutoff.IsZero() && !it.PubDate.IsZero() && it.PubDate.Before(cutoff) {
			continue
		}
		e := store.Episode{PodcastID: p.ID, GUID: it.GUID, Title: it.Title, Description: it.Description, Link: it.Link,
			EnclosureURL: it.EnclosureURL, EnclosureType: it.EnclosureType, EnclosureBytes: it.EnclosureLen, Duration: it.Duration,
			PubDate: store.FormatTime(it.PubDate), ArtworkURL: it.Image, Season: it.Season, EpisodeNo: it.Episode, EpisodeType: it.EpisodeType,
			Explicit: it.Explicit, IsVideo: strings.HasPrefix(it.EnclosureType, "video/"), ChaptersURL: it.ChaptersURL, TranscriptURL: it.TranscriptURL, Position: i}
		if e.Title == "" {
			e.Title = "Untitled"
		}
		isNew, err := s.store.UpsertEpisode(ctx, &e, s.episodeAdded(ctx, p, &e))
		if err != nil {
			return nil, err
		}
		if isNew {
			created = append(created, e)
			// auto-download: every new episode once subscribed; on the first
			// refresh only the newest one (the rest is history).
			if p.AutoDownload && ((!first && (it.PubDate.IsZero() || it.PubDate.After(subscribed.Add(-7*24*time.Hour)))) || (first && len(created) == 1)) {
				_ = s.store.QueueDownload(ctx, e.ID)
			}
		}
	}
	if len(created) > 0 && p.AutoDownload {
		s.kickDownloads()
	}
	_ = s.store.TrimEpisodes(ctx, p.ID, s.cfg.MaxEpisodesPerFeed)
	return created, s.store.UpdateFeedState(ctx, p.ID, res.ETag, res.LastModified, nil)
}

func (s *Server) refreshYouTube(ctx context.Context, p *store.Podcast, l *ytdlp.Listing, first bool) ([]store.Episode, error) {
	if l == nil {
		var err error
		if l, err = s.yt.Index(ctx, p.FeedURL, s.cfg.YouTubeIndexLimit); err != nil {
			return nil, err
		}
	}
	if l.ID != "" && p.YouTubeID == "" {
		p.YouTubeID = l.ID
	}
	_ = s.store.UpdatePodcastMeta(ctx, p.ID, l.Title, l.Uploader, l.Description, l.Thumbnail, l.URL, "", "", false)
	if p.ArtworkURL == "" && l.Thumbnail != "" {
		p.ArtworkURL = l.Thumbnail
	}
	var titleRe *regexp.Regexp
	if p.TitleFilter != "" {
		if re, err := regexp.Compile("(?i)" + p.TitleFilter); err == nil {
			titleRe = re
		}
	}
	known, err := s.store.EpisodeIDsByYouTube(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	firstIndex := len(known) == 0
	var created []store.Episode
	toQueue := 0
	if firstIndex {
		toQueue = p.DownloadLimit
	}
	removed := 0
	members := s.yt.HasCookies() // a paying member's cookies make members-only videos downloadable
	for i, en := range l.Entries {
		if en.MembersOnly && !members {
			// only the channel's paying members can get these; drop any listed before
			if id, ok := known[en.ID]; ok && s.store.RetireMembersOnly(ctx, p.ID, id) {
				removed++
			}
			continue
		}
		if en.Live {
			continue
		}
		if p.MinDuration > 0 && en.Duration > 0 && en.Duration < p.MinDuration {
			continue
		}
		if titleRe != nil && !titleRe.MatchString(en.Title) {
			continue
		}
		pub := ""
		if en.Timestamp > 0 {
			pub = time.Unix(en.Timestamp, 0).UTC().Format(time.RFC3339)
		} else if len(en.UploadDate) == 8 {
			if t, err := time.Parse("20060102", en.UploadDate); err == nil {
				pub = t.UTC().Format(time.RFC3339)
			}
		}
		e := store.Episode{PodcastID: p.ID, GUID: "yt:" + en.ID, Title: en.Title, Link: en.URL, EnclosureType: "audio/mp4", Duration: en.Duration,
			PubDate: pub, ArtworkURL: en.Thumbnail, YouTubeID: en.ID, Position: i}
		if id, ok := known[en.ID]; ok {
			e.ID = id
			e.PubDate = ""                                                                        // the listing's date is approximate; keep the exact one the download stored
			if _, err := s.store.UpsertEpisode(ctx, &e, s.episodeAdded(ctx, p, &e)); err != nil { // keeps the row, refreshes position/title
				return nil, err
			}
			continue
		}
		isNew, err := s.store.UpsertEpisode(ctx, &e, s.episodeAdded(ctx, p, &e))
		if err != nil {
			return nil, err
		}
		if !isNew {
			continue
		}
		created = append(created, e)
		// first index: the newest download_limit videos; afterwards every new upload
		if (firstIndex && toQueue > 0) || !firstIndex {
			_ = s.store.QueueDownload(ctx, e.ID)
			toQueue--
		}
	}
	if removed > 0 {
		s.store.AddActivity(ctx, p.UserID, "refresh", fmt.Sprintf("%s: removed %d members-only video%s (only paying channel members can get them)", p.Title, removed, plural(removed)), p.ID, 0)
	}
	if len(created) > 0 {
		s.kickDownloads()
	}
	return created, s.store.UpdateFeedState(ctx, p.ID, "", "", nil)
}

// notifyNew tells the person about new episodes through their channel.
func (s *Server) notifyNew(ctx context.Context, p *store.Podcast, eps []store.Episode) {
	st, _ := s.store.GetSettings(ctx, p.UserID)
	kind, url, token := st.NotifyKind, st.NotifyURL, st.NotifyToken
	if url == "" && s.cfg.NotifyURL != "" && s.isOwner(ctx, p.UserID) {
		kind, url = s.cfg.NotifyKind, s.cfg.NotifyURL
	}
	if url == "" {
		return
	}
	title := "New episode of " + p.Title
	body := eps[0].Title
	if len(eps) > 1 {
		title = fmt.Sprintf("%d new episodes of %s", len(eps), p.Title)
		var lines []string
		for i, e := range eps {
			if i == 5 {
				lines = append(lines, "…")
				break
			}
			lines = append(lines, "• "+e.Title)
		}
		body = strings.Join(lines, "\n")
	}
	link := ""
	if s.cfg.BaseURL != "" {
		link = fmt.Sprintf("%s/podcasts/%d", s.cfg.BaseURL, p.ID)
	}
	if err := notify.Send(ctx, kind, url, token, title, body, link); err != nil {
		s.log.Warn("notify", "err", err)
		s.store.AddActivity(ctx, p.UserID, "error", "notification failed: "+err.Error(), p.ID, 0)
		return
	}
	s.store.AddActivity(ctx, p.UserID, "notify", title, p.ID, 0)
}

// cacheArtwork keeps a copy of the show's artwork so the UI and the public
// feeds never depend on the publisher's CDN.
func (s *Server) cacheArtwork(ctx context.Context, p *store.Podcast) {
	if p.ArtworkURL == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.ArtworkURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", s.userAgent())
	res, err := s.http.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		if res != nil {
			res.Body.Close()
		}
		return
	}
	defer res.Body.Close()
	ext := ".jpg"
	switch {
	case strings.Contains(res.Header.Get("Content-Type"), "png"):
		ext = ".png"
	case strings.Contains(res.Header.Get("Content-Type"), "webp"):
		ext = ".webp"
	}
	name := fmt.Sprintf("p%d-%d%s", p.ID, time.Now().Unix(), ext)
	tmp := filepath.Join(s.cfg.ArtworkDir, name+".part")
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	n, err := io.Copy(f, io.LimitReader(res.Body, 10<<20))
	f.Close()
	if err != nil || n < 100 {
		os.Remove(tmp)
		return
	}
	if err := os.Rename(tmp, filepath.Join(s.cfg.ArtworkDir, name)); err != nil {
		return
	}
	if p.ArtworkFile != "" {
		os.Remove(filepath.Join(s.cfg.ArtworkDir, p.ArtworkFile))
	}
	p.ArtworkFile = name
	_ = s.store.SetPodcastArtworkFile(ctx, p.ID, name)
}

// ---------- downloads ----------

func (s *Server) downloadWorker(ctx context.Context, n int) {
	for {
		ep, err := s.store.ClaimDownload(ctx)
		if err != nil {
			s.log.Error("claim download", "err", err)
		}
		if ep == nil {
			select {
			case <-ctx.Done():
				return
			case <-s.downloadKick:
			case <-time.After(20 * time.Second):
			}
			continue
		}
		s.downloadEpisode(ctx, ep)
	}
}

var unsafeName = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

func safeName(s string) string {
	s = unsafeName.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, ". ")
	if len(s) > 110 {
		s = strings.TrimSpace(s[:110])
	}
	if s == "" {
		s = "episode"
	}
	return s
}

func (s *Server) episodeBase(e *store.Episode) string {
	date := strings.SplitN(e.PubDate, "T", 2)[0]
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	return fmt.Sprintf("%s %s [%d]", date, safeName(e.Title), e.ID)
}

func (s *Server) downloadEpisode(ctx context.Context, e *store.Episode) {
	p, err := s.store.GetPodcastAny(ctx, e.PodcastID)
	if err != nil {
		_ = s.store.SetDownloadState(ctx, e.ID, store.DownloadFailed, "subscription gone")
		return
	}
	dir := filepath.Join(s.cfg.MediaDir, fmt.Sprintf("%d", p.ID))
	base := s.episodeBase(e)
	var rel string
	var bytes int64
	mimeType := "audio/mpeg"
	if p.Kind == store.KindYouTube || e.YouTubeID != "" {
		d, err := s.yt.DownloadAudio(ctx, e.Link, dir, base, p.SponsorBlock)
		if err != nil {
			s.downloadFailed(ctx, p, e, err)
			return
		}
		rel, _ = filepath.Rel(s.cfg.MediaDir, d.File)
		bytes, mimeType = d.Bytes, "audio/mp4"
		in := d.Info
		pub := store.FormatTime(in.UploadDate)
		_ = s.store.SetEpisodeMeta(ctx, e.ID, in.Title, in.Description, pub, in.Duration, "", in.WebpageURL)
		if pub != "" {
			// the file name carries the date: rename when the index guessed wrong
			newBase := fmt.Sprintf("%s %s [%d]", pub[:10], safeName(firstNonEmpty(in.Title, e.Title)), e.ID)
			if newBase != base {
				if np := filepath.Join(dir, newBase+filepath.Ext(d.File)); os.Rename(d.File, np) == nil {
					rel, _ = filepath.Rel(s.cfg.MediaDir, np)
					if d.Thumbnail != "" {
						_ = os.Rename(d.Thumbnail, filepath.Join(dir, newBase+".jpg"))
					}
				}
			}
		}
		var ch, sb string
		if len(in.Chapters) > 0 {
			b, _ := json.Marshal(in.Chapters)
			ch = string(b)
		}
		if len(in.Sponsors) > 0 {
			b, _ := json.Marshal(in.Sponsors)
			sb = string(b)
		}
		_ = s.store.SetEpisodeExtras(ctx, e.ID, ch, sb)
	} else {
		var err error
		rel, bytes, mimeType, err = s.fetchEnclosure(ctx, e, dir, base)
		if err != nil {
			s.downloadFailed(ctx, p, e, err)
			return
		}
		if e.ChaptersURL != "" {
			s.fetchChapters(ctx, e)
		}
	}
	if err := s.store.SetEpisodeFile(ctx, e.ID, rel, bytes, mimeType, s.personEvent(ctx, p.UserID, func() (string, string, map[string]any) {
		return "podcasts.downloaded", e.Title, map[string]any{"podcast_id": p.ID, "podcast": p.Title, "episode_id": e.ID, "title": e.Title, "bytes": bytes}
	})); err != nil {
		s.log.Error("record download", "err", err)
		return
	}
	s.store.AddActivity(ctx, p.UserID, "download", fmt.Sprintf("Downloaded “%s” (%s) — %.1f MB", e.Title, p.Title, float64(bytes)/1048576), p.ID, e.ID)

	s.log.Info("downloaded", "podcast", p.Title, "episode", e.Title, "mb", bytes>>20)
}

// permanentFailure recognises errors a retry cannot fix (members-only,
// private, removed videos, a gone enclosure) so they are not retried.
func permanentFailure(err error) bool {
	m := strings.ToLower(err.Error())
	for _, k := range []string{"members-only", "members only", "channel's members", "private video", "video unavailable", "has been removed", "no longer available", "http 404", "http 410", "http 403", "sign in to confirm your age", "age-restricted"} {
		if strings.Contains(m, k) {
			return true
		}
	}
	return false
}

func (s *Server) downloadFailed(ctx context.Context, p *store.Podcast, e *store.Episode, err error) {
	_ = s.store.SetDownloadState(ctx, e.ID, store.DownloadFailed, err.Error())
	if permanentFailure(err) {
		_ = s.store.GiveUpDownload(ctx, e.ID)
	}
	s.store.AddActivity(ctx, p.UserID, "error", fmt.Sprintf("Download of “%s” failed: %s", e.Title, err.Error()), p.ID, e.ID)
	s.log.Warn("download failed", "episode", e.Title, "err", err)
}

// fetchEnclosure downloads an RSS episode's media file.
func (s *Server) fetchEnclosure(ctx context.Context, e *store.Episode, dir, base string) (rel string, size int64, mimeType string, err error) {
	if e.EnclosureURL == "" {
		return "", 0, "", errors.New("the episode has no media URL")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.EnclosureURL, nil)
	if err != nil {
		return "", 0, "", err
	}
	req.Header.Set("User-Agent", s.userAgent())
	cl := &http.Client{Timeout: 0, Transport: s.http.Transport}
	res, err := cl.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", 0, "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	mimeType = strings.SplitN(res.Header.Get("Content-Type"), ";", 2)[0]
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = e.EnclosureType
	}
	ext := extFor(mimeType, res.Request.URL.Path, e.EnclosureURL)
	tmp := filepath.Join(dir, base+ext+".part")
	f, err := os.Create(tmp)
	if err != nil {
		return "", 0, "", err
	}
	n, err := io.Copy(f, res.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return "", 0, "", err
	}
	if n < 1024 {
		os.Remove(tmp)
		return "", 0, "", errors.New("the server sent an empty file")
	}
	final := filepath.Join(dir, base+ext)
	if err := os.Rename(tmp, final); err != nil {
		return "", 0, "", err
	}
	rel, _ = filepath.Rel(s.cfg.MediaDir, final)
	return rel, n, mimeType, nil
}

func extFor(mimeType, urlPath, raw string) string {
	switch mimeType {
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/mp4", "audio/x-m4a", "audio/m4a", "audio/aac":
		return ".m4a"
	case "audio/ogg", "application/ogg":
		return ".ogg"
	case "audio/opus":
		return ".opus"
	case "audio/flac":
		return ".flac"
	case "video/mp4":
		return ".mp4"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	}
	for _, p := range []string{urlPath, raw} {
		if ext := strings.ToLower(path.Ext(strings.SplitN(p, "?", 2)[0])); len(ext) >= 3 && len(ext) <= 5 {
			return ext
		}
	}
	if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
		return exts[0]
	}
	return ".mp3"
}

// fetchChapters stores podcast 2.0 chapters (JSON) with the episode.
func (s *Server) fetchChapters(ctx context.Context, e *store.Episode) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.ChaptersURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", s.userAgent())
	res, err := s.http.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		if res != nil {
			res.Body.Close()
		}
		return
	}
	defer res.Body.Close()
	var doc struct {
		Chapters []struct {
			Start float64 `json:"startTime"`
			End   float64 `json:"endTime"`
			Title string  `json:"title"`
			URL   string  `json:"url"`
			Img   string  `json:"img"`
		} `json:"chapters"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&doc) != nil || len(doc.Chapters) == 0 {
		return
	}
	var ch []store.Chapter
	for _, c := range doc.Chapters {
		ch = append(ch, store.Chapter{Start: c.Start, End: c.End, Title: c.Title, URL: c.URL, Image: c.Img})
	}
	b, _ := json.Marshal(ch)
	_ = s.store.SetEpisodeExtras(ctx, e.ID, string(b), "")
}

// removeEpisodeFile deletes the media file and its thumbnail.
func (s *Server) removeEpisodeFile(rel string) {
	if rel == "" {
		return
	}
	full := filepath.Join(s.cfg.MediaDir, rel)
	_ = os.Remove(full)
	_ = os.Remove(strings.TrimSuffix(full, filepath.Ext(full)) + ".jpg")
	// drop the podcast's folder when it emptied
	if entries, err := os.ReadDir(filepath.Dir(full)); err == nil && len(entries) == 0 {
		_ = os.Remove(filepath.Dir(full))
	}
}

// ---------- housekeeping ----------

func (s *Server) housekeeping(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	first := time.After(2 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case <-first:
		case <-t.C:
		}
		s.cull(ctx)
		s.retryFailed(ctx)
		s.repairYouTubeMeta(ctx)
		_ = s.store.PurgeSessions(ctx)
		s.store.PruneActivity(ctx)
	}
}

// cull deletes files past their subscription's retention.
func (s *Server) cull(ctx context.Context) {
	cands, err := s.store.ListCullCandidates(ctx, time.Now())
	if err != nil {
		s.log.Error("cull", "err", err)
		return
	}
	for _, c := range cands {
		s.removeEpisodeFile(c.FilePath)
		_ = s.store.ClearEpisodeFile(ctx, c.EpisodeID, store.DownloadCulled)
		s.store.AddActivity(ctx, c.UserID, "cull", "Removed the file of “"+c.Title+"” (retention)", 0, c.EpisodeID)
	}
	if len(cands) > 0 {
		s.log.Info("culled files past retention", "n", len(cands))
	}
}

// retryFailed gives failed downloads another go, three times at most.
func (s *Server) retryFailed(ctx context.Context) {
	res, err := s.store.DB.ExecContext(ctx, `UPDATE episodes SET download = 'queued' WHERE download = 'failed' AND download_tries < 3`)
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			s.kickDownloads()
		}
	}
}

// repairYouTubeMeta refetches title, notes and date of downloaded videos that
// lost them, a few per hour so YouTube is not hammered.
func (s *Server) repairYouTubeMeta(ctx context.Context) {
	eps, err := s.store.MissingYouTubeMeta(ctx, 25)
	if err != nil || len(eps) == 0 {
		return
	}
	fixed := 0
	for _, e := range eps {
		in, err := s.yt.VideoInfo(ctx, e.Link)
		if err != nil {
			continue
		}
		desc := in.Description
		if desc == "" {
			desc = " " // nothing to fetch: stop asking
		}
		if s.store.SetEpisodeMeta(ctx, e.ID, in.Title, desc, store.FormatTime(in.UploadDate), in.Duration, "", in.WebpageURL) == nil {
			fixed++
		}
	}
	s.log.Info("restored show notes of YouTube episodes", "n", fixed)
}
