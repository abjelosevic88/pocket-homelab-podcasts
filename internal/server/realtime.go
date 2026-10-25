package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

// Live updates. Browser tabs listen on GET /api/v1/events (Server-Sent Events:
// plain HTTP that passes Caddy untouched) and hear about changes made anywhere
// else — another tab, the iPhone app, a feed refresh, a download worker — at
// once, without polling. Every event belongs to one person and goes only to
// that person's streams; a subscription (and so every episode, download and
// refresh under it) has exactly one owner, so the same feed followed by two
// people never crosses over.
//
// Events carry ids and a little state, not whole objects: clients refetch what
// they show. Each "data:" line is JSON with a "type":
//
//	hello        {server_time}
//	now_playing  {active, episode, position, duration, playing, rate, device, client, updated_at, server_time}
//	progress     {episode_id, podcast_id, position, completed, client, device, updated_at}
//	history      {episode_id}                           0 = the whole history was cleared
//	episodes     {podcast_id, added, updated, removed}  id lists; episodes added, changed or removed
//	download     {episode_id, podcast_id, state, error} none|queued|downloading|done|failed|culled
//	podcasts     {added, updated, removed}              subscription id lists
//	refresh      {podcast_id, running, new, error}      a feed or channel refresh started / finished
//	queue        {}                                     the up-next queue changed
//	saved        {episode_id, saved}
//	playlists    {}
//	settings     {}
//
// GET /api/v1/now-playing/events is the older stream: the same hub, only
// now_playing events.

type realtimeHub struct {
	mu   sync.Mutex
	subs map[*realtimeSub]struct{}
}

type realtimeSub struct {
	uid  int64
	only string // "" = every event; else just this type
	out  chan []byte
	once sync.Once
	gone chan struct{}
}

func (c *realtimeSub) stop() { c.once.Do(func() { close(c.gone) }) }

var (
	realtimeMaxPerUser = 16
	realtimeMaxTotal   = 2048
	realtimeBuffer     = 64
	realtimeKeepAlive  = 25 * time.Second
)

// liveObject is one event's payload.
type liveObject = map[string]any

func (h *realtimeHub) add(sub *realtimeSub) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs == nil {
		h.subs = map[*realtimeSub]struct{}{}
	}
	mine := 0
	for other := range h.subs {
		if other.uid == sub.uid {
			mine++
		}
	}
	if mine >= realtimeMaxPerUser || len(h.subs) >= realtimeMaxTotal {
		return false
	}
	h.subs[sub] = struct{}{}
	return true
}

func (h *realtimeHub) remove(sub *realtimeSub) {
	h.mu.Lock()
	delete(h.subs, sub)
	h.mu.Unlock()
}

// listening reports whether the person has an open stream (that wants typ).
func (h *realtimeHub) listening(uid int64, typ string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		if sub.uid == uid && (sub.only == "" || sub.only == typ) {
			return true
		}
	}
	return false
}

func (h *realtimeHub) anyone() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs) > 0
}

// send queues one event for the person's streams. A stream whose buffer is
// full is ended rather than waited for: its client reconnects and reloads.
func (h *realtimeHub) send(uid int64, typ string, b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		if sub.uid != uid || (sub.only != "" && sub.only != typ) {
			continue
		}
		select {
		case sub.out <- b:
		default:
			sub.stop()
		}
	}
}

// live publishes an event to one person. Call it after the change is committed.
func (s *Server) live(uid int64, typ string, v liveObject) {
	if uid == 0 || !s.realtime.listening(uid, typ) {
		return
	}
	if v == nil {
		v = liveObject{}
	}
	v["type"] = typ
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.realtime.send(uid, typ, b)
}

// liveEpisode publishes an episode event to the episode's owner (the person
// whose subscription it is under). Pass the owner when it is already known.
func (s *Server) liveEpisode(ctx context.Context, episodeID int64, typ string, v liveObject) {
	if episodeID == 0 || !s.realtime.anyone() {
		return
	}
	uid, podcast, err := s.store.EpisodeOwner(ctx, episodeID)
	if err != nil {
		return
	}
	if v == nil {
		v = liveObject{}
	}
	v["episode_id"], v["podcast_id"] = episodeID, podcast
	s.live(uid, typ, v)
}

// liveDownload says an episode's file state moved.
func (s *Server) liveDownload(ctx context.Context, episodeID int64, state, errMsg string) {
	v := liveObject{"state": state}
	if errMsg != "" {
		v["error"] = errMsg
	}
	s.liveEpisode(ctx, episodeID, "download", v)
}

// liveEpisodes says episodes of one subscription were added, changed or removed.
func (s *Server) liveEpisodes(uid, podcastID int64, added, updated, removed []int64) {
	if len(added)+len(updated)+len(removed) == 0 {
		return
	}
	s.live(uid, "episodes", liveObject{"podcast_id": podcastID, "added": nz(added), "updated": nz(updated), "removed": nz(removed)})
}

// livePodcasts says subscriptions were added, changed or removed.
func (s *Server) livePodcasts(uid int64, added, updated, removed []int64) {
	s.live(uid, "podcasts", liveObject{"added": nz(added), "updated": nz(updated), "removed": nz(removed)})
}

func nz(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

// liveProgressAt says where the person now is in an episode. client is the
// reporting tab's id (empty for other apps), so a tab can skip its own echo.
func (s *Server) liveProgressAt(uid int64, e *store.Episode, position int, completed bool, client string) {
	s.live(uid, "progress", liveObject{"episode_id": e.ID, "podcast_id": e.PodcastID, "position": position, "completed": completed,
		"client": client, "updated_at": time.Now().UnixMilli()})
}

// liveProgress publishes an episode's stored position.
func (s *Server) liveProgress(uid int64, e *store.Episode, client string) {
	s.liveProgressAt(uid, e, e.PlayPosition, e.Completed, client)
}

// liveProgressNow reads the stored position and publishes it.
func (s *Server) liveProgressNow(ctx context.Context, uid, episodeID int64) {
	if !s.realtime.listening(uid, "progress") {
		return
	}
	if e, err := s.store.GetEpisode(ctx, uid, episodeID); err == nil {
		s.liveProgress(uid, e, "")
	}
}

// liveNowPlaying pushes the person's now-playing state.
func (s *Server) liveNowPlaying(uid int64) {
	if !s.realtime.listening(uid, "now_playing") {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.live(uid, "now_playing", s.nowJSON(ctx, uid))
}

// handleEvents is GET /api/v1/events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) { s.serveEvents(w, r, "") }

// serveEvents streams the signed-in person's events until the client leaves,
// falls behind, or its sign-in is revoked.
func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request, only string) {
	u := userFrom(r.Context())
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	rc := http.NewResponseController(w)
	sub := &realtimeSub{uid: u.ID, only: only, out: make(chan []byte, realtimeBuffer), gone: make(chan struct{})}
	if !s.realtime.add(sub) {
		writeErr(w, http.StatusTooManyRequests, "too many live connections")
		return
	}
	defer s.realtime.remove(sub)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	write := func(b string) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(15 * time.Second))
		if _, err := fmt.Fprint(w, b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	first := liveObject{"type": "hello", "server_time": time.Now().UnixMilli()}
	if only == "now_playing" {
		// the old stream opens with the current state
		first = s.nowJSON(r.Context(), u.ID)
		first["type"] = "now_playing"
	}
	b, _ := json.Marshal(first)
	if !write("retry: 3000\ndata: " + string(b) + "\n\n") {
		return
	}
	tick := time.NewTicker(realtimeKeepAlive)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.gone:
			// too slow to keep up: the client reconnects and reloads what it shows
			return
		case msg := <-sub.out:
			if !write("data: " + string(msg) + "\n\n") {
				return
			}
		case <-tick.C:
			// a revoked sign-in ends the stream, not only the next request
			if again, err := s.resolveUser(r); err != nil || again == nil || again.ID != u.ID {
				write("event: unauthorized\ndata: {}\n\n")
				return
			}
			if !write(": ping\n\n") {
				return
			}
		}
	}
}
