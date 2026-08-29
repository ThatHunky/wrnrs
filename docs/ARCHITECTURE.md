# Architecture

## Package Map

- `cmd/wrnrs`: application entrypoint, HTTP health/webhook server, long polling fallback.
- `internal/app`: update orchestration and high-level route handling.
- `internal/telegram`: Telegram Bot API DTOs, HTTP client, inline/reply keyboards.
- `internal/content`: deck, style, background, and font JSON loading, validation, maturity filtering, and deterministic no-repeat selection.
- `internal/catalog`: generic content catalogs for superapp modules — facet filtering and deterministic no-repeat selection seeded by pair or user id.
- `internal/modules`: module registry and access gate (`18+`, mature opt-in, active pair, premium) with callback-prefix dispatch.
- `internal/game`: durable pair-game state transitions, invite acceptance, completion types, reveal readiness, no-repeat selection, level progression, and support prompt cadence.
- `internal/i18n`: localized UI strings and brand text fallback.
- `internal/storage`: SQLite migration and repository methods for profiles, pairs, sessions, answers, purchases, entitlements, uploads, admin audit, support timestamps, and custom questions.
- `internal/state`: Redis-backed conversational FSM, legacy pending-completion helpers, render cache helper, pair locks, and user rate limits.
- `internal/objectstore`: MinIO bucket/object adapter.
- `internal/render`: card rendering and uploaded background WebP processing.
- `content/styles.v1.json`, `content/backgrounds.v1.json`, `content/fonts.v1.json`, and `assets/fonts/`: theme catalogs, built-in background metadata, selectable font catalog, and tracked TTF assets.
- `internal/onboarding`, `internal/pairing`, `internal/payments`, `internal/admin`: focused feature services.

## Data Boundaries

SQLite is the durable source of truth and is opened with `PRAGMA foreign_keys = ON` so GDPR account deletion cascades through encrypted answer, receipt, entitlement, and asset metadata rows. Redis stores conversational prompts, a render file-ID cache helper, best-effort pair locks, and fixed-window per-user rate-limit counters. MinIO stores processed object bytes only; SQLite stores object keys and metadata. JSON content is immutable application content loaded at boot.

## Current Runtime Behavior

The service boots, validates required config including `ANSWER_ENCRYPTION_KEY`, runs additive SQLite migrations, connects Redis, optionally ensures MinIO, loads content, and handles Telegram updates through webhook or long polling.

Gameplay is pair-synchronized and session-scoped. `game:start` creates a durable `pending_acceptance` session and notifies the partner; no card is sent until the partner accepts. Card controls use callback data such as `game:answer:{session_id}`, `game:skip:{session_id}`, and `game:next:{session_id}`. Text messages entered while a user is in `game:await_answer:{session_id}` are stored in SQLite `game_answers` with AES-GCM encrypted answer bytes.

Question selection is pair-level and randomized through deterministic no-repeat deck cycles. Stock cards and active pair custom questions share one no-repeat path; custom cards use `custom:{id}` IDs. `game_sessions` snapshot localized question text so deleted custom questions remain renderable and journalable for existing sessions/history. `pair_card_history` is written only after both partners complete and reveal a card. After 6 completed cards in the active level, the pair advances to the next level when one exists.

Onboarding covers language, name, gender, optional own-contact hashing, 18+ confirmation, mature opt-in, base theme color, and optional background choice/upload. Own-contact onboarding stores only `phone_lookup_hash`; raw phone numbers are not stored. After onboarding completes, the main menu offers pairing without blocking access to the rest of the bot.

Theme customization supports base color, style selection, style property overrides, font selection, built-in backgrounds, partner-shared uploaded backgrounds, and up to 3 active user-uploaded backgrounds. Upload handling accepts Telegram photos or JPEG/PNG/WebP documents, decodes/re-encodes to a metadata-stripped WebP derivative, stores the derivative in MinIO when configured, and persists only processed asset metadata. Built-in backgrounds are generated locally and can be seeded to MinIO on first use.

Telegram photo cards are edited through `editMessageCaption` for status/prompt changes. Text menu panels continue to use `editMessageText`. If an old message cannot be edited anymore, `internal/app` logs the edit failure and sends one fallback message so the callback still completes.

The main menu is context-aware: it loads the user's display name, selected theme color, active pair status, partner display name, active level, and completed card count when available. Settings language changes save the language for completed users without re-entering onboarding. Theme changes from settings save only the theme color and do not re-run onboarding completion side effects.

Telegram Stars payments store a durable `purchase_receipts` row keyed by `telegram_payment_charge_id` before granting the entitlement. The invoice payload user is checked against the actual payer (`message.from.id`) before any entitlement is granted.

Admin grant/revoke actions support numeric Telegram IDs and known `@username` targets. Both inline-menu and slash-command flows use the same validation and audit path; unknown users are rejected instead of creating placeholder entitlements. Slash commands and the inline admin menu can target premium, styles, fonts, and backgrounds from the loaded catalogs.

Custom questions have storage, settings UI, soft-delete behavior, and gameplay selection. Deleted custom questions are hidden from future selection but remain available through session snapshots and journal history.

Pair break has an explicit confirmation path. It ends the active pair, cancels current pair sessions, clears pair theme shares, resets selected shared backgrounds, clears transient game/FSM state for both users, and notifies the partner. Account deletion performs the same active-pair cleanup/notification before deleting owned uploaded objects and the SQLite user row.

When `FEATURE_INLINE_MODE=true`, inline queries receive one personal text-only `InlineQueryResultArticle` with a safe localized card. Inline photo results are intentionally avoided in v1 because Telegram requires public JPEG URLs for photo inline results.

When `PUBLIC_BASE_URL` is set, long polling is disabled and Telegram should deliver updates to `/telegram/webhook`. If `TELEGRAM_WEBHOOK_SECRET` is set, webhook requests must include the matching `X-Telegram-Bot-Api-Secret-Token` header.

## Remaining Follow-Ups

- `render:file:{hash}` is still a Redis helper only; Telegram send paths do not reuse cached file IDs yet.
- Pair invites remain durable SQLite rows; the earlier idea of Redis invite mirrors is still undecided.
- MinIO object lifecycle cleanup is operational policy rather than app-enforced retention.

## Telegram Notes

Digital goods must use Telegram Stars with `XTR`. The Monobank support path is treated as optional donation only and must not unlock premium or cosmetics.
