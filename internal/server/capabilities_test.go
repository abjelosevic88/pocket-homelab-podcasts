package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/config"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

// capEnv is Podcasts with two people, Alice and Bob, each with an API token
// of their own, next to a fake Pocket Core that vouches for link tokens
// pcx_<alice|bob>_<search|day|widget|capture|read|history|queue> from Pocket
// Hub and remembers the manifest Podcasts registers with. yt-dlp is a small
// script: any video is "Video <id>" (five minutes long), any channel has no
// videos, and the video GONEGONEGON does not exist.
type capEnv struct {
	t          *testing.T
	s          *Server
	ts         *httptest.Server
	alice, bob string // API tokens
	a, b       *store.User
	mu         sync.Mutex
	manifest   map[string]any
}

func newCapEnv(t *testing.T) *capEnv {
	t.Helper()
	e := &capEnv{t: t}
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
		"read": "podcasts:read", "history": "history:read", "queue": "queue:write"}
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			e.mu.Lock()
			e.manifest = m
			e.mu.Unlock()
			_, _ = w.Write([]byte(`{"app":{}}`))
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || scopes[parts[2]] == "" {
				_, _ = w.Write([]byte(`{"active":false}`))
				return
			}
			email := parts[1] + "@example.com"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"active": true, "kind": "delegated", "scope": scopes[parts[2]], "audience": "podcasts",
				"user": map[string]any{"id": 9, "sub": email, "email": email, "name": parts[1], "role": "member"},
				"app":  map[string]any{"id": 4, "slug": "hub", "name": "Pocket Hub"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(coreSrv.Close)

	dir := t.TempDir()
	yt := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
for a; do last=$a; done
if [ "$1" = "--version" ]; then echo 2026.01.01; exit 0; fi
case "$*" in *--flat-playlist*)
  id=$(echo "$last" | sed -n 's#.*/channel/\(UC[A-Za-z0-9_-]*\).*#\1#p')
  printf '{"id":"%s","channel_id":"%s","title":"Channel %s - Videos","channel":"Channel %s","entries":[]}' "$id" "$id" "$id" "$id"
  exit 0;;
esac
id=$(echo "$last" | sed -n 's#.*v=\([A-Za-z0-9_-]*\).*#\1#p')
if [ "$id" = "GONEGONEGON" ]; then echo "ERROR: [youtube] $id: Video unavailable" >&2; exit 1; fi
printf '{"id":"%s","title":"Video %s","description":"About %s","duration":300,"channel":"Some Channel","channel_id":"UCcccccccccccccccccccccc","upload_date":"20261101"}' "$id" "$id" "$id"
`
	if err := os.WriteFile(yt, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AuthMode: "local", SessionTTL: time.Hour, DataDir: dir, DBPath: dir + "/t.db", RefreshMinutes: 60, DownloadWorkers: 1,
		CoreURL: coreSrv.URL, YtDlpPath: yt, YouTubeIndexLimit: 5}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(cfg.DBPath, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e.s = New(cfg, st, log, nil)
	e.s.core = core.New(coreSrv.URL, core.Options{DB: st.DB, Token: "pca_podcasts"})
	if err := e.s.core.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	e.a, _ = e.s.firstOrSystemUser(ctx, "alice@example.com", "Alice")
	e.b, _ = e.s.firstOrSystemUser(ctx, "bob@example.com", "Bob")
	e.alice, _, _ = st.CreateAPIToken(ctx, e.a.ID, "test")
	e.bob, _, _ = st.CreateAPIToken(ctx, e.b.ID, "test")
	e.ts = httptest.NewServer(e.s.Router())
	t.Cleanup(e.ts.Close)
	return e
}

// call makes a request as tok and returns the status and the decoded answer.
func (e *capEnv) call(tok, method, path string, body any) (int, map[string]any) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return res.StatusCode, out
}

// ok is call for an answer that must be 200 or 201.
func (e *capEnv) ok(tok, method, path string, body any) map[string]any {
	e.t.Helper()
	code, out := e.call(tok, method, path, body)
	if code != 200 && code != 201 {
		e.t.Fatalf("%s %s → %d %v", method, path, code, out)
	}
	return out
}

// show adds a show for a person; episode adds an episode to it.
func (e *capEnv) show(u *store.User, title, author string) *store.Podcast {
	e.t.Helper()
	p := &store.Podcast{UserID: u.ID, Kind: store.KindRSS, Title: title, Author: author, FeedURL: "https://example.com/" + strings.ReplaceAll(strings.ToLower(title), " ", "-")}
	if err := e.s.store.CreatePodcast(context.Background(), p); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *capEnv) episode(p *store.Podcast, title, notes string, seconds int, published time.Time) int64 {
	e.t.Helper()
	ep := store.Episode{PodcastID: p.ID, GUID: title, Title: title, Description: notes, Duration: seconds, PubDate: store.FormatTime(published)}
	if _, err := e.s.store.UpsertEpisode(context.Background(), &ep); err != nil {
		e.t.Fatal(err)
	}
	return ep.ID
}

// listen reports listening the way a player does after the fact: seconds
// listened at a moment, where the player was, and whether the episode ended.
func (e *capEnv) listen(tok string, episode int64, at time.Time, position, seconds int, completed bool) {
	e.t.Helper()
	e.ok(tok, "POST", "/api/v1/episodes/"+strconv.FormatInt(episode, 10)+"/progress", map[string]any{
		"report_id": fmt.Sprintf("r-%d-%d", episode, at.UnixNano()), "listened_at": at.UTC().Format(time.RFC3339Nano),
		"position": position, "listened": seconds, "completed": completed})
}

// capItems are the rows of a capability answer.
func capItems(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	l, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("no items list in %v", m)
	}
	out := []map[string]any{}
	for _, v := range l {
		out = append(out, v.(map[string]any))
	}
	return out
}

// titles are the titles of a capability answer, in order.
func titles(t *testing.T, m map[string]any) string {
	t.Helper()
	var out []string
	for _, it := range capItems(t, m) {
		out = append(out, it["title"].(string))
	}
	return strings.Join(out, " | ")
}

func id64(n int64) string { return strconv.FormatInt(n, 10) }

// kitchen gives Alice two shows and Bob one, all about the same thing.
func (e *capEnv) kitchen() (radio *store.Podcast, basics, rye, knives, twelve int64) {
	now := time.Now()
	radio = e.show(e.a, "Kitchen Radio", "Ana Baker")
	basics = e.episode(radio, "Sourdough basics", "Flour, water and salt.", 2520, now.Add(-2*24*time.Hour))
	rye = e.episode(radio, "Rye bread", "<p>All about <b>sourdough</b> starters.</p>", 3720, now.Add(-10*24*time.Hour))
	knives = e.episode(radio, "Knife skills", "Chopping.", 45, now.Add(-20*24*time.Hour))
	weekly := e.show(e.a, "Sourdough Weekly", "Sourdough Weekly")
	twelve = e.episode(weekly, "Episode 12", "", 0, now.Add(-24*time.Hour))
	bobs := e.show(e.b, "Bob's Sourdough Hour", "Bob")
	e.episode(bobs, "Sourdough for Bob", "", 600, now)
	return
}

func TestPocketSearch(t *testing.T) {
	e := newCapEnv(t)
	radio, basics, rye, _, twelve := e.kitchen()
	res := e.ok(e.alice, "GET", "/api/v1/pocket/search?q=sourdough", nil)
	// the show of that name, the episode with it in its title, the one with it in its notes, the show's other episodes
	if got := titles(t, res); got != "Sourdough Weekly | Sourdough basics | Rye bread | Episode 12" {
		t.Fatalf("order: %s", got)
	}
	all := capItems(t, res)
	if it := all[0]; it["kind"] != "show" || !strings.HasPrefix(it["id"].(string), "show:") || it["url"] != "/podcasts/"+strings.TrimPrefix(it["id"].(string), "show:") ||
		it["snippet"] != "1 episode · 1 unplayed" || it["date"] == nil {
		t.Fatalf("show: %v", it)
	}
	if it := all[1]; it["kind"] != "episode" || it["id"] != "episode:"+id64(basics) || it["url"] != "/episodes/"+id64(basics) || it["snippet"] != "Kitchen Radio · 42 min" {
		t.Fatalf("episode: %v", it)
	} else if out, err := time.Parse(time.RFC3339, it["date"].(string)); err != nil || time.Since(out) < 47*time.Hour || time.Since(out) > 49*time.Hour {
		t.Fatalf("the day it came out: %v", it["date"])
	}
	if it := all[2]; it["id"] != "episode:"+id64(rye) || it["snippet"] != "Kitchen Radio · 1h 2m" {
		t.Fatalf("by notes: %v", it)
	}
	if it := all[3]; it["id"] != "episode:"+id64(twelve) || it["snippet"] != "Sourdough Weekly" {
		t.Fatalf("by show: %v", it)
	}
	// where the person is in an episode
	e.ok(e.alice, "POST", "/api/v1/episodes/"+id64(basics)+"/progress", map[string]any{"position": 1800, "listened": 60})
	e.ok(e.alice, "POST", "/api/v1/episodes/"+id64(rye)+"/played", nil)
	res = e.ok(e.alice, "GET", "/api/v1/pocket/search?q=BREAD", nil)
	if all := capItems(t, res); len(all) != 1 || all[0]["snippet"] != "Kitchen Radio · 1h 2m · played" {
		t.Fatalf("played: %v", all)
	}
	if all := capItems(t, e.ok(e.alice, "GET", "/api/v1/pocket/search?q=basics", nil)); len(all) != 1 || all[0]["snippet"] != "Kitchen Radio · 42 min · 12 min left" {
		t.Fatalf("in progress: %v", all)
	}
	// a show by who makes it
	if all := capItems(t, e.ok(e.alice, "GET", "/api/v1/pocket/search?q=ana+baker", nil)); len(all) != 1 || all[0]["id"] != "show:"+id64(radio.ID) ||
		all[0]["snippet"] != "Ana Baker · 3 episodes · 2 unplayed" {
		t.Fatalf("by author: %v", all)
	}
	if got := titles(t, e.ok(e.alice, "GET", "/api/v1/pocket/search?q=sourdough&limit=2", nil)); got != "Sourdough Weekly | Sourdough basics" {
		t.Fatalf("limit 2: %s", got)
	}
	// nothing to look for finds nothing, though the app's own list would give everything
	for _, q := range []string{"", "%3F%3F", "*", "%25", "_"} {
		if all := capItems(t, e.ok(e.alice, "GET", "/api/v1/pocket/search?q="+q, nil)); len(all) != 0 {
			t.Fatalf("q=%q: %v", q, all)
		}
	}
	// Bob finds his own and nothing of Alice's
	if got := titles(t, e.ok(e.bob, "GET", "/api/v1/pocket/search?q=sourdough", nil)); got != "Bob's Sourdough Hour | Sourdough for Bob" {
		t.Fatalf("bob: %s", got)
	}
	for _, q := range []string{"kitchen", "rye", "ana", "episode+12"} {
		if all := capItems(t, e.ok(e.bob, "GET", "/api/v1/pocket/search?q="+q, nil)); len(all) != 0 {
			t.Fatalf("bob finds alice's %q: %v", q, all)
		}
	}
	if code, _ := e.call("", "GET", "/api/v1/pocket/search?q=sourdough", nil); code != 401 {
		t.Fatalf("signed out: %d", code)
	}
}

func TestPocketSearchTitlesBeforeNewerNotes(t *testing.T) {
	e := newCapEnv(t)
	p := e.show(e.a, "Long Runner", "")
	now := time.Now()
	old := e.episode(p, "The lighthouse keeper", "", 600, now.Add(-400*24*time.Hour))
	for i := 0; i < 12; i++ {
		e.episode(p, fmt.Sprintf("Mailbag %d", i), "We mention the lighthouse again.", 600, now.Add(-time.Duration(i)*24*time.Hour))
	}
	all := capItems(t, e.ok(e.alice, "GET", "/api/v1/pocket/search?q=lighthouse&limit=5", nil))
	if len(all) != 5 || all[0]["id"] != "episode:"+id64(old) || all[1]["title"] != "Mailbag 0" {
		t.Fatalf("the title match leads, newest notes follow: %v", all)
	}
}

func TestPocketDay(t *testing.T) {
	e := newCapEnv(t)
	_, basics, rye, knives, _ := e.kitchen()
	// noon three and two days ago, in the box's own zone, which is how the history keeps its days
	y, m, d := time.Now().Date()
	first := time.Date(y, m, d-3, 12, 0, 0, 0, time.Local)
	second := first.AddDate(0, 0, 1)
	day1, day2 := first.Format("2006-01-02"), second.Format("2006-01-02")
	e.listen(e.alice, basics, first, 600, 600, false)
	e.listen(e.alice, basics, first.Add(20*time.Minute), 1200, 300, false)
	e.listen(e.alice, knives, first.Add(time.Hour), 45, 45, true)
	e.listen(e.alice, rye, first.Add(2*time.Hour), 90, 90, false)
	e.listen(e.alice, rye, second, 3720, 1800, true)

	res := e.ok(e.alice, "GET", "/api/v1/pocket/day/"+day1, nil)
	if res["date"] != day1 || res["summary"] != "Listened to 3 episodes for 17 min and finished 1" {
		t.Fatalf("day 1: %v", res)
	}
	all := capItems(t, res)
	if len(all) != 3 {
		t.Fatalf("one line an episode: %v", all)
	}
	if it := all[0]; it["id"] != id64(basics) || it["title"] != "Sourdough basics" || it["sub"] != "Kitchen Radio" || it["minutes"] != 15.0 || it["kind"] != "listened" ||
		it["done"] != nil || it["time"] != "12:00" || it["end"] != "12:20" || it["url"] != "/episodes/"+id64(basics) {
		t.Fatalf("listened: %v", it)
	}
	if it := all[1]; it["id"] != id64(knives) || it["minutes"] != 0.75 || it["done"] != true || it["kind"] != "finished" {
		t.Fatalf("finished: %v", it)
	}
	// finished the day after: not done on the day it was started
	if it := all[2]; it["id"] != id64(rye) || it["minutes"] != 1.5 || it["done"] != nil {
		t.Fatalf("started only: %v", it)
	}
	if ms, _ := res["metrics"].([]any); len(ms) != 2 || ms[0].(map[string]any)["value"] != "17 min" || ms[1].(map[string]any)["value"] != "1" {
		t.Fatalf("metrics: %v", res["metrics"])
	}
	res = e.ok(e.alice, "GET", "/api/v1/pocket/day/"+day2, nil)
	if all := capItems(t, res); res["summary"] != "Listened to Rye bread for 30 min and finished it" || len(all) != 1 || all[0]["done"] != true || all[0]["minutes"] != 30.0 {
		t.Fatalf("day 2: %v", res)
	}
	// the caller's zone says how the clock reads
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	if all := capItems(t, e.ok(e.alice, "GET", "/api/v1/pocket/day/"+day2+"?tz=Asia/Tokyo", nil)); all[0]["time"] != second.In(tokyo).Format("15:04") {
		t.Fatalf("tokyo: %v", all)
	}
	// a day with nothing, and somebody else's day
	for _, c := range []struct{ tok, day string }{{e.alice, first.AddDate(0, 0, -1).Format("2006-01-02")}, {e.bob, day1}, {e.bob, day2}} {
		res := e.ok(c.tok, "GET", "/api/v1/pocket/day/"+c.day, nil)
		if len(capItems(t, res)) != 0 || res["summary"] != nil || res["metrics"] != nil {
			t.Fatalf("nothing on %s: %v", c.day, res)
		}
	}
	if code, _ := e.call(e.alice, "GET", "/api/v1/pocket/day/yesterday", nil); code != 400 {
		t.Fatalf("a bad date: %d", code)
	}
	if code, _ := e.call("", "GET", "/api/v1/pocket/day/"+day1, nil); code != 401 {
		t.Fatalf("signed out: %d", code)
	}
	// what Pocket Day reads on its own is as before
	if rows, _ := e.ok(e.alice, "GET", "/api/v1/listens?date="+day1, nil)["listens"].([]any); len(rows) != 3 || rows[0].(map[string]any)["seconds"] != 900.0 {
		t.Fatalf("listens: %v", rows)
	}
}

func TestPocketDayShortListen(t *testing.T) {
	e := newCapEnv(t)
	_, basics, _, _, _ := e.kitchen()
	at := time.Now().Add(-time.Minute)
	e.listen(e.alice, basics, at, 20, 20, false)
	res := e.ok(e.alice, "GET", "/api/v1/pocket/day/"+at.In(time.Local).Format("2006-01-02"), nil)
	if res["summary"] != "Listened to Sourdough basics for less than a minute" {
		t.Fatalf("summary: %v", res)
	}
}

func TestPocketWidget(t *testing.T) {
	e := newCapEnv(t)
	// nothing followed: nothing to show
	res := e.ok(e.alice, "GET", "/api/v1/pocket/widget", nil)
	if len(capItems(t, res)) != 0 || res["line"] != nil || res["url"] != nil {
		t.Fatalf("empty: %v", res)
	}
	_, basics, rye, knives, twelve := e.kitchen()
	res = e.ok(e.alice, "GET", "/api/v1/pocket/widget", nil)
	if got := titles(t, res); got != "Episode 12 | Sourdough basics | Rye bread | Knife skills" || res["line"] != "2 new episodes this week" || res["url"] != "/home" {
		t.Fatalf("newest unplayed: %s, %v", got, res)
	}
	all := capItems(t, res)
	if it := all[1]; it["id"] != id64(basics) || it["detail"] != "Kitchen Radio · 42 min" || it["url"] != "/episodes/"+id64(basics) {
		t.Fatalf("new: %v", it)
	}
	if it := all[0]; it["id"] != id64(twelve) || it["detail"] != "Sourdough Weekly" {
		t.Fatalf("no length: %v", it)
	}
	// in progress comes first, the one played last on top; a played one is gone
	e.listen(e.alice, rye, time.Now().Add(-2*time.Hour), 720, 720, false)
	e.listen(e.alice, basics, time.Now().Add(-time.Hour), 1800, 1800, false)
	e.listen(e.alice, knives, time.Now().Add(-time.Minute), 45, 45, true)
	res = e.ok(e.alice, "GET", "/api/v1/pocket/widget", nil)
	if got := titles(t, res); got != "Sourdough basics | Rye bread | Episode 12" || res["line"] != "1 new episode this week" {
		t.Fatalf("in progress first: %s, %v", got, res)
	}
	all = capItems(t, res)
	if all[0]["detail"] != "Kitchen Radio · 12 min left" || all[1]["detail"] != "Kitchen Radio · 50 min left" {
		t.Fatalf("time left: %v", all)
	}
	// only what is in progress: no line about new episodes
	e.ok(e.alice, "POST", "/api/v1/episodes/"+id64(twelve)+"/played", nil)
	res = e.ok(e.alice, "GET", "/api/v1/pocket/widget", nil)
	if got := titles(t, res); got != "Sourdough basics | Rye bread" || res["line"] != nil {
		t.Fatalf("no new ones: %s, %v", got, res)
	}
	// Bob sees his own
	if got := titles(t, e.ok(e.bob, "GET", "/api/v1/pocket/widget", nil)); got != "Sourdough for Bob" {
		t.Fatalf("bob: %s", got)
	}
	if code, _ := e.call("", "GET", "/api/v1/pocket/widget", nil); code != 401 {
		t.Fatalf("signed out: %d", code)
	}
}

func TestPocketWidgetIsShort(t *testing.T) {
	e := newCapEnv(t)
	p := e.show(e.a, "Daily", "")
	now := time.Now()
	for i := 0; i < 20; i++ {
		id := e.episode(p, fmt.Sprintf("Day %02d", i), "", 600, now.Add(-time.Duration(i)*24*time.Hour-time.Hour))
		if i >= 10 && i < 16 {
			e.listen(e.alice, id, now.Add(-time.Duration(i)*time.Hour), 60, 60, false)
		}
	}
	res := e.ok(e.alice, "GET", "/api/v1/pocket/widget", nil)
	if got := titles(t, res); got != "Day 10 | Day 11 | Day 12 | Day 13 | Day 00 | Day 01 | Day 02 | Day 03" || res["line"] != "7 new episodes this week" {
		t.Fatalf("four in progress, then the newest: %s, %v", got, res["line"])
	}
}

// episodesOf counts a person's episodes and how many of them wait for a download.
func (e *capEnv) episodesOf(u *store.User) (n, queued int) {
	e.t.Helper()
	eps, err := e.s.store.ListEpisodes(context.Background(), store.EpisodeFilter{UserID: u.ID})
	if err != nil {
		e.t.Fatal(err)
	}
	for _, ep := range eps {
		if ep.Download == store.DownloadQueued {
			queued++
		}
	}
	return len(eps), queued
}

func (e *capEnv) shows(u *store.User) []string {
	e.t.Helper()
	pods, _ := e.s.store.ListPodcasts(context.Background(), u.ID, "", "", "title")
	out := []string{}
	for _, p := range pods {
		out = append(out, p.Title)
	}
	return out
}

func TestPocketCaptureVideo(t *testing.T) {
	e := newCapEnv(t)
	line := map[string]any{"request_id": "req-video-0000000001", "text": "Look at this https://youtu.be/AAAAAAAAAAA?si=xyz"}
	code, res := e.call(e.alice, "POST", "/api/v1/pocket/capture", line)
	if code != 201 || res["title"] != "Video AAAAAAAAAAA" || !strings.HasPrefix(res["id"].(string), "episode:") ||
		res["url"] != "/episodes/"+strings.TrimPrefix(res["id"].(string), "episode:") {
		t.Fatalf("a video: %d %v", code, res)
	}
	// it is in Picks, as POST /youtube/video leaves it: looked up, and waiting for its download
	ep := e.ok(e.alice, "GET", "/api/v1"+res["url"].(string), nil)
	if ep["podcast_title"] != "Picks" || ep["youtube_id"] != "AAAAAAAAAAA" || ep["duration"] != 300.0 || ep["download"] != store.DownloadQueued ||
		ep["link"] != "https://www.youtube.com/watch?v=AAAAAAAAAAA" {
		t.Fatalf("the episode: %v", ep)
	}
	// the same request again: the same answer, nothing new
	for i := 0; i < 2; i++ {
		if code, again := e.call(e.alice, "POST", "/api/v1/pocket/capture", line); code != 201 || again["id"] != res["id"] || again["title"] != res["title"] {
			t.Fatalf("again: %d %v", code, again)
		}
	}
	if n, queued := e.episodesOf(e.a); n != 1 || queued != 1 {
		t.Fatalf("alice has %d episodes, %d queued", n, queued)
	}
	// the same id with other words is refused
	if code, res := e.call(e.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-video-0000000001", "text": "https://youtu.be/BBBBBBBBBBB"}); code != 400 ||
		res["error"] != "this was already kept with other words" {
		t.Fatalf("other words: %d %v", code, res)
	}
	// pasted once more later: it is found again, not added twice, and that answer is remembered too
	later := map[string]any{"request_id": "req-video-0000000002", "title": "https://www.youtube.com/watch?v=AAAAAAAAAAA&list=PLxyz"}
	for i := 0; i < 2; i++ {
		if code, again := e.call(e.alice, "POST", "/api/v1/pocket/capture", later); code != 201 || again["id"] != res["id"] {
			t.Fatalf("pasted again: %d %v", code, again)
		}
	}
	if code, res := e.call(e.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-video-0000000002", "text": "https://youtu.be/BBBBBBBBBBB"}); code != 400 {
		t.Fatalf("other words after a found one: %d %v", code, res)
	}
	if n, _ := e.episodesOf(e.a); n != 1 {
		t.Fatalf("alice has %d episodes", n)
	}
	// a video that cannot be had says so, and keeps nothing
	if code, res := e.call(e.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-video-0000000003", "text": "https://youtu.be/GONEGONEGON"}); code != 400 ||
		!strings.Contains(res["error"].(string), "Video unavailable") {
		t.Fatalf("gone: %d %v", code, res)
	}
	// Bob's Picks are his own: the same request id and link make his own episode
	code, bobs := e.call(e.bob, "POST", "/api/v1/pocket/capture", line)
	if code != 201 || bobs["id"] == res["id"] {
		t.Fatalf("bob: %d %v", code, bobs)
	}
	if n, _ := e.episodesOf(e.a); n != 1 {
		t.Fatalf("alice has %d episodes after bob's", n)
	}
	if code, _ := e.call(e.bob, "GET", "/api/v1"+res["url"].(string), nil); code != 404 {
		t.Fatalf("bob opens alice's episode: %d", code)
	}
	if code, _ := e.call("", "POST", "/api/v1/pocket/capture", line); code != 401 {
		t.Fatalf("signed out: %d", code)
	}
	// the app's own route answers as it did: the episode, a video that is gone, something that is no video
	if code, ep := e.call(e.alice, "POST", "/api/v1/youtube/video", map[string]any{"url": "https://youtu.be/CCCCCCCCCCC"}); code != 201 || ep["title"] != "Video CCCCCCCCCCC" ||
		ep["podcast_title"] != "Picks" || ep["chapters"] == nil || ep["source"].(map[string]any)["title"] != "Some Channel" {
		t.Fatalf("POST /youtube/video: %d %v", code, ep)
	}
	if code, res := e.call(e.alice, "POST", "/api/v1/youtube/video", map[string]any{"url": "https://youtu.be/GONEGONEGON"}); code != 502 {
		t.Fatalf("POST /youtube/video, gone: %d %v", code, res)
	}
	if code, res := e.call(e.alice, "POST", "/api/v1/youtube/video", map[string]any{"url": "https://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa"}); code != 400 ||
		res["error"] != "that is a channel or playlist: subscribe to it instead" {
		t.Fatalf("POST /youtube/video, a channel: %d %v", code, res)
	}
}

func TestPocketCaptureSameRequestAtOnce(t *testing.T) {
	e := newCapEnv(t)
	// Picks is there already
	e.ok(e.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-first-0000000001", "text": "https://youtu.be/AAAAAAAAAAA"})
	line := map[string]any{"request_id": "req-twice-0000000001", "text": "https://youtu.be/BBBBBBBBBBB"}
	var wg sync.WaitGroup
	ids := make([]any, 4)
	codes := make([]int, 4)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res map[string]any
			codes[i], res = e.call(e.alice, "POST", "/api/v1/pocket/capture", line)
			ids[i] = res["id"]
		}()
	}
	wg.Wait()
	for i := range ids {
		if codes[i] != 201 || ids[i] != ids[0] || ids[i] == nil {
			t.Fatalf("answers: %v %v", codes, ids)
		}
	}
	if n, _ := e.episodesOf(e.a); n != 2 {
		t.Fatalf("alice has %d episodes", n)
	}
}

func TestPocketCaptureFollow(t *testing.T) {
	e := newCapEnv(t)
	fs := feedServer(t)
	line := map[string]any{"request_id": "req-feed-00000000001", "text": fs.URL + "/a.xml"}
	code, res := e.call(e.alice, "POST", "/api/v1/pocket/capture", line)
	if code != 201 || res["title"] != "Show a" || !strings.HasPrefix(res["id"].(string), "show:") || res["url"] != "/podcasts/"+strings.TrimPrefix(res["id"].(string), "show:") {
		t.Fatalf("a feed: %d %v", code, res)
	}
	// followed as POST /podcasts follows it: the feed was read
	show := e.ok(e.alice, "GET", "/api/v1"+res["url"].(string), nil)
	if show["feed_url"] != fs.URL+"/a.xml" || show["episode_count"] != 1.0 {
		t.Fatalf("the show: %v", show)
	}
	if code, again := e.call(e.alice, "POST", "/api/v1/pocket/capture", line); code != 201 || again["id"] != res["id"] || again["title"] != "Show a" {
		t.Fatalf("again: %d %v", code, again)
	}
	// a new request for a show already followed says so
	if code, res := e.call(e.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-feed-00000000002", "text": fs.URL + "/a.xml"}); code != 400 ||
		res["error"] != "you already follow Show a" {
		t.Fatalf("already followed: %d %v", code, res)
	}
	// the one link in a sentence; a YouTube channel
	if code, res := e.call(e.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-feed-00000000003", "title": "From Ana", "text": "Try this one: " + fs.URL + "/b.xml."}); code != 201 ||
		res["title"] != "Show b" {
		t.Fatalf("in a sentence: %d %v", code, res)
	}
	if code, res := e.call(e.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-feed-00000000004", "text": "https://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa"}); code != 201 ||
		res["title"] != "Channel UCaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("a channel: %d %v", code, res)
	}
	if got := e.shows(e.a); len(got) != 3 {
		t.Fatalf("alice follows %v", got)
	}
	// what is not an address, or more than one, is refused with what can be pasted
	for i, text := range []string{"buy milk", "sourdough", "@someone", fs.URL + "/a.xml and " + fs.URL + "/b.xml"} {
		code, res := e.call(e.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": fmt.Sprintf("req-none-%011d", i), "text": text})
		if msg, _ := res["error"].(string); code != 400 || !strings.HasPrefix(msg, "paste the address of a YouTube video") {
			t.Fatalf("%q: %d %v", text, code, res)
		}
	}
	// an address that is no feed says so
	if code, res := e.call(e.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-feed-00000000005", "text": e.ts.URL + "/healthz"}); code != 400 ||
		!strings.HasPrefix(res["error"].(string), "could not read the feed") {
		t.Fatalf("not a feed: %d %v", code, res)
	}
	if got := e.shows(e.a); len(got) != 3 {
		t.Fatalf("alice follows %v", got)
	}
	// Bob follows nothing of it
	if got := e.shows(e.b); len(got) != 0 {
		t.Fatalf("bob follows %v", got)
	}
	if code, bobs := e.call(e.bob, "POST", "/api/v1/pocket/capture", line); code != 201 || bobs["id"] == res["id"] {
		t.Fatalf("bob's own: %d %v", code, bobs)
	}
}

func TestCaptureAddress(t *testing.T) {
	for line, want := range map[string]string{
		"https://example.com/feed.xml":            "https://example.com/feed.xml",
		"example.com/feed.xml":                    "https://example.com/feed.xml",
		"HTTPS://Example.com/Feed":                "HTTPS://Example.com/Feed",
		"Listen to https://example.com/feed.xml!": "https://example.com/feed.xml",
		"https://example.com/feed.":               "https://example.com/feed.",
		"milk":                                    "",
		"buy milk at example.com":                 "",
		"https://a.example/1 https://a.example/2": "",
		"ftp://example.com/feed":                  "",
		"https://localhost/feed":                  "",
		"":                                        "",
	} {
		if got, ok := captureAddress(line); got != want || ok != (want != "") {
			t.Errorf("%q → %q %v, want %q", line, got, ok, want)
		}
	}
}

func TestCapabilityLinkScopes(t *testing.T) {
	e := newCapEnv(t)
	e.kitchen()
	day := "/api/v1/pocket/day/" + store.Today()
	line := map[string]any{"request_id": "req-link-00000000001", "text": "https://youtu.be/AAAAAAAAAAA"}
	for _, c := range []struct {
		who, method, path string
		want              int
	}{
		// each scope opens its one route, and who am I
		{"search", "GET", "/api/v1/pocket/search?q=sourdough", 200},
		{"search", "GET", "/api/v1/auth/me", 200},
		{"search", "GET", day, 403},
		{"search", "GET", "/api/v1/pocket/widget", 403},
		{"search", "POST", "/api/v1/pocket/capture", 403},
		{"search", "GET", "/api/v1/search?q=sourdough", 403}, // the app's own directory search stays closed
		{"search", "GET", "/api/v1/episodes?q=sourdough", 403},
		{"search", "GET", "/api/v1/podcasts", 403},
		{"day", "GET", day, 200},
		{"day", "GET", "/api/v1/auth/me", 200},
		{"day", "GET", "/api/v1/pocket/search?q=sourdough", 403},
		{"day", "GET", "/api/v1/pocket/widget", 403},
		{"day", "POST", "/api/v1/pocket/capture", 403},
		{"day", "GET", "/api/v1/listens?date=" + store.Today(), 403},
		{"day", "GET", "/api/v1/history", 403},
		{"widget", "GET", "/api/v1/pocket/widget", 200},
		{"widget", "GET", "/api/v1/auth/me", 200},
		{"widget", "GET", "/api/v1/pocket/search?q=sourdough", 403},
		{"widget", "GET", day, 403},
		{"widget", "POST", "/api/v1/pocket/capture", 403},
		{"widget", "GET", "/api/v1/home", 403},
		{"widget", "GET", "/api/v1/dashboard", 403},
		{"widget", "POST", "/api/v1/refresh", 403},
		{"capture", "POST", "/api/v1/pocket/capture", 201},
		{"capture", "GET", "/api/v1/auth/me", 200},
		{"capture", "GET", "/api/v1/pocket/search?q=sourdough", 403},
		{"capture", "GET", day, 403},
		{"capture", "GET", "/api/v1/pocket/widget", 403},
		{"capture", "POST", "/api/v1/youtube/video", 403},
		{"capture", "POST", "/api/v1/podcasts", 403},
		{"capture", "GET", "/api/v1/episodes", 403},
		{"capture", "DELETE", "/api/v1/history", 403},
		// the scopes Podcasts had before open what they opened, and none of the new routes
		{"history", "GET", "/api/v1/episodes?q=sourdough", 200},
		{"history", "GET", "/api/v1/listens?date=" + store.Today(), 200},
		{"history", "GET", "/api/v1/pocket/search?q=sourdough", 403},
		{"history", "GET", day, 403},
		{"history", "GET", "/api/v1/pocket/widget", 403},
		{"history", "POST", "/api/v1/pocket/capture", 403},
		{"read", "GET", "/api/v1/podcasts", 200},
		{"read", "GET", "/api/v1/pocket/search?q=sourdough", 403},
		{"read", "GET", "/api/v1/pocket/widget", 403},
		{"queue", "POST", "/api/v1/pocket/capture", 403},
		{"queue", "GET", "/api/v1/pocket/widget", 403},
	} {
		if code, res := e.call("pcx_alice_"+c.who, c.method, c.path, line); code != c.want {
			t.Errorf("%s: %s %s → %d (want %d) %v", c.who, c.method, c.path, code, c.want, res)
		}
	}
	// a link acts for its person only: Alice's things, never Bob's
	if got := titles(t, e.ok("pcx_alice_search", "GET", "/api/v1/pocket/search?q=sourdough+for", nil)); got != "" {
		t.Fatalf("alice's link finds bob's: %s", got)
	}
	if got := titles(t, e.ok("pcx_bob_search", "GET", "/api/v1/pocket/search?q=sourdough", nil)); got != "Bob's Sourdough Hour | Sourdough for Bob" {
		t.Fatalf("bob's link: %s", got)
	}
	if got := titles(t, e.ok("pcx_bob_widget", "GET", "/api/v1/pocket/widget", nil)); got != "Sourdough for Bob" {
		t.Fatalf("bob's widget: %s", got)
	}
	// what the link kept is Alice's
	if n, _ := e.episodesOf(e.b); n != 1 {
		t.Fatalf("bob has %d episodes", n)
	}
	if got := e.shows(e.a); len(got) != 3 || got[1] != "Picks" {
		t.Fatalf("alice's shows: %v", got)
	}
	// a token Core does not vouch for
	if code, _ := e.call("pcx_alice_nothing", "GET", "/api/v1/pocket/widget", nil); code != 401 {
		t.Fatalf("an unknown link: %d", code)
	}
}

func TestManifestCarriesCapabilities(t *testing.T) {
	e := newCapEnv(t)
	if err := e.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	m := e.manifest
	e.mu.Unlock()
	if m["category"] != "Media" {
		t.Fatalf("category: %v", m["category"])
	}
	// the capabilities sit in the manifest itself
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	capture, _ := m["capture"].(map[string]any)
	if fmt.Sprint(search["bangs"]) != "[pod podcasts]" || fmt.Sprint(search["kinds"]) != "[episode show]" || search["what"] == "" {
		t.Fatalf("search: %v", search)
	}
	if day["label"] != "Podcasts" || day["one"] != "episode" || day["many"] != "episodes" || day["unit"] != "min" {
		t.Fatalf("day: %v", day)
	}
	if widget["title"] != "Podcasts" {
		t.Fatalf("widget: %v", widget)
	}
	kinds, _ := capture["kinds"].([]any)
	if len(kinds) != 1 || kinds[0].(map[string]any)["kind"] != "link" || kinds[0].(map[string]any)["label"] != "Link" || kinds[0].(map[string]any)["fields"] != nil {
		t.Fatalf("capture: %v", capture)
	}
	offers := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offers[o.(map[string]any)["scope"].(string)] = true
	}
	for _, scope := range []string{core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeCapture, "podcasts:read", "history:read", "queue:write", core.ScopeMoveExport, core.ScopeMoveImport} {
		if !offers[scope] {
			t.Errorf("no offer for %s in %v", scope, offers)
		}
	}
}

// Without Pocket Core the routes are there for the person signed in, and a
// link token is just a token nobody knows.
func TestCapabilitiesWithoutCore(t *testing.T) {
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(dir+"/t.db", log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(&config.Config{AuthMode: "local", SessionTTL: time.Hour, DataDir: dir, RefreshMinutes: 60, DownloadWorkers: 1}, st, log, nil)
	if err = s.core.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	u, _ := s.firstOrSystemUser(ctx, "alice@example.com", "Alice")
	p := &store.Podcast{UserID: u.ID, Kind: store.KindRSS, Title: "Show", FeedURL: "https://example.com/feed"}
	if err = st.CreatePodcast(ctx, p); err != nil {
		t.Fatal(err)
	}
	ep := store.Episode{PodcastID: p.ID, GUID: "e", Title: "Episode", Duration: 1000}
	if _, err = st.UpsertEpisode(ctx, &ep); err != nil {
		t.Fatal(err)
	}
	tok, _, _ := st.CreateAPIToken(ctx, u.ID, "test")
	ts := httptest.NewServer(s.Router())
	defer ts.Close()
	get := func(token, path string) (int, string) {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	for path, want := range map[string]string{"/api/v1/pocket/search?q=episode": `"title":"Episode"`, "/api/v1/pocket/widget": `"title":"Episode"`,
		"/api/v1/pocket/day/" + store.Today(): `"items":[]`} {
		if code, body := get(tok, path); code != 200 || !strings.Contains(body, want) {
			t.Errorf("%s: %d %s", path, code, body)
		}
		if code, _ := get("pcx_alice_search", path); code != 401 {
			t.Errorf("%s with a link token and no Core: %d", path, code)
		}
	}
}
