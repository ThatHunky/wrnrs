package game

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"wrnrs/internal/content"
	"wrnrs/internal/storage"
)

const (
	StartPendingInvite StartKind = "pending_invite"
	StartActiveSession StartKind = "active_session"
	StartRevealed      StartKind = "revealed_session"
)

var (
	ErrActivePairRequired = errors.New("active pair is required")
	ErrGameInviteExpired  = errors.New("game invite expired")
	ErrGameNotPending     = errors.New("game session is not pending acceptance")
	ErrGameNotActive      = errors.New("game session is not active")
	ErrGameNotRevealed    = errors.New("game session is not revealed")
	ErrGameForbidden      = errors.New("game session is not available to this user")
	ErrNoEligibleCards    = errors.New("no eligible cards")
)

type StartKind string

type ServiceOptions struct {
	Repo                 *storage.Repository
	Deck                 *content.Deck
	AnswerCipher         *storage.AnswerCipher
	Now                  func() time.Time
	InviteTTL            time.Duration
	LevelUnlockThreshold int
}

type Service struct {
	repo                 *storage.Repository
	deck                 *content.Deck
	answerCipher         *storage.AnswerCipher
	now                  func() time.Time
	inviteTTL            time.Duration
	levelUnlockThreshold int
}

type StartResult struct {
	Kind      StartKind
	Pair      storage.Pair
	Session   storage.GameSession
	PartnerID int64
	Card      content.Card
}

type StartedResult struct {
	Pair      storage.Pair
	Session   storage.GameSession
	PartnerID int64
	Card      content.Card
}

type SubmitResult struct {
	Pair     storage.Pair
	Session  storage.GameSession
	Revealed bool
	Answers  map[int64]RevealedAnswer
}

type RevealedAnswer struct {
	UserID     int64
	Completion CompletionType
	AnswerText string
}

func NewService(options ServiceOptions) *Service {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	ttl := options.InviteTTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	threshold := options.LevelUnlockThreshold
	if threshold <= 0 {
		threshold = 6
	}
	return &Service{
		repo:                 options.Repo,
		deck:                 options.Deck,
		answerCipher:         options.AnswerCipher,
		now:                  now,
		inviteTTL:            ttl,
		levelUnlockThreshold: threshold,
	}
}

func (s *Service) Start(ctx context.Context, userID int64) (StartResult, error) {
	pair, err := s.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		return StartResult{}, err
	}
	if pair == nil {
		return StartResult{}, ErrActivePairRequired
	}
	partnerID := partnerIDFor(*pair, userID)
	current, err := s.repo.CurrentGameSessionForPair(ctx, pair.ID)
	if err != nil {
		return StartResult{}, err
	}
	if current != nil {
		if current.Status == storage.GameSessionPendingAcceptance && s.inviteExpired(*current) {
			if err := s.repo.UpdateGameSessionStatus(ctx, current.ID, storage.GameSessionExpired, s.now()); err != nil {
				return StartResult{}, err
			}
			current = nil
		}
	}
	if current != nil {
		card, _ := s.cardByID(current.QuestionID)
		kind := StartPendingInvite
		if current.Status == storage.GameSessionActive {
			kind = StartActiveSession
		}
		if current.Status == storage.GameSessionRevealed {
			kind = StartRevealed
		}
		return StartResult{Kind: kind, Pair: *pair, Session: *current, PartnerID: partnerID, Card: card}, nil
	}
	session, card, err := s.createPendingSession(ctx, *pair, userID)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{Kind: StartPendingInvite, Pair: *pair, Session: session, PartnerID: partnerID, Card: card}, nil
}

func (s *Service) Accept(ctx context.Context, userID, sessionID int64) (StartedResult, error) {
	session, pair, partnerID, err := s.sessionForUser(ctx, userID, sessionID)
	if err != nil {
		return StartedResult{}, err
	}
	if session.Status != storage.GameSessionPendingAcceptance {
		if session.Status == storage.GameSessionActive || session.Status == storage.GameSessionRevealed {
			card, _ := s.cardByID(session.QuestionID)
			return StartedResult{Pair: pair, Session: session, PartnerID: partnerID, Card: card}, nil
		}
		return StartedResult{}, ErrGameNotPending
	}
	if session.InvitedByUserID == userID {
		return StartedResult{}, ErrGameForbidden
	}
	if s.inviteExpired(session) {
		if err := s.repo.UpdateGameSessionStatus(ctx, session.ID, storage.GameSessionExpired, s.now()); err != nil {
			return StartedResult{}, err
		}
		return StartedResult{}, ErrGameInviteExpired
	}
	session, err = s.repo.AcceptGameSession(ctx, session.ID, userID, s.now())
	if err != nil {
		return StartedResult{}, err
	}
	card, ok := s.cardByID(session.QuestionID)
	if !ok {
		return StartedResult{}, fmt.Errorf("card %s not found", session.QuestionID)
	}
	return StartedResult{Pair: pair, Session: session, PartnerID: partnerID, Card: card}, nil
}

func (s *Service) Decline(ctx context.Context, userID, sessionID int64) error {
	session, _, _, err := s.sessionForUser(ctx, userID, sessionID)
	if err != nil {
		return err
	}
	if session.Status != storage.GameSessionPendingAcceptance {
		return ErrGameNotPending
	}
	if session.InvitedByUserID == userID {
		return ErrGameForbidden
	}
	return s.repo.UpdateGameSessionStatus(ctx, session.ID, storage.GameSessionCancelled, s.now())
}

func (s *Service) Submit(ctx context.Context, userID, sessionID int64, completion CompletionType, answerText string) (SubmitResult, error) {
	session, pair, _, err := s.sessionForUser(ctx, userID, sessionID)
	if err != nil {
		return SubmitResult{}, err
	}
	if session.Status != storage.GameSessionActive {
		return SubmitResult{}, ErrGameNotActive
	}
	if err := validateCompletion(completion, answerText); err != nil {
		return SubmitResult{}, err
	}
	var encrypted []byte
	if completion == CompletionTyped {
		encrypted, err = s.answerCipher.Encrypt(answerText)
		if err != nil {
			return SubmitResult{}, err
		}
	}
	if err := s.repo.UpsertGameAnswer(ctx, storage.GameAnswer{
		SessionID:           session.ID,
		UserID:              userID,
		CompletionType:      string(completion),
		AnswerTextEncrypted: encrypted,
		CompletedAt:         s.now(),
	}); err != nil {
		return SubmitResult{}, err
	}
	answers, err := s.repo.GameAnswers(ctx, session.ID)
	if err != nil {
		return SubmitResult{}, err
	}
	if !bothPairUsersCompleted(pair, answers) {
		return SubmitResult{Pair: pair, Session: session, Revealed: false}, nil
	}
	revealed, err := s.repo.RevealGameSession(ctx, session.ID, s.now())
	if err != nil {
		return SubmitResult{}, err
	}
	if err := s.repo.RecordPairCardCompletion(ctx, session.PairID, session.QuestionID, session.Level, session.DeckCycle, s.now()); err != nil {
		return SubmitResult{}, err
	}
	if err := s.advanceLevelIfReady(ctx, pair, session.Level); err != nil {
		return SubmitResult{}, err
	}
	revealedAnswers, err := s.revealedAnswers(ctx, session.ID)
	if err != nil {
		return SubmitResult{}, err
	}
	return SubmitResult{Pair: pair, Session: revealed, Revealed: true, Answers: revealedAnswers}, nil
}

func (s *Service) Next(ctx context.Context, userID, sessionID int64) (StartResult, error) {
	session, pair, _, err := s.sessionForUser(ctx, userID, sessionID)
	if err != nil {
		return StartResult{}, err
	}
	if session.Status != storage.GameSessionRevealed {
		return StartResult{}, ErrGameNotRevealed
	}
	if err := s.repo.CompleteGameSession(ctx, session.ID, s.now()); err != nil {
		return StartResult{}, err
	}
	updatedPair, err := s.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		return StartResult{}, err
	}
	if updatedPair != nil {
		pair = *updatedPair
	}
	next, card, err := s.createPendingSession(ctx, pair, userID)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{Kind: StartPendingInvite, Pair: pair, Session: next, PartnerID: partnerIDFor(pair, userID), Card: card}, nil
}

func (s *Service) createPendingSession(ctx context.Context, pair storage.Pair, invitedBy int64) (storage.GameSession, content.Card, error) {
	card, cycle, err := s.selectNextCard(ctx, pair)
	if err != nil {
		return storage.GameSession{}, content.Card{}, err
	}
	expires := s.now().Add(s.inviteTTL)
	session, err := s.repo.CreateGameSession(ctx, storage.GameSession{
		PairID:          pair.ID,
		Level:           pair.ActiveLevel,
		QuestionID:      card.ID,
		Status:          storage.GameSessionPendingAcceptance,
		DeckCycle:       cycle,
		InvitedByUserID: invitedBy,
		InviteExpiresAt: sqlNullTime(expires),
	})
	if err != nil {
		return storage.GameSession{}, content.Card{}, err
	}
	return session, card, nil
}

func (s *Service) selectNextCard(ctx context.Context, pair storage.Pair) (content.Card, int, error) {
	if s.deck == nil {
		return content.Card{}, 0, ErrNoEligibleCards
	}
	userA18, userAMature, err := s.repo.UserMaturity(ctx, pair.UserAID)
	if err != nil {
		return content.Card{}, 0, err
	}
	userB18, userBMature, err := s.repo.UserMaturity(ctx, pair.UserBID)
	if err != nil {
		return content.Card{}, 0, err
	}
	cards := s.deck.EligibleCards(content.Eligibility{
		Level:                  pair.ActiveLevel,
		BothUsersMatureOptedIn: userA18 && userAMature && userB18 && userBMature,
	})
	if len(cards) == 0 {
		return content.Card{}, 0, ErrNoEligibleCards
	}
	cycle, err := s.repo.LatestDeckCycle(ctx, pair.ID, pair.ActiveLevel)
	if err != nil {
		return content.Card{}, 0, err
	}
	seen, err := s.repo.SeenCardIDs(ctx, pair.ID, pair.ActiveLevel, cycle)
	if err != nil {
		return content.Card{}, 0, err
	}
	card, cycle, _, err := content.SelectNextCard(content.SelectionInput{
		PairID: pair.ID,
		Level:  pair.ActiveLevel,
		Cycle:  cycle,
		Cards:  cards,
		Seen:   seen,
	})
	if err != nil {
		return content.Card{}, 0, err
	}
	return card, cycle, nil
}

func (s *Service) advanceLevelIfReady(ctx context.Context, pair storage.Pair, completedLevel int) error {
	if pair.ActiveLevel != completedLevel || s.nextLevel(completedLevel) == 0 {
		return nil
	}
	count, err := s.repo.PairCardCount(ctx, pair.ID, completedLevel)
	if err != nil {
		return err
	}
	if count < s.levelUnlockThreshold {
		return nil
	}
	return s.repo.UpdatePairLevel(ctx, pair.ID, completedLevel+1)
}

func (s *Service) nextLevel(level int) int {
	if s.deck == nil {
		return 0
	}
	for _, card := range s.deck.Cards {
		if card.Level == level+1 {
			return level + 1
		}
	}
	return 0
}

func (s *Service) sessionForUser(ctx context.Context, userID, sessionID int64) (storage.GameSession, storage.Pair, int64, error) {
	session, err := s.repo.GameSession(ctx, sessionID)
	if err != nil {
		return storage.GameSession{}, storage.Pair{}, 0, err
	}
	pair, err := s.repo.ActivePairForUser(ctx, userID)
	if err != nil {
		return storage.GameSession{}, storage.Pair{}, 0, err
	}
	if pair == nil || pair.ID != session.PairID || !pairContainsUser(*pair, userID) {
		return storage.GameSession{}, storage.Pair{}, 0, ErrGameForbidden
	}
	return session, *pair, partnerIDFor(*pair, userID), nil
}

func (s *Service) revealedAnswers(ctx context.Context, sessionID int64) (map[int64]RevealedAnswer, error) {
	stored, err := s.repo.GameAnswers(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	answers := make(map[int64]RevealedAnswer, len(stored))
	for _, answer := range stored {
		revealed := RevealedAnswer{
			UserID:     answer.UserID,
			Completion: CompletionType(answer.CompletionType),
		}
		if answer.CompletionType == string(CompletionTyped) && len(answer.AnswerTextEncrypted) > 0 {
			text, err := s.answerCipher.Decrypt(answer.AnswerTextEncrypted)
			if err != nil {
				return nil, err
			}
			revealed.AnswerText = text
		}
		answers[answer.UserID] = revealed
	}
	return answers, nil
}

func (s *Service) cardByID(questionID string) (content.Card, bool) {
	if s.deck == nil {
		return content.Card{}, false
	}
	for _, card := range s.deck.Cards {
		if card.ID == questionID {
			return card, true
		}
	}
	return content.Card{}, false
}

func (s *Service) inviteExpired(session storage.GameSession) bool {
	return session.InviteExpiresAt.Valid && !s.now().Before(session.InviteExpiresAt.Time)
}

func bothPairUsersCompleted(pair storage.Pair, answers []storage.GameAnswer) bool {
	completed := map[int64]bool{}
	for _, answer := range answers {
		completed[answer.UserID] = true
	}
	return completed[pair.UserAID] && completed[pair.UserBID]
}

func pairContainsUser(pair storage.Pair, userID int64) bool {
	return pair.UserAID == userID || pair.UserBID == userID
}

func partnerIDFor(pair storage.Pair, userID int64) int64 {
	if pair.UserAID == userID {
		return pair.UserBID
	}
	return pair.UserAID
}

func sqlNullTime(value time.Time) storageNullTime {
	return sql.NullTime{Time: value, Valid: !value.IsZero()}
}

type storageNullTime = sql.NullTime
