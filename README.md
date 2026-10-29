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
