package store

import (
	"context"
	"time"
)

// Stats is a person's listening summarised.
type Stats struct {
	TotalSeconds  int       `json:"total_seconds"`
	YearSeconds   int       `json:"year_seconds"`
	MonthSeconds  int       `json:"month_seconds"`
	WeekSeconds   int       `json:"week_seconds"`
	Completed     int       `json:"completed"`
	CompletedYear int       `json:"completed_year"`
	Started       int       `json:"started"`
	Podcasts      int       `json:"podcasts"`
	Episodes      int       `json:"episodes"`
	Downloaded    int       `json:"downloaded"`
	DownloadedMB  int       `json:"downloaded_mb"`
	ActiveDays    int       `json:"active_days"`
	Streak        int       `json:"streak"`
	FirstListen   string    `json:"first_listen"`
	Days          []DayStat `json:"days"`   // last 90 days
	Months        []DayStat `json:"months"` // last 12 months
	Hours         []int     `json:"hours"`  // 24 buckets of last_played_at (approximate)
	TopPodcasts   []PodStat `json:"top_podcasts"`
	TopEpisodes   []EpStat  `json:"top_episodes"`
	Weekdays      []int     `json:"weekdays"` // Mon..Sun seconds
}

// DayStat is listening on one day or month.
type DayStat struct {
	Day     string `json:"day"`
	Seconds int    `json:"seconds"`
}

// PodStat ranks a subscription.
type PodStat struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Seconds  int    `json:"seconds"`
	Episodes int    `json:"episodes"`
}

// EpStat ranks an episode.
type EpStat struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Podcast string `json:"podcast"`
	Seconds int    `json:"seconds"`
}

// Stats computes a person's listening statistics.
func (s *Store) Stats(ctx context.Context, userID int64) (*Stats, error) {
	st := &Stats{Days: []DayStat{}, Months: []DayStat{}, Hours: make([]int, 24), Weekdays: make([]int, 7), TopPodcasts: []PodStat{}, TopEpisodes: []EpStat{}}
	now := time.Now()
	sum := func(since string) int {
		var n int
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(seconds), 0) FROM listens WHERE user_id = ? AND day >= ?`, userID, since).Scan(&n)
		return n
	}
	st.TotalSeconds = sum("")
	st.YearSeconds = sum(now.Format("2006") + "-01-01")
	st.MonthSeconds = sum(now.Format("2006-01") + "-01")
	st.WeekSeconds = sum(now.AddDate(0, 0, -6).Format("2006-01-02"))
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(CASE WHEN completed = 1 THEN 1 END), COUNT(CASE WHEN completed = 1 AND completed_at >= ? THEN 1 END), COUNT(CASE WHEN last_played_at <> '' THEN 1 END) FROM progress WHERE user_id = ?`, now.Format("2006")+"-01-01", userID).Scan(&st.Completed, &st.CompletedYear, &st.Started)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM podcasts WHERE user_id = ?`, userID).Scan(&st.Podcasts)
	var bytes int64
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1), COUNT(CASE WHEN e.download = 'done' THEN 1 END), COALESCE(SUM(CASE WHEN e.download = 'done' THEN e.file_bytes END), 0) FROM episodes e JOIN podcasts p ON p.id = e.podcast_id WHERE p.user_id = ?`, userID).Scan(&st.Episodes, &st.Downloaded, &bytes)
	st.DownloadedMB = int(bytes >> 20)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(DISTINCT day), COALESCE(MIN(day), '') FROM listens WHERE user_id = ? AND seconds > 0`, userID).Scan(&st.ActiveDays, &st.FirstListen)

	// days (last 90) and streak
	byDay := map[string]int{}
	rows, err := s.DB.QueryContext(ctx, `SELECT day, SUM(seconds) FROM listens WHERE user_id = ? AND day >= ? GROUP BY day`, userID, now.AddDate(0, 0, -400).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d string
		var n int
		if err := rows.Scan(&d, &n); err != nil {
			rows.Close()
			return nil, err
		}
		byDay[d] = n
	}
	rows.Close()
	for i := 89; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		st.Days = append(st.Days, DayStat{Day: d, Seconds: byDay[d]})
	}
	for i := 0; i < 400; i++ {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		if byDay[d] < 60 {
			if i == 0 {
				continue // today may still come
			}
			break
		}
		st.Streak++
	}
	for d, n := range byDay {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			st.Weekdays[(int(t.Weekday())+6)%7] += n
		}
	}
	// months (last 12)
	for i := 11; i >= 0; i-- {
		m := time.Date(now.Year(), now.Month()-time.Month(i), 1, 0, 0, 0, 0, time.Local)
		var n int
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(seconds), 0) FROM listens WHERE user_id = ? AND day >= ? AND day < ?`, userID, m.Format("2006-01-02"), m.AddDate(0, 1, 0).Format("2006-01-02")).Scan(&n)
		st.Months = append(st.Months, DayStat{Day: m.Format("2006-01"), Seconds: n})
	}
	// hour of day (from last_played_at, local)
	hrows, err := s.DB.QueryContext(ctx, `SELECT last_played_at FROM progress WHERE user_id = ? AND last_played_at <> ''`, userID)
	if err == nil {
		for hrows.Next() {
			var ts string
			if hrows.Scan(&ts) == nil {
				if t := ParseTime(ts); !t.IsZero() {
					st.Hours[t.Local().Hour()]++
				}
			}
		}
		hrows.Close()
	}
	// top podcasts
	prows, err := s.DB.QueryContext(ctx, `SELECT p.id, p.title, COALESCE(SUM(l.seconds), 0), COUNT(DISTINCT l.episode_id) FROM listens l
		JOIN episodes e ON e.id = l.episode_id JOIN podcasts p ON p.id = e.podcast_id WHERE l.user_id = ? GROUP BY p.id ORDER BY 3 DESC LIMIT 10`, userID)
	if err != nil {
		return nil, err
	}
	for prows.Next() {
		var p PodStat
		if err := prows.Scan(&p.ID, &p.Title, &p.Seconds, &p.Episodes); err != nil {
			prows.Close()
			return nil, err
		}
		st.TopPodcasts = append(st.TopPodcasts, p)
	}
	prows.Close()
	erows, err := s.DB.QueryContext(ctx, `SELECT e.id, e.title, p.title, COALESCE(SUM(l.seconds), 0) FROM listens l
		JOIN episodes e ON e.id = l.episode_id JOIN podcasts p ON p.id = e.podcast_id WHERE l.user_id = ? GROUP BY e.id ORDER BY 4 DESC LIMIT 10`, userID)
	if err != nil {
		return nil, err
	}
	defer erows.Close()
	for erows.Next() {
		var e EpStat
		if err := erows.Scan(&e.ID, &e.Title, &e.Podcast, &e.Seconds); err != nil {
			return nil, err
		}
		st.TopEpisodes = append(st.TopEpisodes, e)
	}
	return st, nil
}
