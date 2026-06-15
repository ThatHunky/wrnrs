package pairing

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode"

	"wrnrs/internal/telegram"
)

type Service struct {
	BotUsername string
	PhoneSecret string
}

func NewService(botUsername, phoneSecret string) *Service {
	return &Service{
		BotUsername: strings.TrimPrefix(botUsername, "@"),
		PhoneSecret: phoneSecret,
	}
}

type Identifier struct {
	TelegramID int64
	Username   string
	PhoneHash  string
}

func (s *Service) InviteToken() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Service) IdentifierFromText(text string) (Identifier, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Identifier{}, false
	}
	if token, ok := TokenFromText(text); ok {
		return Identifier{Username: "token:" + token}, true
	}
	if strings.HasPrefix(text, "@") {
		username := NormalizeUsername(text)
		if username == "" {
			return Identifier{}, false
		}
		return Identifier{Username: username}, true
	}
	if id, err := strconv.ParseInt(text, 10, 64); err == nil && id > 0 {
		return Identifier{TelegramID: id}, true
	}
	return Identifier{}, false
}

func (s *Service) IdentifierFromContact(contact *telegram.Contact) (Identifier, bool) {
	if contact == nil {
		return Identifier{}, false
	}
	if contact.UserID > 0 {
		return Identifier{TelegramID: contact.UserID}, true
	}
	phoneHash := s.PhoneHash(contact.PhoneNumber)
	if phoneHash == "" {
		return Identifier{}, false
	}
	return Identifier{PhoneHash: phoneHash}, true
}

func (s *Service) PhoneHash(phone string) string {
	normalized := NormalizePhone(phone)
	if normalized == "" || s.PhoneSecret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(s.PhoneSecret))
	_, _ = mac.Write([]byte(normalized))
	return hex.EncodeToString(mac.Sum(nil))
}

func NormalizeUsername(username string) string {
	username = strings.TrimSpace(strings.ToLower(username))
	username = strings.TrimPrefix(username, "@")
	return username
}

func NormalizePhone(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TokenFromText(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "/start ") {
		text = strings.TrimSpace(strings.TrimPrefix(text, "/start "))
	}
	if idx := strings.Index(text, "start=pair_"); idx >= 0 {
		text = text[idx+len("start=pair_"):]
	} else if strings.HasPrefix(text, "pair_") {
		text = strings.TrimPrefix(text, "pair_")
	} else {
		return "", false
	}
	text = strings.TrimRight(text, " .)")
	if text == "" || strings.ContainsAny(text, " \n\t") {
		return "", false
	}
	return text, true
}

func (s *Service) InviteURL(token string) string {
	username := s.BotUsername
	if username == "" {
		username = "WRNRSBot"
	}
	return "https://t.me/" + username + "?start=pair_" + token
}

func (s *Service) PairingKeyboard(language, inviteToken string) telegram.InlineKeyboardMarkup {
	shareText := "Share invite"
	if language == "uk" {
		shareText = "Надіслати запрошення"
	}
	url := s.InviteURL(inviteToken)
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: shareText, URL: url}},
	}}
}
