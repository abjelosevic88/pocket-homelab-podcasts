package server

import (
	"net/http"
	"time"
)

func (s *Server) handleListens(w http.ResponseWriter, r *http.Request) {
	day := r.URL.Query().Get("date")
	if _, err := time.Parse("2006-01-02", day); err != nil {
		writeErr(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	rows, err := s.store.DailyListens(r.Context(), me(r), day)
	if err != nil {
		s.fail(w, err, "list listening history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"date": day, "timezone": time.Local.String(), "listens": rows})
}

func (s *Server) handleListeningDates(w http.ResponseWriter, r *http.Request) {
	dates, err := s.store.ListeningDates(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "list listening dates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dates": dates})
}
