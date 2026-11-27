package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core): another
// Pocket app may act here for a person with a pcx_ token, only within these
// scopes. Every route not listed in linkRoutes stays closed to it.

var linkOffers = []core.Offer{
	{Scope: "podcasts:read", Name: "See your subscriptions", Description: "List the podcasts and YouTube channels you follow."},
	{Scope: "history:read", Name: "See what you listened to", Description: "Your listening history, progress and stats (what Pocket Day shows in the journal)."},
	{Scope: "queue:write", Name: "Add to your queue", Description: "Put an episode in your Up next queue."},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core); Register adds move:export/import.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.json", File: "podcasts.json", Kind: core.KindPocket, Title: "Your subscriptions with their settings, where you were in each episode, saved and queued episodes, and your playlists"},
		{Path: "/api/v1/export.opml", File: "podcasts.opml", Kind: core.KindOPMLPodcast, Title: "Your subscriptions as OPML, for any podcast app"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, ContentType: "application/json", MaxBytes: importJSONMax, Title: "Subscribes to the podcasts from another box and brings back progress, saved and queued episodes and playlists; what you already follow is left alone"},
		{Path: "/api/v1/import/opml", Kind: core.KindOPMLPodcast, Form: "file", MaxBytes: importOPMLMax, Title: "Subscribes to every podcast in the OPML file; ones you already follow are skipped"},
		{Path: "/api/v1/import/youtube.csv", Kind: kindYouTubeSubscriptions, Form: "file", MaxBytes: importOPMLMax, Title: "YouTube subscriptions (Google Takeout): follows each channel as a podcast; ones you already follow are skipped"},
	},
}

// capabilities is what Pocket Search, Pocket Day and Pocket Hub may ask of
// Podcasts for a person (docs/capabilities.md in pocket-homelab-core);
// Register adds the pocket:* offers. The routes are in capabilities.go.
var capabilities = core.Capabilities{
	Search: &core.Searchable{What: "Your episodes, by title and show notes, and the shows you follow, by name", Kinds: []string{"episode", "show"}, Bangs: []string{"pod", "podcasts"}},
	Day:    &core.DayFeed{Label: "Podcasts", One: "episode", Many: "episodes", Unit: "min", What: "The episodes you listened to that day, how long for, and the ones you finished"},
	Widget: &core.Widget{Title: "Podcasts", What: "The episodes you are in the middle of and the newest ones you have not played"},
	Capture: &core.QuickCapture{Kinds: []core.CaptureKind{
		{Kind: "link", Label: "Link", What: "A YouTube video is added to your Picks and downloaded; the address of a podcast feed, a YouTube channel or a playlist is followed", Fields: []string{}},
	}},
}

// kindYouTubeSubscriptions is Google Takeout's YouTube subscriptions.csv
// ("Channel Id,Channel Url,Channel Title"); Pocket Move routes it here.
const kindYouTubeSubscriptions = "youtube-subscriptions-csv"

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/podcasts$`), "podcasts:read"},
	{"GET", regexp.MustCompile(`^/api/v1/podcasts/\d+$`), "podcasts:read"},
	{"GET", regexp.MustCompile(`^/api/v1/history$`), "history:read"},
	{"GET", regexp.MustCompile(`^/api/v1/listens(/dates)?$`), "history:read"},
	{"GET", regexp.MustCompile(`^/api/v1/stats$`), "history:read"},
	{"GET", regexp.MustCompile(`^/api/v1/dashboard$`), "history:read"},
	{"GET", regexp.MustCompile(`^/api/v1/episodes$`), "history:read"},
	{"GET", regexp.MustCompile(`^/api/v1/episodes/\d+$`), "history:read"},
	{"POST", regexp.MustCompile(`^/api/v1/episodes/\d+/queue$`), "queue:write"},
}

var errLinkScope = errors.New("outside the link's scope")

func linkAllows(scopes []string, r *http.Request) bool {
	if portable.LinkAllows(scopes, r) || capabilities.LinkAllows(scopes, r) {
		return true
	}
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token and maps it to the person it acts for.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, err
	}
	if !in.Active || !in.Delegated() || in.User == nil || in.User.Email == "" {
		return nil, http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, errLinkScope
	}
	return s.coreIdentityUser(ctx, in)
}
