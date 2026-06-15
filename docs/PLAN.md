# WRNRS / між нами. Implementation Plan

## Summary

Build a Go Telegram bot for a synchronized relationship card game for one active pair per user. SQLite stores durable profiles, pairs, answers, card history, uploads, purchases, admin grants, and support-prompt cadence. Redis stores active flows and gameplay locks. MinIO stores processed user-uploaded `.webp` backgrounds. JSON stores multilingual cards, UI strings, styles, and built-in backgrounds.

Locked product decisions:

- Locale brand: `між нами.` in Ukrainian, `WRNRS` in English.
- Users complete cards by typed answer, `Answered in person`, or `Skip`.
- Typed answers reveal only after both partners complete the card.
- `Answered in person` requires both partners to tap.
- Safe deck works for everyone.
- Direct sexual cards require both partners to confirm 18+ and separately opt into mature content.
- No card repeats for a pair until the eligible level deck is exhausted.
- Levels unlock after 6 completed cards; skips and in-person completions count.
- Basic onboarding includes full-card theme color picker, built-in backgrounds, and user uploads.
- Each user gets 3 free active uploaded backgrounds.
- Uploaded backgrounds are converted to crisp WebP, stored in MinIO, and shared with the active pair while the pair exists.
- Premium access suppresses donation prompts and unlocks all current cosmetics.
- Admin can grant/revoke lifetime premium and cosmetic entitlements through an inline admin menu.
- Non-premium pairs see a warm Monobank support message at most once every 48 hours before reveal, with a 3-second delay.
- If either partner has premium, the pair sees no donation interstitial.
- A persistent `Cancel / Reset` reply-keyboard button always clears the current stuck flow and returns to main menu without deleting account/game data.

References: Telegram Bot API and Telegram Stars payments. Context7 could not run in this environment because `npx` is unavailable.

## Architecture & Data Flow

Use one Go bot service with packages for:

- `telegram`: webhook/long polling, update router, callback parsing, Bot API client.
- `i18n`: localized UI strings and brand text.
- `content`: question/style/background JSON loading, validation, filtering, no-repeat deck selection.
- `onboarding`: language, name, gender, optional contact, 18+ self-attestation, mature opt-in, color/theme setup.
- `pairing`: contact, username, user ID, and invite-link pairing with required partner acceptance.
- `game`: synchronized card state, answer barrier, reveal flow, donation interstitial, level progression.
- `render`: warm romantic card renderer, uploaded background processing, theme tokens.
- `storage`: SQLite repositories, Redis state, MinIO object store.
- `payments`: Telegram Stars purchases for premium/cosmetics.
- `admin`: admin-only inline grant/revoke menu.

Runtime flow:

1. Telegram update enters router.
2. Router loads user/pair from SQLite and transient state from Redis.
3. Handler processes onboarding, pairing, gameplay, admin, payment, upload, settings, or reset.
4. Active pair gameplay is updated under `lock:pair:{pair_id}` in Redis.
5. Completed cards, answers, unlocks, and support-prompt timestamps are persisted in SQLite.
6. Before reveal, game checks pair premium state and `pair_support_prompt_state`.
7. If neither partner is premium and last prompt is older than 48 hours, send localized warm support message with Monobank button and copyable card number, wait 3 seconds, persist prompt timestamp, then reveal.
8. Renderer generates per-user localized cards using the recipient’s language, theme, and entitlements.

## Database Schema

SQLite durable tables:

- `users`: Telegram ID, username, name, gender, language, optional encrypted phone, phone hash, 18+ flag/timestamp, mature opt-in/timestamp, selected color, selected style, selected background, timestamps.
- `pairs`: two user IDs, status, active level, highest unlocked level, timestamps; repository enforces one active pair per user.
- `pair_requests`: requester, target identifiers, invite token, status, expiry.
- `game_sessions`: pair, level, question, status, timestamps.
- `game_answers`: session, user, completion type (`typed`, `skip`, `in_person`), encrypted answer text, reveal timestamp.
- `pair_card_history`: pair, question, level, deck cycle, completed timestamp.
- `pair_support_prompt_state`: pair, `last_prompted_at`, `last_prompt_message_id`.
- `theme_assets`: built-in or uploaded backgrounds, owner, MinIO key, size, dimensions, status.
- `pair_theme_shares`: pair, asset, shared by, status; removed when pair breaks or either side withdraws.
- `purchase_receipts`: Telegram Stars receipts, SKU, `telegram_payment_charge_id`, status.
- `entitlements`: user, type (`premium_access`, `style`, `font`, `background`), ID, source (`purchase`, `admin_grant`), optional expiry; v1 premium is lifetime.
- `admin_audit_log`: admin, target, action, entitlement, timestamp.

Redis keys:

- `fsm:user:{telegram_id}`: current conversational flow, TTL 24h.
- `game:pair:{pair_id}`: active card state and message IDs, TTL 30d.
- `lock:pair:{pair_id}`: short callback lock.
- `render:file:{hash}`: Telegram file ID cache.
- `pair_invite:{token}`: pending invite, TTL 7d.
- `rate:user:{telegram_id}`: abuse limits.

GDPR wipe deletes the user, phone data, owned uploads in MinIO, active pair, shared pair journal, answers, card history, entitlements, support state, and Redis keys. The remaining partner is notified that the pair was removed.

## Content Structure

Use:

- `content/questions.v1.json`
- `content/i18n/uk.json`
- `content/i18n/en.json`
- `content/styles.v1.json`
- `content/backgrounds.v1.json`
- `content/fonts.v1.json`

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
- Mature history is hidden if either partner disables mature access.
- Direct sexual cards are mature; emotionally deep Level 3 cards remain safe unless tagged mature.
- Deck selection is pair-level and language-independent so both users get the same logical card.

No-repeat rules:

- For `pair + level + maturity eligibility`, draw unseen cards first.
- When exhausted, increment deck cycle and reshuffle.
- Shuffle deterministically by `pair_id + level + cycle`.
- Persist seen/completed cards in SQLite so Redis loss does not cause repeats.

## Gameplay UX

Onboarding:

1. `/start`
2. Language.
3. Name.
4. Gender.
5. Optional own contact.
6. 18+ self-attestation: store only boolean and timestamp.
7. Separate mature-content opt-in for direct mature prompts.
8. Theme setup: preset swatches plus custom hex.
9. Optional built-in or uploaded background.
10. Pairing.

Main menu:

- Start / Resume.
- Pair settings.
- Answer journal.
- Theme settings.
- Upload backgrounds.
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
- `Change theme`
- `Pause`
- `Menu`
- `Cancel / Reset`

Current vertical-slice behavior:

- Card inline callbacks are question-scoped, e.g. `game:answer:q001`, with backward-compatible support for older unscoped callbacks.
- Photo card callbacks edit the existing photo caption and inline keyboard via Telegram edit methods instead of trying to edit photo messages as text.
- Typed answers, skips, and in-person confirmations are stored as transient Redis pending completions until the durable synchronized pair-session engine is wired through SQLite.
- Empty typed answers keep the user in the answer-entry FSM and prompt again instead of falling back to the main menu.
- If Telegram can no longer edit an old message, the bot sends one localized fallback message rather than failing the action.
- Settings language changes save language directly for completed users and do not restart onboarding.
- Settings theme changes update the saved color without re-marking onboarding complete.
- Account deletion uses a two-step settings confirmation, deletes the SQLite user row with foreign-key cascades enabled, and clears Redis FSM/game completion state.

Reveal behavior:

- Typed answer can be edited until both partners complete the card.
- Reveal waits for both users.
- If donation interstitial is due, it appears before reveal, waits 3 seconds, then reveal proceeds.
- After reveal, both users receive `Next card`.

## Monetization, Donations, Admin

Premium:

- `premium_access` is a dedicated entitlement.
- Premium suppresses donation prompts for the whole pair if either partner has it.
- Premium unlocks all current styles, fonts, built-in backgrounds, and cosmetic packs.
- Premium is lifetime in v1 unless admin revokes it.

Telegram Stars:

- Digital unlocks use Telegram Stars with `currency='XTR'`.
- Flow: `sendInvoice`, `pre_checkout_query`, `answerPreCheckoutQuery`, `successful_payment`, verify payload user against the actual payer, store `telegram_payment_charge_id`, then grant entitlement with the receipt linked.
- `/paysupport` exists for disputes/refunds.

Monobank support:

- Optional donation only; it must not unlock digital goods.
- Configured by env: `DONATION_MONOBANK_URL`, `DONATION_CARD_NUMBER`.
- Warm localized support message appears before reveal at most once every 48 hours per non-premium pair.
- Message includes Monobank URL button and copy-friendly card number.
- If either partner has premium, skip entirely.

Admin v1:

- Admin IDs from `ADMIN_TELEGRAM_IDS`.
- Inline admin menu, not slash-command-only.
- Lookup user by Telegram ID or username.
- Grant/revoke premium access.
- Grant/revoke specific styles, fonts, and backgrounds.
- Write every action to `admin_audit_log`.
- Admin cannot read private answers in v1.

## Implementation Phases

1. Scaffold Go service, config, Docker Compose, SQLite, Redis, MinIO, Telegram router, healthcheck.
2. Add migrations and repositories for users, pairs, requests, sessions, answers, card history, assets, entitlements, purchases, admin audit, and support prompts.
3. Build i18n and content loaders with validation for locales, mature tags, levels, styles, and duplicate IDs.
4. Implement onboarding, including 18+ confirmation, mature opt-in, color picker, and upload flow.
5. Implement image processing: accept JPEG/PNG/WebP up to 10MB, strip metadata, crop/resize, encode WebP, store in MinIO.
6. Implement pairing with required partner acceptance.
7. Implement game engine: synchronized card state, no-repeat deck, fixed 6-card level unlock, typed/skip/in-person completion.
8. Implement reveal barrier, answer journal, mature-history hiding, and donation interstitial.
9. Implement renderer: warm romantic card system, localized brand, themes, built-in backgrounds, uploaded backgrounds, render cache.
10. Implement Telegram Stars purchases and premium/cosmetic entitlements.
11. Implement admin inline menu for grant/revoke premium and cosmetics.
12. Implement `Cancel / Reset`, GDPR wipe, and pair break cleanup.
13. Add inline-mode skeleton behind a feature flag for future single-card use.
14. Configure production webhook delivery through Caddy on `wrnrs.dobrovolskyi.com.ua`, proxying to the non-default local Compose binding `127.0.0.1:18087`.
15. Harden: rate limits, logs, backups, MinIO lifecycle cleanup, deployment docs.

## Test Plan

- Two users with different languages receive the same logical card localized independently.
- Mature cards appear only when both users are 18+ and mature-opted-in.
- Mature journal entries hide after either user disables mature access.
- No repeats until eligible deck exhaustion.
- Level 2 unlocks after 6 completed Level 1 cards.
- Typed answers reveal only after both complete.
- In-person completion requires both taps.
- Donation interstitial appears once per 48 hours only when neither partner has premium.
- Donation interstitial waits 3 seconds before reveal.
- Premium suppresses prompts for both partners and unlocks all cosmetics.
- Admin can grant/revoke premium and cosmetics, with audit log entries.
- `Cancel / Reset` clears stuck flow without deleting durable data.
- Uploaded backgrounds convert to WebP, store in MinIO, share only while pair is active, and disappear from pair use after withdrawal or pair break.
- GDPR wipe deletes shared pair journal and owned uploads.
