// Package config loads runtime configuration from environment variables.
//
// Every option has a sensible default so that `docker run` with a single
// volume works out of the box. See docs/configuration.md for the full list.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	Host           string
	Port           int
	BaseURL        string
	DataDir        string
	DBPath         string
	MediaDir       string // downloaded episodes
	ArtworkDir     string // cached and custom artwork
	LogLevel       string
	LogFormat      string // text | json
	TrustProxy     bool
	MetricsEnabled bool

	// Feeds
	RefreshMinutes     int           // default refresh interval for a subscription
	FeedTimeout        time.Duration // one HTTP fetch of a feed or enclosure
	DownloadWorkers    int           // parallel downloads
	MaxEpisodesPerFeed int           // keep at most this many episodes per RSS feed in the DB (0 = all)

	// Search
	PodcastIndexKey    string
	PodcastIndexSecret string

	// YouTube
	YtDlpPath           string // yt-dlp executable (default: "yt-dlp" on PATH, else python3 -m yt_dlp)
	YtDlpExtra          string // extra arguments appended to every yt-dlp call (space separated)
	YtDlpCookies        string // path of a cookies.txt file (optional)
	YouTubeIndexLimit   int    // how many of a channel's newest videos to index per refresh
	YouTubeCheckMinutes int    // quick RSS check for new uploads (0 = off)

	AuthMode             string // local | proxy | none
	AuthProxyHeader      string
	AuthProxyEmailHeader string
	SessionTTL           time.Duration
	SecureCookies        bool
	AdminEmail           string
	AdminPassword        string
	AdminName            string

	// Sign in with an OpenID Connect provider. On a box with Pocket Core
	// none of these are needed: the app uses the client Core issued.
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCName         string // button label
	OIDCAutoCreate   bool   // unknown (verified) emails get an account

	// Pocket Core: when CORE_URL is set the app registers itself on start,
	// publishes events and accepts the tokens Core hands to paired phones.
	CoreURL             string
	CoreRegistrationKey string
	CoreAppURL          string // how Core reaches this app; defaults to BASE_URL

	// Notifications for new episodes (the person can also set these in Settings).
	NotifyURL  string // Apprise route, ntfy topic URL or webhook
	NotifyKind string // apprise | ntfy | webhook

	DemoData bool
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(env(key, "")) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func envInt(key string, def int) int {
	if v := env(key, ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := env(key, ""); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	dataDir := env("DATA_DIR", "./data")
	c := &Config{
		Host:           env("HOST", "0.0.0.0"),
		Port:           envInt("PORT", 8080),
		BaseURL:        strings.TrimRight(env("BASE_URL", ""), "/"),
		DataDir:        dataDir,
		DBPath:         env("DB_PATH", dataDir+"/pocket-podcasts.db"),
		MediaDir:       env("MEDIA_DIR", dataDir+"/media"),
		ArtworkDir:     env("ARTWORK_DIR", dataDir+"/artwork"),
		LogLevel:       strings.ToLower(env("LOG_LEVEL", "info")),
		LogFormat:      strings.ToLower(env("LOG_FORMAT", "text")),
		TrustProxy:     envBool("TRUST_PROXY", true),
		MetricsEnabled: envBool("METRICS_ENABLED", true),

		RefreshMinutes:     envInt("REFRESH_MINUTES", 60),
		FeedTimeout:        envDuration("FEED_TIMEOUT", 45*time.Second),
		DownloadWorkers:    envInt("DOWNLOAD_WORKERS", 2),
		MaxEpisodesPerFeed: envInt("MAX_EPISODES_PER_FEED", 0),

		PodcastIndexKey:    env("PODCASTINDEX_KEY", ""),
		PodcastIndexSecret: env("PODCASTINDEX_SECRET", ""),

		YtDlpPath:           env("YTDLP_PATH", ""),
		YtDlpExtra:          env("YTDLP_EXTRA_ARGS", ""),
		YtDlpCookies:        env("YTDLP_COOKIES", ""),
		YouTubeIndexLimit:   envInt("YOUTUBE_INDEX_LIMIT", 30),
		YouTubeCheckMinutes: envInt("YOUTUBE_CHECK_MINUTES", 10),

		AuthMode:             strings.ToLower(env("AUTH_MODE", "local")),
		AuthProxyHeader:      env("AUTH_PROXY_HEADER", "Remote-User"),
		AuthProxyEmailHeader: env("AUTH_PROXY_EMAIL_HEADER", "Remote-Email"),
		SessionTTL:           envDuration("SESSION_TTL", 720*time.Hour),
		SecureCookies:        envBool("SECURE_COOKIES", false),
		AdminEmail:           strings.ToLower(env("ADMIN_EMAIL", "")),
		AdminPassword:        env("ADMIN_PASSWORD", ""),
		AdminName:            env("ADMIN_NAME", "Admin"),

		OIDCIssuer:       strings.TrimRight(env("OIDC_ISSUER", ""), "/"),
		OIDCClientID:     env("OIDC_CLIENT_ID", ""),
		OIDCClientSecret: env("OIDC_CLIENT_SECRET", ""),
		OIDCName:         env("OIDC_NAME", "single sign-on"),
		OIDCAutoCreate:   envBool("OIDC_AUTO_CREATE", true),

		CoreURL:             strings.TrimRight(env("CORE_URL", ""), "/"),
		CoreRegistrationKey: env("CORE_REGISTRATION_KEY", ""),
		CoreAppURL:          strings.TrimRight(env("CORE_APP_URL", ""), "/"),

		NotifyURL:  env("NOTIFY_URL", ""),
		NotifyKind: strings.ToLower(env("NOTIFY_KIND", "apprise")),

		DemoData: envBool("DEMO_DATA", false),
	}
	if c.CoreAppURL == "" {
		c.CoreAppURL = c.BaseURL
	}
	switch c.AuthMode {
	case "local", "proxy", "none":
	default:
		return nil, fmt.Errorf("AUTH_MODE must be one of local, proxy, none (got %q)", c.AuthMode)
	}
	if c.Port <= 0 || c.Port > 65535 {
		return nil, fmt.Errorf("PORT out of range: %d", c.Port)
	}
	if c.OIDCIssuer != "" && (c.OIDCClientID == "" || c.BaseURL == "") {
		return nil, fmt.Errorf("OIDC_ISSUER needs OIDC_CLIENT_ID and BASE_URL (the callback is BASE_URL/api/v1/auth/oidc/callback)")
	}
	if c.RefreshMinutes < 5 {
		c.RefreshMinutes = 5
	}
	if c.DownloadWorkers < 1 {
		c.DownloadWorkers = 1
	}
	if c.YouTubeIndexLimit < 5 {
		c.YouTubeIndexLimit = 5
	}
	return c, nil
}

// Addr returns host:port.
func (c *Config) Addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }
