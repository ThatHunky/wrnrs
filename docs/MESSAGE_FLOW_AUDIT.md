# Message Flow Audit & Fix Plan

Audit of all message-handling paths in `internal/app/app.go` and supporting packages.
Each section describes a confirmed or likely bug, root cause, impact, and proposed fix.

Implementation status as of this pass:

- Implemented: Bugs 1-6, 8-9, 11-19, 24-25, and 27.
- Partially addressed: Bug 10 is mitigated for recognized command/menu interrupts, but durable onboarding-step resume still requires a SQLite onboarding progress field.
- Deferred larger vertical slices: Bugs 7, 20-23, and 26 require the synchronized pair-game engine, upload flow, pair locks, donation reveal flow, or rate limiter wiring and should be implemented as separate slices with their own tests.

---

## Bug 1 — Settings → Change Language re-triggers the full onboarding (name prompt)

**Reported:** Changing language from Settings asks for the name again even though onboarding is already complete.

**Root cause:**
`"onboarding:language_menu"` callback (line 252) sets FSM to `StepLanguage`.
Then `"onboarding:language:{lang}"` callback (line 203) unconditionally sets FSM to `StepName` and shows the name prompt — regardless of whether `onboarding_status = 'complete'`.

The onboarding callbacks are entirely linear and never check whether the user has already finished onboarding.

**Impact:** Post-onboarding users who change language are forced back into the full onboarding wizard.

**Proposed fix:**
In the `onboarding:language:{lang}` callback handler (line 203-211), after updating the language:
1. Check `repo.UserOnboardingComplete(ctx, cb.From.ID)`.
2. If **complete** → clear FSM, show a "language saved" confirmation, return to settings or main menu.
3. If **not complete** → proceed to StepName as currently.

The same guard should be added to `onboarding:language_menu` (line 252): when onboarding is complete, the callback should show a language picker that routes to the "just save language" path, not the onboarding flow.

---

## Bug 2 — Theme color from Settings marks onboarding complete again

**Root cause:**
`"theme:menu"` callback (line 281) sets FSM to `StepThemeColor`.
When a color is selected via `"theme:color:{hex}"` (line 293), it calls `repo.MarkOnboardingComplete()` unconditionally (line 308).

If a user who already completed onboarding changes their theme color, they go through the same code path that finishes onboarding. While it's benign today (it just re-sets status to `'complete'`), it is semantically wrong and will break if `MarkOnboardingComplete` ever acquires side effects (analytics events, welcome messages, etc.).

**Impact:** Latent bug; will cause duplicated welcome or analytics events when those are added.

**Proposed fix:**
The `theme:color:{hex}` handler should:
1. Check if onboarding is already complete.
2. If **yes** → only save the color, clear FSM, return a "color saved" confirmation. Do **not** call `MarkOnboardingComplete`.
3. If **no** → call `MarkOnboardingComplete` as today, completing the wizard.

---

## Bug 3 — FSM state conflicts / handler priority issues

**Root cause:**
`handleMessage()` (line 109) processes incoming text in this priority order:

```
1. /cancel, /reset
2. "Menu" / "Меню"
3. SuccessfulPayment
4. /start ...
5. /paysupport
6. /admin
7. handleGameMessage()     — checks FSM for "game:await_answer:*"
8. handleAdminText()       — checks FSM for "admin:grant:*", "admin:revoke:*"
9. handleOnboardingText()  — checks FSM for "onboarding:name", "onboarding:theme_color"
10. handlePairingMessage() — checks FSM for "pairing:await_identifier"
11. fallback → sendMainMenu
```

**Problem:** If the FSM is set to `onboarding:name` (awaiting the user's name) and the user types text that happens to match a command like `/paysupport`, the command wins over the onboarding handler. This means:
- During name entry, typing `/paysupport` goes to payment support instead of treating it as a name.
- During name entry, typing `/admin` goes to admin menu for admins.
- Any `/start` re-entry during onboarding re-runs the start flow.

While commands overriding FSM is generally acceptable (escape hatch), the **FSM is not cleared** when these early exits happen. The user is left in a dangling FSM state (e.g., `onboarding:name`) after the command runs. Then their next message may be captured by `handleOnboardingText` unexpectedly.

**Impact:** Ghost FSM state causes subsequent messages to be misrouted. A user might type `/paysupport` during onboarding, see the payment info, then their next message is interpreted as a name.

**Proposed fix:**
- Add `state.ClearFSM()` calls before handling `/paysupport` and `/admin` commands.
- Alternatively, add a dedicated early exit that clears FSM for any recognized `/command` before the FSM handlers run.

---

## Bug 4 — `pair:menu` callback enters FSM without checking existing pair

**Root cause:**
`"pair:menu"` callback (line 270):
1. Checks if the user already has an active pair.
2. If yes → shows the active pair info.
3. If no → sets FSM to `pairing:await_identifier` and shows instructions.

**Problem:** The pairing instructions message is shown via `editCallbackScreen` with `nil` reply markup (line 280). The user sees text instructions but **no inline keyboard and no reply keyboard**. They're stuck with whatever persistent keyboard was previously visible. There's no "Back" or "Cancel" button — the only escape is typing `/cancel` or the persistent "Cancel / Reset" button (if visible).

**Impact:** Dead-end UX. After entering the Pair menu, user has no visible way to go back to main menu unless they know about the persistent keyboard buttons.

**Proposed fix:**
- Add a "Back to menu" inline button to the pairing instructions screen.
- Or at minimum send a `PersistentKeyboard` along with the instructions.

---

## Bug 5 — Arbitrary text during pairing FSM is treated as identifier, confusing error

**Root cause:**
When FSM = `pairing:await_identifier`, any text message that isn't a valid `@username`, numeric ID, or pair token gets the response `"pair.invalid_identifier"`. This includes natural language like "hello", "what?", "хто ти?", etc.

The user likely doesn't understand why they're getting identifier validation errors.

**Impact:** Confusing UX when users accidentally send text while in pairing mode.

**Proposed fix:**
This is a UX issue rather than a logic bug. The pairing instructions text should clearly explain what formats are accepted. Additionally, consider adding a `"Back to menu"` button so users can easily exit.

---

## Bug 6 — Contact shared outside of pairing FSM gets a confusing response

**Root cause:**
`handlePairingMessage()` (line 414):
- If FSM ≠ `pairing:await_identifier` **and** `msg.Contact != nil` → sends `"pair.open_first"` ("Open Pair first.").
- If FSM ≠ `pairing:await_identifier` **and** `msg.Contact == nil` → returns `false, nil` (falls through).

**Problem:** If a user shares a contact at any time outside pairing mode, they get the cryptic "Open Pair first" message. This is confusing for users who share contacts for other reasons or accidentally.

**Impact:** Minor UX confusion, but unexpected behavior.

**Proposed fix:**
Consider removing this early contact interception, or making the message more descriptive. Users should be able to share contacts without getting pairing-related messages when they're not in pairing mode.

---

## Bug 7 — `game:start` callback always shows the same first card

**Root cause:**
`previewCard()` (line 680):
```go
cards := a.deck.EligibleCards(content.Eligibility{Level: 1})
card := cards[0]
```
Always picks `cards[0]`. No history, no randomization, no pair context.

**Impact:** Every time the user taps "Start / Resume", they see the exact same card. The game never progresses.

**Proposed fix:**
This is a known limitation documented in ARCHITECTURE.md ("full pair gameplay can be completed by wiring active-pair repository methods into `internal/app`"). Proper card selection requires:
1. Looking up the user's active pair.
2. Checking `pair_card_history` for already-seen cards.
3. Selecting the next unseen card at the current level.

For now, at minimum: random selection from eligible cards, or a TODO marker making it explicit.

---

## Bug 8 — `game:start` ignores the user's theme color

**Root cause:**
`previewCard()` hardcodes `BaseColor: "#d98c9f"` (line 698) instead of loading the user's saved `theme_base_color` from the database.

**Impact:** The user's chosen theme color is never reflected in rendered cards.

**Proposed fix:**
Load the user's profile from the repository and use their `theme_base_color` for rendering.

---

## Bug 9 — `pair:active` message exposes raw Telegram IDs

**Root cause:**
```go
fmt.Sprintf(a.i18n.Text(lang, "pair.active"), active.UserAID, active.UserBID)
```
(line 275) shows raw numeric Telegram IDs to the user.

**Impact:** Privacy concern and poor UX. Users see something like "Active pair: 1234567890, 9876543210" instead of display names.

**Proposed fix:**
Look up display names for both users and show those instead. Fall back to "Partner" if display name is empty.

---

## Bug 10 — FSM TTL expiry causes silent state loss

**Root cause:**
All FSM values are set with `24*time.Hour` TTL in Redis. If a user starts onboarding but doesn't finish within 24 hours, the FSM key silently expires. When they return:
- `handleOnboardingText` won't match any step.
- Their message falls through to the main menu fallback.
- But `onboarding_status` is still `'new'` in SQLite — they're in limbo.

They'd need to send `/start` again to re-enter onboarding, but `/start` calls `EnsureUser` (which doesn't overwrite existing data) and checks `UserOnboardingComplete`. Since it's not complete, it would restart from the language step — losing any partial progress.

**Impact:** Users who take more than 24 hours to complete onboarding lose their progress and may be confused.

**Proposed fix:**
- When FSM is empty but onboarding is not complete, the `/start` handler or `sendMainMenu` should detect this and offer to resume onboarding from the last completed step.
- This requires storing the last completed onboarding step in SQLite (not just "new"/"complete").

---

## Bug 11 — `theme:color:custom` and hex text entry: no back button, no timeout message

**Root cause:**
When user selects "Custom HEX" from the color keyboard:
- FSM is set to `StepThemeColor`.
- The prompt `"Send a HEX color like #d98c9f."` is shown via `editCallbackScreen` with **no keyboard** (`nil` markup).
- There's no inline "Back" button and no indication of how to cancel.

If the user sends non-hex text, `handleOnboardingText` catches it and re-prompts, but only with `PersistentKeyboard` — no inline options to go back to preset colors.

**Impact:** User is trapped in hex color entry with no obvious way to go back to preset colors.

**Proposed fix:**
- Add a "Back to presets" inline button to the custom color prompt.
- Show the persistent keyboard with Cancel.

---

## Bug 12 — `pair.active` i18n key is missing from catalogs

**Root cause:**
Line 275 uses `a.i18n.Text(lang, "pair.active")` but this key does not appear in the test bundle's string catalog (lines 529-590). If it's also missing from the production catalogs, `i18n.Bundle.Text()` returns the raw key `"pair.active"` as the format string for `fmt.Sprintf` with two `int64` arguments, producing garbage output like `"pair.active%!(EXTRA int64=123, int64=456)"`.

**Impact:** Broken display text when viewing an active pair.

**Proposed fix:**
Add `"pair.active"` key to both UK and EN catalogs with appropriate format strings, e.g.:
- UK: `"Активна пара: %s та %s"`
- EN: `"Active pair: %s and %s"`
(Use display names instead of IDs per Bug 9.)

---

## Bug 13 — `game.not_ready` i18n key is missing from catalogs

**Root cause:**
`previewCard()` line 682 uses `a.i18n.Text(language, "game.not_ready")` but this key isn't in the test catalogs. Same issue as Bug 12.

**Impact:** Raw key shown to user if deck/renderer is nil.

**Proposed fix:**
Add `"game.not_ready"` to both catalogs.

---

## Bug 14 — `payments.success` and `payments.support` i18n keys missing from catalogs

Same pattern as Bugs 12-13 for `"payments.success"` (line 657) and `"payments.support"` (line 666).

---

## Bug 15 — Account data deletion is mentioned in settings but not implemented

**Current state:**
The settings panel text (line 783/794 in `app.go`) says *"Change language, reset the current action, or delete your account"*, and [settingsKeyboard](file:///home/thathunky/bots/wrnrs/internal/app/app.go#L800-L811) only has two buttons: "Change language" and "Main menu". There is no "Delete account" button, no callback handler, and no `DeleteUser` repository method.

**Why it matters:**
- GDPR and Telegram Store guidelines require bots that collect personal data to offer a deletion path.
- The settings text promises a feature that doesn't exist — users will look for it and not find it.

**Proposed implementation:**

### Storage layer — `internal/storage/sqlite.go`

Add a `DeleteUser(ctx, telegramID)` method:
```go
func (r *Repository) DeleteUser(ctx context.Context, telegramID int64) error {
    // Enable foreign keys for cascade
    _, _ = r.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
    _, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE telegram_id = ?`, telegramID)
    return err
}
```
The schema already has `ON DELETE CASCADE` on all child tables (`pairs`, `pair_requests`, `game_sessions`, `game_answers`, `pair_card_history`, `theme_assets`, `pair_theme_shares`, `purchase_receipts`, `entitlements`, `admin_audit_log`), so a single `DELETE FROM users` cascades to all user data.

> [!WARNING]
> SQLite foreign keys are off by default. The `DeleteUser` method must run `PRAGMA foreign_keys = ON` in the same connection. Alternatively, enable it once at connection time in `OpenSQLite`.

### State layer — FSM & Redis cleanup

After SQLite deletion, also call:
- `state.ClearFSM(ctx, userID)`
- `state.ClearPendingGameCompletion(ctx, userID)`

### MinIO cleanup

If the user has uploaded theme assets, their MinIO objects should be cleaned up. Either:
1. Query `theme_assets` for the user's objects **before** the cascading delete.
2. Delete MinIO objects.
3. Then delete the SQLite row.

Or accept orphaned MinIO objects for now and add a background cleanup job later.

### Two-step confirmation flow

Account deletion is destructive, so use a two-step confirmation:

1. **Settings keyboard** — add a `"Delete account"` / `"Видалити акаунт"` button with callback data `"settings:delete_account"`.
2. **Confirmation screen** — callback handler shows a warning message and two buttons:
   - `"Yes, delete everything"` / `"Так, видалити все"` → callback `"settings:delete_confirm"`
   - `"Cancel"` / `"Скасувати"` → callback `"menu:main"` (return to main menu)
3. **Delete handler** — on `"settings:delete_confirm"`:
   - Call `repo.DeleteUser(ctx, userID)`.
   - Clear Redis state.
   - Send a final goodbye message with no keyboard (or `ReplyKeyboardRemove`).
   - The user can restart fresh with `/start`.

### I18n keys needed

| Key | UK | EN |
|-----|----|----|
| `settings.delete_account` | `Видалити акаунт` | `Delete account` |
| `settings.delete_confirm_prompt` | `Це видалить усі дані: профіль, пари, відповіді, покупки. Цю дію не можна скасувати.` | `This will permanently delete all your data: profile, pairs, answers, and purchases. This cannot be undone.` |
| `settings.delete_confirm_button` | `Так, видалити все` | `Yes, delete everything` |
| `settings.deleted` | `Акаунт видалено. Надішли /start щоб почати знову.` | `Account deleted. Send /start to start fresh.` |

### Files to modify

- [sqlite.go](file:///home/thathunky/bots/wrnrs/internal/storage/sqlite.go) — add `DeleteUser` method
- [app.go](file:///home/thathunky/bots/wrnrs/internal/app/app.go) — add callback handlers for `settings:delete_account` and `settings:delete_confirm`, update `settingsKeyboard`
- I18n catalogs (test bundle in [app_test.go](file:///home/thathunky/bots/wrnrs/internal/app/app_test.go), production catalogs)
- [app_test.go](file:///home/thathunky/bots/wrnrs/internal/app/app_test.go) — add deletion tests

---

## Feature 16 — Dynamic main menu with partner status and user context

**Current state:**
The main menu ([sendMainMenu](file:///home/thathunky/bots/wrnrs/internal/app/app.go#L705-L710)) shows a static `"menu.title"` string ("Головне меню" / "Main menu") with no personalization. The [MainMenuKeyboard](file:///home/thathunky/bots/wrnrs/internal/telegram/keyboards.go#L28-L43) is also fully static.

Users get no feedback about their current state: whether they have a partner, what level they're on, how many cards they've completed, or their current theme.

**Why it matters:**
- Users have no idea what's happening in their game without tapping into sub-menus.
- The "Pair" button gives no indication of whether a partner is already linked.
- There's no sense of progress or engagement from the main screen.

**Proposed implementation:**

### New method: `buildMainMenuText`

Replace the static `"menu.title"` text with a dynamically assembled status block:

```
між нами.

👤 Сєва
🎨 Rose (#d98c9f)

💑 Paired with Діана
🃏 Level 1 · 3/12 cards completed

Що хочеш зробити?
```

Or for unpaired users:
```
між нами.

👤 Seva
🎨 Sage (#8da68f)

💔 No partner yet
   Tap "Pair" to invite someone.
```

### Data needed

Load a "menu context" struct from the repository:

```go
type MenuContext struct {
    DisplayName   string
    ThemeColor    string
    HasPair       bool
    PartnerName   string
    ActiveLevel   int
    CardsComplete int
    CardsTotal    int
}
```

This requires a new repo method (or a combined query):
- `UserProfile(ctx, telegramID)` → returns display name, theme color
- `ActivePairForUser(ctx, userID)` → already exists, returns pair data
- `PairCardCount(ctx, pairID, level)` → new, counts completed cards at the current level
- Partner display name lookup → `UserDisplayName(ctx, partnerID)` → new

### Dynamic keyboard labels

Consider updating the "Pair" button label based on state:
- No pair: `"Pair 💔"` / `"Пара 💔"`
- Active pair: `"Pair 💑"` / `"Пара 💑"`

This means `MainMenuKeyboard` needs to accept a context struct or at least a `hasPair bool` parameter, rather than just `language string`.

### I18n keys needed

| Key | UK | EN |
|-----|----|----|
| `menu.header` | `між нами.` | `WRNRS` |
| `menu.status_paired` | `💑 У парі з %s` | `💑 Paired with %s` |
| `menu.status_unpaired` | `💔 Партнера поки немає` | `💔 No partner yet` |
| `menu.status_unpaired_hint` | `Натисни «Пара» щоб запросити.` | `Tap "Pair" to invite someone.` |
| `menu.progress` | `🃏 Рівень %d · %d/%d карток` | `🃏 Level %d · %d/%d cards` |
| `menu.prompt` | `Що хочеш зробити?` | `What would you like to do?` |

### Files to modify

- [sqlite.go](file:///home/thathunky/bots/wrnrs/internal/storage/sqlite.go) — add `UserDisplayName`, `PairCardCount` methods
- [app.go](file:///home/thathunky/bots/wrnrs/internal/app/app.go) — replace `sendMainMenu` with `buildMainMenuText` that loads context and assembles status text
- [keyboards.go](file:///home/thathunky/bots/wrnrs/internal/telegram/keyboards.go) — `MainMenuKeyboard` signature change to accept pair status, update "Pair" button label
- I18n catalogs — add new keys
- [app_test.go](file:///home/thathunky/bots/wrnrs/internal/app/app_test.go) — update tests for dynamic menu, update `MainMenuKeyboard` calls

### Edge cases

- User without completed onboarding should **not** get the dynamic menu — they should see the onboarding flow.
- When partner deletes their account (Bug 15), the pair is cascade-deleted. The remaining user's next menu load should show "unpaired" state without errors.
- If card count query fails, fall back to showing just the level without progress.

---

## Bug 17 — SQLite foreign keys are never enabled — cascading deletes are broken

**Root cause:**
[OpenSQLite](file:///home/thathunky/bots/wrnrs/internal/storage/sqlite.go#L73-L89) sets `PRAGMA busy_timeout` and `PRAGMA journal_mode = WAL`, but **never** sets `PRAGMA foreign_keys = ON`. In SQLite, foreign key enforcement is off by default per connection.

This means every `ON DELETE CASCADE` in the schema is inert. Deleting a user row will leave orphaned rows in `pairs`, `pair_requests`, `game_sessions`, `game_answers`, `entitlements`, `theme_assets`, etc.

**Impact:** Critical infrastructure bug. Account deletion (Bug 15) will silently fail to cascade. The CHECK constraint `user_a_id < user_b_id` on `pairs` is also not enforced at the FK level.

**Proposed fix:**
Add `PRAGMA foreign_keys = ON` immediately after opening the connection in `OpenSQLite`, before running migrations.

---

## Bug 18 — Successful payments do not store purchase receipts

**Root cause:**
[handleSuccessfulPayment](file:///home/thathunky/bots/wrnrs/internal/app/app.go#L639-L658) grants the entitlement but never inserts into the `purchase_receipts` table. The `telegram_payment_charge_id` and `stars_amount` from the `SuccessfulPayment` message are completely ignored.

The schema has `purchase_receipts` with `telegram_payment_charge_id TEXT NOT NULL UNIQUE`, and `entitlements` has a `source_receipt_id` FK back to it — both are unused.

**Impact:**
- No audit trail for payments. Refund disputes cannot be resolved.
- The `source_receipt_id` on entitlements is always NULL even for purchases.
- Duplicate payment processing is possible since there's no idempotency check on `telegram_payment_charge_id`.

**Proposed fix:**
1. Add a `StorePurchaseReceipt(ctx, receipt)` method to the repository.
2. In `handleSuccessfulPayment`, insert into `purchase_receipts` first, then grant the entitlement with `source_receipt_id` set.
3. Use the UNIQUE constraint on `telegram_payment_charge_id` as an idempotency guard.

---

## Bug 19 — Payment invoice payload is user-spoofable

**Root cause:**
[InvoicePayload](file:///home/thathunky/bots/wrnrs/internal/payments/service.go#L44-L45) embeds `user={userID}` in the payload. The [handleSuccessfulPayment](file:///home/thathunky/bots/wrnrs/internal/app/app.go#L640) handler trusts the `user` field from the payload to determine who gets the entitlement:

```go
skuID, userID, err := payments.ParseInvoicePayload(msg.SuccessfulPayment.InvoicePayload)
```

But `msg.From.ID` (the actual payer) is never compared to the parsed `userID`. If a user somehow triggers payment with a tampered payload, the entitlement could be granted to the wrong user.

**Impact:** Low risk (Telegram controls the payload round-trip), but violates defense-in-depth. The handler should verify `parsedUserID == msg.From.ID`.

**Proposed fix:**
Add a check: `if userID != msg.From.ID { return error }` or use `msg.From.ID` directly instead of parsing from the payload.

---

## Bug 20 — Onboarding skips planned steps: own contact, background, pairing

**Root cause:**
[PLAN.md](file:///home/thathunky/bots/wrnrs/docs/PLAN.md#L128-L137) defines 10 onboarding steps including:
- Step 5: Optional own contact
- Step 9: Optional built-in or uploaded background
- Step 10: Pairing

The [onboarding package](file:///home/thathunky/bots/wrnrs/internal/onboarding/service.go#L7-L17) defines `StepOwnContact`, `StepBackground`, and `StepPairing` as constants, but **none** of them are referenced anywhere in [app.go](file:///home/thathunky/bots/wrnrs/internal/app/app.go). The actual flow goes: Language → Name → Gender → Adult → (Mature) → ThemeColor → Complete.

**Impact:** Three planned onboarding steps are defined but never wired. The own-contact step would populate `phone_lookup_hash` for phone-based pairing. The background step would let users pick or upload a card background. The pairing step would immediately invite a partner.

**Proposed fix:**
Either:
1. Wire the missing steps into the onboarding flow.
2. Or remove the dead constants and mark them as post-MVP in PLAN.md.

---

## Bug 21 — Donation interstitial before reveal is not implemented

**Root cause:**
[PLAN.md](file:///home/thathunky/bots/wrnrs/docs/PLAN.md#L175-L180) specifies: *"If donation interstitial is due, it appears before reveal, waits 3 seconds, then reveal proceeds."* The [game/support.go](file:///home/thathunky/bots/wrnrs/internal/game/support.go) has the `ShouldShowSupportPrompt` logic and the config has `SupportPromptDelay` / `SupportPromptInterval`, but this is never called from `app.go`.

The `MarkSupportPrompted` and `LastSupportPromptAt` repository methods exist in [sqlite.go](file:///home/thathunky/bots/wrnrs/internal/storage/sqlite.go#L499-L535) but are never invoked.

**Impact:** The Monobank donation interstitial — a key monetization feature — is fully scaffolded in the data and game layers but not wired into the app handler. Non-premium pairs never see support prompts.

**Proposed fix:**
Wire `ShouldShowSupportPrompt` into the reveal flow once the synchronized pair engine is connected.

---

## Bug 22 — No Redis pair lock exists — concurrent callbacks can race

**Root cause:**
[PLAN.md](file:///home/thathunky/bots/wrnrs/docs/PLAN.md#L48) specifies: *"Active pair gameplay is updated under `lock:pair:{pair_id}` in Redis."* No lock implementation exists anywhere in the codebase. There is no `lock:pair:*` key, no distributed lock helper, and no mutex in the state package.

**Impact:** When both partners tap game callbacks simultaneously, there's no protection against race conditions on pair state updates. Two concurrent `game:answer` or `game:skip` callbacks for the same session could produce inconsistent state.

**Proposed fix:**
Add a simple Redis `SET NX` lock with short TTL (e.g., 5s) in the state package, acquired before processing any pair-scoped game callback.

---

## Bug 23 — No rate limiting — abuse protection is missing

**Root cause:**
[PLAN.md](file:///home/thathunky/bots/wrnrs/docs/PLAN.md#L78) specifies `rate:user:{telegram_id}` Redis keys for abuse limits. No rate limiting exists anywhere. A user can spam `/start`, game callbacks, or pairing requests without any throttling.

**Impact:** Bot is vulnerable to abuse. A single user can flood the bot with requests, causing excessive SQLite writes, Redis operations, and Telegram API calls.

**Proposed fix:**
Add a simple sliding-window rate limiter in the state package using Redis `INCR` + `EXPIRE`. Check at the top of `HandleUpdate` before processing.

---

## Bug 24 — `en.json` has malformed JSON indentation (cosmetic)

**Root cause:**
[en.json](file:///home/thathunky/bots/wrnrs/content/i18n/en.json) has inconsistent indentation — `"brand"` and `"strings"` are indented with 4 spaces instead of 2, unlike the properly formatted `uk.json`.

```json
{
  "language": "en",
    "brand": "WRNRS",        ← extra indent
    "strings": {             ← extra indent
```

**Impact:** Cosmetic, but confusing and suggests copy-paste error. Won't cause runtime issues since JSON parsers ignore whitespace.

**Proposed fix:**
Fix indentation to match `uk.json`.

---

## Bug 25 — No `parse_mode` set on any Telegram message — markdown in text won't render

**Root cause:**
The [Telegram client](file:///home/thathunky/bots/wrnrs/internal/telegram/client.go#L30-L36) `SendMessage` never sets `parse_mode` in the payload. The support text in [supportText](file:///home/thathunky/bots/wrnrs/internal/app/app.go#L673-L678) uses backtick-quoted card numbers (`` `...` ``) expecting Markdown rendering, but without `parse_mode: "MarkdownV2"` or `"HTML"`, Telegram will display raw backticks.

**Impact:** The support/donation message with the copyable card number renders incorrectly — users see literal backtick characters.

**Proposed fix:**
Either:
1. Add an optional `parseMode` parameter to `SendMessage` and set it where markdown formatting is used.
2. Or remove markdown syntax from message text and use plain text everywhere.

---

## Bug 26 — Background upload flow is not implemented at all

**Root cause:**
[PLAN.md](file:///home/thathunky/bots/wrnrs/docs/PLAN.md#L17-L19) specifies: users can upload backgrounds, converted to WebP and stored in MinIO, with 3 free active uploads. The infrastructure exists:
- `render/processor.go` — `ProcessUploadedBackground` is fully implemented
- `objectstore/minio.go` — `Put` and `Delete` work
- Schema — `theme_assets` and `pair_theme_shares` tables exist

But the app layer has **no photo message handler**, no upload FSM step, no `theme_assets` repository methods, and no "Upload background" menu button.

**Impact:** A fully-planned and partially-built feature is completely disconnected from user interaction. Users cannot upload backgrounds despite the infrastructure being ready.

**Proposed fix:**
Wire the upload flow: add a `StepBackground` FSM handler in app.go that accepts photo messages, processes via `ProcessUploadedBackground`, stores in MinIO via `objectstore.Put`, inserts into `theme_assets`, and lets users select the background.

---

## Bug 27 — Docker Compose exposes MinIO ports to all interfaces

**Root cause:**
[docker-compose.yml](file:///home/thathunky/bots/wrnrs/docker-compose.yml#L29-L31) binds MinIO ports `9000:9000` and `9001:9001` to `0.0.0.0` (all interfaces), while the bot correctly binds to `127.0.0.1:18087:8080`. MinIO's API and console are exposed to the public internet with default credentials `wrnrs/change-me-in-production`.

**Impact:** Security vulnerability. Anyone who can reach the server on ports 9000/9001 gets full MinIO access, including reading/deleting all user-uploaded backgrounds.

**Proposed fix:**
Bind MinIO ports to localhost like the bot:
```yaml
ports:
  - "127.0.0.1:9000:9000"
  - "127.0.0.1:9001:9001"
```

---

## Summary: Priority Order

| Priority | # | Issue | Effort |
|----------|---|-------|--------|
| P0 🔴 | 1 | Language change re-triggers onboarding | Small |
| P0 🔴 | 3 | FSM not cleared on command interrupts | Small |
| P0 🔴 | 17 | Foreign keys never enabled — cascades broken | Small |
| P0 🔴 | 18 | Payments don't store purchase receipts | Small |
| P0 🔴 | 27 | MinIO ports exposed to public internet | Small |
| P1 🟠 | 2 | Theme change re-marks onboarding complete | Small |
| P1 🟠 | 4 | Pair menu dead-end (no back button) | Small |
| P1 🟠 | 10 | FSM TTL expiry → limbo state | Medium |
| P1 🟠 | 11 | Custom color entry dead-end | Small |
| P1 🟠 | 12-14 | Missing i18n keys (test bundle) | Small |
| P1 🟠 | 15 | Account data deletion not implemented | Medium |
| P1 🟠 | 16 | Main menu has no status / partner info | Medium |
| P1 🟠 | 19 | Payment payload user ID not verified | Small |
| P1 🟠 | 22 | No Redis pair lock — race conditions | Medium |
| P1 🟠 | 25 | No parse_mode — markdown renders as raw text | Small |
| P2 🟡 | 5 | Confusing pairing error on arbitrary text | Small |
| P2 🟡 | 6 | Contact outside pairing mode | Small |
| P2 🟡 | 7 | Game always shows same first card | Large |
| P2 🟡 | 8 | Game ignores user's theme color | Small |
| P2 🟡 | 9 | Raw Telegram IDs shown in pair view | Small |
| P2 🟡 | 20 | Onboarding skips 3 planned steps | Medium |
| P2 🟡 | 21 | Donation interstitial not wired | Medium |
| P2 🟡 | 23 | No rate limiting | Medium |
| P2 🟡 | 24 | en.json malformed indentation | Small |
| P2 🟡 | 26 | Background upload flow not implemented | Large |
