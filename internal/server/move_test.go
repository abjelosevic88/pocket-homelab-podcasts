package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/config"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

// moveCore is a fake Pocket Core: pcx_<who>_<export|import|other> tokens are
// links acting for alice@ or bob@ with move:export, move:import or another scope.
func moveCore(t *testing.T) (*httptest.Server, func() core.Manifest) {
	t.Helper()
	var mu sync.Mutex
	var manifest core.Manifest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&manifest)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"app":{}}`))
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
			if !strings.HasPrefix(in.Token, "pcx_") || len(parts) != 2 {
				_, _ = w.Write([]byte(`{"active":false}`))
				return
			}
			email := parts[0] + "@example.com"
			scope := map[string]string{"export": "move:export", "import": "move:import", "other": "podcasts:read"}[parts[1]]
			_ = json.NewEncoder(w).Encode(map[string]any{
				"active": true, "kind": "delegated", "scope": scope, "audience": "podcasts",
				"user": map[string]any{"id": 9, "sub": email, "email": email, "name": parts[0], "role": "member"},
				"app":  map[string]any{"id": 4, "slug": "move", "name": "Pocket Move"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() core.Manifest { mu.Lock(); defer mu.Unlock(); return manifest }
}

// feedServer serves small RSS feeds at /<name>.xml.
func feedServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".xml")
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Show %s</title><link>https://example.com/%s</link>
<item><title>%s one</title><guid>%s-1</guid><pubDate>Mon, 02 Nov 2026 10:00:00 GMT</pubDate><enclosure url="https://example.com/%s-1.mp3" type="audio/mpeg" length="1"/></item>
</channel></rss>`, name, name, name, name, name)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type moveEnv struct {
	t        *testing.T
	s        *Server
	ts       *httptest.Server
	manifest func() core.Manifest
}

func newMoveEnv(t *testing.T) *moveEnv {
	t.Helper()
	coreSrv, manifest := moveCore(t)
	dir := t.TempDir()
	// a stand-in yt-dlp: a channel listing with no videos
	yt := filepath.Join(dir, "yt-dlp")
	script := `#!/bin/sh
for a; do last=$a; done
if [ "$1" = "--version" ]; then echo 2026.01.01; exit 0; fi
id=$(echo "$last" | sed -n 's#.*/channel/\(UC[A-Za-z0-9_-]*\).*#\1#p')
printf '{"id":"%s","channel_id":"%s","title":"Channel %s - Videos","channel":"Channel %s","entries":[]}' "$id" "$id" "$id" "$id"
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
	s := New(cfg, st, log, nil)
	s.core = core.New(coreSrv.URL, core.Options{DB: st.DB, Token: "pca_podcasts"})
	if err := s.core.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	// the box's owner exists first, so the people Move acts for are members
	if _, err := s.firstOrSystemUser(context.Background(), "owner@example.com", "Owner"); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	return &moveEnv{t: t, s: s, ts: ts, manifest: manifest}
}

func (e *moveEnv) do(tok, method, path, ctype string, body []byte) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.ts.URL+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// upload posts a file in the multipart field Move uses.
func (e *moveEnv) upload(tok, path, field string, data []byte) (int, map[string]any) {
	e.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile(field, "upload")
	_, _ = fw.Write(data)
	_ = mw.Close()
	code, body := e.do(tok, "POST", path, mw.FormDataContentType(), buf.Bytes())
	out := map[string]any{}
	_ = json.Unmarshal([]byte(body), &out)
	if code != 200 {
		e.t.Logf("%s → %d %s", path, code, body)
	}
	return code, out
}

func (e *moveEnv) feeds(email string) []string {
	e.t.Helper()
	u, err := e.s.store.GetUserByEmail(context.Background(), email)
	if err != nil {
		return nil
	}
	pods, _ := e.s.store.ListPodcasts(context.Background(), u.ID, "", "", "title")
	out := []string{}
	for _, p := range pods {
		out = append(out, p.FeedURL)
	}
	return out
}

func num(m map[string]any, k string) int {
	f, _ := m[k].(float64)
	return int(f)
}

func TestPortableManifestAndLinkScopes(t *testing.T) {
	e := newMoveEnv(t)
	if err := e.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	m := e.manifest()
	scopes := map[string]bool{}
	for _, o := range m.Offers {
		scopes[o.Scope] = true
	}
	if !scopes[core.ScopeMoveExport] || !scopes[core.ScopeMoveImport] || m.Portable == nil || len(m.Portable.Imports) != 3 {
		t.Fatalf("manifest: offers %v portable %+v", scopes, m.Portable)
	}

	exp, imp, other := "pcx_bob_export", "pcx_bob_import", "pcx_bob_other"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "GET", "/api/v1/export.opml", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "POST", "/api/v1/import/opml", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/podcasts", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/podcasts", 403},
		{imp, "DELETE", "/api/v1/history", 403},
		{other, "GET", core.PortablePath, 403},
		{other, "GET", "/api/v1/export.json", 403},
	} {
		if code, body := e.do(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d (want %d) %s", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	// the description names the paths it opens
	_, body := e.do(exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal([]byte(body), &p); err != nil || len(p.Exports) != 2 || p.Exports[0].Kind != core.KindPocket {
		t.Fatalf("portable: %s", body)
	}
	// move:import may POST each import (an empty file is a bad request, not a refusal)
	for _, path := range []string{"/api/v1/import", "/api/v1/import/opml", "/api/v1/import/youtube.csv"} {
		if code, body := e.do(imp, "POST", path, "application/json", []byte("")); code == 401 || code == 403 {
			t.Errorf("import %s refused: %d %s", path, code, body)
		}
	}
}

func TestMoveImportsAddOnceAndPerPerson(t *testing.T) {
	e := newMoveEnv(t)
	fs := feedServer(t)
	opml := fmt.Sprintf(`<?xml version="1.0"?><opml version="2.0"><body>
<outline text="A" type="rss" xmlUrl="%s/a.xml"/><outline text="B" type="rss" xmlUrl="%s/b.xml"/></body></opml>`, fs.URL, fs.URL)

	// Alice brings an OPML in twice: the second run adds nothing
	code, r := e.upload("pcx_alice_import", "/api/v1/import/opml", "file", []byte(opml))
	if code != 200 || num(r, "added") != 2 {
		t.Fatalf("first OPML: %d %v", code, r)
	}
	code, r = e.upload("pcx_alice_import", "/api/v1/import/opml", "file", []byte(opml))
	if code != 200 || num(r, "added") != 0 || num(r, "skipped") != 2 {
		t.Fatalf("second OPML: %d %v", code, r)
	}
	if got := e.feeds("alice@example.com"); len(got) != 2 {
		t.Fatalf("alice has %v", got)
	}
	// Bob sees none of it, and his export is empty
	if got := e.feeds("bob@example.com"); len(got) != 0 {
		t.Fatalf("bob has %v", got)
	}
	if code, body := e.do("pcx_bob_export", "GET", "/api/v1/export.opml", "", nil); code != 200 || strings.Contains(body, fs.URL) {
		t.Fatalf("bob's OPML: %d %s", code, body)
	}

	// Alice's own export, with a playlist of one podcast, goes to Bob twice
	alice, _ := e.s.store.GetUserByEmail(context.Background(), "alice@example.com")
	pods, _ := e.s.store.ListPodcasts(context.Background(), alice.ID, "", "", "title")
	pl := store.Playlist{UserID: alice.ID, Name: "Just A", PodcastIDs: []int64{pods[0].ID}}
	if err := e.s.store.CreatePlaylist(context.Background(), &pl); err != nil {
		t.Fatal(err)
	}
	code, export := e.do("pcx_alice_export", "GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(export, fs.URL+"/a.xml") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	for i, want := range []int{2, 0} {
		code, body := e.do("pcx_bob_import", "POST", "/api/v1/import", "application/json", []byte(export))
		var res map[string]any
		_ = json.Unmarshal([]byte(body), &res)
		if code != 200 || num(res, "added") != want || num(res, "playlists") != 1-i {
			t.Fatalf("bob's import #%d: %d %s", i+1, code, body)
		}
	}
	// the multipart form works too and still adds nothing
	if code, r := e.upload("pcx_bob_import", "/api/v1/import", "file", []byte(export)); code != 200 || num(r, "added") != 0 || num(r, "existing") != 2 {
		t.Fatalf("multipart import: %d %v", code, r)
	}
	bob, _ := e.s.store.GetUserByEmail(context.Background(), "bob@example.com")
	if got := e.feeds("bob@example.com"); len(got) != 2 {
		t.Fatalf("bob has %v", got)
	}
	bpls, _ := e.s.store.ListPlaylists(context.Background(), bob.ID)
	bpods, _ := e.s.store.ListPodcasts(context.Background(), bob.ID, "", "", "title")
	names := map[string]int{}
	var justA *store.Playlist
	for i, p := range bpls {
		names[p.Name]++
		if p.Name == "Just A" {
			justA = &bpls[i]
		}
	}
	for n, c := range names {
		if c != 1 {
			t.Errorf("bob has %d playlists called %q", c, n)
		}
	}
	// the playlist points at Bob's copy of the podcast, not Alice's id
	if justA == nil || len(justA.PodcastIDs) != 1 || justA.PodcastIDs[0] != bpods[0].ID {
		t.Fatalf("bob's playlists: %+v (podcasts %d, %d)", bpls, bpods[0].ID, bpods[1].ID)
	}
	if got := e.feeds("alice@example.com"); len(got) != 2 {
		t.Fatalf("alice changed: %v", got)
	}
}

func TestMoveYouTubeTakeout(t *testing.T) {
	e := newMoveEnv(t)
	csvData := "\xef\xbb\xbfChannel Id,Channel Url,Channel Title\n" +
		"UCaaaaaaaaaaaaaaaaaaaaaa,http://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa,First, the channel\n" +
		"UCbbbbbbbbbbbbbbbbbbbbbb,http://www.youtube.com/channel/UCbbbbbbbbbbbbbbbbbbbbbb,\"Second, quoted\"\n" +
		"UCaaaaaaaaaaaaaaaaaaaaaa,http://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa,First again\n\n"
	code, r := e.upload("pcx_alice_import", "/api/v1/import/youtube.csv", "file", []byte(csvData))
	if code != 200 || num(r, "added") != 2 || num(r, "skipped") != 0 {
		t.Fatalf("first CSV: %d %v", code, r)
	}
	code, r = e.upload("pcx_alice_import", "/api/v1/import/youtube.csv", "file", []byte(csvData))
	if code != 200 || num(r, "added") != 0 || num(r, "skipped") != 2 {
		t.Fatalf("second CSV: %d %v", code, r)
	}
	got := e.feeds("alice@example.com")
	if len(got) != 2 || got[0] != "https://www.youtube.com/channel/UCaaaaaaaaaaaaaaaaaaaaaa/videos" {
		t.Fatalf("alice follows %v", got)
	}
	// Bob's import is his own: Alice's channels don't count as his
	if code, r := e.upload("pcx_bob_import", "/api/v1/import/youtube.csv", "file", []byte(csvData)); code != 200 || num(r, "added") != 2 {
		t.Fatalf("bob's CSV: %d %v", code, r)
	}
	if code, r := e.upload("pcx_bob_import", "/api/v1/import/youtube.csv", "file", []byte("just,some,text\n")); code != 400 {
		t.Fatalf("not a subscriptions file: %d %v", code, r)
	}
}

func TestParseYouTubeSubscriptions(t *testing.T) {
	// a translated header and a row with only an id
	got := parseYouTubeSubscriptions([]byte("ID kanala,URL kanala,Naziv kanala\r\nUCcccccccccccccccccccccc,,Treći\r\n"))
	if len(got) != 1 || got[0].URL != "https://www.youtube.com/channel/UCcccccccccccccccccccccc" || got[0].Title != "Treći" {
		t.Fatalf("got %+v", got)
	}
}
