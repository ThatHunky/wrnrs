# WRNRS / між нами.

Telegram bot backend for a synchronized relationship card game for pairs.

## Current Status

This repository contains a production-shaped Go backend with:

- Telegram update routing and Bot API client.
- SQLite migrations and repositories for users, pairs, game sessions, encrypted answers, entitlements, uploads, purchases, admin audit, support-prompt timestamps, and custom questions.
- Redis FSM/cache adapter.
- MinIO object storage adapter.
- Multilingual card content and UI strings.
- Dynamic card rendering with selectable Cyrillic-capable fonts, styles, built-in backgrounds, and uploaded WebP backgrounds.
- Telegram Stars premium purchase flow and admin grant/revoke flows.
- Core tests for deck filtering, no-repeat session selection, synchronized invite/reveal gameplay, encrypted answer storage, answer journal, support interstitials, rendering, storage, admin actions, payments, inline mode, pair cleanup, rate limits, and theme uploads.

The runnable version supports onboarding entry with optional own-contact and background steps, reset/cancel, pairing by ID, username, contact, or invite link, synchronized pair gameplay with partner acceptance, rendered cards, custom questions in gameplay, answer journal, before-reveal support prompts, theme customization, uploaded/shared backgrounds, pair break/account deletion cleanup, text-only inline mode, admin card previews, admin grants/revokes, premium invoices, and `/paysupport`.

Remaining follow-ups are lower-level hardening: wire the Redis render file-ID cache into send paths, define a MinIO lifecycle policy, and decide whether pair-invite Redis mirrors are still worth adding. See [Full plan](docs/PLAN.md) and [Message flow audit](docs/MESSAGE_FLOW_AUDIT.md).

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
- [Message flow audit](docs/MESSAGE_FLOW_AUDIT.md)
- [Operations](docs/OPERATIONS.md)
- [Development](docs/DEVELOPMENT.md)
