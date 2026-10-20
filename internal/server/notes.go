package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

// Notes about an episode live in Pocket Notes, as notes that link to the
// episode's page here (at a moment of it: ?t=seconds); through Pocket Core
// (link notes:read) the episode's page lists them and offers to start one.
// See pocket-homelab-notes docs/notes-from-apps.md.

var notesUse = core.Use{App: "notes", Scopes: []string{"notes:read"}, Reason: "Your notes about an episode (in Pocket Notes), listed on the episode's page"}

// notesBox is what a page shows under "Notes": the person's notes about the
// thing, or why there are none to show.
type notesBox struct {
	Available bool       `json:"available"`
	Missing   bool       `json:"missing,omitempty"` // no Pocket Notes here at all: the page shows no box
	Reason    string     `json:"reason,omitempty"`
	Notes     []noteLink `json:"notes"`
	NewURL    string     `json:"new_url,omitempty"`
	NotesURL  string     `json:"notes_url,omitempty"`
}

type noteLink struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Folder   string `json:"folder"`
	Excerpt  string `json:"excerpt"`
	Modified int64  `json:"modified"`
	URL      string `json:"url"`
}

var notesHTTP = &http.Client{Timeout: 10 * time.Second}

// notesAbout asks Pocket Notes, as the person, for their notes linking to
// item (this app's public address of a thing), and builds the link that
// starts a new one (title, link label, folder).
func (s *Server) notesAbout(ctx context.Context, email, item, title, label, folder string) notesBox {
	box := notesBox{Notes: []noteLink{}}
	if !s.core.Configured() || s.cfg.BaseURL == "" {
		box.Missing, box.Reason = true, "Notes come from Pocket Notes through Pocket Core, which this Pocket Podcasts is not joined to."
		return box
	}
	lt, err := s.core.ActAs(ctx, email, "notes", "notes:read")
	if err != nil {
		var le *core.LinkError
		if !errors.As(err, &le) {
			box.Reason = "Pocket Core does not answer right now, so your notes cannot be shown."
			return box
		}
		box.Missing, box.Reason = linkReason(le)
		return box
	}
	base := strings.TrimRight(lt.URL, "/")
	pub := strings.TrimRight(lt.PublicURL, "/")
	if pub == "" {
		pub = base
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/mentions?url="+url.QueryEscape(item), nil)
	req.Header.Set("Authorization", "Bearer "+lt.AccessToken)
	req.Header.Set("Accept", "application/json")
	res, err := notesHTTP.Do(req)
	if err != nil {
		box.Reason = "Pocket Notes does not answer right now."
		return box
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		box.Reason = fmt.Sprintf("Pocket Notes answered HTTP %d.", res.StatusCode)
		return box
	}
	var found []struct {
		ID       int64  `json:"id"`
		Title    string `json:"title"`
		Category string `json:"category"`
		Excerpt  string `json:"excerpt"`
		Modified int64  `json:"modified"`
	}
	if err := json.Unmarshal(raw, &found); err != nil {
		box.Reason = "Pocket Notes answered with something unexpected."
		return box
	}
	for _, n := range found {
		box.Notes = append(box.Notes, noteLink{ID: n.ID, Title: n.Title, Folder: n.Category, Excerpt: n.Excerpt, Modified: n.Modified, URL: pub + "/note/" + strconv.FormatInt(n.ID, 10)})
	}
	box.Available = true
	box.NotesURL = pub
	box.NewURL = pub + "/new?" + url.Values{"title": {title}, "url": {item}, "label": {label}, "folder": {folder}}.Encode()
	return box
}

// linkReason says, for a person, why the link to Pocket Notes does not work
// and where it is turned on; missing is true when there is nothing to turn on.
func linkReason(le *core.LinkError) (missing bool, why string) {
	switch le.Reason {
	case "target_missing":
		return true, "Pocket Notes is not on this box."
	case "off_person":
		return false, "Showing your notes is switched off for you. Turn on “Pocket Podcasts → Pocket Notes” in Pocket Core, Settings → Apps acting for me."
	case "off_admin":
		return false, "Showing notes is switched off for this box. An admin turns on “Pocket Podcasts → Pocket Notes” in Pocket Core, Links."
	case "not_in_audience":
		return false, "You have no Pocket Notes yet. An admin can give you access in Pocket Core (Apps → Pocket Notes → who can use it)."
	case "not_offered", "not_declared":
		return false, "This Pocket Notes or Pocket Podcasts is too old to link the two; update both."
	case "unknown_user":
		return false, "Your account is not in Pocket Core, so Pocket Podcasts cannot ask Pocket Notes for you."
	}
	return false, "Pocket Notes: " + le.Message
}

// GET /api/v1/episodes/{id}/notes
func (s *Server) handleEpisodeNotes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, err := s.store.GetEpisode(ctx, me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "episode notes")
		return
	}
	u := userFrom(ctx)
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "sign in")
		return
	}
	label := "🎧 " + e.Title
	if p := strings.TrimSpace(e.PodcastTitle); p != "" && !strings.HasSuffix(e.Title, p) {
		label += " — " + p
	}
	item := s.cfg.BaseURL + "/episodes/" + strconv.FormatInt(e.ID, 10)
	writeJSON(w, http.StatusOK, s.notesAbout(ctx, u.Email, item, e.Title, label, "Podcasts"))
}
