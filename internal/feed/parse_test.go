package feed

import (
	"testing"
	"time"
)

const sample = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:podcast="https://podcastindex.org/namespace/1.0">
<channel><title>Test &amp; Show</title><link>https://example.com</link><description>Desc</description>
<itunes:author>Someone</itunes:author><itunes:image href="https://example.com/a.jpg"/><itunes:category text="Technology"><itunes:category text="Tech News"/></itunes:category>
<item><title>Ep 1</title><guid>g1</guid><pubDate>Tue, 12 Mar 2024 10:00:00 +0000</pubDate><enclosure url="https://example.com/1.mp3" type="audio/mpeg" length="123"/><itunes:duration>1:02:03</itunes:duration><itunes:episode>1</itunes:episode><podcast:chapters url="https://example.com/1.json" type="application/json+chapters"/></item>
<item><title>Ep 2</title><enclosure url="https://example.com/2.mp3" type="audio/mpeg"/><itunes:duration>125</itunes:duration></item>
</channel></rss>`

func TestParseRSS(t *testing.T) {
	f, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Test & Show" || f.Author != "Someone" || f.Image != "https://example.com/a.jpg" {
		t.Fatalf("channel: %+v", f)
	}
	if len(f.Categories) != 2 {
		t.Fatalf("categories: %v", f.Categories)
	}
	if len(f.Items) != 2 {
		t.Fatalf("items: %d", len(f.Items))
	}
	it := f.Items[0]
	if it.GUID != "g1" || it.Duration != 3723 || it.Episode != 1 || it.EnclosureLen != 123 || it.ChaptersURL == "" {
		t.Fatalf("item: %+v", it)
	}
	if it.PubDate != time.Date(2024, 3, 12, 10, 0, 0, 0, time.UTC) {
		t.Fatalf("date: %v", it.PubDate)
	}
	if f.Items[1].GUID != "https://example.com/2.mp3" || f.Items[1].Duration != 125 {
		t.Fatalf("item2: %+v", f.Items[1])
	}
}

func TestOPMLRoundTrip(t *testing.T) {
	in := []OPMLEntry{{Title: "A", FeedURL: "https://a/feed", SiteURL: "https://a"}, {Title: "B & C", FeedURL: "https://b/feed"}}
	out, err := ParseOPML(BuildOPML("x", in))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[1].Title != "B & C" || out[0].FeedURL != "https://a/feed" {
		t.Fatalf("%+v", out)
	}
}

func TestBuildRSS(t *testing.T) {
	b := BuildRSS(Show{Title: "S", Description: "<b>d</b>", Episodes: []ShowEpisode{{GUID: "1", Title: "E", MediaURL: "https://x/1.m4a", MediaType: "audio/mp4", Duration: 61, PubDate: time.Now()}}})
	f, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "S" || len(f.Items) != 1 || f.Items[0].Duration != 61 || f.Items[0].EnclosureURL != "https://x/1.m4a" {
		t.Fatalf("%+v", f)
	}
}
