package play

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"wrnrs/internal/i18n"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

// Bot is the narrow slice of telegram.Client this module needs to render its
// screens. Declaring it here — instead of depending on *telegram.Client, or
// worse, on internal/app — keeps this package testable and keeps
// internal/app free to wire in the real client in a later task. Like
// internal/wishlist's Bot, every play screen is text: no card ever carries a
// photo.
type Bot interface {
	SendMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error
	EditMessageText(ctx context.Context, chatID, messageID int64, text string, replyMarkup any) error
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
}

// Repository is the narrow slice of storage.Repository this module needs:
// resolving the caller's active pair, the caller's language, and the
// display name of whichever partner a drawn card addresses. Declared here
// (rather than depending on *storage.Repository directly) purely to match
// the pattern internal/positions and internal/wishlist use for their own
// Bot/Repository/StateStore interfaces — *storage.Repository already
// satisfies it without any adapter.
type Repository interface {
	ActivePairForUser(ctx context.Context, userID int64) (*storage.Pair, error)
	UserDisplayName(ctx context.Context, telegramID int64) (string, error)
	UserLanguage(ctx context.Context, telegramID int64) (string, error)
}

// StateStore is the narrow slice of the Redis-backed state this module
// needs: the per-user GameState (filter, draw counter, recent ring, whose
// turn is next). Redis can be absent at runtime, so every caller in this
// file guards a nil StateStore rather than assuming it is wired — exactly
// like internal/positions' own StateStore.
type StateStore interface {
	SetModuleState(ctx context.Context, userID int64, module, value string, ttl time.Duration) error
	ModuleState(ctx context.Context, userID int64, module string) (string, error)
}

// moduleName keys this module's entry in the shared per-user module-state
// table. stateTTL mirrors internal/positions' own browse-state TTL: long
// enough that a session picked back up the next day still remembers whose
// turn it is and which filters were set, short enough that an abandoned
// session does not linger in Redis forever.
const (
	moduleName = "play"
	stateTTL   = 24 * time.Hour
)

// allowedFilterFacets are the only facet names FiltersKeyboard ever emits a
// button for (see keyboards.go's kindValues/intensityValues). A callback
// naming anything else is stale, forged, or simply a typo, and must not be
// allowed to silently zero out the selection by adding a facet no card in
// the catalog carries.
var allowedFilterFacets = map[string]bool{"kind": true, "intensity": true}

// HandlerOptions configures a Handler. Service, Repository, Bot and I18n are
// required; State and Logger are optional. Logger defaults to slog.Default.
type HandlerOptions struct {
	Service    *Service
	Repository Repository
	Bot        Bot
	State      StateStore
	I18n       *i18n.Bundle
	Logger     *slog.Logger
}

// Handler implements modules.Handler for the play module: the hub, drawing
// (and skipping) cards in turn, and the kind/intensity filter toggles.
type Handler struct {
	service *Service
	repo    Repository
	bot     Bot
	state   StateStore
	i18n    *i18n.Bundle
	logger  *slog.Logger
}

// NewHandler builds a Handler. It panics only on a genuinely unusable
// configuration (nil Service, Repository, Bot or I18n) — those are
// programmer errors in the wiring code, not runtime conditions to degrade
// from. State may be nil. Because every other field is required here, no
// exported way exists to construct a Handler carrying one of those as nil,
// so none of the screens below need to re-guard against it.
func NewHandler(options HandlerOptions) *Handler {
	if options.Service == nil || options.Repository == nil || options.Bot == nil || options.I18n == nil {
		panic("play.NewHandler: Service, Repository, Bot and I18n are required")
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		service: options.Service,
		repo:    options.Repository,
		bot:     options.Bot,
		state:   options.State,
		i18n:    options.I18n,
		logger:  logger,
	}
}

// HandleMessage never consumes free text: the module is driven entirely by
// its inline keyboards.
func (h *Handler) HandleMessage(ctx context.Context, msg *telegram.Message) (bool, error) {
	return false, nil
}

// HandleCallback parses the play: callback and renders the matching screen.
// It never panics on malformed or stale callback data — an unrecognised
// filter facet just re-renders the filters screen instead of toggling
// anything.
//
// The caller (internal/app's dispatchModuleCallback) already answers the
// callback query before invoking a module handler, so nothing here calls
// AnswerCallbackQuery — doing so again would be a second, redundant answer
// for the same callback id, which Telegram rejects. The 18+/mature gate is
// likewise already enforced by the framework before this method runs.
func (h *Handler) HandleCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	if cb == nil {
		return nil
	}
	if cb.Message != nil && isGroupChat(cb.Message.Chat.Type) {
		// Truth-or-dare content is inherently a direct-message feature: the
		// maturity gate resolves the TAPPING user, not the chat, so a shared
		// chat with one 18+ opted-in member would otherwise let that member
		// deal explicit cards to everyone in it. Refuse outright, for every
		// screen, exactly like internal/positions and internal/wishlist do.
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
	case data == "play:open":
		return h.showHub(ctx, cb, chatID, userID, language)
	case data == "play:next":
		// The one behavioural distinction the whole module exists to make:
		// taking a card flips whose turn is next.
		return h.showCard(ctx, cb, chatID, userID, language, true)
	case data == "play:skip":
		// Skipping redraws without flipping the turn — it must never become
		// a way to push the current card onto your partner instead of
		// drawing again yourself.
		return h.showCard(ctx, cb, chatID, userID, language, false)
	case data == "play:filters":
		return h.showFilters(ctx, cb, chatID, userID, language)
	case strings.HasPrefix(data, "play:filter:"):
		return h.handleFilterToggle(ctx, cb, chatID, userID, language, strings.TrimPrefix(data, "play:filter:"))
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

// --- state -------------------------------------------------------------------

// loadState reads the caller's GameState from Redis. A nil StateStore, a
// missing entry, or a corrupt one all degrade the same way: a fresh
// GameState{}, which is a perfectly valid starting point (filter open, no
// cards drawn yet, A's turn first).
func (h *Handler) loadState(ctx context.Context, userID int64) GameState {
	if h.state == nil {
		return GameState{}
	}
	raw, err := h.state.ModuleState(ctx, userID, moduleName)
	if err != nil || raw == "" {
		return GameState{}
	}
	state, err := DecodeState(raw)
	if err != nil {
		return GameState{}
	}
	return state
}

// saveState persists state, best-effort. A nil StateStore or an encode
// failure is silently a no-op: without Redis the session simply cannot
// remember whose turn it is or which cards were recently drawn, but every
// screen still renders and the game stays playable.
func (h *Handler) saveState(ctx context.Context, userID int64, state GameState) {
	if h.state == nil {
		return
	}
	encoded, err := EncodeState(state)
	if err != nil {
		return
	}
	_ = h.state.SetModuleState(ctx, userID, moduleName, encoded, stateTTL)
}

// --- screens -----------------------------------------------------------------

// showHub renders the module entry screen: title, intro, and — only for a
// solo player — the hint that cards will start naming a partner once one
// exists. One repository read (the active pair) is enough to decide that.
func (h *Handler) showHub(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string) error {
	pair, err := h.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		h.logger.Error("play: load active pair", "error", err)
		return err
	}
	lines := []string{
		h.i18n.Text(language, "play.hub.title"),
		h.i18n.Text(language, "play.hub.intro"),
	}
	if pair == nil {
		lines = append(lines, h.i18n.Text(language, "play.hub.solo_hint"))
	}
	text := strings.Join(lines, "\n\n")
	return h.presentText(ctx, cb, chatID, text, HubKeyboard(h.i18n, language))
}

// showCard draws one card and renders it. flipTurn is the whole point of
// having two near-identical callers (play:next and play:skip): true flips
// whose turn is next before saving, false leaves it exactly as loaded, so a
// skipped card stays the same player's problem instead of becoming their
// partner's.
//
// One repository call (ActivePairForUser) is unconditional; a second
// (UserDisplayName) only runs when a pair exists, since a solo draw has no
// actor to name at all. Neither Service.Next nor the state read/write touch
// SQLite — GameState lives in Redis, never in storage.Repository — so a
// paired play:next costs exactly two database queries, a solo one exactly
// one.
func (h *Handler) showCard(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string, flipTurn bool) error {
	pair, err := h.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		h.logger.Error("play: load active pair", "error", err)
		return err
	}
	state := h.loadState(ctx, userID)
	turnB := state.TurnB

	seed := userID
	if pair != nil {
		// The pair id, not either partner's telegram id, so both phones —
		// were this ever used from two devices — would land on the same
		// deterministic shuffle. Mirrors internal/positions' own seedFor.
		seed = pair.ID
	}

	item, next, err := h.service.Next(seed, state)
	if err != nil {
		// An empty selection (every card filtered out) must not go silent:
		// surface it as play.empty, with the filters screen right there to
		// fix it, rather than rendering a blank or stale card.
		return h.presentText(ctx, cb, chatID, h.i18n.Text(language, "play.empty"), FiltersKeyboard(h.i18n, language, state.Filter))
	}
	if flipTurn {
		next.TurnB = !turnB
	}
	h.saveState(ctx, userID, next)

	actor := h.resolveActor(ctx, pair, turnB, language)
	caption := CardCaption(h.i18n, language, item, actor)
	return h.presentText(ctx, cb, chatID, caption, CardKeyboard(h.i18n, language))
}

// resolveActor names whichever partner the card in hand addresses: A when
// turnB is false, B when true. Without a pair there is nobody to name — it
// returns "" without even querying UserDisplayName, and CardCaption already
// knows to omit the actor prefix entirely rather than leave an orphaned
// separator. An empty display name (never set) or a lookup error both fall
// back to the localized "menu.partner_fallback" rather than surfacing a
// blank or an error for what is purely cosmetic.
func (h *Handler) resolveActor(ctx context.Context, pair *storage.Pair, turnB bool, language string) string {
	if pair == nil {
		return ""
	}
	id := pair.UserAID
	if turnB {
		id = pair.UserBID
	}
	name, err := h.repo.UserDisplayName(ctx, id)
	if err != nil || strings.TrimSpace(name) == "" {
		return h.i18n.Text(language, "menu.partner_fallback")
	}
	return name
}

// showFilters renders the kind/intensity toggle screen. It costs no
// database or Redis read beyond the state load (the two facet vocabularies
// are fixed and compiled into keyboards.go, unlike internal/positions'
// catalog-discovered ones).
func (h *Handler) showFilters(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string) error {
	state := h.loadState(ctx, userID)
	text := h.i18n.Text(language, "play.filters.title")
	return h.presentText(ctx, cb, chatID, text, FiltersKeyboard(h.i18n, language, state.Filter))
}

// handleFilterToggle parses "{facet}:{value}" (the callback data with its
// "play:filter:" prefix already trimmed by the caller) and flips that value
// in the stored filter. Every failure mode — a missing separator or a facet
// outside allowedFilterFacets — falls through to re-rendering the filters
// screen unchanged rather than panicking or writing anything.
func (h *Handler) handleFilterToggle(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language, rest string) error {
	facet, value, ok := splitOnce(rest, ":")
	if !ok || !allowedFilterFacets[facet] || value == "" {
		return h.showFilters(ctx, cb, chatID, userID, language)
	}
	state := h.loadState(ctx, userID)
	state.Filter = ToggleFilterValue(state.Filter, facet, value)
	h.saveState(ctx, userID, state)
	return h.showFilters(ctx, cb, chatID, userID, language)
}

// --- rendering ---------------------------------------------------------------

// presentText renders a plain-text screen, editing the triggering message in
// place when possible. A message that currently carries a photo cannot
// become a text message via an edit, so that case deletes and sends fresh —
// the same fallback internal/positions' and internal/wishlist's own
// presentText use. No screen in this module ever attaches a photo itself,
// but the triggering message (cb.Message) can still be one if this callback
// happens to be a stray tap on an old card from elsewhere in the app.
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
