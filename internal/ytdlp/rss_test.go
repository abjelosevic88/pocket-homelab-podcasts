package ytdlp

import "testing"

func TestRSSURL(t *testing.T) {
	if got := RSSURL(KindChannel, "UCsBjURrPoezykLs9EqgamOA"); got != "https://www.youtube.com/feeds/videos.xml?playlist_id=UULFsBjURrPoezykLs9EqgamOA" {
		t.Fatal(got)
	}
	if got := RSSURL(KindPlaylist, "PLQWAYEAq7TDo"); got != "https://www.youtube.com/feeds/videos.xml?playlist_id=PLQWAYEAq7TDo" {
		t.Fatal(got)
	}
	if RSSURL(KindChannel, "") != "" || RSSURL("picks", "x") != "" {
		t.Fatal("unexpected url")
	}
}
