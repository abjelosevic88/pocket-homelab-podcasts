package server

import (
	"net/http"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.Dashboard(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "dashboard")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// handleHome is everything the Home page shows in one request.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	cont, err := s.store.ListEpisodes(ctx, store.EpisodeFilter{UserID: uid, Status: "partial", History: true, Sort: "played", Limit: 8})
	if err != nil {
		s.fail(w, err, "home")
		return
	}
	queue, _ := s.store.ListEpisodes(ctx, store.EpisodeFilter{UserID: uid, Queued: true, Sort: "queue", Limit: 8})
	fresh, _ := s.store.ListEpisodes(ctx, store.EpisodeFilter{UserID: uid, Status: "unplayed", Sort: "newest", Limit: 30})
	recent, _ := s.store.ListEpisodes(ctx, store.EpisodeFilter{UserID: uid, History: true, Status: "played", Sort: "played", Limit: 6})
	pods, _ := s.store.ListPodcasts(ctx, uid, "", "", "recent")
	d, _ := s.store.Dashboard(ctx, uid)
	podsOut := make([]podcastJSON, 0, len(pods))
	for i := range pods {
		if i == 12 {
			break
		}
		podsOut = append(podsOut, s.podcastOut(&pods[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"continue": s.episodesOut(cont), "queue": s.episodesOut(queue), "fresh": s.episodesOut(fresh), "recent": s.episodesOut(recent),
		"podcasts": podsOut, "dashboard": d,
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Stats(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "stats")
		return
	}
	writeJSON(w, http.StatusOK, st)
}
