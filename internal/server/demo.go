package server

import (
	"context"
	"fmt"
	"time"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

// seedDemo fills an empty database with a couple of made-up shows so the UI
// can be tried without the network (DEMO_DATA=true).
func (s *Server) seedDemo(ctx context.Context) error {
	if n, _ := s.store.Counts(ctx)["podcasts"]; n > 0 {
		return nil
	}
	email, name := "demo@localhost", "Demo"
	if s.cfg.AuthMode == "none" {
		email, name = "admin@localhost", "Admin" // the account AUTH_MODE=none signs everyone in as
	}
	u, err := s.firstOrSystemUser(ctx, email, name)
	if err != nil {
		return err
	}
	shows := []struct {
		title, author, desc string
		kind                string
		n                   int
	}{
		{"The Weekly Byte", "Ada & Linus", "Two engineers argue about the week in software.", store.KindRSS, 12},
		{"Night Walks", "Mara Lund", "Slow stories for the end of the day.", store.KindRSS, 8},
		{"Fireship (demo)", "Fireship", "100-second explainers, as audio.", store.KindYouTube, 6},
	}
	for i, sh := range shows {
		p := &store.Podcast{UserID: u.ID, Kind: sh.kind, Title: sh.title, Author: sh.author, Description: sh.desc,
			FeedURL: fmt.Sprintf("https://demo.invalid/%d/feed.xml", i+1), SiteURL: "https://demo.invalid", AutoPlayNext: -1, PubDateMode: "published", SponsorBlock: true,
			DownloadLimit: 3, RefreshMinutes: 10080}
		if err := s.store.CreatePodcast(ctx, p); err != nil {
			return err
		}
		_ = s.store.UpdateFeedState(ctx, p.ID, "", "", nil)
		for j := 0; j < sh.n; j++ {
			e := store.Episode{PodcastID: p.ID, GUID: fmt.Sprintf("demo-%d-%d", i, j), Title: fmt.Sprintf("Episode %d: %s", sh.n-j, []string{"The one about caching", "Why Tuesday", "A long walk", "Rebuilding the garage", "On naming things", "Deadlines"}[j%6]),
				Description: "<p>A demo episode. Nothing to hear here.</p>", EnclosureURL: fmt.Sprintf("https://demo.invalid/%d/%d.mp3", i+1, j), EnclosureType: "audio/mpeg",
				Duration: 900 + (j*7919)%4000, PubDate: time.Now().AddDate(0, 0, -j*4-i).UTC().Format(time.RFC3339), Position: j}
			if sh.kind == store.KindYouTube {
				e.YouTubeID = fmt.Sprintf("demo%07d", i*100+j)
				e.EnclosureURL, e.EnclosureType = "", "audio/mp4"
			}
			if _, err := s.store.UpsertEpisode(ctx, &e); err != nil {
				return err
			}
			switch {
			case j == 0 && i == 0:
				_ = s.store.SaveProgress(ctx, u.ID, e.ID, 420, 420, false)
			case j == 1 && i < 2:
				_ = s.store.SaveProgress(ctx, u.ID, e.ID, e.Duration, 1200, true)
			case j == 2 && i == 0:
				_ = s.store.AddToQueue(ctx, u.ID, e.ID, false)
			case j == 3:
				_ = s.store.SetSaved(ctx, u.ID, e.ID, true)
			}
		}
	}
	s.log.Info("demo data seeded")
	return nil
}
