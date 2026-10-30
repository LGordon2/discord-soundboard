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

Full snapshots now reserve retained sound IDs before assigning additions to slots
freed by removed IDs. A replacement can therefore appear even if no intermediate
delete-only snapshot was received. Metadata changes also produce card updates,
and identical snapshots no longer resend unchanged cards. Board state is
published before optional CDN archiving, so downloading a newly discovered sound
does not hold up its readiness indication. Regression tests cover full-board
replacement, metadata/deletion deltas, repeated snapshots, and duplicate IDs.

Floating notifications now include an accessible close button. Timed success
messages show a shrinking countdown bar over their 3.5-second lifetime; errors
remain until dismissed or replaced.

## Delivery and layout stability

A public-site check found an initial 160,633-byte WebSocket snapshot containing
596 library cards, with no compression negotiated. HTTP assets were already
compressed by the proxy. The application now negotiates permessage-deflate at
BestSpeed for browser WebSockets. A 600-card regression test transfers 195,548
bytes uncompressed versus 4,600 bytes compressed, including the upgrade handshake
(97.6% smaller). Clients that do not negotiate compression continue to work.

Hashed assets receive `public, max-age=31536000, immutable`; HTML revalidates with
`no-cache` so deployments load their new asset URLs. Missing files are not cached
immutably, and range requests retain normal FileServer behavior.

Automatic playback notifications now return before constructing Audio objects
when the browser has autoplay off or is muted. Manual preview behavior remains.
Send-button highlights preload metadata rather than requesting full audio.
Only voice events that change the shared user's channel membership refresh the
board, and identical soundboard snapshots no longer broadcast unchanged cards.

The supplied Lighthouse report had FCP/LCP of 0.5 s, zero blocking time, and CLS
of 0.498. Layout stability is therefore the primary target, not further minifying
extension scripts. The HTML reserves the board and initial library space, restores
preview layout before paint, and suppresses the empty-library message while data
is loading. The initial board/library arrive in one atomic DOM update. Preview
board cards reserve description space; basic doctype, language, main landmark,
meta description, and per-sound download labels are present. A fresh Lighthouse
run without browser extensions is needed to measure the resulting score.

Cloudflare's injected analytics beacon and browser-extension scripts shown in the
report are outside the repository's bundle. Cloudflare Web Analytics can be
disabled separately if only the local analytics are desired.

After deployment, the public TLS/WebSocket connection through receipt of the
same 160,858-byte decoded snapshot used 164,800 bytes without compression and
14,613 bytes with it (91.1% less, including TLS/upgrade overhead). Public JS/CSS
responses also confirmed the one-year immutable cache policy. These are transfer
measurements; a replacement Lighthouse run is still needed to quantify CLS.

Uploaded preview cards now use compact height when the description is empty and
reserve extra height only for a real description. The initial HTML includes small
per-slot sizing hints derived from the current board/metadata caches; these are
removed after the browser has both snapshots. This avoids making every card tall
while still reserving the known description heights before paint. Generated HTML
uses a content-derived ETag so a description being added/cleared invalidates its
initial layout hint correctly.

## Correlated switching diagnostics and indexed library updates

Each tracked switch logs `switch_timing operation=<epoch>-<token>` records to the
service journal. Stages `prepare`, `delete`, and `create` report `duration_ms` and
success. Delete/create include DiscordGo's rate-limit waits and retries. The
`accepted` stage reports elapsed time through the successful upload response.
Completion records distinguish `confirmed`, `failed`, `timeout`, and `missing_id`.
Confirmed completion includes `gateway_wait_ms` (acceptance until the confirming
card is prepared for broadcast) and `total_ms`. If the Gateway beats the HTTP
response, the existing board refresh confirms it immediately after acceptance.
This measures the service's confirmation path, not Discord client's audio
readiness or browser delivery. No credentials, audio, or descriptions are logged.

Inspect with:

```sh
journalctl -u discord-soundboard.service --since '10 minutes ago' --grep switch_timing
```

The browser indexes library cards by name after a library replacement. Uploaded
name changes update only affected cards and adjust cached availability counts;
loading-state changes update only affected add buttons. A capacity transition
still updates all add buttons because their permissions actually change. Full
search/filter/metadata work remains for changes that require it. Regression tests
verify ordinary slot/loading events perform no full library scans, counts stay
correct, and replacing the library invalidates the index, including file variants.

HTML responses now include `Cache-Control: no-cache, no-transform`. The
`no-transform` directive prevents Cloudflare's automatic Web Analytics beacon
injection while HTML still revalidates. Local SQLite analytics are unaffected.
See https://developers.cloudflare.com/web-analytics/get-started/ .
