package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

// filterFromQuery reads the common episode list parameters.
func filterFromQuery(r *http.Request, uid int64) store.EpisodeFilter {
	q := r.URL.Query()
	f := store.EpisodeFilter{UserID: uid, Query: q.Get("q"), Status: q.Get("status"), Kind: q.Get("kind"), Sort: q.Get("sort")}
	switch q.Get("downloaded") {
	case "1", "true", "yes":
		f.Downloaded = 1
	case "0", "false", "no":
		f.Downloaded = -1
	}
	f.Saved = queryBool(r, "saved")
	f.Queued = queryBool(r, "queued")
	if d := queryInt(r, "days"); d > 0 {
		f.Since = time.Now().AddDate(0, 0, -d).UTC().Format(time.RFC3339)
	}
	f.MinDuration = queryInt(r, "min_duration")
	f.MaxDuration = queryInt(r, "max_duration")
	f.Limit = clampInt(queryInt(r, "limit"), 0, 500)
	if f.Limit == 0 {
		f.Limit = 50
	}
	f.Offset = queryInt(r, "offset")
	if id := queryInt(r, "podcast"); id > 0 {
		f.PodcastID = int64(id)
	}
	return f
}

// episodesOut resolves artwork and stream URLs for the UI.
func (s *Server) episodesOut(eps []store.Episode) []store.Episode {
	for i := range eps {
		s.decorateEpisode(&eps[i])
	}
	return eps
}

func (s *Server) decorateEpisode(e *store.Episode) {
	if strings.HasPrefix(e.PodcastArtwork, "file:") {
		e.PodcastArtwork = s.podcastArt(e.PodcastID, strings.TrimPrefix(e.PodcastArtwork, "file:"), "")
	}
	if e.ArtworkURL == "" {
		e.ArtworkURL = e.PodcastArtwork
	}
	switch {
	case e.Download == store.DownloadDone && e.FilePath != "":
		e.StreamURL = fmt.Sprintf("/api/v1/episodes/%d/stream", e.ID)
	case e.EnclosureURL != "" && e.YouTubeID == "":
		e.StreamURL = e.EnclosureURL
	case e.YouTubeID != "" && e.Download != store.DownloadFailed:
		// the server fetches the video's audio when the player asks for it
		e.StreamURL = fmt.Sprintf("/api/v1/episodes/%d/stream", e.ID)
	}
}

func (s *Server) handleListEpisodes(w http.ResponseWriter, r *http.Request) {
	f := filterFromQuery(r, me(r))
	eps, err := s.store.ListEpisodes(r.Context(), f)
	if err != nil {
		s.fail(w, err, "list episodes")
		return
	}
	total, _ := s.store.CountEpisodes(r.Context(), f)
	writeJSON(w, http.StatusOK, map[string]any{"episodes": s.episodesOut(eps), "total": total})
}

// episodeDetail is an episode with its chapters and SponsorBlock segments.
type episodeDetail struct {
	*store.Episode
	Chapters []store.Chapter `json:"chapters"`
	Sponsors []store.Segment `json:"sponsors"`
	Source   *episodeSource  `json:"source,omitempty"`
}

// episodeSource is the show a picked episode came from.
type episodeSource struct {
	FeedURL      string `json:"feed_url"`
	Title        string `json:"title"`
	Kind         string `json:"kind"` // rss | youtube
	SubscribedID int64  `json:"subscribed_id,omitempty"`
}

func (s *Server) episodeDetail(r *http.Request, e *store.Episode) episodeDetail {
	s.decorateEpisode(e)
	d := episodeDetail{Episode: e, Chapters: []store.Chapter{}, Sponsors: []store.Segment{}}
	ch, sb, _ := s.store.EpisodeExtras(r.Context(), e.ID)
	if ch != "" {
		_ = json.Unmarshal([]byte(ch), &d.Chapters)
	}
	if sb != "" {
		_ = json.Unmarshal([]byte(sb), &d.Sponsors)
	}
	if feed, title := s.store.EpisodeSource(r.Context(), e.ID); feed != "" {
		src := &episodeSource{FeedURL: feed, Title: title, Kind: store.KindRSS}
		ytID := ""
		if strings.Contains(feed, "youtube.com") {
			src.Kind = store.KindYouTube
			if i := strings.Index(feed, "/channel/"); i >= 0 {
				ytID = strings.SplitN(feed[i+9:], "/", 2)[0]
			}
		}
		if p, err := s.store.FindPodcastBySource(r.Context(), me(r), feed, ytID); err == nil {
			src.SubscribedID = p.ID
		}
		d.Source = src
	}
	return d
}

func (s *Server) handleGetEpisode(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.GetEpisode(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get episode")
		return
	}
	writeJSON(w, http.StatusOK, s.episodeDetail(r, e))
}

func (s *Server) handleDeleteEpisode(w http.ResponseWriter, r *http.Request) {
	file, err := s.store.DeleteEpisode(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "delete episode")
		return
	}
	s.removeEpisodeFile(file)
	w.WriteHeader(http.StatusNoContent)
}

// ensureFile makes sure an episode's audio is on the server, downloading it
// right away when it is not (a player or a phone is waiting for it). The
// download runs on its own, so a client giving up does not waste it; the
// request waits for it until its own context ends.
func (s *Server) ensureFile(ctx context.Context, e *store.Episode) (*store.Episode, error) {
	if e.Download == store.DownloadDone && e.FilePath != "" && !fileExists(filepath.Join(s.cfg.MediaDir, filepath.FromSlash(e.FilePath))) {
		_ = s.store.ClearEpisodeFile(ctx, e.ID, store.DownloadNone) // the file went missing on disk
	}
	claimed, err := s.store.ClaimDownloadNow(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	if claimed {
		s.log.Info("downloading on request", "episode", e.Title)
		go func(id int64) {
			bg, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
			defer cancel()
			if ep, err := s.store.GetEpisodeAny(bg, id); err == nil {
				s.downloadEpisode(bg, ep)
			}
		}(e.ID)
	}
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		cur, err := s.store.GetEpisodeAny(ctx, e.ID)
		if err != nil {
			return nil, err
		}
		switch cur.Download {
		case store.DownloadDone:
			if cur.FilePath != "" {
				return cur, nil
			}
		case store.DownloadFailed:
			return nil, errors.New(firstNonEmpty(cur.DownloadError, "the download failed"))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

// handleStream serves an episode's file with range support (seeking),
// downloading it to the server first when it is not there yet.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.GetEpisode(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get episode")
		return
	}
	if e.FilePath == "" || e.Download != store.DownloadDone || !fileExists(filepath.Join(s.cfg.MediaDir, filepath.FromSlash(e.FilePath))) {
		// not on the server yet: fetch it now and answer with the file when it is there
		if e, err = s.ensureFile(r.Context(), e); err != nil {
			writeErr(w, http.StatusBadGateway, "could not get the audio: "+err.Error())
			return
		}
	}
	s.serveMedia(w, r, e, queryBool(r, "download"))
}

func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request, e *store.Episode, attachment bool) {
	full := filepath.Join(s.cfg.MediaDir, filepath.FromSlash(e.FilePath))
	f, err := os.Open(full)
	if err != nil {
		writeErr(w, http.StatusNotFound, "file is missing on disk")
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	if e.FileType != "" {
		w.Header().Set("Content-Type", e.FileType)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if attachment {
		w.Header().Set("Content-Disposition", `attachment; filename="`+url.PathEscape(filepath.Base(full))+`"`)
	}
	http.ServeContent(w, r, filepath.Base(full), fi.ModTime(), f)
}

func (s *Server) handleEpisodeArtwork(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.GetEpisode(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get episode")
		return
	}
	s.serveEpisodeArtwork(w, r, e)
}

func (s *Server) localThumb(e *store.Episode) string {
	if e.FilePath == "" {
		return ""
	}
	full := filepath.Join(s.cfg.MediaDir, filepath.FromSlash(e.FilePath))
	th := strings.TrimSuffix(full, filepath.Ext(full)) + ".jpg"
	if fileExists(th) {
		return th
	}
	return ""
}

func (s *Server) serveEpisodeArtwork(w http.ResponseWriter, r *http.Request, e *store.Episode) {
	if th := s.localThumb(e); th != "" {
		w.Header().Set("Cache-Control", "public, max-age=604800")
		http.ServeFile(w, r, th)
		return
	}
	if e.ArtworkURL != "" {
		http.Redirect(w, r, e.ArtworkURL, http.StatusFound)
		return
	}
	p, err := s.store.GetPodcastAny(r.Context(), e.PodcastID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "no artwork")
		return
	}
	s.serveArtwork(w, r, p)
}

// ---------- playback ----------

// handleProgress is called by the player every few seconds: where it is and
// how many seconds were listened since the last call.
func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, err := s.store.GetEpisode(ctx, me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get episode")
		return
	}
	var in struct {
		Position  int   `json:"position"`
		Listened  int   `json:"listened"`
		Duration  int   `json:"duration"`
		Completed *bool `json:"completed"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Position = clampInt(in.Position, 0, 86400*7)
	in.Listened = clampInt(in.Listened, 0, 3600)
	if in.Duration > 0 && e.Duration == 0 {
		_ = s.store.SetEpisodeMeta(ctx, e.ID, "", "", "", in.Duration, "", "")
		e.Duration = in.Duration
	}
	st, _ := s.store.GetSettings(ctx, me(r))
	completed := in.Completed != nil && *in.Completed
	if in.Completed == nil && e.Duration > 0 && st.CompleteAt > 0 && in.Position*100 >= e.Duration*st.CompleteAt {
		completed = true
	}
	wasPlayed, wasDone := e.LastPlayedAt != "", e.Completed
	if err := s.store.SaveProgress(ctx, me(r), e.ID, in.Position, in.Listened, completed); err != nil {
		s.fail(w, err, "save progress")
		return
	}
	if !wasPlayed && in.Listened > 0 {
		s.publish(me(r), "podcasts.played", e.Title, map[string]any{"podcast_id": e.PodcastID, "podcast": e.PodcastTitle, "episode_id": e.ID, "title": e.Title, "duration": e.Duration})
	}
	if completed && !wasDone {
		s.publish(me(r), "podcasts.finished", e.Title, map[string]any{"podcast_id": e.PodcastID, "podcast": e.PodcastTitle, "episode_id": e.ID, "title": e.Title, "duration": e.Duration})
		if completed && !st.KeepPlayed && e.FilePath != "" {
			// a played episode's file goes when retention is not asked to keep it
			if p, err := s.store.GetPodcastAny(ctx, e.PodcastID); err == nil && p.RetentionDays == 0 && p.Kind == store.KindRSS && !e.Saved {
				// RSS files are only a cache of the enclosure: drop them once played
				s.removeEpisodeFile(e.FilePath)
				_ = s.store.ClearEpisodeFile(ctx, e.ID, store.DownloadNone)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"position": in.Position, "completed": completed || wasDone})
}

func (s *Server) handleMarkPlayed(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.GetEpisode(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get episode")
		return
	}
	if err := s.store.SetCompleted(r.Context(), me(r), e.ID, true); err != nil {
		s.fail(w, err, "mark played")
		return
	}
	if !e.Completed {
		s.publish(me(r), "podcasts.finished", e.Title, map[string]any{"podcast_id": e.PodcastID, "podcast": e.PodcastTitle, "episode_id": e.ID, "title": e.Title, "duration": e.Duration, "manual": true})
	}
	_ = s.store.RemoveFromQueue(r.Context(), me(r), e.ID)
	writeJSON(w, http.StatusOK, map[string]bool{"completed": true})
}

func (s *Server) handleMarkUnplayed(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetCompleted(r.Context(), me(r), idParam(r, "id"), false); err != nil {
		s.fail(w, err, "mark unplayed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"completed": false})
}

func (s *Server) handleForgetEpisode(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearHistory(r.Context(), me(r), idParam(r, "id")); err != nil {
		s.fail(w, err, "forget")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- downloads ----------

func (s *Server) handleDownloadEpisode(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.GetEpisode(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get episode")
		return
	}
	if e.Download == store.DownloadDone {
		writeJSON(w, http.StatusOK, s.episodeDetail(r, e))
		return
	}
	if e.PodcastKind == store.KindYouTube && !s.ytAvailable() {
		writeErr(w, http.StatusBadRequest, "yt-dlp is not available on this box")
		return
	}
	if err := s.store.QueueDownload(r.Context(), e.ID); err != nil {
		s.fail(w, err, "queue download")
		return
	}
	s.kickDownloads()
	e, _ = s.store.GetEpisode(r.Context(), me(r), e.ID)
	writeJSON(w, http.StatusAccepted, s.episodeDetail(r, e))
}

func (s *Server) handleDeleteDownload(w http.ResponseWriter, r *http.Request) {
	e, err := s.store.GetEpisode(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get episode")
		return
	}
	s.removeEpisodeFile(e.FilePath)
	if err := s.store.ClearEpisodeFile(r.Context(), e.ID, store.DownloadNone); err != nil {
		s.fail(w, err, "delete download")
		return
	}
	e, _ = s.store.GetEpisode(r.Context(), me(r), e.ID)
	writeJSON(w, http.StatusOK, s.episodeDetail(r, e))
}

// handleDownloads lists what is on disk and what is pending.
func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	uid := me(r)
	pending, err := s.store.ListEpisodes(r.Context(), store.EpisodeFilter{UserID: uid, Pending: true, Sort: "pending", Limit: 200})
	if err != nil {
		s.fail(w, err, "list downloads")
		return
	}
	done, err := s.store.ListEpisodes(r.Context(), store.EpisodeFilter{UserID: uid, Downloaded: 1, Sort: "downloaded", Limit: clampInt(queryInt(r, "limit"), 0, 1000)})
	if err != nil {
		s.fail(w, err, "list downloads")
		return
	}
	var bytes int64
	for _, e := range done {
		bytes += e.FileBytes
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending": s.episodesOut(pending), "done": s.episodesOut(done), "bytes": bytes, "yt_dlp": s.ytVersion.Load()})
}

// ---------- queue ----------

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	eps, err := s.store.ListEpisodes(r.Context(), store.EpisodeFilter{UserID: me(r), Queued: true, Sort: "queue", Limit: 500})
	if err != nil {
		s.fail(w, err, "queue")
		return
	}
	writeJSON(w, http.StatusOK, s.episodesOut(eps))
}

func (s *Server) handleQueueAdd(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Front bool `json:"front"`
	}
	_ = decode(r, &in)
	if err := s.store.AddToQueue(r.Context(), me(r), idParam(r, "id"), in.Front); err != nil {
		s.fail(w, err, "queue")
		return
	}
	s.handleQueue(w, r)
}

func (s *Server) handleQueueRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RemoveFromQueue(r.Context(), me(r), idParam(r, "id")); err != nil {
		s.fail(w, err, "queue")
		return
	}
	s.handleQueue(w, r)
}

func (s *Server) handleQueueReorder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.ReorderQueue(r.Context(), me(r), in.IDs); err != nil {
		s.fail(w, err, "queue")
		return
	}
	s.handleQueue(w, r)
}

func (s *Server) handleQueueClear(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearQueue(r.Context(), me(r)); err != nil {
		s.fail(w, err, "queue")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleQueueNext(w http.ResponseWriter, r *http.Request) {
	nx, err := s.store.NextInQueue(r.Context(), me(r), int64(queryInt(r, "after")))
	if err != nil {
		s.fail(w, err, "queue")
		return
	}
	if nx == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, s.episodeDetail(r, nx))
}

// ---------- saved / history ----------

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetSaved(r.Context(), me(r), idParam(r, "id"), true); err != nil {
		s.fail(w, err, "save")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (s *Server) handleUnsave(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetSaved(r.Context(), me(r), idParam(r, "id"), false); err != nil {
		s.fail(w, err, "save")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": false})
}

func (s *Server) handleSaved(w http.ResponseWriter, r *http.Request) {
	eps, err := s.store.ListEpisodes(r.Context(), store.EpisodeFilter{UserID: me(r), Saved: true, Sort: "saved", Limit: 500})
	if err != nil {
		s.fail(w, err, "saved")
		return
	}
	writeJSON(w, http.StatusOK, s.episodesOut(eps))
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	f := filterFromQuery(r, me(r))
	f.History, f.Sort = true, "played"
	eps, err := s.store.ListEpisodes(r.Context(), f)
	if err != nil {
		s.fail(w, err, "history")
		return
	}
	total, _ := s.store.CountEpisodes(r.Context(), f)
	writeJSON(w, http.StatusOK, map[string]any{"episodes": s.episodesOut(eps), "total": total})
}

func (s *Server) handleClearHistory(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearHistory(r.Context(), me(r), 0); err != nil {
		s.fail(w, err, "history")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
