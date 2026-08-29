package game

import "time"

const SupportPromptInterval = 48 * time.Hour

type SupportPromptInput struct {
	Now            time.Time
	LastPromptedAt *time.Time
	UserAPremium   bool
	UserBPremium   bool
	Interval       time.Duration
}

func ShouldShowSupportPrompt(input SupportPromptInput) bool {
	if input.UserAPremium || input.UserBPremium {
		return false
	}
	if input.LastPromptedAt == nil {
		return true
	}
	interval := input.Interval
	if interval <= 0 {
		interval = SupportPromptInterval
	}
	return !input.LastPromptedAt.Add(interval).After(input.Now)
}
