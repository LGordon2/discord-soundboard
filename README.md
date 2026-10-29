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

## Local analytics

`/analytics` returns totals and per-sound counters as JSON:

- `added`: successful Discord uploads through this app, including replacements.
- `sent`: successful sends to Discord, once per operation even if DiscordGo retries.
- `played`: user-initiated browser previews when playback actually starts. Muted or
  failed previews and automatic playback of sent sounds do not count.

The service stores aggregates in `data/analytics.json` relative to its working
directory. Set `ANALYTICS_FILE` to choose another path. On this host the default is
`/home/lew/dev/discord-soundboard/data/analytics.json`. The file is excluded from
Git, uses owner-only permissions, and survives rebuilds and service restarts.

Counters are saved every five seconds using atomic file replacement, and flushed
on SIGTERM/SIGINT. An abrupt kill or power loss can lose the last few seconds.
Recording does not wait for disk writes. Storage errors are logged without
breaking sound controls; a corrupt existing file is preserved and analytics stay
disabled until it is repaired. If a later write fails, memory counters remain and
persistence retries on the next interval.

Counts start at installation; no historical counts are inferred. Sound names
identify aggregate rows, so renamed sounds get separate rows. Sends whose sound
name is not in the current board use `sound:<id>`. No IP addresses, cookies, user
identifiers, or audio are collected. Preview reports are best effort and are not
audited usage records. The JSON endpoint is visible to anyone who can access this
soundboard, and shows sound names/counts without identifying listeners.

## Preview UI and compiled CSS

Enable **Preview new UI** at the top of the page to try search (including sound
descriptions), favorites, recently used sounds, and visible Preview/Discord
labels. It is off by default. The toggle, favorites, and last 24 recently used
sound names are saved in this browser's local storage. Turning preview off
restores the existing card layout and filters without changing the Discord board.

Tailwind is now a build-time dependency, compiled through PostCSS by `pnpm build`.
The browser loads only the local, minified CSS asset; it does not fetch or run the
Tailwind CDN compiler. Content scanning includes the HTML, TypeScript, and Go
source files so server-rendered cards and dynamic status colors are included.
Dark mode continues to follow the system preference.

`pnpm test` builds the production assets and exercises the native client against
a test DOM, including preview filters, favorites, descriptions, emoji display,
playback reporting, and the compiled CSS. Node 24 is recommended. To test an
already-built staged directory, run `CLIENT_DIST=/path/to/dist pnpm test:client`.

## Sound descriptions and emoji

In preview mode, use the **ⓘ** button on a sound card to edit its description or
source. Descriptions are shared across browsers and saved immediately to
`data/sound-metadata.json`. Set `SOUND_METADATA_FILE` to override that path.
The file uses the same atomic-write helper as analytics, owner-only permissions,
and is excluded from Git. Entries are keyed by sound name; same-named MP3/Ogg
variants share details. Renaming a sound creates a distinct entry.

Unicode and custom emoji are captured when Discord soundboard snapshots arrive,
kept after the sound leaves the board, and reused during subsequent uploads.
Previously archived sounds without recorded metadata cannot have their original
emoji inferred. Emoji changes save in the background every five seconds and on
normal shutdown. If Discord explicitly rejects a stored emoji, the upload retries
once without it so the audio can still be restored.
