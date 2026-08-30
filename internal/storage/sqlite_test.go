package storage_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wrnrs/internal/storage"
)

func TestSQLiteMigrationsCreateCoreTablesAndPremiumEntitlements(t *testing.T) {
	ctx := context.Background()
	db, err := storage.OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	repo := storage.NewRepository(db)
	if err := repo.UpsertUser(ctx, storage.User{
		TelegramID:      1001,
		Username:        "alice",
		DisplayName:     "Alice",
		Gender:          "female",
		Language:        "en",
		ThemeBaseColor:  "#d98c9f",
		SelectedStyleID: "default_warm",
	}); err != nil {
		t.Fatalf("UpsertUser returned error: %v", err)
	}

	if premium, err := repo.UserHasEntitlement(ctx, 1001, storage.EntitlementPremiumAccess, "premium_access"); err != nil {
		t.Fatalf("UserHasEntitlement before grant returned error: %v", err)
	} else if premium {
		t.Fatal("user had premium before grant")
	}

	if err := repo.GrantEntitlement(ctx, storage.Entitlement{
		UserID:   1001,
		Type:     storage.EntitlementPremiumAccess,
		UnlockID: "premium_access",
		Source:   "admin_grant",
	}); err != nil {
		t.Fatalf("GrantEntitlement returned error: %v", err)
	}

	if premium, err := repo.UserHasEntitlement(ctx, 1001, storage.EntitlementPremiumAccess, "premium_access"); err != nil {
		t.Fatalf("UserHasEntitlement after grant returned error: %v", err)
	} else if !premium {
		t.Fatal("user did not have premium after grant")
	}
}

func TestDeleteUserCascadesPairsAndEntitlements(t *testing.T) {
	ctx := context.Background()
	db, err := storage.OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	repo := storage.NewRepository(db)
	for _, user := range []storage.User{
		{TelegramID: 1001, Username: "alice", DisplayName: "Alice", Language: "en", ThemeBaseColor: "#d98c9f", SelectedStyleID: "default_warm"},
		{TelegramID: 2002, Username: "bob", DisplayName: "Bob", Language: "en", ThemeBaseColor: "#8da68f", SelectedStyleID: "default_warm"},
	} {
		if err := repo.UpsertUser(ctx, user); err != nil {
			t.Fatalf("UpsertUser(%d) returned error: %v", user.TelegramID, err)
		}
	}
	request, err := repo.CreatePairRequest(ctx, storage.PairRequest{
		RequesterID: 1001,
		InviteToken: "token-delete",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePairRequest returned error: %v", err)
	}
	if _, err := repo.AcceptPairRequest(ctx, request.InviteToken, 2002); err != nil {
		t.Fatalf("AcceptPairRequest returned error: %v", err)
	}
	if err := repo.GrantEntitlement(ctx, storage.Entitlement{
		UserID:   1001,
		Type:     storage.EntitlementPremiumAccess,
		UnlockID: "premium_access",
		Source:   "admin_grant",
	}); err != nil {
		t.Fatalf("GrantEntitlement returned error: %v", err)
	}

	if err := repo.DeleteUser(ctx, 1001); err != nil {
		t.Fatalf("DeleteUser returned error: %v", err)
	}

	for table, want := range map[string]int{
		"users":        1,
		"pairs":        0,
		"entitlements": 0,
	} {
		var got int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
			t.Fatalf("count %s returned error: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s count = %d, want %d", table, got, want)
		}
	}
}

func TestStorePurchaseReceiptIsIdempotentAndLinksEntitlement(t *testing.T) {
	ctx := context.Background()
	db, err := storage.OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	repo := storage.NewRepository(db)
	if err := repo.UpsertUser(ctx, storage.User{
		TelegramID:      1001,
		Username:        "alice",
		DisplayName:     "Alice",
		Language:        "en",
		ThemeBaseColor:  "#d98c9f",
		SelectedStyleID: "default_warm",
	}); err != nil {
		t.Fatalf("UpsertUser returned error: %v", err)
	}

	receipt := storage.PurchaseReceipt{
		UserID:                  1001,
		SKU:                     "premium_lifetime",
		Currency:                "XTR",
		StarsAmount:             250,
		TelegramPaymentChargeID: "charge-1",
		ProviderPaymentChargeID: sql.NullString{String: "provider-1", Valid: true},
	}
	id, inserted, err := repo.StorePurchaseReceipt(ctx, receipt)
	if err != nil {
		t.Fatalf("StorePurchaseReceipt returned error: %v", err)
	}
	if !inserted || id == 0 {
		t.Fatalf("receipt id=%d inserted=%v, want inserted id", id, inserted)
	}
	againID, inserted, err := repo.StorePurchaseReceipt(ctx, receipt)
	if err != nil {
		t.Fatalf("second StorePurchaseReceipt returned error: %v", err)
	}
	if inserted || againID != id {
		t.Fatalf("duplicate receipt id=%d inserted=%v, want same id and inserted=false", againID, inserted)
	}

	if err := repo.GrantEntitlement(ctx, storage.Entitlement{
		UserID:          1001,
		Type:            storage.EntitlementPremiumAccess,
		UnlockID:        "premium_access",
		Source:          "purchase",
		SourceReceiptID: sql.NullInt64{Int64: id, Valid: true},
	}); err != nil {
		t.Fatalf("GrantEntitlement returned error: %v", err)
	}

	var sourceReceiptID sql.NullInt64
	if err := db.QueryRowContext(ctx, `
		SELECT source_receipt_id
		FROM entitlements
		WHERE user_id = ? AND unlock_type = ? AND unlock_id = ?
	`, 1001, storage.EntitlementPremiumAccess, "premium_access").Scan(&sourceReceiptID); err != nil {
		t.Fatalf("load entitlement receipt id returned error: %v", err)
	}
	if !sourceReceiptID.Valid || sourceReceiptID.Int64 != id {
		t.Fatalf("source_receipt_id = %#v, want %d", sourceReceiptID, id)
	}
}

func TestSupportPromptTimestampPersistsPerPair(t *testing.T) {
	ctx := context.Background()
	db, err := storage.OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	repo := storage.NewRepository(db)
	at := time.Date(2026, 6, 14, 12, 30, 0, 0, time.UTC)

	if last, err := repo.LastSupportPromptAt(ctx, 77); err != nil {
		t.Fatalf("LastSupportPromptAt returned error: %v", err)
	} else if last != nil {
		t.Fatalf("expected no prompt timestamp, got %v", last)
	}

	if err := repo.MarkSupportPrompted(ctx, 77, at, 12345); err != nil {
		t.Fatalf("MarkSupportPrompted returned error: %v", err)
	}

	last, err := repo.LastSupportPromptAt(ctx, 77)
	if err != nil {
		t.Fatalf("LastSupportPromptAt after mark returned error: %v", err)
	}
	if last == nil || !last.Equal(at) {
		t.Fatalf("last prompt timestamp = %v, want %v", last, at)
	}
}

func TestCustomQuestions(t *testing.T) {
	ctx := context.Background()
	db, err := storage.OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	repo := storage.NewRepository(db)

	// Create two users
	for _, user := range []storage.User{
		{TelegramID: 1001, Username: "alice", DisplayName: "Alice", Language: "en", ThemeBaseColor: "#d98c9f", SelectedStyleID: "default_warm"},
		{TelegramID: 2002, Username: "bob", DisplayName: "Bob", Language: "en", ThemeBaseColor: "#8da68f", SelectedStyleID: "default_warm"},
	} {
		if err := repo.UpsertUser(ctx, user); err != nil {
			t.Fatalf("UpsertUser(%d) returned error: %v", user.TelegramID, err)
		}
	}

	// Get custom questions for Alice (unpaired)
	list, err := repo.GetPairCustomQuestions(ctx, 1001)
	if err != nil {
		t.Fatalf("GetPairCustomQuestions returned error: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 custom questions, got %d", len(list))
	}

	// Alice creates custom question
	qId, err := repo.CreateCustomQuestion(ctx, 1001, "What is your favorite memory?")
	if err != nil {
		t.Fatalf("CreateCustomQuestion returned error: %v", err)
	}
	if qId == 0 {
		t.Fatal("expected positive ID, got 0")
	}

	// Verify Alice can see it
	list, err = repo.GetPairCustomQuestions(ctx, 1001)
	if err != nil {
		t.Fatalf("GetPairCustomQuestions returned error: %v", err)
	}
	if len(list) != 1 || list[0].QuestionText != "What is your favorite memory?" {
		t.Fatalf("unexpected list: %+v", list)
	}

	// Bob should NOT see Alice's question yet because they are not paired
	list, err = repo.GetPairCustomQuestions(ctx, 2002)
	if err != nil {
		t.Fatalf("GetPairCustomQuestions returned error: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("Bob saw Alice's question while unpaired: %+v", list)
	}

	// Pair them
	request, err := repo.CreatePairRequest(ctx, storage.PairRequest{
		RequesterID: 1001,
		InviteToken: "token-pair",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePairRequest returned error: %v", err)
	}
	if _, err := repo.AcceptPairRequest(ctx, request.InviteToken, 2002); err != nil {
		t.Fatalf("AcceptPairRequest returned error: %v", err)
	}

	// Now Bob should see Alice's question too
	list, err = repo.GetPairCustomQuestions(ctx, 2002)
	if err != nil {
		t.Fatalf("GetPairCustomQuestions returned error: %v", err)
	}
	if len(list) != 1 || list[0].QuestionText != "What is your favorite memory?" {
		t.Fatalf("Bob did not see Alice's question after pairing: %+v", list)
	}

	// Delete question
	if err := repo.DeleteCustomQuestion(ctx, qId, 1001); err != nil {
		t.Fatalf("DeleteCustomQuestion returned error: %v", err)
	}

	// Verify deleted
	list, err = repo.GetPairCustomQuestions(ctx, 1001)
	if err != nil {
		t.Fatalf("GetPairCustomQuestions returned error: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 questions after deletion, got %d", len(list))
	}
}

// TestCoupleCanRePairAfterBreakingUp guards against a regression of
// pairs_unique_users_idx being declared without "WHERE status = 'active'".
// EndActivePair keeps the ended row around (status = 'ended') rather than
// deleting it, and AcceptPairRequest is a plain INSERT with no ON CONFLICT
// handling, so an unscoped unique index on (user_a_id, user_b_id) makes the
// second pairing hit "UNIQUE constraint failed" forever - the same two
// people could never get back together. Against the fixed, status-scoped
// index this must succeed.
func TestCoupleCanRePairAfterBreakingUp(t *testing.T) {
	ctx := context.Background()
	db, err := storage.OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	defer db.Close()

	repo := storage.NewRepository(db)
	for _, user := range []storage.User{
		{TelegramID: 1001, Username: "alice", DisplayName: "Alice", Language: "en", ThemeBaseColor: "#d98c9f", SelectedStyleID: "default_warm"},
		{TelegramID: 2002, Username: "bob", DisplayName: "Bob", Language: "en", ThemeBaseColor: "#8da68f", SelectedStyleID: "default_warm"},
	} {
		if err := repo.UpsertUser(ctx, user); err != nil {
			t.Fatalf("UpsertUser(%d) returned error: %v", user.TelegramID, err)
		}
	}

	firstRequest, err := repo.CreatePairRequest(ctx, storage.PairRequest{
		RequesterID: 1001,
		InviteToken: "token-first-pairing",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePairRequest (first) returned error: %v", err)
	}
	if _, err := repo.AcceptPairRequest(ctx, firstRequest.InviteToken, 2002); err != nil {
		t.Fatalf("AcceptPairRequest (first) returned error: %v", err)
	}

	if _, err := repo.EndActivePair(ctx, 1001, time.Now().UTC()); err != nil {
		t.Fatalf("EndActivePair returned error: %v", err)
	}

	secondRequest, err := repo.CreatePairRequest(ctx, storage.PairRequest{
		RequesterID: 1001,
		InviteToken: "token-second-pairing",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePairRequest (second) returned error: %v", err)
	}
	if _, err := repo.AcceptPairRequest(ctx, secondRequest.InviteToken, 2002); err != nil {
		t.Fatalf("re-pairing the same two users after breaking up failed, want success: %v", err)
	}

	pair, err := repo.ActivePairForUser(ctx, 1001)
	if err != nil {
		t.Fatalf("ActivePairForUser returned error: %v", err)
	}
	if pair == nil || pair.Status != "active" {
		t.Fatalf("active pair after re-pairing = %#v, want an active pair", pair)
	}
}

// TestOpenSQLiteMigratesLegacyUnscopedPairsUniqueIndex proves that an
// existing on-disk database created before the WHERE status = 'active' fix
// actually gets the corrected index when OpenSQLite runs against it again -
// schemaSQL's CREATE UNIQUE INDEX IF NOT EXISTS alone cannot do this because
// the index already exists under that name with the old definition.
func TestOpenSQLiteMigratesLegacyUnscopedPairsUniqueIndex(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	legacyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db returned error: %v", err)
	}
	if _, err := legacyDB.ExecContext(ctx, `
		CREATE TABLE pairs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_a_id INTEGER NOT NULL,
			user_b_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			active_level INTEGER NOT NULL DEFAULT 1,
			highest_unlocked_level INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ended_at TEXT,
			CHECK(user_a_id < user_b_id)
		)
	`); err != nil {
		t.Fatalf("create legacy pairs table returned error: %v", err)
	}
	if _, err := legacyDB.ExecContext(ctx, `
		CREATE UNIQUE INDEX pairs_unique_users_idx
		ON pairs(user_a_id, user_b_id)
	`); err != nil {
		t.Fatalf("create legacy unscoped index returned error: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy db returned error: %v", err)
	}

	// Confirm the fixture actually reproduces the old, unscoped index before
	// letting OpenSQLite anywhere near it.
	verifyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen legacy db returned error: %v", err)
	}
	var legacySQL string
	if err := verifyDB.QueryRowContext(ctx, `
		SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'pairs_unique_users_idx'
	`).Scan(&legacySQL); err != nil {
		t.Fatalf("load legacy index sql returned error: %v", err)
	}
	if err := verifyDB.Close(); err != nil {
		t.Fatalf("close verify db returned error: %v", err)
	}
	if strings.Contains(legacySQL, "WHERE") {
		t.Fatalf("fixture index unexpectedly scoped: %s", legacySQL)
	}

	db, err := storage.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite against legacy db returned error: %v", err)
	}
	defer db.Close()

	var migratedSQL string
	if err := db.QueryRowContext(ctx, `
		SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'pairs_unique_users_idx'
	`).Scan(&migratedSQL); err != nil {
		t.Fatalf("load migrated index sql returned error: %v", err)
	}
	if !strings.Contains(migratedSQL, "WHERE status = 'active'") {
		t.Fatalf("migrated pairs_unique_users_idx sql = %q, want it scoped to WHERE status = 'active'", migratedSQL)
	}
}
