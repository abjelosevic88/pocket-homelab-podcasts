// Package search finds podcasts in public directories: iTunes (no key) and
// Podcast Index (free key).
package search

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Result is a podcast found in a directory.
type Result struct {
	Title       string   `json:"title"`
	Author      string   `json:"author"`
	FeedURL     string   `json:"feed_url"`
	SiteURL     string   `json:"site_url"`
	Image       string   `json:"image"`
	Description string   `json:"description"`
	Language    string   `json:"language"`
	Categories  []string `json:"categories"`
	Episodes    int      `json:"episodes"`
	Source      string   `json:"source"`
	SourceID    string   `json:"source_id"`
	LastUpdate  string   `json:"last_update,omitempty"`
}

// Client searches the directories.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	PIKey     string
	PISecret  string
}

// New creates a client.
func New(ua string, timeout time.Duration) *Client {
	return &Client{HTTP: &http.Client{Timeout: timeout}, UserAgent: ua}
}

// With returns a client using the given Podcast Index credentials.
func (c *Client) With(key, secret string) *Client {
	cp := *c
	cp.PIKey, cp.PISecret = key, secret
	return &cp
}

// Sources lists what is available with the current keys.
func (c *Client) Sources() []string {
	s := []string{"itunes"}
	if c.PIKey != "" && c.PISecret != "" {
		s = append(s, "podcastindex")
	}
	return s
}

func (c *Client) get(ctx context.Context, u string, headers map[string]string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, msg)
	}
	return json.Unmarshal(body, v)
}

// Search queries the chosen source ("all", "itunes", "podcastindex").
func (c *Client) Search(ctx context.Context, q, source string, limit int) ([]Result, []string) {
	if limit <= 0 {
		limit = 25
	}
	var out []Result
	var warnings []string
	seen := map[string]bool{}
	add := func(rs []Result) {
		for _, r := range rs {
			key := strings.ToLower(strings.TrimSuffix(r.FeedURL, "/"))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, r)
		}
	}
	if source == "all" || source == "podcastindex" {
		if c.PIKey != "" && c.PISecret != "" {
			rs, err := c.podcastIndex(ctx, q, limit)
			if err != nil {
				warnings = append(warnings, "Podcast Index: "+err.Error())
			}
			add(rs)
		} else if source == "podcastindex" {
			warnings = append(warnings, "Podcast Index needs an API key (Settings → Search)")
		}
	}
	if source == "all" || source == "itunes" {
		rs, err := c.itunes(ctx, q, limit)
		if err != nil {
			warnings = append(warnings, "iTunes: "+err.Error())
		}
		add(rs)
	}
	if out == nil {
		out = []Result{}
	}
	return out, warnings
}

func (c *Client) itunes(ctx context.Context, q string, limit int) ([]Result, error) {
	var res struct {
		Results []struct {
			CollectionID   int64    `json:"collectionId"`
			CollectionName string   `json:"collectionName"`
			ArtistName     string   `json:"artistName"`
			FeedURL        string   `json:"feedUrl"`
			Artwork        string   `json:"artworkUrl600"`
			Artwork100     string   `json:"artworkUrl100"`
			ViewURL        string   `json:"collectionViewUrl"`
			TrackCount     int      `json:"trackCount"`
			Genres         []string `json:"genres"`
			Country        string   `json:"country"`
			ReleaseDate    string   `json:"releaseDate"`
		} `json:"results"`
	}
	u := "https://itunes.apple.com/search?media=podcast&entity=podcast&limit=" + strconv.Itoa(limit) + "&term=" + url.QueryEscape(q)
	if err := c.get(ctx, u, nil, &res); err != nil {
		return nil, err
	}
	var out []Result
	for _, r := range res.Results {
		if r.FeedURL == "" {
			continue
		}
		var cats []string
		for _, g := range r.Genres {
			if g != "Podcasts" {
				cats = append(cats, g)
			}
		}
		out = append(out, Result{Title: r.CollectionName, Author: r.ArtistName, FeedURL: r.FeedURL, SiteURL: r.ViewURL,
			Image: firstNonEmpty(r.Artwork, r.Artwork100), Categories: cats, Episodes: r.TrackCount, Source: "itunes", SourceID: strconv.FormatInt(r.CollectionID, 10), LastUpdate: r.ReleaseDate})
	}
	return out, nil
}

func (c *Client) piHeaders() map[string]string {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	sum := sha1.Sum([]byte(c.PIKey + c.PISecret + now))
	return map[string]string{"X-Auth-Date": now, "X-Auth-Key": c.PIKey, "Authorization": hex.EncodeToString(sum[:])}
}

type piFeed struct {
	ID            int64             `json:"id"`
	Title         string            `json:"title"`
	URL           string            `json:"url"`
	Link          string            `json:"link"`
	Description   string            `json:"description"`
	Author        string            `json:"author"`
	Image         string            `json:"image"`
	Artwork       string            `json:"artwork"`
	Language      string            `json:"language"`
	Categories    map[string]string `json:"categories"`
	EpisodeCount  int               `json:"episodeCount"`
	LastUpdate    int64             `json:"lastUpdateTime"`
	NewestItemPub int64             `json:"newestItemPubdate"`
}

func (f piFeed) result() Result {
	var cats []string
	for _, v := range f.Categories {
		cats = append(cats, v)
	}
	sort.Strings(cats)
	r := Result{Title: f.Title, Author: f.Author, FeedURL: f.URL, SiteURL: f.Link, Image: firstNonEmpty(f.Artwork, f.Image), Description: f.Description,
		Language: f.Language, Categories: cats, Episodes: f.EpisodeCount, Source: "podcastindex", SourceID: strconv.FormatInt(f.ID, 10)}
	if t := f.NewestItemPub; t > 0 {
		r.LastUpdate = time.Unix(t, 0).UTC().Format(time.RFC3339)
	}
	return r
}

func (c *Client) podcastIndex(ctx context.Context, q string, limit int) ([]Result, error) {
	var res struct {
		Feeds []piFeed `json:"feeds"`
	}
	u := "https://api.podcastindex.org/api/1.0/search/byterm?max=" + strconv.Itoa(limit) + "&q=" + url.QueryEscape(q)
	if err := c.get(ctx, u, c.piHeaders(), &res); err != nil {
		return nil, err
	}
	var out []Result
	for _, f := range res.Feeds {
		if f.URL != "" {
			out = append(out, f.result())
		}
	}
	return out, nil
}

// Trending lists what Podcast Index sees rising (needs a key).
func (c *Client) Trending(ctx context.Context, lang string, limit int) ([]Result, error) {
	if c.PIKey == "" || c.PISecret == "" {
		return nil, fmt.Errorf("Podcast Index needs an API key")
	}
	var res struct {
		Feeds []piFeed `json:"feeds"`
	}
	u := "https://api.podcastindex.org/api/1.0/podcasts/trending?max=" + strconv.Itoa(limit)
	if lang != "" {
		u += "&lang=" + url.QueryEscape(lang)
	}
	if err := c.get(ctx, u, c.piHeaders(), &res); err != nil {
		return nil, err
	}
	out := []Result{}
	for _, f := range res.Feeds {
		if f.URL != "" {
			out = append(out, f.result())
		}
	}
	return out, nil
}

// Test checks the Podcast Index credentials.
func (c *Client) Test(ctx context.Context) error {
	var res struct {
		Status string `json:"status"`
	}
	return c.get(ctx, "https://api.podcastindex.org/api/1.0/search/byterm?max=1&q=test", c.piHeaders(), &res)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
