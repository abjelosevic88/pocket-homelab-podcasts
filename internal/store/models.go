package store

// User is an account that can sign in. The first user is the admin; admins
// manage other users. Every user has their own subscriptions.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Podcast kinds.
const (
	KindRSS     = "rss"
	KindYouTube = "youtube"
)

// Download states of an episode's file.
const (
	DownloadNone        = "none"
	DownloadQueued      = "queued"
	DownloadDownloading = "downloading"
	DownloadDone        = "done"
	DownloadFailed      = "failed"
	DownloadCulled      = "culled"
)

// Podcast is one subscription of one person.
type Podcast struct {
	ID             int64   `json:"id"`
	UserID         int64   `json:"user_id"`
	Kind           string  `json:"kind"`
	Title          string  `json:"title"`
	FeedURL        string  `json:"feed_url"`
	SiteURL        string  `json:"site_url"`
	YouTubeID      string  `json:"youtube_id"`
	YouTubeKind    string  `json:"youtube_kind"`
	Author         string  `json:"author"`
	Description    string  `json:"description"`
	ArtworkURL     string  `json:"artwork_url"`
	ArtworkFile    string  `json:"artwork_file"`
	Language       string  `json:"language"`
	Categories     string  `json:"categories"`
	Explicit       bool    `json:"explicit"`
	Favorite       bool    `json:"favorite"`
	PlaybackSpeed  float64 `json:"playback_speed"`
	StartSkip      int     `json:"start_skip"`
	EndSkip        int     `json:"end_skip"`
	AutoPlayNext   int     `json:"auto_play_next"`
	Notify         bool    `json:"notify"`
	AutoDownload   bool    `json:"auto_download"`
	DownloadLimit  int     `json:"download_limit"`
	RetentionDays  int     `json:"retention_days"`
	MinDuration    int     `json:"min_duration"`
	TitleFilter    string  `json:"title_filter"`
	FeedCutoffDays int     `json:"feed_cutoff_days"`
	PubDateMode    string  `json:"pub_date_mode"`
	SponsorBlock   bool    `json:"sponsorblock"`
	RefreshMinutes int     `json:"refresh_minutes"`
	ETag           string  `json:"-"`
	LastModified   string  `json:"-"`
	LastRefreshAt  string  `json:"last_refresh_at"`
	LastRefreshOK  string  `json:"last_refresh_ok"`
	LastError      string  `json:"last_error"`
	Failures       int     `json:"failures"`
	SubscribedAt   string  `json:"subscribed_at"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`

	// Derived, read-only.
	EpisodeCount    int    `json:"episode_count"`
	UnplayedCount   int    `json:"unplayed_count"`
	DownloadedCount int    `json:"downloaded_count"`
	DownloadedBytes int64  `json:"downloaded_bytes"`
	LatestPubDate   string `json:"latest_pub_date"`
}

// Chapter is a point in an episode (podcast 2.0 chapters, YouTube chapters).
type Chapter struct {
	Start float64 `json:"start"`
	End   float64 `json:"end,omitempty"`
	Title string  `json:"title"`
	URL   string  `json:"url,omitempty"`
	Image string  `json:"img,omitempty"`
}

// Segment is a SponsorBlock segment the player may skip.
type Segment struct {
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Category string  `json:"category"`
}

// Episode is one episode of a subscription; the progress fields are the
// caller's own when loaded with a user id.
type Episode struct {
	ID             int64  `json:"id"`
	PodcastID      int64  `json:"podcast_id"`
	GUID           string `json:"guid"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Link           string `json:"link"`
	EnclosureURL   string `json:"enclosure_url"`
	EnclosureType  string `json:"enclosure_type"`
	EnclosureBytes int64  `json:"enclosure_bytes"`
	Duration       int    `json:"duration"`
	PubDate        string `json:"pub_date"`
	ArtworkURL     string `json:"artwork_url"`
	Season         int    `json:"season"`
	EpisodeNo      int    `json:"episode_no"`
	EpisodeType    string `json:"episode_type"`
	Explicit       bool   `json:"explicit"`
	IsVideo        bool   `json:"is_video"`
	ChaptersURL    string `json:"chapters_url"`
	TranscriptURL  string `json:"transcript_url"`
	ChaptersJSON   string `json:"-"`
	SponsorsJSON   string `json:"-"`
	YouTubeID      string `json:"youtube_id"`
	Position       int    `json:"position"`
	FilePath       string `json:"-"`
	FileBytes      int64  `json:"file_bytes"`
	FileType       string `json:"file_type"`
	DownloadedAt   string `json:"downloaded_at"`
	Download       string `json:"download"`
	DownloadError  string `json:"download_error"`
	DownloadTries  int    `json:"-"`
	CreatedAt      string `json:"created_at"`

	// Joined, read-only.
	PodcastTitle   string  `json:"podcast_title"`
	PodcastKind    string  `json:"podcast_kind"`
	PodcastArtwork string  `json:"podcast_artwork"`
	PlaybackSpeed  float64 `json:"playback_speed"`
	StartSkip      int     `json:"start_skip"`
	EndSkip        int     `json:"end_skip"`
	// The caller's own state.
	PlayPosition int    `json:"play_position"`
	Completed    bool   `json:"completed"`
	LastPlayedAt string `json:"last_played_at"`
	PlayCount    int    `json:"play_count"`
	Saved        bool   `json:"saved"`
	QueuePos     int    `json:"queue_pos"` // 0 = not queued
	HasChapters  bool   `json:"has_chapters"`
	HasSponsors  bool   `json:"has_sponsors"`
	// Where the browser plays it from (local file or the original enclosure).
	StreamURL string `json:"stream_url"`
}

// Playlist is a smart list over a person's episodes.
type Playlist struct {
	ID              int64   `json:"id"`
	UserID          int64   `json:"user_id"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Icon            string  `json:"icon"`
	PodcastIDs      []int64 `json:"podcast_ids"`
	IncludeUnplayed bool    `json:"include_unplayed"`
	IncludePartial  bool    `json:"include_partial"`
	IncludePlayed   bool    `json:"include_played"`
	MinDuration     int     `json:"min_duration"`
	MaxDuration     int     `json:"max_duration"`
	MaxAgeDays      int     `json:"max_age_days"`
	DownloadedOnly  bool    `json:"downloaded_only"`
	Sort            string  `json:"sort"`
	Limit           int     `json:"limit"`
	Pos             int     `json:"pos"`
	CreatedAt       string  `json:"created_at"`
	Count           int     `json:"count"`
}

// Activity is one line of what the app did on its own.
type Activity struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	PodcastID int64  `json:"podcast_id"`
	EpisodeID int64  `json:"episode_id"`
	CreatedAt string `json:"created_at"`
}

// Settings are what the UI reads as one object: the box title (shared,
// admin-only to change) plus the preferences of the signed-in person.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`
	Landing       string `json:"landing"` // home | podcasts | queue

	// Playback
	PlaybackSpeed    float64 `json:"playback_speed"`
	SkipForward      int     `json:"skip_forward"`
	SkipBack         int     `json:"skip_back"`
	AutoPlayNext     bool    `json:"auto_play_next"`
	CompleteAt       int     `json:"complete_at"` // percent listened that marks an episode played
	AutoSkipSponsors bool    `json:"auto_skip_sponsors"`
	SkipCategories   string  `json:"skip_categories"` // comma separated SponsorBlock categories
	QueueNext        bool    `json:"queue_next"`      // when an episode ends, play the next queued one

	// Feeds and downloads
	RefreshMinutes      int  `json:"refresh_minutes"` // 0 = box default
	AutoDownloadDefault bool `json:"auto_download_default"`
	RetentionDays       int  `json:"retention_days"` // default for new subscriptions
	YouTubeLimit        int  `json:"youtube_limit"`  // default download_limit for new YouTube subscriptions
	YouTubeMinDuration  int  `json:"youtube_min_duration"`
	KeepPlayed          bool `json:"keep_played"` // keep files of played episodes until retention

	// Search
	SearchSource       string `json:"search_source"` // all | itunes | podcastindex
	PodcastIndexKey    string `json:"podcastindex_key"`
	PodcastIndexSecret string `json:"podcastindex_secret"`

	// Notifications
	NotifyKind  string `json:"notify_kind"` // apprise | ntfy | webhook
	NotifyURL   string `json:"notify_url"`
	NotifyToken string `json:"notify_token"`

	// Public feeds (Apple Podcasts, Overcast, …)
	FeedKey     string `json:"feed_key"`
	FeedBaseURL string `json:"feed_base_url"`
}

// Dashboard is the compact status used by widgets.
type Dashboard struct {
	Podcasts      int          `json:"podcasts"`
	Unplayed      int          `json:"unplayed"`
	Queued        int          `json:"queued"`
	Downloaded    int          `json:"downloaded"`
	DownloadedMB  int          `json:"downloaded_mb"`
	ListenedToday int          `json:"listened_today_min"`
	ListenedWeek  int          `json:"listened_week_min"`
	NowPlaying    *EpisodeLine `json:"now_playing"`
	NextUp        *EpisodeLine `json:"next_up"`
	NewToday      int          `json:"new_today"`
	Pending       int          `json:"downloads_pending"`
}

// EpisodeLine is the short form of an episode for widgets.
type EpisodeLine struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Podcast  string `json:"podcast"`
	Progress int    `json:"progress"` // percent
	Date     string `json:"date,omitempty"`
}
