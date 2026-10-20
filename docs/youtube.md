# YouTube

A YouTube subscription is a channel or playlist whose videos become audio episodes on this box.

**Adding.** Search channels by name on Add → YouTube (the Search tab lists the best matches too) and press **+ Subscribe**, or paste `https://www.youtube.com/@Fireship`, `/channel/UC…`, a playlist link (unlisted ones work — keep a "Watch later" playlist you control) or `@handle`. Channels are read through their `/videos` tab so only uploads appear (no Shorts tab, no live tab; Shorts are still uploads, so use the minimum length to keep them out). A single video goes into your "Picks" show (where single episodes picked from podcast feeds land too).

**Indexing.** `yt-dlp --flat-playlist -J --playlist-end N` lists the newest N videos (`YOUTUBE_INDEX_LIMIT`, 30) in about a second, with approximate upload dates. Videos that fail the filters (minimum length, title regex, live) are not even listed. Refresh runs on the same schedule as podcasts.

**Downloading.** On subscribe the newest `download_limit` videos are queued; after that every new upload. Older ones can be fetched from the podcast page ("Download 5 more") or per episode. The download is `yt-dlp -f bestaudio[ext=m4a]/bestaudio -x --audio-format m4a --embed-metadata --write-thumbnail`; the real upload date, description, duration and thumbnail replace what the index guessed, and the file is named `YYYY-MM-DD Title [id].m4a` under `data/media/<podcast id>/`.

**SponsorBlock.** With the subscription's switch on (default), `--sponsorblock-mark all` records the community's segments with the episode. The player skips the categories ticked in Settings → Playback and shows them on the progress bar; seek back into one and it plays. Nothing is cut from the file, so you can turn skipping off later.

**Retention.** `retention_days` per subscription (default from Settings → Feeds & downloads) removes files that long after download, hourly. Saved and queued episodes are never removed. A removed episode stays in the list as "File removed" and can be downloaded again.

**Public feed.** A YouTube feed lists downloaded episodes only — what Apple Podcasts or Overcast can actually play. With "Episode date = Downloaded" old videos you add to a playlist later sort to the top instead of a year deep.

**When it breaks.** YouTube changes often. Settings → Feeds & downloads shows the yt-dlp version and an **Update now** button (pip into `data/ytdlp`, no rebuild). Errors show on the episode ("Download failed: …"), on the Downloads page and under Settings → Activity. `YTDLP_COOKIES` points at a `cookies.txt` for age-restricted content; `YTDLP_EXTRA_ARGS` passes anything else.
