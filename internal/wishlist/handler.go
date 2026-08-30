package wishlist

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"wrnrs/internal/i18n"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

// Bot is the narrow slice of telegram.Client this module needs to render its
// screens. Declaring it here — instead of depending on *telegram.Client, or
// worse, on internal/app — keeps this package testable and keeps
// internal/app free to wire in the real client in a later task. Unlike
// internal/positions' Bot, this module never sends a photo: every wishlist
// screen is text.
type Bot interface {
	SendMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error
	EditMessageText(ctx context.Context, chatID, messageID int64, text string, replyMarkup any) error
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
}

// Repository is the narrow slice of storage.Repository this module needs:
// resolving the caller's active pair, reading/writing wish answers and
// computing matches. Declared here (rather than depending on
// *storage.Repository directly) purely to match the pattern internal/app
// uses for its own Bot, FSMStore and ObjectStore interfaces —
// *storage.Repository already satisfies it without any adapter.
type Repository interface {
	ActivePairForUser(ctx context.Context, userID int64) (*storage.Pair, error)
	UserLanguage(ctx context.Context, telegramID int64) (string, error)
	SetWishAnswer(ctx context.Context, userID int64, kind storage.WishItemKind, itemID string, answer storage.WishAnswer, now time.Time) error
	UserWishAnswers(ctx context.Context, userID int64) (map[string]storage.WishAnswer, error)
	PairWishMatches(ctx context.Context, pairID int64) ([]storage.WishMatch, error)
	PartnerHasAnyWishAnswer(ctx context.Context, pairID, userID int64) (bool, error)
}

// HandlerOptions configures a Handler. Service, Repository, Bot and I18n are
// required; Logger, Now and PositionTitle are optional. Logger and Now
// default to slog.Default and time.Now respectively.
type HandlerOptions struct {
	Service    *Service
	Repository Repository
	Bot        Bot
	I18n       *i18n.Bundle
	Logger     *slog.Logger
	// Now is injected for tests; defaults to time.Now.
	Now func() time.Time
	// PositionTitle resolves the display title for an item id from the
	// positions catalog — a plain func value, deliberately not
	// *positions.Service or *catalog.Catalog, because this package must
	// never import internal/positions (see the package-level boundary this
	// module and internal/positions both document on their Repository
	// interfaces). cmd/wrnrs/main.go injects it once both catalogs are
	// loaded. Left nil (the zero value — e.g. the positions catalog failed
	// to load, or a test constructs HandlerOptions directly), itemLabel
	// falls back to the "wish.item.position" i18n key instead of a bare
	// item id.
	PositionTitle func(id string) (string, bool)
}

// Handler implements modules.Handler for the wishlist module: the hub, the
// swipe queue, per-answer writes, matches and a self-only review of one's
// own answers.
type Handler struct {
	service       *Service
	repo          Repository
	bot           Bot
	i18n          *i18n.Bundle
	logger        *slog.Logger
	now           func() time.Time
	positionTitle func(id string) (string, bool)
}

// NewHandler builds a Handler. It panics only on a genuinely unusable
// configuration (nil Service, Repository, Bot or I18n) — those are
// programmer errors in the wiring code, not runtime conditions to degrade
// from. Because every field is required here, no exported way exists to
// construct a Handler carrying a nil dependency, so none of the screens
// below need to re-guard against one.
func NewHandler(options HandlerOptions) *Handler {
	if options.Service == nil || options.Repository == nil || options.Bot == nil || options.I18n == nil {
		panic("wishlist.NewHandler: Service, Repository, Bot and I18n are required")
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Handler{
		service:       options.Service,
		repo:          options.Repository,
		bot:           options.Bot,
		i18n:          options.I18n,
		logger:        logger,
		now:           now,
		positionTitle: options.PositionTitle,
	}
}

// HandleMessage never consumes free text: the module is driven entirely by
// its inline keyboards (spec Р5).
func (h *Handler) HandleMessage(ctx context.Context, msg *telegram.Message) (bool, error) {
	return false, nil
}

// HandleCallback parses the wish: callback and renders the matching screen.
// It never panics on malformed or stale callback data — an unrecognised
// kind or answer just re-renders the current screen instead of writing
// anything.
//
// The caller (internal/app's dispatchModuleCallback) already answers the
// callback query before invoking a module handler, so nothing here calls
// AnswerCallbackQuery — doing so again would be a second, redundant answer
// for the same callback id, which Telegram rejects. The 18+/mature gate is
// likewise already enforced by the framework before this method runs; the
// one requirement the gate does not cover — the matches screen needing an
// active pair — is handled explicitly in showMatches below.
func (h *Handler) HandleCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	if cb == nil {
		return nil
	}
	if cb.Message != nil && isGroupChat(cb.Message.Chat.Type) {
		// Wishlist content is inherently a direct-message feature: the
		// maturity gate resolves the TAPPING user, not the chat, so a shared
		// chat with one 18+ opted-in member would otherwise let that member
		// expose intimate wish content to everyone in it. Refuse outright,
		// for every screen, exactly like internal/positions does.
		return nil
	}
	userID := cb.From.ID
	chatID := userID
	if cb.Message != nil {
		chatID = cb.Message.Chat.ID
	}
	language := h.language(ctx, userID, cb.From.LanguageCode)
	data := cb.Data

	switch {
	case data == "wish:open":
		return h.showHub(ctx, cb, chatID, userID, language)
	case data == "wish:next":
		return h.showNext(ctx, cb, chatID, userID, language)
	case strings.HasPrefix(data, "wish:answer:"):
		return h.handleAnswer(ctx, cb, chatID, userID, language, strings.TrimPrefix(data, "wish:answer:"))
	case data == "wish:matches":
		return h.showMatches(ctx, cb, chatID, userID, language)
	case data == "wish:mine":
		return h.showMine(ctx, cb, chatID, userID, language)
	default:
		return nil
	}
}

// isGroupChat reports whether chatType names a shared chat — every type
// Telegram uses for something that is not a one-to-one chat with the bot.
func isGroupChat(chatType string) bool {
	switch chatType {
	case "group", "supergroup", "channel":
		return true
	default:
		return false
	}
}

// language resolves the caller's saved language, falling back to their
// Telegram client language when no user record is available.
func (h *Handler) language(ctx context.Context, userID int64, clientLanguage string) string {
	fallback := "uk"
	if strings.HasPrefix(strings.ToLower(clientLanguage), "en") {
		fallback = "en"
	}
	language, err := h.repo.UserLanguage(ctx, userID)
	if err != nil || language == "" {
		return fallback
	}
	if strings.HasPrefix(strings.ToLower(language), "en") {
		return "en"
	}
	return "uk"
}

// --- screens -----------------------------------------------------------------

// showHub renders the module entry screen: title, intro, progress, an
// optional "partner is marking too" line, and HubKeyboard. Solo (no active
// pair) costs two reads (pair, own answers); paired costs two more (match
// count, partner activity) so the hub can show a truthful matches count and
// partner_active line without ever exposing an individual answer.
func (h *Handler) showHub(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string) error {
	pair, err := h.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		h.logger.Error("wishlist: load active pair", "error", err)
		return err
	}
	answers, err := h.repo.UserWishAnswers(ctx, userID)
	if err != nil {
		h.logger.Error("wishlist: load user answers", "error", err)
		return err
	}
	answered, total := h.service.Progress(answers)

	matches := 0
	partnerActive := false
	if pair != nil {
		pairMatches, err := h.repo.PairWishMatches(ctx, pair.ID)
		if err != nil {
			h.logger.Error("wishlist: load pair matches", "error", err)
			return err
		}
		matches = len(pairMatches)

		partnerActive, err = h.repo.PartnerHasAnyWishAnswer(ctx, pair.ID, userID)
		if err != nil {
			h.logger.Error("wishlist: load partner activity", "error", err)
			return err
		}
	}

	lines := []string{
		h.i18n.Text(language, "wish.hub.title"),
		h.i18n.Text(language, "wish.hub.intro"),
		sprintfSafe(h.i18n.Text(language, "wish.hub.progress"), answered, total),
	}
	if partnerActive {
		lines = append(lines, h.i18n.Text(language, "wish.hub.partner_active"))
	}
	text := strings.Join(lines, "\n\n")
	return h.presentText(ctx, cb, chatID, text, HubKeyboard(h.i18n, language, pair != nil, matches))
}

// showNext renders the next unanswered wish, or wish.done once the queue is
// exhausted. One read (the caller's own answers) is enough: the queue order
// itself is static and lives entirely in h.service.
func (h *Handler) showNext(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string) error {
	answers, err := h.repo.UserWishAnswers(ctx, userID)
	if err != nil {
		h.logger.Error("wishlist: load user answers", "error", err)
		return err
	}
	item, ok := h.service.NextUnanswered(answers)
	if !ok {
		return h.presentText(ctx, cb, chatID, h.i18n.Text(language, "wish.done"), BackKeyboard(h.i18n, language))
	}
	answered, total := h.service.Progress(answers)
	caption := SwipeCaption(h.i18n, language, item, answered, total)
	return h.presentText(ctx, cb, chatID, caption, SwipeKeyboard(h.i18n, language, item.ID))
}

// handleAnswer parses "{kind}:{id}:{answer}" (the callback data with its
// "wish:answer:" prefix already trimmed by the caller), records a
// recognised answer, and always re-renders the next unanswered card
// afterward. Every failure mode — a missing separator, an unknown kind, an
// unknown answer, or an item id this module's catalog does not recognise —
// falls through to the same "re-render, write nothing" behaviour rather
// than panicking or guessing.
//
// One successful answer costs exactly two repository calls: the write
// itself (SetWishAnswer) and the read that picks the next card
// (UserWishAnswers, inside showNext). That second read is not redundant —
// without it showNext would not know the answer just written is already
// done — but it does mean a rapid string of taps costs one write plus one
// read each, not a single combined round trip; that trade was made for
// reusing showNext verbatim rather than threading a second answers map
// through two code paths.
func (h *Handler) handleAnswer(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language, rest string) error {
	kindRaw, tail, ok := splitOnce(rest, ":")
	if !ok {
		return h.showNext(ctx, cb, chatID, userID, language)
	}
	itemID, answerRaw, ok := splitOnce(tail, ":")
	if !ok {
		return h.showNext(ctx, cb, chatID, userID, language)
	}
	kind, ok := parseWishKind(kindRaw)
	if !ok {
		return h.showNext(ctx, cb, chatID, userID, language)
	}
	answer, ok := parseWishAnswer(answerRaw)
	if !ok {
		return h.showNext(ctx, cb, chatID, userID, language)
	}
	if _, found := h.service.Item(itemID); !found {
		// Stale or forged callback for an id that no longer (or never did)
		// exist in the wishes catalog: refresh instead of writing for
		// nothing.
		return h.showNext(ctx, cb, chatID, userID, language)
	}

	if err := h.repo.SetWishAnswer(ctx, userID, kind, itemID, answer, h.now()); err != nil {
		h.logger.Error("wishlist: write answer", "error", err)
		return err
	}
	return h.showNext(ctx, cb, chatID, userID, language)
}

// parseWishKind accepts only "wish": the only kind this module's own
// keyboard (SwipeKeyboard) ever emits. Rejecting anything else — including
// storage's other valid kind, "position" — keeps this handler from writing
// answers on behalf of a catalog it has no way to validate an item id
// against.
func parseWishKind(raw string) (storage.WishItemKind, bool) {
	if storage.WishItemKind(raw) == storage.WishKindWish {
		return storage.WishKindWish, true
	}
	return "", false
}

func parseWishAnswer(raw string) (storage.WishAnswer, bool) {
	switch storage.WishAnswer(raw) {
	case storage.AnswerWant, storage.AnswerCurious, storage.AnswerNo:
		return storage.WishAnswer(raw), true
	default:
		return "", false
	}
}

// showMatches renders the pair's mutual matches. Without an active pair it
// shows wish.matches.needs_pair — a distinct message from wish.matches.empty
// (paired, zero matches so far), since "no partner yet" and "partner hasn't
// matched you on anything yet" are different situations and must read
// differently. Storage's PairWishMatches already guarantees a match reveals
// nothing about the individual answer behind it (see
// internal/storage/wishes.go), so this screen only ever prints the mutual
// result.
func (h *Handler) showMatches(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string) error {
	pair, err := h.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		h.logger.Error("wishlist: load active pair", "error", err)
		return err
	}
	if pair == nil {
		return h.presentText(ctx, cb, chatID, h.i18n.Text(language, "wish.matches.needs_pair"), BackKeyboard(h.i18n, language))
	}

	matches, err := h.repo.PairWishMatches(ctx, pair.ID)
	if err != nil {
		h.logger.Error("wishlist: load pair matches", "error", err)
		return err
	}
	if len(matches) == 0 {
		return h.presentText(ctx, cb, chatID, h.i18n.Text(language, "wish.matches.empty"), BackKeyboard(h.i18n, language))
	}

	lines := []string{h.i18n.Text(language, "wish.matches.title")}
	for _, m := range matches {
		lines = append(lines, h.matchLine(language, m))
	}
	text := strings.Join(lines, "\n")
	return h.presentText(ctx, cb, chatID, text, BackKeyboard(h.i18n, language))
}

func (h *Handler) matchLine(language string, m storage.WishMatch) string {
	label := h.itemLabel(language, m.ItemKind, m.ItemID)
	if m.Strong {
		return "🔥 " + label
	}
	return "• " + label
}

// itemLabel resolves a display label for one item id. WishKindWish ids are
// looked up in h.service, the only catalog this module holds directly.
// WishKindPosition ids — the expected common case now that internal/positions
// puts a wish button on every one of its 519 cards — are resolved through
// h.positionTitle, the optional cross-module title lookup injected by
// cmd/wrnrs/main.go; this package must never import internal/positions
// itself. When that resolver is absent (not wired, e.g. in a test that
// builds HandlerOptions directly) or it misses (a stale/removed catalog id),
// this falls back to the i18n-labelled "wish.item.position" form rather than
// a bare number — a raw id like "042" reads as noise next to a real title
// like "перше" on the matches and mine screens.
func (h *Handler) itemLabel(language string, kind storage.WishItemKind, itemID string) string {
	switch kind {
	case storage.WishKindWish:
		if item, ok := h.service.Item(itemID); ok {
			if title, _ := itemTitleAndBody(language, item); title != "" {
				return title
			}
		}
		return itemID
	case storage.WishKindPosition:
		if h.positionTitle != nil {
			if title, ok := h.positionTitle(itemID); ok && title != "" {
				return title
			}
		}
		return sprintfSafe(h.i18n.Text(language, "wish.item.position"), itemID)
	default:
		return itemID
	}
}

// showMine renders the caller's own answers, grouped by answer value
// (want/curious/no) — never the partner's. Nothing here ever queries
// another user's answers; that capability does not exist on Repository by
// design (see storage.WishMatch's doc comment).
func (h *Handler) showMine(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string) error {
	answers, err := h.repo.UserWishAnswers(ctx, userID)
	if err != nil {
		h.logger.Error("wishlist: load user answers", "error", err)
		return err
	}
	if len(answers) == 0 {
		return h.presentText(ctx, cb, chatID, h.i18n.Text(language, "wish.mine.empty"), BackKeyboard(h.i18n, language))
	}

	groups := map[storage.WishAnswer][]string{}
	for key, answer := range answers {
		kindRaw, itemID, ok := splitOnce(key, ":")
		if !ok {
			continue
		}
		label := h.itemLabel(language, storage.WishItemKind(kindRaw), itemID)
		groups[answer] = append(groups[answer], label)
	}

	lines := []string{h.i18n.Text(language, "wish.mine.title")}
	for _, answer := range []storage.WishAnswer{storage.AnswerWant, storage.AnswerCurious, storage.AnswerNo} {
		items := groups[answer]
		if len(items) == 0 {
			continue
		}
		sort.Strings(items)
		lines = append(lines, "", h.answerHeading(language, answer))
		for _, label := range items {
			lines = append(lines, "• "+label)
		}
	}
	lines = append(lines, "", h.i18n.Text(language, "wish.privacy_note"))

	text := strings.Join(lines, "\n")
	return h.presentText(ctx, cb, chatID, text, BackKeyboard(h.i18n, language))
}

func (h *Handler) answerHeading(language string, answer storage.WishAnswer) string {
	switch answer {
	case storage.AnswerWant:
		return h.i18n.Text(language, "wish.answer.want")
	case storage.AnswerCurious:
		return h.i18n.Text(language, "wish.answer.curious")
	case storage.AnswerNo:
		return h.i18n.Text(language, "wish.answer.no")
	default:
		return string(answer)
	}
}

// --- rendering ---------------------------------------------------------------

// presentText renders a plain-text screen, editing the triggering message in
// place when possible. A message that currently carries a photo cannot
// become a text message via an edit, so that case deletes and sends fresh —
// the same fallback internal/positions' own presentText uses. None of this
// module's own screens ever attach a photo, but the triggering message
// (cb.Message) can still be one if this callback happens to be a stray tap
// on an old card from elsewhere in the app.
func (h *Handler) presentText(ctx context.Context, cb *telegram.CallbackQuery, chatID int64, text string, markup telegram.InlineKeyboardMarkup) error {
	if cb != nil && cb.Message != nil {
		if len(cb.Message.Photo) > 0 {
			_ = h.bot.DeleteMessage(ctx, chatID, cb.Message.MessageID)
			return h.bot.SendMessage(ctx, chatID, text, markup)
		}
		if err := h.bot.EditMessageText(ctx, chatID, cb.Message.MessageID, text, markup); err == nil {
			return nil
		}
	}
	return h.bot.SendMessage(ctx, chatID, text, markup)
}

// splitOnce splits s on the first occurrence of sep into (head, tail). It
// reports ok=false if sep is absent, so callers can treat a malformed
// callback as "nothing to do" instead of indexing out of range.
func splitOnce(s, sep string) (string, string, bool) {
	i := strings.Index(s, sep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+len(sep):], true
}

// sprintfSafe formats an i18n string that is expected to carry %-verbs. If
// the loaded catalog is missing the key (Bundle.Text falls back to
// returning the raw key), fmt.Sprintf would otherwise emit "%!d(MISSING)"
// noise; returning the raw template instead keeps a missing-translation bug
// visibly a missing string rather than a mangled one.
func sprintfSafe(template string, args ...any) string {
	if !strings.Contains(template, "%") {
		return template
	}
	return fmt.Sprintf(template, args...)
}
