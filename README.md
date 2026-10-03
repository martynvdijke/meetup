# meetup

Audience interaction for meetups and small conferences — QR-joined live polls, multi-select, ranking, yes/no, rating and NPS questions, Q&A with voting, word clouds, session feedback, image/video question media, presentation uploads, a projector/live view, an admin panel with live statistics, Umami analytics, and OpenTelemetry traces.

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
- **Questions** — all question kinds (poll, multi, ranking, yes/no, rating, NPS, open, word cloud), optional image/video prompt media, Q&A and feedback tied to events.
- **Presentations** — file uploads served from `MEDIA_DIR`.
- **Live/projector** — SSE stream for the projector view and audience clients.
- **Stats** — global and per-event statistics with live SSE updates (admin).
- **Admin** — event, question, Q&A, presentation and settings management.
- **Auth** — optional OIDC login and local auth; see environment variables below.

Refer to `/swagger/` for exact paths, request/response schemas, and auth requirements — this README does not duplicate endpoint listings that may evolve.

## Question Types

Every question works in live mode (audience answers while the talk is running) and on the feedback form.

| Kind | Input | Results |
|------|-------|---------|
| `poll` | single choice | bars in option order |
| `multi` | multiple choice | bars per option, share of respondents |
| `ranking` | order all options (2–10) | Borda score + average rank |
| `yesno` | Yes / No | two bars |
| `rating` | 1–5 stars | bars |
| `nps` | 0–10 scale | NPS score (−100…100) + distribution |
| `open` | free text (≤500 chars) | top answers |
| `wordcloud` | short text (≤200 chars) | word cloud |

Questions can carry an optional image or video prompt: upload a file (JPEG/PNG/GIF/WebP/AVIF ≤10 MB, MP4/WebM/MOV ≤100 MB) or paste an external `https://` URL in the admin question form. Uploaded media is served from `/media/…` and rendered on both the audience and projector views.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `6280` | HTTP listen port |
| `DB_PATH` | `./meetup.db` (compose: `/db/meetup.db`) | SQLite file path |
| `MEDIA_DIR` | `./media` (compose: `/app/media`) | Uploaded presentation and question media storage |
| `PUBLIC_BASE_URL` | _(empty)_ | Public origin used for QR links / redirects |
| `OIDC_ENABLED` | _(empty)_ | Set to `true` to enable OIDC |
| `OIDC_ISSUER_URL` | `https://authelia.vandijke.xyz` | OIDC issuer URL |
| `OIDC_CLIENT_ID` | `meetup` | OIDC client ID |
| `OIDC_CLIENT_SECRET` | _(empty)_ | OIDC client secret |
| `OIDC_REDIRECT_URL` | `https://meetup.vandijke.xyz/api/auth/oidc/callback` | OIDC callback URL |
| `OIDC_SCOPES` | `openid email profile groups` | OIDC scopes |
| `OIDC_ADMIN_EMAILS` | _(empty)_ | Comma-separated emails allowed to sign in via OIDC in addition to existing accounts |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(empty)_ | OTLP endpoint; when set, telemetry is exported over OTLP (environment overrides admin settings) |
| `SMTP_HOST` | _(empty)_ | SMTP host for password reset emails (environment overrides admin settings) |
| `SMTP_PORT` | `587` (`465` for SSL) | SMTP port |
| `SMTP_USER` | _(empty)_ | SMTP username |
| `SMTP_PASS` | _(empty)_ | SMTP password |
| `SMTP_FROM` | _(SMTP_USER)_ | From address |
| `SMTP_TLS` | `auto` | `starttls`, `ssl` or empty (auto) |
| `OTEL_*` | | Any standard OTEL env (e.g. `OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_HEADERS`) is honored and takes precedence over admin settings |

### OIDC sign-in

When `OIDC_ENABLED=true`, the setup and login screens offer **Sign in with OIDC**:

- The first OIDC user becomes the admin (handy for first-run setup).
- Afterwards, OIDC sign-in is only allowed for existing accounts and emails listed in `OIDC_ADMIN_EMAILS`; anyone else is redirected back with "not allowed to sign in".
- Local admin accounts keep working alongside OIDC.

### OpenTelemetry

Traces, metrics and logs are exported over OTLP when an endpoint resolves, with this precedence:

1. Environment variables (`OTEL_EXPORTER_OTLP_ENDPOINT` or signal-specific endpoints, `OTEL_SERVICE_NAME`, `OTEL_EXPORTER_OTLP_HEADERS`)
2. Values saved under **Admin → Settings → OpenTelemetry**
3. Defaults (service name `meetup`, telemetry disabled)

Settings saved in the admin UI apply after a restart; the panel shows stored vs effective values and flags when a restart is required.

### Statistics

The admin **Stats** section shows global totals (events, participants, answers, questions, Q&A, votes) plus per-event cards — participants, answered participants, response rate, answers, questions, Q&A, votes and feedback completion — with a per-question breakdown including ranking scores and NPS. The selected event's stats update live over SSE.

## Tailwind CSS

Source: `src/input.css` → output: `static/tailwind.css` (committed so `go build` works without Node).

```bash
task css           # one-shot build
npm run watch:css  # watch mode
```

## Development

See [AGENTS.md](AGENTS.md) for commit conventions, `task prepush`, and all available tasks.
