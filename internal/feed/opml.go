package feed

import (
	"encoding/xml"
	"strings"
	"time"
)

// OPMLEntry is one subscription in an OPML file.
type OPMLEntry struct {
	Title   string
	FeedURL string
	SiteURL string
}

type opml struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    struct {
		Title       string `xml:"title"`
		DateCreated string `xml:"dateCreated,omitempty"`
	} `xml:"head"`
	Body struct {
		Outlines []outline `xml:"outline"`
	} `xml:"body"`
}

type outline struct {
	Text     string    `xml:"text,attr"`
	Title    string    `xml:"title,attr,omitempty"`
	Type     string    `xml:"type,attr,omitempty"`
	XMLURL   string    `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string    `xml:"htmlUrl,attr,omitempty"`
	Outlines []outline `xml:"outline,omitempty"`
}

// ParseOPML lists the feeds in an OPML file (nested folders flattened).
func ParseOPML(data []byte) ([]OPMLEntry, error) {
	var o opml
	if err := xml.Unmarshal(data, &o); err != nil {
		return nil, err
	}
	var out []OPMLEntry
	var walk func([]outline)
	walk = func(ols []outline) {
		for _, ol := range ols {
			if ol.XMLURL != "" {
				out = append(out, OPMLEntry{Title: pick(ol.Title, ol.Text), FeedURL: strings.TrimSpace(ol.XMLURL), SiteURL: strings.TrimSpace(ol.HTMLURL)})
			}
			walk(ol.Outlines)
		}
	}
	walk(o.Body.Outlines)
	return out, nil
}

// BuildOPML writes subscriptions as OPML 2.0.
func BuildOPML(title string, entries []OPMLEntry) []byte {
	o := opml{Version: "2.0"}
	o.Head.Title = title
	o.Head.DateCreated = time.Now().UTC().Format(time.RFC1123Z)
	for _, e := range entries {
		o.Body.Outlines = append(o.Body.Outlines, outline{Text: e.Title, Title: e.Title, Type: "rss", XMLURL: e.FeedURL, HTMLURL: e.SiteURL})
	}
	b, _ := xml.MarshalIndent(o, "", "  ")
	return append([]byte(xml.Header), b...)
}
