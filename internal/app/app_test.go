package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"wrnrs/internal/admin"
	"wrnrs/internal/config"
	"wrnrs/internal/content"
	"wrnrs/internal/i18n"
	"wrnrs/internal/onboarding"
	"wrnrs/internal/pairing"
	"wrnrs/internal/render"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

var errFakeEditFailed = errors.New("fake edit failed")

type fakeBot struct {
	messages         []sentMessage
	edits            []editedMessage
	captionEdits     []editedMessage
	replyMarkupEdits []editedMessage
	mediaEdits       []editedMedia
	invoices         []sentInvoice
	inlineAnswers    []answeredInlineQuery
	deletedMessages  []deletedMessage
	sentPhotos       []sentPhoto
	photos           int
	failCaptionEdits bool
	failTextEdits    bool
	failMarkupEdits  bool
}

type deletedMessage struct {
	chatID    int64
	messageID int64
}

type answeredInlineQuery struct {
	id         string
	results    []telegram.InlineQueryResult
	cacheTime  int
	isPersonal bool
}

type sentInvoice struct {
	chatID      int64
	title       string
	description string
	payload     string
	amount      int64
	markup      any
}

type editedMedia struct {
	chatID    int64
	messageID int64
	png       []byte
	caption   string
	markup    any
}

type sentMessage struct {
	chatID int64
	text   string
	markup any
}

type sentPhoto struct {
	chatID  int64
	caption string
	markup  any
}

type editedMessage struct {
	chatID    int64
	messageID int64
	text      string
	markup    any
}

func (b *fakeBot) SendMessage(_ context.Context, chatID int64, text string, replyMarkup any) error {
	b.messages = append(b.messages, sentMessage{chatID: chatID, text: text, markup: replyMarkup})
	return nil
}

func (b *fakeBot) EditMessageText(_ context.Context, chatID, messageID int64, text string, replyMarkup any) error {
	if b.failTextEdits {
		return errFakeEditFailed
	}
	b.edits = append(b.edits, editedMessage{chatID: chatID, messageID: messageID, text: text, markup: replyMarkup})
	return nil
}

func (b *fakeBot) EditMessageCaption(_ context.Context, chatID, messageID int64, caption string, replyMarkup any) error {
	if b.failCaptionEdits {
		return errFakeEditFailed
	}
	b.captionEdits = append(b.captionEdits, editedMessage{chatID: chatID, messageID: messageID, text: caption, markup: replyMarkup})
	return nil
}

func (b *fakeBot) EditMessageReplyMarkup(_ context.Context, chatID, messageID int64, replyMarkup any) error {
	if b.failMarkupEdits {
		return errFakeEditFailed
	}
	b.replyMarkupEdits = append(b.replyMarkupEdits, editedMessage{chatID: chatID, messageID: messageID, markup: replyMarkup})
	return nil
}

func (b *fakeBot) SendPhoto(_ context.Context, chatID int64, _ []byte, caption string, replyMarkup any) error {
	b.photos++
	b.sentPhotos = append(b.sentPhotos, sentPhoto{chatID: chatID, caption: caption, markup: replyMarkup})
	return nil
}

func (b *fakeBot) EditMessageMedia(_ context.Context, chatID, messageID int64, png []byte, caption string, replyMarkup any) error {
	b.mediaEdits = append(b.mediaEdits, editedMedia{
		chatID:    chatID,
		messageID: messageID,
		png:       png,
		caption:   caption,
		markup:    replyMarkup,
	})
	return nil
}

func (b *fakeBot) AnswerCallbackQuery(context.Context, string, string) error          { return nil }
func (b *fakeBot) AnswerPreCheckoutQuery(context.Context, string, bool, string) error { return nil }
func (b *fakeBot) AnswerInlineQuery(_ context.Context, inlineQueryID string, results []telegram.InlineQueryResult, cacheTime int, isPersonal bool) error {
	b.inlineAnswers = append(b.inlineAnswers, answeredInlineQuery{
		id:         inlineQueryID,
		results:    results,
		cacheTime:  cacheTime,
		isPersonal: isPersonal,
	})
	return nil
}
func (b *fakeBot) SendInvoice(_ context.Context, chatID int64, title, description, payload string, amount int64, replyMarkup any) error {
	b.invoices = append(b.invoices, sentInvoice{
		chatID:      chatID,
		title:       title,
		description: description,
		payload:     payload,
		amount:      amount,
		markup:      replyMarkup,
	})
	return nil
}

func (b *fakeBot) DeleteMessage(_ context.Context, chatID, messageID int64) error {
	b.deletedMessages = append(b.deletedMessages, deletedMessage{chatID: chatID, messageID: messageID})
	return nil
}

func (b *fakeBot) GetFile(_ context.Context, fileID string) (telegram.File, error) {
	return telegram.File{
		FileID:   fileID,
		FilePath: "photos/" + fileID + ".png",
	}, nil
}

func createValidPNGBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func (b *fakeBot) DownloadFile(_ context.Context, filePath string) ([]byte, error) {
	return createValidPNGBytes(), nil
}

type fakeState struct {
	values         map[int64]string
	completions    map[int64]GameCompletion
	blockedActions map[string]bool
	pairLockCalls  []int64
}

func newFakeState() *fakeState {
	return &fakeState{values: map[int64]string{}, completions: map[int64]GameCompletion{}, blockedActions: map[string]bool{}}
}

func (s *fakeState) SetFSM(_ context.Context, userID int64, value string, _ time.Duration) error {
	s.values[userID] = value
	return nil
}

func (s *fakeState) GetFSM(_ context.Context, userID int64) (string, error) {
	return s.values[userID], nil
}

func (s *fakeState) ClearFSM(_ context.Context, userID int64) error {
	delete(s.values, userID)
	return nil
}

func (s *fakeState) SetPendingGameCompletion(_ context.Context, userID int64, completion GameCompletion, _ time.Duration) error {
	s.completions[userID] = completion
	return nil
}

func (s *fakeState) PendingGameCompletion(_ context.Context, userID int64) (GameCompletion, bool, error) {
	completion, ok := s.completions[userID]
	return completion, ok, nil
}

func (s *fakeState) ClearPendingGameCompletion(_ context.Context, userID int64) error {
	delete(s.completions, userID)
	return nil
}

func (s *fakeState) AllowUserAction(_ context.Context, _ int64, action string, _ int, _ time.Duration) (bool, error) {
	if s.blockedActions[action] {
		return false, nil
	}
	return true, nil
}

func (s *fakeState) WithPairLock(_ context.Context, pairID int64, _ time.Duration, fn func() error) error {
	s.pairLockCalls = append(s.pairLockCalls, pairID)
	return fn()
}

func TestOnboardingNameAdvancesToGenderInsteadOfMainMenu(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()

	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("/start")}); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("onboarding:language:uk")}); err != nil {
		t.Fatalf("language callback failed: %v", err)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("Сєва")}); err != nil {
		t.Fatalf("name message failed: %v", err)
	}

	if got := state.values[1001]; got != string(onboarding.StepGender) {
		t.Fatalf("fsm step = %q, want %q", got, onboarding.StepGender)
	}
	last := bot.messages[len(bot.messages)-1].text
	if !strings.Contains(last, "гендер") && !strings.Contains(last, "стать") {
		t.Fatalf("last message = %q, want gender prompt", last)
	}
	if strings.EqualFold(last, "Main menu") {
		t.Fatal("name input returned to main menu")
	}
}

func TestMainMenuCallbacksOpenSpecificScreens(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()

	cases := map[string]string{
		"pair:menu":     "Pair",
		"theme:menu":    "Theme",
		"journal:open":  "Journal",
		"settings:open": "Settings",
	}
	for data, want := range cases {
		messageBefore := len(bot.messages)
		editBefore := len(bot.edits)
		if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback(data)}); err != nil {
			t.Fatalf("%s callback failed: %v", data, err)
		}
		if len(bot.messages) != messageBefore {
			t.Fatalf("%s sent %d new messages, want 0", data, len(bot.messages)-messageBefore)
		}
		if len(bot.edits) != editBefore+1 {
			t.Fatalf("%s edited %d messages, want 1", data, len(bot.edits)-editBefore)
		}
		got := bot.edits[len(bot.edits)-1].text
		if !strings.Contains(got, want) {
			t.Fatalf("%s response = %q, want to contain %q", data, got, want)
		}
		if got == "Main menu" {
			t.Fatalf("%s returned main menu", data)
		}
	}
}

func TestCallbackWithoutMessageFallsBackToSendMessage(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()
	cb := testCallback("settings:open")
	cb.Message = nil

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("callback failed: %v", err)
	}
	if len(bot.edits) != 0 {
		t.Fatalf("edited %d messages, want 0", len(bot.edits))
	}
	if len(bot.messages) != 1 {
		t.Fatalf("sent %d messages, want fallback send", len(bot.messages))
	}
}

func TestReplyMenuButtonReturnsMainMenu(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("Меню")}); err != nil {
		t.Fatalf("menu message failed: %v", err)
	}
	got := bot.messages[before].text
	if !strings.Contains(got, "між нами.") || !strings.Contains(got, "Сєва") {
		t.Fatalf("menu response = %q, want dynamic Ukrainian main menu", got)
	}
	if _, ok := bot.messages[before].markup.(telegram.InlineKeyboardMarkup); !ok {
		t.Fatalf("menu markup = %T, want inline main menu", bot.messages[before].markup)
	}
}

func TestOnboardingCanReachThemeColorStep(t *testing.T) {
	app, _, state := newTestApp(t)
	ctx := context.Background()

	steps := []telegram.Update{
		{Message: testMessage("/start")},
		{CallbackQuery: testCallback("onboarding:language:en")},
		{Message: testMessage("Seva")},
		{CallbackQuery: testCallback("onboarding:gender:male")},
		{CallbackQuery: testCallback("onboarding:adult:yes")},
		{CallbackQuery: testCallback("onboarding:mature:yes")},
	}
	for _, update := range steps {
		if err := app.HandleUpdate(ctx, update); err != nil {
			t.Fatalf("HandleUpdate failed: %v", err)
		}
	}

	if got := state.values[1001]; got != string(onboarding.StepThemeColor) {
		t.Fatalf("fsm step = %q, want %q", got, onboarding.StepThemeColor)
	}
}

func TestSavedLanguageIsUsedForFallbackMessages(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()

	for _, update := range []telegram.Update{
		{Message: testMessage("/start")},
		{CallbackQuery: testCallback("onboarding:language:uk")},
		{Message: testMessage("Сєва")},
		{CallbackQuery: testCallback("onboarding:gender:male")},
		{CallbackQuery: testCallback("onboarding:adult:no")},
		{CallbackQuery: testCallback("theme:color:#8da68f")},
		{CallbackQuery: testCallback("onboarding:bg:skip")},
	} {
		if err := app.HandleUpdate(ctx, update); err != nil {
			t.Fatalf("onboarding update failed: %v", err)
		}
	}

	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("неочікуваний текст")}); err != nil {
		t.Fatalf("fallback message failed: %v", err)
	}
	got := bot.messages[before].text
	if strings.Contains(got, "Main menu") {
		t.Fatalf("fallback used Telegram profile language: %q", got)
	}
	if !strings.Contains(got, "між нами.") || !strings.Contains(got, "Сєва") {
		t.Fatalf("fallback = %q, want dynamic Ukrainian main menu", got)
	}
}

func TestOnboardingOwnContactStoresPhoneHashOnly(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()

	for _, update := range []telegram.Update{
		{Message: testMessage("/start")},
		{CallbackQuery: testCallback("onboarding:language:en")},
		{Message: testMessage("Seva")},
		{CallbackQuery: testCallback("onboarding:gender:male")},
	} {
		if err := app.HandleUpdate(ctx, update); err != nil {
			t.Fatalf("onboarding update failed: %v", err)
		}
	}
	if got := state.values[1001]; got != string(onboarding.StepOwnContact) {
		t.Fatalf("fsm step = %q, want %q", got, onboarding.StepOwnContact)
	}
	before := len(bot.messages)
	contact := testContactMessage(1001, "+380977797598", "Seva")
	if err := app.HandleUpdate(ctx, telegram.Update{Message: contact}); err != nil {
		t.Fatalf("contact update failed: %v", err)
	}
	hash, err := app.repo.UserPhoneHash(ctx, 1001)
	if err != nil {
		t.Fatalf("UserPhoneHash returned error: %v", err)
	}
	if !hash.Valid || hash.String == "" {
		t.Fatal("phone hash was not stored")
	}
	if strings.Contains(hash.String, "380977797598") || strings.Contains(hash.String, "+") {
		t.Fatalf("stored phone hash appears to contain raw phone: %q", hash.String)
	}
	if got := state.values[1001]; got != string(onboarding.StepAdult) {
		t.Fatalf("fsm step after contact = %q, want %q", got, onboarding.StepAdult)
	}
	if !strings.Contains(bot.messages[before].text, "18") {
		t.Fatalf("next prompt = %q, want adult confirmation", bot.messages[before].text)
	}
}

func TestOnboardingBackgroundSkipCompletesAndOffersPairing(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()

	for _, update := range []telegram.Update{
		{Message: testMessage("/start")},
		{CallbackQuery: testCallback("onboarding:language:en")},
		{Message: testMessage("Seva")},
		{CallbackQuery: testCallback("onboarding:gender:male")},
		{CallbackQuery: testCallback("onboarding:contact:skip")},
		{CallbackQuery: testCallback("onboarding:adult:no")},
		{CallbackQuery: testCallback("theme:color:#8da68f")},
	} {
		if err := app.HandleUpdate(ctx, update); err != nil {
			t.Fatalf("onboarding update failed: %v", err)
		}
	}
	if got := state.values[1001]; got != string(onboarding.StepBackground) {
		t.Fatalf("fsm step = %q, want %q", got, onboarding.StepBackground)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("onboarding:bg:skip")}); err != nil {
		t.Fatalf("background skip failed: %v", err)
	}
	if got := state.values[1001]; got != "" {
		t.Fatalf("fsm after background skip = %q, want cleared", got)
	}
	if complete, err := app.repo.UserOnboardingComplete(ctx, 1001); err != nil {
		t.Fatalf("UserOnboardingComplete returned error: %v", err)
	} else if !complete {
		t.Fatal("onboarding was not marked complete")
	}
	last := bot.edits[len(bot.edits)-1]
	if !strings.Contains(last.text, "Pair") || strings.Contains(last.text, "Start / Resume") {
		t.Fatalf("main menu after onboarding = %q", last.text)
	}
}

func TestChangingLanguageFromSettingsDoesNotRestartOnboarding(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("onboarding:language_menu")}); err != nil {
		t.Fatalf("language menu callback failed: %v", err)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("onboarding:language:en")}); err != nil {
		t.Fatalf("language callback failed: %v", err)
	}

	if got := state.values[1001]; got != "" {
		t.Fatalf("fsm = %q, want cleared", got)
	}
	language, err := app.repo.UserLanguage(ctx, 1001)
	if err != nil {
		t.Fatalf("UserLanguage returned error: %v", err)
	}
	if language != "en" {
		t.Fatalf("language = %q, want en", language)
	}
	lastEdit := bot.edits[len(bot.edits)-1].text
	if strings.Contains(lastEdit, "What should I call you") || strings.Contains(lastEdit, "Як тебе називати") {
		t.Fatalf("language change restarted onboarding: %q", lastEdit)
	}
	if !strings.Contains(lastEdit, "Language saved") {
		t.Fatalf("language change edit = %q, want saved confirmation", lastEdit)
	}
}

func TestCommandInterruptClearsOnboardingFSM(t *testing.T) {
	app, _, state := newTestApp(t)
	ctx := context.Background()
	state.values[1001] = string(onboarding.StepName)

	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("/paysupport")}); err != nil {
		t.Fatalf("paysupport failed: %v", err)
	}

	if got := state.values[1001]; got != "" {
		t.Fatalf("fsm after /paysupport = %q, want cleared", got)
	}
}

func TestPairMenuAndCustomColorPromptHaveBackButtons(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("pair:menu")}); err != nil {
		t.Fatalf("pair menu failed: %v", err)
	}
	pairMarkup, ok := bot.edits[len(bot.edits)-1].markup.(telegram.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("pair menu markup = %T, want inline keyboard", bot.edits[len(bot.edits)-1].markup)
	}
	if !inlineKeyboardHasCallback(pairMarkup, "menu:main") {
		t.Fatalf("pair menu keyboard = %#v, want menu:main back button", pairMarkup)
	}

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("theme:color:custom")}); err != nil {
		t.Fatalf("custom color callback failed: %v", err)
	}
	colorMarkup, ok := bot.edits[len(bot.edits)-1].markup.(telegram.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("custom color markup = %T, want inline keyboard", bot.edits[len(bot.edits)-1].markup)
	}
	if !inlineKeyboardHasCallback(colorMarkup, "theme:menu") {
		t.Fatalf("custom color keyboard = %#v, want theme:menu back button", colorMarkup)
	}
}

func TestSharedContactOutsidePairingDoesNotTriggerPairingError(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testContactMessage(2002, "+380977797598", "Діана")}); err != nil {
		t.Fatalf("contact outside pairing failed: %v", err)
	}

	got := bot.messages[before].text
	if strings.Contains(got, "Спочатку") || strings.Contains(got, "Open Pair first") {
		t.Fatalf("contact outside pairing got pairing-specific response: %q", got)
	}
}

func TestActivePairMenuShowsNamesInsteadOfTelegramIDs(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)
	if err := app.repo.UpsertUser(ctx, storage.User{
		TelegramID:      2002,
		Username:        "partner",
		DisplayName:     "Діана",
		Language:        "uk",
		ThemeBaseColor:  "#d98c9f",
		SelectedStyleID: "default_warm",
	}); err != nil {
		t.Fatalf("UpsertUser partner returned error: %v", err)
	}
	request, err := app.createPairRequest(ctx, 1001, pairingIdentifierTelegramID(2002))
	if err != nil {
		t.Fatalf("createPairRequest returned error: %v", err)
	}
	if _, err := app.repo.AcceptPairRequest(ctx, request.InviteToken, 2002); err != nil {
		t.Fatalf("AcceptPairRequest returned error: %v", err)
	}

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("pair:menu")}); err != nil {
		t.Fatalf("pair menu failed: %v", err)
	}
	got := bot.edits[len(bot.edits)-1].text
	if strings.Contains(got, "1001") || strings.Contains(got, "2002") {
		t.Fatalf("active pair exposed raw ids: %q", got)
	}
	if !strings.Contains(got, "Сєва") || !strings.Contains(got, "Діана") {
		t.Fatalf("active pair text = %q, want display names", got)
	}
}

func TestSettingsDeleteAccountDeletesUserAndClearsState(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)
	state.values[1001] = "game:await_answer:q001"
	state.completions[1001] = GameCompletion{QuestionID: "q001", Type: "typed", AnswerText: "test"}

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("settings:delete_account")}); err != nil {
		t.Fatalf("delete account prompt failed: %v", err)
	}
	prompt := bot.edits[len(bot.edits)-1].text
	if !strings.Contains(prompt, "видалить") && !strings.Contains(prompt, "delete") {
		t.Fatalf("delete prompt = %q, want warning", prompt)
	}

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("settings:delete_confirm")}); err != nil {
		t.Fatalf("delete confirm failed: %v", err)
	}
	if got := state.values[1001]; got != "" {
		t.Fatalf("fsm after delete = %q, want cleared", got)
	}
	if _, ok := state.completions[1001]; ok {
		t.Fatal("pending completion was not cleared")
	}
	if language, err := app.repo.UserLanguage(ctx, 1001); err != nil {
		t.Fatalf("UserLanguage after delete returned error: %v", err)
	} else if language != "" {
		t.Fatalf("language after delete = %q, want user removed", language)
	}
}

func TestMainMenuIncludesUserAndPairStatus(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("Меню")}); err != nil {
		t.Fatalf("menu message failed: %v", err)
	}

	got := bot.messages[before].text
	if !strings.Contains(got, "Сєва") {
		t.Fatalf("menu text = %q, want user display name", got)
	}
	if !strings.Contains(got, "Партнера") {
		t.Fatalf("menu text = %q, want unpaired status", got)
	}
}

func TestSuccessfulPaymentRejectsSpoofedPayloadUser(t *testing.T) {
	app, _, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	err := app.HandleUpdate(ctx, telegram.Update{Message: testSuccessfulPaymentMessage(1001, "sku=premium_lifetime;user=2002", "charge-spoof")})
	if err == nil {
		t.Fatal("spoofed payment payload returned nil error")
	}
	if premium, checkErr := app.repo.UserHasEntitlement(ctx, 2002, storage.EntitlementPremiumAccess, "premium_access"); checkErr != nil {
		t.Fatalf("UserHasEntitlement returned error: %v", checkErr)
	} else if premium {
		t.Fatal("spoofed payment granted premium to payload user")
	}
}

func TestSuccessfulPaymentStoresReceiptAndGrantsPremium(t *testing.T) {
	app, _, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	msg := testSuccessfulPaymentMessage(1001, "sku=premium_lifetime;user=1001", "charge-1")
	if err := app.HandleUpdate(ctx, telegram.Update{Message: msg}); err != nil {
		t.Fatalf("successful payment failed: %v", err)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{Message: msg}); err != nil {
		t.Fatalf("duplicate successful payment failed: %v", err)
	}

	if premium, err := app.repo.UserHasEntitlement(ctx, 1001, storage.EntitlementPremiumAccess, "premium_access"); err != nil {
		t.Fatalf("UserHasEntitlement returned error: %v", err)
	} else if !premium {
		t.Fatal("successful payment did not grant premium")
	}
	if receipt, err := app.repo.PurchaseReceiptByCharge(ctx, "charge-1"); err != nil {
		t.Fatalf("PurchaseReceiptByCharge returned error: %v", err)
	} else if receipt.UserID != 1001 || receipt.SKU != "premium_lifetime" {
		t.Fatalf("receipt = %#v, want premium receipt for 1001", receipt)
	}
}

func TestSupportTextDoesNotUseUnrenderedMarkdown(t *testing.T) {
	app, _, _ := newTestApp(t)
	app.cfg.Donation.MonobankURL = "https://send.monobank.ua/jar/test"
	app.cfg.Donation.CardNumber = "4441111122223333"

	got := app.supportText("en")
	if strings.Contains(got, "`") {
		t.Fatalf("support text contains raw markdown backticks: %q", got)
	}
}

func TestPairMenuAcceptsSharedContactWithoutReturningMainMenu(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("pair:menu")}); err != nil {
		t.Fatalf("pair menu failed: %v", err)
	}
	if got := state.values[1001]; got != "pairing:await_identifier" {
		t.Fatalf("fsm = %q, want pairing identifier step", got)
	}

	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testContactMessage(2002, "+380977797598", "Діана")}); err != nil {
		t.Fatalf("contact message failed: %v", err)
	}
	got := bot.messages[before].text
	if strings.Contains(got, "Main menu") {
		t.Fatalf("contact fell through to main menu: %q", got)
	}
	if !strings.Contains(got, "Запит") && !strings.Contains(got, "запрошення") {
		t.Fatalf("contact response = %q, want Ukrainian pairing response", got)
	}
	if len(bot.messages) < before+2 {
		t.Fatalf("target user was not notified; sent %d messages", len(bot.messages)-before)
	}
	if bot.messages[before+1].chatID != 2002 {
		t.Fatalf("target notification chat = %d, want 2002", bot.messages[before+1].chatID)
	}
}

func TestInviteAcceptanceCreatesActivePair(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("pair:menu")}); err != nil {
		t.Fatalf("pair menu failed: %v", err)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("@partner")}); err != nil {
		t.Fatalf("username pairing failed: %v", err)
	}
	token := extractToken(t, bot.messages[len(bot.messages)-1].text)

	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessageFrom(2002, "partner", "en", "/start pair_"+token)}); err != nil {
		t.Fatalf("invite start failed: %v", err)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallbackFrom(2002, "partner", "en", "pair:accept:"+token)}); err != nil {
		t.Fatalf("accept callback failed: %v", err)
	}
	if len(bot.edits) == 0 {
		t.Fatal("accept callback did not edit the accepter message")
	}

	pair, err := app.repo.ActivePairForUser(ctx, 1001)
	if err != nil {
		t.Fatalf("ActivePairForUser returned error: %v", err)
	}
	if pair == nil {
		t.Fatal("request acceptance did not create an active pair")
	}
	if pair.UserAID != 1001 || pair.UserBID != 2002 {
		t.Fatalf("pair users = %d,%d; want 1001,2002", pair.UserAID, pair.UserBID)
	}
}

func TestGameAnswerCallbackOnPhotoSetsFSMAndEditsCaption(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()
	pairUsersForGame(t, app)
	sessionID := startAndAcceptGame(t, app, bot)

	textEditsBefore := len(bot.edits)
	captionEditsBefore := len(bot.captionEdits)
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallback("game:answer:" + sessionID)}); err != nil {
		t.Fatalf("answer callback failed: %v", err)
	}

	if got := state.values[1001]; got != "game:await_answer:"+sessionID {
		t.Fatalf("fsm = %q, want game answer wait", got)
	}
	if got := len(bot.edits) - textEditsBefore; got != 0 {
		t.Fatalf("text edits = %d, want 0 for photo callback", got)
	}
	if got := len(bot.captionEdits) - captionEditsBefore; got != 1 {
		t.Fatalf("caption edits = %d, want 1", got)
	}
	got := bot.captionEdits[len(bot.captionEdits)-1].text
	if !strings.Contains(strings.ToLower(got), "answer") && !strings.Contains(got, "відповідь") {
		t.Fatalf("caption edit = %q, want answer prompt", got)
	}
}

func TestTypedGameAnswerIsSavedInsteadOfReturningMainMenu(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()
	pairUsersForGame(t, app)
	sessionID := startAndAcceptGame(t, app, bot)
	state.values[1001] = "game:await_answer:" + sessionID

	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("Тест")}); err != nil {
		t.Fatalf("typed answer failed: %v", err)
	}

	if _, ok := state.values[1001]; ok {
		t.Fatalf("fsm was not cleared: %q", state.values[1001])
	}
	if _, ok, _ := state.PendingGameCompletion(ctx, 1001); ok {
		t.Fatal("typed answer stored legacy pending completion")
	}
	got := bot.messages[before].text
	if strings.Contains(got, "Головне меню") || strings.Contains(got, "Main menu") {
		t.Fatalf("typed answer returned to main menu: %q", got)
	}
	if !strings.Contains(got, "partner") && !strings.Contains(got, "партнер") {
		t.Fatalf("typed answer response = %q, want waiting-for-partner acknowledgement", got)
	}
}

func TestEmptyGameAnswerKeepsFSMAndReprompts(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()
	pairUsersForGame(t, app)
	sessionID := startAndAcceptGame(t, app, bot)
	state.values[1001] = "game:await_answer:" + sessionID

	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("   ")}); err != nil {
		t.Fatalf("empty answer failed: %v", err)
	}

	if got := state.values[1001]; got != "game:await_answer:"+sessionID {
		t.Fatalf("fsm = %q, want answer wait to remain", got)
	}
	if _, ok, _ := state.PendingGameCompletion(ctx, 1001); ok {
		t.Fatal("empty answer stored a completion")
	}
	got := bot.messages[before].text
	if !strings.Contains(strings.ToLower(got), "empty") && !strings.Contains(got, "порож") {
		t.Fatalf("empty answer response = %q, want empty-answer prompt", got)
	}
}

func TestGameSkipAndInPersonClearFSMAndRecordCompletion(t *testing.T) {
	cases := map[string]string{
		"game:skip:":      "skip",
		"game:in_person:": "in_person",
	}
	for data := range cases {
		app, bot, state := newTestApp(t)
		ctx := context.Background()
		pairUsersForGame(t, app)
		sessionID := startAndAcceptGame(t, app, bot)
		state.values[1001] = "game:await_answer:" + sessionID

		if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallback(data + sessionID)}); err != nil {
			t.Fatalf("%s callback failed: %v", data, err)
		}

		if _, ok := state.values[1001]; ok {
			t.Fatalf("%s did not clear answer fsm", data)
		}
		if _, ok, _ := state.PendingGameCompletion(ctx, 1001); ok {
			t.Fatalf("%s stored legacy pending completion", data)
		}
		if len(bot.captionEdits) != 1 {
			t.Fatalf("%s caption edits = %d, want 1", data, len(bot.captionEdits))
		}
	}
}

func TestGamePauseClearsFSMWithoutCompletion(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()
	pairUsersForGame(t, app)
	sessionID := startAndAcceptGame(t, app, bot)
	state.values[1001] = "game:await_answer:" + sessionID

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallback("game:pause:" + sessionID)}); err != nil {
		t.Fatalf("pause callback failed: %v", err)
	}

	if _, ok := state.values[1001]; ok {
		t.Fatal("pause did not clear answer fsm")
	}
	if _, ok, _ := state.PendingGameCompletion(ctx, 1001); ok {
		t.Fatal("pause stored a completion")
	}
	if len(bot.captionEdits) != 1 {
		t.Fatalf("caption edits = %d, want 1", len(bot.captionEdits))
	}
}

func TestMenuDuringGameAnswerClearsFSMWithoutCompletion(t *testing.T) {
	app, _, state := newTestApp(t)
	ctx := context.Background()
	pairUsersForGame(t, app)
	state.values[1001] = "game:await_answer:1"

	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("Меню")}); err != nil {
		t.Fatalf("menu message failed: %v", err)
	}

	if _, ok := state.values[1001]; ok {
		t.Fatal("menu did not clear answer fsm")
	}
	if _, ok, _ := state.PendingGameCompletion(ctx, 1001); ok {
		t.Fatal("menu stored a completion")
	}
}

func TestOldUnscopedGameAnswerCallbackFallsBackToCurrentCard(t *testing.T) {
	app, _, state := newTestApp(t)
	ctx := context.Background()
	pairUsersForGame(t, app)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallback("game:answer")}); err != nil {
		t.Fatalf("old answer callback failed: %v", err)
	}

	if got := state.values[1001]; got != "" {
		t.Fatalf("fsm = %q, want stale callback to leave FSM empty", got)
	}
}

func TestGamePhotoEditFailureFallsBackToSingleMessage(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)
	bot.failCaptionEdits = true

	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallback("game:answer:q001")}); err != nil {
		t.Fatalf("answer callback failed: %v", err)
	}

	if got := len(bot.messages) - before; got != 1 {
		t.Fatalf("fallback messages = %d, want 1", got)
	}
	if len(bot.captionEdits) != 0 {
		t.Fatalf("caption edits recorded after failure = %d, want 0", len(bot.captionEdits))
	}
}

type fakeObjectStore struct {
	objects map[string][]byte
}

func newFakeObjectStore() *fakeObjectStore {
	return &fakeObjectStore{objects: map[string][]byte{}}
}

func (f *fakeObjectStore) Put(ctx context.Context, objectKey, contentType string, data []byte) error {
	f.objects[objectKey] = data
	return nil
}

func (f *fakeObjectStore) Get(ctx context.Context, objectKey string) ([]byte, error) {
	d, ok := f.objects[objectKey]
	if !ok {
		return nil, errors.New("not found")
	}
	return d, nil
}

func (f *fakeObjectStore) Delete(ctx context.Context, objectKey string) error {
	delete(f.objects, objectKey)
	return nil
}

func newTestApp(t *testing.T) (*App, *fakeBot, *fakeState) {
	t.Helper()
	db, err := storage.OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	bundle := i18n.NewBundle()
	bundle.Add(i18n.Catalog{Language: "uk", Brand: "між нами.", Strings: map[string]string{
		"menu.title":                       "Головне меню",
		"menu.header":                      "між нами.",
		"menu.profile_fallback":            "Ти",
		"menu.partner_fallback":            "Партнер",
		"menu.status_paired":               "У парі з %s",
		"menu.status_unpaired":             "Партнера поки немає",
		"menu.status_unpaired_hint":        "Натисни «Пара» щоб запросити.",
		"menu.progress":                    "Рівень %d · %d/%d карток",
		"menu.level":                       "Рівень %d",
		"menu.prompt":                      "Що хочеш зробити?",
		"onboarding.language":              "Оберіть мову.",
		"onboarding.name":                  "Як тебя називати?",
		"onboarding.gender":                "Обери гендер.",
		"onboarding.adult":                 "Підтверди, чи тобі є 18+.",
		"onboarding.mature":                "Показувати 18+ картки?",
		"onboarding.theme_color":           "Обери колір карток.",
		"menu.reset_complete":              "Скинуто. Головне меню",
		"settings.change_language":         "Змінити мову",
		"settings.language_saved":          "Language saved.",
		"settings.delete_account":          "Видалити акаунт",
		"settings.delete_confirm_prompt":   "Це видалить усі дані.",
		"settings.delete_confirm_button":   "Так, видалити все",
		"settings.deleted":                 "Акаунт видалено.",
		"pair.active":                      "Активна пара: %s та %s",
		"pair.instructions":                "Пара\n\nНадішли партнера.",
		"pair.request_sent":                "Запит створено: %s",
		"pair.invite_created":              "Запит створено: %s",
		"pair.incoming":                    "%s запрошує тебе.",
		"pair.accept_prompt":               "Прийняти запит?",
		"pair.accepted":                    "Пару створено.",
		"pair.accepted_with_id":            "Пару створено #%d.",
		"pair.open_first":                  "Спочатку відкрий Пара.",
		"pair.invalid_identifier":          "Надішли username або ID.",
		"pair.self_error":                  "Не можна із собою.",
		"pair.already_paired":              "Уже є пара.",
		"pair.not_found":                   "Запит недоступний.",
		"pair.error":                       "Помилка пари.",
		"game.no_cards":                    "Картки недоступні.",
		"game.not_ready":                   "Пара або колода ще не готові.",
		"game.answer_prompt":               "Напиши відповідь одним повідомленням.",
		"game.answer_empty":                "Відповідь не може бути порожньою.",
		"game.answer_saved_demo":           "Відповідь збережено.",
		"game.skipped_demo":                "Картку пропущено.",
		"game.in_person_demo":              "Позначено як відповіли наживо.",
		"game.paused":                      "Гру поставлено на паузу.",
		"game.card_stale":                  "Кнопка застаріла.",
		"game.invite_sent":                 "Запрошення на гру надіслано партнеру.",
		"game.invite_incoming":             "%s хоче почати гру.",
		"game.invite_declined":             "Запрошення відхилено.",
		"game.invite_expired":              "Запрошення застаріло.",
		"game.waiting_partner":             "Відповідь збережено. Чекаємо партнера.",
		"game.revealed":                    "Картку завершено.",
		"game.reveal_skipped":              "пропущено",
		"game.reveal_in_person":            "відповіли наживо",
		"game.reveal_empty":                "без відповіді",
		"payments.success":                 "Преміум відкрито.",
		"payments.support":                 "Підтримка платежів.",
		"game.requires_pair":               "Для гри потрібна активна пара.",
		"settings.test_cards":              "🧪 Тест карток",
		"game.admin_prev":                  "← Назад",
		"game.admin_next":                  "Далі →",
		"game.admin_test_caption":          "[ТЕСТ] Рівень %d · ID: %s",
		"payments.premium_title":           "між нами. Преміум",
		"payments.premium_desc":            "Довічний преміум-доступ, усі поточні косметичні покращення та відсутність пропозицій підтримки.",
		"settings.custom_questions":        "✍️ Мої питання",
		"custom_questions.menu_title":      "✍️ Власні питання",
		"custom_questions.menu_text":       "Тут ти можеш керувати власними питаннями, які з'являтимуться під час вашої гри.\n\nПитання у грі:\n%s",
		"custom_questions.no_questions":    "Власних питань поки немає.",
		"custom_questions.add_button":      "➕ Додати питання",
		"custom_questions.delete_button":   "❌ Видалити",
		"custom_questions.enter_prompt":    "Напиши текст свого питання (до 200 символів):",
		"custom_questions.invalid_length":  "Текст питання занадто короткий або довгий. Спробуй ще раз (до 200 символів):",
		"custom_questions.success_added":   "🎉 Питання успішно додано!",
		"custom_questions.success_deleted": "❌ Питання видалено!",

		"theme.current_settings":  "Поточні налаштування теми:\n🎨 Колір: %s\n✨ Стиль: %s\n🔤 Шрифт: %s\n🖼 Фон: %s",
		"theme.style_locked":      "Стиль %s заблоковано.",
		"theme.font_locked":       "Шрифт %s заблоковано.",
		"theme.bg_locked":         "Фон %s заблоковано.",
		"theme.upload_prompt":     "Надішли фото для фону.",
		"theme.upload_failed":     "Помилка завантаження.",
		"theme.upload_limit":      "Досягнуто ліміту 3 фонів.",
		"theme.delete_bg_confirm": "Видалити цей фон?",
		"theme.upload_success":    "Фон успішно завантажено!",
	}})
	bundle.Add(i18n.Catalog{Language: "en", Brand: "WRNRS", Strings: map[string]string{
		"menu.title":                       "Main menu",
		"menu.header":                      "WRNRS",
		"menu.profile_fallback":            "You",
		"menu.partner_fallback":            "Partner",
		"menu.status_paired":               "Paired with %s",
		"menu.status_unpaired":             "No partner yet",
		"menu.status_unpaired_hint":        "Tap Pair to invite someone.",
		"menu.progress":                    "Level %d · %d/%d cards",
		"menu.level":                       "Level %d",
		"menu.prompt":                      "What would you like to do?",
		"onboarding.language":              "Choose your language.",
		"onboarding.name":                  "What should I call you?",
		"onboarding.gender":                "Choose your gender.",
		"onboarding.adult":                 "Confirm whether you are 18+.",
		"onboarding.mature":                "Show mature 18+ cards?",
		"onboarding.theme_color":           "Choose your card color.",
		"menu.reset_complete":              "Reset complete. Main menu",
		"settings.change_language":         "Change language",
		"settings.language_saved":          "Language saved.",
		"settings.delete_account":          "Delete account",
		"settings.delete_confirm_prompt":   "This will permanently delete all your data.",
		"settings.delete_confirm_button":   "Yes, delete everything",
		"settings.deleted":                 "Account deleted.",
		"pair.active":                      "Active pair: %s and %s",
		"pair.instructions":                "Pair\n\nSend partner.",
		"pair.request_sent":                "Request created: %s",
		"pair.invite_created":              "Request created: %s",
		"pair.incoming":                    "%s invited you.",
		"pair.accept_prompt":               "Accept request?",
		"pair.accepted":                    "Pair accepted.",
		"pair.accepted_with_id":            "Pair accepted #%d.",
		"pair.open_first":                  "Open Pair first.",
		"pair.invalid_identifier":          "Send username or ID.",
		"pair.self_error":                  "No self pair.",
		"pair.already_paired":              "Already paired.",
		"pair.not_found":                   "Request unavailable.",
		"pair.error":                       "Pair error.",
		"game.no_cards":                    "No cards available.",
		"game.not_ready":                   "Pair setup and card deck are not ready yet.",
		"game.answer_prompt":               "Write your answer in one message.",
		"game.answer_empty":                "The answer cannot be empty.",
		"game.answer_saved_demo":           "Answer saved.",
		"game.skipped_demo":                "Card skipped.",
		"game.in_person_demo":              "Marked as answered in person.",
		"game.paused":                      "Game paused.",
		"game.card_stale":                  "This button is stale.",
		"game.invite_sent":                 "Game invite sent to your partner.",
		"game.invite_incoming":             "%s wants to start the game.",
		"game.invite_declined":             "Game invite declined.",
		"game.invite_expired":              "Game invite expired.",
		"game.waiting_partner":             "Answer saved. Waiting for your partner.",
		"game.revealed":                    "Card complete.",
		"game.reveal_skipped":              "skipped",
		"game.reveal_in_person":            "answered in person",
		"game.reveal_empty":                "no answer",
		"payments.success":                 "Premium unlocked.",
		"payments.support":                 "Payment support.",
		"game.requires_pair":               "An active pair is required to play.",
		"settings.test_cards":              "🧪 Test cards",
		"game.admin_prev":                  "← Prev",
		"game.admin_next":                  "Next →",
		"game.admin_test_caption":          "[TEST] Level %d · ID: %s",
		"payments.premium_title":           "WRNRS Premium",
		"payments.premium_desc":            "Lifetime premium access, all current cosmetics, and no support prompts.",
		"settings.custom_questions":        "✍️ Custom Questions",
		"custom_questions.menu_title":      "✍️ Custom Questions",
		"custom_questions.menu_text":       "Here you can manage your custom questions that will appear in your game.\n\nYour questions in play:\n%s",
		"custom_questions.no_questions":    "No custom questions yet.",
		"custom_questions.add_button":      "➕ Add Question",
		"custom_questions.delete_button":   "❌ Delete Question",
		"custom_questions.enter_prompt":    "Send the text of your question (up to 200 characters):",
		"custom_questions.invalid_length":  "The question is too short or too long. Please try again (up to 200 characters):",
		"custom_questions.success_added":   "🎉 Question added successfully!",
		"custom_questions.success_deleted": "❌ Question deleted!",

		"theme.current_settings":  "Current Theme Settings:\n🎨 Color: %s\n✨ Style: %s\n🔤 Font: %s\n🖼 Background: %s",
		"theme.style_locked":      "Style %s is locked.",
		"theme.font_locked":       "Font %s is locked.",
		"theme.bg_locked":         "Background %s is locked.",
		"theme.upload_prompt":     "Send a photo for background.",
		"theme.upload_failed":     "Upload failed.",
		"theme.upload_limit":      "Limit of 3 backgrounds reached.",
		"theme.delete_bg_confirm": "Delete this background?",
		"theme.upload_success":    "Background uploaded successfully!",
	}})
	bot := &fakeBot{}
	state := newFakeState()
	store := newFakeObjectStore()
	return New(Options{
		Config: config.Config{
			BotUsername:         "wrnrs_bot",
			PhoneHashSecret:     "test-secret",
			AnswerEncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		},
		Bot:      bot,
		Repo:     storage.NewRepository(db),
		State:    state,
		I18N:     bundle,
		Deck:     &content.Deck{Version: 1, Cards: []content.Card{{ID: "q001", Level: 1, Text: map[string]string{"uk": "Питання", "en": "Question"}}}},
		Renderer: render.NewCardRenderer(render.CardRendererOptions{FontPath: "../../assets/fonts/Nunito/static/Nunito-Bold.ttf"}),
		Styles: &content.StyleCatalog{
			Version: 1,
			Styles: []content.Style{
				{
					ID:      "default_warm",
					Name:    map[string]string{"uk": "Теплий", "en": "Warm"},
					Premium: false,
					Tokens: content.StyleTokens{
						BorderRadius: 20,
						GlassOpacity: 0.3,
						DefaultColor: "#d98c9f",
					},
				},
				{
					ID:      "premium_velvet",
					Name:    map[string]string{"uk": "Вельвет", "en": "Velvet"},
					Premium: true,
					Tokens: content.StyleTokens{
						BorderRadius: 25,
						GlassOpacity: 0.45,
						DefaultColor: "#8f3f5f",
					},
				},
			},
		},
		Fonts: &content.FontCatalog{
			Version: 1,
			Fonts: []content.Font{
				{
					ID:      "nunito_bold",
					Name:    map[string]string{"uk": "Нуніто Болд", "en": "Nunito Bold"},
					Path:    "../../assets/fonts/Nunito/static/Nunito-Bold.ttf",
					Premium: false,
				},
				{
					ID:      "google_sans_regular",
					Name:    map[string]string{"uk": "Гугл Санс", "en": "Google Sans"},
					Path:    "../../assets/fonts/Nunito/static/Nunito-Bold.ttf",
					Premium: true,
				},
			},
		},
		Backgrounds: &content.BackgroundCatalog{
			Version: 1,
			Backgrounds: []content.Background{
				{
					ID:        "bg_blush",
					Kind:      "built_in",
					Premium:   false,
					Name:      map[string]string{"uk": "Румяна", "en": "Blush"},
					ObjectKey: "built-in/blush-gradient.webp",
				},
				{
					ID:        "bg_candle",
					Kind:      "built_in",
					Premium:   true,
					Name:      map[string]string{"uk": "Свічка", "en": "Candle"},
					ObjectKey: "built-in/candle-glow.webp",
				},
			},
		},
		ObjectStore: store,
	}), bot, state
}

func testMessage(text string) *telegram.Message {
	return testMessageFrom(1001, "tester", "en", text)
}

func testMessageFrom(userID int64, username, language, text string) *telegram.Message {
	return &telegram.Message{
		MessageID: 1,
		From:      &telegram.User{ID: userID, FirstName: "Test", Username: username, LanguageCode: language},
		Chat:      telegram.Chat{ID: userID, Type: "private"},
		Text:      text,
	}
}

func testContactMessage(contactUserID int64, phone, firstName string) *telegram.Message {
	msg := testMessage("")
	msg.Contact = &telegram.Contact{
		PhoneNumber: phone,
		FirstName:   firstName,
		UserID:      contactUserID,
	}
	return msg
}

func testSuccessfulPaymentMessage(userID int64, payload, chargeID string) *telegram.Message {
	msg := testMessageFrom(userID, "tester", "en", "")
	msg.SuccessfulPayment = &telegram.SuccessfulPayment{
		Currency:                "XTR",
		TotalAmount:             250,
		InvoicePayload:          payload,
		TelegramPaymentChargeID: chargeID,
		ProviderPaymentChargeID: "provider-" + chargeID,
	}
	return msg
}

func pairingIdentifierTelegramID(userID int64) pairing.Identifier {
	return pairing.Identifier{TelegramID: userID}
}

func pairUsersForGame(t *testing.T, app *App) *storage.Pair {
	t.Helper()
	ctx := context.Background()
	for _, user := range []storage.User{
		{TelegramID: 1001, Username: "tester", DisplayName: "Alice", Language: "en", Is18Plus: true, MatureOptIn: true, SelectedStyleID: "default_warm", ThemeBaseColor: "#d98c9f"},
		{TelegramID: 2002, Username: "partner", DisplayName: "Bob", Language: "en", Is18Plus: true, MatureOptIn: true, SelectedStyleID: "default_warm", ThemeBaseColor: "#d98c9f"},
	} {
		if err := app.repo.UpsertUser(ctx, user); err != nil {
			t.Fatalf("UpsertUser(%d) returned error: %v", user.TelegramID, err)
		}
	}
	request, err := app.repo.CreatePairRequest(ctx, storage.PairRequest{
		RequesterID: 1001,
		InviteToken: "game-pair-token",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePairRequest returned error: %v", err)
	}
	pair, err := app.repo.AcceptPairRequest(ctx, request.InviteToken, 2002)
	if err != nil {
		t.Fatalf("AcceptPairRequest returned error: %v", err)
	}
	return pair
}

func startAndAcceptGame(t *testing.T, app *App, bot *fakeBot) string {
	t.Helper()
	ctx := context.Background()
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("game:start")}); err != nil {
		t.Fatalf("game start callback failed: %v", err)
	}
	sessionID := extractCallbackSuffix(t, lastMessageTo(t, bot, 2002).markup, "game:accept:")
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallbackFrom(2002, "partner", "en", "game:accept:"+sessionID)}); err != nil {
		t.Fatalf("game accept callback failed: %v", err)
	}
	return sessionID
}

func ensureKnownTargetUser(t *testing.T, app *App, userID int64, username string) {
	t.Helper()
	if err := app.repo.UpsertUser(context.Background(), storage.User{
		TelegramID:      userID,
		Username:        username,
		DisplayName:     "Target",
		Language:        "en",
		SelectedStyleID: "default_warm",
		ThemeBaseColor:  "#d98c9f",
	}); err != nil {
		t.Fatalf("UpsertUser target returned error: %v", err)
	}
}

func testCallback(data string) *telegram.CallbackQuery {
	return testCallbackFrom(1001, "tester", "en", data)
}

func testCallbackFrom(userID int64, username, language, data string) *telegram.CallbackQuery {
	return &telegram.CallbackQuery{
		ID:   "cb",
		From: telegram.User{ID: userID, FirstName: "Test", Username: username, LanguageCode: language},
		Message: &telegram.Message{
			MessageID: 1,
			Chat:      telegram.Chat{ID: userID, Type: "private"},
		},
		Data: data,
	}
}

func testPhotoCallback(data string) *telegram.CallbackQuery {
	return testPhotoCallbackFrom(1001, "tester", "en", data)
}

func testPhotoCallbackFrom(userID int64, username, language, data string) *telegram.CallbackQuery {
	cb := testCallbackFrom(userID, username, language, data)
	cb.Message.Photo = []telegram.PhotoSize{{FileID: "file-1", Width: 1200, Height: 800}}
	return cb
}

func completeUkrainianOnboarding(t *testing.T, app *App) {
	t.Helper()
	ctx := context.Background()
	for _, update := range []telegram.Update{
		{Message: testMessage("/start")},
		{CallbackQuery: testCallback("onboarding:language:uk")},
		{Message: testMessage("Сєва")},
		{CallbackQuery: testCallback("onboarding:gender:male")},
		{CallbackQuery: testCallback("onboarding:adult:no")},
		{CallbackQuery: testCallback("theme:color:#8da68f")},
		{CallbackQuery: testCallback("onboarding:bg:skip")},
	} {
		if err := app.HandleUpdate(ctx, update); err != nil {
			t.Fatalf("onboarding update failed: %v", err)
		}
	}
}

func extractToken(t *testing.T, text string) string {
	t.Helper()
	const marker = "pair_"
	idx := strings.Index(text, marker)
	if idx < 0 {
		t.Fatalf("message %q did not contain invite token", text)
	}
	token := text[idx+len(marker):]
	for i, r := range token {
		if r == ' ' || r == '\n' || r == ')' {
			return token[:i]
		}
	}
	return token
}

func inlineKeyboardHasCallback(markup telegram.InlineKeyboardMarkup, callbackData string) bool {
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == callbackData {
				return true
			}
		}
	}
	return false
}

func inlineKeyboardHasCallbackPrefix(markup telegram.InlineKeyboardMarkup, prefix string) bool {
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			if strings.HasPrefix(button.CallbackData, prefix) {
				return true
			}
		}
	}
	return false
}

func extractCallbackSuffix(t *testing.T, markup any, prefix string) string {
	t.Helper()
	inline, ok := markup.(telegram.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("markup = %T, want inline keyboard", markup)
	}
	for _, row := range inline.InlineKeyboard {
		for _, button := range row {
			if strings.HasPrefix(button.CallbackData, prefix) {
				return strings.TrimPrefix(button.CallbackData, prefix)
			}
		}
	}
	t.Fatalf("keyboard %#v did not contain callback prefix %q", inline, prefix)
	return ""
}

func lastMessageTo(t *testing.T, bot *fakeBot, chatID int64) sentMessage {
	t.Helper()
	for i := len(bot.messages) - 1; i >= 0; i-- {
		if bot.messages[i].chatID == chatID {
			return bot.messages[i]
		}
	}
	t.Fatalf("no message sent to chat %d; messages=%#v", chatID, bot.messages)
	return sentMessage{}
}

func sentPhotoTo(bot *fakeBot, chatID int64) bool {
	for _, photo := range bot.sentPhotos {
		if photo.chatID == chatID {
			return true
		}
	}
	return false
}

func newTestAppWithOptions(t *testing.T, bot *fakeBot, adminIDs []int64) *App {
	t.Helper()
	db, err := storage.OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	bundle := i18n.NewBundle()
	bundle.Add(i18n.Catalog{Language: "uk", Brand: "між нами.", Strings: map[string]string{
		"menu.title":                       "Головне меню",
		"menu.header":                      "між нами.",
		"menu.profile_fallback":            "Ти",
		"menu.partner_fallback":            "Партнер",
		"menu.status_paired":               "У парі з %s",
		"menu.status_unpaired":             "Партнера поки немає",
		"menu.status_unpaired_hint":        "Натисни «Пара» щоб запросити.",
		"menu.progress":                    "Рівень %d · %d/%d карток",
		"menu.level":                       "Рівень %d",
		"menu.prompt":                      "Що хочеш зробити?",
		"onboarding.language":              "Оберіть мову.",
		"onboarding.name":                  "Як тебе називати?",
		"onboarding.gender":                "Обери гендер.",
		"onboarding.adult":                 "Підтверди, чи тобі є 18+.",
		"onboarding.mature":                "Показувати 18+ картки?",
		"onboarding.theme_color":           "Обери колір карток.",
		"menu.reset_complete":              "Скинуто. Головне меню",
		"settings.change_language":         "Змінити мову",
		"settings.language_saved":          "Language saved.",
		"settings.delete_account":          "Видалити акаунт",
		"settings.delete_confirm_prompt":   "Це видалить усі дані.",
		"settings.delete_confirm_button":   "Так, видалити все",
		"settings.deleted":                 "Акаунт видалено.",
		"pair.active":                      "Активна пара: %s та %s",
		"pair.instructions":                "Пара\n\nНадішли партнера.",
		"pair.request_sent":                "Запит створено: %s",
		"pair.invite_created":              "Запит створено: %s",
		"pair.incoming":                    "%s запрошує тебе.",
		"pair.accept_prompt":               "Прийняти запит?",
		"pair.accepted":                    "Пару створено.",
		"pair.accepted_with_id":            "Пару створено #%d.",
		"pair.open_first":                  "Спочатку відкрий Пара.",
		"pair.invalid_identifier":          "Надішли username або ID.",
		"pair.self_error":                  "Не можна із собою.",
		"pair.already_paired":              "Уже є пара.",
		"pair.not_found":                   "Запит недоступний.",
		"pair.error":                       "Помилка пари.",
		"game.no_cards":                    "Картки недоступні.",
		"game.not_ready":                   "Пара або колода ще не готові.",
		"game.answer_prompt":               "Напиши відповідь одним повідомленням.",
		"game.answer_empty":                "Відповідь не може бути порожньою.",
		"game.answer_saved_demo":           "Відповідь збережено.",
		"game.skipped_demo":                "Картку пропущено.",
		"game.in_person_demo":              "Позначено як відповіли наживо.",
		"game.paused":                      "Гру поставлено на паузу.",
		"game.card_stale":                  "Кнопка застаріла.",
		"game.invite_sent":                 "Запрошення на гру надіслано партнеру.",
		"game.invite_incoming":             "%s хоче почати гру.",
		"game.invite_declined":             "Запрошення відхилено.",
		"game.invite_expired":              "Запрошення застаріло.",
		"game.waiting_partner":             "Відповідь збережено. Чекаємо партнера.",
		"game.revealed":                    "Картку завершено.",
		"game.reveal_skipped":              "пропущено",
		"game.reveal_in_person":            "відповіли наживо",
		"game.reveal_empty":                "без відповіді",
		"payments.success":                 "Преміум відкрито.",
		"payments.support":                 "Підтримка платежів.",
		"game.requires_pair":               "Для гри потрібна активна пара.",
		"settings.test_cards":              "🧪 Тест карток",
		"game.admin_prev":                  "← Назад",
		"game.admin_next":                  "Далі →",
		"game.admin_test_caption":          "[ТЕСТ] Рівень %d · ID: %s",
		"payments.premium_title":           "між нами. Преміум",
		"payments.premium_desc":            "Довічний преміум-доступ, усі поточні косметичні покращення та відсутність пропозицій підтримки.",
		"settings.custom_questions":        "✍️ Мої питання",
		"custom_questions.menu_title":      "✍️ Власні питання",
		"custom_questions.menu_text":       "Тут ти можеш керувати власними питаннями, які з'являтимуться під час вашої гри.\n\nПитання у грі:\n%s",
		"custom_questions.no_questions":    "Власних питань поки немає.",
		"custom_questions.add_button":      "➕ Додати питання",
		"custom_questions.delete_button":   "❌ Видалити",
		"custom_questions.enter_prompt":    "Напиши текст свого питання (до 200 символів):",
		"custom_questions.invalid_length":  "Текст питання занадто короткий або довгий. Спробуй ще раз (до 200 символів):",
		"custom_questions.success_added":   "🎉 Питання успішно додано!",
		"custom_questions.success_deleted": "❌ Питання видалено!",
	}})
	bundle.Add(i18n.Catalog{Language: "en", Brand: "WRNRS", Strings: map[string]string{
		"menu.title":                       "Main menu",
		"menu.header":                      "WRNRS",
		"menu.profile_fallback":            "You",
		"menu.partner_fallback":            "Partner",
		"menu.status_paired":               "Paired with %s",
		"menu.status_unpaired":             "No partner yet",
		"menu.status_unpaired_hint":        "Tap Pair to invite someone.",
		"menu.progress":                    "Level %d · %d/%d cards",
		"menu.level":                       "Level %d",
		"menu.prompt":                      "What would you like to do?",
		"onboarding.language":              "Choose your language.",
		"onboarding.name":                  "What should I call you?",
		"onboarding.gender":                "Choose your gender.",
		"onboarding.adult":                 "Confirm whether you are 18+.",
		"onboarding.mature":                "Show mature 18+ cards?",
		"onboarding.theme_color":           "Choose your card color.",
		"menu.reset_complete":              "Reset complete. Main menu",
		"settings.change_language":         "Change language",
		"settings.language_saved":          "Language saved.",
		"settings.delete_account":          "Delete account",
		"settings.delete_confirm_prompt":   "This will permanently delete all your data.",
		"settings.delete_confirm_button":   "Yes, delete everything",
		"settings.deleted":                 "Account deleted.",
		"pair.active":                      "Active pair: %s and %s",
		"pair.instructions":                "Pair\n\nSend partner.",
		"pair.request_sent":                "Request created: %s",
		"pair.invite_created":              "Request created: %s",
		"pair.incoming":                    "%s invited you.",
		"pair.accept_prompt":               "Accept request?",
		"pair.accepted":                    "Pair accepted.",
		"pair.accepted_with_id":            "Pair accepted #%d.",
		"pair.open_first":                  "Open Pair first.",
		"pair.invalid_identifier":          "Send username or ID.",
		"pair.self_error":                  "No self pair.",
		"pair.already_paired":              "Already paired.",
		"pair.not_found":                   "Request unavailable.",
		"pair.error":                       "Pair error.",
		"game.no_cards":                    "No cards available.",
		"game.not_ready":                   "Pair setup and card deck are not ready yet.",
		"game.answer_prompt":               "Write your answer in one message.",
		"game.answer_empty":                "The answer cannot be empty.",
		"game.answer_saved_demo":           "Answer saved.",
		"game.skipped_demo":                "Card skipped.",
		"game.in_person_demo":              "Marked as answered in person.",
		"game.paused":                      "Game paused.",
		"game.card_stale":                  "This button is stale.",
		"game.invite_sent":                 "Game invite sent to your partner.",
		"game.invite_incoming":             "%s wants to start the game.",
		"game.invite_declined":             "Game invite declined.",
		"game.invite_expired":              "Game invite expired.",
		"game.waiting_partner":             "Answer saved. Waiting for your partner.",
		"game.revealed":                    "Card complete.",
		"game.reveal_skipped":              "skipped",
		"game.reveal_in_person":            "answered in person",
		"game.reveal_empty":                "no answer",
		"payments.success":                 "Premium unlocked.",
		"payments.support":                 "Payment support.",
		"game.requires_pair":               "An active pair is required to play.",
		"settings.test_cards":              "🧪 Test cards",
		"game.admin_prev":                  "← Prev",
		"game.admin_next":                  "Next →",
		"game.admin_test_caption":          "[TEST] Level %d · ID: %s",
		"payments.premium_title":           "WRNRS Premium",
		"payments.premium_desc":            "Lifetime premium access, all current cosmetics, and no support prompts.",
		"settings.custom_questions":        "✍️ Custom Questions",
		"custom_questions.menu_title":      "✍️ Custom Questions",
		"custom_questions.menu_text":       "Here you can manage your custom questions that will appear in your game.\n\nYour questions in play:\n%s",
		"custom_questions.no_questions":    "No custom questions yet.",
		"custom_questions.add_button":      "➕ Add Question",
		"custom_questions.delete_button":   "❌ Delete Question",
		"custom_questions.enter_prompt":    "Send the text of your question (up to 200 characters):",
		"custom_questions.invalid_length":  "The question is too short or too long. Please try again (up to 200 characters):",
		"custom_questions.success_added":   "🎉 Question added successfully!",
		"custom_questions.success_deleted": "❌ Question deleted!",
	}})
	state := newFakeState()
	return New(Options{
		Config: config.Config{
			BotUsername:         "wrnrs_bot",
			PhoneHashSecret:     "test-secret",
			AdminTelegramIDs:    adminIDs,
			AnswerEncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		},
		Bot:      bot,
		Repo:     storage.NewRepository(db),
		State:    state,
		I18N:     bundle,
		Deck:     &content.Deck{Version: 1, Cards: []content.Card{{ID: "q001", Level: 1, Text: map[string]string{"uk": "Питання", "en": "Question"}}}},
		Renderer: render.NewCardRenderer(render.CardRendererOptions{FontPath: "../../assets/fonts/Nunito/static/Nunito-Bold.ttf"}),
	})
}

func TestStartWithoutPairShowsRequiresPairMessage(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)

	completeUkrainianOnboarding(t, app)

	beforeEdits := len(bot.edits)

	cb := testCallback("game:start")
	err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: cb})
	if err != nil {
		t.Fatalf("handle callback: %v", err)
	}

	if len(bot.edits) <= beforeEdits {
		t.Fatalf("expected new edit message, got none")
	}
	edit := bot.edits[len(bot.edits)-1]
	if !strings.Contains(edit.text, "Для гри потрібна активна пара") {
		t.Errorf("unexpected message text: %q", edit.text)
	}
}

func TestGameStartSendsPartnerInviteWithoutSendingCard(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	pairUsersForGame(t, app)

	beforePhotos := bot.photos
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("game:start")}); err != nil {
		t.Fatalf("game start callback failed: %v", err)
	}

	if bot.photos != beforePhotos {
		t.Fatalf("game start sent %d photos before partner accepted", bot.photos-beforePhotos)
	}
	invite := lastMessageTo(t, bot, 2002)
	markup, ok := invite.markup.(telegram.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("partner invite markup = %T, want inline keyboard", invite.markup)
	}
	if !inlineKeyboardHasCallbackPrefix(markup, "game:accept:") || !inlineKeyboardHasCallbackPrefix(markup, "game:decline:") {
		t.Fatalf("partner invite keyboard = %#v, want accept/decline callbacks", markup)
	}
}

func TestGameAcceptSendsCurrentCardToBothPartners(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	pairUsersForGame(t, app)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("game:start")}); err != nil {
		t.Fatalf("game start callback failed: %v", err)
	}
	sessionID := extractCallbackSuffix(t, lastMessageTo(t, bot, 2002).markup, "game:accept:")

	beforePhotos := bot.photos
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallbackFrom(2002, "partner", "en", "game:accept:"+sessionID)}); err != nil {
		t.Fatalf("game accept callback failed: %v", err)
	}

	if bot.photos != beforePhotos+2 {
		t.Fatalf("accept sent %d photos, want 2", bot.photos-beforePhotos)
	}
	if !sentPhotoTo(bot, 1001) || !sentPhotoTo(bot, 2002) {
		t.Fatalf("sent photos = %#v, want one card for each partner", bot.sentPhotos)
	}
	for _, photo := range bot.sentPhotos[len(bot.sentPhotos)-2:] {
		markup, ok := photo.markup.(telegram.InlineKeyboardMarkup)
		if !ok {
			t.Fatalf("card markup = %T, want inline keyboard", photo.markup)
		}
		if !inlineKeyboardHasCallback(markup, "game:answer:"+sessionID) {
			t.Fatalf("card keyboard = %#v, want session-scoped answer callback", markup)
		}
	}
}

func TestGameRevealWaitsForBothPartnerCompletions(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	pair := pairUsersForGame(t, app)
	sessionID := startAndAcceptGame(t, app, bot)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallbackFrom(1001, "tester", "en", "game:answer:"+sessionID)}); err != nil {
		t.Fatalf("answer callback failed: %v", err)
	}
	if got := app.state.(*fakeState).values[1001]; got != "game:await_answer:"+sessionID {
		t.Fatalf("answer FSM = %q, want session-scoped wait", got)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("private answer")}); err != nil {
		t.Fatalf("typed answer failed: %v", err)
	}
	if count, err := app.repo.PairCardCount(ctx, pair.ID, 1); err != nil {
		t.Fatalf("PairCardCount after first answer returned error: %v", err)
	} else if count != 0 {
		t.Fatalf("history count after one answer = %d, want 0", count)
	}

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallbackFrom(2002, "partner", "en", "game:skip:"+sessionID)}); err != nil {
		t.Fatalf("partner skip callback failed: %v", err)
	}
	if count, err := app.repo.PairCardCount(ctx, pair.ID, 1); err != nil {
		t.Fatalf("PairCardCount after reveal returned error: %v", err)
	} else if count != 1 {
		t.Fatalf("history count after both complete = %d, want 1", count)
	}
}

func TestJournalShowsRevealedAnswersForActivePair(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	pairUsersForGame(t, app)
	sessionID := startAndAcceptGame(t, app, bot)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallbackFrom(1001, "tester", "en", "game:answer:"+sessionID)}); err != nil {
		t.Fatalf("answer callback failed: %v", err)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("I liked the morning walk.")}); err != nil {
		t.Fatalf("typed answer failed: %v", err)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallbackFrom(2002, "partner", "en", "game:skip:"+sessionID)}); err != nil {
		t.Fatalf("partner skip callback failed: %v", err)
	}

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("journal:open")}); err != nil {
		t.Fatalf("journal callback failed: %v", err)
	}
	last := bot.edits[len(bot.edits)-1]
	if !strings.Contains(last.text, "Question") || !strings.Contains(last.text, "I liked the morning walk.") || !strings.Contains(last.text, "skipped") {
		t.Fatalf("journal text = %q", last.text)
	}
}

func TestRevealSendsSupportPromptToBothPartnersWhenDue(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	app.cfg.Donation.MonobankURL = "https://send.monobank.ua/jar/test"
	app.cfg.Donation.CardNumber = "4441111122223333"
	pair := pairUsersForGame(t, app)
	sessionID := startAndAcceptGame(t, app, bot)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallbackFrom(1001, "tester", "en", "game:skip:"+sessionID)}); err != nil {
		t.Fatalf("first skip callback failed: %v", err)
	}
	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallbackFrom(2002, "partner", "en", "game:skip:"+sessionID)}); err != nil {
		t.Fatalf("partner skip callback failed: %v", err)
	}

	if got := len(bot.messages) - before; got != 4 {
		t.Fatalf("new messages after reveal = %d, want 4", got)
	}
	newMessages := bot.messages[before:]
	if !strings.Contains(newMessages[0].text, "Support the author here") || !strings.Contains(newMessages[1].text, "Support the author here") {
		t.Fatalf("support prompts not sent first: %#v", newMessages[:2])
	}
	if !strings.Contains(newMessages[2].text, "Card complete.") || !strings.Contains(newMessages[3].text, "Card complete.") {
		t.Fatalf("reveal messages not sent after support prompts: %#v", newMessages[2:])
	}
	if last, err := app.repo.LastSupportPromptAt(ctx, pair.ID); err != nil {
		t.Fatalf("LastSupportPromptAt returned error: %v", err)
	} else if last == nil {
		t.Fatal("support prompt timestamp was not stored")
	}
}

func TestRevealSuppressesSupportPromptWhenEitherPartnerPremium(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	app.cfg.Donation.MonobankURL = "https://send.monobank.ua/jar/test"
	app.cfg.Donation.CardNumber = "4441111122223333"
	pair := pairUsersForGame(t, app)
	if err := app.repo.GrantEntitlement(ctx, storage.Entitlement{
		UserID:   2002,
		Type:     storage.EntitlementPremiumAccess,
		UnlockID: storage.EntitlementPremiumAccess,
	}); err != nil {
		t.Fatalf("GrantEntitlement returned error: %v", err)
	}
	sessionID := startAndAcceptGame(t, app, bot)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallbackFrom(1001, "tester", "en", "game:skip:"+sessionID)}); err != nil {
		t.Fatalf("first skip callback failed: %v", err)
	}
	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallbackFrom(2002, "partner", "en", "game:skip:"+sessionID)}); err != nil {
		t.Fatalf("partner skip callback failed: %v", err)
	}

	if got := len(bot.messages) - before; got != 2 {
		t.Fatalf("new messages after premium reveal = %d, want 2", got)
	}
	for _, msg := range bot.messages[before:] {
		if strings.Contains(msg.text, "Support the author here") {
			t.Fatalf("premium reveal included support prompt: %#v", bot.messages[before:])
		}
	}
	if last, err := app.repo.LastSupportPromptAt(ctx, pair.ID); err != nil {
		t.Fatalf("LastSupportPromptAt returned error: %v", err)
	} else if last != nil {
		t.Fatalf("premium reveal stored support prompt timestamp: %v", last)
	}
}

func TestRevealSuppressesSupportPromptWhenRecentlyPrompted(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	app.cfg.Donation.MonobankURL = "https://send.monobank.ua/jar/test"
	app.cfg.Donation.CardNumber = "4441111122223333"
	pair := pairUsersForGame(t, app)
	if err := app.repo.MarkSupportPrompted(ctx, pair.ID, time.Now().UTC(), 0); err != nil {
		t.Fatalf("MarkSupportPrompted returned error: %v", err)
	}
	sessionID := startAndAcceptGame(t, app, bot)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallbackFrom(1001, "tester", "en", "game:skip:"+sessionID)}); err != nil {
		t.Fatalf("first skip callback failed: %v", err)
	}
	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallbackFrom(2002, "partner", "en", "game:skip:"+sessionID)}); err != nil {
		t.Fatalf("partner skip callback failed: %v", err)
	}

	if got := len(bot.messages) - before; got != 2 {
		t.Fatalf("new messages after recent reveal = %d, want 2", got)
	}
	for _, msg := range bot.messages[before:] {
		if strings.Contains(msg.text, "Support the author here") {
			t.Fatalf("recent reveal included support prompt: %#v", bot.messages[before:])
		}
	}
}

func TestPairBreakEndsPairClearsSharedBackgroundAndCancelsGame(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	state := app.state.(*fakeState)
	pair := pairUsersForGame(t, app)

	_ = state.SetFSM(ctx, 1001, string(onboarding.StepBackground), 24*time.Hour)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testPhotoMessage(1001, "pair_bg")}); err != nil {
		t.Fatalf("background upload failed: %v", err)
	}
	profileA, err := app.repo.UserProfile(ctx, 1001)
	if err != nil {
		t.Fatalf("UserProfile A: %v", err)
	}
	assetID := profileA.SelectedBackgroundAssetID
	if assetID == "" {
		t.Fatal("uploaded background was not selected")
	}
	if shares, err := app.repo.PairThemeShares(ctx, pair.ID); err != nil {
		t.Fatalf("PairThemeShares returned error: %v", err)
	} else if len(shares) != 1 || shares[0].AssetID != assetID {
		t.Fatalf("pair shares = %#v, want uploaded asset %s", shares, assetID)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallbackFrom(2002, "partner", "en", "theme:bg:select:"+assetID)}); err != nil {
		t.Fatalf("partner select shared background failed: %v", err)
	}
	profileB, err := app.repo.UserProfile(ctx, 2002)
	if err != nil {
		t.Fatalf("UserProfile B: %v", err)
	}
	if profileB.SelectedBackgroundAssetID != assetID {
		t.Fatalf("partner selected background = %q, want %q", profileB.SelectedBackgroundAssetID, assetID)
	}

	sessionID := startAndAcceptGame(t, app, bot)
	beforePartnerMessages := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("pair:break_confirm")}); err != nil {
		t.Fatalf("pair break confirm failed: %v", err)
	}

	if active, err := app.repo.ActivePairForUser(ctx, 1001); err != nil {
		t.Fatalf("ActivePairForUser A returned error: %v", err)
	} else if active != nil {
		t.Fatalf("user A still has active pair: %#v", active)
	}
	if active, err := app.repo.ActivePairForUser(ctx, 2002); err != nil {
		t.Fatalf("ActivePairForUser B returned error: %v", err)
	} else if active != nil {
		t.Fatalf("user B still has active pair: %#v", active)
	}
	if shares, err := app.repo.PairThemeShares(ctx, pair.ID); err != nil {
		t.Fatalf("PairThemeShares after break returned error: %v", err)
	} else if len(shares) != 0 {
		t.Fatalf("pair shares after break = %#v, want none", shares)
	}
	sessionIDInt, err := parseInt64(sessionID)
	if err != nil {
		t.Fatalf("parse session id: %v", err)
	}
	session, err := app.repo.GameSession(ctx, sessionIDInt)
	if err != nil {
		t.Fatalf("GameSession returned error: %v", err)
	}
	if session.Status != storage.GameSessionCancelled {
		t.Fatalf("session status = %q, want cancelled", session.Status)
	}
	profileA, _ = app.repo.UserProfile(ctx, 1001)
	profileB, _ = app.repo.UserProfile(ctx, 2002)
	if profileA.SelectedBackgroundAssetID != "" || profileB.SelectedBackgroundAssetID != "" {
		t.Fatalf("selected backgrounds after break = %q/%q, want both empty", profileA.SelectedBackgroundAssetID, profileB.SelectedBackgroundAssetID)
	}
	if len(bot.messages) == beforePartnerMessages || !strings.Contains(lastMessageTo(t, bot, 2002).text, "Pair ended.") {
		t.Fatalf("partner was not notified, messages = %#v", bot.messages[beforePartnerMessages:])
	}
}

func TestAccountDeletionNotifiesRemainingPartnerAndEndsPair(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	pairUsersForGame(t, app)

	before := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("settings:delete_confirm")}); err != nil {
		t.Fatalf("delete account confirm failed: %v", err)
	}
	if active, err := app.repo.ActivePairForUser(ctx, 2002); err != nil {
		t.Fatalf("ActivePairForUser returned error: %v", err)
	} else if active != nil {
		t.Fatalf("remaining partner still has active pair: %#v", active)
	}
	if len(bot.messages) == before || !strings.Contains(lastMessageTo(t, bot, 2002).text, "Pair ended.") {
		t.Fatalf("remaining partner was not notified, messages = %#v", bot.messages[before:])
	}
}

func TestInlineModeOffDoesNotAnswerQueries(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)

	if err := app.HandleUpdate(ctx, telegram.Update{InlineQuery: &telegram.InlineQuery{
		ID:    "inline-off",
		From:  telegram.User{ID: 1001, FirstName: "Test", Username: "tester", LanguageCode: "en"},
		Query: "card",
	}}); err != nil {
		t.Fatalf("inline update failed: %v", err)
	}
	if len(bot.inlineAnswers) != 0 {
		t.Fatalf("inline answers = %#v, want none", bot.inlineAnswers)
	}
}

func TestInlineModeOnReturnsPersonalTextArticle(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	app.cfg.FeatureInlineMode = true

	if err := app.HandleUpdate(ctx, telegram.Update{InlineQuery: &telegram.InlineQuery{
		ID:    "inline-on",
		From:  telegram.User{ID: 1001, FirstName: "Test", Username: "tester", LanguageCode: "en"},
		Query: "card",
	}}); err != nil {
		t.Fatalf("inline update failed: %v", err)
	}
	if len(bot.inlineAnswers) != 1 {
		t.Fatalf("inline answers = %d, want 1", len(bot.inlineAnswers))
	}
	answer := bot.inlineAnswers[0]
	if answer.id != "inline-on" || !answer.isPersonal || answer.cacheTime != 0 {
		t.Fatalf("inline answer metadata = %#v", answer)
	}
	if len(answer.results) != 1 {
		t.Fatalf("inline result count = %d, want 1", len(answer.results))
	}
	article, ok := answer.results[0].(telegram.InlineQueryResultArticle)
	if !ok {
		t.Fatalf("inline result = %T, want article", answer.results[0])
	}
	if article.Type != "article" || article.InputMessageContent.MessageText != "Question" {
		t.Fatalf("inline article = %#v", article)
	}
}

func TestOldQuestionScopedGameCallbackIsStale(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	pairUsersForGame(t, app)

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallback("game:skip:q001")}); err != nil {
		t.Fatalf("old game callback returned error: %v", err)
	}
	if _, ok, _ := app.state.(*fakeState).PendingGameCompletion(ctx, 1001); ok {
		t.Fatal("old question-scoped callback stored a demo pending completion")
	}
}

func TestGameCallbackUsesPairLock(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)
	state := app.state.(*fakeState)
	pair := pairUsersForGame(t, app)
	sessionID := startAndAcceptGame(t, app, bot)
	state.pairLockCalls = nil

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallback("game:skip:" + sessionID)}); err != nil {
		t.Fatalf("game skip callback failed: %v", err)
	}
	if len(state.pairLockCalls) != 1 || state.pairLockCalls[0] != pair.ID {
		t.Fatalf("pair lock calls = %#v, want pair %d", state.pairLockCalls, pair.ID)
	}
}

func TestRateLimitsRejectUploadPairingInlineAndGameCallbacks(t *testing.T) {
	ctx := context.Background()

	t.Run("upload", func(t *testing.T) {
		app, bot, state := newTestApp(t)
		completeUkrainianOnboarding(t, app)
		state.blockedActions["upload"] = true
		_ = state.SetFSM(ctx, 1001, string(onboarding.StepBackground), 24*time.Hour)

		if err := app.HandleUpdate(ctx, telegram.Update{Message: testPhotoMessage(1001, "blocked_upload")}); err != nil {
			t.Fatalf("upload update failed: %v", err)
		}
		if got := bot.messages[len(bot.messages)-1].text; !strings.Contains(got, "Забагато") {
			t.Fatalf("upload rate response = %q", got)
		}
	})

	t.Run("pairing", func(t *testing.T) {
		app, bot, state := newTestApp(t)
		completeUkrainianOnboarding(t, app)
		state.blockedActions["pairing"] = true
		if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("pair:menu")}); err != nil {
			t.Fatalf("pair menu failed: %v", err)
		}
		if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("2002")}); err != nil {
			t.Fatalf("pairing message failed: %v", err)
		}
		if got := bot.messages[len(bot.messages)-1].text; !strings.Contains(got, "Забагато") {
			t.Fatalf("pairing rate response = %q", got)
		}
	})

	t.Run("inline", func(t *testing.T) {
		bot := &fakeBot{}
		app := newTestAppWithOptions(t, bot, nil)
		app.cfg.FeatureInlineMode = true
		app.state.(*fakeState).blockedActions["inline"] = true
		if err := app.HandleUpdate(ctx, telegram.Update{InlineQuery: &telegram.InlineQuery{
			ID:    "inline-limited",
			From:  telegram.User{ID: 1001, FirstName: "Test", Username: "tester", LanguageCode: "en"},
			Query: "card",
		}}); err != nil {
			t.Fatalf("inline update failed: %v", err)
		}
		if len(bot.inlineAnswers) != 1 || len(bot.inlineAnswers[0].results) != 0 {
			t.Fatalf("inline limited answers = %#v", bot.inlineAnswers)
		}
	})

	t.Run("game_callback", func(t *testing.T) {
		bot := &fakeBot{}
		app := newTestAppWithOptions(t, bot, nil)
		state := app.state.(*fakeState)
		pairUsersForGame(t, app)
		sessionID := startAndAcceptGame(t, app, bot)
		state.blockedActions["game_callback"] = true

		if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testPhotoCallback("game:skip:" + sessionID)}); err != nil {
			t.Fatalf("game callback failed: %v", err)
		}
		if len(bot.captionEdits) == 0 {
			t.Fatal("rate-limited game callback did not edit caption")
		}
		if got := bot.captionEdits[len(bot.captionEdits)-1].text; !strings.Contains(got, "Too many attempts") {
			t.Fatalf("game callback rate response = %q", got)
		}
	})
}

func TestAdminTestCardsFromSettings(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, []int64{1001})

	completeUkrainianOnboarding(t, app)

	cb := testCallback("settings:test_cards")
	err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: cb})
	if err != nil {
		t.Fatalf("handle callback: %v", err)
	}

	if bot.photos != 1 {
		t.Errorf("expected 1 photo sent, got %d", bot.photos)
	}
}

func TestAdminTestNextNavigatesToNextCard(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, []int64{1001})

	completeUkrainianOnboarding(t, app)

	cb := testPhotoCallback("game:admin_next:q001")
	err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: cb})
	if err != nil {
		t.Fatalf("handle callback: %v", err)
	}

	if len(bot.mediaEdits) != 1 {
		t.Fatalf("expected 1 EditMessageMedia, got %d", len(bot.mediaEdits))
	}

	edit := bot.mediaEdits[0]
	if !strings.Contains(edit.caption, "[ТЕСТ]") {
		t.Errorf("expected caption to contain [ТЕСТ], got %q", edit.caption)
	}
}

func TestAdminTestPrevNavigatesToPrevCard(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, []int64{1001})

	completeUkrainianOnboarding(t, app)

	cb := testPhotoCallback("game:admin_prev:q001")
	err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: cb})
	if err != nil {
		t.Fatalf("handle callback: %v", err)
	}

	if len(bot.mediaEdits) != 1 {
		t.Fatalf("expected 1 EditMessageMedia, got %d", len(bot.mediaEdits))
	}
}

func TestAdminMenuEnumeratesCosmeticCatalogActions(t *testing.T) {
	ctx := context.Background()
	app, bot, _ := newTestApp(t)
	app.admin = admin.NewService([]int64{1001})

	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("/admin")}); err != nil {
		t.Fatalf("admin command failed: %v", err)
	}
	if len(bot.messages) == 0 {
		t.Fatal("admin command sent no message")
	}
	markup, ok := bot.messages[len(bot.messages)-1].markup.(telegram.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("admin markup = %T, want inline keyboard", bot.messages[len(bot.messages)-1].markup)
	}
	for _, callback := range []string{
		"admin:grant:premium_access:premium_access",
		"admin:revoke:premium_access:premium_access",
		"admin:grant:style:premium_velvet",
		"admin:revoke:style:premium_velvet",
		"admin:grant:font:google_sans_regular",
		"admin:revoke:font:google_sans_regular",
		"admin:grant:background:bg_candle",
		"admin:revoke:background:bg_candle",
	} {
		if !inlineKeyboardHasCallback(markup, callback) {
			t.Fatalf("admin menu missing callback %q in %#v", callback, markup)
		}
	}
}

func TestNonAdminCannotTriggerTestCardsCallback(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, []int64{1002})

	completeUkrainianOnboarding(t, app)

	beforeMessages := len(bot.messages)
	beforeEdits := len(bot.edits)
	beforePhotos := bot.photos

	cb := testCallback("settings:test_cards")
	err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: cb})
	if err != nil {
		t.Fatalf("handle callback: %v", err)
	}

	if bot.photos != beforePhotos || len(bot.edits) != beforeEdits || len(bot.messages) != beforeMessages {
		t.Errorf("expected silent ignore, but bot received new actions")
	}
}

func TestStoreOpenSendsInvoiceWithTranslations(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)

	completeUkrainianOnboarding(t, app)

	cb := testCallback("store:open")
	err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: cb})
	if err != nil {
		t.Fatalf("handle callback: %v", err)
	}

	if len(bot.invoices) != 1 {
		t.Fatalf("expected 1 invoice sent, got %d", len(bot.invoices))
	}

	inv := bot.invoices[0]
	if inv.title != "між нами. Преміум" {
		t.Errorf("unexpected invoice title: %q", inv.title)
	}
	if !strings.Contains(inv.description, "Довічний преміум-доступ") {
		t.Errorf("unexpected invoice description: %q", inv.description)
	}
}

func TestMenuMainCallbackFromPhotoMessageDeletesMessage(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, nil)

	completeUkrainianOnboarding(t, app)

	cb := testPhotoCallback("menu:main")
	err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: cb})
	if err != nil {
		t.Fatalf("handle callback: %v", err)
	}

	if len(bot.deletedMessages) != 1 {
		t.Fatalf("expected 1 deleted message, got %d", len(bot.deletedMessages))
	}
	if bot.deletedMessages[0].messageID != cb.Message.MessageID {
		t.Errorf("unexpected deleted message ID: %d", bot.deletedMessages[0].messageID)
	}

	if len(bot.messages) == 0 {
		t.Fatalf("expected new message sent, got 0")
	}
	lastMsg := bot.messages[len(bot.messages)-1]
	if !strings.Contains(lastMsg.text, "між нами.") {
		t.Errorf("unexpected main menu text: %q", lastMsg.text)
	}
}

func TestAdminTestCardActionPreservesTestKeyboard(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, []int64{1001})

	completeUkrainianOnboarding(t, app)

	cb := testPhotoCallback("game:skip:q001")
	cb.Message.Caption = "[TEST] Level 1 · ID: q001"
	err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: cb})
	if err != nil {
		t.Fatalf("handle callback: %v", err)
	}

	if len(bot.captionEdits) != 1 {
		t.Fatalf("expected 1 caption edit, got %d", len(bot.captionEdits))
	}
	edit := bot.captionEdits[0]
	markup, ok := edit.markup.(telegram.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected inline keyboard markup, got %T", edit.markup)
	}

	if !inlineKeyboardHasCallback(markup, "game:admin_next:q001") {
		t.Errorf("expected keyboard to have game:admin_next callback, got: %v", markup)
	}
}

func TestAdminGrantPremiumCommandByIDGrantsAndAudits(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, []int64{1001})
	ensureKnownTargetUser(t, app, 2002, "partner")

	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("/grant 2002 premium")}); err != nil {
		t.Fatalf("grant command failed: %v", err)
	}

	if premium, err := app.repo.UserHasEntitlement(ctx, 2002, storage.EntitlementPremiumAccess, "premium_access"); err != nil {
		t.Fatalf("UserHasEntitlement returned error: %v", err)
	} else if !premium {
		t.Fatal("grant command did not grant premium")
	}
	if count, err := app.repo.AdminAuditCount(ctx, 1001, 2002, "grant", storage.EntitlementPremiumAccess, "premium_access"); err != nil {
		t.Fatalf("AdminAuditCount returned error: %v", err)
	} else if count != 1 {
		t.Fatalf("audit count = %d, want 1", count)
	}
}

func TestAdminGrantPremiumCommandByKnownUsername(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, []int64{1001})
	ensureKnownTargetUser(t, app, 2002, "partner")

	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("/grant @partner premium")}); err != nil {
		t.Fatalf("grant username command failed: %v", err)
	}

	if premium, err := app.repo.UserHasEntitlement(ctx, 2002, storage.EntitlementPremiumAccess, "premium_access"); err != nil {
		t.Fatalf("UserHasEntitlement returned error: %v", err)
	} else if !premium {
		t.Fatal("grant by username did not grant premium")
	}
}

func TestAdminGrantUnknownUsernameDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, []int64{1001})

	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("/grant @missing premium")}); err != nil {
		t.Fatalf("unknown username command returned transport error: %v", err)
	}

	if len(bot.messages) == 0 || !strings.Contains(strings.ToLower(bot.messages[len(bot.messages)-1].text), "unknown") {
		t.Fatalf("last admin response = %#v, want unknown target error", bot.messages)
	}
	if premium, err := app.repo.UserHasEntitlement(ctx, 2002, storage.EntitlementPremiumAccess, "premium_access"); err != nil {
		t.Fatalf("UserHasEntitlement returned error: %v", err)
	} else if premium {
		t.Fatal("unknown username grant mutated premium entitlement")
	}
}

func TestAdminInlineGrantAcceptsKnownUsernameAndClearsFSM(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, []int64{1001})
	ensureKnownTargetUser(t, app, 2002, "partner")

	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("admin:grant:premium_access:premium_access")}); err != nil {
		t.Fatalf("admin grant callback failed: %v", err)
	}
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("@partner")}); err != nil {
		t.Fatalf("admin grant username response failed: %v", err)
	}

	if premium, err := app.repo.UserHasEntitlement(ctx, 2002, storage.EntitlementPremiumAccess, "premium_access"); err != nil {
		t.Fatalf("UserHasEntitlement returned error: %v", err)
	} else if !premium {
		t.Fatal("inline grant by username did not grant premium")
	}
	if got := app.state.(*fakeState).values[1001]; got != "" {
		t.Fatalf("admin FSM after grant = %q, want cleared", got)
	}
}

func TestAdminRevokePremiumCommandByUsernameDisablesEntitlementAndAudits(t *testing.T) {
	ctx := context.Background()
	bot := &fakeBot{}
	app := newTestAppWithOptions(t, bot, []int64{1001})
	ensureKnownTargetUser(t, app, 2002, "partner")
	if err := app.repo.GrantEntitlement(ctx, storage.Entitlement{
		UserID:   2002,
		Type:     storage.EntitlementPremiumAccess,
		UnlockID: "premium_access",
		Source:   "admin_grant",
	}); err != nil {
		t.Fatalf("pre-grant premium returned error: %v", err)
	}

	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("/revoke @partner premium")}); err != nil {
		t.Fatalf("revoke command failed: %v", err)
	}

	if premium, err := app.repo.UserHasEntitlement(ctx, 2002, storage.EntitlementPremiumAccess, "premium_access"); err != nil {
		t.Fatalf("UserHasEntitlement returned error: %v", err)
	} else if premium {
		t.Fatal("revoke command left premium active")
	}
	if count, err := app.repo.AdminAuditCount(ctx, 1001, 2002, "revoke", storage.EntitlementPremiumAccess, "premium_access"); err != nil {
		t.Fatalf("AdminAuditCount returned error: %v", err)
	} else if count != 1 {
		t.Fatalf("revoke audit count = %d, want 1", count)
	}
}

func TestCustomQuestionsSettingsMenuAndFlow(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	// Open settings first
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("settings:open")}); err != nil {
		t.Fatalf("settings open callback failed: %v", err)
	}

	// Check settings menu contains the custom questions button
	lastEdit := bot.edits[len(bot.edits)-1]
	markup, ok := lastEdit.markup.(telegram.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected InlineKeyboardMarkup, got %T", lastEdit.markup)
	}
	if !inlineKeyboardHasCallback(markup, "custom_questions:menu") {
		t.Fatal("expected custom_questions:menu button in settings keyboard")
	}

	// Click custom questions button
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("custom_questions:menu")}); err != nil {
		t.Fatalf("custom questions menu callback failed: %v", err)
	}

	// Verify menu shows "no questions"
	lastEdit = bot.edits[len(bot.edits)-1]
	if !strings.Contains(lastEdit.text, "Власних питань поки немає") {
		t.Fatalf("expected menu to show 'no questions', got: %q", lastEdit.text)
	}
	markup = lastEdit.markup.(telegram.InlineKeyboardMarkup)
	if !inlineKeyboardHasCallback(markup, "custom_questions:add") {
		t.Fatal("expected custom_questions:add button")
	}

	// Click add button
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("custom_questions:add")}); err != nil {
		t.Fatalf("custom questions add callback failed: %v", err)
	}

	// Verify FSM state is set
	if got := state.values[1001]; got != "custom_question:await_text" {
		t.Fatalf("FSM state = %q, want custom_question:await_text", got)
	}

	// Type question
	beforeMsgs := len(bot.messages)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: testMessage("Яке твоє улюблене кіно?")}); err != nil {
		t.Fatalf("send question text failed: %v", err)
	}

	// Verify FSM is cleared
	if _, ok := state.values[1001]; ok {
		t.Fatal("expected FSM to be cleared")
	}

	// Verify success message sent and question listed
	if len(bot.messages) == beforeMsgs {
		t.Fatal("expected new message sent")
	}
	lastMsg := bot.messages[len(bot.messages)-1]
	if !strings.Contains(lastMsg.text, "успішно додано") {
		t.Fatalf("expected success message, got: %q", lastMsg.text)
	}
	if !strings.Contains(lastMsg.text, "Яке твоє улюблене кіно?") {
		t.Fatalf("expected question to be listed, got: %q", lastMsg.text)
	}

	// Now check custom questions are in play (mix custom questions into gameplay)
	// We need to pair first to play
	err := app.repo.EnsureUser(ctx, storage.User{
		TelegramID:      2002,
		Username:        "partner",
		DisplayName:     "Bob",
		Language:        "uk",
		SelectedStyleID: "default_warm",
		ThemeBaseColor:  "#d98c9f",
	})
	if err != nil {
		t.Fatalf("EnsureUser for partner failed: %v", err)
	}
	request, err := app.repo.CreatePairRequest(ctx, storage.PairRequest{
		RequesterID: 1001,
		InviteToken: "test-game-token",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePairRequest: %v", err)
	}
	if _, err := app.repo.AcceptPairRequest(ctx, request.InviteToken, 2002); err != nil {
		t.Fatalf("AcceptPairRequest: %v", err)
	}

	// Resume game now sends an invite first, then cards after partner acceptance.
	beforePhotos := bot.photos
	sessionID := startAndAcceptGame(t, app, bot)
	if sessionID == "" {
		t.Fatal("expected accepted game session id")
	}

	if bot.photos != beforePhotos+2 {
		t.Fatalf("expected cards sent to both partners, photos count before: %d, after: %d", beforePhotos, bot.photos)
	}

	// Delete question
	// First let's get the question ID from DB
	questions, err := app.repo.GetPairCustomQuestions(ctx, 1001)
	if err != nil {
		t.Fatalf("GetPairCustomQuestions: %v", err)
	}
	if len(questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(questions))
	}
	qID := questions[0].ID

	// Delete callback
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback(fmt.Sprintf("custom_questions:delete:%d", qID))}); err != nil {
		t.Fatalf("delete callback failed: %v", err)
	}

	// Verify question deleted
	questions, err = app.repo.GetPairCustomQuestions(ctx, 1001)
	if err != nil {
		t.Fatalf("GetPairCustomQuestions: %v", err)
	}
	if len(questions) != 0 {
		t.Fatalf("expected 0 questions after deletion, got %d", len(questions))
	}
}

func TestThemeMenuRendering(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	// Trigger theme menu callback
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("theme:menu")}); err != nil {
		t.Fatalf("theme menu callback failed: %v", err)
	}

	lastEdit := bot.edits[len(bot.edits)-1]
	if !strings.Contains(strings.ToLower(lastEdit.text), "settings") && !strings.Contains(lastEdit.text, "налаштування") {
		t.Fatalf("expected theme menu content, got: %q", lastEdit.text)
	}

	markup, ok := lastEdit.markup.(telegram.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected inline keyboard markup, got %T", lastEdit.markup)
	}

	// Verify keyboard has color, style, font, bg menu buttons
	expectedButtons := []string{"theme:color:menu", "theme:style:menu", "theme:font:menu", "theme:bg:menu"}
	for _, expected := range expectedButtons {
		if !inlineKeyboardHasCallback(markup, expected) {
			t.Errorf("expected callback button %q in theme menu keyboard", expected)
		}
	}
}

func TestThemePremiumLocking(t *testing.T) {
	app, bot, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	// User is not premium by default. Let's try to select premium style "premium_velvet"
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("theme:style:select:premium_velvet")}); err != nil {
		t.Fatalf("select premium style failed: %v", err)
	}

	lastEdit := bot.edits[len(bot.edits)-1]
	if !strings.Contains(lastEdit.text, "Style Velvet is locked") && !strings.Contains(lastEdit.text, "заблоковано") {
		t.Fatalf("expected lock message, got: %q", lastEdit.text)
	}

	markup := lastEdit.markup.(telegram.InlineKeyboardMarkup)
	if !inlineKeyboardHasCallback(markup, "store:open") {
		t.Error("expected store:open button for locked style")
	}

	// Now try to select premium font "google_sans_regular"
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("theme:font:select:google_sans_regular")}); err != nil {
		t.Fatalf("select premium font failed: %v", err)
	}

	lastEdit = bot.edits[len(bot.edits)-1]
	if !strings.Contains(lastEdit.text, "Font Google Sans is locked") && !strings.Contains(lastEdit.text, "заблоковано") {
		t.Fatalf("expected lock message, got: %q", lastEdit.text)
	}

	markup = lastEdit.markup.(telegram.InlineKeyboardMarkup)
	if !inlineKeyboardHasCallback(markup, "store:open") {
		t.Error("expected store:open button for locked font")
	}

	// Grant premium access
	err := app.repo.GrantEntitlement(ctx, storage.Entitlement{
		UserID:   1001,
		Type:     "premium_access",
		UnlockID: "premium_access",
	})
	if err != nil {
		t.Fatalf("GrantEntitlement: %v", err)
	}

	// Try selecting premium style again - should succeed and take us back to theme menu
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("theme:style:select:premium_velvet")}); err != nil {
		t.Fatalf("select premium style failed: %v", err)
	}
	lastEdit = bot.edits[len(bot.edits)-1]
	if !strings.Contains(strings.ToLower(lastEdit.text), "settings") && !strings.Contains(lastEdit.text, "налаштування") {
		t.Fatalf("expected theme menu, got: %q", lastEdit.text)
	}

	// Verify DB has the new style
	profile, err := app.repo.UserProfile(ctx, 1001)
	if err != nil {
		t.Fatalf("UserProfile: %v", err)
	}
	if profile.SelectedStyleID != "premium_velvet" {
		t.Errorf("expected selected style premium_velvet, got %s", profile.SelectedStyleID)
	}
}

func TestStyleParameterOverrides(t *testing.T) {
	app, _, _ := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	// Set border radius to 15
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("theme:style:set_border:15")}); err != nil {
		t.Fatalf("set border failed: %v", err)
	}

	// Set glass opacity to 0.4
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("theme:style:set_glass:0.4")}); err != nil {
		t.Fatalf("set glass failed: %v", err)
	}

	// Fetch profile to verify DB updated
	profile, err := app.repo.UserProfile(ctx, 1001)
	if err != nil {
		t.Fatalf("UserProfile: %v", err)
	}
	if !profile.CustomBorderRadius.Valid || profile.CustomBorderRadius.Int64 != 15 {
		t.Errorf("expected custom border radius 15, got %v", profile.CustomBorderRadius)
	}
	if !profile.CustomGlassOpacity.Valid || profile.CustomGlassOpacity.Float64 != 0.4 {
		t.Errorf("expected custom glass opacity 0.4, got %v", profile.CustomGlassOpacity)
	}

	// Test themeCardInput produces the custom overrides
	input, err := app.themeCardInput(ctx, 1001, "en")
	if err != nil {
		t.Fatalf("themeCardInput failed: %v", err)
	}
	if input.BorderRadius != 15 {
		t.Errorf("expected input border radius 15, got %f", input.BorderRadius)
	}
	if input.GlassOpacity != 0.4 {
		t.Errorf("expected input glass opacity 0.4, got %f", input.GlassOpacity)
	}
}

func testPhotoMessage(userID int64, fileID string) *telegram.Message {
	msg := testMessageFrom(userID, "tester", "en", "")
	msg.Photo = []telegram.PhotoSize{
		{
			FileID:   fileID,
			FileSize: 1000,
			Width:    100,
			Height:   100,
		},
	}
	return msg
}

func testDocumentMessage(userID int64, fileID, fileName, mimeType string, fileSize int64) *telegram.Message {
	msg := testMessageFrom(userID, "tester", "en", "")
	msg.Document = &telegram.Document{
		FileID:   fileID,
		FileName: fileName,
		MimeType: mimeType,
		FileSize: fileSize,
	}
	return msg
}

func TestBackgroundUploadAndLimit(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	// Trigger custom background upload callback
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("theme:bg:upload")}); err != nil {
		t.Fatalf("bg upload callback failed: %v", err)
	}

	// Verify state is in onboarding step for background
	if got := state.values[1001]; got != string(onboarding.StepBackground) {
		t.Fatalf("expected FSM to be StepBackground, got %q", got)
	}

	// Send first photo
	beforeMsgs := len(bot.messages)
	photoMsg := testPhotoMessage(1001, "photo_one")
	if err := app.HandleUpdate(ctx, telegram.Update{Message: photoMsg}); err != nil {
		t.Fatalf("handle first photo failed: %v", err)
	}

	// Verify success message sent and selected background ID updated
	if len(bot.messages) == beforeMsgs {
		t.Fatal("expected new message sent")
	}
	lastMsg := bot.messages[len(bot.messages)-1]
	if !strings.Contains(lastMsg.text, "successfully") && !strings.Contains(lastMsg.text, "завантажено") {
		t.Fatalf("expected success message, got: %q", lastMsg.text)
	}

	// Retrieve profile to check selected background
	profile, err := app.repo.UserProfile(ctx, 1001)
	if err != nil {
		t.Fatalf("UserProfile: %v", err)
	}
	if !strings.HasPrefix(profile.SelectedBackgroundAssetID, "upload_1001_") {
		t.Fatalf("expected selected background to start with upload_1001_, got %q", profile.SelectedBackgroundAssetID)
	}

	// Let's upload a second and third background
	for i := 2; i <= 3; i++ {
		// Set FSM again
		_ = state.SetFSM(ctx, 1001, string(onboarding.StepBackground), 24*time.Hour)
		photoMsg = testPhotoMessage(1001, fmt.Sprintf("photo_%d", i))
		if err := app.HandleUpdate(ctx, telegram.Update{Message: photoMsg}); err != nil {
			t.Fatalf("handle photo %d failed: %v", i, err)
		}
	}

	// Now we have 3 custom backgrounds. Let's try to trigger upload again
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("theme:bg:upload")}); err != nil {
		t.Fatalf("bg upload callback failed: %v", err)
	}

	// Verify we got the upload limit message, NOT the upload prompt
	lastEdit := bot.edits[len(bot.edits)-1]
	if !strings.Contains(lastEdit.text, "limit") && !strings.Contains(lastEdit.text, "ліміту") {
		t.Fatalf("expected limit message, got: %q", lastEdit.text)
	}

	// Get current selected background ID
	profile, err = app.repo.UserProfile(ctx, 1001)
	if err != nil {
		t.Fatalf("UserProfile: %v", err)
	}
	currentSelectedAssetID := profile.SelectedBackgroundAssetID

	// Also let's test deleting a background
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("theme:bg:delete_confirm:" + currentSelectedAssetID)}); err != nil {
		t.Fatalf("delete background callback failed: %v", err)
	}

	// Verify profile selected background is reset to default (empty) since we deleted the selected one
	profile, err = app.repo.UserProfile(ctx, 1001)
	if err != nil {
		t.Fatalf("UserProfile: %v", err)
	}
	if profile.SelectedBackgroundAssetID != "" {
		t.Fatalf("expected selected background to be reset to empty, got %q", profile.SelectedBackgroundAssetID)
	}
}

func TestBackgroundDocumentUploadAcceptsImagesAndRejectsUnsupported(t *testing.T) {
	app, bot, state := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	_ = state.SetFSM(ctx, 1001, string(onboarding.StepBackground), 24*time.Hour)
	beforeMsgs := len(bot.messages)
	docMsg := testDocumentMessage(1001, "doc_png", "background.png", "image/png", 1000)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: docMsg}); err != nil {
		t.Fatalf("handle document image failed: %v", err)
	}
	if len(bot.messages) == beforeMsgs {
		t.Fatal("expected document upload response")
	}
	profile, err := app.repo.UserProfile(ctx, 1001)
	if err != nil {
		t.Fatalf("UserProfile: %v", err)
	}
	if !strings.HasPrefix(profile.SelectedBackgroundAssetID, "upload_1001_") {
		t.Fatalf("selected background = %q, want uploaded asset", profile.SelectedBackgroundAssetID)
	}

	_ = state.SetFSM(ctx, 1001, string(onboarding.StepBackground), 24*time.Hour)
	before := len(bot.messages)
	badDoc := testDocumentMessage(1001, "doc_txt", "notes.txt", "text/plain", 1000)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: badDoc}); err != nil {
		t.Fatalf("handle unsupported document failed: %v", err)
	}
	if got := bot.messages[before].text; !strings.Contains(got, "failed") && !strings.Contains(got, "Помилка") {
		t.Fatalf("unsupported document response = %q", got)
	}

	_ = state.SetFSM(ctx, 1001, string(onboarding.StepBackground), 24*time.Hour)
	before = len(bot.messages)
	oversizeDoc := testDocumentMessage(1001, "doc_big", "huge.webp", "image/webp", render.DefaultMaxUploadBytes+1)
	if err := app.HandleUpdate(ctx, telegram.Update{Message: oversizeDoc}); err != nil {
		t.Fatalf("handle oversized document failed: %v", err)
	}
	if got := bot.messages[before].text; !strings.Contains(got, "failed") && !strings.Contains(got, "Помилка") {
		t.Fatalf("oversized document response = %q", got)
	}
}

func TestGDPRFileCleanups(t *testing.T) {
	app, _, state := newTestApp(t)
	ctx := context.Background()
	completeUkrainianOnboarding(t, app)

	// Set FSM to background upload and upload a background
	_ = state.SetFSM(ctx, 1001, string(onboarding.StepBackground), 24*time.Hour)
	photoMsg := testPhotoMessage(1001, "photo_gdpr")
	if err := app.HandleUpdate(ctx, telegram.Update{Message: photoMsg}); err != nil {
		t.Fatalf("upload photo for gdpr test failed: %v", err)
	}

	// Verify it put the file in object store
	fakeStore := app.objectStore.(*fakeObjectStore)
	if len(fakeStore.objects) != 1 {
		t.Fatalf("expected 1 file in object store, got %d", len(fakeStore.objects))
	}

	// Trigger delete account callback
	if err := app.HandleUpdate(ctx, telegram.Update{CallbackQuery: testCallback("settings:delete_confirm")}); err != nil {
		t.Fatalf("delete account confirm failed: %v", err)
	}

	// Verify file is removed from object store
	if len(fakeStore.objects) != 0 {
		t.Errorf("expected 0 files in object store after account deletion, got %d", len(fakeStore.objects))
	}

	// Verify user is deleted from repo
	profile, err := app.repo.UserProfile(ctx, 1001)
	if err != nil {
		t.Fatalf("UserProfile failed: %v", err)
	}
	if profile.TelegramID != 0 {
		t.Error("expected user to be deleted (TelegramID == 0)")
	}
}
