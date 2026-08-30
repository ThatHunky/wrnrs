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

func TestLoadAssetConfigAppliesDefaultsWithoutRequiringAnswerEncryptionKey(t *testing.T) {
	// LoadAssetConfig must work standalone (no BOT_TOKEN, no
	// ANSWER_ENCRYPTION_KEY) so a seeding-only tool never has to satisfy the
	// full bot-runtime config just to talk to the object store.
	assets := config.LoadAssetConfig(func(string) string { return "" })

	if assets.MinIO.Endpoint != "minio:9000" {
		t.Fatalf("MinIO.Endpoint = %q, want default minio:9000", assets.MinIO.Endpoint)
	}
	if assets.MinIO.Bucket != "wrnrs-assets" {
		t.Fatalf("MinIO.Bucket = %q, want default wrnrs-assets", assets.MinIO.Bucket)
	}
	if assets.PositionsBucket != "wrnrs-assets" {
		t.Fatalf("PositionsBucket = %q, want default wrnrs-assets", assets.PositionsBucket)
	}
	if assets.PositionsPrefix != "positions/" {
		t.Fatalf("PositionsPrefix = %q, want default positions/", assets.PositionsPrefix)
	}
}

func TestLoadAssetConfigReadsOverridesAndAgreesWithLoad(t *testing.T) {
	env := map[string]string{
		"BOT_TOKEN":             "123:abc",
		"ANSWER_ENCRYPTION_KEY": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		"MINIO_ENDPOINT":        "minio.internal:9000",
		"MINIO_ACCESS_KEY":      "ak",
		"MINIO_SECRET_KEY":      "sk",
		"MINIO_BUCKET":          "custom-bucket",
		"MINIO_USE_SSL":         "true",
		"POSITIONS_BUCKET":      "positions-bucket",
		"POSITIONS_PREFIX":      "assets/positions/",
	}
	getenv := func(key string) string { return env[key] }

	assets := config.LoadAssetConfig(getenv)
	if assets.MinIO.Endpoint != "minio.internal:9000" || assets.MinIO.AccessKey != "ak" || assets.MinIO.SecretKey != "sk" || assets.MinIO.Bucket != "custom-bucket" || !assets.MinIO.UseSSL {
		t.Fatalf("LoadAssetConfig MinIO = %+v", assets.MinIO)
	}
	if assets.PositionsBucket != "positions-bucket" {
		t.Fatalf("PositionsBucket = %q, want positions-bucket", assets.PositionsBucket)
	}
	if assets.PositionsPrefix != "assets/positions/" {
		t.Fatalf("PositionsPrefix = %q, want assets/positions/", assets.PositionsPrefix)
	}

	// Load must agree with LoadAssetConfig for the fields they share, since
	// Load now composes its MinIO/PositionsBucket/PositionsPrefix fields
	// from the same loadAssetConfig helper.
	cfg, err := config.Load(getenv)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.MinIO != assets.MinIO {
		t.Fatalf("Load MinIO = %+v, want it to match LoadAssetConfig MinIO = %+v", cfg.MinIO, assets.MinIO)
	}
	if cfg.PositionsBucket != assets.PositionsBucket || cfg.PositionsPrefix != assets.PositionsPrefix {
		t.Fatalf("Load positions bucket/prefix = %q/%q, want %q/%q", cfg.PositionsBucket, cfg.PositionsPrefix, assets.PositionsBucket, assets.PositionsPrefix)
	}
}
