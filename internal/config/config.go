package config

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BotToken              string
	BotUsername           string
	TelegramWebhookSecret string
	PublicBaseURL         string
	PhoneHashSecret       string
	AnswerEncryptionKey   []byte
	SQLitePath            string
	RedisAddr             string
	RedisPassword         string
	MinIO                 MinIOConfig
	Donation              DonationConfig
	AdminTelegramIDs      []int64
	SupportPromptDelay    time.Duration
	SupportPromptInterval time.Duration
	FeatureInlineMode     bool
	CardFontPath          string
	PositionsCatalogPath  string
	PositionsBucket       string
	PositionsPrefix       string
	WishesCatalogPath     string
}

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type DonationConfig struct {
	MonobankURL string
	CardNumber  string
}

// AssetConfig is the narrow slice of Config that anything talking to the
// object store needs: MinIO connection details plus the bucket/prefix the
// positions catalog's images live under. It exists so a standalone tool
// (cmd/ingest-positions' seeding mode) can read exactly these settings via
// LoadAssetConfig without going through the full Load, which additionally
// requires ANSWER_ENCRYPTION_KEY and other bot-runtime settings that a
// one-off seeding run has no use for.
type AssetConfig struct {
	MinIO           MinIOConfig
	PositionsBucket string
	PositionsPrefix string
}

type Getter func(string) string

// LoadAssetConfig reads only the object-store settings: MINIO_ENDPOINT,
// MINIO_ACCESS_KEY, MINIO_SECRET_KEY, MINIO_BUCKET, MINIO_USE_SSL,
// POSITIONS_BUCKET, and POSITIONS_PREFIX. It shares its defaults with Load
// (both call loadAssetConfig) so the two never drift apart.
func LoadAssetConfig(getenv Getter) AssetConfig {
	return loadAssetConfig(getenv)
}

func loadAssetConfig(getenv Getter) AssetConfig {
	return AssetConfig{
		MinIO: MinIOConfig{
			Endpoint:  withDefault(getenv("MINIO_ENDPOINT"), "minio:9000"),
			AccessKey: getenv("MINIO_ACCESS_KEY"),
			SecretKey: getenv("MINIO_SECRET_KEY"),
			Bucket:    withDefault(getenv("MINIO_BUCKET"), "wrnrs-assets"),
			UseSSL:    parseBool(getenv("MINIO_USE_SSL")),
		},
		PositionsBucket: withDefault(getenv("POSITIONS_BUCKET"), "wrnrs-assets"),
		PositionsPrefix: withDefault(getenv("POSITIONS_PREFIX"), "positions/"),
	}
}

func Load(getenv Getter) (Config, error) {
	assets := loadAssetConfig(getenv)
	cfg := Config{
		BotToken:              getenv("BOT_TOKEN"),
		BotUsername:           strings.TrimPrefix(getenv("BOT_USERNAME"), "@"),
		TelegramWebhookSecret: getenv("TELEGRAM_WEBHOOK_SECRET"),
		PublicBaseURL:         getenv("PUBLIC_BASE_URL"),
		PhoneHashSecret:       getenv("PHONE_HASH_SECRET"),
		SQLitePath:            withDefault(getenv("SQLITE_PATH"), "/data/wrnrs.sqlite3"),
		RedisAddr:             withDefault(getenv("REDIS_ADDR"), "redis:6379"),
		RedisPassword:         getenv("REDIS_PASSWORD"),
		SupportPromptDelay:    3 * time.Second,
		SupportPromptInterval: 48 * time.Hour,
		FeatureInlineMode:     parseBool(getenv("FEATURE_INLINE_MODE")),
		CardFontPath:          withDefault(getenv("CARD_FONT_PATH"), "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"),
		PositionsCatalogPath:  withDefault(getenv("POSITIONS_CATALOG_PATH"), "content/positions.v1.json"),
		WishesCatalogPath:     withDefault(getenv("WISHES_CATALOG_PATH"), "content/wishes.v1.json"),
		PositionsBucket:       assets.PositionsBucket,
		PositionsPrefix:       assets.PositionsPrefix,
		MinIO:                 assets.MinIO,
		Donation: DonationConfig{
			MonobankURL: getenv("DONATION_MONOBANK_URL"),
			CardNumber:  getenv("DONATION_CARD_NUMBER"),
		},
	}

	adminIDs, err := parseAdminIDs(getenv("ADMIN_TELEGRAM_IDS"))
	if err != nil {
		return Config{}, err
	}
	cfg.AdminTelegramIDs = adminIDs
	answerKey, err := parseAnswerEncryptionKey(getenv("ANSWER_ENCRYPTION_KEY"))
	if err != nil {
		return Config{}, err
	}
	cfg.AnswerEncryptionKey = answerKey
	return cfg, nil
}

func (c Config) IsAdmin(userID int64) bool {
	for _, adminID := range c.AdminTelegramIDs {
		if userID == adminID {
			return true
		}
	}
	return false
}

func parseAdminIDs(raw string) ([]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse ADMIN_TELEGRAM_IDS value %q: %w", part, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func parseAnswerEncryptionKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("ANSWER_ENCRYPTION_KEY is required")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode ANSWER_ENCRYPTION_KEY: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("ANSWER_ENCRYPTION_KEY must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}

func withDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
