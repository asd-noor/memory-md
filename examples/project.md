# My Project

High-level notes and decisions for the project. Tags: architecture, decisions, conventions.

## Stack

Backend: Go 1.26. Frontend: React + TypeScript. Database: PostgreSQL 17.
Deployed on Fly.io. CI via GitHub Actions.

### API

REST over HTTP/1.1. All responses are JSON. Base URL: `/api/v1`.
Auth uses bearer tokens (JWT, HS256). Token TTL is 15 minutes; refresh tokens last 30 days.

### Database

PostgreSQL 17 with `pgvector` extension for embeddings.
Connection pool: max 25 connections, idle timeout 5 minutes.
Migrations are managed with `goose` (sequential, committed to repo).

## Conventions

Coding and naming standards applied across the entire codebase.

### Naming

- Tables: `snake_case`, plural (e.g. `user_accounts`).
- Go packages: single lowercase word; no underscores.
- Environment variables: `APP_` prefix (e.g. `APP_DATABASE_URL`).

### Error Handling

All errors are wrapped with `fmt.Errorf("context: %w", err)`.
HTTP errors use a shared `Problem` struct (RFC 7807).
Sentinel errors live in `internal/errs/errs.go`.

### Testing

Unit tests live next to the code (`*_test.go`).
Integration tests live in `internal/testutil/` and require a live database.
Run integration tests with `go test -tags integration ./...`.

## Decisions

Architectural decisions with rationale and revisit criteria.

### Why Go over Node

Chosen for strong typing, single binary deployment, and lower memory footprint.
Node was considered but ruled out due to callback complexity in stream processing.
Decision made 2025-11-03. Revisit if frontend team needs to own the backend.

### Why Fly.io over AWS

Simpler pricing, built-in Anycast, and no VPC configuration overhead.
AWS was considered; rejected because the team lacked AWS expertise.
Decision made 2025-11-10. Revisit at >10 000 req/s.

## Secrets

All secrets are stored in 1Password under the `my-project` vault.
Injected at runtime via `op run -- ./app` in production.
Never commit secrets to git; the `.env` file is gitignored.

### Rotation Policy

Service tokens rotate every 90 days via a GitHub Actions scheduled workflow.
Database passwords rotate every 180 days. Rotation runbook: `docs/runbooks/secret-rotation.md`.
