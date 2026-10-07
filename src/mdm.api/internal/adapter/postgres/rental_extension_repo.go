package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

var (
	ErrExtensionPendingExists = errors.New("已有一筆審核中的續借申請，請等審核完成")
	ErrExtensionNotPending    = errors.New("此續借申請已經處理過了")
)

type RentalExtensionRepo struct{ pool *pgxpool.Pool }

func NewRentalExtensionRepo(pool *pgxpool.Pool) *RentalExtensionRepo {
	return &RentalExtensionRepo{pool: pool}
}

const extensionSelect = `
	SELECT e.id, e.rental_number, e.requested_by, COALESCE(NULLIF(u.display_name,''), u.username, ''),
	       e.previous_expected_return, e.requested_expected_return, e.reason, e.status,
	       e.decided_by, e.decided_at, e.decision_note, e.created_at
	FROM rental_extensions e
	LEFT JOIN users u ON u.id = e.requested_by`

func scanExtension(row pgx.Row) (*domain.RentalExtension, error) {
	e := &domain.RentalExtension{}
	var requestedBy *string
	if err := row.Scan(&e.ID, &e.RentalNumber, &requestedBy, &e.RequestedByName,
		&e.PreviousExpectedReturn, &e.RequestedExpectedReturn, &e.Reason, &e.Status,
		&e.DecidedBy, &e.DecidedAt, &e.DecisionNote, &e.CreatedAt); err != nil {
		return nil, err
	}
	if requestedBy != nil {
		e.RequestedBy = *requestedBy
	}
	return e, nil
}

// Create records a new pending request. The partial unique index allows only
// one pending request per rental_number, surfaced here as
// ErrExtensionPendingExists.
func (r *RentalExtensionRepo) Create(ctx context.Context, e *domain.RentalExtension) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO rental_extensions (rental_number, requested_by, previous_expected_return, requested_expected_return, reason)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		e.RentalNumber, e.RequestedBy, e.PreviousExpectedReturn, e.RequestedExpectedReturn, e.Reason,
	).Scan(&id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return "", ErrExtensionPendingExists
	}
	return id, err
}

func (r *RentalExtensionRepo) Get(ctx context.Context, id string) (*domain.RentalExtension, error) {
	return scanExtension(r.pool.QueryRow(ctx, extensionSelect+` WHERE e.id=$1`, id))
}

// ListByNumber returns a rental batch's extension history, newest first.
func (r *RentalExtensionRepo) ListByNumber(ctx context.Context, rentalNumber int) ([]*domain.RentalExtension, error) {
	rows, err := r.pool.Query(ctx, extensionSelect+` WHERE e.rental_number=$1 ORDER BY e.created_at DESC`, rentalNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.RentalExtension
	for rows.Next() {
		e, err := scanExtension(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListPending returns every pending request keyed by rental_number, so the
// rental list can show them without a query per row.
func (r *RentalExtensionRepo) ListPending(ctx context.Context) (map[int]*domain.RentalExtension, error) {
	rows, err := r.pool.Query(ctx, extensionSelect+` WHERE e.status='pending'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]*domain.RentalExtension{}
	for rows.Next() {
		e, err := scanExtension(rows)
		if err != nil {
			return nil, err
		}
		out[e.RentalNumber] = e
	}
	return out, rows.Err()
}

// Approve marks the request approved and applies the new expected_return to
// every row of the rental batch, in one transaction. If the rental is no
// longer active (returned in the meantime) nothing changes and
// domain.ErrExtensionNotActive is returned.
func (r *RentalExtensionRepo) Approve(ctx context.Context, id, decidedBy, note string) (*domain.RentalExtension, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	cur, err := scanExtension(tx.QueryRow(ctx, extensionSelect+` WHERE e.id=$1 FOR UPDATE OF e`, id))
	if err != nil {
		return nil, err
	}
	if cur.Status != "pending" {
		return nil, ErrExtensionNotPending
	}
	tag, err := tx.Exec(ctx,
		`UPDATE rentals SET expected_return=$1, updated_at=now() WHERE rental_number=$2 AND status='active'`,
		cur.RequestedExpectedReturn, cur.RentalNumber)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, domain.ErrExtensionNotActive
	}
	if _, err := tx.Exec(ctx,
		`UPDATE rental_extensions SET status='approved', decided_by=$1, decided_at=now(), decision_note=$2 WHERE id=$3`,
		decidedBy, note, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *RentalExtensionRepo) Reject(ctx context.Context, id, decidedBy, note string) (*domain.RentalExtension, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE rental_extensions SET status='rejected', decided_by=$1, decided_at=now(), decision_note=$2
		 WHERE id=$3 AND status='pending'`, decidedBy, note, id)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrExtensionNotPending
	}
	return r.Get(ctx, id)
}
