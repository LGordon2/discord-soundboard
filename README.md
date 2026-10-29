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
Dark mode follows the system preference.

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
