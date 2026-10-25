package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/config"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

type liveRig struct {
	t     *testing.T
	s     *Server
	st    *store.Store
	ts    *httptest.Server
	alice *store.User
	bob   *store.User
	aTok  string
	bTok  string
}

func newLiveRig(t *testing.T) *liveRig {
	t.Helper()
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(dir+"/t.db", log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(&config.Config{AuthMode: "local", SessionTTL: time.Hour, DataDir: dir, RefreshMinutes: 60, DownloadWorkers: 1}, st, log, nil)
	if err := s.core.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, _ := s.firstOrSystemUser(ctx, "alice@example.com", "Alice")
	b, _ := s.firstOrSystemUser(ctx, "bob@example.com", "Bob")
	aTok, _, _ := st.CreateAPIToken(ctx, a.ID, "Iphone app")
	bTok, _, _ := st.CreateAPIToken(ctx, b.ID, "browser")
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	return &liveRig{t: t, s: s, st: st, ts: ts, alice: a, bob: b, aTok: aTok, bTok: bTok}
}

func (g *liveRig) call(tok, method, path, body string, want int) map[string]any {
	g.t.Helper()
	req, _ := http.NewRequest(method, g.ts.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		g.t.Fatalf("%s %s: %d want %d: %s", method, path, res.StatusCode, want, data)
	}
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

// liveStream is an open GET /api/v1/events.
type liveStream struct {
	events chan map[string]any // data lines, decoded; "event: x" lines arrive as {"event": x}
	done   chan struct{}
	cancel context.CancelFunc
}

func (g *liveRig) listen(tok, path string) *liveStream {
	g.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", g.ts.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		g.t.Fatal(err)
	}
	if res.StatusCode != 200 {
		cancel()
		g.t.Fatalf("stream: %d", res.StatusCode)
	}
	ls := &liveStream{events: make(chan map[string]any, 100), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(ls.done)
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "data: "):
				var v map[string]any
				if json.Unmarshal([]byte(line[6:]), &v) == nil {
					ls.events <- v
				}
			case strings.HasPrefix(line, "event: "):
				ls.events <- map[string]any{"event": line[7:]}
			}
		}
	}()
	g.t.Cleanup(cancel)
	return ls
}

// next waits for an event of the type, skipping others.
func (ls *liveStream) next(t *testing.T, typ string) map[string]any {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case v := <-ls.events:
			if v["type"] == typ || v["event"] == typ {
				return v
			}
		case <-timeout:
			t.Fatalf("no %q event", typ)
		}
	}
}

// both waits for two event types in any order.
func (ls *liveStream) both(t *testing.T, x, y string) (map[string]any, map[string]any) {
	t.Helper()
	got := map[string]map[string]any{}
	timeout := time.After(3 * time.Second)
	for got[x] == nil || got[y] == nil {
		select {
		case v := <-ls.events:
			if typ, _ := v["type"].(string); (typ == x || typ == y) && got[typ] == nil {
				got[typ] = v
			}
		case <-timeout:
			t.Fatalf("no %q and %q events: %v", x, y, got)
		}
	}
	return got[x], got[y]
}

// drain discards what arrives for a while.
func (ls *liveStream) drain(d time.Duration) {
	timeout := time.After(d)
	for {
		select {
		case <-ls.events:
		case <-timeout:
			return
		}
	}
}

// none checks that nothing but the greeting arrives for a while.
func (ls *liveStream) none(t *testing.T, d time.Duration) {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case v := <-ls.events:
			if v["type"] != "hello" {
				t.Fatalf("unexpected event %v", v)
			}
		case <-timeout:
			return
		}
	}
}

// A progress report from the iPhone app reaches the person's browser tabs as a
// progress event (and now_playing), never anyone else's.
func TestLiveProgressPerUser(t *testing.T) {
	g := newLiveRig(t)
	ctx := context.Background()
	p := &store.Podcast{UserID: g.alice.ID, Kind: store.KindRSS, Title: "Show", FeedURL: "https://example.com/feed"}
	if err := g.st.CreatePodcast(ctx, p); err != nil {
		t.Fatal(err)
	}
	e := store.Episode{PodcastID: p.ID, GUID: "e", Title: "Episode", Duration: 1000}
	if _, err := g.st.UpsertEpisode(ctx, &e); err != nil {
		t.Fatal(err)
	}
	a := g.listen(g.aTok, "/api/v1/events")
	a.next(t, "hello")
	old := g.listen(g.aTok, "/api/v1/now-playing/events")
	if v := old.next(t, "now_playing"); v["active"] != false {
		t.Fatalf("old stream greeting: %v", v)
	}
	b := g.listen(g.bTok, "/api/v1/events")
	b.next(t, "hello")

	path := fmt.Sprintf("/api/v1/episodes/%d/progress", e.ID)
	// the iPhone app's dated, retry-safe report
	body := fmt.Sprintf(`{"report_id":"r1","listened_at":%q,"position":120,"listened":5,"completed":false}`, time.Now().UTC().Format(time.RFC3339Nano))
	g.call(g.aTok, "POST", path, body, 200)
	v, np := a.both(t, "progress", "now_playing")
	if v["episode_id"] != float64(e.ID) || v["podcast_id"] != float64(p.ID) || v["position"] != float64(120) || v["completed"] != false {
		t.Fatalf("progress: %v", v)
	}
	if np["active"] != true || np["device"] != "iPhone" {
		t.Fatalf("now playing: %v", np)
	}
	if np := old.next(t, "now_playing"); np["active"] != true {
		t.Fatalf("old stream: %v", np)
	}
	// a retry of the same report changes nothing and says nothing
	g.call(g.aTok, "POST", path, body, 200)
	// a browser tab's plain report carries its id so it can skip its own echo
	g.call(g.aTok, "POST", path, `{"position":150,"listened":10,"client":"tab1"}`, 200)
	v = a.next(t, "progress")
	if v["position"] != float64(150) || v["client"] != "tab1" {
		t.Fatalf("tab progress: %v", v)
	}
	g.call(g.aTok, "POST", fmt.Sprintf("/api/v1/episodes/%d/played", e.ID), "", 200)
	if v := a.next(t, "progress"); v["completed"] != true {
		t.Fatalf("played: %v", v)
	}
	a.next(t, "queue")
	g.call(g.aTok, "POST", fmt.Sprintf("/api/v1/episodes/%d/save", e.ID), "", 200)
	if v := a.next(t, "saved"); v["saved"] != true || v["episode_id"] != float64(e.ID) {
		t.Fatalf("saved: %v", v)
	}
	g.call(g.aTok, "POST", fmt.Sprintf("/api/v1/episodes/%d/queue", e.ID), "{}", 200)
	a.next(t, "queue")
	// Bob tries Alice's episode: refused, and nobody hears about it
	g.call(g.bTok, "POST", path, `{"position":999,"listened":10}`, 404)
	b.none(t, 300*time.Millisecond)
}

// Two people follow the same feed: each has their own subscription, and a
// refresh of one person's is told only to that person.
func TestLiveSharedFeedOnlyToSubscriber(t *testing.T) {
	g := newLiveRig(t)
	var mu sync.Mutex
	items := []string{"one"}
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Shared</title>`)
		for _, it := range items {
			fmt.Fprintf(w, `<item><title>%s</title><guid>%s</guid><pubDate>Mon, 02 Jan 2026 15:04:05 GMT</pubDate><enclosure url="https://example.com/%s.mp3" type="audio/mpeg" length="1"/></item>`, it, it, it)
		}
		fmt.Fprint(w, `</channel></rss>`)
	}))
	defer feedSrv.Close()

	a := g.listen(g.aTok, "/api/v1/events")
	a.next(t, "hello")
	b := g.listen(g.bTok, "/api/v1/events")
	b.next(t, "hello")
	other := g.listen(g.bTok, "/api/v1/now-playing/events")
	other.next(t, "now_playing")

	ap := g.call(g.aTok, "POST", "/api/v1/podcasts", fmt.Sprintf(`{"url":%q}`, feedSrv.URL), 201)
	aid := ap["id"].(float64)
	if v := a.next(t, "podcasts"); v["added"].([]any)[0] != aid {
		t.Fatalf("subscribe: %v", v)
	}
	if v := a.next(t, "episodes"); v["podcast_id"] != aid || len(v["added"].([]any)) != 1 {
		t.Fatalf("first refresh: %v", v)
	}
	a.drain(200 * time.Millisecond)
	b.none(t, 300*time.Millisecond)

	bp := g.call(g.bTok, "POST", "/api/v1/podcasts", fmt.Sprintf(`{"url":%q}`, feedSrv.URL), 201)
	if bp["id"].(float64) == aid {
		t.Fatal("subscriptions should be per person")
	}
	b.next(t, "podcasts")
	b.drain(200 * time.Millisecond)
	a.none(t, 300*time.Millisecond)

	mu.Lock()
	items = append(items, "two")
	mu.Unlock()
	g.call(g.aTok, "POST", fmt.Sprintf("/api/v1/podcasts/%d/refresh", int64(aid)), "", 200)
	if v := a.next(t, "refresh"); v["running"] != true {
		t.Fatalf("refresh start: %v", v)
	}
	if v := a.next(t, "refresh"); v["running"] != false || v["new"] != float64(1) {
		t.Fatalf("refresh end: %v", v)
	}
	if v := a.next(t, "episodes"); len(v["added"].([]any)) != 1 {
		t.Fatalf("new episode: %v", v)
	}
	b.none(t, 300*time.Millisecond)
	other.none(t, 50*time.Millisecond)

	// a download state change goes to the owner only
	eps := g.call(g.aTok, "GET", fmt.Sprintf("/api/v1/podcasts/%d/episodes", int64(aid)), "", 200)["episodes"].([]any)
	eid := int64(eps[0].(map[string]any)["id"].(float64))
	g.call(g.aTok, "POST", fmt.Sprintf("/api/v1/episodes/%d/download", eid), "", 202)
	if v := a.next(t, "download"); v["episode_id"] != float64(eid) || v["state"] != store.DownloadQueued {
		t.Fatalf("download: %v", v)
	}
	g.call(g.aTok, "DELETE", fmt.Sprintf("/api/v1/episodes/%d", eid), "", 204)
	if v := a.next(t, "episodes"); v["removed"].([]any)[0] != float64(eid) {
		t.Fatalf("removed: %v", v)
	}
	g.call(g.aTok, "DELETE", fmt.Sprintf("/api/v1/podcasts/%d", int64(aid)), "", 204)
	if v := a.next(t, "podcasts"); v["removed"].([]any)[0] != aid {
		t.Fatalf("unsubscribe: %v", v)
	}
	b.none(t, 300*time.Millisecond)
}

// A listener that cannot keep up is cut off instead of holding up everyone.
func TestLiveSlowConsumerDropped(t *testing.T) {
	var h realtimeHub
	slow := &realtimeSub{uid: 1, out: make(chan []byte, 2), gone: make(chan struct{})}
	fast := &realtimeSub{uid: 1, out: make(chan []byte, 100), gone: make(chan struct{})}
	other := &realtimeSub{uid: 2, out: make(chan []byte, 1), gone: make(chan struct{})}
	for _, sub := range []*realtimeSub{slow, fast, other} {
		if !h.add(sub) {
			t.Fatal("refused")
		}
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			h.send(1, "queue", []byte(`{}`))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("send blocked on a slow listener")
	}
	select {
	case <-slow.gone:
	default:
		t.Fatal("slow listener kept")
	}
	select {
	case <-fast.gone:
		t.Fatal("fast listener dropped")
	default:
	}
	if len(fast.out) != 10 || len(other.out) != 0 {
		t.Fatalf("delivered %d to fast, %d to someone else", len(fast.out), len(other.out))
	}
	// the cap per person
	n := 0
	for h.add(&realtimeSub{uid: 3, out: make(chan []byte, 1), gone: make(chan struct{})}) {
		n++
	}
	if n != realtimeMaxPerUser {
		t.Fatalf("cap: %d", n)
	}
}

// The stream of a slow client really ends (end to end).
func TestLiveSlowStreamEnds(t *testing.T) {
	g := newLiveRig(t)
	defer func(n int) { realtimeBuffer = n }(realtimeBuffer)
	realtimeBuffer = 1
	ls := g.listen(g.aTok, "/api/v1/events")
	ls.next(t, "hello")
	// flood far faster than the writer drains a one-event buffer
	for i := 0; i < 1000; i++ {
		g.s.realtime.send(g.alice.ID, "queue", []byte(`{"type":"queue"}`))
	}
	select {
	case <-ls.done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream of a slow client still open")
	}
}

// Revoking the sign-in ends an open stream with an "unauthorized" event.
func TestLiveRevokedEndsStream(t *testing.T) {
	g := newLiveRig(t)
	defer func(d time.Duration) { realtimeKeepAlive = d }(realtimeKeepAlive)
	realtimeKeepAlive = 100 * time.Millisecond
	ls := g.listen(g.aTok, "/api/v1/events")
	ls.next(t, "hello")
	toks, err := g.st.ListAPITokens(context.Background(), g.alice.ID)
	if err != nil || len(toks) != 1 {
		t.Fatal(err, len(toks))
	}
	if err := g.st.DeleteAPIToken(context.Background(), g.alice.ID, toks[0].ID); err != nil {
		t.Fatal(err)
	}
	ls.next(t, "unauthorized")
	select {
	case <-ls.done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream still open after revocation")
	}
}
