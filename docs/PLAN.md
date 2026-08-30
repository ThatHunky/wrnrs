# WRNRS / між нами. Implementation Plan

## Summary

Build a Go Telegram bot for a synchronized relationship card game for one active pair per user. SQLite stores durable profiles, pairs, sessions, answer snapshots/history, card history, uploads, pair shares, purchases, admin grants, support-prompt timestamps, and custom questions. Redis stores active conversational flows, render cache entries, pair locks, and user rate-limit counters. MinIO stores processed user-uploaded `.webp` backgrounds. JSON stores multilingual cards, UI strings, styles, fonts, and built-in background metadata.

Locked product decisions:

- Locale brand: `між нами.` in Ukrainian, `WRNRS` in English.
- Users complete cards by typed answer, `Answered in person`, or `Skip`.
- Typed answers reveal only after both partners complete the card.
- `Answered in person` requires both partners to tap.
- Safe deck works for everyone.
- Direct sexual cards require both partners to confirm 18+ and separately opt into mature content.
- No card repeats for a pair until the eligible level deck is exhausted.
- Levels unlock after 6 completed cards; skips and in-person completions count.
- Basic onboarding includes language, name, gender, optional own contact, 18+ self-attestation, mature opt-in, theme color, and optional background skip/default/upload.
- Each user gets 3 free active uploaded backgrounds.
- Uploaded backgrounds are converted to crisp WebP and stored in MinIO when configured. Active-pair uploaded-background sharing and pair-break cleanup are wired.
- Premium access unlocks current premium styles, fonts, and built-in backgrounds. Premium on either partner suppresses before-reveal support prompts for the pair.
- Admin can grant/revoke lifetime premium and cosmetic entitlements. Slash commands and the inline menu cover premium/styles/fonts/backgrounds from catalogs.
- Non-premium pairs see a warm Monobank support message at most once every 48 hours before reveal, with a configurable delay.
- If either partner has premium, the pair sees no donation interstitial.
- A persistent `Cancel / Reset` reply-keyboard button always clears the current stuck flow and returns to main menu without deleting account/game data.

This plan uses status language: `done` means implemented in the repository, `partial` means some shipped surface exists but the planned workflow is incomplete, and `planned` means no runtime wiring exists yet.

## Architecture & Data Flow

Use one Go bot service with packages for:

- `telegram`: webhook/long polling, update router, callback parsing, Bot API client.
- `i18n`: localized UI strings and brand text.
- `content`: question/style/background/font JSON loading, validation, filtering, no-repeat deck selection.
- `onboarding`: language, name, gender, optional own-contact phone hash, 18+ self-attestation, mature opt-in, color/theme setup, and optional background setup.
- `pairing`: contact, username, user ID, and invite-link pairing with required partner acceptance.
- `game`: synchronized card state, answer barrier, reveal flow, custom/stock no-repeat deck selection, level progression, and support-prompt cadence.
- `render`: warm romantic card renderer, uploaded background processing, theme tokens.
- `storage`: SQLite repositories, Redis state, MinIO object store.
- `payments`: Telegram Stars purchases for premium/cosmetics.
- `admin`: admin-only inline grant/revoke menu.

Runtime flow:

1. Telegram update enters router.
2. Router loads user/pair from SQLite and transient state from Redis.
3. Handler processes onboarding, pairing, gameplay, admin, payment, upload, settings, or reset.
4. Active pair gameplay is stored in SQLite `game_sessions` and `game_answers`; Redis is used for short conversational FSM state.
5. Completed cards, encrypted typed answers, purchases, entitlements, uploaded-asset metadata, admin audit rows, custom questions, and support-prompt timestamps are persisted in SQLite.
6. Renderer generates per-user localized cards using the recipient's language, theme, selected font/style/background, and entitlements.
7. Pair gameplay mutations are wrapped in Redis `lock:pair:{pair_id}` when Redis is available.
8. Before reveal, the app checks pair premium state plus `pair_support_prompt_state`; if a support prompt is due, it sends the Monobank prompt, waits `SupportPromptDelay`, persists the timestamp, then reveals.

## Database Schema

SQLite durable tables:

- `users`: Telegram ID, username, name, gender, language, optional encrypted phone, phone hash, 18+ flag/timestamp, mature opt-in/timestamp, selected color, selected style, selected background, timestamps.
- `pairs`: two user IDs, status, active level, highest unlocked level, timestamps; repository enforces one active pair per user.
- `pair_requests`: requester, target identifiers, invite token, status, expiry.
- `game_sessions`: pair, level, question ID/source, localized question snapshots, mature requirement, status, timestamps.
- `game_answers`: session, user, completion type (`typed`, `skip`, `in_person`), encrypted answer text, reveal timestamp.
- `pair_card_history`: pair, question, level, deck cycle, completed timestamp.
- `pair_support_prompt_state`: pair, `last_prompted_at`, `last_prompt_message_id`.
- `theme_assets`: uploaded backgrounds, owner, MinIO key, size, dimensions, status. Built-in backgrounds live in JSON metadata and generated asset files.
- `pair_theme_shares`: active-pair uploaded-background shares that are cleared on pair break/account deletion.
- `purchase_receipts`: Telegram Stars receipts, SKU, `telegram_payment_charge_id`, status.
- `entitlements`: user, type (`premium_access`, `style`, `font`, `background`), ID, source (`purchase`, `admin_grant`), optional expiry; v1 premium is lifetime.
- `admin_audit_log`: admin, target, action, entitlement, timestamp.
- `custom_questions`: user-authored prompts managed from settings, soft-deleted for future selection, and injected into active-level gameplay as safe custom cards.

Redis keys:

- `fsm:user:{telegram_id}`: current conversational flow, TTL 24h.
- `game:completion:user:{telegram_id}`: legacy pending-completion helper retained by the Redis adapter.
- `render:file:{hash}`: Telegram file ID cache helper.
- `lock:pair:{pair_id}`: best-effort pair callback lock around game mutations.
- `rate:user:{telegram_id}:{action}`: fixed-window per-user limits for upload, pairing, inline, and callback-heavy game flows.
- Pair invites are durable SQLite rows; Redis invite mirrors remain undecided.

GDPR wipe ends the active pair when present, notifies the remaining partner, clears pair shares/current sessions/transient state, deletes owned uploads in MinIO when configured, deletes the SQLite user row, and cascades session/answer/history/receipt/entitlement/upload metadata.

## Content Structure

Use:

- `content/questions.v1.json`
- `content/i18n/uk.json`
- `content/i18n/en.json`
- `content/styles.v1.json`
- `content/backgrounds.v1.json`
- `content/fonts.v1.json`

Current deck inventory:

- Level 1: 6 safe cards.
- Level 2: 10 safe cards.
- Level 3: 16 cards, including 2 mature cards.
- Styles: `default_warm` plus premium `premium_velvet`.
- Fonts: free `nunito_regular`; premium `google_sans_regular`, `roboto_slab_regular`, `caveat_regular`, and `pacifico_regular`.
- Built-in backgrounds: free `builtin_blush_gradient` and premium `builtin_candle_glow`.

Question shape:

```json
{
  "id": "q023",
  "level": 3,
  "tags": ["mature", "sex"],
  "requires_mature_opt_in": true,
  "mode": "both_players",
  "text": {
    "uk": "Що для тебе більше в сексі — пристрасть чи близькість?",
    "en": "What matters more to you in sex: passion or intimacy?"
  }
}
```

Filtering rules:

- Card must have text for the recipient’s language.
- Mature cards require both partners to be 18+ and mature-opted-in.
- Mature journal entries are hidden if either partner disables mature access.
- Direct sexual cards are mature; emotionally deep Level 3 cards remain safe unless tagged mature.
- Deck selection is pair-level and language-independent so both users get the same logical card.

No-repeat rules:

- For `pair + level + maturity eligibility`, draw unseen cards first.
- When exhausted, increment deck cycle and reshuffle.
- Shuffle deterministically by `pair_id + level + cycle`.
- Persist seen/completed cards in SQLite so Redis loss does not cause repeats.

## Gameplay UX

Current onboarding:

1. `/start`
2. Language.
3. Name.
4. Gender.
5. 18+ self-attestation: store only boolean and timestamp.
6. Separate mature-content opt-in for direct mature prompts.
7. Theme color setup: preset swatches plus custom hex.
8. Optional background setup: default, skip, or upload.

The normal path includes an optional own-contact step before adult confirmation. It stores only `phone_lookup_hash` and can be skipped. Pairing is offered immediately from the post-onboarding main menu without blocking access.

Main menu:

- Start / Resume.
- Pair settings.
- Answer journal.
- Theme settings: color, style, style overrides, font, built-in backgrounds, uploaded backgrounds.
- Store / premium.
- Settings.
- Delete account.
- Context status: display name, selected theme color, pair status, partner name, active level, and completed card count when available.

Always-visible safety control:

- Persistent reply keyboard includes `Menu` and `Cancel / Reset`.
- `Menu` and `Cancel / Reset` clear only the current FSM state: stuck onboarding, pairing, upload, answer entry, admin flow, or settings flow.
- Recognized command interrupts such as `/start`, `/paysupport`, and `/admin` clear the current FSM before handling the command.
- In gameplay, it returns to the current card/main menu without deleting answers, pair, or account.
- Destructive operations still require separate confirmation.

Card controls:

- `Type answer`
- `Answered in person`
- `Skip`
- `Pause`
- `Menu`
- `Cancel / Reset`

Current vertical-slice behavior:

- Card inline callbacks are session-scoped, e.g. `game:answer:{session_id}`. Older question-scoped callbacks are treated as stale in normal gameplay, while admin test-card mode still uses card IDs.
- Photo card callbacks edit the existing photo caption and inline keyboard via Telegram edit methods instead of trying to edit photo messages as text.
- Typed answers, skips, and in-person confirmations are stored durably in SQLite through the synchronized pair-session engine. Typed answer bytes are encrypted with AES-GCM.
- Empty typed answers keep the user in the answer-entry FSM and prompt again instead of falling back to the main menu.
- If Telegram can no longer edit an old message, the bot sends one localized fallback message rather than failing the action.
- Settings language changes save language directly for completed users and do not restart onboarding.
- Settings theme changes update the saved color without re-marking onboarding complete.
- Pair break uses a two-step confirmation, ends the pair, cancels current sessions, clears pair background shares, resets selected shared backgrounds, clears transient state, and notifies the partner.
- Account deletion uses a two-step settings confirmation, performs active-pair cleanup/notification, deletes the SQLite user row with foreign-key cascades enabled, and clears Redis FSM/game completion state.
- Custom questions can be created and soft-deleted from settings and participate in normal active-level deck selection.

Reveal behavior:

- Typed answer can be edited until both partners complete the card.
- Reveal waits for both users.
- If the support interstitial is due, it appears before reveal for both partners, waits `SupportPromptDelay`, then reveal proceeds.
- After reveal, both users receive `Next card`.

## Monetization, Donations, Admin

Premium:

- `premium_access` is a dedicated entitlement.
- Premium unlocks all current premium styles, fonts, and built-in backgrounds.
- Premium suppresses donation prompts for the whole pair if either partner has it.
- Premium is lifetime in v1 unless admin revokes it.

Telegram Stars:

- Digital unlocks use Telegram Stars with `currency='XTR'`.
- Flow: `sendInvoice`, `pre_checkout_query`, `answerPreCheckoutQuery`, `successful_payment`, verify payload user against the actual payer, store `telegram_payment_charge_id`, then grant entitlement with the receipt linked.
- `/paysupport` exists for disputes/refunds.

Monobank support:

- Optional donation only; it must not unlock digital goods.
- Configured by env: `DONATION_MONOBANK_URL`, `DONATION_CARD_NUMBER`.
- `/paysupport` sends localized support text and optional Monobank/card details.
- The before-reveal flow uses `pair_support_prompt_state` so a warm localized support message appears at most once every 48 hours per non-premium pair.
- The before-reveal message includes the configured Monobank URL/card details in plain text, waits `SupportPromptDelay`, then reveals.
- If either partner has premium, the before-reveal prompt is skipped entirely.

Admin v1:

- Admin IDs from `ADMIN_TELEGRAM_IDS`.
- Inline admin menu plus slash commands.
- Lookup user by Telegram ID or username.
- Grant/revoke premium access.
- Slash commands can grant/revoke specific styles, fonts, and backgrounds.
- Inline admin menu enumerates grant/revoke choices for premium, styles, fonts, and backgrounds.
- Write every action to `admin_audit_log`.
- Admin cannot read private answers in v1.

## Implementation Phases

1. done - Scaffold Go service, config, Docker Compose, SQLite, Redis, MinIO, Telegram router, healthcheck.
2. done - Add migrations and repositories for users, pairs, requests, sessions, answers, card history, assets, entitlements, purchases, admin audit, support prompts, and custom questions.
3. done - Build i18n and content loaders with validation for locales, mature tags, levels, styles, fonts, backgrounds, and duplicate IDs.
4. done - Implement onboarding. Language, name, optional own contact, gender, 18+ confirmation, mature opt-in, color picker, and optional background skip/default/upload are wired.
5. done - Implement image processing. The processor accepts JPEG/PNG/WebP bytes and emits metadata-stripped WebP. Runtime upload wiring accepts Telegram photo messages and JPEG/PNG/WebP document messages.
6. done - Implement pairing with required partner acceptance.
7. done - Implement game engine: synchronized card state, no-repeat deck, fixed 6-card level unlock, typed/skip/in-person completion.
8. done - Implement reveal barrier, answer journal, mature-history hiding, and donation interstitial.
9. partial - Implement renderer: warm romantic card system, localized brand, themes, built-in backgrounds, uploaded backgrounds, render cache. Rendering, themes, built-ins, and uploads are done; Telegram file-ID render cache is present as a Redis helper but not used by send paths.
10. done - Implement Telegram Stars purchases and premium/cosmetic entitlements.
11. done - Implement admin inline menu for grant/revoke premium and cosmetics with shared validation/audit.
12. done - Implement `Cancel / Reset`, GDPR wipe, pair break cleanup, and partner notification.
13. done - Add text-only inline mode behind `FEATURE_INLINE_MODE`.
14. done - Configure production webhook delivery through Caddy on `wrnrs.dobrovolskyi.com.ua`, proxying to the non-default local Compose binding `127.0.0.1:18087`.
15. partial - Harden: logs, backups, deployment docs, rate limits, and pair locks exist; MinIO lifecycle cleanup remains operational follow-up.

## Unfinished Planned Features

- The positions randomiser (module 1 on the shared `internal/catalog`/`internal/modules` framework) is the only superapp module built so far. Modules 2-11 from `docs/superpowers/specs/2026-08-29-couples-superapp-positions-design.md` §3 — action dice, truth-or-dare, date ideas, a matched wishlist, question-of-the-day/streaks, mood check-ins, compliment cards, calendar/anniversaries, shared lists, and pair achievements — remain planned only; several need a not-yet-built `internal/scheduler`.
- Wire Redis `render:file:{hash}` caching into the card game's own send paths — the positions module already uses this exact helper (`CacheFileID`/`FileID`) to cache Telegram file ids for its own photos.
- Decide whether pair-invite Redis mirrors are still useful now that invites are durable SQLite rows.
- Define MinIO lifecycle/retention policy for deleted or orphaned objects beyond app-driven deletes.
- Consider richer inline image mode only if public JPEG URL hosting is added; v1 intentionally ships text-only inline articles.

## Test Plan

- Two users with different languages receive the same logical card localized independently.
- Mature cards appear only when both users are 18+ and mature-opted-in.
- Mature journal entries hide after either user disables mature access.
- No repeats until eligible deck exhaustion.
- Level 2 unlocks after 6 completed Level 1 cards.
- Typed answers reveal only after both complete.
- In-person completion requires both taps.
- Donation interstitial appears once per 48 hours only when neither partner has premium.
- Donation interstitial waits `SupportPromptDelay` before reveal.
- Premium unlocks all current premium cosmetics and suppresses before-reveal support prompts for both partners.
- Admin can grant/revoke premium and cosmetics, with audit log entries.
- `Cancel / Reset` clears stuck flow without deleting durable data.
- Uploaded backgrounds convert to WebP and store in MinIO when configured.
- Uploaded backgrounds share only while pair is active and disappear from pair use after pair break/account deletion.
- GDPR wipe deletes owned uploads and durable user data, notifies the remaining partner, and clears pair-scoped runtime state known to the app.
