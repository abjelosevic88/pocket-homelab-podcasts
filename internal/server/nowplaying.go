package server

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

// Now playing: every client reports its position while it plays (the web
// player every ~10 s, the iPhone app every 5 s and at once on pause and
// seek). The latest report per person is kept in memory and pushed to that
// person's open browser tabs, which show "Playing on iPhone" in the player
// bar and keep the clock ticking between reports.

type nowSession struct {
	EpisodeID int64
	Position  float64
	Duration  int
	Playing   bool
	Rate      float64
	Device    string
	Client    string
	At        time.Time
}

type nowHub struct {
	mu       sync.Mutex
	sessions map[int64]*nowSession
}

const (
	nowStall = 16 * time.Second // no report for this long while "playing" → treat as paused
	nowKeep  = 30 * time.Minute // a paused session is shown this long
)

// trackPlayback records one progress report and pushes the new state to the
// person's open tabs.
func (s *Server) trackPlayback(r *http.Request, uid int64, e *store.Episode, position, listened int, completed bool, client string) {
	device := s.deviceLabel(r)
	if client == "" {
		client = device
	}
	if s.recordPlayback(uid, e, position, listened, completed, client, device) {
		s.liveNowPlaying(uid)
	}
}

// recordPlayback updates the in-memory session; false when nothing changed.
func (s *Server) recordPlayback(uid int64, e *store.Episode, position, listened int, completed bool, client, device string) bool {
	h := &s.now
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sessions == nil {
		h.sessions = map[int64]*nowSession{}
	}
	prev := h.sessions[uid]
	if completed {
		if prev != nil && prev.EpisodeID == e.ID {
			delete(h.sessions, uid)
			return true
		}
		return false
	}
	cur := &nowSession{EpisodeID: e.ID, Position: float64(position), Duration: e.Duration, Device: device, Client: client, At: now, Rate: 1}
	if prev != nil && prev.EpisodeID == e.ID && prev.Client == client {
		dt := now.Sub(prev.At).Seconds()
		dp := cur.Position - prev.Position
		cur.Playing = dt > 0 && dt < 40 && dp > 0.5
		if cur.Playing {
			cur.Rate = clampFloat(dp/dt, 0.5, 3.5)
			if prev.Playing && prev.Rate > 0 {
				cur.Rate = (cur.Rate + prev.Rate) / 2 // smooth out report jitter
			}
		}
	} else {
		cur.Playing = listened > 0
	}
	h.sessions[uid] = cur
	// no further report means it stopped (or, paused, went stale): say so then
	after := nowKeep + time.Second
	if cur.Playing {
		after = nowStall + time.Second
	}
	at := cur.At
	time.AfterFunc(after, func() {
		h.mu.Lock()
		same := h.sessions[uid] != nil && h.sessions[uid].At.Equal(at)
		h.mu.Unlock()
		if same {
			s.liveNowPlaying(uid)
		}
	})
	return true
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

var (
	reBrowser = regexp.MustCompile(`(Edg|OPR|Firefox|CriOS|FxiOS|Chrome|Safari)/`)
	reApp     = regexp.MustCompile(`(?i)^Pocket(%20| )Homelab`)
)

// deviceLabel names the client behind a request: the paired device's name
// (Pocket Core), the API token's name ("Iphone app" → iPhone), else the
// browser and platform from the User-Agent.
func (s *Server) deviceLabel(r *http.Request) string {
	tok := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(h), "bearer ") {
		tok = strings.TrimSpace(h[7:])
	} else {
		tok = r.Header.Get("X-API-Key")
	}
	if tok != "" {
		if s.core.Configured() && core.IsCoreToken(tok) {
			if in, err := s.core.Introspect(r.Context(), tok); err == nil && in.Device != nil && in.Device.Name != "" {
				return in.Device.Name
			}
		}
		if name := s.store.APITokenName(r.Context(), tok); name != "" {
			return prettyDevice(name)
		}
	}
	return uaLabel(r.Header.Get("User-Agent"))
}

func prettyDevice(name string) string {
	l := strings.ToLower(name)
	switch {
	case strings.Contains(l, "iphone"):
		return "iPhone"
	case strings.Contains(l, "ipad"):
		return "iPad"
	case strings.Contains(l, "android"):
		return "Android"
	case strings.Contains(l, "mac"):
		return "Mac"
	}
	return name
}

func uaLabel(ua string) string {
	platform := ""
	switch {
	case strings.Contains(ua, "iPhone"):
		platform = "iPhone"
	case strings.Contains(ua, "iPad"):
		platform = "iPad"
	case strings.Contains(ua, "Android"):
		platform = "Android"
	case strings.Contains(ua, "Macintosh") || strings.Contains(ua, "Darwin"):
		platform = "Mac"
	case strings.Contains(ua, "Windows"):
		platform = "Windows"
	case strings.Contains(ua, "Linux"):
		platform = "Linux"
	}
	if reApp.MatchString(ua) {
		if platform == "" || platform == "Mac" && !strings.Contains(ua, "Macintosh") {
			return "Pocket Homelab app"
		}
		return platform
	}
	browser := ""
	if m := reBrowser.FindStringSubmatch(ua); m != nil {
		browser = map[string]string{"Edg": "Edge", "OPR": "Opera", "CriOS": "Chrome", "FxiOS": "Firefox"}[m[1]]
		if browser == "" {
			browser = m[1]
		}
	}
	switch {
	case browser != "" && platform != "":
		return browser + " on " + platform
	case platform != "":
		return platform
	case browser != "":
		return browser
	}
	return "another device"
}

// nowJSON is what the browser gets.
func (s *Server) nowJSON(ctx context.Context, uid int64) map[string]any {
	h := &s.now
	h.mu.Lock()
	cur := h.sessions[uid]
	var c nowSession
	if cur != nil {
		c = *cur
	}
	h.mu.Unlock()
	if cur == nil || time.Since(c.At) > nowKeep {
		return map[string]any{"active": false, "server_time": time.Now().UnixMilli()}
	}
	playing := c.Playing && time.Since(c.At) < nowStall
	e, err := s.store.GetEpisode(ctx, uid, c.EpisodeID)
	if err != nil {
		return map[string]any{"active": false, "server_time": time.Now().UnixMilli()}
	}
	s.decorateEpisode(e)
	dur := c.Duration
	if dur == 0 {
		dur = e.Duration
	}
	return map[string]any{"active": true, "episode": e, "position": c.Position, "duration": dur, "playing": playing, "rate": c.Rate,
		"device": c.Device, "client": c.Client, "updated_at": c.At.UnixMilli(), "server_time": time.Now().UnixMilli()}
}

func (s *Server) handleNowPlaying(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.nowJSON(r.Context(), me(r)))
}

// handleNowPlayingEvents streams the person's playback as server-sent events
// (the older stream; GET /api/v1/events carries the same now_playing events).
func (s *Server) handleNowPlayingEvents(w http.ResponseWriter, r *http.Request) {
	s.serveEvents(w, r, "now_playing")
}
