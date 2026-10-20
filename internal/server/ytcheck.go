package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/feed"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/ytdlp"
)

// The quick check spots new YouTube uploads within minutes: it reads the
// source's own RSS feed (one small request, no yt-dlp) and runs the full
// yt-dlp refresh only when a video id shows up that the app has not seen.
// For a channel the feed is its "full-length uploads" list (UULF…), which
// leaves out Shorts and members-only videos. When YouTube's RSS is down the
// regular refresh interval still catches everything, just later.

type rssState struct {
	etag, lastModified string
	seen               map[string]bool // ids already looked at (incl. ones the filters skip)
	ok, failed         time.Time
	err                string
}

type ytChecker struct {
	mu    sync.Mutex
	state map[int64]*rssState
}

func (s *Server) quickCheckLoop(ctx context.Context) {
	every := time.Duration(s.cfg.YouTubeCheckMinutes) * time.Minute
	if every <= 0 {
		return
	}
	if every < 2*time.Minute {
		every = 2 * time.Minute
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(45 * time.Second):
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.quickCheckAll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) quickCheckAll(ctx context.Context) {
	pods, err := s.store.ListAllPodcasts(ctx)
	if err != nil {
		return
	}
	for i := range pods {
		p := pods[i]
		if p.Kind != store.KindYouTube || p.FeedURL == PicksFeedURL {
			continue
		}
		s.quickCheck(ctx, &p)
	}
}

func (s *Server) rss(id int64) *rssState {
	s.ytc.mu.Lock()
	defer s.ytc.mu.Unlock()
	if s.ytc.state == nil {
		s.ytc.state = map[int64]*rssState{}
	}
	st := s.ytc.state[id]
	if st == nil {
		st = &rssState{seen: map[string]bool{}}
		s.ytc.state[id] = st
	}
	return st
}

var rssClient = &http.Client{Timeout: 20 * time.Second}

// quickCheck returns how many new videos the refresh it triggered found.
func (s *Server) quickCheck(ctx context.Context, p *store.Podcast) int {
	u := ytdlp.RSSURL(p.YouTubeKind, p.YouTubeID)
	if u == "" {
		return 0
	}
	st := s.rss(p.ID)
	s.ytc.mu.Lock()
	etag, lm := st.etag, st.lastModified
	s.ytc.mu.Unlock()
	res, err := feed.Fetch(ctx, rssClient, u, etag, lm, s.userAgent())
	s.ytc.mu.Lock()
	if err != nil {
		st.failed, st.err = time.Now(), err.Error()
		s.ytc.mu.Unlock()
		s.log.Debug("youtube rss check failed", "podcast", p.Title, "err", err)
		return 0
	}
	st.ok, st.err = time.Now(), ""
	if res.NotModified {
		s.ytc.mu.Unlock()
		return 0
	}
	st.etag, st.lastModified = res.ETag, res.LastModified
	var ids []string
	for _, it := range res.Feed.Items {
		if id := strings.TrimPrefix(it.GUID, "yt:video:"); id != it.GUID && id != "" {
			ids = append(ids, id)
		}
	}
	s.ytc.mu.Unlock()
	known, err := s.store.EpisodeIDsByYouTube(ctx, p.ID)
	if err != nil {
		return 0
	}
	fresh := false
	s.ytc.mu.Lock()
	for _, id := range ids {
		if _, ok := known[id]; !ok && !st.seen[id] {
			fresh = true
		}
	}
	s.ytc.mu.Unlock()
	if !fresh {
		return 0
	}
	n, err := s.refreshPodcast(ctx, p, nil)
	if err != nil {
		return 0
	}
	s.ytc.mu.Lock()
	for _, id := range ids {
		st.seen[id] = true // filtered out ones (too short, title filter) are not looked at again
	}
	s.ytc.mu.Unlock()
	if n > 0 {
		s.log.Info("youtube: new upload spotted through rss", "podcast", p.Title, "new", n)
	}
	return n
}

// quickCheckStatus summarises the RSS checks for the About page.
func (s *Server) quickCheckStatus() map[string]any {
	s.ytc.mu.Lock()
	defer s.ytc.mu.Unlock()
	ok, failing := 0, 0
	var last time.Time
	for _, st := range s.ytc.state {
		if !st.ok.IsZero() && st.ok.After(st.failed) {
			ok++
		} else if !st.failed.IsZero() {
			failing++
		}
		if st.ok.After(last) {
			last = st.ok
		}
	}
	return map[string]any{"every_minutes": s.cfg.YouTubeCheckMinutes, "ok": ok, "failing": failing, "last_ok": store.FormatTime(last)}
}
