package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/ytdlp"
)

// What Pocket Search, Pocket Day and Pocket Hub ask Podcasts (the capabilities
// value in links.go): one route each, in the shape every Pocket app answers
// in. The first three read what /episodes, /podcasts, /listens and /home
// read, as the request's person, from what is stored here: no feed is
// refreshed and nothing is looked up. Only the capture adds something, a
// YouTube video in Picks or a show to follow, the way /youtube/video and
// /podcasts add them.

func episodeURL(id int64) string { return "/episodes/" + strconv.FormatInt(id, 10) }
func showURL(id int64) string    { return "/podcasts/" + strconv.FormatInt(id, 10) }

// dots joins the parts of a line that are there: "The Daily · 42 min · played".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// count is a number with its word: "1 episode", "42 episodes".
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// durWords is a length of time as the pages show it: "45 s", "42 min", "1h 2m".
func durWords(sec int) string {
	if sec <= 0 {
		return ""
	}
	h, m := sec/3600, int(math.Round(float64(sec%3600)/60))
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0:
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d s", sec)
}

// leftWords is the time left in an episode the person is in the middle of:
// "12 min left". An episode of unknown length is just "started".
func leftWords(e *store.Episode) string {
	switch left := e.Duration - e.PlayPosition; {
	case e.Completed || e.PlayPosition <= 0:
		return ""
	case e.Duration > 0 && left > 0:
		return durWords(left) + " left"
	}
	return "started"
}

// progressWords says where the person is in an episode: "played", "12 min
// left", or nothing for one they have not started.
func progressWords(e *store.Episode) string {
	if e.Completed {
		return "played"
	}
	return leftWords(e)
}

// hasWord reports whether there is a letter or a digit to look for.
func hasWord(q string) bool {
	return strings.IndexFunc(q, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0
}

// ---------- search ----------

// pocketSearch answers Pocket Search with the shows the person follows that
// GET /podcasts?q= finds (by name or author) and the episodes GET /episodes?q=
// finds (by title, show notes or show). The closest come first: a show of
// that name, an episode with the words in its title, then what has them in
// the author or the notes, then the other episodes of a show of that name;
// each group newest first.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	if !hasWord(q) {
		return nil, nil
	}
	ctx, uid := r.Context(), me(r)
	lq := strings.ToLower(q)
	has := func(v string) bool { return strings.Contains(strings.ToLower(v), lq) }
	out := []core.SearchItem{}

	pods, err := s.store.ListPodcasts(ctx, uid, q, "", "")
	if err != nil {
		return nil, err
	}
	for i := range pods {
		if i == limit {
			break
		}
		p := &pods[i]
		it := core.SearchItem{ID: "show:" + strconv.FormatInt(p.ID, 10), Title: p.Title, Kind: "show", Date: p.LatestPubDate, URL: showURL(p.ID), Score: 2}
		if has(p.Title) {
			it.Score = 4
		}
		author, unplayed := p.Author, ""
		if strings.EqualFold(author, p.Title) {
			author = ""
		}
		if p.UnplayedCount > 0 {
			unplayed = strconv.Itoa(p.UnplayedCount) + " unplayed"
		}
		it.Snippet = dots(author, count(p.EpisodeCount, "episode", "episodes"), unplayed)
		out = append(out, it)
	}

	// titles first: the newest matches of the wider search may all be show
	// notes, or all the episodes of one show
	seen := map[int64]bool{}
	for _, titleOnly := range []bool{true, false} {
		eps, err := s.store.ListEpisodes(ctx, store.EpisodeFilter{UserID: uid, Query: q, TitleOnly: titleOnly, Limit: limit})
		if err != nil {
			return nil, err
		}
		for i := range eps {
			e := &eps[i]
			if seen[e.ID] {
				continue
			}
			seen[e.ID] = true
			it := core.SearchItem{ID: "episode:" + strconv.FormatInt(e.ID, 10), Title: e.Title, Kind: "episode", Date: e.PubDate, URL: episodeURL(e.ID),
				Snippet: dots(e.PodcastTitle, durWords(e.Duration), progressWords(e)), Score: 2}
			switch {
			case has(e.Title):
				it.Score = 3
			case has(e.PodcastTitle) && !has(e.Description):
				it.Score = 1
			}
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- day ----------

// pocketDay answers Pocket Day from the listening history (what GET
// /listens?date= lists): one line for each episode the person listened to
// that day, with the minutes they really listened, and whether they finished
// it that day. The history is kept by day in the box's own time zone, so that
// is the day meant here; the caller's zone only says how the clock times read.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx, uid := r.Context(), me(r)
	rows, err := s.store.DailyListens(ctx, uid, date)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(rows))
	for _, v := range rows {
		ids = append(ids, v.EpisodeID)
	}
	finishedAt, err := s.store.FinishedAt(ctx, uid, ids)
	if err != nil {
		return nil, err
	}
	loc := time.Local
	if tz != nil {
		loc = tz
	}
	day := &core.Day{Date: date}
	seconds, finished := 0, 0
	for _, v := range rows {
		it := core.DayItem{ID: strconv.FormatInt(v.EpisodeID, 10), Title: v.Title, Sub: v.Podcast, Kind: "listened",
			Minutes: math.Round(float64(v.Seconds)/60*100) / 100, URL: episodeURL(v.EpisodeID)}
		// older history has no clock times: nothing is made up for it
		if t := store.ParseTime(v.FirstAt); !t.IsZero() {
			it.Time = t.In(loc).Format("15:04")
		}
		if t := store.ParseTime(v.LastAt); !t.IsZero() {
			it.End = t.In(loc).Format("15:04")
		}
		if t := store.ParseTime(finishedAt[v.EpisodeID]); !t.IsZero() && t.In(time.Local).Format("2006-01-02") == date {
			it.Done, it.Kind = true, "finished"
			finished++
		}
		day.Items = append(day.Items, it)
		seconds += v.Seconds
	}
	length := durWords(seconds)
	if seconds < 60 {
		length = "less than a minute"
	}
	if n := len(rows); n == 1 {
		day.Summary = "Listened to " + rows[0].Title + " for " + length
		if finished == 1 {
			day.Summary += " and finished it"
		}
	} else {
		day.Summary = fmt.Sprintf("Listened to %d episodes for %s", n, length)
		switch {
		case finished == n:
			day.Summary += " and finished them all"
		case finished > 0:
			day.Summary += fmt.Sprintf(" and finished %d", finished)
		}
	}
	day.Metrics = []core.Metric{{Label: "Time", Value: length}}
	if finished > 0 {
		day.Metrics = append(day.Metrics, core.Metric{Label: "Finished", Value: strconv.Itoa(finished)})
	}
	return day, nil
}

// ---------- widget ----------

const (
	widgetRows     = 8 // what a widget shows
	widgetContinue = 4 // how many of them may be episodes in progress
	widgetNewDays  = 7 // an unplayed episode counts as new for this long
)

// pocketWidget answers Pocket Hub with the top of the Home page: the episodes
// the person is in the middle of, the one played last on top, then the newest
// ones they have not started. The line counts the unplayed episodes that came
// out in the last week. It reads what is stored; feeds are not refreshed.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx, uid := r.Context(), me(r)
	cont, err := s.store.ListEpisodes(ctx, store.EpisodeFilter{UserID: uid, Status: "partial", History: true, Sort: "played", Limit: widgetContinue})
	if err != nil {
		return nil, err
	}
	fresh, err := s.store.ListEpisodes(ctx, store.EpisodeFilter{UserID: uid, Status: "unplayed", Sort: "newest", Limit: widgetRows - len(cont)})
	if err != nil {
		return nil, err
	}
	if len(cont)+len(fresh) == 0 {
		return nil, nil
	}
	w := &core.WidgetData{URL: "/home"}
	for i := range cont {
		e := &cont[i]
		w.Items = append(w.Items, core.WidgetItem{ID: strconv.FormatInt(e.ID, 10), Title: e.Title, Detail: dots(e.PodcastTitle, leftWords(e)), URL: episodeURL(e.ID)})
	}
	for i := range fresh {
		e := &fresh[i]
		w.Items = append(w.Items, core.WidgetItem{ID: strconv.FormatInt(e.ID, 10), Title: e.Title, Detail: dots(e.PodcastTitle, durWords(e.Duration)), URL: episodeURL(e.ID)})
	}
	if len(fresh) > 0 {
		since := time.Now().AddDate(0, 0, -widgetNewDays).UTC().Format(time.RFC3339)
		n, err := s.store.CountEpisodes(ctx, store.EpisodeFilter{UserID: uid, Status: "unplayed", Since: since})
		if err != nil {
			return nil, err
		}
		if n > 0 {
			w.Line = count(n, "new episode", "new episodes") + " this week"
		}
	}
	return w, nil
}

// ---------- capture ----------

// captureAddress finds the address in a quick line that is not a YouTube
// video: the line itself when it is one address ("https://example.com/feed",
// "example.com/feed.xml"), or the one link in a sentence. Two links, or no
// address at all, is nothing to go by.
func captureAddress(line string) (string, bool) {
	words := strings.Fields(line)
	var links []string
	for _, w := range words {
		if l := strings.ToLower(w); strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
			links = append(links, w)
		}
	}
	addr := ""
	switch {
	case len(links) == 1 && len(words) == 1:
		addr = links[0]
	case len(links) == 1:
		addr = strings.TrimRight(links[0], ".,;!?)") // the end of the sentence, not of the address
	case len(links) == 0 && len(words) == 1 && !strings.Contains(words[0], "://"):
		addr = "https://" + words[0]
	default:
		return "", false
	}
	u, err := url.Parse(addr)
	if err != nil || !strings.Contains(u.Hostname(), ".") {
		return "", false
	}
	return addr, true
}

// pocketCapture keeps a pasted address from Pocket Hub. A YouTube video goes
// to the person's Picks and its download starts, as POST /youtube/video does
// for a shared link. Any other address is followed as POST /podcasts follows
// it from the address alone (a podcast's feed, a YouTube channel or a
// playlist), with the person's own defaults from Settings. Both look the
// address up before they answer, as those routes do. The same request_id
// again answers the same and adds nothing: the receipt is written in the
// transaction that adds the episode or the show.
func (s *Server) pocketCapture(r *http.Request, in core.CaptureInput) (*core.CaptureResult, error) {
	ctx, uid := r.Context(), me(r)
	op, err := core.NewOperation(fmt.Sprintf("podcasts:user:%d:%s", uid, core.CapturePath), in.RequestID, in)
	if err != nil {
		return nil, err
	}
	var res core.CaptureResult
	replay := func() (*core.CaptureResult, error) {
		found, err := op.Replay(ctx, s.store.DB, &res)
		switch {
		case errors.Is(err, core.ErrOperationConflict):
			return nil, fmt.Errorf("%w: this was already kept with other words", core.ErrCapture)
		case err != nil || !found:
			return nil, err
		}
		return &res, nil
	}
	if kept, err := replay(); kept != nil || err != nil {
		return kept, err
	}
	// refused says why in the app's own words, unless the same line, sent
	// twice at once, was kept by the other request
	refused := func(err error) (*core.CaptureResult, error) {
		if kept, rerr := replay(); kept != nil || rerr != nil {
			return kept, rerr
		}
		var se *subscribeError
		if errors.As(err, &se) {
			return nil, fmt.Errorf("%w: %s", core.ErrCapture, se.msg)
		}
		return nil, err
	}

	line := strings.TrimSpace(in.Title + "\n" + in.Text)
	if _, _, err := ytdlp.VideoFromShare(line); err == nil {
		video := func(e *store.Episode) core.CaptureResult {
			return core.CaptureResult{ID: "episode:" + strconv.FormatInt(e.ID, 10), Title: e.Title, URL: episodeURL(e.ID)}
		}
		e, created, err := s.addVideo(ctx, uid, line, func(tx *sql.Tx, e *store.Episode) error {
			res = video(e)
			return op.Save(ctx, tx, res)
		})
		if err != nil {
			return refused(err)
		}
		if !created {
			// it was in Picks already: nothing was added, the answer is still remembered
			res = video(e)
			if err := s.keepReceipt(ctx, op, res); err != nil {
				return refused(err)
			}
		}
		return &res, nil
	}

	addr, ok := captureAddress(line)
	if !ok {
		return nil, fmt.Errorf("%w: paste the address of a YouTube video, or of a podcast feed, a YouTube channel or a playlist to follow", core.ErrCapture)
	}
	_, err = s.subscribe(ctx, uid, subscribeInput{URL: addr, kept: func(tx *sql.Tx, p *store.Podcast) error {
		res = core.CaptureResult{ID: "show:" + strconv.FormatInt(p.ID, 10), Title: p.Title, URL: showURL(p.ID)}
		return op.Save(ctx, tx, res)
	}})
	if err != nil {
		return refused(err)
	}
	return &res, nil
}

// keepReceipt remembers the answer to a quick line that added nothing new.
func (s *Server) keepReceipt(ctx context.Context, op *core.Operation, res core.CaptureResult) error {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := op.Save(ctx, tx, res); err != nil {
		return err
	}
	return tx.Commit()
}
