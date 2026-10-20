package server

import (
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

func (s *Server) handleListPlaylists(w http.ResponseWriter, r *http.Request) {
	pls, err := s.store.ListPlaylists(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "list playlists")
		return
	}
	for i := range pls {
		pls[i].Count, _ = s.store.CountEpisodes(r.Context(), pls[i].Filter())
	}
	writeJSON(w, http.StatusOK, pls)
}

func (s *Server) readPlaylist(w http.ResponseWriter, r *http.Request, p *store.Playlist) bool {
	var in store.Playlist
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return false
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return false
	}
	// only the caller's own subscriptions may be referenced
	if len(in.PodcastIDs) > 0 {
		own, _ := s.store.ListPodcasts(r.Context(), me(r), "", "", "")
		ok := map[int64]bool{}
		for _, o := range own {
			ok[o.ID] = true
		}
		var ids []int64
		for _, id := range in.PodcastIDs {
			if ok[id] {
				ids = append(ids, id)
			}
		}
		in.PodcastIDs = ids
	}
	switch in.Sort {
	case "newest", "oldest", "shortest", "longest", "played":
	default:
		in.Sort = "newest"
	}
	p.Name, p.Description, p.Icon, p.PodcastIDs = in.Name, strings.TrimSpace(in.Description), strings.TrimSpace(in.Icon), in.PodcastIDs
	p.IncludeUnplayed, p.IncludePartial, p.IncludePlayed = in.IncludeUnplayed, in.IncludePartial, in.IncludePlayed
	p.MinDuration, p.MaxDuration, p.MaxAgeDays = clampInt(in.MinDuration, 0, 86400), clampInt(in.MaxDuration, 0, 86400), clampInt(in.MaxAgeDays, 0, 3650)
	p.DownloadedOnly, p.Sort, p.Limit = in.DownloadedOnly, in.Sort, clampInt(in.Limit, 0, 500)
	return true
}

func (s *Server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	p := store.Playlist{UserID: me(r)}
	if !s.readPlaylist(w, r, &p) {
		return
	}
	if err := s.store.CreatePlaylist(r.Context(), &p); err != nil {
		s.fail(w, err, "create playlist")
		return
	}
	p.Count, _ = s.store.CountEpisodes(r.Context(), p.Filter())
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetPlaylist(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPlaylist(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get playlist")
		return
	}
	p.Count, _ = s.store.CountEpisodes(r.Context(), p.Filter())
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdatePlaylist(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPlaylist(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get playlist")
		return
	}
	if !s.readPlaylist(w, r, p) {
		return
	}
	if err := s.store.UpdatePlaylist(r.Context(), p); err != nil {
		s.fail(w, err, "update playlist")
		return
	}
	p.Count, _ = s.store.CountEpisodes(r.Context(), p.Filter())
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePlaylist(r.Context(), me(r), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete playlist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlaylistEpisodes(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPlaylist(r.Context(), me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "get playlist")
		return
	}
	f := p.Filter()
	if l := queryInt(r, "limit"); l > 0 {
		f.Limit = clampInt(l, 1, 500)
	} else if f.Limit == 0 {
		f.Limit = 200
	}
	f.Offset = queryInt(r, "offset")
	eps, err := s.store.ListEpisodes(r.Context(), f)
	if err != nil {
		s.fail(w, err, "list episodes")
		return
	}
	total, _ := s.store.CountEpisodes(r.Context(), f)
	writeJSON(w, http.StatusOK, map[string]any{"playlist": p, "episodes": s.episodesOut(eps), "total": total})
}
