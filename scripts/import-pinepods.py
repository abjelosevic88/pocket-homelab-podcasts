#!/usr/bin/env python3
"""Move from PinePods to Pocket Podcasts.

Reads a PinePods PostgreSQL database (through `docker exec` on the db
container, or any `psql` connection string) and writes the JSON that
Pocket Podcasts' Settings → Backup & data → Import JSON (or POST /api/v1/import)
accepts: every subscription with its settings, and for every episode you
touched the position, played state, play count, saved and queued flags.

PinePods podcasts whose feed is a YouTube channel (website youtube.com/@…)
can be imported as native YouTube subscriptions (downloaded by Pocket Podcasts
itself) with --youtube-native; otherwise the RSS feed URL is kept as it is.

Examples
  ./import-pinepods.py --docker pinepods_db --user admin > pinepods.json
  ./import-pinepods.py --docker pinepods_db --user admin --youtube-native \
      --push https://podcasts.example.com --token pp_…

No write ever touches PinePods.
"""
import argparse
import json
import subprocess
import sys
import urllib.request


def psql(args, sql):
    cmd = ["docker", "exec", args.docker, "psql", "-U", args.db_user, "-d", args.db_name, "-At", "-F", "\x1f", "-c", sql] if args.docker \
        else ["psql", args.dsn, "-At", "-F", "\x1f", "-c", sql]
    out = subprocess.run(cmd, check=True, capture_output=True, text=True).stdout
    return [line.split("\x1f") for line in out.splitlines() if line]


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--docker", help="PinePods db container name (uses docker exec psql)")
    ap.add_argument("--dsn", help="psql connection string instead of --docker")
    ap.add_argument("--db-user", default="pinepods")
    ap.add_argument("--db-name", default="pinepods_database")
    ap.add_argument("--user", required=True, help="PinePods username to export")
    ap.add_argument("--youtube-native", action="store_true", help="turn YouTube-backed feeds into native YouTube subscriptions")
    ap.add_argument("--push", metavar="BASE_URL", help="POST the result to a Pocket Podcasts instance")
    ap.add_argument("--token", help="Pocket Podcasts API token for --push")
    args = ap.parse_args()
    if not args.docker and not args.dsn:
        ap.error("--docker or --dsn is required")

    rows = psql(args, f"select userid from \"Users\" where username = '{args.user}'")
    if not rows:
        sys.exit(f"no PinePods user {args.user!r}")
    uid = rows[0][0]

    pods = psql(args, f"""select podcastid, podcastname, feedurl, coalesce(websiteurl,''), coalesce(autodownload::int,0), coalesce(startskip,0), coalesce(endskip,0),
        coalesce(playbackspeed,1), coalesce(notificationsenabled::int,0), coalesce(feedcutoffdays,0), coalesce(isfavorite::int,0)
        from "Podcasts" where userid = {uid} order by podcastid""")
    out = {"version": "pinepods-import", "podcasts": [], "playlists": []}
    by_id = {}
    for pid, name, feed, site, autodl, start, end, speed, notify, cutoff, fav in pods:
        p = {"title": name, "feed_url": feed, "site_url": site, "kind": "rss", "auto_download": autodl == "1", "start_skip": int(start), "end_skip": int(end),
             "playback_speed": float(speed) if float(speed) != 1 else 0, "notify": notify == "1", "feed_cutoff_days": int(cutoff), "favorite": fav == "1",
             "pub_date_mode": "published", "download_limit": 3, "retention_days": 0, "min_duration": 0, "title_filter": "", "episodes": []}
        if args.youtube_native and "youtube.com" in site:
            p["kind"] = "youtube"
            p["feed_url"] = site  # Pocket Podcasts resolves @handles and playlists itself
            p["pub_date_mode"] = "downloaded"
            p["min_duration"] = 120
            p["retention_days"] = 90 if "list=" in site else 7  # a playlist you curate keeps longer than a channel firehose
        out["podcasts"].append(p)
        by_id[pid] = p

    eps = psql(args, f"""select e.podcastid, e.episodeguid, e.episodetitle, coalesce(to_char(e.episodepubdate, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
        coalesce(h.listenduration, 0), coalesce(e.completed::int, 0), coalesce(to_char(h.listendate, 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
        (s.episodeid is not null)::int, coalesce(q.queueposition, 0)
        from "Episodes" e
        left join "UserEpisodeHistory" h on h.episodeid = e.episodeid and h.userid = {uid}
        left join "SavedEpisodes" s on s.episodeid = e.episodeid and s.userid = {uid}
        left join "EpisodeQueue" q on q.episodeid = e.episodeid and q.userid = {uid}
        where h.episodeid is not null or s.episodeid is not null or q.episodeid is not null or e.completed""")
    n = 0
    for pid, guid, title, pub, pos, done, played_at, saved, qpos in eps:
        p = by_id.get(pid)
        if not p:
            continue
        p["episodes"].append({"guid": guid, "title": title, "pub_date": pub, "position": int(pos), "completed": done == "1", "last_played_at": played_at,
                              "play_count": 1 if played_at else 0, "saved": saved == "1", "queue_pos": int(qpos)})
        n += 1
    sys.stderr.write(f"{len(out['podcasts'])} podcasts, {n} episodes with history/saved/queue\n")

    data = json.dumps(out, ensure_ascii=False, indent=1)
    if args.push:
        if not args.token:
            sys.exit("--push needs --token")
        req = urllib.request.Request(args.push.rstrip("/") + "/api/v1/import", data=data.encode(), method="POST",
                                     headers={"Content-Type": "application/json", "Authorization": "Bearer " + args.token})
        with urllib.request.urlopen(req, timeout=900) as res:
            print(res.read().decode())
    else:
        print(data)


if __name__ == "__main__":
    main()
