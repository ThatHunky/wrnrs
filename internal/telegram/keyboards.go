package telegram

const CancelResetText = "Cancel / Reset"
const CancelResetTextUK = "Скасувати / Скинути"
const MenuTextEN = "Menu"
const MenuTextUK = "Меню"

func PersistentKeyboard(language string) ReplyKeyboardMarkup {
	return ControlsKeyboard(language)
}

func ControlsKeyboard(language string) ReplyKeyboardMarkup {
	cancel := CancelResetText
	menu := MenuTextEN
	if language == "uk" {
		cancel = CancelResetTextUK
		menu = MenuTextUK
	}
	return ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		IsPersistent:   true,
		Keyboard: [][]KeyboardButton{
			{{Text: menu}, {Text: cancel}},
		},
	}
}

func MainMenuKeyboard(language string) InlineKeyboardMarkup {
	return MainMenuKeyboardWithPair(language, false)
}

func MainMenuKeyboardWithPair(language string, hasPair bool) InlineKeyboardMarkup {
	var rows [][]InlineKeyboardButton

	if language == "uk" {
		if hasPair {
			rows = append(rows, []InlineKeyboardButton{{Text: "Почати / продовжити", CallbackData: "game:start"}})
		}
		pairText := "Пара 💔"
		if hasPair {
			pairText = "Пара 💑"
		}
		rows = append(rows, []InlineKeyboardButton{
			{Text: pairText, CallbackData: "pair:menu"},
			{Text: "Тема", CallbackData: "theme:menu"},
		})
		rows = append(rows, []InlineKeyboardButton{
			{Text: "Журнал", CallbackData: "journal:open"},
			{Text: "Преміум", CallbackData: "store:open"},
		})
		rows = append(rows, []InlineKeyboardButton{
			{Text: "Налаштування", CallbackData: "settings:open"},
		})
	} else {
		if hasPair {
			rows = append(rows, []InlineKeyboardButton{{Text: "Start / Resume", CallbackData: "game:start"}})
		}
		pairText := "Pair 💔"
		if hasPair {
			pairText = "Pair 💑"
		}
		rows = append(rows, []InlineKeyboardButton{
			{Text: pairText, CallbackData: "pair:menu"},
			{Text: "Theme", CallbackData: "theme:menu"},
		})
		rows = append(rows, []InlineKeyboardButton{
			{Text: "Journal", CallbackData: "journal:open"},
			{Text: "Premium", CallbackData: "store:open"},
		})
		rows = append(rows, []InlineKeyboardButton{
			{Text: "Settings", CallbackData: "settings:open"},
		})
	}

	return InlineKeyboardMarkup{InlineKeyboard: rows}
}

func CardControls(language string) InlineKeyboardMarkup {
	return CardControlsForQuestion(language, "")
}

func CardControlsForQuestion(language, questionID string) InlineKeyboardMarkup {
	suffix := ""
	if questionID != "" {
		suffix = ":" + questionID
	}
	if language == "uk" {
		return InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
			{{Text: "Ввести відповідь", CallbackData: "game:answer" + suffix}},
			{{Text: "Відповіли наживо", CallbackData: "game:in_person" + suffix}, {Text: "Пропустити", CallbackData: "game:skip" + suffix}},
			{{Text: "Пауза", CallbackData: "game:pause" + suffix}, {Text: "Меню", CallbackData: "menu:main"}},
		}}
	}
	return InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "Type answer", CallbackData: "game:answer" + suffix}},
		{{Text: "Answered in person", CallbackData: "game:in_person" + suffix}, {Text: "Skip", CallbackData: "game:skip" + suffix}},
		{{Text: "Pause", CallbackData: "game:pause" + suffix}, {Text: "Menu", CallbackData: "menu:main"}},
	}}
}

func AdminTestKeyboard(language, cardID string) InlineKeyboardMarkup {
	prevText := "← Prev"
	nextText := "Next →"
	typeText := "Type answer"
	answeredInPersonText := "Answered in person"
	skipText := "Skip"
	pauseText := "Pause"
	menuText := "Menu"

	if language == "uk" {
		prevText = "← Назад"
		nextText = "Далі →"
		typeText = "Ввести відповідь"
		answeredInPersonText = "Відповіли наживо"
		skipText = "Пропустити"
		pauseText = "Пауза"
		menuText = "Меню"
	}

	return InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{
			{Text: prevText, CallbackData: "game:admin_prev:" + cardID},
			{Text: nextText, CallbackData: "game:admin_next:" + cardID},
		},
		{
			{Text: typeText, CallbackData: "game:answer:" + cardID},
			{Text: skipText, CallbackData: "game:skip:" + cardID},
		},
		{
			{Text: answeredInPersonText, CallbackData: "game:in_person:" + cardID},
			{Text: pauseText, CallbackData: "game:pause:" + cardID},
		},
		{
			{Text: menuText, CallbackData: "menu:main"},
		},
	}}
}
