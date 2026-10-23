# Listening history and offline playback

Pocket Day reads actual listening deltas, not resume positions or episode lengths.
All endpoints use the authenticated person's account. A Pocket Core app link with
`history:read` can read the two GET endpoints; it cannot submit playback reports.

## Reading daily history

`GET /api/v1/listens?date=YYYY-MM-DD` returns:

```json
{"date":"2026-10-21","timezone":"Europe/Sarajevo","listens":[{"episode_id":42,"title":"Episode","podcast":"Show","seconds":120,"duration":3600,"first_at":"2026-10-21T05:15:00Z","last_at":"2026-10-21T05:17:00Z"}]}
```

Dates follow the server's configured `TZ`. `first_at` and `last_at` are the earliest
and latest listening report timestamps, not exact session boundaries. Historical
rows without known times omit these fields. Empty days return `listens: []`.
The response contains all episodes for that date, without pagination.

`GET /api/v1/listens/dates` returns `{"dates":[{"date":"2026-10-21","updated_at":"..."}]}`.
`updated_at` is the server ingestion time and changes when listening seconds change.
Day checks this index every minute and re-fetches changed dates, including frozen
historical days. It caches the index revision only after a successful fetch.

## Sending retry-safe, dated progress

The existing `POST /api/v1/episodes/{id}/progress` also accepts:

```json
{
  "report_id": "a-persisted-unique-UUID",
  "listened_at": "2026-10-21T07:17:00+02:00",
  "position": 720,
  "listened": 60,
  "completed": false,
  "duration": 3600,
  "client": "stable-device-identifier"
}
```

- `report_id` is unique per user, 1–128 characters. Persist it with the report before
  sending. Retry the same ID, episode, position, listened seconds, completion flag,
  and timestamp unchanged until acknowledged. A successful retry has `applied:false`.
- `listened_at` is the original playback sample time, RFC3339 with timezone. It can
  be in the past, but no more than five minutes in the future. Both this field and
  `report_id` must be supplied together; `completed` must be explicit for dated reports.
- `listened` is an integer delta of actual listening time, 0–3600 seconds. Do not
  use the absolute resume position or count seeks as listening. `position` is the
  resume position, 0–604800 seconds. Split reports at midnight in the server's TZ;
  do not batch an entire multi-day offline session into one report.
- Reports are receipted and counted in one database transaction. Reusing an ID with
  different accounting fields returns HTTP 409. Validation errors return 400; an
  episode the person cannot access returns 404. Retain failed reports for diagnosis.
- Old reports add listening to the original date without rewinding a newer resume
  position or marking newer playback completed. The response's `position` and
  `completed` describe current server state. A client actively playing must not seek
  backwards merely because a delayed report was acknowledged.
- The receipt and listening delta are atomic; episode duration metadata is handled
  by the existing progress handler. Dated reports do not trigger file retention cleanup.
- Only newly applied reports from the last 30 seconds update live now-playing state.
  Delayed offline uploads do not appear as currently playing.
- Old clients may omit all new fields. Their existing behavior remains: seconds are
  added to the current server day, and retries are not idempotent.

The iPhone app must keep a durable outbox per account, episode and date. A global
in-memory minute counter cannot safely attribute listening across episode changes,
account changes, app termination, or a delayed reconnect. Persist pending reports,
flush on reconnect and foreground, and remove only acknowledged reports. Keep
unfinished seconds durable too; pausing before one minute must not lose them.

The server cannot reconstruct listening that the phone never recorded or uploaded.
Local resume positions alone are insufficient evidence of listening duration.
