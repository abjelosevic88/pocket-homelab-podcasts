// Package feed reads podcast RSS/Atom feeds, OPML files and writes RSS.
package feed

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

// Feed is what a podcast feed says about the show.
type Feed struct {
	Title       string
	Link        string
	Description string
	Author      string
	Image       string
	Language    string
	Categories  []string
	Explicit    bool
	Items       []Item
}

// Item is one episode in a feed.
type Item struct {
	GUID          string
	Title         string
	Description   string
	Link          string
	EnclosureURL  string
	EnclosureType string
	EnclosureLen  int64
	Duration      int
	PubDate       time.Time
	Image         string
	Season        int
	Episode       int
	EpisodeType   string
	Explicit      bool
	ChaptersURL   string
	TranscriptURL string
}

// Result of a fetch: the parsed feed or NotModified with the caching headers.
type Result struct {
	Feed         *Feed
	NotModified  bool
	ETag         string
	LastModified string
}

// Fetch downloads and parses a feed, honouring ETag / Last-Modified.
func Fetch(ctx context.Context, client *http.Client, url, etag, lastModified, ua string) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml, */*;q=0.5")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified {
		return &Result{NotModified: true, ETag: etag, LastModified: lastModified}, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	f, err := Parse(body)
	if err != nil {
		return nil, err
	}
	return &Result{Feed: f, ETag: res.Header.Get("ETag"), LastModified: res.Header.Get("Last-Modified")}, nil
}

// ---- RSS ----

type rss struct {
	Channel struct {
		Title       string `xml:"title"`
		Link        string `xml:"link"`
		Description string `xml:"description"`
		Language    string `xml:"language"`
		Author      string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd author"`
		Summary     string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd summary"`
		Explicit    string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd explicit"`
		Owner       struct {
			Name string `xml:"name"`
		} `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd owner"`
		ItunesImage struct {
			Href string `xml:"href,attr"`
		} `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
		Image struct {
			URL string `xml:"url"`
		} `xml:"image"`
		Categories []struct {
			Text string `xml:"text,attr"`
			Sub  []struct {
				Text string `xml:"text,attr"`
			} `xml:"category"`
		} `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd category"`
		PlainCategories []string  `xml:"category"`
		Items           []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Content     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	Summary     string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd summary"`
	PubDate     string `xml:"pubDate"`
	GUID        struct {
		Value string `xml:",chardata"`
	} `xml:"guid"`
	Enclosures []struct {
		URL    string `xml:"url,attr"`
		Type   string `xml:"type,attr"`
		Length string `xml:"length,attr"`
	} `xml:"enclosure"`
	Media []struct {
		URL    string `xml:"url,attr"`
		Type   string `xml:"type,attr"`
		Medium string `xml:"medium,attr"`
	} `xml:"http://search.yahoo.com/mrss/ content"`
	Duration    string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd duration"`
	Explicit    string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd explicit"`
	Season      string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd season"`
	Episode     string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd episode"`
	EpisodeType string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd episodeType"`
	ItunesImage struct {
		Href string `xml:"href,attr"`
	} `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
	Chapters struct {
		URL string `xml:"url,attr"`
	} `xml:"https://podcastindex.org/namespace/1.0 chapters"`
	Transcripts []struct {
		URL  string `xml:"url,attr"`
		Type string `xml:"type,attr"`
	} `xml:"https://podcastindex.org/namespace/1.0 transcript"`
}

// ---- Atom (YouTube's feeds and a few podcasts) ----

type atom struct {
	Title    string `xml:"title"`
	Subtitle string `xml:"subtitle"`
	Links    []struct {
		Rel  string `xml:"rel,attr"`
		Href string `xml:"href,attr"`
		Type string `xml:"type,attr"`
	} `xml:"link"`
	Author struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Logo    string `xml:"logo"`
	Icon    string `xml:"icon"`
	Entries []struct {
		ID      string `xml:"id"`
		Title   string `xml:"title"`
		Updated string `xml:"updated"`
		Publ    string `xml:"published"`
		Summary string `xml:"summary"`
		Content string `xml:"content"`
		Links   []struct {
			Rel    string `xml:"rel,attr"`
			Href   string `xml:"href,attr"`
			Type   string `xml:"type,attr"`
			Length string `xml:"length,attr"`
		} `xml:"link"`
		VideoID string `xml:"http://www.youtube.com/xml/schemas/2015 videoId"`
		Media   struct {
			Thumb struct {
				URL string `xml:"url,attr"`
			} `xml:"thumbnail"`
			Desc string `xml:"description"`
		} `xml:"http://search.yahoo.com/mrss/ group"`
	} `xml:"entry"`
}

// Parse reads an RSS 2.0 or Atom feed.
func Parse(data []byte) (*Feed, error) {
	data = []byte(strings.TrimLeft(strings.TrimPrefix(string(data), "\xef\xbb\xbf"), " \t\r\n"))
	dec := func(v any) error {
		d := xml.NewDecoder(strings.NewReader(string(data)))
		d.CharsetReader = charset.NewReaderLabel
		d.Strict = false
		return d.Decode(v)
	}
	if strings.Contains(string(data[:min(len(data), 2000)]), "<feed") && !strings.Contains(string(data[:min(len(data), 2000)]), "<rss") {
		var a atom
		if err := dec(&a); err != nil {
			return nil, fmt.Errorf("parse atom: %w", err)
		}
		return fromAtom(&a), nil
	}
	var r rss
	if err := dec(&r); err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}
	if r.Channel.Title == "" && len(r.Channel.Items) == 0 {
		return nil, errors.New("not a podcast feed (no channel)")
	}
	return fromRSS(&r), nil
}

func fromRSS(r *rss) *Feed {
	c := r.Channel
	f := &Feed{Title: clean(c.Title), Link: strings.TrimSpace(c.Link), Language: strings.TrimSpace(c.Language), Explicit: yes(c.Explicit)}
	f.Description = pick(c.Description, c.Summary)
	f.Author = clean(pick(c.Author, c.Owner.Name))
	f.Image = pick(c.ItunesImage.Href, c.Image.URL)
	for _, cat := range c.Categories {
		if cat.Text != "" {
			f.Categories = append(f.Categories, cat.Text)
		}
		for _, sub := range cat.Sub {
			if sub.Text != "" {
				f.Categories = append(f.Categories, sub.Text)
			}
		}
	}
	for _, cat := range c.PlainCategories {
		if cat = strings.TrimSpace(cat); cat != "" {
			f.Categories = append(f.Categories, cat)
		}
	}
	for _, it := range c.Items {
		item := Item{Title: clean(it.Title), Link: strings.TrimSpace(it.Link), Explicit: yes(it.Explicit), EpisodeType: strings.TrimSpace(it.EpisodeType),
			Image: strings.TrimSpace(it.ItunesImage.Href), ChaptersURL: strings.TrimSpace(it.Chapters.URL)}
		item.Description = pick(it.Content, it.Description, it.Summary)
		item.Duration = ParseDuration(it.Duration)
		item.Season, _ = strconv.Atoi(strings.TrimSpace(it.Season))
		item.Episode, _ = strconv.Atoi(strings.TrimSpace(it.Episode))
		item.PubDate = ParseDate(it.PubDate)
		for _, en := range it.Enclosures {
			if en.URL == "" {
				continue
			}
			if item.EnclosureURL == "" || (!isMedia(item.EnclosureType) && isMedia(en.Type)) {
				item.EnclosureURL, item.EnclosureType = strings.TrimSpace(en.URL), strings.TrimSpace(en.Type)
				item.EnclosureLen, _ = strconv.ParseInt(strings.TrimSpace(en.Length), 10, 64)
			}
		}
		if item.EnclosureURL == "" {
			for _, m := range it.Media {
				if m.URL != "" && (isMedia(m.Type) || m.Medium == "audio" || m.Medium == "video") {
					item.EnclosureURL, item.EnclosureType = m.URL, m.Type
					break
				}
			}
		}
		for _, t := range it.Transcripts {
			if t.URL != "" && (item.TranscriptURL == "" || strings.Contains(t.Type, "json") || strings.Contains(t.Type, "vtt")) {
				item.TranscriptURL = t.URL
			}
		}
		item.GUID = strings.TrimSpace(it.GUID.Value)
		if item.GUID == "" {
			item.GUID = pick(item.EnclosureURL, item.Link, item.Title+"|"+it.PubDate)
		}
		if item.Title == "" && item.EnclosureURL == "" {
			continue
		}
		f.Items = append(f.Items, item)
	}
	return f
}

func fromAtom(a *atom) *Feed {
	f := &Feed{Title: clean(a.Title), Description: a.Subtitle, Author: clean(a.Author.Name), Image: pick(a.Logo, a.Icon)}
	for _, l := range a.Links {
		if l.Rel == "" || l.Rel == "alternate" {
			f.Link = l.Href
			break
		}
	}
	for _, e := range a.Entries {
		it := Item{GUID: strings.TrimSpace(e.ID), Title: clean(e.Title), Description: pick(e.Content, e.Summary, e.Media.Desc), Image: e.Media.Thumb.URL}
		it.PubDate = ParseDate(pick(e.Publ, e.Updated))
		for _, l := range e.Links {
			switch l.Rel {
			case "enclosure":
				it.EnclosureURL, it.EnclosureType = l.Href, l.Type
				it.EnclosureLen, _ = strconv.ParseInt(l.Length, 10, 64)
			case "", "alternate":
				if it.Link == "" {
					it.Link = l.Href
				}
			}
		}
		if e.VideoID != "" && it.Link == "" {
			it.Link = "https://www.youtube.com/watch?v=" + e.VideoID
		}
		if it.GUID == "" {
			it.GUID = pick(it.EnclosureURL, it.Link)
		}
		f.Items = append(f.Items, it)
	}
	return f
}

func isMedia(t string) bool {
	return strings.HasPrefix(t, "audio/") || strings.HasPrefix(t, "video/")
}

func yes(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "yes" || s == "true" || s == "explicit"
}

func pick(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

var spaces = regexp.MustCompile(`\s+`)

func clean(s string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(s), " "))
}

// ParseDuration reads "1:02:03", "62:03", "3723" or "3723.5".
func ParseDuration(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		total := 0
		for _, p := range parts {
			n, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if err != nil {
				return 0
			}
			total = total*60 + int(n)
		}
		return total
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int(n)
}

var dateLayouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC3339, time.RFC3339Nano,
	"Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", "Mon, 02 Jan 2006 15:04:05 -0700 MST",
	"2 Jan 2006 15:04:05 -0700", "02 Jan 2006 15:04:05 -0700", "Mon, 02 Jan 2006 15:04 -0700", "Mon, 02 Jan 2006",
	"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02", "Mon, 02 Jan 2006 15:04:05 Z", "Mon, 02 Jan 2006 15:04:05 UT",
}

// ParseDate reads the date formats found in the wild (zero time when it can't).
func ParseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	// "Tue, 12 Mar 2024 10:00:00 +0000 (UTC)" and friends
	if i := strings.Index(s, " ("); i > 0 {
		return ParseDate(s[:i])
	}
	return time.Time{}
}

// StripHTML turns an HTML description into readable text (for lists).
func StripHTML(s string) string {
	s = regexp.MustCompile(`(?i)<br\s*/?>|</p>|</li>|</div>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
