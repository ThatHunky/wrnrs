package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	EntitlementPremiumAccess = "premium_access"
	EntitlementStyle         = "style"
	EntitlementFont          = "font"
	EntitlementBackground    = "background"
)

var (
	ErrPairRequestNotFound  = errors.New("pair request not found")
	ErrPairRequestForbidden = errors.New("pair request is not for this user")
	ErrSelfPair             = errors.New("cannot pair user with self")
	ErrAlreadyPaired        = errors.New("user already has an active pair")
	ErrGameSessionNotFound  = errors.New("game session not found")
)

type User struct {
	TelegramID         int64
	Username           string
	DisplayName        string
	Gender             string
	Language           string
	ThemeBaseColor     string
	SelectedStyleID    string
	SelectedAssetID    sql.NullString
	SelectedFontID     string
	CustomBorderRadius sql.NullInt64
	CustomGlassOpacity sql.NullFloat64
	Is18Plus           bool
	MatureOptIn        bool
}

type Pair struct {
	ID                   int64
	UserAID              int64
	UserBID              int64
	Status               string
	ActiveLevel          int
	HighestUnlockedLevel int
}

type PairRequest struct {
	ID                       int64
	RequesterID              int64
	TargetTelegramID         sql.NullInt64
	TargetUsernameNormalized sql.NullString
	TargetPhoneHash          sql.NullString
	InviteToken              string
	Status                   string
	ExpiresAt                time.Time
}

const (
	GameSessionPendingAcceptance = "pending_acceptance"
	GameSessionActive            = "active"
	GameSessionRevealed          = "revealed"
	GameSessionCompleted         = "completed"
	GameSessionCancelled         = "cancelled"
	GameSessionExpired           = "expired"
)

type GameSession struct {
	ID               int64
	PairID           int64
	Level            int
	QuestionID       string
	QuestionSource   string
	QuestionTextUK   string
	QuestionTextEN   string
	RequiresMature   bool
	Status           string
	DeckCycle        int
	InvitedByUserID  int64
	AcceptedByUserID sql.NullInt64
	InviteExpiresAt  sql.NullTime
	StartedAt        sql.NullTime
	RevealedAt       sql.NullTime
	CompletedAt      sql.NullTime
}

type GameAnswer struct {
	SessionID           int64
	UserID              int64
	CompletionType      string
	AnswerTextEncrypted []byte
	CompletedAt         time.Time
	RevealedAt          sql.NullTime
}

type Entitlement struct {
	UserID          int64
	Type            string
	UnlockID        string
	Source          string
	SourceReceiptID sql.NullInt64
	ExpiresAt       sql.NullTime
}

type PurchaseReceipt struct {
	ID                      int64
	UserID                  int64
	SKU                     string
	Currency                string
	StarsAmount             int64
	TelegramPaymentChargeID string
	ProviderPaymentChargeID sql.NullString
	Status                  string
}

type UserProfile struct {
	TelegramID                int64
	DisplayName               string
	Language                  string
	ThemeBaseColor            string
	SelectedStyleID           string
	SelectedBackgroundAssetID string
	SelectedFontID            string
	CustomBorderRadius        sql.NullInt64
	CustomGlassOpacity        sql.NullFloat64
}

type ThemeAsset struct {
	ID             string
	OwnerUserID    int64
	Kind           string
	MinioObjectKey string
	Status         string
	Width          int
	Height         int
	SizeBytes      int64
}

type PairThemeShare struct {
	PairID         int64
	AssetID        string
	SharedByUserID int64
	Status         string
}

type CustomQuestion struct {
	ID           int64
	CreatorID    int64
	QuestionText string
	CreatedAt    time.Time
	DeletedAt    sql.NullTime
}

type JournalAnswer struct {
	UserID              int64
	CompletionType      string
	AnswerTextEncrypted []byte
}

type JournalEntry struct {
	SessionID      int64
	PairID         int64
	Level          int
	QuestionID     string
	QuestionTextUK string
	QuestionTextEN string
	RequiresMature bool
	RevealedAt     time.Time
	Answers        []JournalAnswer
}

type Repository struct {
	db *sql.DB
}

func OpenSQLite(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	_, _ = db.ExecContext(ctx, `PRAGMA journal_mode = WAL`)
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	// Migrate existing database table to add new columns if they do not exist
	_, _ = db.ExecContext(ctx, `ALTER TABLE users ADD COLUMN selected_font_id TEXT NOT NULL DEFAULT 'nunito_regular'`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE users ADD COLUMN custom_border_radius INTEGER`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE users ADD COLUMN custom_glass_opacity REAL`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE game_sessions ADD COLUMN deck_cycle INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE game_sessions ADD COLUMN invited_by_user_id INTEGER`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE game_sessions ADD COLUMN accepted_by_user_id INTEGER`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE game_sessions ADD COLUMN invite_expires_at TEXT`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE game_sessions ADD COLUMN started_at TEXT`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE game_sessions ADD COLUMN completed_at TEXT`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE game_sessions ADD COLUMN question_source TEXT NOT NULL DEFAULT 'stock'`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE game_sessions ADD COLUMN question_text_uk TEXT NOT NULL DEFAULT ''`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE game_sessions ADD COLUMN question_text_en TEXT NOT NULL DEFAULT ''`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE game_sessions ADD COLUMN requires_mature_opt_in BOOLEAN NOT NULL DEFAULT 0`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE custom_questions ADD COLUMN deleted_at TEXT`)
	if _, err := db.ExecContext(ctx, `
		CREATE UNIQUE INDEX IF NOT EXISTS game_sessions_one_current_pair_idx
		ON game_sessions(pair_id)
		WHERE status IN ('pending_acceptance', 'active', 'revealed')
	`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create current game session index: %w", err)
	}
	return db, nil
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) UpsertUser(ctx context.Context, user User) error {
	if user.TelegramID == 0 {
		return errors.New("telegram id is required")
	}
	language := user.Language
	if language == "" {
		language = "uk"
	}
	style := user.SelectedStyleID
	if style == "" {
		style = "default_warm"
	}
	font := user.SelectedFontID
	if font == "" {
		font = "nunito_regular"
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (
			telegram_id, username_normalized, display_name, gender, language,
			is_18_plus, mature_opt_in, theme_base_color, selected_style_id,
			selected_background_asset_id, selected_font_id, custom_border_radius,
			custom_glass_opacity, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(telegram_id) DO UPDATE SET
			username_normalized = excluded.username_normalized,
			display_name = excluded.display_name,
			gender = excluded.gender,
			language = excluded.language,
			is_18_plus = excluded.is_18_plus,
			mature_opt_in = excluded.mature_opt_in,
			theme_base_color = excluded.theme_base_color,
			selected_style_id = excluded.selected_style_id,
			selected_background_asset_id = excluded.selected_background_asset_id,
			selected_font_id = excluded.selected_font_id,
			custom_border_radius = excluded.custom_border_radius,
			custom_glass_opacity = excluded.custom_glass_opacity,
			updated_at = CURRENT_TIMESTAMP
	`, user.TelegramID, normalizeUsername(user.Username), user.DisplayName, user.Gender, language,
		user.Is18Plus, user.MatureOptIn, user.ThemeBaseColor, style, nullableStringValue(user.SelectedAssetID),
		font, nullableInt64Value(user.CustomBorderRadius), nullableFloat64Value(user.CustomGlassOpacity))
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

func (r *Repository) EnsureUser(ctx context.Context, user User) error {
	if user.TelegramID == 0 {
		return errors.New("telegram id is required")
	}
	language := user.Language
	if language == "" {
		language = "uk"
	}
	style := user.SelectedStyleID
	if style == "" {
		style = "default_warm"
	}
	font := user.SelectedFontID
	if font == "" {
		font = "nunito_regular"
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (
			telegram_id, username_normalized, display_name, language,
			theme_base_color, selected_style_id, selected_font_id, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(telegram_id) DO UPDATE SET
			username_normalized = excluded.username_normalized,
			updated_at = CURRENT_TIMESTAMP
	`, user.TelegramID, normalizeUsername(user.Username), user.DisplayName, language, user.ThemeBaseColor, style, font)
	if err != nil {
		return fmt.Errorf("ensure user: %w", err)
	}
	return nil
}

func (r *Repository) MarkOnboardingComplete(ctx context.Context, telegramID int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET onboarding_status = 'complete', updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, telegramID)
	if err != nil {
		return fmt.Errorf("mark onboarding complete: %w", err)
	}
	return nil
}

func (r *Repository) UserOnboardingComplete(ctx context.Context, telegramID int64) (bool, error) {
	var status string
	err := r.db.QueryRowContext(ctx, `
		SELECT onboarding_status
		FROM users
		WHERE telegram_id = ?
	`, telegramID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load onboarding status: %w", err)
	}
	return status == "complete", nil
}

func (r *Repository) UserLanguage(ctx context.Context, telegramID int64) (string, error) {
	var language string
	err := r.db.QueryRowContext(ctx, `
		SELECT language
		FROM users
		WHERE telegram_id = ?
	`, telegramID).Scan(&language)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load user language: %w", err)
	}
	return language, nil
}

func (r *Repository) UserProfile(ctx context.Context, telegramID int64) (UserProfile, error) {
	var profile UserProfile
	var color sql.NullString
	var bg sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT telegram_id, display_name, language, theme_base_color,
		       selected_style_id, selected_background_asset_id, selected_font_id,
		       custom_border_radius, custom_glass_opacity
		FROM users
		WHERE telegram_id = ?
	`, telegramID).Scan(&profile.TelegramID, &profile.DisplayName, &profile.Language, &color,
		&profile.SelectedStyleID, &bg, &profile.SelectedFontID,
		&profile.CustomBorderRadius, &profile.CustomGlassOpacity)
	if errors.Is(err, sql.ErrNoRows) {
		return UserProfile{}, nil
	}
	if err != nil {
		return UserProfile{}, fmt.Errorf("load user profile: %w", err)
	}
	if color.Valid && color.String != "" {
		profile.ThemeBaseColor = color.String
	}
	if bg.Valid && bg.String != "" {
		profile.SelectedBackgroundAssetID = bg.String
	}
	if profile.SelectedStyleID == "" {
		profile.SelectedStyleID = "default_warm"
	}
	if profile.SelectedFontID == "" {
		profile.SelectedFontID = "nunito_regular"
	}
	return profile, nil
}

func (r *Repository) UserDisplayName(ctx context.Context, telegramID int64) (string, error) {
	var displayName string
	err := r.db.QueryRowContext(ctx, `
		SELECT display_name
		FROM users
		WHERE telegram_id = ?
	`, telegramID).Scan(&displayName)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load user display name: %w", err)
	}
	return displayName, nil
}

func (r *Repository) UserExists(ctx context.Context, telegramID int64) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM users
		WHERE telegram_id = ?
	`, telegramID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check user exists: %w", err)
	}
	return count > 0, nil
}

func (r *Repository) UserIDByUsername(ctx context.Context, username string) (int64, bool, error) {
	var userID int64
	err := r.db.QueryRowContext(ctx, `
		SELECT telegram_id
		FROM users
		WHERE username_normalized = ?
	`, normalizeUsername(username)).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("load user by username: %w", err)
	}
	return userID, true, nil
}

func (r *Repository) UpdateUserLanguage(ctx context.Context, telegramID int64, language string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET language = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, language, telegramID)
	if err != nil {
		return fmt.Errorf("update user language: %w", err)
	}
	return nil
}

func (r *Repository) UpdateUserName(ctx context.Context, telegramID int64, displayName string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET display_name = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, displayName, telegramID)
	if err != nil {
		return fmt.Errorf("update user name: %w", err)
	}
	return nil
}

func (r *Repository) UpdateUserGender(ctx context.Context, telegramID int64, gender string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET gender = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, gender, telegramID)
	if err != nil {
		return fmt.Errorf("update user gender: %w", err)
	}
	return nil
}

func (r *Repository) UpdateAdultConfirmation(ctx context.Context, telegramID int64, is18Plus bool) error {
	confirmedAt := sql.NullString{}
	if is18Plus {
		confirmedAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339Nano), Valid: true}
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET is_18_plus = ?, adult_confirmed_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, is18Plus, nullableStringValue(confirmedAt), telegramID)
	if err != nil {
		return fmt.Errorf("update adult confirmation: %w", err)
	}
	return nil
}

func (r *Repository) UpdateMatureOptIn(ctx context.Context, telegramID int64, matureOptIn bool) error {
	optedInAt := sql.NullString{}
	if matureOptIn {
		optedInAt = sql.NullString{String: time.Now().UTC().Format(time.RFC3339Nano), Valid: true}
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET mature_opt_in = ?, mature_opted_in_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, matureOptIn, nullableStringValue(optedInAt), telegramID)
	if err != nil {
		return fmt.Errorf("update mature opt-in: %w", err)
	}
	return nil
}

func (r *Repository) UpdateThemeColor(ctx context.Context, telegramID int64, color string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET theme_base_color = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, color, telegramID)
	if err != nil {
		return fmt.Errorf("update theme color: %w", err)
	}
	return nil
}

func (r *Repository) UpdateUserStyle(ctx context.Context, telegramID int64, styleID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET selected_style_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, styleID, telegramID)
	if err != nil {
		return fmt.Errorf("update user style: %w", err)
	}
	return nil
}

func (r *Repository) UpdateUserBackground(ctx context.Context, telegramID int64, bgAssetID string) error {
	var val any
	if bgAssetID != "" {
		val = bgAssetID
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET selected_background_asset_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, val, telegramID)
	if err != nil {
		return fmt.Errorf("update user background: %w", err)
	}
	return nil
}

func (r *Repository) ResetSelectedBackgroundAsset(ctx context.Context, assetID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET selected_background_asset_id = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE selected_background_asset_id = ?
	`, assetID)
	if err != nil {
		return fmt.Errorf("reset selected background asset: %w", err)
	}
	return nil
}

func (r *Repository) UpdateUserFont(ctx context.Context, telegramID int64, fontID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET selected_font_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, fontID, telegramID)
	if err != nil {
		return fmt.Errorf("update user font: %w", err)
	}
	return nil
}

func (r *Repository) UpdateCustomBorderRadius(ctx context.Context, telegramID int64, val sql.NullInt64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET custom_border_radius = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, nullableInt64Value(val), telegramID)
	if err != nil {
		return fmt.Errorf("update custom border radius: %w", err)
	}
	return nil
}

func (r *Repository) UpdateCustomGlassOpacity(ctx context.Context, telegramID int64, val sql.NullFloat64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET custom_glass_opacity = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, nullableFloat64Value(val), telegramID)
	if err != nil {
		return fmt.Errorf("update custom glass opacity: %w", err)
	}
	return nil
}

func (r *Repository) UserActiveUploadedBackgroundsCount(ctx context.Context, userID int64) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM theme_assets
		WHERE owner_user_id = ? AND kind = 'user_upload' AND status = 'active'
	`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count user active backgrounds: %w", err)
	}
	return count, nil
}

func (r *Repository) CreateThemeAsset(ctx context.Context, assetID string, ownerID int64, minioKey string, sizeBytes int64, width, height int) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO theme_assets (id, owner_user_id, kind, minio_object_key, size_bytes, width, height, status, created_at, updated_at)
		VALUES (?, ?, 'user_upload', ?, ?, ?, ?, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, assetID, ownerID, minioKey, sizeBytes, width, height)
	if err != nil {
		return fmt.Errorf("create theme asset: %w", err)
	}
	return nil
}

func (r *Repository) GetThemeAsset(ctx context.Context, assetID string) (ThemeAsset, error) {
	var asset ThemeAsset
	var owner sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT id, owner_user_id, kind, minio_object_key, status, width, height, size_bytes
		FROM theme_assets
		WHERE id = ?
	`, assetID).Scan(&asset.ID, &owner, &asset.Kind, &asset.MinioObjectKey, &asset.Status, &asset.Width, &asset.Height, &asset.SizeBytes)
	if err != nil {
		return ThemeAsset{}, err
	}
	if owner.Valid {
		asset.OwnerUserID = owner.Int64
	}
	return asset, nil
}

func (r *Repository) GetUserUploadedBackgrounds(ctx context.Context, userID int64) ([]ThemeAsset, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, owner_user_id, kind, minio_object_key, status, width, height, size_bytes
		FROM theme_assets
		WHERE owner_user_id = ? AND kind = 'user_upload' AND status = 'active'
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query user uploaded backgrounds: %w", err)
	}
	defer rows.Close()

	var assets []ThemeAsset
	for rows.Next() {
		var asset ThemeAsset
		var owner sql.NullInt64
		if err := rows.Scan(&asset.ID, &owner, &asset.Kind, &asset.MinioObjectKey, &asset.Status, &asset.Width, &asset.Height, &asset.SizeBytes); err != nil {
			return nil, err
		}
		if owner.Valid {
			asset.OwnerUserID = owner.Int64
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func (r *Repository) DeleteThemeAsset(ctx context.Context, assetID string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM theme_assets WHERE id = ?
	`, assetID)
	if err != nil {
		return fmt.Errorf("delete theme asset: %w", err)
	}
	return nil
}

func (r *Repository) CreatePairThemeShare(ctx context.Context, pairID int64, assetID string, sharedByUserID int64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pair_theme_shares (pair_id, asset_id, shared_by_user_id, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(pair_id, asset_id) DO UPDATE SET
			shared_by_user_id = excluded.shared_by_user_id,
			status = 'active',
			updated_at = CURRENT_TIMESTAMP
	`, pairID, assetID, sharedByUserID)
	if err != nil {
		return fmt.Errorf("create pair theme share: %w", err)
	}
	return nil
}

func (r *Repository) PairThemeShares(ctx context.Context, pairID int64) ([]PairThemeShare, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT pair_id, asset_id, shared_by_user_id, status
		FROM pair_theme_shares
		WHERE pair_id = ?
		  AND status = 'active'
		ORDER BY created_at DESC
	`, pairID)
	if err != nil {
		return nil, fmt.Errorf("query pair theme shares: %w", err)
	}
	defer rows.Close()
	var shares []PairThemeShare
	for rows.Next() {
		var share PairThemeShare
		if err := rows.Scan(&share.PairID, &share.AssetID, &share.SharedByUserID, &share.Status); err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return shares, nil
}

func (r *Repository) SharedUploadedBackgroundsForUser(ctx context.Context, userID int64) ([]ThemeAsset, error) {
	pair, err := r.ActivePairForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if pair == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT ta.id, ta.owner_user_id, ta.kind, ta.minio_object_key, ta.status,
		       ta.width, ta.height, ta.size_bytes
		FROM pair_theme_shares pts
		JOIN theme_assets ta ON ta.id = pts.asset_id
		WHERE pts.pair_id = ?
		  AND pts.status = 'active'
		  AND pts.shared_by_user_id != ?
		  AND ta.status = 'active'
		ORDER BY pts.created_at DESC
	`, pair.ID, userID)
	if err != nil {
		return nil, fmt.Errorf("query shared uploaded backgrounds: %w", err)
	}
	defer rows.Close()
	var assets []ThemeAsset
	for rows.Next() {
		var asset ThemeAsset
		var owner sql.NullInt64
		if err := rows.Scan(&asset.ID, &owner, &asset.Kind, &asset.MinioObjectKey, &asset.Status, &asset.Width, &asset.Height, &asset.SizeBytes); err != nil {
			return nil, err
		}
		if owner.Valid {
			asset.OwnerUserID = owner.Int64
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return assets, nil
}

func (r *Repository) PairHasActiveThemeShare(ctx context.Context, pairID int64, assetID string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM pair_theme_shares
		WHERE pair_id = ?
		  AND asset_id = ?
		  AND status = 'active'
	`, pairID, assetID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check pair theme share: %w", err)
	}
	return count > 0, nil
}

func (r *Repository) EndActivePair(ctx context.Context, userID int64, now time.Time) (*Pair, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin end pair: %w", err)
	}
	defer tx.Rollback()

	pair, err := activePairForUserTx(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if pair == nil {
		return nil, nil
	}
	nowValue := now.UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
		UPDATE pairs
		SET status = 'ended', ended_at = ?
		WHERE id = ? AND status = 'active'
	`, nowValue, pair.ID); err != nil {
		return nil, fmt.Errorf("end pair: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE game_sessions
		SET status = 'cancelled', updated_at = ?
		WHERE pair_id = ?
		  AND status IN ('pending_acceptance', 'active', 'revealed')
	`, nowValue, pair.ID); err != nil {
		return nil, fmt.Errorf("cancel pair game sessions: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT asset_id
		FROM pair_theme_shares
		WHERE pair_id = ?
		  AND status = 'active'
	`, pair.ID)
	if err != nil {
		return nil, fmt.Errorf("query pair share assets: %w", err)
	}
	var assetIDs []string
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		assetIDs = append(assetIDs, assetID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pair_theme_shares
		SET status = 'ended', updated_at = ?
		WHERE pair_id = ?
		  AND status = 'active'
	`, nowValue, pair.ID); err != nil {
		return nil, fmt.Errorf("end pair theme shares: %w", err)
	}
	for _, assetID := range assetIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET selected_background_asset_id = NULL, updated_at = CURRENT_TIMESTAMP
			WHERE telegram_id IN (?, ?)
			  AND selected_background_asset_id = ?
		`, pair.UserAID, pair.UserBID, assetID); err != nil {
			return nil, fmt.Errorf("reset shared selected background: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit end pair: %w", err)
	}
	return pair, nil
}

func (r *Repository) UpdateUserPhoneHash(ctx context.Context, telegramID int64, phoneHash string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET phone_lookup_hash = ?, updated_at = CURRENT_TIMESTAMP
		WHERE telegram_id = ?
	`, phoneHash, telegramID)
	if err != nil {
		return fmt.Errorf("update user phone hash: %w", err)
	}
	return nil
}

func (r *Repository) UserPhoneHash(ctx context.Context, telegramID int64) (sql.NullString, error) {
	var phoneHash sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT phone_lookup_hash
		FROM users
		WHERE telegram_id = ?
	`, telegramID).Scan(&phoneHash)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullString{}, nil
	}
	if err != nil {
		return sql.NullString{}, fmt.Errorf("load user phone hash: %w", err)
	}
	return phoneHash, nil
}

func (r *Repository) CreatePairRequest(ctx context.Context, request PairRequest) (PairRequest, error) {
	if request.RequesterID == 0 {
		return PairRequest{}, errors.New("pair request requester is required")
	}
	if request.InviteToken == "" {
		return PairRequest{}, errors.New("pair request invite token is required")
	}
	if request.ExpiresAt.IsZero() {
		request.ExpiresAt = time.Now().UTC().Add(7 * 24 * time.Hour)
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pair_requests (
			requester_id, target_telegram_id, target_username_normalized,
			target_phone_hash, invite_token, status, expires_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, 'pending', ?, CURRENT_TIMESTAMP)
	`, request.RequesterID, nullableInt64Value(request.TargetTelegramID),
		nullableStringValue(request.TargetUsernameNormalized), nullableStringValue(request.TargetPhoneHash),
		request.InviteToken, request.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return PairRequest{}, fmt.Errorf("create pair request: %w", err)
	}
	return r.GetPairRequestByToken(ctx, request.InviteToken)
}

func (r *Repository) GetPairRequestByToken(ctx context.Context, token string) (PairRequest, error) {
	var request PairRequest
	var expiresAt string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, requester_id, target_telegram_id, target_username_normalized,
		       target_phone_hash, invite_token, status, expires_at
		FROM pair_requests
		WHERE invite_token = ?
	`, token).Scan(&request.ID, &request.RequesterID, &request.TargetTelegramID,
		&request.TargetUsernameNormalized, &request.TargetPhoneHash, &request.InviteToken,
		&request.Status, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PairRequest{}, ErrPairRequestNotFound
	}
	if err != nil {
		return PairRequest{}, fmt.Errorf("load pair request: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return PairRequest{}, fmt.Errorf("parse pair request expiry: %w", err)
	}
	request.ExpiresAt = parsed
	return request, nil
}

func (r *Repository) AcceptPairRequest(ctx context.Context, token string, accepterID int64) (*Pair, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin accept pair request: %w", err)
	}
	defer tx.Rollback()

	request, err := getPairRequestByTokenTx(ctx, tx, token)
	if err != nil {
		return nil, err
	}
	if request.Status != "pending" || time.Now().UTC().After(request.ExpiresAt) {
		return nil, ErrPairRequestNotFound
	}
	if request.RequesterID == accepterID {
		return nil, ErrSelfPair
	}
	if request.TargetTelegramID.Valid && request.TargetTelegramID.Int64 != accepterID {
		return nil, ErrPairRequestForbidden
	}
	if active, err := activePairForUserTx(ctx, tx, request.RequesterID); err != nil {
		return nil, err
	} else if active != nil {
		return nil, ErrAlreadyPaired
	}
	if active, err := activePairForUserTx(ctx, tx, accepterID); err != nil {
		return nil, err
	} else if active != nil {
		return nil, ErrAlreadyPaired
	}

	userA, userB := orderedPairUsers(request.RequesterID, accepterID)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO pairs (user_a_id, user_b_id, status, active_level, highest_unlocked_level)
		VALUES (?, ?, 'active', 1, 1)
	`, userA, userB)
	if err != nil {
		return nil, fmt.Errorf("create pair: %w", err)
	}
	pairID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read created pair id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pair_requests
		SET status = 'accepted', updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, request.ID); err != nil {
		return nil, fmt.Errorf("mark pair request accepted: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit accept pair request: %w", err)
	}
	return &Pair{ID: pairID, UserAID: userA, UserBID: userB, Status: "active", ActiveLevel: 1, HighestUnlockedLevel: 1}, nil
}

func (r *Repository) DeclinePairRequest(ctx context.Context, token string, userID int64) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE pair_requests
		SET status = 'declined', updated_at = CURRENT_TIMESTAMP
		WHERE invite_token = ?
		  AND status = 'pending'
		  AND (target_telegram_id IS NULL OR target_telegram_id = ?)
	`, token, userID)
	if err != nil {
		return fmt.Errorf("decline pair request: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read declined pair request rows: %w", err)
	}
	if rows == 0 {
		return ErrPairRequestNotFound
	}
	return nil
}

func (r *Repository) ActivePairForUser(ctx context.Context, userID int64) (*Pair, error) {
	return activePairForUser(ctx, r.db, userID)
}

func (r *Repository) PairCardCount(ctx context.Context, pairID int64, level int) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM pair_card_history
		WHERE pair_id = ? AND level = ?
	`, pairID, level).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pair cards: %w", err)
	}
	return count, nil
}

func (r *Repository) UserMaturity(ctx context.Context, userID int64) (is18Plus, matureOptIn bool, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT is_18_plus, mature_opt_in
		FROM users
		WHERE telegram_id = ?
	`, userID).Scan(&is18Plus, &matureOptIn)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("load user maturity: %w", err)
	}
	return is18Plus, matureOptIn, nil
}

func (r *Repository) CreateGameSession(ctx context.Context, session GameSession) (GameSession, error) {
	if session.PairID == 0 {
		return GameSession{}, errors.New("game session pair id is required")
	}
	if session.Level <= 0 {
		return GameSession{}, errors.New("game session level is required")
	}
	if strings.TrimSpace(session.QuestionID) == "" {
		return GameSession{}, errors.New("game session question id is required")
	}
	status := session.Status
	if status == "" {
		status = GameSessionPendingAcceptance
	}
	source := session.QuestionSource
	if source == "" {
		source = "stock"
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO game_sessions (
			pair_id, level, question_id, question_source, question_text_uk,
			question_text_en, requires_mature_opt_in, status, deck_cycle, invited_by_user_id,
			accepted_by_user_id, invite_expires_at, started_at, revealed_at, completed_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, session.PairID, session.Level, session.QuestionID, source, session.QuestionTextUK,
		session.QuestionTextEN, session.RequiresMature, status, session.DeckCycle, nullableInt64Value(sql.NullInt64{Int64: session.InvitedByUserID, Valid: session.InvitedByUserID != 0}),
		nullableInt64Value(session.AcceptedByUserID), nullableTimeValue(session.InviteExpiresAt),
		nullableTimeValue(session.StartedAt), nullableTimeValue(session.RevealedAt), nullableTimeValue(session.CompletedAt))
	if err != nil {
		return GameSession{}, fmt.Errorf("create game session: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return GameSession{}, fmt.Errorf("read game session id: %w", err)
	}
	return r.GameSession(ctx, id)
}

func (r *Repository) GameSession(ctx context.Context, sessionID int64) (GameSession, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, pair_id, level, question_id, status, deck_cycle,
		       question_source, question_text_uk, question_text_en, requires_mature_opt_in,
		       invited_by_user_id, accepted_by_user_id, invite_expires_at,
		       started_at, revealed_at, completed_at
		FROM game_sessions
		WHERE id = ?
	`, sessionID)
	session, err := scanGameSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return GameSession{}, ErrGameSessionNotFound
	}
	if err != nil {
		return GameSession{}, fmt.Errorf("load game session: %w", err)
	}
	return session, nil
}

func (r *Repository) CurrentGameSessionForPair(ctx context.Context, pairID int64) (*GameSession, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, pair_id, level, question_id, status, deck_cycle,
		       question_source, question_text_uk, question_text_en, requires_mature_opt_in,
		       invited_by_user_id, accepted_by_user_id, invite_expires_at,
		       started_at, revealed_at, completed_at
		FROM game_sessions
		WHERE pair_id = ?
		  AND status IN ('pending_acceptance', 'active', 'revealed')
		ORDER BY id DESC
		LIMIT 1
	`, pairID)
	session, err := scanGameSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load current game session: %w", err)
	}
	return &session, nil
}

func (r *Repository) UpdateGameSessionStatus(ctx context.Context, sessionID int64, status string, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE game_sessions
		SET status = ?, updated_at = ?
		WHERE id = ?
	`, status, now.UTC().Format(time.RFC3339Nano), sessionID)
	if err != nil {
		return fmt.Errorf("update game session status: %w", err)
	}
	return nil
}

func (r *Repository) AcceptGameSession(ctx context.Context, sessionID, accepterID int64, now time.Time) (GameSession, error) {
	_, err := r.db.ExecContext(ctx, `
		UPDATE game_sessions
		SET status = 'active',
		    accepted_by_user_id = ?,
		    started_at = ?,
		    updated_at = ?
		WHERE id = ? AND status = 'pending_acceptance'
	`, accepterID, now.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano), sessionID)
	if err != nil {
		return GameSession{}, fmt.Errorf("accept game session: %w", err)
	}
	return r.GameSession(ctx, sessionID)
}

func (r *Repository) UpsertGameAnswer(ctx context.Context, answer GameAnswer) error {
	if answer.SessionID == 0 || answer.UserID == 0 {
		return errors.New("game answer session and user are required")
	}
	completedAt := answer.CompletedAt
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO game_answers (
			session_id, user_id, completion_type, answer_text_encrypted,
			completed_at, revealed_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(session_id, user_id) DO UPDATE SET
			completion_type = excluded.completion_type,
			answer_text_encrypted = excluded.answer_text_encrypted,
			completed_at = excluded.completed_at,
			revealed_at = excluded.revealed_at,
			updated_at = CURRENT_TIMESTAMP
	`, answer.SessionID, answer.UserID, answer.CompletionType, nullableBytes(answer.AnswerTextEncrypted),
		completedAt.UTC().Format(time.RFC3339Nano), nullableTimeValue(answer.RevealedAt))
	if err != nil {
		return fmt.Errorf("upsert game answer: %w", err)
	}
	return nil
}

func (r *Repository) GameAnswers(ctx context.Context, sessionID int64) ([]GameAnswer, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT session_id, user_id, completion_type, answer_text_encrypted,
		       completed_at, revealed_at
		FROM game_answers
		WHERE session_id = ?
		ORDER BY user_id
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query game answers: %w", err)
	}
	defer rows.Close()
	var answers []GameAnswer
	for rows.Next() {
		var answer GameAnswer
		var completedAt string
		var revealedAt sql.NullString
		if err := rows.Scan(&answer.SessionID, &answer.UserID, &answer.CompletionType, &answer.AnswerTextEncrypted, &completedAt, &revealedAt); err != nil {
			return nil, err
		}
		parsed, err := parseStoredTime(completedAt)
		if err != nil {
			return nil, fmt.Errorf("parse answer completed_at: %w", err)
		}
		answer.CompletedAt = parsed
		answer.RevealedAt = parseNullableStoredTime(revealedAt)
		answers = append(answers, answer)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return answers, nil
}

func (r *Repository) RevealGameSession(ctx context.Context, sessionID int64, now time.Time) (GameSession, error) {
	ts := now.UTC().Format(time.RFC3339Nano)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return GameSession{}, fmt.Errorf("begin reveal game session: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE game_sessions
		SET status = 'revealed', revealed_at = ?, updated_at = ?
		WHERE id = ? AND status = 'active'
	`, ts, ts, sessionID); err != nil {
		return GameSession{}, fmt.Errorf("mark game session revealed: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE game_answers
		SET revealed_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE session_id = ?
	`, ts, sessionID); err != nil {
		return GameSession{}, fmt.Errorf("mark game answers revealed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return GameSession{}, fmt.Errorf("commit reveal game session: %w", err)
	}
	return r.GameSession(ctx, sessionID)
}

func (r *Repository) CompleteGameSession(ctx context.Context, sessionID int64, now time.Time) error {
	ts := now.UTC().Format(time.RFC3339Nano)
	_, err := r.db.ExecContext(ctx, `
		UPDATE game_sessions
		SET status = 'completed', completed_at = ?, updated_at = ?
		WHERE id = ? AND status = 'revealed'
	`, ts, ts, sessionID)
	if err != nil {
		return fmt.Errorf("complete game session: %w", err)
	}
	return nil
}

func (r *Repository) LatestDeckCycle(ctx context.Context, pairID int64, level int) (int, error) {
	var cycle sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT MAX(deck_cycle)
		FROM pair_card_history
		WHERE pair_id = ? AND level = ?
	`, pairID, level).Scan(&cycle)
	if err != nil {
		return 0, fmt.Errorf("load latest deck cycle: %w", err)
	}
	if !cycle.Valid {
		return 0, nil
	}
	return int(cycle.Int64), nil
}

func (r *Repository) SeenCardIDs(ctx context.Context, pairID int64, level, cycle int) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT question_id
		FROM pair_card_history
		WHERE pair_id = ? AND level = ? AND deck_cycle = ?
	`, pairID, level, cycle)
	if err != nil {
		return nil, fmt.Errorf("query seen card ids: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var questionID string
		if err := rows.Scan(&questionID); err != nil {
			return nil, err
		}
		seen[questionID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return seen, nil
}

func (r *Repository) RecordPairCardCompletion(ctx context.Context, pairID int64, questionID string, level, cycle int, completedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO pair_card_history (pair_id, question_id, level, deck_cycle, completed_at)
		VALUES (?, ?, ?, ?, ?)
	`, pairID, questionID, level, cycle, completedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record pair card completion: %w", err)
	}
	return nil
}

func (r *Repository) UpdatePairLevel(ctx context.Context, pairID int64, level int) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE pairs
		SET active_level = ?,
		    highest_unlocked_level = CASE
		        WHEN highest_unlocked_level < ? THEN ?
		        ELSE highest_unlocked_level
		    END
		WHERE id = ?
	`, level, level, level, pairID)
	if err != nil {
		return fmt.Errorf("update pair level: %w", err)
	}
	return nil
}

func (r *Repository) GrantEntitlement(ctx context.Context, entitlement Entitlement) error {
	if entitlement.UserID == 0 {
		return errors.New("entitlement user id is required")
	}
	if entitlement.Type == "" || entitlement.UnlockID == "" {
		return errors.New("entitlement type and unlock id are required")
	}
	source := entitlement.Source
	if source == "" {
		source = "admin_grant"
	}
	var expires any
	if entitlement.ExpiresAt.Valid {
		expires = entitlement.ExpiresAt.Time.UTC().Format(time.RFC3339Nano)
	}
	var sourceReceipt any
	if entitlement.SourceReceiptID.Valid {
		sourceReceipt = entitlement.SourceReceiptID.Int64
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO entitlements (user_id, unlock_type, unlock_id, source, source_receipt_id, expires_at, active, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id, unlock_type, unlock_id) DO UPDATE SET
			source = excluded.source,
			source_receipt_id = COALESCE(excluded.source_receipt_id, entitlements.source_receipt_id),
			expires_at = excluded.expires_at,
			active = 1,
			updated_at = CURRENT_TIMESTAMP
	`, entitlement.UserID, entitlement.Type, entitlement.UnlockID, source, sourceReceipt, expires)
	if err != nil {
		return fmt.Errorf("grant entitlement: %w", err)
	}
	return nil
}

func (r *Repository) StorePurchaseReceipt(ctx context.Context, receipt PurchaseReceipt) (int64, bool, error) {
	if receipt.UserID == 0 {
		return 0, false, errors.New("receipt user id is required")
	}
	if receipt.SKU == "" || receipt.TelegramPaymentChargeID == "" {
		return 0, false, errors.New("receipt sku and telegram charge id are required")
	}
	currency := receipt.Currency
	if currency == "" {
		currency = "XTR"
	}
	status := receipt.Status
	if status == "" {
		status = "successful"
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO purchase_receipts (
			user_id, sku, currency, stars_amount, telegram_payment_charge_id,
			provider_payment_charge_id, status
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, receipt.UserID, receipt.SKU, currency, receipt.StarsAmount, receipt.TelegramPaymentChargeID,
		nullableStringValue(receipt.ProviderPaymentChargeID), status)
	if err != nil {
		return 0, false, fmt.Errorf("store purchase receipt: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("read purchase receipt rows: %w", err)
	}
	stored, err := r.PurchaseReceiptByCharge(ctx, receipt.TelegramPaymentChargeID)
	if err != nil {
		return 0, false, err
	}
	return stored.ID, rows == 1, nil
}

func (r *Repository) PurchaseReceiptByCharge(ctx context.Context, telegramPaymentChargeID string) (PurchaseReceipt, error) {
	var receipt PurchaseReceipt
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, sku, currency, stars_amount, telegram_payment_charge_id,
		       provider_payment_charge_id, status
		FROM purchase_receipts
		WHERE telegram_payment_charge_id = ?
	`, telegramPaymentChargeID).Scan(&receipt.ID, &receipt.UserID, &receipt.SKU, &receipt.Currency,
		&receipt.StarsAmount, &receipt.TelegramPaymentChargeID, &receipt.ProviderPaymentChargeID, &receipt.Status)
	if err != nil {
		return PurchaseReceipt{}, fmt.Errorf("load purchase receipt: %w", err)
	}
	return receipt, nil
}

func (r *Repository) DeleteUser(ctx context.Context, telegramID int64) error {
	if _, err := r.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE telegram_id = ?`, telegramID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

func (r *Repository) RevokeEntitlement(ctx context.Context, userID int64, entitlementType, unlockID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE entitlements
		SET active = 0, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND unlock_type = ? AND unlock_id = ?
	`, userID, entitlementType, unlockID)
	if err != nil {
		return fmt.Errorf("revoke entitlement: %w", err)
	}
	return nil
}

func (r *Repository) LogAdminAction(ctx context.Context, adminID, targetID int64, action, entitlementType, unlockID string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO admin_audit_log (admin_user_id, target_user_id, action, unlock_type, unlock_id)
		VALUES (?, ?, ?, ?, ?)
	`, adminID, targetID, action, entitlementType, unlockID)
	if err != nil {
		return fmt.Errorf("log admin action: %w", err)
	}
	return nil
}

func (r *Repository) AdminAuditCount(ctx context.Context, adminID, targetID int64, action, entitlementType, unlockID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM admin_audit_log
		WHERE admin_user_id = ?
		  AND target_user_id = ?
		  AND action = ?
		  AND unlock_type = ?
		  AND unlock_id = ?
	`, adminID, targetID, action, entitlementType, unlockID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count admin audit rows: %w", err)
	}
	return count, nil
}

func (r *Repository) UserHasEntitlement(ctx context.Context, userID int64, entitlementType, unlockID string) (bool, error) {
	var count int
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM entitlements
		WHERE user_id = ?
		  AND unlock_type = ?
		  AND unlock_id = ?
		  AND active = 1
		  AND (expires_at IS NULL OR expires_at > ?)
	`, userID, entitlementType, unlockID, now).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check entitlement: %w", err)
	}
	return count > 0, nil
}

func (r *Repository) MarkSupportPrompted(ctx context.Context, pairID int64, promptedAt time.Time, messageID int64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO pair_support_prompt_state (pair_id, last_prompted_at, last_prompt_message_id, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(pair_id) DO UPDATE SET
			last_prompted_at = excluded.last_prompted_at,
			last_prompt_message_id = excluded.last_prompt_message_id,
			updated_at = CURRENT_TIMESTAMP
	`, pairID, promptedAt.UTC().Format(time.RFC3339Nano), messageID)
	if err != nil {
		return fmt.Errorf("mark support prompted: %w", err)
	}
	return nil
}

func (r *Repository) LastSupportPromptAt(ctx context.Context, pairID int64) (*time.Time, error) {
	var raw sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT last_prompted_at
		FROM pair_support_prompt_state
		WHERE pair_id = ?
	`, pairID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load support prompt timestamp: %w", err)
	}
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil {
		return nil, fmt.Errorf("parse support prompt timestamp: %w", err)
	}
	return &parsed, nil
}

func (r *Repository) CreateCustomQuestion(ctx context.Context, creatorID int64, text string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO custom_questions (creator_id, question_text)
		VALUES (?, ?)
	`, creatorID, text)
	if err != nil {
		return 0, fmt.Errorf("create custom question: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return id, nil
}

func (r *Repository) DeleteCustomQuestion(ctx context.Context, id, creatorID int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE custom_questions
		SET deleted_at = COALESCE(deleted_at, CURRENT_TIMESTAMP)
		WHERE id = ? AND creator_id = ?
	`, id, creatorID)
	if err != nil {
		return fmt.Errorf("delete custom question: %w", err)
	}
	return nil
}

func (r *Repository) GetPairCustomQuestions(ctx context.Context, userID int64) ([]CustomQuestion, error) {
	// First check if they have an active pair
	pair, err := r.ActivePairForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	var query string
	var args []any
	if pair != nil {
		query = `
			SELECT id, creator_id, question_text, created_at, deleted_at
			FROM custom_questions
			WHERE (creator_id = ? OR creator_id = ?)
			  AND deleted_at IS NULL
			ORDER BY created_at ASC
		`
		args = []any{pair.UserAID, pair.UserBID}
	} else {
		query = `
			SELECT id, creator_id, question_text, created_at, deleted_at
			FROM custom_questions
			WHERE creator_id = ?
			  AND deleted_at IS NULL
			ORDER BY created_at ASC
		`
		args = []any{userID}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query custom questions: %w", err)
	}
	defer rows.Close()

	var list []CustomQuestion
	for rows.Next() {
		var q CustomQuestion
		var createdAtStr string
		var deletedAt sql.NullString
		if err := rows.Scan(&q.ID, &q.CreatorID, &q.QuestionText, &createdAtStr, &deletedAt); err != nil {
			return nil, err
		}
		if parsed, err := time.Parse(time.RFC3339Nano, createdAtStr); err == nil {
			q.CreatedAt = parsed
		} else if parsed, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			q.CreatedAt = parsed
		} else if parsed, err := time.Parse("2006-01-02 15:04:05", createdAtStr); err == nil {
			q.CreatedAt = parsed
		} else {
			q.CreatedAt = time.Now()
		}
		q.DeletedAt = parseNullableStoredTime(deletedAt)
		list = append(list, q)
	}
	return list, nil
}

func (r *Repository) PairJournalEntries(ctx context.Context, userID int64) ([]JournalEntry, error) {
	pair, err := r.ActivePairForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if pair == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, pair_id, level, question_id, question_text_uk, question_text_en,
		       requires_mature_opt_in, revealed_at
		FROM game_sessions
		WHERE pair_id = ?
		  AND status IN ('revealed', 'completed')
		  AND revealed_at IS NOT NULL
		ORDER BY revealed_at DESC, id DESC
	`, pair.ID)
	if err != nil {
		return nil, fmt.Errorf("query pair journal entries: %w", err)
	}
	defer rows.Close()

	var entries []JournalEntry
	for rows.Next() {
		var entry JournalEntry
		var revealedAt string
		if err := rows.Scan(&entry.SessionID, &entry.PairID, &entry.Level, &entry.QuestionID,
			&entry.QuestionTextUK, &entry.QuestionTextEN, &entry.RequiresMature, &revealedAt); err != nil {
			return nil, err
		}
		parsed, err := parseStoredTime(revealedAt)
		if err != nil {
			return nil, fmt.Errorf("parse journal revealed_at: %w", err)
		}
		entry.RevealedAt = parsed
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for idx := range entries {
		answers, err := r.journalAnswers(ctx, entries[idx].SessionID)
		if err != nil {
			return nil, err
		}
		entries[idx].Answers = answers
	}
	return entries, nil
}

func (r *Repository) journalAnswers(ctx context.Context, sessionID int64) ([]JournalAnswer, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT user_id, completion_type, answer_text_encrypted
		FROM game_answers
		WHERE session_id = ?
		ORDER BY user_id
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query journal answers: %w", err)
	}
	defer rows.Close()
	var answers []JournalAnswer
	for rows.Next() {
		var answer JournalAnswer
		if err := rows.Scan(&answer.UserID, &answer.CompletionType, &answer.AnswerTextEncrypted); err != nil {
			return nil, err
		}
		answers = append(answers, answer)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return answers, nil
}

func normalizeUsername(username string) string {
	username = strings.TrimSpace(strings.ToLower(username))
	username = strings.TrimPrefix(username, "@")
	return username
}

type scanner interface {
	Scan(...any) error
}

func scanGameSession(row scanner) (GameSession, error) {
	var session GameSession
	var invitedBy sql.NullInt64
	var inviteExpiresAt sql.NullString
	var startedAt sql.NullString
	var revealedAt sql.NullString
	var completedAt sql.NullString
	if err := row.Scan(&session.ID, &session.PairID, &session.Level, &session.QuestionID, &session.Status,
		&session.DeckCycle, &session.QuestionSource, &session.QuestionTextUK, &session.QuestionTextEN,
		&session.RequiresMature, &invitedBy, &session.AcceptedByUserID, &inviteExpiresAt,
		&startedAt, &revealedAt, &completedAt); err != nil {
		return GameSession{}, err
	}
	if session.QuestionSource == "" {
		session.QuestionSource = "stock"
	}
	if invitedBy.Valid {
		session.InvitedByUserID = invitedBy.Int64
	}
	session.InviteExpiresAt = parseNullableStoredTime(inviteExpiresAt)
	session.StartedAt = parseNullableStoredTime(startedAt)
	session.RevealedAt = parseNullableStoredTime(revealedAt)
	session.CompletedAt = parseNullableStoredTime(completedAt)
	return session, nil
}

func parseNullableStoredTime(raw sql.NullString) sql.NullTime {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return sql.NullTime{}
	}
	parsed, err := parseStoredTime(raw.String)
	if err != nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: parsed, Valid: true}
}

func parseStoredTime(raw string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	return time.Parse("2006-01-02 15:04:05", raw)
}

func nullableStringValue(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func nullableInt64Value(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func nullableFloat64Value(value sql.NullFloat64) any {
	if value.Valid {
		return value.Float64
	}
	return nil
}

func nullableTimeValue(value sql.NullTime) any {
	if value.Valid {
		return value.Time.UTC().Format(time.RFC3339Nano)
	}
	return nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func orderedPairUsers(a, b int64) (int64, int64) {
	if a < b {
		return a, b
	}
	return b, a
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func activePairForUser(ctx context.Context, q queryer, userID int64) (*Pair, error) {
	var pair Pair
	err := q.QueryRowContext(ctx, `
		SELECT id, user_a_id, user_b_id, status, active_level, highest_unlocked_level
		FROM pairs
		WHERE status = 'active'
		  AND (user_a_id = ? OR user_b_id = ?)
	`, userID, userID).Scan(&pair.ID, &pair.UserAID, &pair.UserBID, &pair.Status, &pair.ActiveLevel, &pair.HighestUnlockedLevel)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load active pair: %w", err)
	}
	return &pair, nil
}

func activePairForUserTx(ctx context.Context, tx *sql.Tx, userID int64) (*Pair, error) {
	return activePairForUser(ctx, tx, userID)
}

func getPairRequestByTokenTx(ctx context.Context, tx *sql.Tx, token string) (PairRequest, error) {
	var request PairRequest
	var expiresAt string
	err := tx.QueryRowContext(ctx, `
		SELECT id, requester_id, target_telegram_id, target_username_normalized,
		       target_phone_hash, invite_token, status, expires_at
		FROM pair_requests
		WHERE invite_token = ?
	`, token).Scan(&request.ID, &request.RequesterID, &request.TargetTelegramID,
		&request.TargetUsernameNormalized, &request.TargetPhoneHash, &request.InviteToken,
		&request.Status, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PairRequest{}, ErrPairRequestNotFound
	}
	if err != nil {
		return PairRequest{}, fmt.Errorf("load pair request: %w", err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return PairRequest{}, fmt.Errorf("parse pair request expiry: %w", err)
	}
	request.ExpiresAt = parsed
	return request, nil
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS users (
	telegram_id INTEGER PRIMARY KEY,
	username_normalized TEXT,
	display_name TEXT NOT NULL DEFAULT '',
	gender TEXT NOT NULL DEFAULT '',
	language TEXT NOT NULL DEFAULT 'uk',
	phone_e164_encrypted BLOB,
	phone_lookup_hash TEXT,
	is_18_plus BOOLEAN NOT NULL DEFAULT 0,
	adult_confirmed_at TEXT,
	mature_opt_in BOOLEAN NOT NULL DEFAULT 0,
	mature_opted_in_at TEXT,
	theme_base_color TEXT,
	selected_style_id TEXT NOT NULL DEFAULT 'default_warm',
	selected_background_asset_id TEXT,
	selected_font_id TEXT NOT NULL DEFAULT 'nunito_regular',
	custom_border_radius INTEGER,
	custom_glass_opacity REAL,
	onboarding_status TEXT NOT NULL DEFAULT 'new',
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS users_username_normalized_idx
	ON users(username_normalized)
	WHERE username_normalized IS NOT NULL AND username_normalized != '';

CREATE INDEX IF NOT EXISTS users_phone_lookup_hash_idx
	ON users(phone_lookup_hash)
	WHERE phone_lookup_hash IS NOT NULL;

CREATE TABLE IF NOT EXISTS pairs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_a_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	user_b_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	status TEXT NOT NULL DEFAULT 'active',
	active_level INTEGER NOT NULL DEFAULT 1,
	highest_unlocked_level INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	ended_at TEXT,
	CHECK(user_a_id < user_b_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS pairs_unique_users_idx
	ON pairs(user_a_id, user_b_id);

CREATE UNIQUE INDEX IF NOT EXISTS pairs_one_active_user_a_idx
	ON pairs(user_a_id)
	WHERE status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS pairs_one_active_user_b_idx
	ON pairs(user_b_id)
	WHERE status = 'active';

CREATE TABLE IF NOT EXISTS pair_requests (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	requester_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	target_telegram_id INTEGER,
	target_username_normalized TEXT,
	target_phone_hash TEXT,
	invite_token TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL DEFAULT 'pending',
	expires_at TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS game_sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	pair_id INTEGER NOT NULL REFERENCES pairs(id) ON DELETE CASCADE,
	level INTEGER NOT NULL,
	question_id TEXT NOT NULL,
	question_source TEXT NOT NULL DEFAULT 'stock',
	question_text_uk TEXT NOT NULL DEFAULT '',
	question_text_en TEXT NOT NULL DEFAULT '',
	requires_mature_opt_in BOOLEAN NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'active',
	deck_cycle INTEGER NOT NULL DEFAULT 0,
	invited_by_user_id INTEGER,
	accepted_by_user_id INTEGER,
	invite_expires_at TEXT,
	started_at TEXT,
	revealed_at TEXT,
	completed_at TEXT,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS game_sessions_one_current_pair_idx
	ON game_sessions(pair_id)
	WHERE status IN ('pending_acceptance', 'active', 'revealed');

CREATE TABLE IF NOT EXISTS game_answers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id INTEGER NOT NULL REFERENCES game_sessions(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	completion_type TEXT NOT NULL,
	answer_text_encrypted BLOB,
	completed_at TEXT NOT NULL,
	revealed_at TEXT,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(session_id, user_id)
);

CREATE TABLE IF NOT EXISTS pair_card_history (
	pair_id INTEGER NOT NULL REFERENCES pairs(id) ON DELETE CASCADE,
	question_id TEXT NOT NULL,
	level INTEGER NOT NULL,
	deck_cycle INTEGER NOT NULL DEFAULT 0,
	completed_at TEXT NOT NULL,
	PRIMARY KEY(pair_id, question_id, deck_cycle)
);

CREATE TABLE IF NOT EXISTS pair_support_prompt_state (
	pair_id INTEGER PRIMARY KEY,
	last_prompted_at TEXT,
	last_prompt_message_id INTEGER,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS theme_assets (
	id TEXT PRIMARY KEY,
	owner_user_id INTEGER REFERENCES users(telegram_id) ON DELETE CASCADE,
	kind TEXT NOT NULL,
	minio_object_key TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'active',
	width INTEGER NOT NULL DEFAULT 0,
	height INTEGER NOT NULL DEFAULT 0,
	size_bytes INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pair_theme_shares (
	pair_id INTEGER NOT NULL REFERENCES pairs(id) ON DELETE CASCADE,
	asset_id TEXT NOT NULL REFERENCES theme_assets(id) ON DELETE CASCADE,
	shared_by_user_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	status TEXT NOT NULL DEFAULT 'active',
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(pair_id, asset_id)
);

CREATE TABLE IF NOT EXISTS purchase_receipts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	sku TEXT NOT NULL,
	currency TEXT NOT NULL DEFAULT 'XTR',
	stars_amount INTEGER NOT NULL,
	telegram_payment_charge_id TEXT NOT NULL UNIQUE,
	provider_payment_charge_id TEXT,
	status TEXT NOT NULL DEFAULT 'successful',
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS entitlements (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	unlock_type TEXT NOT NULL,
	unlock_id TEXT NOT NULL,
	source TEXT NOT NULL,
	source_receipt_id INTEGER REFERENCES purchase_receipts(id) ON DELETE SET NULL,
	expires_at TEXT,
	active BOOLEAN NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(user_id, unlock_type, unlock_id)
);

CREATE TABLE IF NOT EXISTS admin_audit_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	admin_user_id INTEGER NOT NULL,
	target_user_id INTEGER NOT NULL,
	action TEXT NOT NULL,
	unlock_type TEXT NOT NULL,
	unlock_id TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS custom_questions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	creator_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	question_text TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS custom_questions_creator_id_idx ON custom_questions(creator_id);

CREATE TABLE IF NOT EXISTS pair_position_marks (
	pair_id      INTEGER NOT NULL REFERENCES pairs(id) ON DELETE CASCADE,
	position_id  TEXT    NOT NULL,
	tried_at     TIMESTAMP,
	favorited_at TIMESTAMP,
	hidden_at    TIMESTAMP,
	marked_by    INTEGER REFERENCES users(telegram_id) ON DELETE SET NULL,
	updated_at   TIMESTAMP NOT NULL,
	PRIMARY KEY (pair_id, position_id)
);
`
