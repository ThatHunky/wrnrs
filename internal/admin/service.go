package admin

import (
	"fmt"
	"strconv"
	"strings"

	"wrnrs/internal/storage"
	"wrnrs/internal/telegram"
)

type Service struct {
	adminIDs map[int64]bool
}

func NewService(adminIDs []int64) *Service {
	ids := make(map[int64]bool, len(adminIDs))
	for _, id := range adminIDs {
		ids[id] = true
	}
	return &Service{adminIDs: ids}
}

func (s *Service) IsAdmin(userID int64) bool {
	return s.adminIDs[userID]
}

func (s *Service) Menu() telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{
		{{Text: "Grant premium", CallbackData: "admin:grant:premium_access:premium_access"}},
		{{Text: "Revoke premium", CallbackData: "admin:revoke:premium_access:premium_access"}},
		{{Text: "Grant style", CallbackData: "admin:grant:style:default_warm"}},
		{{Text: "Revoke style", CallbackData: "admin:revoke:style:default_warm"}},
	}}
}

type ParsedAction struct {
	Action     string
	UnlockType string
	UnlockID   string
	TargetRef  string
	TargetID   int64
}

func ParseCommand(text string) (ParsedAction, bool, error) {
	fields := strings.Fields(text)
	if len(fields) != 3 && len(fields) != 4 {
		return ParsedAction{}, false, nil
	}
	switch fields[0] {
	case "/grant", "/revoke":
	default:
		return ParsedAction{}, false, nil
	}
	targetRef := fields[1]
	targetID, _ := strconv.ParseInt(strings.TrimPrefix(targetRef, "@"), 10, 64)
	unlockType := normalizeUnlockType(fields[2])
	unlockID := ""
	if len(fields) == 4 {
		unlockID = fields[3]
	} else if unlockType == storage.EntitlementPremiumAccess {
		unlockID = storage.EntitlementPremiumAccess
	} else {
		return ParsedAction{}, true, fmt.Errorf("unlock id is required for %s", unlockType)
	}
	return ParsedAction{
		Action:     strings.TrimPrefix(fields[0], "/"),
		TargetID:   targetID,
		TargetRef:  targetRef,
		UnlockType: unlockType,
		UnlockID:   unlockID,
	}, true, nil
}

func EntitlementFromAction(action ParsedAction) storage.Entitlement {
	return storage.Entitlement{
		UserID:   action.TargetID,
		Type:     action.UnlockType,
		UnlockID: action.UnlockID,
		Source:   "admin_grant",
	}
}

func normalizeUnlockType(value string) string {
	if value == "premium" {
		return storage.EntitlementPremiumAccess
	}
	return value
}
