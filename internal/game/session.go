package game

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type CompletionType string

const (
	CompletionTyped    CompletionType = "typed"
	CompletionSkip     CompletionType = "skip"
	CompletionInPerson CompletionType = "in_person"
)

type ParticipantState struct {
	UserID      int64
	Completion  CompletionType
	AnswerText  string
	CompletedAt *time.Time
}

type Completion struct {
	QuestionID  string         `json:"question_id"`
	Type        CompletionType `json:"type"`
	AnswerText  string         `json:"answer_text,omitempty"`
	CompletedAt time.Time      `json:"completed_at"`
}

type Session struct {
	ID           int64
	PairID       int64
	QuestionID   string
	participants map[int64]ParticipantState
	revealedAt   *time.Time
}

type Reveal struct {
	SessionID  int64
	PairID     int64
	QuestionID string
	RevealedAt time.Time
	Answers    map[int64]ParticipantState
}

func NewSession(id, pairID int64, questionID string, userIDs []int64) *Session {
	participants := make(map[int64]ParticipantState, len(userIDs))
	for _, userID := range userIDs {
		participants[userID] = ParticipantState{UserID: userID}
	}
	return &Session{
		ID:           id,
		PairID:       pairID,
		QuestionID:   questionID,
		participants: participants,
	}
}

func (s *Session) Submit(userID int64, completion CompletionType, answerText string, now time.Time) (bool, error) {
	if s.revealedAt != nil {
		return false, errors.New("session already revealed")
	}
	state, ok := s.participants[userID]
	if !ok {
		return false, fmt.Errorf("user %d is not part of session", userID)
	}
	if err := validateCompletion(completion, answerText); err != nil {
		return false, err
	}

	state.Completion = completion
	if completion == CompletionTyped {
		state.AnswerText = strings.TrimSpace(answerText)
	} else {
		state.AnswerText = ""
	}
	state.CompletedAt = &now
	s.participants[userID] = state

	return s.ReadyToReveal(), nil
}

func (s *Session) ReadyToReveal() bool {
	if len(s.participants) == 0 {
		return false
	}
	for _, state := range s.participants {
		if state.CompletedAt == nil {
			return false
		}
	}
	return true
}

func (s *Session) Reveal(now time.Time) (Reveal, error) {
	if s.revealedAt != nil {
		return Reveal{}, errors.New("session already revealed")
	}
	if !s.ReadyToReveal() {
		return Reveal{}, errors.New("session is not ready to reveal")
	}
	s.revealedAt = &now
	answers := make(map[int64]ParticipantState, len(s.participants))
	for userID, state := range s.participants {
		answers[userID] = state
	}
	return Reveal{
		SessionID:  s.ID,
		PairID:     s.PairID,
		QuestionID: s.QuestionID,
		RevealedAt: now,
		Answers:    answers,
	}, nil
}

func validateCompletion(completion CompletionType, answerText string) error {
	switch completion {
	case CompletionTyped:
		if strings.TrimSpace(answerText) == "" {
			return errors.New("typed answer cannot be empty")
		}
	case CompletionSkip, CompletionInPerson:
		return nil
	default:
		return fmt.Errorf("unknown completion type %q", completion)
	}
	return nil
}
