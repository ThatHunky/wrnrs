package storage

import (
	"context"
	"fmt"
	"time"
)

// WishAnswer is one person's private stance on one item.
type WishAnswer string

const (
	AnswerWant    WishAnswer = "want"
	AnswerCurious WishAnswer = "curious"
	AnswerNo      WishAnswer = "no"
)

// WishItemKind separates the two id spaces a wish answer can point at: the
// wishes catalog and the positions catalog. Without it, wish "001" and
// position "001" would collide.
type WishItemKind string

const (
	WishKindWish     WishItemKind = "wish"
	WishKindPosition WishItemKind = "position"
)

// WishMatch is one item both partners are open to. Strong marks the case
// where both said "want" rather than one merely being curious.
//
// This is deliberately the ONLY shape in which one partner's stance reaches
// the other: a match says "you are both open to this" and never reveals an
// individual answer. There is no repository method that returns another
// user's answers, and adding one would break the module's core promise.
type WishMatch struct {
	ItemKind WishItemKind
	ItemID   string
	Strong   bool
}

func (a WishAnswer) valid() bool {
	switch a {
	case AnswerWant, AnswerCurious, AnswerNo:
		return true
	default:
		return false
	}
}

func (k WishItemKind) valid() bool {
	switch k {
	case WishKindWish, WishKindPosition:
		return true
	default:
		return false
	}
}

// SetWishAnswer records or replaces one person's answer for one item.
func (r *Repository) SetWishAnswer(ctx context.Context, userID int64, kind WishItemKind, itemID string, answer WishAnswer, now time.Time) error {
	if !kind.valid() {
		return fmt.Errorf("unknown wish item kind %q", kind)
	}
	if !answer.valid() {
		return fmt.Errorf("unknown wish answer %q", answer)
	}
	if itemID == "" {
		return fmt.Errorf("wish item id is required")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO wish_answers (user_id, item_kind, item_id, answer, answered_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, item_kind, item_id) DO UPDATE SET
			answer = excluded.answer,
			answered_at = excluded.answered_at
	`, userID, string(kind), itemID, string(answer), now)
	if err != nil {
		return fmt.Errorf("write wish answer: %w", err)
	}
	return nil
}

// UserWishAnswers returns the caller's own answers keyed "kind:itemID".
func (r *Repository) UserWishAnswers(ctx context.Context, userID int64) (map[string]WishAnswer, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT item_kind, item_id, answer FROM wish_answers WHERE user_id = ?
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("load wish answers: %w", err)
	}
	defer rows.Close()

	out := map[string]WishAnswer{}
	for rows.Next() {
		var kind, id, answer string
		if err := rows.Scan(&kind, &id, &answer); err != nil {
			return nil, fmt.Errorf("scan wish answer: %w", err)
		}
		out[kind+":"+id] = WishAnswer(answer)
	}
	return out, rows.Err()
}

// PairWishMatches computes the pair's matches inside the database so that no
// individual answer ever leaves this package. A match requires both partners
// to have answered, neither to have said "no", and at least one to have said
// "want".
func (r *Repository) PairWishMatches(ctx context.Context, pairID int64) ([]WishMatch, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT a.item_kind, a.item_id,
		       (a.answer = 'want' AND b.answer = 'want') AS strong
		FROM pairs p
		JOIN wish_answers a ON a.user_id = p.user_a_id
		JOIN wish_answers b ON b.user_id = p.user_b_id
		                   AND b.item_kind = a.item_kind
		                   AND b.item_id = a.item_id
		WHERE p.id = ?
		  AND a.answer <> 'no'
		  AND b.answer <> 'no'
		  AND (a.answer = 'want' OR b.answer = 'want')
		ORDER BY a.item_kind, a.item_id
	`, pairID)
	if err != nil {
		return nil, fmt.Errorf("load wish matches: %w", err)
	}
	defer rows.Close()

	var out []WishMatch
	for rows.Next() {
		var m WishMatch
		var kind string
		if err := rows.Scan(&kind, &m.ItemID, &m.Strong); err != nil {
			return nil, fmt.Errorf("scan wish match: %w", err)
		}
		m.ItemKind = WishItemKind(kind)
		out = append(out, m)
	}
	return out, rows.Err()
}

// PartnerHasAnyWishAnswer reports only whether the other partner has started.
// It is deliberately a boolean and not a count: a count would let a partner
// subtract matches from progress and infer the caller's "no" answers.
func (r *Repository) PartnerHasAnyWishAnswer(ctx context.Context, pairID, userID int64) (bool, error) {
	var found int
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pairs p
			JOIN wish_answers w
			  ON w.user_id = CASE WHEN p.user_a_id = ? THEN p.user_b_id ELSE p.user_a_id END
			WHERE p.id = ?
		)
	`, userID, pairID).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("check partner wish activity: %w", err)
	}
	return found == 1, nil
}
