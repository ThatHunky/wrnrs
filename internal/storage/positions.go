package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PositionMarkKind is one of the independent flags a pair can set on a position.
type PositionMarkKind string

const (
	MarkTried     PositionMarkKind = "tried_at"
	MarkFavorited PositionMarkKind = "favorited_at"
	MarkHidden    PositionMarkKind = "hidden_at"
)

// PositionMark is the shared state of one position for one pair. Marks belong
// to the pair, not to a partner: both see the same flags.
type PositionMark struct {
	PositionID  string
	TriedAt     sql.NullTime
	FavoritedAt sql.NullTime
	HiddenAt    sql.NullTime
	MarkedBy    sql.NullInt64
}

func (k PositionMarkKind) valid() bool {
	switch k {
	case MarkTried, MarkFavorited, MarkHidden:
		return true
	default:
		return false
	}
}

// TogglePositionMark flips one flag and returns its new state. kind is
// interpolated into the SQL as a column name, so it MUST be validated by
// valid() before it ever reaches a query — that check is a security
// boundary, not a formality, and must run before any use of kind in SQL.
func (r *Repository) TogglePositionMark(ctx context.Context, pairID int64, positionID string, kind PositionMarkKind, markedBy int64, now time.Time) (bool, error) {
	if !kind.valid() {
		return false, fmt.Errorf("unknown position mark kind %q", kind)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin toggle position mark: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var current sql.NullTime
	query := fmt.Sprintf(`SELECT %s FROM pair_position_marks WHERE pair_id = ? AND position_id = ?`, string(kind))
	err = tx.QueryRowContext(ctx, query, pairID, positionID).Scan(&current)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("read position mark: %w", err)
	}

	setOn := !current.Valid
	var value any
	if setOn {
		value = now
	}

	upsert := fmt.Sprintf(`
		INSERT INTO pair_position_marks (pair_id, position_id, %s, marked_by, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(pair_id, position_id) DO UPDATE SET
			%s = excluded.%s,
			marked_by = excluded.marked_by,
			updated_at = excluded.updated_at
	`, string(kind), string(kind), string(kind))
	if _, err := tx.ExecContext(ctx, upsert, pairID, positionID, value, markedBy, now); err != nil {
		return false, fmt.Errorf("write position mark: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit position mark: %w", err)
	}
	return setOn, nil
}

// PairPositionMarks loads every mark set for a pair, keyed by position id.
func (r *Repository) PairPositionMarks(ctx context.Context, pairID int64) (map[string]PositionMark, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT position_id, tried_at, favorited_at, hidden_at, marked_by
		FROM pair_position_marks
		WHERE pair_id = ?
	`, pairID)
	if err != nil {
		return nil, fmt.Errorf("load position marks: %w", err)
	}
	defer rows.Close()

	marks := map[string]PositionMark{}
	for rows.Next() {
		var mark PositionMark
		if err := rows.Scan(&mark.PositionID, &mark.TriedAt, &mark.FavoritedAt, &mark.HiddenAt, &mark.MarkedBy); err != nil {
			return nil, fmt.Errorf("scan position mark: %w", err)
		}
		marks[mark.PositionID] = mark
	}
	return marks, rows.Err()
}
