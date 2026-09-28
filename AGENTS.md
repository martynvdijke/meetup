# Meetup Developer Guide

## Conventional Commits

All commits **MUST** follow the [Conventional Commits](https://www.conventionalcommits.org/) format.
The release workflow uses `semantic-release` to automatically version and publish based on commit messages.

| Prefix | Release | Example |
|--------|---------|---------|
| `feat:` | minor | `feat: add live poll results SSE` |
| `fix:` | patch | `fix: word cloud dedup on resubmit` |
| `refactor:` | patch | `refactor: extract media upload helper` |
| `style:` | patch | `style: format Go imports` |
| `chore(deps):` | patch | `chore(deps): bump modernc.org/sqlite` |
| `docs:` | none | `docs: update env var table` |
| `test:` | none | `test: add poll vote coverage` |
| `chore:` | none | `chore: clean up temp files` |

A commit message should look like:

```
feat: add projector live view

Stream poll/Q&A results via SSE to the projector page.

Closes #42
```

## Pre-Push Checks

Before pushing, run all validation checks:

```bash
task prepush
```

This runs (in order):

1. `task css` — build Tailwind CSS (`npm run build:css`)
2. `task tidy` — `go fmt ./...` and `go mod tidy`
3. `task lint` — `go vet ./...`
4. `task test` — `go test -v ./...`
5. `task build` — `go build -o meetup .` (includes CSS build)

## Running Tests

```bash
task test          # Go unit tests
task test:e2e      # Playwright e2e tests (builds binary first)
task test-all      # All tests (unit + e2e)

# Directly:
go test -v ./...
npx playwright test --workers=1 --reporter=line
npx playwright test e2e/admin.spec.ts   # single file
```

E2E tests require the built binary (`task build`) and hit `http://localhost:6280`.

## Tailwind CSS

```bash
task css           # Build static/tailwind.css from src/input.css
npm run watch:css  # Watch mode
```

`static/tailwind.css` is committed so `go build` works without Node.

## Swagger / OpenAPI

```bash
task swagger       # Requires swag on PATH: go install github.com/swaggo/swag/cmd/swag@latest
```

Generated docs are served at `/swagger/` when the app is running.

## All Tasks

| Task | Description |
|------|-------------|
| `task css` | Build Tailwind CSS |
| `task swagger` | Generate Swagger docs |
| `task build` | Build Go binary (deps: css) |
| `task run` | Build and run on port 6280 |
| `task dev` | Hot reload via `air -c .air.toml` |
| `task test` | Run Go unit tests |
| `task test:e2e` | Run Playwright e2e tests (deps: build) |
| `task test-all` | Run all tests |
| `task lint` | Run `go vet` |
| `task tidy` | Format and tidy Go modules |
| `task prepush` | All checks before pushing |
| `task docker-build` | Build Docker image |
| `task docker-run` | Run Docker container |
