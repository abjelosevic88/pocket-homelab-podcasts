// Package server wires the HTTP API, auth, background jobs and the embedded SPA.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/config"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/search"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/ytdlp"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg    *config.Config
	store  *store.Store
	log    *slog.Logger
	webFS  fs.FS
	search *search.Client
	yt     *ytdlp.Runner
	http   *http.Client // feeds, enclosures, artwork

	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64

	refreshKick  chan struct{}
	downloadKick chan struct{}
	refreshing   sync.Map // podcast id → true while a refresh runs
	ytVersion    atomic.Value
	ytc          ytChecker
	ytBusy       atomic.Bool

	core    *core.Client
	coreMu  sync.Mutex
	coreOK  bool
	coreErr string

	oidc   *oidcProvider // keyed on the issuer; rebuilt by provider() when it changes
	oidcMu sync.Mutex
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	if cfg.MediaDir == "" {
		cfg.MediaDir = filepath.Join(cfg.DataDir, "media")
	}
	if cfg.ArtworkDir == "" {
		cfg.ArtworkDir = filepath.Join(cfg.DataDir, "artwork")
	}
	if cfg.FeedTimeout == 0 {
		cfg.FeedTimeout = 45 * time.Second
	}
	ua := "PocketPodcasts/" + Version + " (+https://github.com/abjelosevic88/pocket-homelab-podcasts)"
	s := &Server{cfg: cfg, store: st, log: log, webFS: webFS, started: time.Now(),
		refreshKick: make(chan struct{}, 1), downloadKick: make(chan struct{}, 1)}
	s.search = search.New(ua, 15*time.Second)
	s.http = &http.Client{Timeout: cfg.FeedTimeout}
	s.yt = &ytdlp.Runner{Path: cfg.YtDlpPath, SiteDir: filepath.Join(cfg.DataDir, "ytdlp"), Cookies: cfg.YtDlpCookies, Log: log}
	if cfg.YtDlpExtra != "" {
		s.yt.Extra = strings.Fields(cfg.YtDlpExtra)
	}
	s.core = core.New(cfg.CoreURL, core.Options{TokenFile: cfg.DataDir + "/core.token", UserAgent: ua})
	s.oidc = &oidcProvider{issuerURL: cfg.OIDCIssuer}
	s.ytVersion.Store("")
	return s
}

// UserAgent is sent to feeds and directories.
func (s *Server) userAgent() string { return s.search.UserAgent }

// isOwner reports whether uid is the account the box was set up with. Keys
// given to the whole box through the environment are the owner's.
func (s *Server) isOwner(ctx context.Context, uid int64) bool {
	return uid > 0 && uid == s.store.OwnerID(ctx)
}

// searchFor is the directory client with one person's Podcast Index key.
func (s *Server) searchFor(ctx context.Context, uid int64) *search.Client {
	st, _ := s.store.GetSettings(ctx, uid)
	key, secret := st.PodcastIndexKey, st.PodcastIndexSecret
	if key == "" && s.cfg.PodcastIndexKey != "" && s.isOwner(ctx, uid) {
		key, secret = s.cfg.PodcastIndexKey, s.cfg.PodcastIndexSecret
	}
	return s.search.With(key, secret)
}

// Bootstrap seeds the first admin (if configured), demo data and starts the workers.
func (s *Server) Bootstrap(ctx context.Context) error {
	for _, d := range []string{s.cfg.MediaDir, s.cfg.ArtworkDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 && s.cfg.AuthMode == "local" && s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if len(s.cfg.AdminPassword) < 8 {
			return errors.New("ADMIN_PASSWORD must be at least 8 characters")
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
		u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: "admin"}
		if err := s.store.CreateUser(ctx, u); err != nil {
			return err
		}
		s.seedUser(ctx, u.ID)
		st, _ := s.store.GetSettings(ctx, 0)
		st.SetupComplete = true
		if err := s.store.SaveBoxSettings(ctx, st); err != nil {
			return err
		}
		s.log.Info("admin user created from environment", "email", u.Email)
	}
	if s.cfg.DemoData {
		if err := s.seedDemo(ctx); err != nil {
			return err
		}
	}
	_ = s.store.PurgeSessions(ctx)
	_ = s.store.ResetStaleDownloads(ctx)
	go s.refreshLoop(ctx)
	for i := 0; i < s.cfg.DownloadWorkers; i++ {
		go s.downloadWorker(ctx, i)
	}
	go s.housekeeping(ctx)
	go s.quickCheckLoop(ctx)
	go func() {
		if v, err := s.yt.Version(ctx); err == nil {
			s.ytVersion.Store(v)
			s.log.Info("yt-dlp available", "version", v)
		} else {
			s.log.Warn("yt-dlp not available: YouTube subscriptions will not work", "err", err)
		}
	}()
	if s.core.Configured() {
		go s.coreLoop(ctx)
	}
	return nil
}

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5, "application/json", "text/html", "text/css", "application/javascript", "application/xml", "application/rss+xml", "text/plain"))

	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}

	// Public feeds for podcast apps (Apple Podcasts, Overcast…): the key is the person's secret.
	r.Route("/feed/{key}", func(r chi.Router) {
		r.Get("/", s.handleFeedIndex)
		r.Get("/{podcast}.xml", s.handleFeedRSS)
		r.Get("/e/{episode}.{ext}", s.handleFeedMedia)
		r.Get("/e/{episode}/chapters.json", s.handleFeedChapters)
		r.Get("/art/{podcast}.jpg", s.handleFeedArtwork)
		r.Get("/art/e/{episode}.jpg", s.handleFeedEpisodeArtwork)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(timeoutExcept(120*time.Second, "/stream"))
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)

			r.Get("/dashboard", s.handleDashboard)
			r.Get("/home", s.handleHome)
			r.Get("/stats", s.handleStats)
			r.Get("/search", s.handleSearch)
			r.Get("/search/trending", s.handleTrending)
			r.Post("/search/test", s.handleTestPodcastIndex)
			r.Post("/preview", s.handlePreview)

			r.Get("/podcasts", s.handleListPodcasts)
			r.Post("/podcasts", s.handleSubscribe)
			r.Get("/podcasts/{id}", s.handleGetPodcast)
			r.Put("/podcasts/{id}", s.handleUpdatePodcast)
			r.Delete("/podcasts/{id}", s.handleUnsubscribe)
			r.Post("/podcasts/{id}/refresh", s.handleRefreshPodcast)
			r.Post("/podcasts/{id}/favorite", s.handleFavoritePodcast)
			r.Get("/podcasts/{id}/artwork", s.handlePodcastArtwork)
			r.Post("/podcasts/{id}/artwork", s.handleSetPodcastArtwork)
			r.Delete("/podcasts/{id}/artwork", s.handleDeletePodcastArtwork)
			r.Get("/podcasts/{id}/episodes", s.handlePodcastEpisodes)
			r.Post("/podcasts/{id}/played", s.handlePodcastMarkAll)
			r.Post("/podcasts/{id}/download", s.handlePodcastDownloadAll)
			r.Delete("/podcasts/{id}/files", s.handlePodcastDeleteFiles)
			r.Post("/refresh", s.handleRefreshAll)

			r.Get("/episodes", s.handleListEpisodes)
			r.Get("/episodes/{id}", s.handleGetEpisode)
			r.Delete("/episodes/{id}", s.handleDeleteEpisode)
			r.Get("/episodes/{id}/stream", s.handleStream)
			r.Get("/episodes/{id}/artwork", s.handleEpisodeArtwork)
			r.Post("/episodes/{id}/progress", s.handleProgress)
			r.Post("/episodes/{id}/played", s.handleMarkPlayed)
			r.Delete("/episodes/{id}/played", s.handleMarkUnplayed)
			r.Post("/episodes/{id}/download", s.handleDownloadEpisode)
			r.Delete("/episodes/{id}/download", s.handleDeleteDownload)
			r.Post("/episodes/{id}/queue", s.handleQueueAdd)
			r.Delete("/episodes/{id}/queue", s.handleQueueRemove)
			r.Post("/episodes/{id}/save", s.handleSave)
			r.Delete("/episodes/{id}/save", s.handleUnsave)
			r.Delete("/episodes/{id}/history", s.handleForgetEpisode)

			r.Get("/queue", s.handleQueue)
			r.Put("/queue", s.handleQueueReorder)
			r.Delete("/queue", s.handleQueueClear)
			r.Get("/queue/next", s.handleQueueNext)
			r.Get("/history", s.handleHistory)
			r.Delete("/history", s.handleClearHistory)
			r.Get("/saved", s.handleSaved)
			r.Get("/downloads", s.handleDownloads)

			r.Get("/playlists", s.handleListPlaylists)
			r.Post("/playlists", s.handleCreatePlaylist)
			r.Get("/playlists/{id}", s.handleGetPlaylist)
			r.Put("/playlists/{id}", s.handleUpdatePlaylist)
			r.Delete("/playlists/{id}", s.handleDeletePlaylist)
			r.Get("/playlists/{id}/episodes", s.handlePlaylistEpisodes)

			r.Post("/youtube/video", s.handleAddVideo)
			r.Post("/picks", s.handlePickEpisode)
			r.Get("/youtube/search", s.handleSearchChannels)
			r.Get("/youtube/listing", s.handleListing)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Get("/settings/system", s.handleSystemInfo)
			r.Post("/settings/notify/test", s.handleTestNotify)
			r.Post("/settings/feed-key", s.handleRotateFeedKey)
			r.Post("/settings/ytdlp/update", s.handleUpdateYtDlp)
			r.Get("/activity", s.handleActivity)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/backup", s.handleBackup)
			r.Get("/export.opml", s.handleExportOPML)
			r.Get("/export.json", s.handleExportJSON)
			r.Post("/import/opml", s.handleImportOPML)
			r.Post("/import", s.handleImportJSON)
		})
	})

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, "already there")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 16<<20))
	return dec.Decode(v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

func me(r *http.Request) int64 { return actorID(r.Context()) }

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// ---------- Pocket Core ----------

// coreLoop registers with Core on start (retrying until it answers) and
// re-registers every 30 minutes so version and last-seen stay fresh.
func (s *Server) coreLoop(ctx context.Context) {
	wait := 5 * time.Second
	for {
		err := s.registerWithCore(ctx)
		s.coreMu.Lock()
		s.coreOK, s.coreErr = err == nil, ""
		if err != nil {
			s.coreErr = err.Error()
		}
		s.coreMu.Unlock()
		if err != nil {
			s.log.Warn("pocket core: registration failed, retrying", "err", err, "in", wait.String())
		} else {
			wait = 30 * time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err != nil && wait < 5*time.Minute {
			wait *= 2
		}
	}
}

func (s *Server) registerWithCore(ctx context.Context) error {
	m := core.Manifest{
		Slug: "podcasts", Name: "Pocket Podcasts",
		Description: "A self-hosted podcast server: subscriptions, a player that remembers where you were, smart playlists, server-side downloads, YouTube channels as audio podcasts with SponsorBlock, and public feeds for any podcast app.",
		Icon:        "/favicon.svg", Version: Version,
		URL: s.cfg.CoreAppURL, PublicURL: s.cfg.BaseURL, HealthPath: "/healthz", EnvPrefix: "PODCASTS",
		Publishes: []string{"podcasts.subscribed", "podcasts.unsubscribed", "podcasts.episode_added", "podcasts.played", "podcasts.finished", "podcasts.downloaded"},
		Offers:    linkOffers,
	}
	if s.cfg.BaseURL != "" {
		m.OIDC = &core.OIDCRequest{RedirectURIs: []string{s.cfg.BaseURL + "/api/v1/auth/oidc/callback"}}
	}
	reg, err := s.core.Register(ctx, s.cfg.CoreRegistrationKey, m)
	if err != nil {
		return err
	}
	if reg.Token != "" {
		s.log.Info("pocket core: registered, app token stored", "core", s.cfg.CoreURL)
	} else {
		s.log.Debug("pocket core: re-registered", "core", s.cfg.CoreURL)
	}
	return nil
}

// publish sends an event to Core without blocking the request. The payload
// names whose subscription it happened on (user_id, user_email).
func (s *Server) publish(userID int64, topic, subject string, payload map[string]any) {
	if !s.core.Configured() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		payload["user_id"] = userID
		if u, err := s.store.GetUser(ctx, userID); err == nil {
			payload["user_email"] = u.Email
		}
		if err := s.core.Publish(ctx, topic, subject, payload); err != nil {
			s.log.Warn("pocket core: publish failed", "topic", topic, "err", err)
		}
	}()
}

// coreStatus summarises the Pocket Core link for the About page.
func (s *Server) coreStatus() map[string]any {
	s.coreMu.Lock()
	defer s.coreMu.Unlock()
	return map[string]any{"configured": s.core.Configured(), "url": s.cfg.CoreURL, "registered": s.coreOK, "error": s.coreErr}
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_podcasts_info Build info.\n# TYPE pocket_podcasts_info gauge\npocket_podcasts_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_podcasts_uptime_seconds Process uptime.\n# TYPE pocket_podcasts_uptime_seconds gauge\npocket_podcasts_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_podcasts_http_requests_total HTTP requests served.\n# TYPE pocket_podcasts_http_requests_total counter\npocket_podcasts_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_podcasts_http_errors_total HTTP 5xx responses.\n# TYPE pocket_podcasts_http_errors_total counter\npocket_podcasts_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_podcasts_rows Rows per table.\n# TYPE pocket_podcasts_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_podcasts_rows{table=%q} %d\n", k, v)
	}
	var queued, running, done int
	_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(CASE WHEN download='queued' THEN 1 END), COUNT(CASE WHEN download='downloading' THEN 1 END), COUNT(CASE WHEN download='done' THEN 1 END) FROM episodes`).Scan(&queued, &running, &done)
	fmt.Fprintf(w, "# HELP pocket_podcasts_downloads Episodes by download state, all users.\n# TYPE pocket_podcasts_downloads gauge\n")
	fmt.Fprintf(w, "pocket_podcasts_downloads{state=\"queued\"} %d\npocket_podcasts_downloads{state=\"downloading\"} %d\npocket_podcasts_downloads{state=\"done\"} %d\n", queued, running, done)
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/feed/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}

// timeoutExcept is chi's Timeout for every request except paths ending in
// one of the suffixes (a stream may wait for its file to be downloaded).
func timeoutExcept(d time.Duration, suffixes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		timed := middleware.Timeout(d)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, suf := range suffixes {
				if strings.HasSuffix(r.URL.Path, suf) {
					next.ServeHTTP(w, r)
					return
				}
			}
			timed.ServeHTTP(w, r)
		})
	}
}
