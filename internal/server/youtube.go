package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/feed"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/ytdlp"
)

// PicksFeedURL marks the per-person "Picks" subscription: single episodes
// of any feed and single YouTube videos added by hand land there, without
// following the whole show. It is never refreshed.
const PicksFeedURL = "pocket:picks"

func (s *Server) picksPodcast(ctx context.Context, uid int64) (*store.Podcast, error) {
	if p, err := s.store.FindPodcastByFeed(ctx, uid, PicksFeedURL); err == nil {
		return p, nil
	}
	st, _ := s.store.GetSettings(ctx, uid)
	p := &store.Podcast{UserID: uid, Kind: store.KindRSS, Title: "Picks", FeedURL: PicksFeedURL, YouTubeKind: "picks",
		Description: "Single episodes and videos you picked by hand, from shows you do not follow.", AutoPlayNext: -1, RetentionDays: st.RetentionDays, PubDateMode: "downloaded", SponsorBlock: true, AutoDownload: true,
		LastRefreshOK: store.Now()}
	if err := s.store.CreatePodcast(ctx, p); err != nil {
		return nil, err
	}
	_ = s.store.UpdateFeedState(ctx, p.ID, "", "", nil)
	return s.store.GetPodcast(ctx, uid, p.ID)
}

// handleAddVideo downloads one YouTube video as an episode of "Picks".
func (s *Server) handleAddVideo(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.URL) == "" {
		writeErr(w, http.StatusBadRequest, "url is required")
		return
	}
	kind, norm, id, err := ytdlp.Classify(in.URL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if kind != ytdlp.KindVideo {
		writeErr(w, http.StatusBadRequest, "that is a channel or playlist: subscribe to it instead")
		return
	}
	if !s.ytAvailable() {
		writeErr(w, http.StatusBadRequest, "yt-dlp is not available on this box")
		return
	}
	ctx := r.Context()
	p, err := s.picksPodcast(ctx, me(r))
	if err != nil {
		s.fail(w, err, "picks")
		return
	}
	info, err := s.yt.VideoInfo(ctx, norm)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "channel's members") {
			writeErr(w, http.StatusBadRequest, "this is a members-only video: only the channel's paying members can get it (see docs/youtube.md for using your member login)")
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	e := store.Episode{PodcastID: p.ID, GUID: "yt:" + id, Title: info.Title, Description: info.Description, Link: norm, EnclosureType: "audio/mp4",
		Duration: info.Duration, PubDate: store.FormatTime(info.UploadDate), ArtworkURL: info.Thumbnail, YouTubeID: id}
	if _, err := s.store.UpsertEpisode(ctx, &e); err != nil {
		s.fail(w, err, "add video")
		return
	}
	if info.ChannelID != "" {
		_ = s.store.SetEpisodeSource(ctx, e.ID, "https://www.youtube.com/channel/"+info.ChannelID+"/videos", info.Channel)
	}
	_ = s.store.QueueDownload(ctx, e.ID)
	s.kickDownloads()
	s.store.AddActivity(ctx, me(r), "pick", "Picked video “"+e.Title+"”", p.ID, e.ID)
	ep, _ := s.store.GetEpisode(ctx, me(r), e.ID)
	writeJSON(w, http.StatusCreated, s.episodeDetail(r, ep))
}

// handlePickEpisode adds one episode of a feed to "Picks" without
// subscribing to the show: {feed_url, guid} (guid from /preview, or a title).
func (s *Server) handlePickEpisode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FeedURL  string `json:"feed_url"`
		GUID     string `json:"guid"`
		Title    string `json:"title"`
		Download *bool  `json:"download"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.FeedURL) == "" || (in.GUID == "" && in.Title == "") {
		writeErr(w, http.StatusBadRequest, "feed_url and guid (or title) are required")
		return
	}
	ctx := r.Context()
	u := strings.TrimSpace(in.FeedURL)
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	res, err := feed.Fetch(ctx, s.http, u, "", "", s.userAgent())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "could not read the feed: "+err.Error())
		return
	}
	var item *feed.Item
	for i := range res.Feed.Items {
		it := &res.Feed.Items[i]
		if (in.GUID != "" && it.GUID == in.GUID) || (in.GUID == "" && strings.EqualFold(it.Title, in.Title)) {
			item = it
			break
		}
	}
	if item == nil || item.EnclosureURL == "" {
		writeErr(w, http.StatusNotFound, "that episode is not in the feed (or has no audio)")
		return
	}
	p, err := s.picksPodcast(ctx, me(r))
	if err != nil {
		s.fail(w, err, "picks")
		return
	}
	e := store.Episode{PodcastID: p.ID, GUID: u + "#" + item.GUID, Title: item.Title, Description: item.Description, Link: item.Link,
		EnclosureURL: item.EnclosureURL, EnclosureType: item.EnclosureType, EnclosureBytes: item.EnclosureLen, Duration: item.Duration,
		PubDate: store.FormatTime(item.PubDate), ArtworkURL: firstNonEmpty(item.Image, res.Feed.Image), Season: item.Season, EpisodeNo: item.Episode,
		ChaptersURL: item.ChaptersURL, TranscriptURL: item.TranscriptURL}
	if e.Title == "" {
		e.Title = "Untitled"
	}
	if res.Feed.Title != "" {
		e.Title = e.Title + " — " + res.Feed.Title
	}
	if _, err := s.store.UpsertEpisode(ctx, &e); err != nil {
		s.fail(w, err, "pick")
		return
	}
	_ = s.store.SetEpisodeSource(ctx, e.ID, u, res.Feed.Title)
	if in.Download == nil || *in.Download {
		_ = s.store.QueueDownload(ctx, e.ID)
		s.kickDownloads()
	}
	s.store.AddActivity(ctx, me(r), "pick", "Picked “"+item.Title+"” from "+res.Feed.Title, p.ID, e.ID)
	ep, _ := s.store.GetEpisode(ctx, me(r), e.ID)
	writeJSON(w, http.StatusCreated, s.episodeDetail(r, ep))
}

type ytSearch struct {
	at       time.Time
	channels []ytdlp.Channel
	items    []ytdlp.SearchResult
}

var (
	ytSearchMu    sync.Mutex
	ytSearchCache = map[string]ytSearch{}
)

// channelJSON is a search result with the caller's subscription, if any.
type channelJSON struct {
	ytdlp.Channel
	SubscribedID int64 `json:"subscribed_id,omitempty"`
}

// itemJSON is a video or playlist found by a search, with what the caller has of it.
type itemJSON struct {
	ytdlp.SearchResult
	SubscribedID int64 `json:"subscribed_id,omitempty"` // playlist followed
	EpisodeID    int64 `json:"episode_id,omitempty"`    // video already in the library
}

// handleSearchChannels searches YouTube for channels, videos or playlists
// (?type=channel|video|playlist&limit=, cached ten minutes). YouTube's search has
// no offset: a client that wants more asks again with a larger limit (up to 100).
func (s *Server) handleSearchChannels(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	typ := r.URL.Query().Get("type")
	if typ != "video" && typ != "playlist" {
		typ = "channel"
	}
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}})
		return
	}
	if !s.ytAvailable() {
		writeErr(w, http.StatusBadRequest, "yt-dlp is not available on this box")
		return
	}
	limit := clampInt(queryInt(r, "limit"), 0, 100)
	if limit == 0 {
		limit = 12
	}
	key := typ + "|" + strings.ToLower(q) + "|" + strconv.Itoa(limit)
	ytSearchMu.Lock()
	c, ok := ytSearchCache[key]
	ytSearchMu.Unlock()
	if !ok || time.Since(c.at) > 10*time.Minute {
		var err error
		c = ytSearch{at: time.Now()}
		switch typ {
		case "channel":
			c.channels, err = s.yt.SearchChannels(r.Context(), q, limit)
		case "video":
			c.items, err = s.yt.SearchVideos(r.Context(), q, limit)
		case "playlist":
			c.items, err = s.yt.SearchPlaylists(r.Context(), q, limit)
		}
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		ytSearchMu.Lock()
		if len(ytSearchCache) > 300 {
			ytSearchCache = map[string]ytSearch{}
		}
		ytSearchCache[key] = c
		ytSearchMu.Unlock()
	}
	pods, _ := s.store.ListPodcasts(r.Context(), me(r), "", store.KindYouTube, "")
	byID := map[string]int64{}
	for _, p := range pods {
		if p.YouTubeID != "" {
			byID[p.YouTubeID] = p.ID
		}
	}
	if typ == "channel" {
		out := make([]channelJSON, 0, len(c.channels))
		for _, ch := range c.channels {
			out = append(out, channelJSON{Channel: ch, SubscribedID: byID[ch.ID]})
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": out})
		return
	}
	var ids []string
	for _, it := range c.items {
		ids = append(ids, it.ID)
	}
	have := s.store.EpisodesByYouTubeIDs(r.Context(), me(r), ids)
	out := make([]itemJSON, 0, len(c.items))
	for _, it := range c.items {
		if it.MembersOnly && !s.yt.HasCookies() {
			continue
		}
		j := itemJSON{SearchResult: it}
		if typ == "playlist" {
			j.SubscribedID = byID[it.ID]
		} else {
			j.EpisodeID = have[it.ID]
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

// handleListing pages through a YouTube channel or playlist for the Videos
// window: ?url=&offset=&limit= → {episodes, more}.
func (s *Server) handleListing(w http.ResponseWriter, r *http.Request) {
	kind, norm, _, err := ytdlp.Classify(r.URL.Query().Get("url"))
	if err != nil || kind == ytdlp.KindVideo {
		writeErr(w, http.StatusBadRequest, "a channel or playlist URL is required")
		return
	}
	offset := clampInt(queryInt(r, "offset"), 0, 5000)
	limit := clampInt(queryInt(r, "limit"), 0, 50)
	if limit == 0 {
		limit = 30
	}
	l, err := s.yt.IndexPage(r.Context(), norm, offset, limit)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	members := s.yt.HasCookies()
	items := []map[string]any{}
	for _, e := range l.Entries {
		if e.MembersOnly && !members {
			continue
		}
		items = append(items, map[string]any{"title": e.Title, "duration": e.Duration, "date": ytdlp.Day(e.UploadDate, e.Timestamp), "guid": "yt:" + e.ID, "image": e.Thumbnail, "link": e.URL, "has_audio": !e.Live})
	}
	// a full page means there may be more; members-only ones count as seen
	writeJSON(w, http.StatusOK, map[string]any{"episodes": items, "more": len(l.Entries) >= limit, "next": offset + len(l.Entries)})
}
