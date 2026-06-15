package storage_test

import (
	"context"
	"database/sql"
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
