package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"wrnrs/internal/admin"
	"wrnrs/internal/config"
	"wrnrs/internal/content"
	"wrnrs/internal/game"
	"wrnrs/internal/i18n"
	"wrnrs/internal/onboarding"
	"wrnrs/internal/pairing"
	"wrnrs/internal/payments"
	"wrnrs/internal/render"
	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

type Bot interface {
	SendMessage(ctx context.Context, chatID int64, text string, replyMarkup any) error
	EditMessageText(ctx context.Context, chatID, messageID int64, text string, replyMarkup any) error
	EditMessageCaption(ctx context.Context, chatID, messageID int64, caption string, replyMarkup any) error
	EditMessageReplyMarkup(ctx context.Context, chatID, messageID int64, replyMarkup any) error
	SendPhoto(ctx context.Context, chatID int64, png []byte, caption string, replyMarkup any) error
	EditMessageMedia(ctx context.Context, chatID, messageID int64, png []byte, caption string, replyMarkup any) error
	AnswerCallbackQuery(ctx context.Context, callbackID, text string) error
	AnswerPreCheckoutQuery(ctx context.Context, id string, ok bool, errorMessage string) error
	SendInvoice(ctx context.Context, chatID int64, title, description, payload string, amount int64, replyMarkup any) error
	DeleteMessage(ctx context.Context, chatID, messageID int64) error
	GetFile(ctx context.Context, fileID string) (telegram.File, error)
	DownloadFile(ctx context.Context, filePath string) ([]byte, error)
}

type FSMStore interface {
	SetFSM(ctx context.Context, userID int64, value string, ttl time.Duration) error
	GetFSM(ctx context.Context, userID int64) (string, error)
	ClearFSM(ctx context.Context, userID int64) error
	SetPendingGameCompletion(ctx context.Context, userID int64, completion game.Completion, ttl time.Duration) error
	PendingGameCompletion(ctx context.Context, userID int64) (game.Completion, bool, error)
	ClearPendingGameCompletion(ctx context.Context, userID int64) error
}

type ObjectStore interface {
	Put(ctx context.Context, objectKey, contentType string, data []byte) error
	Get(ctx context.Context, objectKey string) ([]byte, error)
	Delete(ctx context.Context, objectKey string) error
}

type GameCompletion = game.Completion

type App struct {
	cfg         config.Config
	bot         Bot
	repo        *storage.Repository
	state       FSMStore
	i18n        *i18n.Bundle
	deck        *content.Deck
	renderer    *render.CardRenderer
	styles      *content.StyleCatalog
	backgrounds *content.BackgroundCatalog
	fonts       *content.FontCatalog
	objectStore ObjectStore
	onboarding  *onboarding.Service
	pairing     *pairing.Service
	gameService *game.Service
	admin       *admin.Service
	payments    payments.Catalog
	logger      *slog.Logger
}

type Options struct {
	Config      config.Config
	Bot         Bot
	Repo        *storage.Repository
	State       FSMStore
	I18N        *i18n.Bundle
	Deck        *content.Deck
	Renderer    *render.CardRenderer
	Styles      *content.StyleCatalog
	Backgrounds *content.BackgroundCatalog
	Fonts       *content.FontCatalog
	ObjectStore ObjectStore
	Logger      *slog.Logger
}

func New(options Options) *App {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	var gameService *game.Service
	if options.Repo != nil && options.Deck != nil && len(options.Config.AnswerEncryptionKey) == 32 {
		answerCipher, err := storage.NewAnswerCipher(options.Config.AnswerEncryptionKey)
		if err == nil {
			gameService = game.NewService(game.ServiceOptions{
				Repo:         options.Repo,
				Deck:         options.Deck,
				AnswerCipher: answerCipher,
			})
		}
	}
	return &App{
		cfg:         options.Config,
		bot:         options.Bot,
		repo:        options.Repo,
		state:       options.State,
		i18n:        options.I18N,
		deck:        options.Deck,
		renderer:    options.Renderer,
		styles:      options.Styles,
		backgrounds: options.Backgrounds,
		fonts:       options.Fonts,
		objectStore: options.ObjectStore,
		onboarding:  onboarding.NewService(),
		pairing:     pairing.NewService(options.Config.BotUsername, options.Config.PhoneHashSecret),
		gameService: gameService,
		admin:       admin.NewService(options.Config.AdminTelegramIDs),
		payments:    payments.DefaultCatalog(),
		logger:      logger,
	}
}

func (a *App) HandleUpdate(ctx context.Context, update telegram.Update) error {
	switch {
	case update.PreCheckoutQuery != nil:
		return a.handlePreCheckout(ctx, update.PreCheckoutQuery)
	case update.Message != nil:
		return a.handleMessage(ctx, update.Message)
	case update.CallbackQuery != nil:
		return a.handleCallback(ctx, update.CallbackQuery)
	case update.InlineQuery != nil:
		a.logger.Info("inline query received", "from", update.InlineQuery.From.ID, "query", update.InlineQuery.Query)
		return nil
	default:
		return nil
	}
}

func (a *App) handleMessage(ctx context.Context, msg *telegram.Message) error {
	if msg.From == nil {
		return nil
	}
	userID := msg.From.ID
	lang := a.userLanguage(ctx, userID, normalizeLanguage(msg.From.LanguageCode))
	if isResetText(msg.Text) {
		if a.state != nil {
			if err := a.state.ClearFSM(ctx, userID); err != nil {
				return err
			}
		}
		return a.sendMainMenu(ctx, msg.Chat.ID, lang, a.i18n.Text(lang, "menu.reset_complete"))
	}
	if isMenuText(msg.Text) {
		if a.state != nil {
			if err := a.state.ClearFSM(ctx, userID); err != nil {
				return err
			}
		}
		return a.sendMainMenu(ctx, msg.Chat.ID, lang, a.i18n.Text(lang, "menu.title"))
	}
	if msg.SuccessfulPayment != nil {
		return a.handleSuccessfulPayment(ctx, msg)
	}
	if strings.HasPrefix(msg.Text, "/start") {
		if a.state != nil {
			if err := a.state.ClearFSM(ctx, userID); err != nil {
				return err
			}
		}
		return a.start(ctx, msg, normalizeLanguage(msg.From.LanguageCode))
	}
	if msg.Text == "/paysupport" {
		if a.state != nil {
			if err := a.state.ClearFSM(ctx, userID); err != nil {
				return err
			}
		}
		return a.sendPaymentSupport(ctx, msg.Chat.ID, lang)
	}
	if msg.Text == "/admin" && a.admin.IsAdmin(userID) {
		if a.state != nil {
			if err := a.state.ClearFSM(ctx, userID); err != nil {
				return err
			}
		}
		return a.bot.SendMessage(ctx, msg.Chat.ID, "Admin menu. Use buttons, then send a numeric Telegram ID when prompted.", a.admin.Menu())
	}
	if handled, err := a.handleGameMessage(ctx, msg); handled || err != nil {
		return err
	}
	if a.admin.IsAdmin(userID) {
		if handled, err := a.handleAdminText(ctx, msg); handled || err != nil {
			return err
		}
	}
	if handled, err := a.handleOnboardingText(ctx, msg); handled || err != nil {
		return err
	}
	if handled, err := a.handlePairingMessage(ctx, msg); handled || err != nil {
		return err
	}
	if handled, err := a.handleCustomQuestionMessage(ctx, msg); handled || err != nil {
		return err
	}
	return a.sendMainMenu(ctx, msg.Chat.ID, lang, a.i18n.Text(lang, "menu.title"))
}

func (a *App) start(ctx context.Context, msg *telegram.Message, language string) error {
	user := storage.User{
		TelegramID:      msg.From.ID,
		Username:        msg.From.Username,
		DisplayName:     strings.TrimSpace(msg.From.FirstName + " " + msg.From.LastName),
		Language:        language,
		SelectedStyleID: "default_warm",
		ThemeBaseColor:  "#d98c9f",
	}
	if err := a.repo.EnsureUser(ctx, user); err != nil {
		return err
	}
	if token, ok := pairing.TokenFromText(msg.Text); ok {
		return a.showPairRequest(ctx, msg.Chat.ID, msg.From.ID, token)
	}
	lang := a.userLanguage(ctx, msg.From.ID, language)
	complete, err := a.repo.UserOnboardingComplete(ctx, msg.From.ID)
	if err != nil {
		return err
	}
	if complete {
		return a.sendMainMenu(ctx, msg.Chat.ID, lang, a.i18n.Text(lang, "menu.title"))
	}
	if a.state != nil {
		if err := a.state.SetFSM(ctx, msg.From.ID, string(onboarding.StepLanguage), 24*time.Hour); err != nil {
			return err
		}
	}
	return a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "onboarding.language"), a.onboarding.LanguageKeyboard())
}

func (a *App) handleCallback(ctx context.Context, cb *telegram.CallbackQuery) error {
	if err := a.ensureTelegramUser(ctx, cb.From); err != nil {
		return err
	}
	lang := a.userLanguage(ctx, cb.From.ID, normalizeLanguage(cb.From.LanguageCode))
	chatID := cb.From.ID
	if cb.Message != nil {
		chatID = cb.Message.Chat.ID
	}
	_ = a.bot.AnswerCallbackQuery(ctx, cb.ID, "")

	switch {
	case strings.HasPrefix(cb.Data, "onboarding:language:"):
		chosen := strings.TrimPrefix(cb.Data, "onboarding:language:")
		if err := a.repo.UpdateUserLanguage(ctx, cb.From.ID, chosen); err != nil {
			return err
		}
		complete, err := a.repo.UserOnboardingComplete(ctx, cb.From.ID)
		if err != nil {
			return err
		}
		if complete {
			if a.state != nil {
				if err := a.state.ClearFSM(ctx, cb.From.ID); err != nil {
					return err
				}
			}
			return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(chosen, "settings.language_saved"), settingsKeyboard(chosen, a.i18n, a.admin.IsAdmin(cb.From.ID)))
		}
		if a.state != nil {
			_ = a.state.SetFSM(ctx, cb.From.ID, string(onboarding.StepName), 24*time.Hour)
		}
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(chosen, "onboarding.name"), nil)
	case strings.HasPrefix(cb.Data, "onboarding:gender:"):
		chosen := strings.TrimPrefix(cb.Data, "onboarding:gender:")
		if err := a.repo.UpdateUserGender(ctx, cb.From.ID, chosen); err != nil {
			return err
		}
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		if a.state != nil {
			_ = a.state.SetFSM(ctx, cb.From.ID, string(onboarding.StepAdult), 24*time.Hour)
		}
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "onboarding.adult"), a.onboarding.AdultKeyboard(lang))
	case strings.HasPrefix(cb.Data, "onboarding:adult:"):
		answer := strings.TrimPrefix(cb.Data, "onboarding:adult:")
		is18Plus := answer == "yes"
		if err := a.repo.UpdateAdultConfirmation(ctx, cb.From.ID, is18Plus); err != nil {
			return err
		}
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		if is18Plus {
			if a.state != nil {
				_ = a.state.SetFSM(ctx, cb.From.ID, string(onboarding.StepMatureOptIn), 24*time.Hour)
			}
			return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "onboarding.mature"), a.onboarding.MatureKeyboard(lang))
		}
		if err := a.repo.UpdateMatureOptIn(ctx, cb.From.ID, false); err != nil {
			return err
		}
		if a.state != nil {
			_ = a.state.SetFSM(ctx, cb.From.ID, string(onboarding.StepThemeColor), 24*time.Hour)
		}
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "onboarding.theme_color"), a.onboarding.ColorKeyboard(lang))
	case strings.HasPrefix(cb.Data, "onboarding:mature:"):
		answer := strings.TrimPrefix(cb.Data, "onboarding:mature:")
		if err := a.repo.UpdateMatureOptIn(ctx, cb.From.ID, answer == "yes"); err != nil {
			return err
		}
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		if a.state != nil {
			_ = a.state.SetFSM(ctx, cb.From.ID, string(onboarding.StepThemeColor), 24*time.Hour)
		}
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "onboarding.theme_color"), a.onboarding.ColorKeyboard(lang))
	case cb.Data == "onboarding:language_menu":
		complete, err := a.repo.UserOnboardingComplete(ctx, cb.From.ID)
		if err != nil {
			return err
		}
		if a.state != nil && !complete {
			_ = a.state.SetFSM(ctx, cb.From.ID, string(onboarding.StepLanguage), 24*time.Hour)
		}
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "onboarding.language"), a.onboarding.LanguageKeyboard())
	case strings.HasPrefix(cb.Data, "admin:grant:"), strings.HasPrefix(cb.Data, "admin:revoke:"):
		if !a.admin.IsAdmin(cb.From.ID) {
			return a.bot.SendMessage(ctx, chatID, "Admin access required.", nil)
		}
		if a.state != nil {
			_ = a.state.SetFSM(ctx, cb.From.ID, cb.Data, 10*time.Minute)
		}
		return a.bot.SendMessage(ctx, chatID, "Send target numeric Telegram ID.", telegram.PersistentKeyboard(lang))
	case cb.Data == "store:open":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		return a.sendPremiumInvoice(ctx, chatID, cb.From.ID, lang)
	case cb.Data == "game:start":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		if a.gameService == nil {
			return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "game.not_ready"), telegram.MainMenuKeyboard(lang))
		}
		result, err := a.gameService.Start(ctx, cb.From.ID)
		if errors.Is(err, game.ErrActivePairRequired) {
			return a.editCallbackScreen(ctx, cb, chatID,
				a.i18n.Text(lang, "game.requires_pair"),
				telegram.MainMenuKeyboardWithPair(lang, false))
		}
		if err != nil {
			return err
		}
		switch result.Kind {
		case game.StartPendingInvite:
			if err := a.sendGameInvite(ctx, cb.From.ID, result.PartnerID, result.Session.ID); err != nil {
				return err
			}
			return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "game.invite_sent"), telegram.MainMenuKeyboardWithPair(lang, true))
		case game.StartActiveSession:
			return a.sendGameCard(ctx, cb.From.ID, lang, result.Card, result.Session.ID)
		case game.StartRevealed:
			return a.bot.SendMessage(ctx, chatID, a.i18n.Text(lang, "game.revealed"), nextCardKeyboard(lang, result.Session.ID))
		default:
			return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "game.card_stale"), telegram.MainMenuKeyboardWithPair(lang, true))
		}
	case cb.Data == "pair:menu":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		if active, err := a.repo.ActivePairForUser(ctx, cb.From.ID); err != nil {
			return err
		} else if active != nil {
			return a.editCallbackScreen(ctx, cb, chatID, a.activePairText(ctx, lang, active), telegram.MainMenuKeyboardWithPair(lang, true))
		}
		if a.state != nil {
			_ = a.state.SetFSM(ctx, cb.From.ID, "pairing:await_identifier", 24*time.Hour)
		}
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "pair.instructions"), pairMenuKeyboard(lang))
	case cb.Data == "journal:open":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		return a.editCallbackScreen(ctx, cb, chatID, menuPanelText(lang, "journal"), telegram.MainMenuKeyboard(lang))
	case cb.Data == "settings:open":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		return a.editCallbackScreen(ctx, cb, chatID, menuPanelText(lang, "settings"), settingsKeyboard(lang, a.i18n, a.admin.IsAdmin(cb.From.ID)))
	case cb.Data == "settings:delete_account":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "settings.delete_confirm_prompt"), deleteAccountKeyboard(lang, a.i18n))
	case cb.Data == "settings:delete_confirm":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		if uploads, err := a.repo.GetUserUploadedBackgrounds(ctx, cb.From.ID); err == nil {
			for _, upload := range uploads {
				if a.objectStore != nil {
					_ = a.objectStore.Delete(ctx, upload.MinioObjectKey)
				}
			}
		}
		if err := a.repo.DeleteUser(ctx, cb.From.ID); err != nil {
			return err
		}
		if a.state != nil {
			if err := a.state.ClearFSM(ctx, cb.From.ID); err != nil {
				return err
			}
			if err := a.state.ClearPendingGameCompletion(ctx, cb.From.ID); err != nil {
				return err
			}
		}
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "settings.deleted"), nil)
	case cb.Data == "custom_questions:menu":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		return a.renderCustomQuestionsMenu(ctx, cb, chatID, cb.From.ID, lang, "")
	case cb.Data == "custom_questions:add":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		if a.state != nil {
			_ = a.state.SetFSM(ctx, cb.From.ID, "custom_question:await_text", 24*time.Hour)
		}
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "custom_questions.enter_prompt"), nil)
	case strings.HasPrefix(cb.Data, "custom_questions:delete:"):
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		idStr := strings.TrimPrefix(cb.Data, "custom_questions:delete:")
		var id int64
		if _, err := fmt.Sscanf(idStr, "%d", &id); err == nil {
			_ = a.repo.DeleteCustomQuestion(ctx, id, cb.From.ID)
		}
		return a.renderCustomQuestionsMenu(ctx, cb, chatID, cb.From.ID, lang, a.i18n.Text(lang, "custom_questions.success_deleted"))
	case cb.Data == "custom_questions:noop":
		return nil
	case cb.Data == "theme:menu":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		if a.state != nil {
			_ = a.state.ClearFSM(ctx, cb.From.ID)
		}
		return a.themeMenu(ctx, cb, chatID, lang)
	case cb.Data == "theme:color:menu":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		title := "Color Picker\n\nChoose base color presets or enter Custom HEX code."
		if lang == "uk" {
			title = "Вибір кольору\n\nОберіть колір із готових пресетів або вкажіть власний HEX-код."
		}
		return a.editCallbackScreen(ctx, cb, chatID, title, a.themeColorKeyboard(lang))
	case cb.Data == "theme:style:menu":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		title := "Style Picker\n\nChoose a style layout for your cards."
		if lang == "uk" {
			title = "Вибір стилю\n\nОберіть стиль оформлення для ваших карток."
		}
		return a.editCallbackScreen(ctx, cb, chatID, title, a.themeStyleKeyboard(ctx, cb.From.ID, lang))
	case cb.Data == "theme:style:edit":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		title := "Edit Style Properties\n\nAdjust the border radius of the text block and the glass opacity."
		if lang == "uk" {
			title = "Редагування властивостей стилю\n\nНалаштуйте радіус рамки текстового блоку та прозорість скла."
		}
		return a.editCallbackScreen(ctx, cb, chatID, title, a.themeStyleEditKeyboard(ctx, cb.From.ID, lang))
	case cb.Data == "theme:style:edit_nop":
		return nil
	case strings.HasPrefix(cb.Data, "theme:style:set_border:"):
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		valStr := strings.TrimPrefix(cb.Data, "theme:style:set_border:")
		var val sql.NullInt64
		if valStr != "default" {
			var parsed int64
			if _, err := fmt.Sscanf(valStr, "%d", &parsed); err == nil {
				val = sql.NullInt64{Int64: parsed, Valid: true}
			}
		}
		if err := a.repo.UpdateCustomBorderRadius(ctx, cb.From.ID, val); err != nil {
			return err
		}
		title := "Edit Style Properties\n\nAdjust the border radius of the text block and the glass opacity."
		if lang == "uk" {
			title = "Редагування властивостей стилю\n\nНалаштуйте радіус рамки текстового блоку та прозорість скла."
		}
		return a.editCallbackScreen(ctx, cb, chatID, title, a.themeStyleEditKeyboard(ctx, cb.From.ID, lang))
	case strings.HasPrefix(cb.Data, "theme:style:set_glass:"):
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		valStr := strings.TrimPrefix(cb.Data, "theme:style:set_glass:")
		var val sql.NullFloat64
		if valStr != "default" {
			var parsed float64
			if _, err := fmt.Sscanf(valStr, "%f", &parsed); err == nil {
				val = sql.NullFloat64{Float64: parsed, Valid: true}
			}
		}
		if err := a.repo.UpdateCustomGlassOpacity(ctx, cb.From.ID, val); err != nil {
			return err
		}
		title := "Edit Style Properties\n\nAdjust the border radius of the text block and the glass opacity."
		if lang == "uk" {
			title = "Редагування властивостей стилю\n\nНалаштуйте радіус рамки текстового блоку та прозорість скла."
		}
		return a.editCallbackScreen(ctx, cb, chatID, title, a.themeStyleEditKeyboard(ctx, cb.From.ID, lang))
	case strings.HasPrefix(cb.Data, "theme:style:select:"):
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		styleID := strings.TrimPrefix(cb.Data, "theme:style:select:")
		if style, ok := a.styles.Style(styleID); ok && style.Premium {
			unlocked := a.userHasThemeEntitlementBool(ctx, cb.From.ID, storage.EntitlementStyle, style.ID)
			if !unlocked {
				text := fmt.Sprintf(a.i18n.Text(lang, "theme.style_locked"), style.Name[lang])
				return a.editCallbackScreen(ctx, cb, chatID, text, premiumLockKeyboard(lang))
			}
		}
		if err := a.repo.UpdateUserStyle(ctx, cb.From.ID, styleID); err != nil {
			return err
		}
		return a.themeMenu(ctx, cb, chatID, lang)
	case cb.Data == "theme:font:menu":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		title := "Font Picker\n\nChoose a custom font for your cards."
		if lang == "uk" {
			title = "Вибір шрифту\n\nОберіть шрифт для ваших карток."
		}
		return a.editCallbackScreen(ctx, cb, chatID, title, a.themeFontKeyboard(ctx, cb.From.ID, lang))
	case strings.HasPrefix(cb.Data, "theme:font:select:"):
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		fontID := strings.TrimPrefix(cb.Data, "theme:font:select:")
		if font, ok := a.fonts.Font(fontID); ok && font.Premium {
			unlocked := a.userHasThemeEntitlementBool(ctx, cb.From.ID, storage.EntitlementFont, font.ID)
			if !unlocked {
				text := fmt.Sprintf(a.i18n.Text(lang, "theme.font_locked"), font.Name[lang])
				return a.editCallbackScreen(ctx, cb, chatID, text, premiumLockKeyboard(lang))
			}
		}
		if err := a.repo.UpdateUserFont(ctx, cb.From.ID, fontID); err != nil {
			return err
		}
		return a.themeMenu(ctx, cb, chatID, lang)
	case cb.Data == "theme:bg:menu":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		title := "Background Picker\n\nChoose built-in backgrounds or upload custom ones (up to 3)."
		if lang == "uk" {
			title = "Вибір фону\n\nОберіть стандартний фон або завантажте власний (до 3)."
		}
		return a.editCallbackScreen(ctx, cb, chatID, title, a.themeBgKeyboard(ctx, cb.From.ID, lang))
	case strings.HasPrefix(cb.Data, "theme:bg:select:"):
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		bgID := strings.TrimPrefix(cb.Data, "theme:bg:select:")
		if bgID == "default" {
			if err := a.repo.UpdateUserBackground(ctx, cb.From.ID, ""); err != nil {
				return err
			}
		} else {
			if bg, ok := a.backgrounds.Background(bgID); ok {
				if bg.Premium {
					unlocked := a.userHasThemeEntitlementBool(ctx, cb.From.ID, storage.EntitlementBackground, bg.ID)
					if !unlocked {
						text := fmt.Sprintf(a.i18n.Text(lang, "theme.bg_locked"), bg.Name[lang])
						return a.editCallbackScreen(ctx, cb, chatID, text, premiumLockKeyboard(lang))
					}
				}
				if err := a.repo.UpdateUserBackground(ctx, cb.From.ID, bgID); err != nil {
					return err
				}
			} else {
				// Uploaded asset selection
				if asset, err := a.repo.GetThemeAsset(ctx, bgID); err == nil && asset.OwnerUserID == cb.From.ID && asset.Status == "active" {
					if err := a.repo.UpdateUserBackground(ctx, cb.From.ID, bgID); err != nil {
						return err
					}
				}
			}
		}
		return a.themeMenu(ctx, cb, chatID, lang)
	case cb.Data == "theme:bg:upload":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		count, err := a.repo.UserActiveUploadedBackgroundsCount(ctx, cb.From.ID)
		if err != nil {
			return err
		}
		if count >= 3 {
			return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "theme.upload_limit"), a.themeBgKeyboard(ctx, cb.From.ID, lang))
		}
		if a.state != nil {
			_ = a.state.SetFSM(ctx, cb.From.ID, string(onboarding.StepBackground), 24*time.Hour)
		}
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "theme.upload_prompt"), nil)
	case strings.HasPrefix(cb.Data, "theme:bg:delete:"):
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		assetID := strings.TrimPrefix(cb.Data, "theme:bg:delete:")
		yesBtn := "✅ Delete"
		noBtn := "« Back"
		if lang == "uk" {
			yesBtn = "✅ Видалити"
			noBtn = "« Назад"
		}
		kbd := telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: yesBtn, CallbackData: "theme:bg:delete_confirm:" + assetID}, {Text: noBtn, CallbackData: "theme:bg:menu"}},
		}}
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "theme.delete_bg_confirm"), kbd)
	case strings.HasPrefix(cb.Data, "theme:bg:delete_confirm:"):
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		assetID := strings.TrimPrefix(cb.Data, "theme:bg:delete_confirm:")
		asset, err := a.repo.GetThemeAsset(ctx, assetID)
		if err == nil && asset.OwnerUserID == cb.From.ID {
			if a.objectStore != nil {
				_ = a.objectStore.Delete(ctx, asset.MinioObjectKey)
			}
			_ = a.repo.DeleteThemeAsset(ctx, assetID)
			// If selected background was this one, reset to default
			profile, err := a.repo.UserProfile(ctx, cb.From.ID)
			if err == nil && profile.SelectedBackgroundAssetID == assetID {
				_ = a.repo.UpdateUserBackground(ctx, cb.From.ID, "")
			}
		}
		title := "Background Picker\n\nChoose built-in backgrounds or upload custom ones (up to 3)."
		if lang == "uk" {
			title = "Вибір фону\n\nОберіть стандартний фон або завантажте власний (до 3)."
		}
		return a.editCallbackScreen(ctx, cb, chatID, title, a.themeBgKeyboard(ctx, cb.From.ID, lang))
	case strings.HasPrefix(cb.Data, "theme:color:"):
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		color := strings.TrimPrefix(cb.Data, "theme:color:")
		if color == "custom" {
			if a.state != nil {
				_ = a.state.SetFSM(ctx, cb.From.ID, string(onboarding.StepThemeColor), 24*time.Hour)
			}
			return a.editCallbackScreen(ctx, cb, chatID, customColorPrompt(lang), customColorKeyboard(lang))
		}
		if !isHexColor(color) {
			return a.editCallbackScreen(ctx, cb, chatID, customColorPrompt(lang), customColorKeyboard(lang))
		}
		if err := a.repo.UpdateThemeColor(ctx, cb.From.ID, color); err != nil {
			return err
		}
		complete, err := a.repo.UserOnboardingComplete(ctx, cb.From.ID)
		if err != nil {
			return err
		}
		if !complete {
			if err := a.repo.MarkOnboardingComplete(ctx, cb.From.ID); err != nil {
				return err
			}
			if a.state != nil {
				_ = a.state.ClearFSM(ctx, cb.From.ID)
			}
			return a.editCallbackScreen(ctx, cb, chatID, themeSavedText(lang), telegram.MainMenuKeyboardWithPair(lang, a.userHasPair(ctx, cb.From.ID)))
		}
		if a.state != nil {
			_ = a.state.ClearFSM(ctx, cb.From.ID)
		}
		return a.themeMenu(ctx, cb, chatID, lang)
	case cb.Data == "menu:main":
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		text, hasPair := a.buildMainMenuText(ctx, cb.From.ID, lang)
		return a.editCallbackScreen(ctx, cb, chatID, text, telegram.MainMenuKeyboardWithPair(lang, hasPair))
	case strings.HasPrefix(cb.Data, "pair:accept:"):
		return a.acceptPairRequest(ctx, cb, chatID, cb.From.ID, strings.TrimPrefix(cb.Data, "pair:accept:"))
	case strings.HasPrefix(cb.Data, "pair:decline:"):
		return a.declinePairRequest(ctx, cb, chatID, cb.From.ID, strings.TrimPrefix(cb.Data, "pair:decline:"))
	case strings.HasPrefix(cb.Data, "game:accept:"):
		return a.acceptGameInvite(ctx, cb, chatID, cb.From.ID, strings.TrimPrefix(cb.Data, "game:accept:"))
	case strings.HasPrefix(cb.Data, "game:decline:"):
		return a.declineGameInvite(ctx, cb, chatID, cb.From.ID, strings.TrimPrefix(cb.Data, "game:decline:"))
	case cb.Data == "settings:test_cards":
		if !a.admin.IsAdmin(cb.From.ID) {
			return nil
		}
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		return a.adminTestCard(ctx, cb, chatID, lang, "")
	case strings.HasPrefix(cb.Data, "game:admin_next:"):
		if !a.admin.IsAdmin(cb.From.ID) {
			return nil
		}
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		return a.adminTestCard(ctx, cb, chatID, lang, strings.TrimPrefix(cb.Data, "game:admin_next:"))
	case strings.HasPrefix(cb.Data, "game:admin_prev:"):
		if !a.admin.IsAdmin(cb.From.ID) {
			return nil
		}
		lang = a.userLanguage(ctx, cb.From.ID, lang)
		return a.adminTestCardPrev(ctx, cb, chatID, lang, strings.TrimPrefix(cb.Data, "game:admin_prev:"))
	case strings.HasPrefix(cb.Data, "game:"):
		return a.handleGameCallback(ctx, cb, chatID, lang)
	default:
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "menu.title"), telegram.MainMenuKeyboard(lang))
	}
}

func (a *App) handleGameMessage(ctx context.Context, msg *telegram.Message) (bool, error) {
	if a.state == nil {
		return false, nil
	}
	fsm, err := a.state.GetFSM(ctx, msg.From.ID)
	if err != nil {
		return true, err
	}
	sessionID, ok := gameSessionFromFSM(fsm)
	if !ok {
		return false, nil
	}
	lang := a.userLanguage(ctx, msg.From.ID, normalizeLanguage(msg.From.LanguageCode))
	answer := strings.TrimSpace(msg.Text)
	if answer == "" {
		return true, a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "game.answer_empty"), telegram.ControlsKeyboard(lang))
	}
	if a.gameService == nil {
		return true, a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "game.not_ready"), telegram.ControlsKeyboard(lang))
	}
	result, err := a.gameService.Submit(ctx, msg.From.ID, sessionID, game.CompletionTyped, answer)
	if err != nil {
		return true, err
	}
	if err := a.state.ClearFSM(ctx, msg.From.ID); err != nil {
		return true, err
	}
	if result.Revealed {
		return true, a.sendRevealToPair(ctx, result)
	}
	return true, a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "game.waiting_partner"), telegram.ControlsKeyboard(lang))
}

func (a *App) handleGameCallback(ctx context.Context, cb *telegram.CallbackQuery, chatID int64, language string) error {
	action, rawID := parseGameCallback(cb.Data)
	isTestMode := false
	var testPrefix string
	if cb.Message != nil {
		if strings.HasPrefix(cb.Message.Caption, "[TEST]") {
			isTestMode = true
			parts := strings.Split(cb.Message.Caption, "\n")
			testPrefix = parts[0] + "\n\n"
		} else if strings.HasPrefix(cb.Message.Caption, "[ТЕСТ]") {
			isTestMode = true
			parts := strings.Split(cb.Message.Caption, "\n")
			testPrefix = parts[0] + "\n\n"
		}
	}

	if isTestMode {
		questionID := rawID
		if questionID == "" {
			questionID = a.defaultQuestionID(language)
		}
		if questionID == "" {
			return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(language, "game.no_cards"), telegram.MainMenuKeyboard(language))
		}
		controls := telegram.AdminTestKeyboard(language, questionID)
		var text string
		switch action {
		case "answer":
			if a.state != nil {
				if err := a.state.SetFSM(ctx, cb.From.ID, "game:await_answer:"+questionID, 24*time.Hour); err != nil {
					return err
				}
			}
			text = a.i18n.Text(language, "game.answer_prompt")
		case "skip":
			text = a.i18n.Text(language, "game.skipped_demo")
		case "in_person":
			text = a.i18n.Text(language, "game.in_person_demo")
		case "pause":
			if a.state != nil {
				if err := a.state.ClearFSM(ctx, cb.From.ID); err != nil {
					return err
				}
			}
			text = a.i18n.Text(language, "game.paused")
		default:
			return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(language, "game.card_stale"), telegram.MainMenuKeyboard(language))
		}
		return a.editCallbackScreen(ctx, cb, chatID, testPrefix+text, controls)
	}

	if a.gameService == nil {
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(language, "game.not_ready"), telegram.MainMenuKeyboard(language))
	}
	sessionID, err := parseInt64(rawID)
	if err != nil || sessionID <= 0 {
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(language, "game.card_stale"), telegram.MainMenuKeyboard(language))
	}

	controls := telegram.CardControlsForQuestion(language, rawID)
	var text string
	switch action {
	case "answer":
		if a.state != nil {
			if err := a.state.SetFSM(ctx, cb.From.ID, "game:await_answer:"+rawID, 24*time.Hour); err != nil {
				return err
			}
		}
		text = a.i18n.Text(language, "game.answer_prompt")
	case "skip":
		result, err := a.gameService.Submit(ctx, cb.From.ID, sessionID, game.CompletionSkip, "")
		if err != nil {
			return err
		}
		if a.state != nil {
			if err := a.state.ClearFSM(ctx, cb.From.ID); err != nil {
				return err
			}
		}
		if result.Revealed {
			return a.sendRevealToPair(ctx, result)
		}
		text = a.i18n.Text(language, "game.waiting_partner")
	case "in_person":
		result, err := a.gameService.Submit(ctx, cb.From.ID, sessionID, game.CompletionInPerson, "")
		if err != nil {
			return err
		}
		if a.state != nil {
			if err := a.state.ClearFSM(ctx, cb.From.ID); err != nil {
				return err
			}
		}
		if result.Revealed {
			return a.sendRevealToPair(ctx, result)
		}
		text = a.i18n.Text(language, "game.waiting_partner")
	case "pause":
		if a.state != nil {
			if err := a.state.ClearFSM(ctx, cb.From.ID); err != nil {
				return err
			}
		}
		text = a.i18n.Text(language, "game.paused")
	case "next":
		result, err := a.gameService.Next(ctx, cb.From.ID, sessionID)
		if err != nil {
			return err
		}
		if err := a.sendGameInvite(ctx, cb.From.ID, result.PartnerID, result.Session.ID); err != nil {
			return err
		}
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(language, "game.invite_sent"), telegram.MainMenuKeyboardWithPair(language, true))
	default:
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(language, "game.card_stale"), telegram.MainMenuKeyboard(language))
	}
	return a.editCallbackScreen(ctx, cb, chatID, text, controls)
}

func (a *App) handlePairingMessage(ctx context.Context, msg *telegram.Message) (bool, error) {
	if a.state == nil {
		return false, nil
	}
	fsm, err := a.state.GetFSM(ctx, msg.From.ID)
	if err != nil {
		return true, err
	}
	if fsm != "pairing:await_identifier" {
		return false, nil
	}
	lang := a.userLanguage(ctx, msg.From.ID, normalizeLanguage(msg.From.LanguageCode))
	if token, ok := pairing.TokenFromText(msg.Text); ok && strings.Contains(msg.Text, "pair_") {
		return true, a.showPairRequest(ctx, msg.Chat.ID, msg.From.ID, token)
	}
	var identifier pairing.Identifier
	var ok bool
	if msg.Contact != nil {
		identifier, ok = a.pairing.IdentifierFromContact(msg.Contact)
	} else {
		identifier, ok = a.pairing.IdentifierFromText(msg.Text)
	}
	if !ok {
		return true, a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "pair.invalid_identifier"), pairMenuKeyboard(lang))
	}
	request, err := a.createPairRequest(ctx, msg.From.ID, identifier)
	if err != nil {
		return true, a.pairError(ctx, msg.Chat.ID, msg.From.ID, err)
	}
	_ = a.state.ClearFSM(ctx, msg.From.ID)
	inviteURL := a.pairing.InviteURL(request.InviteToken)
	if identifier.TelegramID > 0 {
		targetLang := a.userLanguage(ctx, identifier.TelegramID, lang)
		if err := a.bot.SendMessage(ctx, msg.Chat.ID, fmt.Sprintf(a.i18n.Text(lang, "pair.request_sent"), inviteURL), telegram.PersistentKeyboard(lang)); err != nil {
			return true, err
		}
		_ = a.bot.SendMessage(ctx, identifier.TelegramID, fmt.Sprintf(a.i18n.Text(targetLang, "pair.incoming"), msg.From.FirstName), pairDecisionKeyboard(targetLang, request.InviteToken))
		return true, nil
	}
	return true, a.bot.SendMessage(ctx, msg.Chat.ID, fmt.Sprintf(a.i18n.Text(lang, "pair.invite_created"), inviteURL), a.pairing.PairingKeyboard(lang, request.InviteToken))
}

func (a *App) createPairRequest(ctx context.Context, requesterID int64, identifier pairing.Identifier) (storage.PairRequest, error) {
	if identifier.TelegramID == requesterID {
		return storage.PairRequest{}, storage.ErrSelfPair
	}
	token, err := a.pairing.InviteToken()
	if err != nil {
		return storage.PairRequest{}, err
	}
	request := storage.PairRequest{
		RequesterID: requesterID,
		InviteToken: token,
		ExpiresAt:   time.Now().UTC().Add(7 * 24 * time.Hour),
	}
	if identifier.TelegramID > 0 {
		request.TargetTelegramID.Valid = true
		request.TargetTelegramID.Int64 = identifier.TelegramID
	}
	if identifier.Username != "" && !strings.HasPrefix(identifier.Username, "token:") {
		request.TargetUsernameNormalized.Valid = true
		request.TargetUsernameNormalized.String = identifier.Username
	}
	if identifier.PhoneHash != "" {
		request.TargetPhoneHash.Valid = true
		request.TargetPhoneHash.String = identifier.PhoneHash
	}
	return a.repo.CreatePairRequest(ctx, request)
}

func (a *App) showPairRequest(ctx context.Context, chatID, userID int64, token string) error {
	lang := a.userLanguage(ctx, userID, "uk")
	request, err := a.repo.GetPairRequestByToken(ctx, token)
	if err != nil {
		return a.pairError(ctx, chatID, userID, err)
	}
	if request.RequesterID == userID {
		return a.bot.SendMessage(ctx, chatID, a.i18n.Text(lang, "pair.self_error"), telegram.PersistentKeyboard(lang))
	}
	return a.bot.SendMessage(ctx, chatID, a.i18n.Text(lang, "pair.accept_prompt"), pairDecisionKeyboard(lang, token))
}

func (a *App) acceptPairRequest(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, token string) error {
	lang := a.userLanguage(ctx, userID, "uk")
	request, err := a.repo.GetPairRequestByToken(ctx, token)
	if err != nil {
		return a.pairError(ctx, chatID, userID, err)
	}
	pair, err := a.repo.AcceptPairRequest(ctx, token, userID)
	if err != nil {
		return a.pairError(ctx, chatID, userID, err)
	}
	if a.state != nil {
		_ = a.state.ClearFSM(ctx, userID)
	}
	_ = a.bot.SendMessage(ctx, request.RequesterID, a.i18n.Text(a.userLanguage(ctx, request.RequesterID, lang), "pair.accepted"), telegram.MainMenuKeyboardWithPair(a.userLanguage(ctx, request.RequesterID, lang), true))
	return a.editCallbackScreen(ctx, cb, chatID, fmt.Sprintf(a.i18n.Text(lang, "pair.accepted_with_id"), pair.ID), telegram.MainMenuKeyboardWithPair(lang, true))
}

func (a *App) declinePairRequest(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, token string) error {
	lang := a.userLanguage(ctx, userID, "uk")
	if err := a.repo.DeclinePairRequest(ctx, token, userID); err != nil {
		return a.pairError(ctx, chatID, userID, err)
	}
	return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "pair.declined"), telegram.MainMenuKeyboardWithPair(lang, false))
}

func (a *App) acceptGameInvite(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, rawSessionID string) error {
	lang := a.userLanguage(ctx, userID, "uk")
	sessionID, err := parseInt64(rawSessionID)
	if err != nil || sessionID <= 0 {
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "game.card_stale"), telegram.MainMenuKeyboardWithPair(lang, true))
	}
	if a.gameService == nil {
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "game.not_ready"), telegram.MainMenuKeyboardWithPair(lang, true))
	}
	started, err := a.gameService.Accept(ctx, userID, sessionID)
	if errors.Is(err, game.ErrGameInviteExpired) {
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "game.invite_expired"), telegram.MainMenuKeyboardWithPair(lang, true))
	}
	if err != nil {
		return err
	}
	return a.sendGameCardToPair(ctx, started)
}

func (a *App) declineGameInvite(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, rawSessionID string) error {
	lang := a.userLanguage(ctx, userID, "uk")
	sessionID, err := parseInt64(rawSessionID)
	if err != nil || sessionID <= 0 {
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "game.card_stale"), telegram.MainMenuKeyboardWithPair(lang, true))
	}
	if a.gameService == nil {
		return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "game.not_ready"), telegram.MainMenuKeyboardWithPair(lang, true))
	}
	if err := a.gameService.Decline(ctx, userID, sessionID); err != nil {
		return err
	}
	return a.editCallbackScreen(ctx, cb, chatID, a.i18n.Text(lang, "game.invite_declined"), telegram.MainMenuKeyboardWithPair(lang, true))
}

func (a *App) sendGameInvite(ctx context.Context, inviterID, partnerID, sessionID int64) error {
	inviterLang := a.userLanguage(ctx, inviterID, "uk")
	partnerLang := a.userLanguage(ctx, partnerID, inviterLang)
	inviterName := a.displayNameOrFallback(ctx, inviterID, partnerLang, "partner")
	text := fmt.Sprintf(a.i18n.Text(partnerLang, "game.invite_incoming"), inviterName)
	return a.bot.SendMessage(ctx, partnerID, text, gameInviteKeyboard(partnerLang, sessionID))
}

func (a *App) sendGameCardToPair(ctx context.Context, started game.StartedResult) error {
	for _, userID := range []int64{started.Pair.UserAID, started.Pair.UserBID} {
		lang := a.userLanguage(ctx, userID, "uk")
		if err := a.sendGameCard(ctx, userID, lang, started.Card, started.Session.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) sendGameCard(ctx context.Context, chatID int64, language string, card content.Card, sessionID int64) error {
	text, ok := card.LocalizedText(language)
	if !ok {
		text, _ = card.LocalizedText("uk")
	}
	cardInput, err := a.themeCardInput(ctx, chatID, language)
	if err != nil {
		a.logger.Error("load theme input failed", "error", err, "user_id", chatID)
	}
	cardInput.Level = card.Level
	cardInput.Question = text
	rendered, err := a.renderer.Render(cardInput)
	if err != nil {
		return err
	}
	return a.bot.SendPhoto(ctx, chatID, rendered.PNG, "", telegram.CardControlsForQuestion(language, strconv.FormatInt(sessionID, 10)))
}

func (a *App) sendRevealToPair(ctx context.Context, result game.SubmitResult) error {
	session := result.Session
	for _, userID := range []int64{result.Pair.UserAID, result.Pair.UserBID} {
		lang := a.userLanguage(ctx, userID, "uk")
		if err := a.bot.SendMessage(ctx, userID, a.revealText(ctx, lang, result), nextCardKeyboard(lang, session.ID)); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) revealText(ctx context.Context, language string, result game.SubmitResult) string {
	lines := []string{a.i18n.Text(language, "game.revealed")}
	userIDs := make([]int64, 0, len(result.Answers))
	for userID := range result.Answers {
		userIDs = append(userIDs, userID)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })
	for _, userID := range userIDs {
		answer := result.Answers[userID]
		name := a.displayNameOrFallback(ctx, userID, language, "user")
		value := answer.AnswerText
		if value == "" {
			switch answer.Completion {
			case game.CompletionSkip:
				value = a.i18n.Text(language, "game.reveal_skipped")
			case game.CompletionInPerson:
				value = a.i18n.Text(language, "game.reveal_in_person")
			default:
				value = a.i18n.Text(language, "game.reveal_empty")
			}
		}
		lines = append(lines, fmt.Sprintf("%s: %s", name, value))
	}
	return strings.Join(lines, "\n")
}

func (a *App) pairError(ctx context.Context, chatID, userID int64, err error) error {
	lang := a.userLanguage(ctx, userID, "uk")
	key := "pair.error"
	switch {
	case errors.Is(err, storage.ErrSelfPair):
		key = "pair.self_error"
	case errors.Is(err, storage.ErrAlreadyPaired):
		key = "pair.already_paired"
	case errors.Is(err, storage.ErrPairRequestForbidden), errors.Is(err, storage.ErrPairRequestNotFound):
		key = "pair.not_found"
	}
	return a.bot.SendMessage(ctx, chatID, a.i18n.Text(lang, key), telegram.PersistentKeyboard(lang))
}

func (a *App) handleOnboardingText(ctx context.Context, msg *telegram.Message) (bool, error) {
	if a.state == nil {
		return false, nil
	}
	fsm, err := a.state.GetFSM(ctx, msg.From.ID)
	if err != nil {
		return true, err
	}
	lang := a.userLanguage(ctx, msg.From.ID, normalizeLanguage(msg.From.LanguageCode))
	switch onboarding.Step(fsm) {
	case onboarding.StepName:
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			return true, a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "onboarding.name"), telegram.PersistentKeyboard(lang))
		}
		if err := a.repo.UpdateUserName(ctx, msg.From.ID, name); err != nil {
			return true, err
		}
		if err := a.state.SetFSM(ctx, msg.From.ID, string(onboarding.StepGender), 24*time.Hour); err != nil {
			return true, err
		}
		return true, a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "onboarding.gender"), a.onboarding.GenderKeyboard(lang))
	case onboarding.StepThemeColor:
		color := strings.TrimSpace(msg.Text)
		if !strings.HasPrefix(color, "#") {
			color = "#" + color
		}
		if !isHexColor(color) {
			return true, a.bot.SendMessage(ctx, msg.Chat.ID, customColorPrompt(lang), customColorKeyboard(lang))
		}
		if err := a.repo.UpdateThemeColor(ctx, msg.From.ID, color); err != nil {
			return true, err
		}
		complete, err := a.repo.UserOnboardingComplete(ctx, msg.From.ID)
		if err != nil {
			return true, err
		}
		if !complete {
			if err := a.repo.MarkOnboardingComplete(ctx, msg.From.ID); err != nil {
				return true, err
			}
			if err := a.state.ClearFSM(ctx, msg.From.ID); err != nil {
				return true, err
			}
			return true, a.sendMainMenu(ctx, msg.Chat.ID, lang, themeSavedText(lang))
		}
		if err := a.state.ClearFSM(ctx, msg.From.ID); err != nil {
			return true, err
		}
		return true, a.sendThemeMenu(ctx, msg.Chat.ID, lang, themeSavedText(lang))
	case onboarding.StepBackground:
		if len(msg.Photo) == 0 {
			return true, a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "theme.upload_prompt"), nil)
		}
		return true, a.handleBackgroundUpload(ctx, msg, lang)
	default:
		return false, nil
	}
}

func (a *App) handleAdminText(ctx context.Context, msg *telegram.Message) (bool, error) {
	if a.state != nil {
		fsm, err := a.state.GetFSM(ctx, msg.From.ID)
		if err != nil {
			return true, err
		}
		if strings.HasPrefix(fsm, "admin:grant:") || strings.HasPrefix(fsm, "admin:revoke:") {
			action, unlockType, unlockID := parseAdminFSM(fsm)
			response, execErr := a.executeAdminAction(ctx, msg.From.ID, strings.TrimSpace(msg.Text), action, unlockType, unlockID)
			if execErr != nil {
				return true, a.bot.SendMessage(ctx, msg.Chat.ID, execErr.Error(), nil)
			}
			if err := a.state.ClearFSM(ctx, msg.From.ID); err != nil {
				return true, err
			}
			return true, a.bot.SendMessage(ctx, msg.Chat.ID, response, a.admin.Menu())
		}
	}
	action, ok, err := admin.ParseCommand(msg.Text)
	if err != nil {
		return true, a.bot.SendMessage(ctx, msg.Chat.ID, err.Error(), nil)
	}
	if !ok {
		return false, nil
	}
	response, execErr := a.executeAdminAction(ctx, msg.From.ID, action.TargetRef, action.Action, action.UnlockType, action.UnlockID)
	if execErr != nil {
		return true, a.bot.SendMessage(ctx, msg.Chat.ID, execErr.Error(), nil)
	}
	return true, a.bot.SendMessage(ctx, msg.Chat.ID, response, a.admin.Menu())
}

func (a *App) executeAdminAction(ctx context.Context, adminID int64, targetRef, action, unlockType, unlockID string) (string, error) {
	targetID, err := a.resolveAdminTarget(ctx, targetRef)
	if err != nil {
		return "", err
	}
	unlockType = normalizeAdminUnlockType(unlockType)
	if unlockType == storage.EntitlementPremiumAccess {
		unlockID = storage.EntitlementPremiumAccess
	}
	if err := a.validateAdminUnlock(unlockType, unlockID); err != nil {
		return "", err
	}
	switch action {
	case "grant":
		if err := a.repo.GrantEntitlement(ctx, storage.Entitlement{
			UserID:   targetID,
			Type:     unlockType,
			UnlockID: unlockID,
			Source:   "admin_grant",
		}); err != nil {
			return "", err
		}
	case "revoke":
		if err := a.repo.RevokeEntitlement(ctx, targetID, unlockType, unlockID); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unknown admin action %q", action)
	}
	if err := a.repo.LogAdminAction(ctx, adminID, targetID, action, unlockType, unlockID); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s:%s for %d", action, unlockType, unlockID, targetID), nil
}

func (a *App) resolveAdminTarget(ctx context.Context, targetRef string) (int64, error) {
	targetRef = strings.TrimSpace(targetRef)
	if targetRef == "" {
		return 0, errors.New("unknown target user")
	}
	if strings.HasPrefix(targetRef, "@") {
		userID, ok, err := a.repo.UserIDByUsername(ctx, targetRef)
		if err != nil {
			return 0, err
		}
		if !ok {
			return 0, fmt.Errorf("unknown target user %s", targetRef)
		}
		return userID, nil
	}
	targetID, err := parseInt64(targetRef)
	if err == nil {
		exists, err := a.repo.UserExists(ctx, targetID)
		if err != nil {
			return 0, err
		}
		if !exists {
			return 0, fmt.Errorf("unknown target user %d", targetID)
		}
		return targetID, nil
	}
	userID, ok, err := a.repo.UserIDByUsername(ctx, targetRef)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("unknown target user %s", targetRef)
	}
	return userID, nil
}

func (a *App) validateAdminUnlock(unlockType, unlockID string) error {
	switch unlockType {
	case storage.EntitlementPremiumAccess:
		if unlockID != storage.EntitlementPremiumAccess {
			return fmt.Errorf("premium unlock id must be %s", storage.EntitlementPremiumAccess)
		}
	case storage.EntitlementStyle:
		if a.styles == nil {
			return errors.New("style catalog is unavailable")
		}
		if _, ok := a.styles.Style(unlockID); !ok {
			return fmt.Errorf("unknown style %s", unlockID)
		}
	case storage.EntitlementFont:
		if a.fonts == nil {
			return errors.New("font catalog is unavailable")
		}
		if _, ok := a.fonts.Font(unlockID); !ok {
			return fmt.Errorf("unknown font %s", unlockID)
		}
	case storage.EntitlementBackground:
		if a.backgrounds == nil {
			return errors.New("background catalog is unavailable")
		}
		if _, ok := a.backgrounds.Background(unlockID); !ok {
			return fmt.Errorf("unknown background %s", unlockID)
		}
	default:
		return fmt.Errorf("unknown entitlement type %s", unlockType)
	}
	return nil
}

func normalizeAdminUnlockType(value string) string {
	if value == "premium" {
		return storage.EntitlementPremiumAccess
	}
	return value
}

func (a *App) handlePreCheckout(ctx context.Context, query *telegram.PreCheckoutQuery) error {
	if query.Currency != "XTR" {
		return a.bot.AnswerPreCheckoutQuery(ctx, query.ID, false, "Only Telegram Stars are supported.")
	}
	if _, _, err := payments.ParseInvoicePayload(query.InvoicePayload); err != nil {
		return a.bot.AnswerPreCheckoutQuery(ctx, query.ID, false, "Invalid invoice payload.")
	}
	return a.bot.AnswerPreCheckoutQuery(ctx, query.ID, true, "")
}

func (a *App) handleSuccessfulPayment(ctx context.Context, msg *telegram.Message) error {
	skuID, userID, err := payments.ParseInvoicePayload(msg.SuccessfulPayment.InvoicePayload)
	if err != nil {
		return err
	}
	if msg.From == nil || userID != msg.From.ID {
		return fmt.Errorf("payment payload user %d does not match payer", userID)
	}
	sku, ok := a.payments.SKU(skuID)
	if !ok {
		return fmt.Errorf("unknown sku %s", skuID)
	}
	receiptID, _, err := a.repo.StorePurchaseReceipt(ctx, storage.PurchaseReceipt{
		UserID:                  msg.From.ID,
		SKU:                     sku.ID,
		Currency:                msg.SuccessfulPayment.Currency,
		StarsAmount:             msg.SuccessfulPayment.TotalAmount,
		TelegramPaymentChargeID: msg.SuccessfulPayment.TelegramPaymentChargeID,
		ProviderPaymentChargeID: sql.NullString{String: msg.SuccessfulPayment.ProviderPaymentChargeID, Valid: msg.SuccessfulPayment.ProviderPaymentChargeID != ""},
	})
	if err != nil {
		return err
	}
	if err := a.repo.GrantEntitlement(ctx, storage.Entitlement{
		UserID:          msg.From.ID,
		Type:            sku.UnlockType,
		UnlockID:        sku.UnlockID,
		Source:          "purchase",
		SourceReceiptID: sql.NullInt64{Int64: receiptID, Valid: receiptID > 0},
	}); err != nil {
		return err
	}
	lang := a.userLanguage(ctx, msg.From.ID, normalizeLanguage(msg.From.LanguageCode))
	return a.sendMainMenu(ctx, msg.Chat.ID, lang, a.i18n.Text(lang, "payments.success"))
}

func (a *App) sendPremiumInvoice(ctx context.Context, chatID, userID int64, language string) error {
	sku, _ := a.payments.SKU(payments.PremiumSKU)
	title := a.i18n.Text(language, "payments.premium_title")
	if title == "" || title == "payments.premium_title" {
		title = sku.Title
	}
	desc := a.i18n.Text(language, "payments.premium_desc")
	if desc == "" || desc == "payments.premium_desc" {
		desc = sku.Description
	}
	return a.bot.SendInvoice(ctx, chatID, title, desc, payments.InvoicePayload(userID, sku.ID), sku.Stars, payments.PayKeyboard(language))
}

func (a *App) sendPaymentSupport(ctx context.Context, chatID int64, language string) error {
	text := a.i18n.Text(language, "payments.support")
	if a.cfg.Donation.MonobankURL != "" || a.cfg.Donation.CardNumber != "" {
		text += "\n\n" + a.supportText(language)
	}
	return a.bot.SendMessage(ctx, chatID, text, telegram.MainMenuKeyboard(language))
}

func (a *App) supportText(language string) string {
	if language == "uk" {
		return fmt.Sprintf("Підтримати автора можна тут: %s\nКартка: %s", a.cfg.Donation.MonobankURL, a.cfg.Donation.CardNumber)
	}
	return fmt.Sprintf("Support the author here: %s\nCard: %s", a.cfg.Donation.MonobankURL, a.cfg.Donation.CardNumber)
}

func (a *App) adminSortedCards() []content.Card {
	if a.deck == nil {
		return nil
	}
	cards := make([]content.Card, len(a.deck.Cards))
	copy(cards, a.deck.Cards)
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Level != cards[j].Level {
			return cards[i].Level < cards[j].Level
		}
		return cards[i].ID < cards[j].ID
	})
	return cards
}

func (a *App) adminTestCard(ctx context.Context, cb *telegram.CallbackQuery, chatID int64, language, afterID string) error {
	cards := a.adminSortedCards()
	if len(cards) == 0 {
		return a.bot.SendMessage(ctx, chatID, a.i18n.Text(language, "game.no_cards"), telegram.MainMenuKeyboard(language))
	}

	idx := 0
	if afterID != "" {
		found := false
		for i, c := range cards {
			if c.ID == afterID {
				idx = (i + 1) % len(cards)
				found = true
				break
			}
		}
		if !found {
			idx = 0
		}
	}
	card := cards[idx]

	text, ok := card.LocalizedText(language)
	if !ok {
		text, _ = card.LocalizedText("uk")
	}

	userID := chatID
	if cb != nil {
		userID = cb.From.ID
	}
	cardInput, err := a.themeCardInput(ctx, userID, language)
	if err != nil {
		a.logger.Error("load theme input failed", "error", err, "user_id", userID)
	}
	cardInput.Level = card.Level
	cardInput.Question = text

	rendered, err := a.renderer.Render(cardInput)
	if err != nil {
		return err
	}

	caption := fmt.Sprintf(a.i18n.Text(language, "game.admin_test_caption"), card.Level, card.ID)

	if cb != nil && cb.Message != nil && len(cb.Message.Photo) > 0 {
		return a.bot.EditMessageMedia(ctx, chatID, cb.Message.MessageID, rendered.PNG, caption, telegram.AdminTestKeyboard(language, card.ID))
	}

	return a.bot.SendPhoto(ctx, chatID, rendered.PNG, caption, telegram.AdminTestKeyboard(language, card.ID))
}

func (a *App) adminTestCardPrev(ctx context.Context, cb *telegram.CallbackQuery, chatID int64, language, currentID string) error {
	cards := a.adminSortedCards()
	if len(cards) == 0 {
		return a.bot.SendMessage(ctx, chatID, a.i18n.Text(language, "game.no_cards"), telegram.MainMenuKeyboard(language))
	}

	idx := len(cards) - 1
	if currentID != "" {
		found := false
		for i, c := range cards {
			if c.ID == currentID {
				idx = (i - 1 + len(cards)) % len(cards)
				found = true
				break
			}
		}
		if !found {
			idx = len(cards) - 1
		}
	}
	card := cards[idx]

	text, ok := card.LocalizedText(language)
	if !ok {
		text, _ = card.LocalizedText("uk")
	}

	userID := chatID
	if cb != nil {
		userID = cb.From.ID
	}
	cardInput, err := a.themeCardInput(ctx, userID, language)
	if err != nil {
		a.logger.Error("load theme input failed", "error", err, "user_id", userID)
	}
	cardInput.Level = card.Level
	cardInput.Question = text

	rendered, err := a.renderer.Render(cardInput)
	if err != nil {
		return err
	}

	caption := fmt.Sprintf(a.i18n.Text(language, "game.admin_test_caption"), card.Level, card.ID)

	if cb != nil && cb.Message != nil && len(cb.Message.Photo) > 0 {
		return a.bot.EditMessageMedia(ctx, chatID, cb.Message.MessageID, rendered.PNG, caption, telegram.AdminTestKeyboard(language, card.ID))
	}

	return a.bot.SendPhoto(ctx, chatID, rendered.PNG, caption, telegram.AdminTestKeyboard(language, card.ID))
}

func (a *App) sendMainMenu(ctx context.Context, chatID int64, language, text string) error {
	menuText, hasPair := a.buildMainMenuText(ctx, chatID, language)
	if strings.TrimSpace(text) == "" || text == a.i18n.Text(language, "menu.title") {
		text = menuText
	} else {
		text = strings.TrimSpace(text) + "\n\n" + menuText
	}
	return a.bot.SendMessage(ctx, chatID, text, telegram.MainMenuKeyboardWithPair(language, hasPair))
}

func (a *App) editCallbackScreen(ctx context.Context, cb *telegram.CallbackQuery, chatID int64, text string, replyMarkup any) error {
	if cb != nil && cb.Message != nil {
		if len(cb.Message.Photo) > 0 {
			if !strings.HasPrefix(cb.Data, "game:") {
				_ = a.bot.DeleteMessage(ctx, chatID, cb.Message.MessageID)
				return a.bot.SendMessage(ctx, chatID, text, replyMarkup)
			}
			err := a.bot.EditMessageCaption(ctx, chatID, cb.Message.MessageID, text, replyMarkup)
			if err == nil {
				return nil
			}
			a.logger.Warn("callback edit caption failed; sending fallback message", "chat_id", chatID, "message_id", cb.Message.MessageID, "err", err)
		} else {
			err := a.bot.EditMessageText(ctx, chatID, cb.Message.MessageID, text, replyMarkup)
			if err == nil {
				return nil
			}
			a.logger.Warn("callback edit text failed; sending fallback message", "chat_id", chatID, "message_id", cb.Message.MessageID, "err", err)
		}
	}
	return a.bot.SendMessage(ctx, chatID, text, replyMarkup)
}

func (a *App) defaultQuestionID(language string) string {
	if a.deck == nil {
		return ""
	}
	cards := a.deck.EligibleCards(content.Eligibility{Level: 1})
	if len(cards) == 0 {
		return ""
	}
	return cards[0].ID
}

func (a *App) ensureTelegramUser(ctx context.Context, user telegram.User) error {
	return a.repo.EnsureUser(ctx, storage.User{
		TelegramID:      user.ID,
		Username:        user.Username,
		DisplayName:     strings.TrimSpace(user.FirstName + " " + user.LastName),
		Language:        normalizeLanguage(user.LanguageCode),
		SelectedStyleID: "default_warm",
		ThemeBaseColor:  "#d98c9f",
	})
}

func (a *App) userLanguage(ctx context.Context, userID int64, fallback string) string {
	if a.repo == nil {
		return fallback
	}
	language, err := a.repo.UserLanguage(ctx, userID)
	if err != nil || language == "" {
		return fallback
	}
	return normalizeLanguage(language)
}

func (a *App) buildMainMenuText(ctx context.Context, userID int64, language string) (string, bool) {
	if a.repo == nil {
		return a.i18n.Text(language, "menu.title"), false
	}
	profile, err := a.repo.UserProfile(ctx, userID)
	if err != nil {
		a.logger.Warn("load menu profile failed", "user_id", userID, "err", err)
		return a.i18n.Text(language, "menu.title"), false
	}
	displayName := strings.TrimSpace(profile.DisplayName)
	if displayName == "" {
		displayName = a.i18n.Text(language, "menu.profile_fallback")
	}
	themeColor := strings.TrimSpace(profile.ThemeBaseColor)
	if themeColor == "" {
		themeColor = "#d98c9f"
	}
	lines := []string{
		a.i18n.Text(language, "menu.header"),
		"",
		fmt.Sprintf("👤 %s", displayName),
		fmt.Sprintf("🎨 %s", themeColor),
		"",
	}
	pair, err := a.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		a.logger.Warn("load menu pair failed", "user_id", userID, "err", err)
		lines = append(lines, a.i18n.Text(language, "menu.status_unpaired"))
	} else if pair != nil {
		partnerID := pair.UserAID
		if partnerID == userID {
			partnerID = pair.UserBID
		}
		partnerName := a.displayNameOrFallback(ctx, partnerID, language, "partner")
		lines = append(lines, fmt.Sprintf(a.i18n.Text(language, "menu.status_paired"), partnerName))
		total := a.levelCardTotal(pair.ActiveLevel)
		if completed, err := a.repo.PairCardCount(ctx, pair.ID, pair.ActiveLevel); err == nil && total > 0 {
			lines = append(lines, fmt.Sprintf(a.i18n.Text(language, "menu.progress"), pair.ActiveLevel, completed, total))
		} else {
			lines = append(lines, fmt.Sprintf(a.i18n.Text(language, "menu.level"), pair.ActiveLevel))
		}
		lines = append(lines, "", a.i18n.Text(language, "menu.prompt"))
		return strings.Join(lines, "\n"), true
	} else {
		lines = append(lines, a.i18n.Text(language, "menu.status_unpaired"), a.i18n.Text(language, "menu.status_unpaired_hint"))
	}
	lines = append(lines, "", a.i18n.Text(language, "menu.prompt"))
	return strings.Join(lines, "\n"), false
}

func (a *App) levelCardTotal(level int) int {
	if a.deck == nil {
		return 0
	}
	return len(a.deck.EligibleCards(content.Eligibility{Level: level}))
}

func (a *App) userHasPair(ctx context.Context, userID int64) bool {
	if a.repo == nil {
		return false
	}
	pair, err := a.repo.ActivePairForUser(ctx, userID)
	return err == nil && pair != nil
}

func (a *App) activePairText(ctx context.Context, language string, pair *storage.Pair) string {
	userA := a.displayNameOrFallback(ctx, pair.UserAID, language, "user")
	userB := a.displayNameOrFallback(ctx, pair.UserBID, language, "partner")
	return fmt.Sprintf(a.i18n.Text(language, "pair.active"), userA, userB)
}

func (a *App) displayNameOrFallback(ctx context.Context, userID int64, language, fallbackKind string) string {
	name, err := a.repo.UserDisplayName(ctx, userID)
	if err != nil {
		a.logger.Warn("load display name failed", "user_id", userID, "err", err)
	}
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	if fallbackKind == "user" {
		return a.i18n.Text(language, "menu.profile_fallback")
	}
	return a.i18n.Text(language, "menu.partner_fallback")
}

func pairDecisionKeyboard(language, token string) telegram.InlineKeyboardMarkup {
	accept := "Accept"
	decline := "Decline"
	if language == "uk" {
		accept = "Прийняти"
		decline = "Відхилити"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: accept, CallbackData: "pair:accept:" + token}, {Text: decline, CallbackData: "pair:decline:" + token}},
	}}
}

func gameInviteKeyboard(language string, sessionID int64) telegram.InlineKeyboardMarkup {
	accept := "Yes, start"
	decline := "Not now"
	if language == "uk" {
		accept = "Так, почати"
		decline = "Не зараз"
	}
	rawID := strconv.FormatInt(sessionID, 10)
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: accept, CallbackData: "game:accept:" + rawID}, {Text: decline, CallbackData: "game:decline:" + rawID}},
	}}
}

func nextCardKeyboard(language string, sessionID int64) telegram.InlineKeyboardMarkup {
	next := "Next card"
	menu := "Menu"
	if language == "uk" {
		next = "Наступна картка"
		menu = "Меню"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: next, CallbackData: "game:next:" + strconv.FormatInt(sessionID, 10)}},
		{{Text: menu, CallbackData: "menu:main"}},
	}}
}

func pairMenuKeyboard(language string) telegram.InlineKeyboardMarkup {
	back := "Back to menu"
	if language == "uk" {
		back = "Назад до меню"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: back, CallbackData: "menu:main"}},
	}}
}

func customColorKeyboard(language string) telegram.InlineKeyboardMarkup {
	back := "Back to presets"
	menu := "Main menu"
	if language == "uk" {
		back = "Назад до кольорів"
		menu = "Головне меню"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: back, CallbackData: "theme:menu"}},
		{{Text: menu, CallbackData: "menu:main"}},
	}}
}

func menuPanelText(language, panel string) string {
	if language == "uk" {
		switch panel {
		case "pair":
			return "Пара\n\nТут буде прив'язка партнера через контакт, username, ID або запрошення."
		case "theme":
			return "Тема\n\nОбери базовий колір карток."
		case "journal":
			return "Журнал\n\nВідповіді з'являться тут після спільних відкриттів карток."
		case "settings":
			return "Налаштування\n\nМожна змінити мову, скинути поточну дію або видалити акаунт."
		}
	}
	switch panel {
	case "pair":
		return "Pair\n\nPartner binding by contact, username, ID, or invite link will live here."
	case "theme":
		return "Theme\n\nChoose the base color for your cards."
	case "journal":
		return "Journal\n\nAnswers will appear here after shared card reveals."
	case "settings":
		return "Settings\n\nChange language, reset the current action, or delete your account."
	default:
		return "Menu"
	}
}

func settingsKeyboard(language string, bundle *i18n.Bundle, isAdmin bool) telegram.InlineKeyboardMarkup {
	var rows [][]telegram.InlineKeyboardButton

	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: bundle.Text(language, "settings.change_language"), CallbackData: "onboarding:language_menu"},
	})
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: bundle.Text(language, "settings.custom_questions"), CallbackData: "custom_questions:menu"},
	})
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: bundle.Text(language, "settings.delete_account"), CallbackData: "settings:delete_account"},
	})

	if isAdmin {
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: bundle.Text(language, "settings.test_cards"), CallbackData: "settings:test_cards"},
		})
	}

	mainMenuText := "Main menu"
	if language == "uk" {
		mainMenuText = "Головне меню"
	}
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: mainMenuText, CallbackData: "menu:main"},
	})

	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func deleteAccountKeyboard(language string, bundle *i18n.Bundle) telegram.InlineKeyboardMarkup {
	cancel := "Cancel"
	if language == "uk" {
		cancel = "Скасувати"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: bundle.Text(language, "settings.delete_confirm_button"), CallbackData: "settings:delete_confirm"}},
		{{Text: cancel, CallbackData: "menu:main"}},
	}}
}

func customColorPrompt(language string) string {
	if language == "uk" {
		return "Надішли HEX-колір у форматі #d98c9f."
	}
	return "Send a HEX color like #d98c9f."
}

func themeSavedText(language string) string {
	if language == "uk" {
		return "Колір збережено. Головне меню"
	}
	return "Theme color saved. Main menu"
}

func normalizeLanguage(language string) string {
	if strings.HasPrefix(strings.ToLower(language), "en") {
		return "en"
	}
	return "uk"
}

func isHexColor(color string) bool {
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for _, r := range color[1:] {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

func isResetText(text string) bool {
	text = strings.TrimSpace(strings.ToLower(text))
	return text == "/cancel" || text == "/reset" || text == strings.ToLower(telegram.CancelResetText) || text == strings.ToLower(telegram.CancelResetTextUK)
}

func isMenuText(text string) bool {
	text = strings.TrimSpace(strings.ToLower(text))
	return text == "/menu" || text == strings.ToLower(telegram.MenuTextEN) || text == strings.ToLower(telegram.MenuTextUK)
}

func parseGameCallback(data string) (action, questionID string) {
	parts := strings.Split(data, ":")
	if len(parts) < 2 || parts[0] != "game" {
		return "", ""
	}
	action = parts[1]
	if len(parts) >= 3 {
		questionID = strings.TrimSpace(parts[2])
	}
	return action, questionID
}

func gameSessionFromFSM(fsm string) (int64, bool) {
	const prefix = "game:await_answer:"
	if !strings.HasPrefix(fsm, prefix) {
		return 0, false
	}
	sessionID, err := parseInt64(strings.TrimPrefix(fsm, prefix))
	return sessionID, err == nil && sessionID > 0
}

func parseAdminFSM(fsm string) (action, unlockType, unlockID string) {
	parts := strings.Split(fsm, ":")
	if len(parts) >= 4 {
		return parts[1], parts[2], parts[3]
	}
	return "", "", ""
}

func parseInt64(raw string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
}

func (a *App) userHasThemeEntitlement(ctx context.Context, userID int64, entType, unlockID string) (bool, error) {
	hasPremium, err := a.repo.UserHasEntitlement(ctx, userID, storage.EntitlementPremiumAccess, "premium_access")
	if err != nil {
		return false, err
	}
	if hasPremium {
		return true, nil
	}
	return a.repo.UserHasEntitlement(ctx, userID, entType, unlockID)
}

func (a *App) userHasThemeEntitlementBool(ctx context.Context, userID int64, entType, unlockID string) bool {
	ok, _ := a.userHasThemeEntitlement(ctx, userID, entType, unlockID)
	return ok
}

func (a *App) themeCardInput(ctx context.Context, userID int64, language string) (render.CardInput, error) {
	input := render.CardInput{
		Brand:     a.i18n.Brand(language),
		BaseColor: "#d98c9f",
	}

	profile, err := a.repo.UserProfile(ctx, userID)
	if err != nil {
		return input, err
	}

	// 1. Font Selection
	fontPath := ""
	if profile.SelectedFontID != "" && a.fonts != nil {
		if font, ok := a.fonts.Font(profile.SelectedFontID); ok {
			if !font.Premium || a.userHasThemeEntitlementBool(ctx, userID, storage.EntitlementFont, font.ID) {
				fontPath = font.Path
			}
		}
	}
	input.FontPath = fontPath

	// 2. Style & Overrides Selection
	var borderRadius float64
	var glassOpacity float64
	baseColor := "#d98c9f"

	styleID := profile.SelectedStyleID
	if styleID == "" {
		styleID = "default_warm"
	}
	if a.styles != nil {
		if style, ok := a.styles.Style(styleID); ok {
			targetStyle := style
			if style.Premium && !a.userHasThemeEntitlementBool(ctx, userID, storage.EntitlementStyle, style.ID) {
				if defStyle, ok := a.styles.Style("default_warm"); ok {
					targetStyle = defStyle
				}
			}
			borderRadius = targetStyle.Tokens.BorderRadius
			glassOpacity = targetStyle.Tokens.GlassOpacity
			baseColor = targetStyle.Tokens.DefaultColor
		}
	}

	if profile.ThemeBaseColor != "" {
		baseColor = profile.ThemeBaseColor
	}
	if profile.CustomBorderRadius.Valid {
		borderRadius = float64(profile.CustomBorderRadius.Int64)
	}
	if profile.CustomGlassOpacity.Valid {
		glassOpacity = profile.CustomGlassOpacity.Float64
	}

	input.BaseColor = baseColor
	input.BorderRadius = borderRadius
	input.GlassOpacity = glassOpacity

	// 3. Background Image Selection
	var bgBytes []byte
	if profile.SelectedBackgroundAssetID != "" && a.backgrounds != nil {
		if bg, ok := a.backgrounds.Background(profile.SelectedBackgroundAssetID); ok {
			// Built-in background
			allowed := !bg.Premium || a.userHasThemeEntitlementBool(ctx, userID, storage.EntitlementBackground, bg.ID)
			if allowed {
				if a.objectStore != nil {
					bgBytes, _ = a.objectStore.Get(ctx, bg.ObjectKey)
				}
				if len(bgBytes) == 0 {
					// Fallback to local file read & seed
					localPath := filepath.Join("assets/backgrounds", filepath.Base(bg.ObjectKey))
					if data, err := os.ReadFile(localPath); err == nil {
						bgBytes = data
						if a.objectStore != nil {
							_ = a.objectStore.Put(ctx, bg.ObjectKey, "image/webp", data)
						}
					}
				}
			}
		} else {
			// User uploaded background (verify owner is correct if status is active)
			if asset, err := a.repo.GetThemeAsset(ctx, profile.SelectedBackgroundAssetID); err == nil && asset.Status == "active" {
				if asset.OwnerUserID == userID && a.objectStore != nil {
					bgBytes, _ = a.objectStore.Get(ctx, asset.MinioObjectKey)
				}
			}
		}
	}
	input.BackgroundBytes = bgBytes

	return input, nil
}

func (a *App) themeMenu(ctx context.Context, cb *telegram.CallbackQuery, chatID int64, language string) error {
	profile, err := a.repo.UserProfile(ctx, chatID)
	if err != nil {
		return err
	}

	colorStr := profile.ThemeBaseColor
	if colorStr == "" {
		colorStr = "#d98c9f (default)"
	}

	styleName := "Warm Basic (default)"
	if a.styles != nil {
		if style, ok := a.styles.Style(profile.SelectedStyleID); ok {
			styleName = style.Name[language]
		}
	}

	fontName := "Nunito (default)"
	if a.fonts != nil {
		if font, ok := a.fonts.Font(profile.SelectedFontID); ok {
			fontName = font.Name[language]
		}
	}

	bgName := "Default Gradient"
	if profile.SelectedBackgroundAssetID != "" {
		if a.backgrounds != nil {
			if bg, ok := a.backgrounds.Background(profile.SelectedBackgroundAssetID); ok {
				bgName = bg.Name[language]
			} else {
				bgName = "Custom Background"
			}
		}
	}

	currentSettingsPattern := a.i18n.Text(language, "theme.current_settings")
	if currentSettingsPattern == "theme.current_settings" || !strings.Contains(currentSettingsPattern, "%s") {
		currentSettingsPattern = "Current Theme Settings:\n🎨 Color: %s\n✨ Style: %s\n🔤 Font: %s\n🖼 Background: %s"
	}
	text := fmt.Sprintf(currentSettingsPattern, colorStr, styleName, fontName, bgName)

	return a.editCallbackScreen(ctx, cb, chatID, text, a.themeMenuKeyboard(language))
}

func (a *App) sendThemeMenu(ctx context.Context, chatID int64, language, text string) error {
	profile, err := a.repo.UserProfile(ctx, chatID)
	if err != nil {
		return err
	}

	colorStr := profile.ThemeBaseColor
	if colorStr == "" {
		colorStr = "#d98c9f (default)"
	}

	styleName := "Warm Basic (default)"
	if a.styles != nil {
		if style, ok := a.styles.Style(profile.SelectedStyleID); ok {
			styleName = style.Name[language]
		}
	}

	fontName := "Nunito (default)"
	if a.fonts != nil {
		if font, ok := a.fonts.Font(profile.SelectedFontID); ok {
			fontName = font.Name[language]
		}
	}

	bgName := "Default Gradient"
	if profile.SelectedBackgroundAssetID != "" {
		if a.backgrounds != nil {
			if bg, ok := a.backgrounds.Background(profile.SelectedBackgroundAssetID); ok {
				bgName = bg.Name[language]
			} else {
				bgName = "Custom Background"
			}
		}
	}

	currentSettingsPattern := a.i18n.Text(language, "theme.current_settings")
	if currentSettingsPattern == "theme.current_settings" || !strings.Contains(currentSettingsPattern, "%s") {
		currentSettingsPattern = "Current Theme Settings:\n🎨 Color: %s\n✨ Style: %s\n🔤 Font: %s\n🖼 Background: %s"
	}
	themeText := fmt.Sprintf(currentSettingsPattern, colorStr, styleName, fontName, bgName)
	if text != "" {
		themeText = text + "\n\n" + themeText
	}

	return a.bot.SendMessage(ctx, chatID, themeText, a.themeMenuKeyboard(language))
}

func (a *App) themeMenuKeyboard(language string) telegram.InlineKeyboardMarkup {
	btnColor := "🎨 Color"
	btnStyle := "✨ Style"
	btnFont := "🔤 Font"
	btnBg := "🖼 Background"
	btnBack := "« Back"
	if language == "uk" {
		btnColor = "🎨 Колір"
		btnStyle = "✨ Стиль"
		btnFont = "🔤 Шрифт"
		btnBg = "🖼 Фон"
		btnBack = "« Назад"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: btnColor, CallbackData: "theme:color:menu"}, {Text: btnStyle, CallbackData: "theme:style:menu"}},
		{{Text: btnFont, CallbackData: "theme:font:menu"}, {Text: btnBg, CallbackData: "theme:bg:menu"}},
		{{Text: btnBack, CallbackData: "settings:open"}},
	}}
}

func (a *App) themeColorKeyboard(language string) telegram.InlineKeyboardMarkup {
	swatches := []telegram.InlineKeyboardButton{
		{Text: "Rose", CallbackData: "theme:color:#d98c9f"},
		{Text: "Wine", CallbackData: "theme:color:#8f3f5f"},
		{Text: "Peach", CallbackData: "theme:color:#e4a07a"},
		{Text: "Sage", CallbackData: "theme:color:#8da68f"},
	}
	customHex := "Custom HEX"
	btnBack := "« Back"
	if language == "uk" {
		swatches[0].Text = "Рожевий"
		swatches[1].Text = "Вино"
		swatches[2].Text = "Персик"
		swatches[3].Text = "Шавлія"
		customHex = "Свій HEX"
		btnBack = "« Назад"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{swatches[0], swatches[1]},
		{swatches[2], swatches[3]},
		{{Text: customHex, CallbackData: "theme:color:custom"}},
		{{Text: btnBack, CallbackData: "theme:menu"}},
	}}
}

func (a *App) themeStyleKeyboard(ctx context.Context, userID int64, language string) telegram.InlineKeyboardMarkup {
	var rows [][]telegram.InlineKeyboardButton
	if a.styles != nil {
		for _, style := range a.styles.Styles {
			label := style.Name[language]
			if style.Premium {
				unlocked, _ := a.userHasThemeEntitlement(ctx, userID, storage.EntitlementStyle, style.ID)
				if unlocked {
					label = "✨ " + label + " (Premium ✅)"
				} else {
					label = "🔒 " + label + " (Premium)"
				}
			}
			rows = append(rows, []telegram.InlineKeyboardButton{
				{Text: label, CallbackData: "theme:style:select:" + style.ID},
			})
		}
	}
	btnEdit := "🛠 Edit Style Properties"
	btnBack := "« Back"
	if language == "uk" {
		btnEdit = "🛠 Редагувати властивості стилю"
		btnBack = "« Назад"
	}
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: btnEdit, CallbackData: "theme:style:edit"},
	})
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: btnBack, CallbackData: "theme:menu"},
	})
	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (a *App) themeStyleEditKeyboard(ctx context.Context, userID int64, language string) telegram.InlineKeyboardMarkup {
	radiusLabel := "Border Radius:"
	opacityLabel := "Glass Opacity:"
	btnBack := "« Back"
	if language == "uk" {
		radiusLabel = "Радіус рамки:"
		opacityLabel = "Прозорість скла:"
		btnBack = "« Назад"
	}

	var rows [][]telegram.InlineKeyboardButton
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: "--- " + radiusLabel + " ---", CallbackData: "theme:style:edit_nop"}})
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: "Default", CallbackData: "theme:style:set_border:default"},
		{Text: "0px", CallbackData: "theme:style:set_border:0"},
		{Text: "15px", CallbackData: "theme:style:set_border:15"},
		{Text: "30px", CallbackData: "theme:style:set_border:30"},
		{Text: "45px", CallbackData: "theme:style:set_border:45"},
	})
	rows = append(rows, []telegram.InlineKeyboardButton{{Text: "--- " + opacityLabel + " ---", CallbackData: "theme:style:edit_nop"}})
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: "Default", CallbackData: "theme:style:set_glass:default"},
		{Text: "20%", CallbackData: "theme:style:set_glass:0.2"},
		{Text: "40%", CallbackData: "theme:style:set_glass:0.4"},
		{Text: "60%", CallbackData: "theme:style:set_glass:0.6"},
		{Text: "80%", CallbackData: "theme:style:set_glass:0.8"},
		{Text: "100%", CallbackData: "theme:style:set_glass:1.0"},
	})
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: btnBack, CallbackData: "theme:style:menu"},
	})
	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (a *App) themeFontKeyboard(ctx context.Context, userID int64, language string) telegram.InlineKeyboardMarkup {
	var rows [][]telegram.InlineKeyboardButton
	if a.fonts != nil {
		for _, font := range a.fonts.Fonts {
			label := font.Name[language]
			if font.Premium {
				unlocked, _ := a.userHasThemeEntitlement(ctx, userID, storage.EntitlementFont, font.ID)
				if unlocked {
					label = "🔤 " + label + " (Premium ✅)"
				} else {
					label = "🔒 " + label + " (Premium)"
				}
			}
			rows = append(rows, []telegram.InlineKeyboardButton{
				{Text: label, CallbackData: "theme:font:select:" + font.ID},
			})
		}
	}
	btnBack := "« Back"
	if language == "uk" {
		btnBack = "« Назад"
	}
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: btnBack, CallbackData: "theme:menu"},
	})
	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (a *App) themeBgKeyboard(ctx context.Context, userID int64, language string) telegram.InlineKeyboardMarkup {
	var rows [][]telegram.InlineKeyboardButton

	if a.backgrounds != nil {
		for _, bg := range a.backgrounds.Backgrounds {
			label := bg.Name[language]
			if bg.Premium {
				unlocked, _ := a.userHasThemeEntitlement(ctx, userID, storage.EntitlementBackground, bg.ID)
				if unlocked {
					label = "🖼 " + label + " (Premium ✅)"
				} else {
					label = "🔒 " + label + " (Premium)"
				}
			}
			rows = append(rows, []telegram.InlineKeyboardButton{
				{Text: label, CallbackData: "theme:bg:select:" + bg.ID},
			})
		}
	}

	defLabel := "Default dynamic gradient"
	if language == "uk" {
		defLabel = "Стандартний градієнт"
	}
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: defLabel, CallbackData: "theme:bg:select:default"},
	})

	uploads, err := a.repo.GetUserUploadedBackgrounds(ctx, userID)
	if err == nil && len(uploads) > 0 {
		for i, upload := range uploads {
			upName := fmt.Sprintf("Uploaded Background %d", i+1)
			deleteBtn := "❌ Delete"
			if language == "uk" {
				upName = fmt.Sprintf("Завантажений фон %d", i+1)
				deleteBtn = "❌ Видалити"
			}
			rows = append(rows, []telegram.InlineKeyboardButton{
				{Text: upName, CallbackData: "theme:bg:select:" + upload.ID},
				{Text: deleteBtn, CallbackData: "theme:bg:delete:" + upload.ID},
			})
		}
	}

	if len(uploads) < 3 {
		upBtn := "📤 Upload Custom Background"
		if language == "uk" {
			upBtn = "📤 Завантажити свій фон"
		}
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: upBtn, CallbackData: "theme:bg:upload"},
		})
	}

	btnBack := "« Back"
	if language == "uk" {
		btnBack = "« Назад"
	}
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: btnBack, CallbackData: "theme:menu"},
	})
	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func premiumLockKeyboard(language string) telegram.InlineKeyboardMarkup {
	btnBuy := "💎 Buy Premium (250 Stars)"
	btnBack := "« Back"
	if language == "uk" {
		btnBuy = "💎 Купити Преміум (250 Stars)"
		btnBack = "« Назад"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: btnBuy, CallbackData: "store:open"}},
		{{Text: btnBack, CallbackData: "theme:menu"}},
	}}
}

func (a *App) handleBackgroundUpload(ctx context.Context, msg *telegram.Message, lang string) error {
	if len(msg.Photo) == 0 {
		return a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "theme.upload_prompt"), nil)
	}

	photo := msg.Photo[len(msg.Photo)-1]

	if photo.FileSize > render.DefaultMaxUploadBytes {
		return a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "theme.upload_failed"), nil)
	}

	tgFile, err := a.bot.GetFile(ctx, photo.FileID)
	if err != nil {
		a.logger.Error("telegram getFile failed", "error", err, "file_id", photo.FileID)
		return a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "theme.upload_failed"), nil)
	}

	data, err := a.bot.DownloadFile(ctx, tgFile.FilePath)
	if err != nil {
		a.logger.Error("telegram download file failed", "error", err, "path", tgFile.FilePath)
		return a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "theme.upload_failed"), nil)
	}

	processed, err := render.ProcessUploadedBackground(bytes.NewReader(data), render.UploadOptions{
		UserID: msg.From.ID,
	})
	if err != nil {
		a.logger.Error("process uploaded background failed", "error", err)
		return a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "theme.upload_failed"), nil)
	}

	if a.objectStore != nil {
		err = a.objectStore.Put(ctx, processed.ObjectKey, processed.MIMEType, processed.Bytes)
		if err != nil {
			a.logger.Error("minio put failed for custom background", "error", err, "key", processed.ObjectKey)
			return a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "theme.upload_failed"), nil)
		}
	}

	assetID := fmt.Sprintf("upload_%d_%d", msg.From.ID, time.Now().UnixNano())
	err = a.repo.CreateThemeAsset(ctx, assetID, msg.From.ID, processed.ObjectKey, int64(processed.SizeBytes), processed.Width, processed.Height)
	if err != nil {
		a.logger.Error("create theme asset failed", "error", err)
		if a.objectStore != nil {
			_ = a.objectStore.Delete(ctx, processed.ObjectKey)
		}
		return a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "theme.upload_failed"), nil)
	}

	if err := a.repo.UpdateUserBackground(ctx, msg.From.ID, assetID); err != nil {
		a.logger.Error("select custom background failed", "error", err)
	}

	if a.state != nil {
		_ = a.state.ClearFSM(ctx, msg.From.ID)
	}

	return a.sendThemeMenu(ctx, msg.Chat.ID, lang, a.i18n.Text(lang, "theme.upload_success"))
}

func (a *App) handleCustomQuestionMessage(ctx context.Context, msg *telegram.Message) (bool, error) {
	if a.state == nil {
		return false, nil
	}
	fsm, err := a.state.GetFSM(ctx, msg.From.ID)
	if err != nil {
		return true, err
	}
	if fsm != "custom_question:await_text" {
		return false, nil
	}
	lang := a.userLanguage(ctx, msg.From.ID, normalizeLanguage(msg.From.LanguageCode))
	text := strings.TrimSpace(msg.Text)
	if text == "" || len([]rune(text)) > 200 {
		return true, a.bot.SendMessage(ctx, msg.Chat.ID, a.i18n.Text(lang, "custom_questions.invalid_length"), telegram.PersistentKeyboard(lang))
	}
	_, err = a.repo.CreateCustomQuestion(ctx, msg.From.ID, text)
	if err != nil {
		return true, err
	}
	_ = a.state.ClearFSM(ctx, msg.From.ID)
	return true, a.renderCustomQuestionsMenu(ctx, nil, msg.Chat.ID, msg.From.ID, lang, a.i18n.Text(lang, "custom_questions.success_added"))
}

func (a *App) renderCustomQuestionsMenu(ctx context.Context, cb *telegram.CallbackQuery, chatID, userID int64, language string, prefixText string) error {
	questions, err := a.repo.GetPairCustomQuestions(ctx, userID)
	if err != nil {
		return err
	}

	var listLines []string
	var rows [][]telegram.InlineKeyboardButton
	if len(questions) > 0 {
		for i, q := range questions {
			listLines = append(listLines, fmt.Sprintf("%d. %s", i+1, q.QuestionText))

			if q.CreatorID == userID {
				deleteText := fmt.Sprintf("❌ %d", i+1)
				rows = append(rows, []telegram.InlineKeyboardButton{
					{Text: fmt.Sprintf("%d. %s", i+1, truncateString(q.QuestionText, 25)), CallbackData: "custom_questions:noop"},
					{Text: deleteText, CallbackData: fmt.Sprintf("custom_questions:delete:%d", q.ID)},
				})
			} else {
				rows = append(rows, []telegram.InlineKeyboardButton{
					{Text: fmt.Sprintf("%d. %s", i+1, truncateString(q.QuestionText, 30)), CallbackData: "custom_questions:noop"},
				})
			}
		}
	}

	var listStr string
	if len(listLines) > 0 {
		listStr = strings.Join(listLines, "\n")
	} else {
		listStr = a.i18n.Text(language, "custom_questions.no_questions")
	}

	bodyText := fmt.Sprintf(a.i18n.Text(language, "custom_questions.menu_text"), listStr)
	if prefixText != "" {
		bodyText = prefixText + "\n\n" + bodyText
	}

	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: a.i18n.Text(language, "custom_questions.add_button"), CallbackData: "custom_questions:add"},
	})

	backText := "« Back"
	if language == "uk" {
		backText = "« Назад"
	}
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: backText, CallbackData: "settings:open"},
	})

	return a.editCallbackScreen(ctx, cb, chatID, bodyText, telegram.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max-3]) + "..."
	}
	return s
}
