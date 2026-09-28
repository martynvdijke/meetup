# meetup

Audience interaction for meetups and small conferences — QR-joined live polls, Q&A with voting, word clouds, session feedback, presentation uploads, a projector/live view, admin panel, Umami analytics, and OpenTelemetry traces.

Single Go binary serving an embedded vanilla-JS frontend (Tailwind CSS). SQLite for persistence. No separate frontend build step required to run.

## Quick Start

### Local

```bash
# prerequisites: Go 1.27, Node 24 (only for CSS), Task (https://taskfile.dev), swag (optional)
task css          # build static/tailwind.css (committed, so optional if not changing CSS)
task run          # builds and starts on http://localhost:6280
# or hot reload:
task dev          # requires air: go install github.com/cosmtrek/air@latest
```

Environment (defaults shown by `task run`):

```bash
PORT=6280 DB_PATH=./meetup.db MEDIA_DIR=./media ./meetup
```

### Docker

```bash
docker compose up --build
# http://localhost:6280
```

Or manually:

```bash
task docker-build
task docker-run
```

## API

The full API reference is served at **`/swagger/`** (Swagger UI) when the app is running. Swagger is generated with `task swagger` (`swag init`).

At a high level the API covers:

- **Events** — create/list events (admin).
- **Sessions/questions** — live polls, Q&A, word clouds, and feedback tied to events.
- **Presentations** — file uploads served from `MEDIA_DIR`.
- **Live/projector** — SSE/WS stream for the projector view and audience clients.
- **Admin** — event and session management.
- **Auth** — optional OIDC login and local auth; see environment variables below.

Refer to `/swagger/` for exact paths, request/response schemas, and auth requirements — this README does not duplicate endpoint listings that may evolve.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `6280` | HTTP listen port |
| `DB_PATH` | `./meetup.db` (compose: `/db/meetup.db`) | SQLite file path |
| `MEDIA_DIR` | `./media` (compose: `/app/media`) | Uploaded presentation storage |
| `PUBLIC_BASE_URL` | _(empty)_ | Public origin used for QR links / redirects |
| `OIDC_ENABLED` | _(empty)_ | Set to `true` to enable OIDC |
| `OIDC_ISSUER_URL` | `https://authelia.vandijke.xyz` | OIDC issuer URL |
| `OIDC_CLIENT_ID` | `meetup` | OIDC client ID |
| `OIDC_CLIENT_SECRET` | _(empty)_ | OIDC client secret |
| `OIDC_REDIRECT_URL` | `https://meetup.vandijke.xyz/api/auth/oidc/callback` | OIDC callback URL |
| `OIDC_SCOPES` | `openid email profile groups` | OIDC scopes |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(empty)_ | OTLP HTTP/gRPC endpoint; when set, telemetry is exported via OTLP (opt-in) |
| `OTEL_*` | | Any standard OTEL env (e.g. `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`) is honored when the exporter is configured |

## Tailwind CSS

Source: `src/input.css` → output: `static/tailwind.css` (committed so `go build` works without Node).

```bash
task css           # one-shot build
npm run watch:css  # watch mode
```

## Development

See [AGENTS.md](AGENTS.md) for commit conventions, `task prepush`, and all available tasks.
