# Discord Soundboard

Discord sound swapper. You can host sound files and page them in and out as needed.

## Usage

1. `go mod tidy` -- Pull in deps
2. Set up environment variables (see environment variables section)
3. `go run .` -- Should start and host everything on :3000.

### Environment Variables

- `AUTH_TOKEN` pull this from a browser Discord call or by other means.
- `SOUNDS_DIR` where server based sounds are hosted. (e.g. `/home/lew/mysounds/`)

#### Unused, but maybe in the future

 - `CLIENT_ID` app client ID. Referenced in code, but not really used in the app yet.
 - `CLIENT_SECRET` app client secret. Same as above.

## Browser client

The UI uses native `fetch` and `WebSocket` APIs with delegated button and drag
handlers. HTMX is no longer required. The backend streams server-rendered HTML
fragments identified by element ID; library state and playback notifications use
data attributes rather than executable scripts. Only changed fragments are
hydrated. Reconnects use capped exponential backoff.

Build the frontend with `pnpm build` (Node 24 is available on the service host),
and run `tsc --noEmit`, `go test ./...`, and `go vet ./...` for checks. Deploy
frontend and backend together when changing the fragment protocol, and refresh
existing browser tabs. The systemd service serves `dist` from this checkout.

## Local SQLite storage

Analytics, sound descriptions, and emoji are stored in `data/soundboard.sqlite3`
relative to the service working directory. Set `DATABASE_FILE` to override the
path. The database uses WAL mode, a five-second busy timeout, full synchronous
commits, primary keys on sound names, and owner-only file permissions. Run one
soundboard service process per database; the application caches current views in
memory and serializes batched database writes.

On first startup, the service imports the existing `data/analytics.json` and
`data/sound-metadata.json` in one transaction. A migration marker prevents repeat
imports, so restarts preserve newer database values. The original JSON files are
left intact as backups and stop receiving updates. `ANALYTICS_FILE` and
`SOUND_METADATA_FILE` select alternate legacy files for this first import only.
A corrupt legacy file stops migration rather than silently resetting data.

Analytics and captured emoji changes commit only changed rows every five seconds,
and flush on SIGTERM/SIGINT. Description edits commit before reporting success.
An abrupt kill can lose the last few seconds of buffered counts/emoji changes.
Failed transactions retain pending changes for retry. The database and WAL files
are excluded from Git. Use SQLite's backup command/API for a live backup so the
WAL is included, e.g. `sqlite3 data/soundboard.sqlite3 '.backup backup.sqlite3'`.

The Go SQLite driver requires CGO and a C compiler when building the backend.
The binary contains SQLite; no separate database server is needed.

## Local analytics

`/analytics` returns totals and per-sound counters as JSON:

- `added`: successful Discord uploads through this app, including replacements.
- `sent`: successful sends to Discord, once per operation even if DiscordGo retries.
- `played`: user-initiated browser previews when playback actually starts. Muted or
  failed previews and automatic playback of sent sounds do not count.

Sound names identify aggregate rows, so renamed sounds get separate rows. Sends
whose name is not on the current board use `sound:<id>`. No IP addresses, cookies,
user identifiers, or audio are collected. Preview reports are best effort. The
analytics endpoint shows sound names/counts to anyone who can access the board.

## Preview UI and production assets

Enable **Preview new UI** at the top of the page for search, favorites, recently
used sounds, descriptions, and visible Preview/Discord labels. It is off by
default. The toggle, favorites, and last 24 recent names stay in this browser's
local storage. Turning preview off restores the existing layout and filters.

Search matches names and descriptions, ignoring case and accents. Partial matches
work directly. Bounded Levenshtein matching permits one edit for words of four to
six characters, and two for longer words. Short words use literal matching.

Styles live in `index.css` under component classes; Tailwind is not required at
build time or in the browser. Vite emits minified CSS/JavaScript, and the build
also minifies `index.html`. The backend minifies live card/library HTML fragments.
Dark mode follows the system preference. Both the uploaded board and the upload library use four columns on desktop, two
on tablets, and one on phones. Eight uploaded slots form two desktop rows.

`pnpm test` builds production assets and runs DOM and search regressions. Node 24
is recommended. For staged assets run `CLIENT_DIST=/path/to/dist pnpm test:client`.
Also run `pnpm exec tsc --noEmit`, `go test ./...`, and `go vet ./...`.
Deploy frontend and backend together when template classes or protocols change.

## Sound descriptions and emoji

In preview mode, use **ⓘ** to edit a sound's description or source. Descriptions
are shared across browsers and committed to SQLite immediately. They appear as
subtext on both library and uploaded cards. The uploader tooltip remains separate.
Entries are keyed by name, so same-named MP3/Ogg variants share details.

Unicode/custom emoji are captured from Discord snapshots, retained when a sound
leaves the board, and reused during re-upload. Previously archived sounds without
recorded metadata cannot have their original emoji inferred. If Discord explicitly
rejects a stored emoji, the upload retries once without it to restore the audio.

Production delivery uses compressed WebSocket snapshots and immutable caching for
hashed assets. HTML revalidates on refresh. Initial board/library placeholders
reserve layout space until the complete snapshot arrives; preview cards expand only when they have description text. The server supplies
initial per-card sizing hints to avoid reserving blank description space. Automatic local echoes do not load audio when
muted or when “Play send sounds” is off.

Clicking **+** and dropping a library sound use the same pending state: a spinner
on the source add button and target slot until Discord confirms the new sound.
Pending sounds/slots cannot be submitted twice; failures restore the controls.

Pending uploads are shared over the WebSocket: other viewers see the target
slot's loading indicator, including when joining midway through an upload. Popup
messages and recent-history updates remain local to the initiating browser.
Slots and sounds already being uploaded are reserved against conflicting requests.
The shared indicator clears with the confirmed Discord card update, on failure,
or after the existing 20-second confirmation timeout. Pending state is in memory
and resets on service restart; sound data and analytics remain in SQLite.

New browsers connect to `/ws?format=json` and receive newline-delimited JSON
board, loading-state, playback, metadata-invalidation, and viewer-count events.
Snapshots include the library; ordinary board updates include only changed slots
and current library availability. Local JavaScript templates escape server data
and hydrate only replaced cards. Related events share one message so confirmation
and loading completion apply together. WebSocket compression remains enabled.
Connections to `/ws` without the format parameter retain HTML fragments for cached
clients during deployment; the JSON path does not generate server-side card HTML.

Cloudflare's automatic analytics beacon is suppressed by the HTML response's
`no-transform` policy. Added/sent/played counters continue to use local SQLite.
Correlated switching diagnostics are available in the service journal; see
[PERFORMANCE.md](PERFORMANCE.md) for stages, interpretation, and the log command.

Current browsers request `/ws?format=json&v=2`. In version 2, each library entry
is `[name, extension]` (for example `["Airhorn", ".ogg"]`), avoiding repeated
object keys across the catalog. Slot/events retain named fields. JSON clients
without `v=2` keep the original object entries; HTML clients remain supported.

The upstream Discord Gateway uses `zlib-stream` transport compression by default.
One streaming decompressor is retained per connection; a reconnect gets a fresh
context. `DISCORD_GATEWAY_COMPRESSION=none` restores uncompressed upstream JSON
if needed. This setting is independent of browser WebSocket compression.
Gateway READY/disconnect logs include compressed and decoded byte counts without
logging credentials or payload contents.

`GET /sound-metadata` supports content-based ETags and conditional requests.
Browsers cache it privately and revalidate before reuse; unchanged data returns
304 without a response body. Description and emoji changes invalidate the cached
representation. ETags remain stable across restarts when the content is unchanged.
PUT responses remain `no-store`, and WebSocket metadata invalidations still trigger
refreshes. No client-side cache management is required for normal browser fetches.

On narrow screens (under 640 px) or touch-first devices, **Preview new UI** also
turns on larger touch controls, sticky library search/filters, safe-area-aware
notifications, and tap-to-replace. Tap **Choose slot** on a library card, then
**Replace …** or **Use free slot** on an editable uploaded slot. **Cancel** or
Escape exits without uploading; busy and other users' protected slots cannot be
chosen. The **+** button continues to add directly to a free slot. Leaving preview
or the mobile layout cancels an active selection. The setting remains browser-local.

With **Preview new UI** enabled, library cards include **Preview** to audition
local Ogg/MP3 files without uploading to Discord or occupying a slot. Tap again
to stop; selecting another local or uploaded preview stops the previous preview.
Audio loads only on demand. Mute and disabling preview stop local playback.
Actual playback increments the existing SQLite played counter and recent history;
failed/muted playback and fetching audio files alone do not count as plays.
The `/library-audio?file=<filename>` endpoint supports GET/HEAD, seeking via byte
ranges, and cache revalidation, and serves only regular Ogg/MP3 library files.

The info dialog also edits emoji: paste a Unicode emoji, custom Discord emoji ID,
or `<:name:id>` / `<a:name:id>`. Clear the field to remove it. Changes are shared
immediately in this UI and used on the next Discord upload; an existing Discord
sound is not reuploaded by editing details. Manual choices (including removal)
are persisted in SQLite and protected from later Gateway snapshots. Editing only
the description preserves automatic emoji synchronization. Invalid/unavailable
custom emoji retain the existing upload retry without emoji.
