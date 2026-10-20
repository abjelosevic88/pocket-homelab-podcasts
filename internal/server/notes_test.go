package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/config"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

// An episode's page lists the person's notes about it from Pocket Notes,
// asked as that person through Core; without Core there is nothing to show.
func TestEpisodeNotes(t *testing.T) {
	notes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mentions" || r.Header.Get("Authorization") != "Bearer pcx_alice" {
			http.Error(w, `{"error":"no"}`, http.StatusUnauthorized)
			return
		}
		if !strings.HasSuffix(r.URL.Query().Get("url"), "/episodes/1") {
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 9, "title": "Ep 1, the good part", "category": "Podcasts", "modified": 1792500000}})
	}))
	t.Cleanup(notes.Close)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ User, App string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path != "/api/v1/links/token" || in.App != "notes" {
			http.NotFound(w, r)
			return
		}
		if in.User != "alice@example.com" {
			http.Error(w, `{"error":"off","reason":"off_admin"}`, http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pcx_alice", "url": notes.URL, "public_url": "https://notes.example.com"})
	}))
	t.Cleanup(core.Close)

	dir := t.TempDir()
	cfg := &config.Config{AuthMode: "local", SessionTTL: time.Hour, DataDir: dir, DBPath: dir + "/t.db", RefreshMinutes: 60, DownloadWorkers: 1,
		BaseURL: "https://podcasts.example.com", CoreURL: core.URL}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(cfg.DBPath, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := New(cfg, st, log, nil)
	ctx := context.Background()
	alice, _ := s.firstOrSystemUser(ctx, "alice@example.com", "Alice")
	bob, _ := s.firstOrSystemUser(ctx, "bob@example.com", "Bob")
	mk := func(uid int64, feed string) int64 {
		p := &store.Podcast{UserID: uid, Kind: store.KindRSS, Title: "The Show", FeedURL: feed}
		if err := st.CreatePodcast(ctx, p); err != nil {
			t.Fatal(err)
		}
		e := store.Episode{PodcastID: p.ID, GUID: "g", Title: "Ep 1", EnclosureURL: feed + "/1.mp3", Duration: 100}
		if _, err := st.UpsertEpisode(ctx, &e); err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	aid := mk(alice.ID, "https://a/feed")
	bid := mk(bob.ID, "https://b/feed")
	aTok, _, _ := st.CreateAPIToken(ctx, alice.ID, "a")
	bTok, _, _ := st.CreateAPIToken(ctx, bob.ID, "b")
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	get := func(tok string, id int64) (int, notesBox) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/episodes/"+itoa(id)+"/notes", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var b notesBox
		_ = json.NewDecoder(res.Body).Decode(&b)
		return res.StatusCode, b
	}

	c, box := get(aTok, aid)
	if c != 200 || !box.Available || len(box.Notes) != 1 || box.Notes[0].URL != "https://notes.example.com/note/9" {
		t.Fatalf("alice: %d %+v", c, box)
	}
	nu, _ := url.Parse(box.NewURL)
	if nu.Query().Get("url") != "https://podcasts.example.com/episodes/"+itoa(aid) || nu.Query().Get("folder") != "Podcasts" || !strings.Contains(nu.Query().Get("label"), "The Show") {
		t.Fatalf("new url: %s", box.NewURL)
	}
	if c, _ := get(bTok, aid); c != 404 {
		t.Fatalf("bob on alice's episode: %d", c)
	}
	if _, box := get(bTok, bid); box.Available || box.Missing || !strings.Contains(box.Reason, "Links") {
		t.Fatalf("bob, link off for the box: %+v", box)
	}

	// Pocket Podcasts on its own, without Core: no box at all.
	s2 := New(&config.Config{AuthMode: "local", SessionTTL: time.Hour, DataDir: dir, DBPath: dir + "/t.db", RefreshMinutes: 60, DownloadWorkers: 1, BaseURL: "https://podcasts.example.com"}, st, log, nil)
	ts2 := httptest.NewServer(s2.Router())
	t.Cleanup(ts2.Close)
	req, _ := http.NewRequest("GET", ts2.URL+"/api/v1/episodes/"+itoa(aid)+"/notes", nil)
	req.Header.Set("Authorization", "Bearer "+aTok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	box = notesBox{}
	_ = json.NewDecoder(res.Body).Decode(&box)
	if !box.Missing || box.Available {
		t.Fatalf("standalone: %+v", box)
	}
}
