package store

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

// A refresh re-saves episodes from a listing that may lack notes and exact
// dates; it must not blank what a download stored.
func TestUpsertKeepsDownloadedMeta(t *testing.T) {
	st, err := Open(t.TempDir()+"/t.db", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	u := &User{Email: "a@b.c", Role: "admin"}
	_ = st.CreateUser(ctx, u)
	p := &Podcast{UserID: u.ID, Kind: KindYouTube, Title: "C", FeedURL: "https://www.youtube.com/@c/videos"}
	if err := st.CreatePodcast(ctx, p); err != nil {
		t.Fatal(err)
	}
	e := Episode{PodcastID: p.ID, GUID: "yt:x", Title: "Video", YouTubeID: "x", PubDate: "2026-10-01T00:00:00Z"}
	if _, err := st.UpsertEpisode(ctx, &e); err != nil {
		t.Fatal(err)
	}
	_ = st.SetEpisodeMeta(ctx, e.ID, "Video", "<p>notes</p>", "2026-10-01T17:35:15Z", 340, "", "")
	again := Episode{PodcastID: p.ID, GUID: "yt:x", Title: "Video", YouTubeID: "x", Position: 3}
	if _, err := st.UpsertEpisode(ctx, &again); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetEpisodeAny(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "<p>notes</p>" || got.PubDate != "2026-10-01T17:35:15Z" || got.Duration != 340 || got.Position != 3 {
		t.Fatalf("refresh clobbered the episode: %+v", got)
	}
}

func TestRetireMembersOnlyMovesHistory(t *testing.T) {
	st, err := Open(t.TempDir()+"/t.db", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	u := &User{Email: "a@b.c", Role: "admin"}
	_ = st.CreateUser(ctx, u)
	p := &Podcast{UserID: u.ID, Kind: KindYouTube, Title: "C", FeedURL: "https://www.youtube.com/@c/videos"}
	_ = st.CreatePodcast(ctx, p)
	pub := Episode{PodcastID: p.ID, GUID: "yt:pub", Title: "Same title", YouTubeID: "pub", Duration: 100}
	mem := Episode{PodcastID: p.ID, GUID: "yt:mem", Title: "Same title", YouTubeID: "mem", Duration: 90}
	_, _ = st.UpsertEpisode(ctx, &pub)
	_, _ = st.UpsertEpisode(ctx, &mem)
	_ = st.SaveProgress(ctx, u.ID, mem.ID, 90, 90, true) // history landed on the members-only twin
	if !st.RetireMembersOnly(ctx, p.ID, mem.ID) {
		t.Fatal("not retired")
	}
	if _, err := st.GetEpisodeAny(ctx, mem.ID); err == nil {
		t.Fatal("members-only episode still there")
	}
	got, _ := st.GetEpisode(ctx, u.ID, pub.ID)
	if !got.Completed || got.LastPlayedAt == "" {
		t.Fatalf("history not moved to the public twin: %+v", got)
	}
	// history with no public twin stays as history
	lone := Episode{PodcastID: p.ID, GUID: "yt:lone", Title: "Only for members", YouTubeID: "lone", Duration: 80}
	_, _ = st.UpsertEpisode(ctx, &lone)
	_ = st.SaveProgress(ctx, u.ID, lone.ID, 80, 80, true)
	if st.RetireMembersOnly(ctx, p.ID, lone.ID) {
		t.Fatal("listened episode without a twin was deleted")
	}
	// a downloaded one stays
	_ = st.SetEpisodeFile(ctx, pub.ID, "x.m4a", 1, "audio/mp4")
	if st.RetireMembersOnly(ctx, p.ID, pub.ID) {
		t.Fatal("downloaded episode retired")
	}
}
