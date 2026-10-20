package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/config"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

// Two people, one box: nothing of one is reachable by the other.
func TestStrictlyPerUser(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{AuthMode: "local", SessionTTL: time.Hour, DataDir: dir, DBPath: dir + "/t.db", RefreshMinutes: 60, DownloadWorkers: 1}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(cfg.DBPath, log)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(cfg, st, log, nil)
	if err := s.core.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	alice, _ := s.firstOrSystemUser(ctx, "alice@example.com", "Alice")
	bob, _ := s.firstOrSystemUser(ctx, "bob@example.com", "Bob")
	if alice.Role != "admin" || bob.Role != "member" {
		t.Fatalf("roles: %s %s", alice.Role, bob.Role)
	}
	p := &store.Podcast{UserID: alice.ID, Kind: store.KindRSS, Title: "A", FeedURL: "https://a/feed"}
	if err := st.CreatePodcast(ctx, p); err != nil {
		t.Fatal(err)
	}
	e := store.Episode{PodcastID: p.ID, GUID: "g", Title: "E", EnclosureURL: "https://a/1.mp3", Duration: 100}
	if _, err := st.UpsertEpisode(ctx, &e); err != nil {
		t.Fatal(err)
	}
	pl := store.Playlist{UserID: alice.ID, Name: "mine"}
	_ = st.CreatePlaylist(ctx, &pl)
	aTok, _, _ := st.CreateAPIToken(ctx, alice.ID, "a")
	bTok, _, _ := st.CreateAPIToken(ctx, bob.ID, "b")

	ts := httptest.NewServer(s.Router())
	defer ts.Close()
	do := func(tok, method, path, body string) int {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	pid := itoa(p.ID)
	eid := itoa(e.ID)
	for _, c := range []struct {
		method, path, body string
	}{
		{"GET", "/api/v1/podcasts/" + pid, ""}, {"PUT", "/api/v1/podcasts/" + pid, `{"title":"x"}`}, {"DELETE", "/api/v1/podcasts/" + pid, ""},
		{"GET", "/api/v1/podcasts/" + pid + "/episodes", ""}, {"POST", "/api/v1/podcasts/" + pid + "/refresh", ""},
		{"GET", "/api/v1/episodes/" + eid, ""}, {"POST", "/api/v1/episodes/" + eid + "/progress", `{"position":5,"listened":5}`},
		{"POST", "/api/v1/episodes/" + eid + "/queue", ""}, {"POST", "/api/v1/episodes/" + eid + "/save", ""}, {"POST", "/api/v1/episodes/" + eid + "/played", ""},
		{"GET", "/api/v1/episodes/" + eid + "/stream", ""}, {"HEAD", "/api/v1/episodes/" + eid + "/stream", ""}, {"POST", "/api/v1/episodes/" + eid + "/download", ""}, {"DELETE", "/api/v1/episodes/" + eid, ""},
		{"GET", "/api/v1/playlists/" + itoa(pl.ID), ""}, {"DELETE", "/api/v1/playlists/" + itoa(pl.ID), ""},
	} {
		if code := do(bTok, c.method, c.path, c.body); code != 404 {
			t.Errorf("bob %s %s → %d, want 404", c.method, c.path, code)
		}
		if code := do(aTok, "GET", "/api/v1/podcasts/"+pid, ""); code != 200 {
			t.Fatalf("alice lost her podcast: %d", code)
		}
	}
	// an availability check answers without fetching the file
	if code := do(aTok, "HEAD", "/api/v1/episodes/"+eid+"/stream", ""); code != 200 {
		t.Errorf("alice HEAD stream → %d, want 200", code)
	}
	if got, _ := st.GetEpisode(ctx, alice.ID, e.ID); got == nil || got.Download != store.DownloadNone {
		t.Errorf("HEAD started a download: %+v", got)
	}
	if code := do(bTok, "GET", "/api/v1/backup", ""); code != 403 {
		t.Errorf("member backup → %d, want 403", code)
	}
	// a feed key only opens its owner's feeds
	key, _ := st.EnsureFeedKey(ctx, bob.ID)
	res, _ := http.Get(ts.URL + "/feed/" + key + "/" + pid + ".xml")
	if res.StatusCode != 404 {
		t.Errorf("bob's feed key reads alice's podcast: %d", res.StatusCode)
	}
	akey, _ := st.EnsureFeedKey(ctx, alice.ID)
	res, _ = http.Get(ts.URL + "/feed/" + akey + "/" + pid + ".xml")
	if res.StatusCode != 200 {
		t.Errorf("alice's feed: %d", res.StatusCode)
	}
	// bob's own list is empty, alice's has one
	if n := countJSON(t, ts.URL+"/api/v1/podcasts", bTok); n != 0 {
		t.Errorf("bob sees %d podcasts", n)
	}
	if n := countJSON(t, ts.URL+"/api/v1/podcasts", aTok); n != 1 {
		t.Errorf("alice sees %d podcasts", n)
	}
}

func itoa(n int64) string { return strings.TrimSpace(strings.Repeat(" ", 0) + fmtInt(n)) }

func fmtInt(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func countJSON(t *testing.T, url, tok string) int {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return strings.Count(string(b), `"feed_url"`)
}
