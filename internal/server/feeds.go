package server

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/feed"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

// Public feeds: /feed/{key}/… where key is the person's secret feed key.
// They let Apple Podcasts, Overcast, AntennaPod… follow a subscription
// (YouTube channels as audio!) with the files served from this box.

func (s *Server) feedUser(w http.ResponseWriter, r *http.Request) *store.User {
	u, err := s.store.UserByFeedKey(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil
	}
	return u
}

// feedBase is the absolute URL feeds are published under.
func (s *Server) feedBase(r *http.Request, uid int64) string {
	st, _ := s.store.GetSettings(r.Context(), uid)
	if st.FeedBaseURL != "" {
		return st.FeedBaseURL
	}
	if s.cfg.BaseURL != "" {
		return s.cfg.BaseURL
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) handleFeedIndex(w http.ResponseWriter, r *http.Request) {
	u := s.feedUser(w, r)
	if u == nil {
		return
	}
	pods, err := s.store.ListPodcasts(r.Context(), u.ID, "", "", "title")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	base := s.feedBase(r, u.ID) + "/feed/" + chi.URLParam(r, "key")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Pocket Podcasts feeds</title>
<style>body{font-family:system-ui,sans-serif;max-width:720px;margin:40px auto;padding:0 16px;color:#222}li{margin:10px 0}code{font-size:12px;color:#555;word-break:break-all}</style>
<h1>Feeds</h1><p>Paste a feed URL into any podcast app. This page is private: anyone with the link can read it.</p><ul>`)
	for _, p := range pods {
		if p.FeedURL == PicksFeedURL && p.EpisodeCount == 0 {
			continue
		}
		u := fmt.Sprintf("%s/%d.xml", base, p.ID)
		fmt.Fprintf(w, `<li><b>%s</b><br><code><a href="%s">%s</a></code></li>`, html.EscapeString(p.Title), u, u)
	}
	fmt.Fprint(w, "</ul>")
}

func (s *Server) handleFeedRSS(w http.ResponseWriter, r *http.Request) {
	u := s.feedUser(w, r)
	if u == nil {
		return
	}
	p, err := s.store.GetPodcast(r.Context(), u.ID, idParam(r, "podcast"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	key := chi.URLParam(r, "key")
	base := s.feedBase(r, u.ID) + "/feed/" + key
	f := store.EpisodeFilter{UserID: u.ID, PodcastID: p.ID, Sort: "newest", Limit: 500}
	if p.Kind == store.KindYouTube {
		f.Downloaded = 1 // only what is on disk: a YouTube feed is its downloads
	}
	eps, err := s.store.ListEpisodes(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	show := feed.Show{Title: p.Title, Link: p.SiteURL, Description: p.Description, Author: p.Author, Language: p.Language, Explicit: p.Explicit,
		FeedURL: fmt.Sprintf("%s/%d.xml", base, p.ID)}
	if p.ArtworkFile != "" {
		show.Image = fmt.Sprintf("%s/art/%d.jpg?v=%d", base, p.ID, artVersion(p.ArtworkFile))
	} else {
		show.Image = p.ArtworkURL
	}
	for _, e := range eps {
		se := feed.ShowEpisode{GUID: e.GUID, Title: e.Title, Description: e.Description, Link: e.Link, Duration: e.Duration, Season: e.Season, Episode: e.EpisodeNo}
		if e.Download == store.DownloadDone && e.FilePath != "" {
			se.MediaURL = fmt.Sprintf("%s/e/%d%s", base, e.ID, path.Ext(e.FilePath))
			se.MediaType, se.MediaBytes = firstNonEmpty(e.FileType, "audio/mpeg"), e.FileBytes
		} else if e.EnclosureURL != "" {
			se.MediaURL, se.MediaType, se.MediaBytes = e.EnclosureURL, firstNonEmpty(e.EnclosureType, "audio/mpeg"), e.EnclosureBytes
		} else {
			continue
		}
		if p.PubDateMode == "downloaded" && e.DownloadedAt != "" {
			se.PubDate = store.ParseTime(e.DownloadedAt)
		} else {
			se.PubDate = store.ParseTime(e.PubDate)
			if se.PubDate.IsZero() {
				se.PubDate = store.ParseTime(firstNonEmpty(e.DownloadedAt, e.CreatedAt))
			}
		}
		if s.localThumb(&e) != "" {
			se.Image = fmt.Sprintf("%s/art/e/%d.jpg", base, e.ID)
		} else if e.ArtworkURL != "" {
			se.Image = e.ArtworkURL
		}
		if e.HasChapters {
			se.ChaptersURL = fmt.Sprintf("%s/e/%d/chapters.json", base, e.ID)
		}
		show.Episodes = append(show.Episodes, se)
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(feed.BuildRSS(show))
}

func artVersion(file string) int64 {
	var n int64
	if i := strings.LastIndex(file, "-"); i > 0 {
		fmt.Sscanf(file[i+1:], "%d", &n)
	}
	return n
}

func (s *Server) handleFeedMedia(w http.ResponseWriter, r *http.Request) {
	u := s.feedUser(w, r)
	if u == nil {
		return
	}
	e, err := s.store.GetEpisode(r.Context(), u.ID, idParam(r, "episode"))
	if err != nil || e.FilePath == "" || e.Download != store.DownloadDone {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.serveMedia(w, r, e, false)
}

func (s *Server) handleFeedArtwork(w http.ResponseWriter, r *http.Request) {
	u := s.feedUser(w, r)
	if u == nil {
		return
	}
	p, err := s.store.GetPodcast(r.Context(), u.ID, idParam(r, "podcast"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.serveArtwork(w, r, p)
}

func (s *Server) handleFeedEpisodeArtwork(w http.ResponseWriter, r *http.Request) {
	u := s.feedUser(w, r)
	if u == nil {
		return
	}
	e, err := s.store.GetEpisode(r.Context(), u.ID, idParam(r, "episode"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.serveEpisodeArtwork(w, r, e)
}

// handleFeedChapters publishes chapters in the podcast 2.0 JSON format.
func (s *Server) handleFeedChapters(w http.ResponseWriter, r *http.Request) {
	u := s.feedUser(w, r)
	if u == nil {
		return
	}
	e, err := s.store.GetEpisode(r.Context(), u.ID, idParam(r, "episode"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ch, _, _ := s.store.EpisodeExtras(r.Context(), e.ID)
	var chapters []store.Chapter
	_ = json.Unmarshal([]byte(ch), &chapters)
	type jc struct {
		Start float64 `json:"startTime"`
		End   float64 `json:"endTime,omitempty"`
		Title string  `json:"title"`
		URL   string  `json:"url,omitempty"`
		Img   string  `json:"img,omitempty"`
	}
	out := struct {
		Version  string `json:"version"`
		Chapters []jc   `json:"chapters"`
	}{Version: "1.2.0", Chapters: []jc{}}
	for _, c := range chapters {
		out.Chapters = append(out.Chapters, jc{Start: c.Start, End: c.End, Title: c.Title, URL: c.URL, Img: c.Image})
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, out)
}
