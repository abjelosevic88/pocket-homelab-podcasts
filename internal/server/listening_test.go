package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/config"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListeningAPI(t *testing.T) {
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
	b, _ := s.firstOrSystemUser(ctx, "bob@example.com", "Bob")
	p := &store.Podcast{UserID: u.ID, Kind: store.KindRSS, Title: "Show", FeedURL: "https://example.com/feed"}
	if err = st.CreatePodcast(ctx, p); err != nil {
		t.Fatal(err)
	}
	e := store.Episode{PodcastID: p.ID, GUID: "e", Title: "Episode", Duration: 1000}
	if _, err = st.UpsertEpisode(ctx, &e); err != nil {
		t.Fatal(err)
	}
	tok, _, _ := st.CreateAPIToken(ctx, u.ID, "test")
	btok, _, _ := st.CreateAPIToken(ctx, b.ID, "test")
	ts := httptest.NewServer(s.Router())
	defer ts.Close()
	call := func(token, method, path, body string, want int) map[string]any {
		t.Helper()
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s: %d want %d: %s", path, res.StatusCode, want, data)
		}
		var out map[string]any
		_ = json.Unmarshal(data, &out)
		return out
	}
	path := fmt.Sprintf("/api/v1/episodes/%d/progress", e.ID)
	at := time.Now().Add(-72 * time.Hour).UTC()
	body := fmt.Sprintf(`{"report_id":"stable-id","listened_at":%q,"position":100,"listened":60,"completed":false}`, at.Format(time.RFC3339Nano))
	if call(tok, "POST", path, body, 200)["applied"] != true {
		t.Fatal("not applied")
	}
	if call(tok, "POST", path, body, 200)["applied"] != false {
		t.Fatal("duplicate not acknowledged")
	}
	call(tok, "POST", path, strings.Replace(body, `"listened":60`, `"listened":61`, 1), 409)
	call(btok, "POST", path, body, 404)
	call(tok, "POST", path, `{"report_id":"no-time","listened":60}`, 400)
	call(tok, "POST", path, `{"listened_at":"2026-01-01T00:00:00Z","listened":60}`, 400)
	call(tok, "POST", path, fmt.Sprintf(`{"report_id":"future","listened_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339)), 400)
	call(tok, "GET", "/api/v1/listens?date=bad", "", 400)
	rows := call(tok, "GET", "/api/v1/listens?date="+at.In(time.Local).Format("2006-01-02"), "", 200)["listens"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["seconds"] != float64(60) {
		t.Fatalf("daily: %v", rows)
	}
	if rows := call(btok, "GET", "/api/v1/listens?date="+at.In(time.Local).Format("2006-01-02"), "", 200)["listens"].([]any); len(rows) != 0 {
		t.Fatal("leaked history")
	}
	call(tok, "GET", "/api/v1/listens/dates", "", 200)
	for _, path := range []string{"/api/v1/listens", "/api/v1/listens/dates"} {
		r, _ := http.NewRequest("GET", path, nil)
		if !linkAllows([]string{"history:read"}, r) || linkAllows([]string{"podcasts:read"}, r) {
			t.Fatal("wrong read scope", path)
		}
	}
	r, _ := http.NewRequest("POST", path, nil)
	if linkAllows([]string{"history:read"}, r) {
		t.Fatal("read-only link can write progress")
	}
}
