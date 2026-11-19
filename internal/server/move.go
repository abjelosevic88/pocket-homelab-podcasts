package server

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/ytdlp"
)

// Largest files the imports take (also declared to Pocket Move).
const (
	importJSONMax = 16 << 20
	importOPMLMax = 8 << 20
)

var errTooLarge = errors.New("file too large")

// readUpload reads an uploaded file: the multipart field, or the raw body.
func readUpload(r *http.Request, field string, max int64) ([]byte, error) {
	body := http.MaxBytesReader(nil, r.Body, max+1<<20) // room for the multipart wrapping
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		r.Body = body
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			return nil, errTooLarge
		}
		f, _, err := r.FormFile(field)
		if err != nil {
			return nil, errors.New(field + " is required")
		}
		defer f.Close()
		return readMax(f, max)
	}
	return readMax(body, max)
}

func readMax(rd io.Reader, max int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(rd, max+1))
	if err != nil {
		return nil, errTooLarge
	}
	if int64(len(data)) > max {
		return nil, errTooLarge
	}
	return data, nil
}

// ytChannel is one row of YouTube's subscriptions.csv.
type ytChannel struct{ ID, URL, Title string }

var reChannelID = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)

// parseYouTubeSubscriptions reads Google Takeout's subscriptions.csv
// ("Channel Id,Channel Url,Channel Title"; the header is translated in other
// languages, so columns are found by what is in them).
func parseYouTubeSubscriptions(data []byte) []ytChannel {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord, cr.LazyQuotes = -1, true
	seen := map[string]bool{}
	var out []ytChannel
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		var c ytChannel
		for _, cell := range rec {
			cell = strings.TrimSpace(cell)
			switch {
			case cell == "":
			case c.ID == "" && reChannelID.MatchString(cell):
				c.ID = cell
			case c.URL == "" && strings.Contains(strings.ToLower(cell), "youtube.com/"):
				c.URL = cell
			case c.Title == "":
				c.Title = cell
			}
		}
		if c.URL == "" && c.ID != "" {
			c.URL = "https://www.youtube.com/channel/" + c.ID
		}
		if c.URL == "" { // the header, or a row without a channel
			continue
		}
		key := strings.ToLower(firstNonEmpty(c.ID, c.URL))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

// handleImportYouTubeCSV follows every channel of a YouTube subscriptions
// export (Google Takeout) as a podcast, the way "add a YouTube channel" does.
// Channels the person already follows are skipped; nothing is removed.
func (s *Server) handleImportYouTubeCSV(w http.ResponseWriter, r *http.Request) {
	data, err := readUpload(r, "file", importOPMLMax)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	channels := parseYouTubeSubscriptions(data)
	if len(channels) == 0 {
		writeErr(w, http.StatusBadRequest, "no YouTube channels in that file (expected Google Takeout's subscriptions.csv)")
		return
	}
	ctx, uid := r.Context(), me(r)
	have := map[string]bool{}
	pods, err := s.store.ListPodcasts(ctx, uid, "", store.KindYouTube, "title")
	if err != nil {
		s.fail(w, err, "list podcasts")
		return
	}
	for _, p := range pods {
		have[strings.ToLower(p.FeedURL)] = true
		if p.YouTubeID != "" {
			have[strings.ToLower(p.YouTubeID)] = true
		}
	}
	added, skipped := 0, 0
	failed := []string{}
	for _, c := range channels {
		label := firstNonEmpty(c.Title, c.URL)
		_, norm, id, err := ytdlp.Classify(c.URL)
		if err != nil {
			failed = append(failed, label+": "+err.Error())
			continue
		}
		if have[strings.ToLower(norm)] || (id != "" && have[strings.ToLower(id)]) || (c.ID != "" && have[strings.ToLower(c.ID)]) {
			skipped++
			continue
		}
		p, err := s.subscribe(ctx, uid, subscribeInput{URL: c.URL})
		if err != nil {
			var se *subscribeError
			if errors.As(err, &se) && se.status == http.StatusConflict {
				skipped++
				continue
			}
			failed = append(failed, label+": "+err.Error())
			continue
		}
		added++
		have[strings.ToLower(norm)] = true
		if p.YouTubeID != "" {
			have[strings.ToLower(p.YouTubeID)] = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "skipped": skipped, "failed": failed})
}
