# Architecture

## Package Map

- `cmd/wrnrs`: application entrypoint, HTTP health/webhook server, long polling fallback.
- `internal/app`: update orchestration and high-level route handling.
- `internal/telegram`: Telegram Bot API DTOs, HTTP client, inline/reply keyboards.
- `internal/content`: deck loading, validation, maturity filtering, deterministic no-repeat selection.
- `internal/game`: durable pair-game state transitions, invite acceptance, completion types, reveal readiness, no-repeat selection, level progression, and support prompt cadence.
- `internal/i18n`: localized UI strings and brand text fallback.
- `internal/storage`: SQLite migration and repository methods.
- `internal/state`: Redis-backed conversational FSM and render cache helper.
- `internal/objectstore`: MinIO bucket/object adapter.
- `internal/render`: card rendering and uploaded background WebP processing.
- `content/fonts.v1.json` and `assets/fonts/`: selectable font catalog and tracked TTF assets.
- `internal/onboarding`, `internal/pairing`, `internal/payments`, `internal/admin`: focused feature services.

## Data Boundaries

SQLite is the durable source of truth and is opened with `PRAGMA foreign_keys = ON` so GDPR account deletion cascades through pair, encrypted answer, receipt, entitlement, and asset metadata rows. Redis is disposable transient state for conversational prompts only. MinIO stores processed object bytes only; SQLite stores object keys and metadata. JSON content is immutable application content loaded at boot.

## Current Runtime Behavior

The service boots, validates required config including `ANSWER_ENCRYPTION_KEY`, runs additive SQLite migrations, connects Redis, optionally ensures MinIO, loads content, and handles Telegram updates through webhook or long polling.

Gameplay is pair-synchronized and session-scoped. `game:start` creates a durable `pending_acceptance` session and notifies the partner; no card is sent until the partner accepts. Card controls use callback data such as `game:answer:{session_id}`, `game:skip:{session_id}`, and `game:next:{session_id}`. Text messages entered while a user is in `game:await_answer:{session_id}` are stored in SQLite `game_answers` with AES-GCM encrypted answer bytes.

Question selection is pair-level and randomized through deterministic no-repeat deck cycles. `pair_card_history` is written only after both partners complete and reveal a card. After 6 completed cards in the active level, the pair advances to the next level when one exists.

Telegram photo cards are edited through `editMessageCaption` for status/prompt changes. Text menu panels continue to use `editMessageText`. If an old message cannot be edited anymore, `internal/app` logs the edit failure and sends one fallback message so the callback still completes.

The main menu is context-aware: it loads the user's display name, selected theme color, active pair status, partner display name, active level, and completed card count when available. Settings language changes save the language for completed users without re-entering onboarding. Theme changes from settings save only the theme color and do not re-run onboarding completion side effects.

Telegram Stars payments store a durable `purchase_receipts` row keyed by `telegram_payment_charge_id` before granting the entitlement. The invoice payload user is checked against the actual payer (`message.from.id`) before any entitlement is granted.

Admin grant/revoke actions support numeric Telegram IDs and known `@username` targets. Both inline-menu and slash-command flows use the same validation and audit path; unknown users are rejected instead of creating placeholder entitlements.

When `PUBLIC_BASE_URL` is set, long polling is disabled and Telegram should deliver updates to `/telegram/webhook`. If `TELEGRAM_WEBHOOK_SECRET` is set, webhook requests must include the matching `X-Telegram-Bot-Api-Secret-Token` header.

## Telegram Notes

Digital goods must use Telegram Stars with `XTR`. The Monobank support prompt is treated as optional donation only and must not unlock premium or cosmetics.
