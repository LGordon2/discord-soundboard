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

`/analytics/data` returns totals and per-sound counters as JSON for signed-in,
upload-allowlisted accounts:

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

**Choose emoji** in the info dialog opens a searchable common-emoji palette with
categories, skin tones for supported hand gestures, and a Custom category built
from emoji already stored in sound metadata. Selecting one fills the editor;
**Save details** persists it. **Remove emoji** clears the choice. The palette is a
separate lazy-loaded local JavaScript chunk, with no external picker scripts or
emoji image sheets. Unicode/custom values can still be pasted directly.

Enable **Preview new UI**, then **Upload sound**, to add a clip to the server
library. Sign in with Discord, choose/drop MP3/Ogg/WAV audio (up to two minutes
and 10 MiB), select up to five seconds on the waveform/range controls, adjust
volume, and preview that selection. Continue to name, description and emoji before
saving. The server validates and applies the trim/volume using FFmpeg and stores
an optimized MP3. File names remain unique, and connected viewers receive the new
library entry. Existing shared credentials continue to handle Discord slot uploads.

Upload access uses Discord OAuth2 `identify`, not a shared password. Configure
`DISCORD_CLIENT_ID` and `DISCORD_CLIENT_SECRET` (existing `CLIENT_ID` / `CLIENT_SECRET`
are supported as fallbacks). Register this callback in the application's OAuth2
redirects: `https://discord-soundboard.lewgordon.com/auth/discord/callback`.
`DISCORD_REDIRECT_URI` can override it for another deployment.

Set `UPLOAD_ALLOWED_USER_IDS` to comma-separated Discord IDs, or add IDs one per
line to `data/upload-allowlist` next to SQLite (`UPLOAD_ALLOWLIST_FILE` overrides
the path). The default allowlist is empty. File changes take effect without a
restart, including removal of access from existing sessions. Login shows the
user's Discord ID even if they are not approved, so it can be added to the list.

Sessions use HttpOnly, Secure, SameSite cookies, with hashed session tokens stored
in SQLite and a seven-day expiry. Uploads and logout require a session CSRF token.
Discord access tokens are used only to verify identity and are not retained.
The uploader ID/name and upload time are saved in `sound_uploads`. The old password
is no longer accepted. Login does not replace the shared Discord account.

The service requires `ffmpeg` (with `libmp3lame`) and `ffprobe` on PATH. One
conversion runs at a time with a 30-second timeout. Uploaded clips are rechecked
against Discord's output limits. Temporary files are cleaned up on failure;
completed files are published atomically without overwriting the library. The
uploader and waveform editor load only when opened. Browser preview applies the
same selection and volume before upload; waveform decoding stays local.

The upload waveform has touch-sized edge grabbers and a draggable middle that
moves the selected clip without changing its length. It automatically frames the
selection with surrounding audio; shorter clips zoom in further. The view stays
fixed throughout a drag and refits on release. **Position in full audio** navigates
longer sources, **Full audio** shows the overview, and **Fit clip** restores the
close view. **Precise start and end** retains the sliders. Grabbers support arrow
keys (Shift for larger steps), Home and End. Cached waveform peaks are reused for
zooming rather than decoding the file again.

## Library administration

Open `/admin` and sign in with the owner's Discord account. Admin access is bound
on the server to the owner's immutable Discord ID, independently of the upload
allowlist. The top account menu also shows **Manage sounds** for that account.

The page searches, renames, and deletes saved library sounds. Renaming updates
both MP3/Ogg variants, descriptions, emoji, and upload attribution. Deletion
removes library entries and archives the audio in `sounds/.admin-archive-*`.
These actions do not edit sounds already installed on the Discord soundboard;
historical analytics remain under their original names. Connected soundboard
pages receive updated library and metadata events. All mutations require an
active owner session and its CSRF token.

The **Analytics** links in the top account menu and on `/admin` open `/analytics`.
Analytics access uses the same Discord account allowlist as uploads; a valid
OAuth session is required, and allowlist changes apply on the next request.
It shows cumulative totals, the top eight sounds for browser plays,
Discord sends, and Discord board additions, and a searchable, sortable table.
Refresh the page to read the current in-memory counters. Plays include previews
and can be reported by multiple browser clients; they are not unique listeners.
The time-series chart records new activity in persistent hourly UTC buckets,
with a 24-hour view and daily rollups for 7, 30, or 90 days. History starts when
this feature is enabled; earlier cumulative totals are not backfilled. The
current bucket is partial. Refresh to include new events.

The charts page lives at `/analytics`; `/analytics/data` serves its JSON snapshot
with the same login and upload-allowlist checks. Only the owner can use `/admin`
to rename or delete sounds.

## Browser security policy

The HTTP server applies CSP, HSTS (one year, including subdomains), COOP
`same-origin`, frame denial, and MIME sniffing protection to all routes. CSP
permits local scripts and hashes of the shipped inline bootstrap/admin/analytics
scripts; Discord CDN images/audio and local blob audio remain available. Inline
styles remain permitted for layout hints, waveform controls, and chart sizing.
Build the frontend before starting/restarting the backend: bootstrap hashes are
read from `dist/index.html` at startup. Deploy assets and restart together.

Trusted Types enforcement is not enabled: the current client renders escaped HTML
fragments through DOM HTML sinks and needs a separate migration before enforcement.
The policy blocks unapproved third-party scripts; verify browser DevTools after
deployment to identify any remaining third-party-cookie issues. Lighthouse scores
must be measured again against the deployed service.
