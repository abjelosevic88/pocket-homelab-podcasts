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
