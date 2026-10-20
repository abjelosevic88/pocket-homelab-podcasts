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

type channelSearch struct {
	at  time.Time
	res []ytdlp.Channel
}

var (
	channelCacheMu sync.Mutex
	channelCache   = map[string]channelSearch{}
)

// channelJSON is a search result with the caller's subscription, if any.
type channelJSON struct {
	ytdlp.Channel
	SubscribedID int64 `json:"subscribed_id,omitempty"`
}

// handleSearchChannels finds YouTube channels by name (cached ten minutes).
func (s *Server) handleSearchChannels(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"results": []channelJSON{}})
		return
	}
	if !s.ytAvailable() {
		writeErr(w, http.StatusBadRequest, "yt-dlp is not available on this box")
		return
	}
	limit := clampInt(queryInt(r, "limit"), 0, 30)
	if limit == 0 {
		limit = 12
	}
	key := strings.ToLower(q) + "|" + strconv.Itoa(limit)
	channelCacheMu.Lock()
	c, ok := channelCache[key]
	channelCacheMu.Unlock()
	if !ok || time.Since(c.at) > 10*time.Minute {
		res, err := s.yt.SearchChannels(r.Context(), q, limit)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		c = channelSearch{at: time.Now(), res: res}
		channelCacheMu.Lock()
		if len(channelCache) > 200 {
			channelCache = map[string]channelSearch{}
		}
		channelCache[key] = c
		channelCacheMu.Unlock()
	}
	pods, _ := s.store.ListPodcasts(r.Context(), me(r), "", store.KindYouTube, "")
	byID := map[string]int64{}
	for _, p := range pods {
		if p.YouTubeID != "" {
			byID[p.YouTubeID] = p.ID
		}
	}
	out := make([]channelJSON, 0, len(c.res))
	for _, ch := range c.res {
		out = append(out, channelJSON{Channel: ch, SubscribedID: byID[ch.ID]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}
