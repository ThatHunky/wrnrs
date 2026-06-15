package game

import "time"

const SupportPromptInterval = 48 * time.Hour

type SupportPromptInput struct {
	Now            time.Time
	LastPromptedAt *time.Time
	UserAPremium   bool
	UserBPremium   bool
}

func ShouldShowSupportPrompt(input SupportPromptInput) bool {
	if input.UserAPremium || input.UserBPremium {
		return false
	}
	if input.LastPromptedAt == nil {
		return true
	}
	return !input.LastPromptedAt.Add(SupportPromptInterval).After(input.Now)
}
