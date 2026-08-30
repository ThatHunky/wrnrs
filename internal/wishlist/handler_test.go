package wishlist_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"wrnrs/internal/catalog"
	"wrnrs/internal/i18n"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
	"wrnrs/internal/wishlist"
)

// testBundle carries the real "uk" copy for every key the keyboard builders
// read, so tests exercise the same lookups production code will make.
func testBundle() *i18n.Bundle {
	b := i18n.NewBundle()
	b.Add(i18n.Catalog{Language: "uk", Brand: "між нами.", Strings: map[string]string{
		"wish.hub.title":      "Бажання",
		"wish.hub.progress":   "Відмічено: %d з %d",
		"wish.hub.swipe":      "💛 Відмічати",
		"wish.hub.matches":    "🔥 Збіги (%d)",
		"wish.hub.mine":       "📊 Мої відповіді",
		"wish.answer.want":    "💛 Хочу",
		"wish.answer.curious": "🤔 Цікаво",
		"wish.answer.no":      "🚫 Ні",
		"wish.answer.skip":    "⏭ Пропустити",
	}})
	return b
}

func TestSwipeKeyboardOffersThreeAnswersAndSkip(t *testing.T) {
	markup := wishlist.SwipeKeyboard(testBundle(), "uk", "w007")

	var data []string
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			data = append(data, button.CallbackData)
		}
	}
	joined := strings.Join(data, " ")

	for _, want := range []string{
		"wish:answer:wish:w007:want",
		"wish:answer:wish:w007:curious",
		"wish:answer:wish:w007:no",
		"wish:next",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("keyboard callbacks %q are missing %q", joined, want)
		}
	}
	for _, d := range data {
		if len(d) > 64 {
			t.Fatalf("callback data %q is %d bytes, over Telegram's 64-byte cap", d, len(d))
		}
	}
}

func TestHubKeyboardHidesMatchesCountWithoutAPair(t *testing.T) {
	withPair := wishlist.HubKeyboard(testBundle(), "uk", true, 4)
	withoutPair := wishlist.HubKeyboard(testBundle(), "uk", false, 0)

	var withText, withoutText string
	for _, row := range withPair.InlineKeyboard {
		for _, b := range row {
			withText += b.Text + " "
		}
	}
	for _, row := range withoutPair.InlineKeyboard {
		for _, b := range row {
			withoutText += b.Text + " "
		}
	}
	if !strings.Contains(withText, "4") {
		t.Fatalf("paired hub %q does not show the match count", withText)
	}
	if strings.Contains(withoutText, "(0)") {
		t.Fatalf("solo hub %q shows a zero match count; it should not promise matches without a pair", withoutText)
	}
}

// TestHubKeyboardButtonLabelComesFromBundle guards against the button copy
// creeping back into Go as hardcoded uk/en literals: it registers a
// distinctive value for "wish.hub.swipe" that appears nowhere in the source
// and asserts the rendered keyboard carries exactly that text. If
// HubKeyboard ever stops reading the bundle, this is the test that notices.
func TestHubKeyboardButtonLabelComesFromBundle(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.Add(i18n.Catalog{Language: "uk", Strings: map[string]string{
		"wish.hub.swipe":   "💛 XYZZY-СЛОВО",
		"wish.hub.matches": "🔥 Збіги (%d)",
		"wish.hub.mine":    "📊 Мої відповіді",
	}})

	markup := wishlist.HubKeyboard(bundle, "uk", true, 1)

	var text string
	for _, row := range markup.InlineKeyboard {
		for _, b := range row {
			text += b.Text + " "
		}
	}
	if !strings.Contains(text, "XYZZY-СЛОВО") {
		t.Fatalf("hub keyboard %q does not contain the bundle-supplied swipe label; button text must come from the bundle, not a hardcoded literal", text)
	}
}

func TestBackKeyboardLabelComesFromBundle(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.Add(i18n.Catalog{Language: "uk", Strings: map[string]string{
		"wish.hub.title": "XYZZY-НАЗВА",
	}})

	markup := wishlist.BackKeyboard(bundle, "uk")

	var text string
	for _, row := range markup.InlineKeyboard {
		for _, b := range row {
			text += b.Text + " "
		}
	}
	if !strings.Contains(text, "XYZZY-НАЗВА") {
		t.Fatalf("back keyboard %q does not contain the bundle-supplied title; button text must come from the bundle, not a hardcoded literal", text)
	}
}

func TestSwipeCaptionShowsTitleAndProgress(t *testing.T) {
	item := catalog.Item{
		ID:   "w007",
		Text: map[string]catalog.ItemText{"uk": {Title: "Свічки", Body: "Кімната, освітлена лише свічками."}},
	}
	caption := wishlist.SwipeCaption(testBundle(), "uk", item, 6, 60)

	if !strings.Contains(caption, "Свічки") {
		t.Fatalf("caption %q does not contain the title", caption)
	}
	if !strings.Contains(caption, "Кімната") {
		t.Fatalf("caption %q does not contain the body", caption)
	}
	if !strings.Contains(caption, "6") || !strings.Contains(caption, "60") {
		t.Fatalf("caption %q does not contain the 6/60 progress", caption)
	}
}

// --- Handler routing tests (no Telegram network involved: fakeBot and
// fakeWishRepo are in-memory stubs, in the same style as
// internal/positions/handler_test.go's fakePhotoBot) --------------------

type fakeBot struct {
	sentTexts   []string
	editedTexts []string
	deletedIDs  []int64
}

func (b *fakeBot) SendMessage(_ context.Context, _ int64, text string, _ any) error {
	b.sentTexts = append(b.sentTexts, text)
	return nil
}

func (b *fakeBot) EditMessageText(_ context.Context, _, _ int64, text string, _ any) error {
	b.editedTexts = append(b.editedTexts, text)
	return nil
}

func (b *fakeBot) DeleteMessage(_ context.Context, _, messageID int64) error {
	b.deletedIDs = append(b.deletedIDs, messageID)
	return nil
}

type setAnswerCall struct {
	userID int64
	kind   storage.WishItemKind
	itemID string
	answer storage.WishAnswer
}

type fakeWishRepo struct {
	pair          *storage.Pair
	answers       map[string]storage.WishAnswer
	matches       []storage.WishMatch
	partnerActive bool
	language      string

	activePairCalls int
	setAnswerCalls  []setAnswerCall
}

func (r *fakeWishRepo) ActivePairForUser(_ context.Context, _ int64) (*storage.Pair, error) {
	r.activePairCalls++
	return r.pair, nil
}

func (r *fakeWishRepo) UserLanguage(_ context.Context, _ int64) (string, error) {
	return r.language, nil
}

func (r *fakeWishRepo) SetWishAnswer(_ context.Context, userID int64, kind storage.WishItemKind, itemID string, answer storage.WishAnswer, _ time.Time) error {
	r.setAnswerCalls = append(r.setAnswerCalls, setAnswerCall{userID: userID, kind: kind, itemID: itemID, answer: answer})
	if r.answers == nil {
		r.answers = map[string]storage.WishAnswer{}
	}
	r.answers[wishlist.AnswerKey(kind, itemID)] = answer
	return nil
}

func (r *fakeWishRepo) UserWishAnswers(_ context.Context, _ int64) (map[string]storage.WishAnswer, error) {
	return r.answers, nil
}

func (r *fakeWishRepo) PairWishMatches(_ context.Context, _ int64) ([]storage.WishMatch, error) {
	return r.matches, nil
}

func (r *fakeWishRepo) PartnerHasAnyWishAnswer(_ context.Context, _, _ int64) (bool, error) {
	return r.partnerActive, nil
}

func newTestHandler(repo *fakeWishRepo, bot *fakeBot) *wishlist.Handler {
	service := wishlist.NewService(wishlist.ServiceOptions{Catalog: testCatalog()})
	return wishlist.NewHandler(wishlist.HandlerOptions{
		Service:    service,
		Repository: repo,
		Bot:        bot,
		I18n:       testBundle(),
	})
}

func privateCallback(userID int64, data string) *telegram.CallbackQuery {
	return &telegram.CallbackQuery{
		From:    telegram.User{ID: userID},
		Message: &telegram.Message{MessageID: 10, Chat: telegram.Chat{ID: userID, Type: "private"}},
		Data:    data,
	}
}

// TestHandleCallbackParsesValidAnswer pins the parsing of
// "wish:answer:wish:w001:want" into kind=wish, id=w001, answer=want, and
// that a recognised answer is actually persisted through SetWishAnswer.
func TestHandleCallbackParsesValidAnswer(t *testing.T) {
	repo := &fakeWishRepo{}
	bot := &fakeBot{}
	h := newTestHandler(repo, bot)

	if err := h.HandleCallback(context.Background(), privateCallback(1, "wish:answer:wish:w001:want")); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}

	if len(repo.setAnswerCalls) != 1 {
		t.Fatalf("SetWishAnswer calls = %d, want 1", len(repo.setAnswerCalls))
	}
	got := repo.setAnswerCalls[0]
	if got.kind != storage.WishKindWish || got.itemID != "w001" || got.answer != storage.AnswerWant {
		t.Fatalf("SetWishAnswer call = %+v, want kind=wish id=w001 answer=want", got)
	}
}

// TestHandleCallbackIgnoresUnrecognisedAnswerCallbacks asserts that a
// malformed or unrecognised wish:answer callback never panics and never
// reaches storage.SetWishAnswer — it must re-render the current screen
// instead of writing anything.
func TestHandleCallbackIgnoresUnrecognisedAnswerCallbacks(t *testing.T) {
	for _, data := range []string{
		"wish:answer:song:w001:want",  // unknown kind
		"wish:answer:wish:w001:bogus", // unknown answer
		"wish:answer:",                // nothing to split
		"wish:answer:wish",            // no colon left to split
	} {
		t.Run(data, func(t *testing.T) {
			repo := &fakeWishRepo{}
			bot := &fakeBot{}
			h := newTestHandler(repo, bot)

			if err := h.HandleCallback(context.Background(), privateCallback(1, data)); err != nil {
				t.Fatalf("HandleCallback(%q): %v", data, err)
			}
			if len(repo.setAnswerCalls) != 0 {
				t.Fatalf("HandleCallback(%q) wrote %d answers, want 0", data, len(repo.setAnswerCalls))
			}
		})
	}
}

// TestHandleCallbackRefusesGroupChats pins the same guard
// internal/positions/handler.go applies: a callback whose message came from
// a shared chat must never reach the repository, regardless of which wish:
// screen it names.
func TestHandleCallbackRefusesGroupChats(t *testing.T) {
	for _, chatType := range []string{"group", "supergroup", "channel"} {
		t.Run(chatType, func(t *testing.T) {
			repo := &fakeWishRepo{}
			bot := &fakeBot{}
			h := newTestHandler(repo, bot)
			cb := &telegram.CallbackQuery{
				From:    telegram.User{ID: 1},
				Message: &telegram.Message{MessageID: 10, Chat: telegram.Chat{ID: -100, Type: chatType}},
				Data:    "wish:open",
			}

			if err := h.HandleCallback(context.Background(), cb); err != nil {
				t.Fatalf("HandleCallback: %v", err)
			}
			if repo.activePairCalls != 0 {
				t.Fatalf("group chat callback reached the repository: %d ActivePairForUser calls", repo.activePairCalls)
			}
			if len(bot.sentTexts)+len(bot.editedTexts) != 0 {
				t.Fatal("group chat callback produced a bot reply")
			}
		})
	}
}

// TestHandleMessageNeverConsumesText pins the Р5 rule that this module reads
// no free text: HandleMessage must always report (false, nil).
func TestHandleMessageNeverConsumesText(t *testing.T) {
	h := newTestHandler(&fakeWishRepo{}, &fakeBot{})
	handled, err := h.HandleMessage(context.Background(), &telegram.Message{Text: "hello"})
	if handled || err != nil {
		t.Fatalf("HandleMessage = (%v, %v), want (false, nil)", handled, err)
	}
}

// TestShowMatchesWithoutPairShowsNeedsPair and
// TestShowMatchesWithPairButNoMatchesShowsEmpty pin the requirement that the
// two "nothing to show yet" states on the matches screen must read
// differently: one because there is no partner to match with at all, the
// other because there is a partner but nothing has matched (yet).
func TestShowMatchesWithoutPairShowsNeedsPair(t *testing.T) {
	repo := &fakeWishRepo{pair: nil}
	bot := &fakeBot{}
	h := newTestHandler(repo, bot)

	if err := h.HandleCallback(context.Background(), privateCallback(1, "wish:matches")); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	text := lastText(bot)
	if !strings.Contains(text, testBundle().Text("uk", "wish.matches.needs_pair")) {
		t.Fatalf("solo matches screen %q does not show wish.matches.needs_pair", text)
	}
}

func TestShowMatchesWithPairButNoMatchesShowsEmpty(t *testing.T) {
	repo := &fakeWishRepo{pair: &storage.Pair{ID: 1, UserAID: 1, UserBID: 2}}
	bot := &fakeBot{}
	h := newTestHandler(repo, bot)

	if err := h.HandleCallback(context.Background(), privateCallback(1, "wish:matches")); err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	text := lastText(bot)
	if !strings.Contains(text, testBundle().Text("uk", "wish.matches.empty")) {
		t.Fatalf("paired-but-empty matches screen %q does not show wish.matches.empty", text)
	}
	if strings.Contains(text, testBundle().Text("uk", "wish.matches.needs_pair")) {
		t.Fatalf("paired-but-empty matches screen %q wrongly shows wish.matches.needs_pair", text)
	}
}

func lastText(bot *fakeBot) string {
	if len(bot.editedTexts) > 0 {
		return bot.editedTexts[len(bot.editedTexts)-1]
	}
	if len(bot.sentTexts) > 0 {
		return bot.sentTexts[len(bot.sentTexts)-1]
	}
	return ""
}
