package pairing_test

import (
	"testing"

	"wrnrs/internal/pairing"
	"wrnrs/internal/telegram"
)

func TestIdentifierFromTextParsesUsernameAndTelegramID(t *testing.T) {
	service := pairing.NewService("wrnrs_bot", "secret")

	username, ok := service.IdentifierFromText("@Partner_User")
	if !ok || username.Username != "partner_user" {
		t.Fatalf("username identifier = %#v, %v", username, ok)
	}

	id, ok := service.IdentifierFromText("2002")
	if !ok || id.TelegramID != 2002 {
		t.Fatalf("telegram id identifier = %#v, %v", id, ok)
	}
}

func TestIdentifierFromContactPrefersTelegramUserIDAndHashesPhoneFallback(t *testing.T) {
	service := pairing.NewService("wrnrs_bot", "secret")

	withUserID, ok := service.IdentifierFromContact(&telegram.Contact{UserID: 2002, PhoneNumber: "+380 97 779 75 98"})
	if !ok || withUserID.TelegramID != 2002 || withUserID.PhoneHash != "" {
		t.Fatalf("contact with user id identifier = %#v, %v", withUserID, ok)
	}

	phoneOnly, ok := service.IdentifierFromContact(&telegram.Contact{PhoneNumber: "+380 97 779 75 98"})
	if !ok || phoneOnly.PhoneHash == "" {
		t.Fatalf("phone-only contact identifier = %#v, %v", phoneOnly, ok)
	}
	if phoneOnly.PhoneHash == pairing.NormalizePhone("+380 97 779 75 98") {
		t.Fatal("phone hash exposed the normalized phone number")
	}
}

func TestTokenFromTextOnlyAcceptsPairTokens(t *testing.T) {
	if _, ok := pairing.TokenFromText("/start"); ok {
		t.Fatal("plain /start was parsed as pair token")
	}
	if token, ok := pairing.TokenFromText("/start pair_abc123"); !ok || token != "abc123" {
		t.Fatalf("start payload token = %q, %v", token, ok)
	}
	if token, ok := pairing.TokenFromText("https://t.me/wrnrs_bot?start=pair_abc123"); !ok || token != "abc123" {
		t.Fatalf("deep link token = %q, %v", token, ok)
	}
}
