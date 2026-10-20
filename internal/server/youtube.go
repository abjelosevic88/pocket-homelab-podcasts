package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/ytdlp"
)

// PicksFeedURL marks the per-person "YouTube picks" subscription: single
// videos added by hand land there; it is never refreshed.
const PicksFeedURL = "pocket:youtube-picks"

func (s *Server) picksPodcast(ctx context.Context, uid int64) (*store.Podcast, error) {
	if p, err := s.store.FindPodcastByFeed(ctx, uid, PicksFeedURL); err == nil {
		return p, nil
	}
	st, _ := s.store.GetSettings(ctx, uid)
	p := &store.Podcast{UserID: uid, Kind: store.KindYouTube, Title: "YouTube picks", FeedURL: PicksFeedURL, YouTubeKind: "picks",
		Description: "Single videos you added by hand, as audio.", AutoPlayNext: -1, RetentionDays: st.RetentionDays, PubDateMode: "downloaded", SponsorBlock: true, AutoDownload: true,
		LastRefreshOK: store.Now()}
	if err := s.store.CreatePodcast(ctx, p); err != nil {
		return nil, err
	}
	_ = s.store.UpdateFeedState(ctx, p.ID, "", "", nil)
	return s.store.GetPodcast(ctx, uid, p.ID)
}

// handleAddVideo downloads one YouTube video as an episode of "YouTube picks".
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
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	e := store.Episode{PodcastID: p.ID, GUID: "yt:" + id, Title: info.Title, Description: info.Description, Link: norm, EnclosureType: "audio/mp4",
		Duration: info.Duration, PubDate: store.FormatTime(info.UploadDate), ArtworkURL: info.Thumbnail, YouTubeID: id}
	if _, err := s.store.UpsertEpisode(ctx, &e); err != nil {
		s.fail(w, err, "add video")
		return
	}
	_ = s.store.QueueDownload(ctx, e.ID)
	s.kickDownloads()
	s.store.AddActivity(ctx, me(r), "subscribe", "Added video “"+e.Title+"” to YouTube picks", p.ID, e.ID)
	ep, _ := s.store.GetEpisode(ctx, me(r), e.ID)
	writeJSON(w, http.StatusCreated, s.episodeDetail(r, ep))
}
