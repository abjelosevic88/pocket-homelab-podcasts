# Migrating from PinePods

1. Create your account in Pocket Podcasts and an API token (Settings → API tokens).
2. Run the importer against the PinePods database. It only reads.

```
scripts/import-pinepods.py --docker pinepods_db --user admin > pinepods.json
# or straight into the app:
scripts/import-pinepods.py --docker pinepods_db --user admin --youtube-native \
    --push https://podcasts.example.com --token pp_…
```

`--docker NAME` uses `docker exec NAME psql`; `--dsn` takes a connection string. `--db-user` / `--db-name` default to PinePods' `pinepods` / `pinepods_database`.

3. Without `--push`, import the file under Settings → Backup & data → Import JSON.

What comes over: every subscription (title, feed, auto-download, intro/outro skip, speed, notifications, cut-off, favourite) and, for every episode you touched, position, played state, play count, saved and queued flags. Episodes are matched by GUID, then by title. Subscriptions already present are not duplicated; their history is still filled in.

`--youtube-native` turns PinePods subscriptions whose website is a YouTube channel (the ones served through Pinchflat feeds) into real YouTube subscriptions: Pocket Podcasts indexes and downloads them itself (newest 3, every upload after, 7-day retention, Shorts under 120 s skipped, episode date = download date). Leave it out to keep following the old RSS feeds as they are.

PinePods playlists, gpodder sync state, themes and the people database are not migrated.
