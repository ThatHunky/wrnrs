# AGENTS.md

## Project

This repo is the `WRNRS / між нами.` Telegram bot backend. It is a Go service using SQLite, Redis, MinIO, JSON content, and Telegram Bot API.

## Mandatory Documentation Lookup

Use the `ctx7` CLI to fetch current documentation whenever the user asks about a library, framework, SDK, API, CLI tool, or cloud service. This includes Telegram Bot API, Go libraries, Redis, SQLite drivers, MinIO, Docker Compose, and payment APIs.

Steps:

1. Resolve library: `npx ctx7@latest library <name> "<user's question>"`
2. Fetch docs: `npx ctx7@latest docs <libraryId> "<user's question>"`
3. Answer or implement using the fetched docs.

If `npx` is unavailable, state that clearly and use primary docs or local package docs as fallback. Do not include secrets in documentation queries.

## Engineering Rules

- Use Go 1.24 unless the user explicitly approves a toolchain bump.
- Verify with `GOTOOLCHAIN=local go test ./...`.
- Add tests before behavior changes.
- Keep Telegram handlers thin; business rules belong in `internal/content`, `internal/game`, `internal/storage`, or focused service packages.
- Do not hardcode tokens, Monobank details, card numbers, admin IDs, or MinIO secrets.
- Do not hardcode Telegram webhook secrets. Store them in `.env` or deployment secrets.
- Do not store original uploaded images after processing; only processed WebP derivatives should be persisted.
- Direct sexual cards require both `requires_mature_opt_in: true` and the `mature` tag.
- Donation prompts are optional author support only and must not unlock digital goods. Premium/cosmetics must use Telegram Stars or admin grants.
- Admin tools must not expose private answers unless the user explicitly changes the support policy.

## Verification Checklist

Before claiming work is complete:

- Run `gofmt -w cmd internal`.
- Run `GOTOOLCHAIN=local go test ./...`.
- If Docker/deployment changed, run or at least validate `docker compose config`.
- Confirm no secrets are present in source files.
- Update docs when architecture, env vars, schemas, commands, or content rules change.

## Important Files

- `docs/PLAN.md`: product and implementation plan.
- `docs/ARCHITECTURE.md`: system boundaries and package map.
- `docs/OPERATIONS.md`: deployment and backups.
- `docs/DEVELOPMENT.md`: development workflow and content rules.
- `migrations/001_init.sql`: durable SQLite schema.
- `content/questions.v1.json`: multilingual card deck.
- `content/fonts.v1.json`: selectable font catalog.
- `assets/fonts/`: tracked font files and upstream license/readme files.
