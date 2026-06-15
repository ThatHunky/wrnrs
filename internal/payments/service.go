package payments

import (
	"fmt"
	"strings"

	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

const PremiumSKU = "premium_lifetime"

type SKU struct {
	ID          string
	Title       string
	Description string
	Stars       int64
	UnlockType  string
	UnlockID    string
}

type Catalog struct {
	items map[string]SKU
}

func DefaultCatalog() Catalog {
	return Catalog{items: map[string]SKU{
		PremiumSKU: {
			ID:          PremiumSKU,
			Title:       "WRNRS Premium",
			Description: "Lifetime premium access, all current cosmetics, and no support prompts.",
			Stars:       250,
			UnlockType:  storage.EntitlementPremiumAccess,
			UnlockID:    "premium_access",
		},
	}}
}

func (c Catalog) SKU(id string) (SKU, bool) {
	item, ok := c.items[id]
	return item, ok
}

func InvoicePayload(userID int64, sku string) string {
	return fmt.Sprintf("sku=%s;user=%d", sku, userID)
}

func ParseInvoicePayload(payload string) (string, int64, error) {
	var sku string
	var userID int64
	for _, part := range strings.Split(payload, ";") {
		if strings.HasPrefix(part, "sku=") {
			sku = strings.TrimPrefix(part, "sku=")
		}
		if strings.HasPrefix(part, "user=") {
			if _, err := fmt.Sscanf(strings.TrimPrefix(part, "user="), "%d", &userID); err != nil {
				return "", 0, err
			}
		}
	}
	if sku == "" || userID == 0 {
		return "", 0, fmt.Errorf("invalid invoice payload")
	}
	return sku, userID, nil
}

func PayKeyboard(language string) telegram.InlineKeyboardMarkup {
	text := "Pay"
	if language == "uk" {
		text = "Оплатити"
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: text, Pay: true}},
	}}
}
