package feed

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// Show is what the public feed says about a subscription.
type Show struct {
	Title       string
	Link        string
	Description string
	Author      string
	Image       string
	Language    string
	Explicit    bool
	FeedURL     string
	Episodes    []ShowEpisode
}

// ShowEpisode is one item of the public feed.
type ShowEpisode struct {
	GUID        string
	Title       string
	Description string
	Link        string
	MediaURL    string
	MediaType   string
	MediaBytes  int64
	Duration    int
	PubDate     time.Time
	Image       string
	Season      int
	Episode     int
	ChaptersURL string
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func cdata(s string) string {
	return "<![CDATA[" + strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>") + "]]>"
}

func fmtDuration(sec int) string {
	return fmt.Sprintf("%02d:%02d:%02d", sec/3600, sec%3600/60, sec%60)
}

// BuildRSS writes a podcast RSS 2.0 feed Apple Podcasts, Overcast and the
// rest accept: itunes namespace, enclosures, durations, per-episode art.
func BuildRSS(s Show) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:podcast="https://podcastindex.org/namespace/1.0">` + "\n<channel>\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", esc(s.Title))
	if s.Link != "" {
		fmt.Fprintf(&b, "<link>%s</link>\n", esc(s.Link))
	}
	if s.FeedURL != "" {
		fmt.Fprintf(&b, `<atom:link href="%s" rel="self" type="application/rss+xml"/>`+"\n", esc(s.FeedURL))
	}
	fmt.Fprintf(&b, "<description>%s</description>\n", cdata(s.Description))
	fmt.Fprintf(&b, "<itunes:summary>%s</itunes:summary>\n", cdata(StripHTML(s.Description)))
	lang := s.Language
	if lang == "" {
		lang = "en"
	}
	fmt.Fprintf(&b, "<language>%s</language>\n", esc(lang))
	if s.Author != "" {
		fmt.Fprintf(&b, "<itunes:author>%s</itunes:author>\n", esc(s.Author))
	}
	fmt.Fprintf(&b, "<itunes:explicit>%s</itunes:explicit>\n", map[bool]string{true: "true", false: "false"}[s.Explicit])
	b.WriteString("<itunes:type>episodic</itunes:type>\n<generator>Pocket Podcasts</generator>\n")
	if s.Image != "" {
		fmt.Fprintf(&b, `<itunes:image href="%s"/>`+"\n<image><url>%s</url><title>%s</title><link>%s</link></image>\n", esc(s.Image), esc(s.Image), esc(s.Title), esc(s.Link))
	}
	if len(s.Episodes) > 0 {
		fmt.Fprintf(&b, "<lastBuildDate>%s</lastBuildDate>\n", time.Now().UTC().Format(time.RFC1123Z))
	}
	for _, e := range s.Episodes {
		b.WriteString("<item>\n")
		fmt.Fprintf(&b, "<title>%s</title>\n", esc(e.Title))
		fmt.Fprintf(&b, `<guid isPermaLink="false">%s</guid>`+"\n", esc(e.GUID))
		if e.Link != "" {
			fmt.Fprintf(&b, "<link>%s</link>\n", esc(e.Link))
		}
		if !e.PubDate.IsZero() {
			fmt.Fprintf(&b, "<pubDate>%s</pubDate>\n", e.PubDate.UTC().Format(time.RFC1123Z))
		}
		fmt.Fprintf(&b, "<description>%s</description>\n", cdata(e.Description))
		fmt.Fprintf(&b, "<content:encoded>%s</content:encoded>\n", cdata(e.Description))
		fmt.Fprintf(&b, "<itunes:summary>%s</itunes:summary>\n", cdata(StripHTML(e.Description)))
		fmt.Fprintf(&b, `<enclosure url="%s" type="%s" length="%d"/>`+"\n", esc(e.MediaURL), esc(e.MediaType), e.MediaBytes)
		if e.Duration > 0 {
			fmt.Fprintf(&b, "<itunes:duration>%s</itunes:duration>\n", fmtDuration(e.Duration))
		}
		if e.Image != "" {
			fmt.Fprintf(&b, `<itunes:image href="%s"/>`+"\n", esc(e.Image))
		}
		if e.Season > 0 {
			fmt.Fprintf(&b, "<itunes:season>%d</itunes:season>\n", e.Season)
		}
		if e.Episode > 0 {
			fmt.Fprintf(&b, "<itunes:episode>%d</itunes:episode>\n", e.Episode)
		}
		if e.ChaptersURL != "" {
			fmt.Fprintf(&b, `<podcast:chapters url="%s" type="application/json+chapters"/>`+"\n", esc(e.ChaptersURL))
		}
		b.WriteString("</item>\n")
	}
	b.WriteString("</channel>\n</rss>\n")
	return []byte(b.String())
}
