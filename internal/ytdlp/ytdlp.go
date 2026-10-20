// Package ytdlp drives yt-dlp: indexing a channel or playlist, downloading
// one video as audio with its metadata, chapters and SponsorBlock segments.
package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Runner runs yt-dlp.
type Runner struct {
	Path    string   // explicit executable; "" = python3 -m yt_dlp (SiteDir on PYTHONPATH) or yt-dlp on PATH
	SiteDir string   // where Update installs newer versions (pip --target)
	Cookies string   // cookies.txt (optional)
	Extra   []string // appended to every call
	Log     *slog.Logger
}

// Available reports whether yt-dlp can be run at all.
func (r *Runner) Available() bool {
	_, err := r.Version(context.Background())
	return err == nil
}

func (r *Runner) cmd(ctx context.Context, args ...string) *exec.Cmd {
	var c *exec.Cmd
	switch {
	case r.Path != "":
		c = exec.CommandContext(ctx, r.Path, args...)
	case r.SiteDir != "" && exists(filepath.Join(r.SiteDir, "yt_dlp")):
		c = exec.CommandContext(ctx, "python3", append([]string{"-m", "yt_dlp"}, args...)...)
		c.Env = append(os.Environ(), "PYTHONPATH="+r.SiteDir)
	default:
		if _, err := exec.LookPath("yt-dlp"); err == nil {
			c = exec.CommandContext(ctx, "yt-dlp", args...)
		} else {
			c = exec.CommandContext(ctx, "python3", append([]string{"-m", "yt_dlp"}, args...)...)
		}
	}
	if c.Env == nil {
		c.Env = os.Environ()
	}
	return c
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (r *Runner) common() []string {
	args := []string{"--no-warnings", "--no-progress", "--ignore-config", "--no-cache-dir", "--socket-timeout", "30", "--retries", "3"}
	// YouTube needs a JavaScript runtime; yt-dlp enables deno by itself, node only when asked
	if _, err := exec.LookPath("deno"); err != nil {
		if _, err := exec.LookPath("node"); err == nil {
			args = append(args, "--js-runtimes", "node")
		}
	}
	if r.Cookies != "" && exists(r.Cookies) {
		args = append(args, "--cookies", r.Cookies)
	}
	return append(args, r.Extra...)
}

// Version returns yt-dlp's version string.
func (r *Runner) Version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := r.cmd(ctx, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("yt-dlp not available: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Update installs the newest yt-dlp into SiteDir with pip (no rebuild needed).
func (r *Runner) Update(ctx context.Context) (string, error) {
	if r.SiteDir == "" {
		return "", errors.New("no site directory configured")
	}
	if err := os.MkdirAll(r.SiteDir, 0o755); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, "python3", "-m", "pip", "install", "--quiet", "--upgrade", "--target", r.SiteDir, "--break-system-packages", "yt-dlp[default]")
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("pip: %s", strings.TrimSpace(lastLines(stderr.String(), 3)))
	}
	return r.Version(ctx)
}

// Kind of a YouTube URL.
const (
	KindChannel  = "channel"
	KindPlaylist = "playlist"
	KindVideo    = "video"
)

var (
	reVideo   = regexp.MustCompile(`(?i)(?:youtube\.com/(?:watch\?(?:.*&)?v=|shorts/|live/|embed/)|youtu\.be/)([A-Za-z0-9_-]{11})`)
	reList    = regexp.MustCompile(`(?i)[?&]list=([A-Za-z0-9_-]+)`)
	reChannel = regexp.MustCompile(`(?i)youtube\.com/(channel/(UC[A-Za-z0-9_-]+)|(@[A-Za-z0-9_.-]+)|c/([A-Za-z0-9_.-]+)|user/([A-Za-z0-9_.-]+))`)
)

// Classify tells what a YouTube URL points at and normalises it.
// Channels become their /videos tab so an index lists uploads, not tabs.
func Classify(raw string) (kind, normalized, id string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", errors.New("empty URL")
	}
	if !strings.Contains(raw, "://") {
		if strings.HasPrefix(raw, "@") {
			raw = "https://www.youtube.com/" + raw
		} else if strings.HasPrefix(raw, "UC") && len(raw) == 24 {
			raw = "https://www.youtube.com/channel/" + raw
		} else if strings.HasPrefix(raw, "PL") || strings.HasPrefix(raw, "UU") || strings.HasPrefix(raw, "OL") {
			raw = "https://www.youtube.com/playlist?list=" + raw
		} else {
			raw = "https://" + raw
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", "", err
	}
	host := strings.ToLower(u.Host)
	if !strings.Contains(host, "youtube.com") && !strings.Contains(host, "youtu.be") {
		return "", "", "", errors.New("not a YouTube URL")
	}
	if m := reList.FindStringSubmatch(raw); m != nil && !strings.Contains(u.Path, "/watch") {
		return KindPlaylist, "https://www.youtube.com/playlist?list=" + m[1], m[1], nil
	}
	if m := reVideo.FindStringSubmatch(raw); m != nil {
		return KindVideo, "https://www.youtube.com/watch?v=" + m[1], m[1], nil
	}
	if m := reList.FindStringSubmatch(raw); m != nil {
		return KindPlaylist, "https://www.youtube.com/playlist?list=" + m[1], m[1], nil
	}
	if m := reChannel.FindStringSubmatch(raw); m != nil {
		base := "https://www.youtube.com/" + strings.TrimSuffix(m[1], "/")
		id := m[2]
		return KindChannel, base + "/videos", id, nil
	}
	return "", "", "", errors.New("unrecognised YouTube URL (use a channel, playlist or video link)")
}

// Entry is one video in a listing.
type Entry struct {
	ID          string
	Title       string
	URL         string
	Duration    int
	UploadDate  string // YYYYMMDD when known
	Timestamp   int64
	Thumbnail   string
	Live        bool
	MembersOnly bool // only the channel's paying members can get it
}

// Listing is a channel or playlist with its newest entries.
type Listing struct {
	ID          string // channel id (UC…) or playlist id
	Title       string
	Uploader    string
	Description string
	Thumbnail   string
	URL         string
	Entries     []Entry
}

type rawListing struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Channel     string     `json:"channel"`
	ChannelID   string     `json:"channel_id"`
	Uploader    string     `json:"uploader"`
	UploaderID  string     `json:"uploader_id"`
	PlaylistID  string     `json:"playlist_id"`
	Description string     `json:"description"`
	WebpageURL  string     `json:"webpage_url"`
	Thumbnails  []rawThumb `json:"thumbnails"`
	Entries     []struct {
		ID           string     `json:"id"`
		Title        string     `json:"title"`
		URL          string     `json:"url"`
		Duration     float64    `json:"duration"`
		UploadDate   string     `json:"upload_date"`
		Timestamp    int64      `json:"timestamp"`
		Thumbnails   []rawThumb `json:"thumbnails"`
		LiveStatus   string     `json:"live_status"`
		Availability string     `json:"availability"`
	} `json:"entries"`
}

type rawThumb struct {
	URL    string `json:"url"`
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func bestThumb(th []rawThumb, prefer string) string {
	best := ""
	bw := -1
	for _, t := range th {
		if prefer != "" && !strings.Contains(t.ID, prefer) {
			continue
		}
		if t.Width > bw {
			best, bw = t.URL, t.Width
		}
	}
	if best == "" && prefer != "" {
		return bestThumb(th, "")
	}
	return best
}

// Index lists the newest limit entries of a channel or playlist without
// visiting each video (fast: one request).
func (r *Runner) Index(ctx context.Context, u string, limit int) (*Listing, error) {
	return r.IndexPage(ctx, u, 0, limit)
}

// IndexPage lists count entries starting after the first offset ones (for
// paging through a long channel or playlist).
func (r *Runner) IndexPage(ctx context.Context, u string, offset, count int) (*Listing, error) {
	if count <= 0 {
		count = 30
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	args := append(r.common(), "--flat-playlist", "-J", "--playlist-start", fmt.Sprint(offset+1), "--playlist-end", fmt.Sprint(offset+count), "--extractor-args", "youtubetab:approximate_date", u)
	c := r.cmd(ctx, args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp: %s", friendly(stderr.String(), err))
	}
	var raw rawListing
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("yt-dlp: unreadable listing: %w", err)
	}
	l := &Listing{ID: firstNonEmpty(raw.ChannelID, raw.PlaylistID, raw.ID), Title: strings.TrimSuffix(raw.Title, " - Videos"), Uploader: firstNonEmpty(raw.Channel, raw.Uploader),
		Description: raw.Description, Thumbnail: bestThumb(raw.Thumbnails, "avatar"), URL: raw.WebpageURL}
	if strings.HasPrefix(raw.ID, "PL") || strings.HasPrefix(raw.ID, "UU") || strings.HasPrefix(raw.ID, "OL") {
		l.ID = raw.ID
		l.Thumbnail = bestThumb(raw.Thumbnails, "")
	}
	for i, e := range raw.Entries {
		if e.ID == "" {
			continue
		}
		en := Entry{ID: e.ID, Title: e.Title, URL: firstNonEmpty(e.URL, "https://www.youtube.com/watch?v="+e.ID), Duration: int(e.Duration), UploadDate: e.UploadDate, Timestamp: e.Timestamp,
			Thumbnail: bestThumb(e.Thumbnails, ""), Live: e.LiveStatus == "is_live" || e.LiveStatus == "is_upcoming",
			MembersOnly: e.Availability == "subscriber_only" || e.Availability == "premium_only" || e.Availability == "needs_auth"}
		if en.Thumbnail == "" {
			en.Thumbnail = "https://i.ytimg.com/vi/" + e.ID + "/hqdefault.jpg"
		}
		if l.Thumbnail == "" && i == 0 {
			l.Thumbnail = en.Thumbnail
		}
		l.Entries = append(l.Entries, en)
	}
	return l, nil
}

// Chapter and Segment mirror the store's JSON shapes.
type Chapter struct {
	Start float64 `json:"start"`
	End   float64 `json:"end,omitempty"`
	Title string  `json:"title"`
}

// Segment is a SponsorBlock segment.
type Segment struct {
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Category string  `json:"category"`
}

// Info is what a download learned about the video.
type Info struct {
	ID          string
	Title       string
	Description string
	Channel     string
	ChannelID   string
	Thumbnail   string
	WebpageURL  string
	Duration    int
	UploadDate  time.Time
	Chapters    []Chapter
	Sponsors    []Segment
}

// Download is a finished audio download.
type Download struct {
	File      string // absolute path of the audio file
	Thumbnail string // absolute path of the jpg, "" when none
	Bytes     int64
	Info      Info
}

// DownloadAudio fetches one video as m4a into dir using base as the file
// name (without extension). With sponsorblock the SponsorBlock segments are
// recorded too (they are not cut out of the file; the player skips them).
func (r *Runner) DownloadAudio(ctx context.Context, videoURL, dir, base string, sponsorblock bool) (*Download, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()
	tmpl := filepath.Join(dir, base+".%(ext)s")
	args := append(r.common(), "--no-playlist", "--newline",
		"-f", "bestaudio[ext=m4a]/bestaudio/best", "-x", "--audio-format", "m4a", "--audio-quality", "0",
		"--embed-metadata", "--write-info-json", "--write-thumbnail", "--convert-thumbnails", "jpg",
		"--print", "after_move:filepath", "-o", tmpl)
	if sponsorblock {
		args = append(args, "--sponsorblock-mark", "all")
	}
	args = append(args, videoURL)
	c := r.cmd(ctx, args...)
	var stderr, stdout bytes.Buffer
	c.Stderr, c.Stdout = &stderr, &stdout
	err := c.Run()
	infoPath := filepath.Join(dir, base+".info.json")
	defer os.Remove(infoPath)
	if err != nil {
		cleanupPartials(dir, base)
		return nil, fmt.Errorf("yt-dlp: %s", friendly(stderr.String(), err))
	}
	file := strings.TrimSpace(lastLines(stdout.String(), 1))
	if file == "" || !exists(file) {
		// fall back to the expected name
		file = filepath.Join(dir, base+".m4a")
		if !exists(file) {
			return nil, errors.New("yt-dlp finished but no audio file appeared")
		}
	}
	d := &Download{File: file}
	if fi, err := os.Stat(file); err == nil {
		d.Bytes = fi.Size()
	}
	if th := filepath.Join(dir, base+".jpg"); exists(th) {
		d.Thumbnail = th
	}
	if b, err := os.ReadFile(infoPath); err == nil {
		d.Info = parseInfo(b)
	}
	return d, nil
}

func cleanupPartials(dir, base string) {
	m, _ := filepath.Glob(filepath.Join(dir, base+".*"))
	for _, f := range m {
		if strings.Contains(f, ".part") || strings.HasSuffix(f, ".ytdl") || strings.HasSuffix(f, ".webp") || strings.HasSuffix(f, ".info.json") {
			_ = os.Remove(f)
		}
	}
}

func parseInfo(b []byte) Info {
	var raw struct {
		ID          string  `json:"id"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Channel     string  `json:"channel"`
		ChannelID   string  `json:"channel_id"`
		Thumbnail   string  `json:"thumbnail"`
		WebpageURL  string  `json:"webpage_url"`
		Duration    float64 `json:"duration"`
		UploadDate  string  `json:"upload_date"`
		Timestamp   int64   `json:"timestamp"`
		ReleaseTS   int64   `json:"release_timestamp"`
		Chapters    []struct {
			Start float64 `json:"start_time"`
			End   float64 `json:"end_time"`
			Title string  `json:"title"`
		} `json:"chapters"`
		SB []struct {
			Start    float64 `json:"start_time"`
			End      float64 `json:"end_time"`
			Category string  `json:"category"`
		} `json:"sponsorblock_chapters"`
	}
	_ = json.Unmarshal(b, &raw)
	in := Info{ID: raw.ID, Title: raw.Title, Description: raw.Description, Channel: raw.Channel, ChannelID: raw.ChannelID, Thumbnail: raw.Thumbnail, WebpageURL: raw.WebpageURL, Duration: int(raw.Duration)}
	switch {
	case raw.ReleaseTS > 0:
		in.UploadDate = time.Unix(raw.ReleaseTS, 0).UTC()
	case raw.Timestamp > 0:
		in.UploadDate = time.Unix(raw.Timestamp, 0).UTC()
	case len(raw.UploadDate) == 8:
		in.UploadDate, _ = time.Parse("20060102", raw.UploadDate)
	}
	for _, c := range raw.Chapters {
		in.Chapters = append(in.Chapters, Chapter{Start: c.Start, End: c.End, Title: c.Title})
	}
	for _, s := range raw.SB {
		in.Sponsors = append(in.Sponsors, Segment{Start: s.Start, End: s.End, Category: s.Category})
	}
	return in
}

// Channel is a YouTube channel found by a search.
type Channel struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Handle      string `json:"handle"`
	URL         string `json:"url"`
	Followers   int64  `json:"followers"`
	Description string `json:"description"`
	Thumbnail   string `json:"thumbnail"`
}

var avatarSize = regexp.MustCompile(`=s\d+-`)

// SearchResult is a video or playlist found by a search.
type SearchResult struct {
	Kind        string `json:"kind"` // video | playlist
	ID          string `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Channel     string `json:"channel"`
	ChannelID   string `json:"channel_id"`
	Duration    int    `json:"duration,omitempty"`
	Views       int64  `json:"views,omitempty"`
	Description string `json:"description,omitempty"`
	Thumbnail   string `json:"thumbnail"`
	MembersOnly bool   `json:"members_only,omitempty"`
	Live        bool   `json:"live,omitempty"`
}

type rawSearchEntry struct {
	ID           string     `json:"id"`
	URL          string     `json:"url"`
	Title        string     `json:"title"`
	Channel      string     `json:"channel"`
	ChannelID    string     `json:"channel_id"`
	ChannelURL   string     `json:"channel_url"`
	Uploader     string     `json:"uploader"`
	UploaderID   string     `json:"uploader_id"`
	Followers    int64      `json:"channel_follower_count"`
	Duration     float64    `json:"duration"`
	Views        int64      `json:"view_count"`
	Description  string     `json:"description"`
	Availability string     `json:"availability"`
	LiveStatus   string     `json:"live_status"`
	Thumbnails   []rawThumb `json:"thumbnails"`
}

// YouTube's own search filters ("type: channel / video / playlist").
var searchFilter = map[string]string{"channel": "EgIQAg%253D%253D", "video": "EgIQAQ%253D%253D", "playlist": "EgIQAw%253D%253D"}

func (r *Runner) search(ctx context.Context, q, kind string, limit int) ([]rawSearchEntry, error) {
	if limit <= 0 {
		limit = 12
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	u := "https://www.youtube.com/results?search_query=" + url.QueryEscape(q) + "&sp=" + searchFilter[kind]
	args := append(r.common(), "--flat-playlist", "-J", "--playlist-end", fmt.Sprint(limit), u)
	c := r.cmd(ctx, args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp: %s", friendly(stderr.String(), err))
	}
	var raw struct {
		Entries []rawSearchEntry `json:"entries"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("yt-dlp: unreadable search results: %w", err)
	}
	return raw.Entries, nil
}

func thumbURL(th []rawThumb) string {
	t := bestThumb(th, "")
	if strings.HasPrefix(t, "//") {
		t = "https:" + t
	}
	return t
}

// SearchChannels asks YouTube's search for channels (one request, ~1 s).
func (r *Runner) SearchChannels(ctx context.Context, q string, limit int) ([]Channel, error) {
	entries, err := r.search(ctx, q, "channel", limit)
	if err != nil {
		return nil, err
	}
	res := []Channel{}
	for _, e := range entries {
		id := firstNonEmpty(e.ChannelID, e.ID)
		if !strings.HasPrefix(id, "UC") {
			continue
		}
		ch := Channel{ID: id, Title: firstNonEmpty(e.Title, e.Channel), Handle: e.UploaderID, Followers: e.Followers, Description: e.Description,
			URL: firstNonEmpty(e.ChannelURL, "https://www.youtube.com/channel/"+id)}
		if t := thumbURL(e.Thumbnails); t != "" {
			ch.Thumbnail = avatarSize.ReplaceAllString(t, "=s240-")
		}
		res = append(res, ch)
	}
	return res, nil
}

// SearchVideos asks YouTube's search for videos.
func (r *Runner) SearchVideos(ctx context.Context, q string, limit int) ([]SearchResult, error) {
	entries, err := r.search(ctx, q, "video", limit)
	if err != nil {
		return nil, err
	}
	res := []SearchResult{}
	for _, e := range entries {
		if len(e.ID) != 11 {
			continue
		}
		v := SearchResult{Kind: KindVideo, ID: e.ID, Title: e.Title, URL: "https://www.youtube.com/watch?v=" + e.ID, Channel: firstNonEmpty(e.Channel, e.Uploader),
			ChannelID: e.ChannelID, Duration: int(e.Duration), Views: e.Views, Description: e.Description, Thumbnail: thumbURL(e.Thumbnails),
			MembersOnly: e.Availability == "subscriber_only" || e.Availability == "premium_only", Live: e.LiveStatus == "is_live" || e.LiveStatus == "is_upcoming"}
		if v.Thumbnail == "" {
			v.Thumbnail = "https://i.ytimg.com/vi/" + e.ID + "/hqdefault.jpg"
		}
		res = append(res, v)
	}
	return res, nil
}

// SearchPlaylists asks YouTube's search for playlists.
func (r *Runner) SearchPlaylists(ctx context.Context, q string, limit int) ([]SearchResult, error) {
	entries, err := r.search(ctx, q, "playlist", limit)
	if err != nil {
		return nil, err
	}
	res := []SearchResult{}
	for _, e := range entries {
		if e.ID == "" {
			continue
		}
		res = append(res, SearchResult{Kind: KindPlaylist, ID: e.ID, Title: e.Title, URL: "https://www.youtube.com/playlist?list=" + e.ID,
			Channel: firstNonEmpty(e.Channel, e.Uploader), ChannelID: e.ChannelID, Thumbnail: thumbURL(e.Thumbnails)})
	}
	return res, nil
}

// VideoInfo fetches one video's metadata without downloading.
func (r *Runner) VideoInfo(ctx context.Context, videoURL string) (*Info, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	args := append(r.common(), "--no-playlist", "-J", "--skip-download", videoURL)
	c := r.cmd(ctx, args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("yt-dlp: %s", friendly(stderr.String(), err))
	}
	in := parseInfo(out)
	return &in, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// friendly turns yt-dlp's stderr into one readable line.
func friendly(stderr string, err error) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ERROR:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "ERROR:"))
			if len(line) > 300 {
				line = line[:300]
			}
			return line
		}
	}
	if l := lastLines(stderr, 1); l != "" {
		if len(l) > 300 {
			l = l[:300]
		}
		return l
	}
	return err.Error()
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// RSSURL is YouTube's own feed for a source: for a channel its "full-length
// uploads" list (UULF…: no Shorts, no members-only videos), for a playlist
// the playlist. "" when the id is neither.
func RSSURL(kind, id string) string {
	switch {
	case kind == KindChannel && strings.HasPrefix(id, "UC") && len(id) > 2:
		return "https://www.youtube.com/feeds/videos.xml?playlist_id=UULF" + id[2:]
	case kind == KindPlaylist && id != "":
		return "https://www.youtube.com/feeds/videos.xml?playlist_id=" + id
	}
	return ""
}

// HasCookies reports whether a cookies file is configured: with a member's
// cookies members-only videos can be downloaded.
func (r *Runner) HasCookies() bool { return r.Cookies != "" && exists(r.Cookies) }
