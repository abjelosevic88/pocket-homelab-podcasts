package store

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestDatedListeningReports(t *testing.T) {
	st, err := Open(t.TempDir()+"/t.db", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	u := &User{Email: "listener@example.com", Role: "admin"}
	if err = st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	other := &User{Email: "other@example.com", Role: "member"}
	if err = st.CreateUser(ctx, other); err != nil {
		t.Fatal(err)
	}
	p := &Podcast{UserID: u.ID, Kind: KindRSS, Title: "Show", FeedURL: "https://example.com/feed"}
	if err = st.CreatePodcast(ctx, p); err != nil {
		t.Fatal(err)
	}
	e := Episode{PodcastID: p.ID, GUID: "e", Title: "Episode", Duration: 1000}
	if _, err = st.UpsertEpisode(ctx, &e); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 3, 28, 23, 59, 50, 0, time.Local)
	later := at.Add(20 * time.Second)
	save := func(id string, pos, secs int, when time.Time) bool {
		t.Helper()
		ok, err := st.SaveProgressReport(ctx, u.ID, e.ID, pos, secs, false, id, when)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !save("new", 200, 20, later) || !save("old", 100, 10, at) {
		t.Fatal("new report not applied")
	}
	if save("old", 100, 10, at) {
		t.Fatal("duplicate applied")
	}
	if _, err = st.SaveProgressReport(ctx, u.ID, e.ID, 100, 11, false, "old", at); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed retry: %v", err)
	}
	got, err := st.GetEpisode(ctx, u.ID, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PlayPosition != 200 || got.LastPlayedAt != later.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("old upload rewound progress: %+v", got)
	}
	for _, day := range []struct {
		at      time.Time
		seconds int
	}{{at, 10}, {later, 20}} {
		rows, err := st.DailyListens(ctx, u.ID, day.at.In(time.Local).Format("2006-01-02"))
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Seconds != day.seconds || rows[0].FirstAt != day.at.UTC().Format(time.RFC3339Nano) {
			t.Fatalf("wrong day totals: %+v", rows)
		}
	}
	if _, err = st.SaveProgressReport(ctx, other.ID, e.ID, 50, 10, false, "old", at); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user wrote: %v", err)
	}
	if rows, err := st.DailyListens(ctx, other.ID, at.Format("2006-01-02")); err != nil || len(rows) != 0 {
		t.Fatalf("other user read: %+v %v", rows, err)
	}
	// A failed transaction must not consume the report ID.
	fail := errors.New("hook failed")
	_, err = st.SaveProgressReport(ctx, u.ID, e.ID, 250, 5, false, "rollback", later, func(*sql.Tx, bool, bool) error { return fail })
	if !errors.Is(err, fail) {
		t.Fatal(err)
	}
	if !save("rollback", 250, 5, later) {
		t.Fatal("failed transaction consumed receipt")
	}
	dates, err := st.ListeningDates(ctx, u.ID)
	if err != nil || len(dates) != 2 || dates[0]["updated_at"] == "" {
		t.Fatalf("dates: %+v %v", dates, err)
	}
	// Existing browser/native clients still work without a report ID.
	if err = st.SaveProgress(ctx, u.ID, e.ID, 300, 12, false); err != nil {
		t.Fatal(err)
	}
	rows, err := st.DailyListens(ctx, u.ID, Today())
	if err != nil || len(rows) != 1 || rows[0].Seconds != 12 {
		t.Fatalf("legacy: %+v %v", rows, err)
	}
}
