package positions

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

// Bot is the narrow slice of telegram.Client this module needs to render its
// screens. Declaring it here — instead of depending on *telegram.Client, or
// worse, on internal/app — keeps this package testable and keeps
// internal/app free to wire in the real client in a later task.
type Bot interface {
	SendMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error
	EditMessageText(ctx context.Context, chatID, messageID int64, text string, replyMarkup any) error
	SendPhotoBytes(ctx context.Context, chatID int64, data []byte, caption string, replyMarkup any) (telegram.SentPhoto, error)
	SendPhotoRef(ctx context.Context, chatID int64, fileID, caption string, replyMarkup any) (telegram.SentPhoto, error)
	EditMessageMediaRef(ctx context.Context, chatID, messageID int64, fileID, caption string, replyMarkup any) error
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
}

// StateStore is the narrow slice of the Redis-backed state this module
// needs: the per-user browse cursor (module state) and the file-id cache
// that lets an image be uploaded to Telegram once and reused forever after.
// Redis can be absent at runtime, so every caller in this file guards a nil
// StateStore rather than assuming it is wired.
type StateStore interface {
	SetModuleState(ctx context.Context, userID int64, module, value string, ttl time.Duration) error
	ModuleState(ctx context.Context, userID int64, module string) (string, error)
	CacheFileID(ctx context.Context, renderHash, fileID string, ttl time.Duration) error
	FileID(ctx context.Context, renderHash string) (string, error)
}

// ObjectStore is the narrow slice of MinIO this module needs: reading back
// position images by key. MinIO can be absent at runtime; a nil ObjectStore
// degrades every screen to text-only instead of failing.
type ObjectStore interface {
	Get(ctx context.Context, objectKey string) ([]byte, error)
}

// Repository is the narrow slice of storage.Repository this module needs:
// resolving the caller's active pair and reading/writing the pair-shared
// marks. Declared here (rather than depending on *storage.Repository
// directly) purely to match the pattern internal/app uses for its own Bot,
// FSMStore and ObjectStore interfaces — *storage.Repository already
// satisfies it without any adapter.
type Repository interface {
	ActivePairForUser(ctx context.Context, userID int64) (*storage.Pair, error)
	PairPositionMarks(ctx context.Context, pairID int64) (map[string]storage.PositionMark, error)
	TogglePositionMark(ctx context.Context, pairID int64, positionID string, kind storage.PositionMarkKind, markedBy int64, now time.Time) (bool, error)
	UserLanguage(ctx context.Context, telegramID int64) (string, error)
}

const (
	moduleName        = "positions"
	browseStateTTL    = 24 * time.Hour
	fileIDCacheTTL    = 180 * 24 * time.Hour
	defaultDumpPeriod = 3 * time.Second
)

// curatedFilterFacets are the facets exposed on the filters screen. The
// catalog carries far more (act, activity, extra, penetration, stimulation,
// type, ...); showing all of them would turn one screen into an
// unnavigable wall of buttons, so only the two most orientation-relevant
// ones are offered here — the same two BrowseCaption surfaces on the card.
var curatedFilterFacets = []string{"level", "location"}

// HandlerOptions configures a Handler. Every field except Service, Catalog,
// Repository, Bot and I18n is optional and must be nil-safe: Redis and
// MinIO can both be absent at runtime.
type HandlerOptions struct {
	Service     *Service
	Catalog     *catalog.Catalog
	Repository  Repository
	Bot         Bot
	State       StateStore
	ObjectStore ObjectStore
	I18n        *i18n.Bundle
	// DumpInterval throttles the bulk send. Defaults to defaultDumpPeriod
	// when zero.
	DumpInterval time.Duration
	// Now is injected for tests; defaults to time.Now.
	Now func() time.Time
}

// Handler implements modules.Handler for the positions catalog: the hub,
// the photo browser, the randomiser, shared marks, filters and the
// throttled bulk send.
type Handler struct {
	service      *Service
	catalog      *catalog.Catalog
	repo         Repository
	bot          Bot
	state        StateStore
	objectStore  ObjectStore
	i18n         *i18n.Bundle
	dumpInterval time.Duration
	now          func() time.Time

	filterFacets []FacetOption

	mu   sync.Mutex
	runs map[int64]context.CancelFunc
}

// NewHandler builds a Handler. It panics only on a genuinely unusable
// configuration (nil Service, Catalog, Repository, Bot or I18n) — those are
// programmer errors in the wiring code, not runtime conditions to degrade
// from. State and ObjectStore may be nil.
func NewHandler(options HandlerOptions) *Handler {
	if options.Service == nil || options.Catalog == nil || options.Repository == nil || options.Bot == nil || options.I18n == nil {
		panic("positions.NewHandler: Service, Catalog, Repository, Bot and I18n are required")
	}
	interval := options.DumpInterval
	if interval <= 0 {
		interval = defaultDumpPeriod
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Handler{
		service:      options.Service,
		catalog:      options.Catalog,
		repo:         options.Repository,
		bot:          options.Bot,
		state:        options.State,
		objectStore:  options.ObjectStore,
		i18n:         options.I18n,
		dumpInterval: interval,
		now:          now,
		filterFacets: collectFacetOptions(options.Catalog, curatedFilterFacets),
		runs:         map[int64]context.CancelFunc{},
	}
}

// collectFacetOptions scans the catalog once to derive the vocabulary for
// each curated facet, rather than hardcoding position-content-specific
// values (level=easy/medium/hard/crazy, location=bed/sofa/...) into this
// module. That keeps the filters screen correct even if the catalog's
// vocabulary changes.
func collectFacetOptions(cat *catalog.Catalog, facets []string) []FacetOption {
	if cat == nil {
		return nil
	}
	sets := make(map[string]map[string]bool, len(facets))
	for _, facet := range facets {
		sets[facet] = map[string]bool{}
	}
	for _, item := range cat.Items {
		for _, facet := range facets {
			for _, value := range item.Facets[facet] {
				sets[facet][value] = true
			}
		}
	}
	options := make([]FacetOption, 0, len(facets))
	for _, facet := range facets {
		values := make([]string, 0, len(sets[facet]))
		for value := range sets[facet] {
			values = append(values, value)
		}
		sort.Strings(values)
		if len(values) == 0 {
			continue
		}
		options = append(options, FacetOption{Facet: facet, Values: values})
	}
	return options
}

// HandleMessage never consumes free text: the module is driven entirely by
// its inline keyboards.
func (h *Handler) HandleMessage(ctx context.Context, msg *telegram.Message) (bool, error) {
	return false, nil
}

// HandleCallback parses the pos: callback and renders the matching screen.
// It never panics on malformed or stale callback data — an index out of
// range wraps (Service.At), an unknown item id is treated as a no-op
// refresh of the current screen, and an unrecognised callback is ignored.
//
// The caller (internal/app's dispatchModuleCallback) already answers the
// callback query before invoking a module handler, so nothing here calls
// AnswerCallbackQuery — doing so again would be a second, redundant answer
// for the same callback id, which Telegram rejects.
func (h *Handler) HandleCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	if cb == nil {
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
	case data == "pos:open":
		return h.showHub(ctx, cb, chatID, language)
	case strings.HasPrefix(data, "pos:browse:"):
		index, _ := strconv.Atoi(strings.TrimPrefix(data, "pos:browse:"))
		return h.showBrowse(ctx, cb, chatID, userID, language, index)
	case data == "pos:random":
		return h.showRandom(ctx, cb, chatID, userID, language)
	case strings.HasPrefix(data, "pos:mark:"):
		return h.handleMark(ctx, cb, chatID, userID, language, strings.TrimPrefix(data, "pos:mark:"))
	case data == "pos:filters":
		return h.showFilters(ctx, cb, chatID, userID, language)
	case strings.HasPrefix(data, "pos:filter:"):
		return h.handleFilterToggle(ctx, cb, chatID, userID, language, strings.TrimPrefix(data, "pos:filter:"))
	case data == "pos:dump:confirm":
		return h.showDumpConfirm(ctx, cb, chatID, userID, language)
	case data == "pos:dump:go":
		return h.startDump(ctx, cb, chatID, userID, language)
	case data == "pos:dump:stop":
		return h.stopDump(ctx, cb, chatID, userID, language)
	default:
		return nil
	}
}

// language resolves the caller's saved language, falling back to their
// Telegram client language when no user record is available.
func (h *Handler) language(ctx context.Context, userID int64, clientLanguage string) string {
	fallback := "uk"
	if strings.HasPrefix(strings.ToLower(clientLanguage), "en") {
		fallback = "en"
	}
	if h.repo == nil {
		return fallback
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

// --- state -----------------------------------------------------------------

func (h *Handler) loadState(ctx context.Context, userID int64) BrowseState {
	if h.state == nil {
		return BrowseState{}
	}
	raw, err := h.state.ModuleState(ctx, userID, moduleName)
	if err != nil || raw == "" {
		return BrowseState{}
	}
	state, err := DecodeState(raw)
	if err != nil {
		return BrowseState{}
	}
	return state
}

func (h *Handler) saveState(ctx context.Context, userID int64, state BrowseState) {
	if h.state == nil {
		return
	}
	encoded, err := EncodeState(state)
	if err != nil {
		return
	}
	_ = h.state.SetModuleState(ctx, userID, moduleName, encoded, browseStateTTL)
}

// pairAndMarks resolves the caller's active pair (nil if solo) and the
// marks their pair has set. A solo user has no marks yet, by construction.
func (h *Handler) pairAndMarks(ctx context.Context, userID int64) (*storage.Pair, map[string]storage.PositionMark, error) {
	pair, err := h.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if pair == nil {
		return nil, map[string]storage.PositionMark{}, nil
	}
	marks, err := h.repo.PairPositionMarks(ctx, pair.ID)
	if err != nil {
		return pair, nil, err
	}
	return pair, marks, nil
}

// seedFor returns the deterministic-shuffle seed for the randomiser: the
// pair id when paired, so both partners see the same sequence, and the
// telegram id otherwise so solo browsing still works.
func seedFor(pair *storage.Pair, userID int64) int64 {
	if pair != nil {
		return pair.ID
	}
	return userID
}

// visibleItems applies the stored filter and hides anything the pair has
// buried.
func (h *Handler) visibleItems(state BrowseState, marks map[string]storage.PositionMark) []catalog.Item {
	return h.service.VisibleWithMarks(state.Filter, marks)
}

// --- screens -----------------------------------------------------------------

func (h *Handler) showHub(ctx context.Context, cb *telegram.CallbackQuery, chatID int64, language string) error {
	text := h.i18n.Text(language, "positions.hub") + "\n\n" + h.i18n.Text(language, "positions.attribution")
	return h.presentText(ctx, cb, chatID, text, HubKeyboard(language))
}

func (h *Handler) showBrowse(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string, index int) error {
	pair, marks, err := h.pairAndMarks(ctx, userID)
	if err != nil {
		return err
	}
	state := h.loadState(ctx, userID)
	items := h.visibleItems(state, marks)
	if len(items) == 0 {
		return h.presentText(ctx, cb, chatID, h.i18n.Text(language, "positions.empty"), HubKeyboard(language))
	}

	item, normalized, ok := h.service.At(items, index)
	if !ok {
		return h.presentText(ctx, cb, chatID, h.i18n.Text(language, "positions.empty"), HubKeyboard(language))
	}

	state.Index = normalized
	h.saveState(ctx, userID, state)

	return h.showItem(ctx, cb, chatID, language, item, normalized, len(items), marks, pair == nil)
}

func (h *Handler) showRandom(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string) error {
	pair, marks, err := h.pairAndMarks(ctx, userID)
	if err != nil {
		return err
	}
	state := h.loadState(ctx, userID)
	items := h.visibleItems(state, marks)
	if len(items) == 0 {
		return h.presentText(ctx, cb, chatID, h.i18n.Text(language, "positions.empty"), HubKeyboard(language))
	}

	item, nextCycle, err := h.service.Random(seedFor(pair, userID), items, marks, state.Cycle)
	if err != nil {
		return h.presentText(ctx, cb, chatID, h.i18n.Text(language, "positions.empty"), HubKeyboard(language))
	}

	index := indexOf(items, item.ID)
	state.Index = index
	state.Cycle = nextCycle
	h.saveState(ctx, userID, state)

	return h.showItem(ctx, cb, chatID, language, item, index, len(items), marks, pair == nil)
}

func indexOf(items []catalog.Item, id string) int {
	for i, item := range items {
		if item.ID == id {
			return i
		}
	}
	return 0
}

// showItem renders one card: caption, keyboard and photo (or text
// fallback), and persists nothing beyond what the caller already did. solo
// is whether the viewer currently has no active pair — it drives the lock
// marker on the (still visible) mark buttons, since marks==nil is not a
// reliable signal: a solo user's marks map is a valid, non-nil empty map.
func (h *Handler) showItem(ctx context.Context, cb *telegram.CallbackQuery, chatID int64, language string, item catalog.Item, index, total int, marks map[string]storage.PositionMark, solo bool) error {
	mark := marks[item.ID]
	caption := BrowseCaption(language, item, index, total, mark.TriedAt.Valid, mark.FavoritedAt.Valid)
	keyboard := BrowseKeyboard(language, item.ID, index, mark.TriedAt.Valid, mark.FavoritedAt.Valid, solo)
	return h.presentCard(ctx, cb, chatID, item, caption, keyboard)
}

func (h *Handler) handleMark(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language, rest string) error {
	kindRaw, itemID, ok := splitOnce(rest, ":")
	if !ok {
		return h.showBrowse(ctx, cb, chatID, userID, language, h.loadState(ctx, userID).Index)
	}
	kind, ok := parseMarkKind(kindRaw)
	if !ok {
		return h.showBrowse(ctx, cb, chatID, userID, language, h.loadState(ctx, userID).Index)
	}
	item, ok := h.catalog.Item(itemID)
	if !ok {
		// Stale or forged callback for an id that no longer (or never did)
		// exist: refresh the current screen instead of writing a mark for
		// nothing, and never panic on it.
		return h.showBrowse(ctx, cb, chatID, userID, language, h.loadState(ctx, userID).Index)
	}

	pair, err := h.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		return err
	}
	if pair == nil {
		return h.presentText(ctx, cb, chatID, h.i18n.Text(language, "positions.needs_pair_for_marks"), HubKeyboard(language))
	}
	if _, err := h.repo.TogglePositionMark(ctx, pair.ID, itemID, kind, userID, h.now()); err != nil {
		return err
	}

	marks, err := h.repo.PairPositionMarks(ctx, pair.ID)
	if err != nil {
		return err
	}
	state := h.loadState(ctx, userID)
	items := h.visibleItems(state, marks)
	index := indexOf(items, item.ID)
	return h.showItem(ctx, cb, chatID, language, item, index, maxInt(len(items), 1), marks, false)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func parseMarkKind(raw string) (storage.PositionMarkKind, bool) {
	switch raw {
	case "tried":
		return storage.MarkTried, true
	case "favorited":
		return storage.MarkFavorited, true
	case "hidden":
		return storage.MarkHidden, true
	default:
		return "", false
	}
}

func (h *Handler) showFilters(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string) error {
	_, marks, err := h.pairAndMarks(ctx, userID)
	if err != nil {
		return err
	}
	state := h.loadState(ctx, userID)
	total := len(h.visibleItems(state, marks))
	text := h.i18n.Text(language, "positions.filters")
	text = sprintfSafe(text, total)
	return h.presentText(ctx, cb, chatID, text, FiltersKeyboard(language, state.Filter, h.filterFacets, total))
}

func (h *Handler) handleFilterToggle(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language, rest string) error {
	facet, value, ok := splitOnce(rest, ":")
	if !ok {
		return h.showFilters(ctx, cb, chatID, userID, language)
	}
	state := h.loadState(ctx, userID)
	state.Filter = ToggleFilterValue(state.Filter, facet, value)
	state.Index = 0
	h.saveState(ctx, userID, state)
	return h.showFilters(ctx, cb, chatID, userID, language)
}

func (h *Handler) showDumpConfirm(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string) error {
	_, marks, err := h.pairAndMarks(ctx, userID)
	if err != nil {
		return err
	}
	state := h.loadState(ctx, userID)
	total := len(h.visibleItems(state, marks))
	minutes := int(time.Duration(total) * h.dumpInterval / time.Minute)
	if minutes < 1 && total > 0 {
		minutes = 1
	}
	text := sprintfSafe(h.i18n.Text(language, "positions.dump_confirm"), total, minutes)
	return h.presentText(ctx, cb, chatID, text, DumpConfirmKeyboard(language))
}

func (h *Handler) startDump(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string) error {
	_, marks, err := h.pairAndMarks(ctx, userID)
	if err != nil {
		return err
	}
	state := h.loadState(ctx, userID)
	items := h.visibleItems(state, marks)
	if len(items) == 0 {
		return h.presentText(ctx, cb, chatID, h.i18n.Text(language, "positions.empty"), HubKeyboard(language))
	}

	h.mu.Lock()
	if existing, running := h.runs[userID]; running {
		existing() // a second "go" while one is in flight restarts it cleanly
	}
	h.mu.Unlock()

	runCtx, cancel := context.WithCancel(context.Background())

	// The map entry is only installed once the goroutine is actually about
	// to start. If presentText fails (a plausible transient Telegram error)
	// the goroutine never launches, so nothing would ever run runDump's
	// deferred cleanup — installing the entry earlier would leave a stale
	// cancel func in h.runs forever, silently no-op'd by a later
	// pos:dump:stop. Cancel our own context on that path so it is never
	// leaked either.
	if err := h.presentText(ctx, cb, chatID, h.i18n.Text(language, "positions.dump_started"), dumpStopKeyboard(language)); err != nil {
		cancel()
		return err
	}

	h.mu.Lock()
	h.runs[userID] = cancel
	h.mu.Unlock()

	go h.runDump(runCtx, chatID, userID, language, items, marks)
	return nil
}

func (h *Handler) runDump(ctx context.Context, chatID, userID int64, language string, items []catalog.Item, marks map[string]storage.PositionMark) {
	defer func() {
		h.mu.Lock()
		delete(h.runs, userID)
		h.mu.Unlock()
	}()

	sender := &dumpSender{h: h, chatID: chatID, language: language, marks: marks, total: len(items)}
	sent, err := Dump(ctx, sender, DumpOptions{Items: items, Interval: h.dumpInterval})

	var text string
	switch {
	case errors.Is(err, context.Canceled):
		text = sprintfSafe(h.i18n.Text(language, "positions.dump_stopped"), sent)
	case err != nil:
		text = sprintfSafe(h.i18n.Text(language, "positions.dump_stopped"), sent)
	default:
		text = sprintfSafe(h.i18n.Text(language, "positions.dump_done"), sent)
	}
	// Best-effort: the dump loop runs detached from the triggering request,
	// so there is no callback query left to answer or edit into.
	_ = h.bot.SendMessage(context.Background(), chatID, text, HubKeyboard(language))
}

func (h *Handler) stopDump(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string) error {
	h.mu.Lock()
	cancel, running := h.runs[userID]
	h.mu.Unlock()
	if running {
		cancel()
	}
	return nil
}

// dumpSender adapts Handler's photo-sending machinery to positions.Sender,
// tracking each item's position for the caption counter since Dump calls
// SendItem in order but does not pass an index itself.
type dumpSender struct {
	h        *Handler
	chatID   int64
	language string
	marks    map[string]storage.PositionMark
	total    int
	index    int
}

func (d *dumpSender) SendItem(ctx context.Context, item catalog.Item) error {
	mark := d.marks[item.ID]
	caption := BrowseCaption(d.language, item, d.index, d.total, mark.TriedAt.Valid, mark.FavoritedAt.Valid)
	d.index++
	return d.h.sendDumpItem(ctx, d.chatID, item, caption)
}

// --- rendering ---------------------------------------------------------------

// cachedFileID looks up the Telegram file_id already uploaded for this
// item, if any. A nil StateStore or a lookup error is treated the same as a
// miss: the caller re-uploads instead of failing.
func (h *Handler) cachedFileID(ctx context.Context, itemID string) string {
	if h.state == nil {
		return ""
	}
	fileID, err := h.state.FileID(ctx, "positions:"+itemID)
	if err != nil {
		return ""
	}
	return fileID
}

func (h *Handler) cacheFileID(ctx context.Context, itemID, fileID string) {
	if h.state == nil || fileID == "" {
		return
	}
	_ = h.state.CacheFileID(ctx, "positions:"+itemID, fileID, fileIDCacheTTL)
}

// presentCard renders one photo card, reusing a cached file_id whenever
// possible so the same image is never uploaded to Telegram twice. If the
// object store is unavailable (nil) or the item carries no media, it
// degrades to a text-only screen rather than failing.
func (h *Handler) presentCard(ctx context.Context, cb *telegram.CallbackQuery, chatID int64, item catalog.Item, caption string, markup telegram.InlineKeyboardMarkup) error {
	if fileID := h.cachedFileID(ctx, item.ID); fileID != "" {
		if cb != nil && cb.Message != nil && len(cb.Message.Photo) > 0 {
			if err := h.bot.EditMessageMediaRef(ctx, chatID, cb.Message.MessageID, fileID, caption, markup); err == nil {
				return nil
			}
			// Edit failed (e.g. the message is too old for Telegram to
			// touch) — fall through to sending a fresh message, exactly as
			// internal/app already does for stale game cards.
		}
		if _, err := h.bot.SendPhotoRef(ctx, chatID, fileID, caption, markup); err == nil {
			h.deleteStale(ctx, cb, chatID)
			return nil
		}
		// The cached id itself may have gone stale; fall through to a
		// fresh upload rather than failing the whole screen.
	}

	if h.objectStore != nil && item.Media != nil {
		if data, err := h.objectStore.Get(ctx, item.Media.Key); err == nil {
			if sent, err := h.bot.SendPhotoBytes(ctx, chatID, data, caption, markup); err == nil {
				h.cacheFileID(ctx, item.ID, sent.FileID)
				h.deleteStale(ctx, cb, chatID)
				return nil
			}
		}
	}

	return h.presentText(ctx, cb, chatID, caption, markup)
}

// sendDumpItem is presentCard's counterpart for the bulk send: every item
// becomes its own new message (there is nothing to edit in place), with the
// same cache-hit/upload/text-fallback ladder.
func (h *Handler) sendDumpItem(ctx context.Context, chatID int64, item catalog.Item, caption string) error {
	if fileID := h.cachedFileID(ctx, item.ID); fileID != "" {
		if _, err := h.bot.SendPhotoRef(ctx, chatID, fileID, caption, nil); err == nil {
			return nil
		}
	}
	if h.objectStore != nil && item.Media != nil {
		if data, err := h.objectStore.Get(ctx, item.Media.Key); err == nil {
			if sent, err := h.bot.SendPhotoBytes(ctx, chatID, data, caption, nil); err == nil {
				h.cacheFileID(ctx, item.ID, sent.FileID)
				return nil
			}
		}
	}
	return h.bot.SendMessage(ctx, chatID, caption, nil)
}

// deleteStale removes the message a freshly-sent card is replacing, so
// paging never leaves a trail of old cards in the chat.
func (h *Handler) deleteStale(ctx context.Context, cb *telegram.CallbackQuery, chatID int64) {
	if cb != nil && cb.Message != nil {
		_ = h.bot.DeleteMessage(ctx, chatID, cb.Message.MessageID)
	}
}

// presentText renders a plain-text screen (hub, filters, dump confirm),
// editing the triggering message in place when possible. A message that
// currently carries a photo cannot become a text message via an edit, so
// that case deletes and sends fresh — the same fallback internal/app's own
// editCallbackScreen uses for its non-game screens.
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
