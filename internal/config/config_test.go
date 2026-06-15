package config_test

import (
	"testing"

	"wrnrs/internal/config"
)

func TestLoadParsesAdminIDsAndDonationConfig(t *testing.T) {
	env := map[string]string{
		"BOT_TOKEN":               "123:abc",
		"BOT_USERNAME":            "@wrnrs_bot",
		"ADMIN_TELEGRAM_IDS":      "1001, 1002",
		"ANSWER_ENCRYPTION_KEY":   "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		"TELEGRAM_WEBHOOK_SECRET": "secret-token",
		"PHONE_HASH_SECRET":       "phone-secret",
		"DONATION_MONOBANK_URL":   "https://send.monobank.ua/jar/example",
		"DONATION_CARD_NUMBER":    "4441111122223333",
	}

	cfg, err := config.Load(func(key string) string { return env[key] })
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.BotToken != "123:abc" {
		t.Fatalf("BotToken = %q", cfg.BotToken)
	}
	if !cfg.IsAdmin(1001) || !cfg.IsAdmin(1002) {
		t.Fatalf("admin IDs not parsed correctly: %#v", cfg.AdminTelegramIDs)
	}
	if cfg.Donation.MonobankURL != env["DONATION_MONOBANK_URL"] {
		t.Fatalf("donation URL = %q", cfg.Donation.MonobankURL)
	}
	if cfg.TelegramWebhookSecret != "secret-token" {
		t.Fatalf("TelegramWebhookSecret = %q", cfg.TelegramWebhookSecret)
	}
	if cfg.BotUsername != "wrnrs_bot" {
		t.Fatalf("BotUsername = %q", cfg.BotUsername)
	}
	if cfg.PhoneHashSecret != "phone-secret" {
		t.Fatalf("PhoneHashSecret = %q", cfg.PhoneHashSecret)
	}
	if got := len(cfg.AnswerEncryptionKey); got != 32 {
		t.Fatalf("AnswerEncryptionKey length = %d, want 32", got)
	}
}

func TestLoadRequiresValidAnswerEncryptionKey(t *testing.T) {
	baseEnv := map[string]string{
		"BOT_TOKEN": "123:abc",
	}

	if _, err := config.Load(func(key string) string { return baseEnv[key] }); err == nil {
		t.Fatal("Load succeeded without ANSWER_ENCRYPTION_KEY")
	}

	env := map[string]string{
		"BOT_TOKEN":             "123:abc",
		"ANSWER_ENCRYPTION_KEY": "short",
	}
	if _, err := config.Load(func(key string) string { return env[key] }); err == nil {
		t.Fatal("Load succeeded with invalid ANSWER_ENCRYPTION_KEY")
	}
}
