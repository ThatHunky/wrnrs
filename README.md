# WRNRS / між нами.

Telegram bot backend for a synchronized relationship card game for pairs.

## Current Status

This repository contains a production-shaped Go backend scaffold with:

- Telegram update routing and Bot API client.
- SQLite migrations and repositories for users, pairs, answers, entitlements, uploads, purchases, and admin audit.
- Redis FSM/cache adapter.
- MinIO object storage adapter.
- Multilingual card content and UI strings.
- Dynamic card rendering with Cyrillic-capable fonts.
- Starter selectable fonts under `assets/fonts/`.
- User-upload processing into WebP derivatives.
- Core tests for deck filtering, no-repeat session selection, synchronized invite/reveal gameplay, encrypted answer storage, support prompt cadence, rendering, and storage.

The runnable version supports onboarding entry, reset/cancel, admin grant/revoke flows, premium invoice flow, `/paysupport`, synchronized pair gameplay with partner acceptance, and rendered cards.

## Quick Start

```bash
cp .env.example .env
docker compose up --build
```

For local tests without Docker:

```bash
GOTOOLCHAIN=local go test ./...
```

## Required Environment

See `.env.example`.

Minimum:

- `BOT_TOKEN`
- `BOT_USERNAME`
- `PHONE_HASH_SECRET`
- `ANSWER_ENCRYPTION_KEY`
- `ADMIN_TELEGRAM_IDS`
- `MINIO_ACCESS_KEY`
- `MINIO_SECRET_KEY`

Donation details are optional and must stay outside source:

- `DONATION_MONOBANK_URL`
- `DONATION_CARD_NUMBER`

## Docs

- [Full plan](docs/PLAN.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Operations](docs/OPERATIONS.md)
- [Development](docs/DEVELOPMENT.md)
