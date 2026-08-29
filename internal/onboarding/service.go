package onboarding

import "wrnrs/internal/telegram"

type Step string

const (
	StepLanguage    Step = "onboarding:language"
	StepName        Step = "onboarding:name"
	StepGender      Step = "onboarding:gender"
	StepOwnContact  Step = "onboarding:own_contact"
	StepAdult       Step = "onboarding:adult"
	StepMatureOptIn Step = "onboarding:mature_opt_in"
	StepThemeColor  Step = "onboarding:theme_color"
	StepBackground  Step = "onboarding:background"
	StepPairing     Step = "onboarding:pairing"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) LanguageKeyboard() telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: "Українська", CallbackData: "onboarding:language:uk"}, {Text: "English", CallbackData: "onboarding:language:en"}},
	}}
}

func (s *Service) GenderKeyboard(language string) telegram.InlineKeyboardMarkup {
	if language == "uk" {
		return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: "Жінка", CallbackData: "onboarding:gender:female"}, {Text: "Чоловік", CallbackData: "onboarding:gender:male"}},
			{{Text: "Інше", CallbackData: "onboarding:gender:other"}},
		}}
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: "Woman", CallbackData: "onboarding:gender:female"}, {Text: "Man", CallbackData: "onboarding:gender:male"}},
		{{Text: "Other", CallbackData: "onboarding:gender:other"}},
	}}
}

func (s *Service) AdultKeyboard(language string) telegram.InlineKeyboardMarkup {
	if language == "uk" {
		return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: "Підтверджую, мені 18+", CallbackData: "onboarding:adult:yes"}},
			{{Text: "Мені немає 18", CallbackData: "onboarding:adult:no"}},
		}}
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: "I confirm I am 18+", CallbackData: "onboarding:adult:yes"}},
		{{Text: "I am under 18", CallbackData: "onboarding:adult:no"}},
	}}
}

func (s *Service) MatureKeyboard(language string) telegram.InlineKeyboardMarkup {
	if language == "uk" {
		return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: "Так, показувати 18+ картки", CallbackData: "onboarding:mature:yes"}},
			{{Text: "Ні, тільки безпечні картки", CallbackData: "onboarding:mature:no"}},
		}}
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: "Yes, show 18+ cards", CallbackData: "onboarding:mature:yes"}},
		{{Text: "No, safe cards only", CallbackData: "onboarding:mature:no"}},
	}}
}

func (s *Service) ColorKeyboard(language string) telegram.InlineKeyboardMarkup {
	swatches := []telegram.InlineKeyboardButton{
		{Text: "Rose", CallbackData: "theme:color:#d98c9f"},
		{Text: "Wine", CallbackData: "theme:color:#8f3f5f"},
		{Text: "Peach", CallbackData: "theme:color:#e4a07a"},
		{Text: "Sage", CallbackData: "theme:color:#8da68f"},
	}
	if language == "uk" {
		swatches[0].Text = "Рожевий"
		swatches[1].Text = "Вино"
		swatches[2].Text = "Персик"
		swatches[3].Text = "Шавлія"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{swatches[0], swatches[1]},
		{swatches[2], swatches[3]},
		{{Text: "Custom HEX", CallbackData: "theme:color:custom"}},
	}}
}

func (s *Service) OwnContactKeyboard(language string) telegram.ReplyKeyboardMarkup {
	share := "Share my contact"
	skip := "Skip"
	if language == "uk" {
		share = "Поділитися моїм контактом"
		skip = "Пропустити"
	}
	return telegram.ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]telegram.KeyboardButton{
			{{Text: share, RequestContact: true}},
			{{Text: skip}},
		},
	}
}

func (s *Service) BackgroundKeyboard(language string) telegram.InlineKeyboardMarkup {
	skip := "Skip"
	useDefault := "Use default"
	upload := "Upload"
	if language == "uk" {
		skip = "Пропустити"
		useDefault = "Стандартний фон"
		upload = "Завантажити"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: useDefault, CallbackData: "onboarding:bg:default"}, {Text: skip, CallbackData: "onboarding:bg:skip"}},
		{{Text: upload, CallbackData: "onboarding:bg:upload"}},
	}}
}
