package server

import (
	"context"
	"database/sql"
	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-podcasts/internal/store"
)

func (s *Server) personEvent(ctx context.Context, uid int64, build func() (string, string, map[string]any)) core.CommitHook {
	if !s.core.Configured() {
		return nil
	}
	u, err := s.store.GetUser(ctx, uid)
	return func(tx *sql.Tx) error {
		if err != nil {
			return err
		}
		topic, subject, p := build()
		p["user_id"] = uid
		p["user_email"] = u.Email
		return core.QueueEvent(ctx, tx, topic, subject, p)
	}
}
func (s *Server) episodeAdded(ctx context.Context, p *store.Podcast, e *store.Episode) core.CommitHook {
	return s.personEvent(ctx, p.UserID, func() (string, string, map[string]any) {
		return "podcasts.episode_added", e.Title, map[string]any{"podcast_id": p.ID, "podcast": p.Title, "episode_id": e.ID, "title": e.Title, "pub_date": e.PubDate, "duration": e.Duration}
	})
}
func (s *Server) progressEvent(ctx context.Context, uid int64, e *store.Episode, manual bool) func(*sql.Tx, bool, bool) error {
	play := s.personEvent(ctx, uid, func() (string, string, map[string]any) {
		return "podcasts.played", e.Title, map[string]any{"podcast_id": e.PodcastID, "podcast": e.PodcastTitle, "episode_id": e.ID, "title": e.Title, "duration": e.Duration}
	})
	finish := s.personEvent(ctx, uid, func() (string, string, map[string]any) {
		p := map[string]any{"podcast_id": e.PodcastID, "podcast": e.PodcastTitle, "episode_id": e.ID, "title": e.Title, "duration": e.Duration}
		if manual {
			p["manual"] = true
		}
		return "podcasts.finished", e.Title, p
	})
	return func(tx *sql.Tx, firstPlay, newCompletion bool) error {
		if firstPlay && play != nil {
			if err := play(tx); err != nil {
				return err
			}
		}
		if newCompletion && finish != nil {
			return finish(tx)
		}
		return nil
	}
}
