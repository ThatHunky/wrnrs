# Message Flow Audit

This audit reflects the current code in `internal/app/app.go`, `internal/game`, and `internal/storage`.

## Current Shipped Flow

- `/start` ensures a SQLite user row and starts onboarding unless onboarding is already complete.
- Onboarding currently covers language, name, gender, optional own-contact hashing, 18+ confirmation, mature opt-in, theme color, and optional background skip/default/upload.
- `Menu`, `/menu`, `Cancel / Reset`, `/cancel`, `/reset`, `/start`, `/paysupport`, and admin entry clear the current FSM before handling their command path.
- Main menu text is context-aware and includes display name, selected color, pair status, partner name, active level, and level progress when available.
- Pairing supports username, numeric Telegram ID, contact, and invite-link token input. Partner acceptance is required before a pair becomes active.
- `game:start` requires an active pair, creates or reuses a durable `pending_acceptance` game session, and sends the partner an invite. Cards are sent to both users only after the partner accepts.
- Card callbacks are session-scoped for normal gameplay. Typed answers, skips, and in-person completions are stored in SQLite; typed answers are AES-GCM encrypted.
- Reveal waits for both pair members. The card is recorded in `pair_card_history`, and the pair advances after 6 completed cards when the next level exists.
- Photo-message callbacks use `editMessageCaption` or delete/send fallback behavior; old edit failures produce one fallback message.
- Theme menus support color, style, style property overrides, font, built-in background, uploaded background, and upload deletion.
- Background upload is wired through Telegram photo messages and JPEG/PNG/WebP document messages, processes images to WebP, and stores only processed derivatives in MinIO when object storage is configured.
- Telegram Stars payment flow validates `XTR`, checks invoice payload user against the payer, stores idempotent purchase receipts, and grants entitlements.
- Admin flows support slash commands and a catalog-generated inline menu, resolve known `@username` targets, validate entitlements, and audit every grant/revoke.
- Pair break uses a confirmation step, ends the active pair, cancels current sessions, clears shared backgrounds, clears transient state, and notifies the partner.
- Account deletion uses a two-step confirmation, performs active-pair cleanup/notification, removes owned uploaded objects from MinIO when configured, deletes the SQLite user row, and clears Redis FSM/pending completion state.
- Custom questions can be created, listed, soft-deleted, and selected in normal gameplay through the same no-repeat path as stock cards.
- Journal opens revealed session history for active pair members, decrypting typed answers only for that pair and hiding mature entries if either partner loses mature access.
- Before reveal, non-premium pairs see a cadence-limited support prompt to both users and reveal waits `SupportPromptDelay`; premium on either partner suppresses the prompt.
- When `FEATURE_INLINE_MODE=true`, inline queries receive one personal text-only article result with a safe localized card.
- Redis-backed pair locks protect game mutations and Redis rate-limit counters guard upload, pairing, inline, and callback-heavy flows.

## Resolved Issues From Earlier Audits

- Settings language changes no longer restart completed onboarding.
- Settings theme color changes no longer re-run onboarding completion side effects.
- Recognized command/menu interrupts clear stale FSM state.
- Pairing and custom-color prompts have inline back paths.
- Contact messages outside pairing mode no longer trigger a pairing-specific error.
- Active pair text uses display names instead of raw Telegram IDs.
- Missing i18n keys for pair/game/payment/settings flows have matching production catalog entries.
- Account deletion is implemented with SQLite foreign-key cascades enabled.
- The game no longer sends the same first preview card on every start; card selection is pair-level, deterministic-shuffled, and no-repeat until level deck exhaustion.
- User theme color, selected style, selected font, and selected background feed rendered card input.
- Photo callback edit failures fall back to a single message.
- Empty typed answers keep the user in answer-entry mode and prompt again.

## Remaining Lower-Risk Cleanup

- Remove or repurpose Redis pending-completion helpers now that synchronized gameplay is durable.
- Either wire `render:file:{hash}` caching into Telegram send paths or document it as a future optimization.
- Decide whether `migrations/001_init.sql` or `internal/storage/schemaSQL` is the source of truth; both currently need to stay synchronized.
