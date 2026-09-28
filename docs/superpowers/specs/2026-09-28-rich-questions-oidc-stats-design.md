# Rich Questions, OIDC Setup, Admin OTel & Stats — Design

Date: 2026-09-28
Status: Approved (design), pending spec review
Scope: meetup app (`/root/projects/meetup`)

## Goal

Upgrade the meetup app with:

1. The full standard question-type set (multi-select, ranking, yes/no, NPS on top of the existing poll/rating/open/wordcloud).
2. Rich questions: an image or video attached to any question prompt.
3. First-run setup that supports signing in with OIDC (first OIDC user becomes admin).
4. OpenTelemetry configurable from the admin UI (DB-backed, env still wins).
5. General statistics in the admin web app, live over SSE.
6. Playwright coverage for all of the above, plus Swagger docs for every new endpoint.

## Decisions (from brainstorming)

- **Question types:** full standard set — `poll`, `multi`, `ranking`, `yesno`, `rating`, `nps`, `open`, `wordcloud`. All work in live mode and on the feedback form.
- **Media:** lives in the question prompt (upload or external URL). Answers stay text/choice.
- **OIDC:** first-ever OIDC user becomes admin; later unknown identities are rejected. Optional allowlist env for extra admins.
- **Analytics/OTel:** admin UI for OTel config (DB-backed, applied on restart, env overrides). Umami stays as-is.
- **Stats:** admin dashboard (per-event + global), live via SSE, simple charts.
- **Approach:** pragmatic — JSON-encoded values in the existing `answers` table; disk media in `MEDIA_DIR`; reuses the existing SSE broker.

## Non-goals

- Audience-submitted media answers or image-based answer options (deferred).
- OIDC group→role mapping (single admin role only).
- User-management UI beyond first-run setup and the `OIDC_ADMIN_EMAILS` allowlist.
- Browser/RUM telemetry; additional analytics vendors (Plausible/GA/Matomo).
- Drag-and-drop ranking (use tap-to-rank + up/down buttons).
- Normalized `answer_items` table.

## 1. Question types and answer semantics

| Kind | Input | Stored `answers.value` | Results |
|------|-------|------------------------|---------|
| `poll` | single choice, ≥2 options | option string | bars in option order (existing) |
| `multi` | checkboxes, ≥2 options | JSON array `["a","c"]` | per-option bars, `respondents` = distinct participants |
| `ranking` | tap-to-rank / up-down buttons, ≥2 options, ≤10 | JSON array in rank order | Borda score + avg rank, ordered best-first |
| `yesno` | Yes / No | `"yes"` / `"no"` | two bars |
| `rating` | 1–5 stars (existing) | `"1"`..`"5"` | bars (existing) |
| `nps` | 0–10 scale | `"0"`..`"10"` | distribution + NPS score −100…100 |
| `open` | free text ≤500 runes | text | top 100 by count (existing) |
| `wordcloud` | free text ≤200 runes | text | word bars (existing) |

### Validation

One centralized function `validateAnswer(kind string, options []string, raw string) (value string, err error)` in the `db` package (or `handlers`; decision: `db` so tests cover it directly), used by `POST /api/events/{code}/answers` before upsert:

- `poll`: value ∈ options.
- `multi`: JSON array of ≥1 unique option strings, all ∈ options.
- `ranking`: JSON array containing every option exactly once (length == len(options)).
- `yesno`: `"yes"` or `"no"`.
- `rating`: `"1"`…`"5"`.
- `nps`: `"0"`…`"10"`.
- `open`: non-empty, ≤500 runes.
- `wordcloud`: non-empty, ≤200 runes.
- Unknown kind: reject.

Empty values are rejected (current behavior kept).

### Results math

`db.QuestionResults` becomes `db.GetQuestionStats(questionID) (*db.QuestionStats, error)` with:

```go
type Result struct {
    Label   string  `json:"label"`
    Count   int     `json:"count"`
    Score   float64 `json:"score,omitempty"`    // ranking: Borda points
    AvgRank float64 `json:"avg_rank,omitempty"` // ranking: mean 1-indexed position
}

type QuestionStats struct {
    Results     []Result
    Total       int  // selections (sum of counts)
    Respondents int  // distinct participants
    NPS         *int // set for kind nps
}
```

- Borda: for n options, a ballot's rank-r option (r = 1 is best) earns `n - r` points; `Score` is the sum over ballots, `AvgRank` the mean of r.
- NPS: promoters 9–10, passives 7–8, detractors 0–6; `round(100 * (promoters − detractors) / responses)`; nil when responses == 0.
- `poll`/`rating` keep zero-count options in original option order (existing behavior). `multi` also lists all options in order. `ranking` is ordered by `Score` desc. `open`/`wordcloud` keep top-100 count desc.
- `Total` stays sum-of-counts; `Respondents` is `COUNT(DISTINCT participant_id)`.

Update all `QuestionResults` callers: `scanQuestionRow`, `ListQuestions`, `ListFeedbackQuestions`, `questionDTO`, `AdminExportCSV`, tests.

### DTO changes (additive)

`QuestionDTO` gains `respondents int`, `media_url string`, `media_type string` (`""|"image"|"video"`), `nps *int` (omitempty). `my_answer` for `multi`/`ranking` is the raw JSON array string; the frontend parses it.

## 2. Media in prompts

- Columns on `questions`: `media_url TEXT NOT NULL DEFAULT ''`, `media_type TEXT NOT NULL DEFAULT ''`.
- Accepted media URLs: either an uploaded file URL (`/media/<uuid>.<ext>`) or an external `http(s)://` URL.
- Upload endpoint: `POST /api/admin/events/{id}/questions/media`, multipart field `file`.
  - Whitelist detected MIME (`http.DetectContentType` + extension check): `image/jpeg|png|gif|webp|avif`, `video/mp4|webm|quicktime`.
  - Size caps: 10 MB images, 100 MB videos (`ParseMultipartForm(128<<20)`).
  - Stored as `<uuid><ext>` under `MEDIA_DIR`.
  - Response `MediaDTO`: `{url: "/media/<stored>", media_type, filename, size}`.
- Public inline serving: `GET /media/{name}`.
  - Reject any name where `filepath.Base(name) != name` (no traversal); must exist in `MEDIA_DIR`.
  - `Content-Type` from extension (`mime.TypeByExtension`, sniff fallback); `Cache-Control: public, max-age=31536000, immutable`; use `http.ServeFile` (supports Range for video).
  - Names are UUIDs, so URLs are unguessable; no auth (audience pages need them).
- Question create/update accept `media_url` + `media_type` (validate: if URL is external it must parse as http/https; media_type ∈ `""|image|video`).
- Audience (`app.js`) and projector (`live.js`) render media above the prompt; videos `controls muted autoplay loop playsinline`.

## 3. Migrations

`db.migrate()` gains an idempotent helper:

```go
func ensureColumn(table, column, ddl string) error // PRAGMA table_info check + ALTER TABLE
```

New columns (applied to existing DBs):

```sql
ALTER TABLE questions ADD COLUMN media_url  TEXT NOT NULL DEFAULT '';
ALTER TABLE questions ADD COLUMN media_type TEXT NOT NULL DEFAULT '';
ALTER TABLE settings  ADD COLUMN otel_endpoint     TEXT NOT NULL DEFAULT '';
ALTER TABLE settings  ADD COLUMN otel_service_name TEXT NOT NULL DEFAULT '';
ALTER TABLE settings  ADD COLUMN otel_headers      TEXT NOT NULL DEFAULT '';
```

The `CREATE TABLE IF NOT EXISTS` statements include the same columns for fresh installs. No change to `answers` (values stay one row per participant).

## 4. API surface

All new/changed handlers get Swagger annotations; `docs/` is regenerated with `task swagger`.

### Public

- `GET /media/{name}` — inline uploaded media (above).
- `POST /api/events/{code}/answers` — per-kind validation; unchanged response shape.
- `GET /api/events/{code}/state`, `/stream` — include media + respondents via `QuestionDTO`.

### Admin

- `POST /api/admin/events/{id}/questions/media` → `MediaDTO` (above).
- Question create/update — accept `media_url`, `media_type`.
- `GET /api/admin/stats` → `GlobalStatsDTO`.
- `GET /api/admin/events/{id}/stats` → `EventStatsDTO`.
- `GET /api/admin/events/{id}/stats/stream` — SSE `event: stats` with `EventStatsDTO` payload, initial snapshot + broker-driven updates + 25s heartbeat; session-cookie auth (same-origin EventSource sends cookies).
- `GET /api/admin/settings/otel` / `PUT /api/admin/settings/otel` → `OTelSettingsDTO`.

### Auth

- `GET /api/auth/oidc/status` unchanged.
- `GET /api/auth/oidc/callback` policy change (below). On rejection redirect to `/admin?oidc_error=unknown_user`.

## 5. OIDC first-run setup

- `/admin` setup screen (when `needs_setup`) calls `/api/auth/oidc/status`; if enabled it shows a **"Sign in with OIDC"** button next to the local admin form.
- Callback policy:
  - `CountUsers() == 0` → create user from OIDC email (`role=admin`), session, redirect `/admin`.
  - Known user (match by username/email) → session, redirect `/admin`.
  - Unknown user whose email ∈ `OIDC_ADMIN_EMAILS` (comma-separated env, optional) → create as admin, session, redirect `/admin`.
  - Otherwise → redirect `/admin?oidc_error=unknown_user` (no account created).
- Extract provisioning into a testable function, e.g. `provisionOIDCUser(email string) (*db.User, error)`, so unit tests can cover all four branches without a live IdP.

## 6. OpenTelemetry admin config

- `settings` gains `otel_endpoint`, `otel_service_name`, `otel_headers`.
- Startup resolution (env > DB): endpoint from `OTEL_EXPORTER_OTLP_ENDPOINT` or any signal-specific `OTEL_EXPORTER_OTLP_{TRACES,METRICS,LOGS}_ENDPOINT`, service name from `OTEL_SERVICE_NAME`, headers from `OTEL_EXPORTER_OTLP_HEADERS`. If no env value, fall back to DB; telemetry is enabled iff an endpoint resolves.
- `setupOTel` reads the resolved config (DB lookups happen after `db.Init`).
- `OTelSettingsDTO` reports stored values, effective values, and per-field source (`env`/`db`/default), plus `restart_required` (true when stored ≠ effective).
- Admin UI card next to Umami with endpoint / service name / headers fields and a "restart to apply" note.
- `GET /api/admin/otel/status` updated to report effective config + source (keeps working for existing consumers).

## 7. General stats

### Endpoints and DTOs

`GlobalStatsDTO`:

- totals: events, participants, answers, questions, Q&A, votes;
- `events []EventSummaryDTO`.

`EventStatsDTO`:

- `event` summary: id, code, name, status, participants, answered participants, response rate (%), answers, questions, Q&A, votes;
- `questions []QuestionDTO` (with results, respondents, NPS);
- feedback completion: participants who answered ≥1 feedback question + percentage of participants.

Definitions: `answered participants` = distinct participants with ≥1 answer; response rate = answered/participants (0 when no participants).

### Live updates

The admin Stats section subscribes to `GET /api/admin/events/{id}/stats/stream`; the handler subscribes to the existing `Broker` by event code. All existing `BroadcastEvent` calls already fire on answer/vote/QA/question changes, so no new broadcast sites are needed. Global stats load on navigation and on demand (refresh button), not streamed.

### UI

New **Stats** nav section in `/admin`:

- per-event selector, cards (participants, response rate, answers, Q&A, votes), question breakdown, feedback completion;
- hand-rolled SVG/CSS bars and donuts (no CDN/external chart lib, consistent with the embedded offline philosophy);
- auto-updating values over SSE for the selected event.

## 8. Frontend changes

- `static/admin.html` / `admin.js`:
  - kind select with all 8 kinds; options textarea shown for `poll`/`multi`/`ranking`; yes/no & NPS hide options;
  - media field: file input + external URL input + preview + clear button;
  - results renderer per kind (multi bars, ranking ordered list with scores, yes/no bars, NPS distribution + score);
  - Stats section; OTel settings card.
- `static/app.js` (audience):
  - render media above prompt; per-kind inputs: multi checkboxes, ranking list with up/down buttons, yes/no buttons, NPS 0–10 buttons; existing poll/rating/open/wordcloud untouched;
  - answered state: show `my_answer` (parse JSON for multi/ranking), keep the existing one-answer-per-question behavior (resubmission overwrites).
- `static/live.js` (projector): media render + result renderers for the new kinds.

## 9. Tests

### Go

- `db`: `validateAnswer` matrix (all kinds, valid/invalid), ranking Borda + avg rank, multi counts/respondents, NPS score math, stats queries, `ensureColumn` idempotency, fresh-vs-existing schema parity.
- `handlers`: `SubmitAnswer` per-kind validation errors; OIDC `provisionOIDCUser` branches (first user, known user, allowlisted, rejected); OTel config resolution (env wins, DB fallback, restart flag).

### Playwright (`e2e/`)

- New spec `question-types.spec.ts`: admin creates `multi`, `ranking`, `yesno`, `nps` questions (plus existing poll), audience answers each, results show expected values (including ranking order and NPS score).
- New spec `media.spec.ts`: upload `e2e/fixtures/pixel.png` to a question; audience page shows an `<img>` pointing at `/media/...` and it loads (`naturalWidth > 0`).
- New spec `stats.spec.ts`: after audience activity, admin Stats shows non-zero participants/answers.
- New spec `oidc.spec.ts`: second web server on port `6281` (fresh DB, `OIDC_ENABLED=true`, dummy issuer) — `/admin` setup screen shows both the local form and the OIDC button. No live IdP round-trip (deliberate limitation).
- Existing specs keep passing unchanged (`poll` flow stays the default).

### Docs

- Regenerate Swagger (`go install github.com/swaggo/swag/cmd/swag@latest`, `task swagger`).
- README: env table additions (`OIDC_ADMIN_EMAILS`, media limits, OTel DB config + precedence), question-type table.

## 10. Rollout phases (commit-sized)

1. `feat(questions): add multi, ranking, yes/no and NPS question types` — schema helper, validation, aggregation, tests.
2. `feat(questions): support image and video prompt media` — upload/serve endpoints, DTOs, frontend.
3. `feat(auth): support OIDC in first-run setup` — callback policy, setup UI, unit tests.
4. `feat(analytics): configure OpenTelemetry from admin settings` — DB config, resolution, UI.
5. `feat(stats): add admin statistics with live SSE` — endpoints, UI.
6. `test: cover question types, media, OIDC setup and stats` — Playwright specs.
7. `docs: document new question types, media, OIDC and stats` — Swagger regen + README.

Each phase runs `task prepush` locally; phases 1–5 include their own targeted tests; phase 6 adds e2e.

## 11. Risks and notes

- Media uploads need a larger multipart memory buffer; `maxBodyBytes` (2 MB JSON cap) only applies to `decodeJSON`, not multipart paths.
- `GET /media/{name}` is unauthenticated by design; UUID names make URLs unguessable, and only media-dir files are served.
- OIDC e2e coverage is UI-level only; callback logic is covered by Go unit tests.
- The existing presentations upload stores the stored filename in the DB and overrides the DTO display name; question media uses a cleaner path (URL in `media_url`) and does not refactor presentations.
- Existing installs upgrade via `ensureColumn`; fresh installs get the columns in `CREATE TABLE`.
