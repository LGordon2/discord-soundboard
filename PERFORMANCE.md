# Replacement latency

The browser immediately POSTs `/swap-sound`. The backend waits for Discord to
DELETE the old sound, then POSTs the replacement audio. DiscordGo v0.28.1 waits
for exhausted rate-limit buckets and automatically retries HTTP 429 responses.
There is no fixed application sleep in this path.

Discord's [soundboard API](https://docs.discord.com/developers/resources/soundboard)
only permits metadata changes through PATCH; it cannot replace audio in place.
With a full board, deletion must finish before creation. Running these requests
concurrently risks losing the old sound while creation fails for lack of capacity.

Recent service log samples show DELETE calls around 151–498 ms and CREATE calls
around 233–612 ms. Sequential pairs commonly total roughly 0.4–0.8 seconds before
Discord client propagation. These samples do not explain long outliers or measure
client availability. Gateway reconnect errors also appear; those can independently
delay this application's updates after Discord has already accepted the audio.

Implemented improvements:

- Base64 uploads are cached at library load, removing repeated encoding and buffer
  copying from switching. The cache replaces raw audio with data URIs, costing
  roughly 33% more persistent audio memory. JSON serialization/upload still occur.
- Disk fallbacks finish before deletion. Missing/empty replacements preserve the
  current sound. Full filename cache keys distinguish MP3 and Ogg variants, and
  uploads use valid MIME types.
- CDN download bodies are closed, and failed HTTP downloads cannot become cached
  audio files. This fixes archive resource handling; it removes no API round trip.
- Individual HTTP attempt timings and rate-limit headers are logged separately
  from create/delete durations (including waits/retries) and total swap duration.
  Authorization and audio are not logged. HTTP timings stop at response headers;
  method timings include body consumption. Neither measures Discord client
  Gateway processing or audio readiness.

During a slow switch, compare individual HTTP attempts with method durations to
identify bucket waits/retries versus network/Discord processing. Respect Discord's
[dynamic rate limits](https://docs.discord.com/developers/topics/rate-limits).
Fresh sessions or buckets must not be used to evade waits. DELETE currently uses
a bucket per sound URL, so shared delete limits can still cause 429 retries.

Further opportunities require runtime evidence or a product change:

- Reliably verified spare guild capacity could allow creation before deletion,
  reducing time without a sound. Two API calls remain; a stale local sound count
  or the hardcoded eight-slot board cannot safely establish spare capacity.
- Keeping the catalog uploaded avoids creation latency during selection but
  requires sufficient guild capacity or validated cross-guild playback support.
- Gateway handlers synchronously archive unknown sounds and fetch a full sound
  list after create/delete. Improving that path can speed this application's
  display, but does not make audio available sooner in Discord.

Tests use fake HTTP transports and do not mutate the live Discord board.
`go test -race ./...` checks tested code paths, not live Gateway concurrency.
`go test -run '^$' -bench BenchmarkPrepareStoredSound -benchmem ./...` compares
256 KiB upload preparation with cached lookup; network latency is excluded.

Validation on this ARM64 host: `go test ./...` and `go vet ./...` passed.
The 256 KiB benchmark measured 2.71 ms and 1,056,770 allocated bytes for
encoding per switch versus 134 ns and zero allocations for cached lookup.
This is a small local saving, not an explanation for multi-second Discord delays.

The race-enabled test binary compiled but could not execute on this host:
ThreadSanitizer reports `unsupported VMA range` (`Found 39 - Supported 48`).
Race validation therefore remains unavailable on this ARM64 configuration.

## Browser update overhead

HTMX and its WebSocket extension have been replaced by native fetch, delegated
controls, and targeted WebSocket fragment updates. This removes two runtime CDN
scripts, per-card event initialization, injected update scripts, and the global
DOM mutation observer used by switching feedback. Empty cards are emitted once,
and library filtering scans past free slots. The production JavaScript bundle is
about 19 kB before gzip (6.3 kB gzipped), including the existing touch drag helper;
HTMX is no longer loaded separately. This affects browser overhead and startup,
not Discord's two API operations or rate limits.

Validation includes DOM integration checks against the production bundle for
button requests, URL encoding, disabled permissions, drag/drop, socket reconnects,
fragment order, and switching through intermediate cards. Successful ready
messages clear after 3.5 seconds. The loading spinner rotates continuously.
