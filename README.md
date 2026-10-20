# Payroll (COBOL → Go migration)

This project is a migration of a COBOL payroll batch program to Go. It contains two entry points:

- `cmd/payroll` — the batch payroll tool, which reads `employees.txt`, printing the grand total (implemented in `internal/payroll`).
- `cmd/server` — an HTTP server exposing a `/healthz` endpoint, added during the migration.

> **Status:** the build passed, but the target unit tests **failed**, and behavior has **not** been compared against the original COBOL implementation. Treat this as work in progress. The migration was self-assessed at 65% confidence by the migrating model — this is not a measured result.

## Tech stack

- Go 1.23 (module `migrated-app`)
- Standard library only for the HTTP server (`net/http`) and configuration (`os.Getenv` in `internal/config`). `go.mod` declares **no third-party dependencies**.
- PostgreSQL (via `DATABASE_URL`; database at migration time was Postgres)

## Prerequisites

- Go toolchain 1.23 or newer (developed against `golang:1.27-alpine`)
- PostgreSQL available if you intend to exercise the database-backed parts
- `psql` client for the DB connectivity check (optional)

## Getting started

Run these from the root of a fresh clone, in this order.

### 1. Install (resolve modules and build the server)

```sh
mkdir -p bin && go mod tidy && go build -o bin/server ./cmd/server
```

### 2. Database setup (connectivity check)

```sh
PGPASSWORD=app psql -h $DB_HOST -U app -d app -c 'SELECT 1' || true
```

### 3. Environment variables

Export the required variables before starting the server:

```sh
export DATABASE_URL="postgres://app:app@localhost:5432/app?sslmode=disable"
export PORT=8080
export JWT_SECRET="change-me"
```

`internal/config` reads every variable directly from the environment with `os.Getenv` and never substitutes silent defaults: if `PORT` is unset, `cmd/server/main.go` falls back to `8080`; `DATABASE_URL` and `JWT_SECRET` are simply empty when unset.

### 4. Run the server

```sh
bin/server
```

The server listens on port `8080` (or the value of `PORT`). It handles SIGINT/SIGTERM with a 10-second graceful shutdown window.

**Note:** no requests have been verified to work against the migrated server yet. The router registers `/healthz`, which responds `ok`, but this has not been confirmed in a verification run.

### Batch tool

Build and run the batch payroll tool:

```sh
go build -o bin/payroll ./cmd/payroll
bin/payroll
```

It reads `employees.txt` by default; optional command-line arguments can override the input file path.

## Running tests

```sh
go test ./...
```

⚠️ This test suite **failed** during the migration verification run. See Known limitations.

## Environment variables

| Variable       | Required | Description |
|----------------|----------|-------------|
| `DATABASE_URL` | Yes (for DB use) | PostgreSQL connection string for the application database. Read by `internal/config`; no default is substituted. |
| `PORT`         | No      | HTTP listen port. If unset or empty, `cmd/server/main.go` defaults to `8080`. |
| `JWT_SECRET`   | Yes     | Secret used for JWT operations. Loaded by `internal/config` but not yet used by the current router. |

## Architecture overview

```
cmd/payroll/        Batch entry point; calls internal/payroll.Main
cmd/server/         HTTP server entry point (lifecycle, graceful shutdown)
cmd/server/config.go  Thin shim delegating to internal/config
cmd/server/router.go  Route registration (currently /healthz only)
internal/config/    Reads DATABASE_URL, PORT, JWT_SECRET from the environment via os.Getenv
internal/payroll/   Payroll computation (batch logic)
employees.txt       Sample/default employee input data
go.mod              Go module definition (migrated-app, go 1.23, no dependencies)
```

Configuration is read directly from the environment in `internal/config`; values genuinely set in the environment are always returned, never silently defaulted to empty strings.

## Migration notes

Changes from the original COBOL codebase:

- **Added an HTTP server (`cmd/server` with `/healthz`) and config infrastructure (`internal/config`) for `DATABASE_URL`/`PORT`/`JWT_SECRET`.**
  *Why:* The plan promises "standard library only" and a pure file-to-file batch tool with no network; the plan gives no reason for the server, so this is extra infrastructure added by the migration (inferred).
- **The batch entry point now accepts optional command-line arguments to override the input file path** (`employees.txt` remains the default).
  *Why:* The plan fixes the file name with no mention of arguments; a small usability addition with no stated motivation (inferred).

No source files were left untranslated.

## Known limitations

- **Unit tests failed.** `go test ./...` did not pass during the migration verification run; failing tests have not yet been fixed.
- **Behavior not compared with the original.** No behavioral diff against the COBOL implementation has been run, so output parity (including the printed grand total) is unconfirmed.
- **No verified requests.** The migrated server answered no requests in a verified check; `/healthz` exists in the code but its runtime behavior is unverified.
- `JWT_SECRET` is loaded but not used by any current route.
- Database-dependent behavior (Postgres via `DATABASE_URL`) has not been exercised in a verified run.

## Manual review required

- Run `go test ./...` locally, inspect the failures, and fix or triage them.
- Compare `bin/payroll` output (grand total) against the original COBOL program for the same `employees.txt`.
- Start the server and manually verify `/healthz` and any routes that should exist beyond it.
- Review the added HTTP server and config package — the migration plan specified a pure batch tool, so decide whether to keep or remove them.
- Confirm `employees.txt` parsing assumptions in `internal/payroll` match the original COBOL file format.
- Verify `JWT_SECRET` handling is either wired up or removed.
