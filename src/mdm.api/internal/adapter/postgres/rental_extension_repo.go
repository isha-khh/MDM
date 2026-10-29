package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

var (
	ErrExtensionPendingExists = errors.New("已有一筆審核中的續借申請，請等審核完成")
	ErrExtensionNotPending    = errors.New("此續借申請已經處理過了")
)

// ErrExtensionConflict: the extended period would overlap another open rental
// or reservation of one of the same assets.
var ErrExtensionConflict = errors.New("續借的期間與這個資產的其他租借或預約重疊")

type RentalExtensionRepo struct{ pool *pgxpool.Pool }

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// extensionConflictSQL is true when moving a rental batch's end to the
// requested date would overlap another open rental of any of its assets. The
// checked window runs from where the batch's span ends now to its new end: the
// requested date at the same clock time as a timed booking's end, or the end of
// the requested day for a date-based one.
const extensionConflictSQL = `
	SELECT EXISTS (
	  SELECT 1
	  FROM (
	    SELECT me.asset_id, me.rental_number,
	           upper(rental_span(me)) AS lo,
	           CASE WHEN me.end_at IS NULL
	                THEN (($2::date + 1)::timestamp) AT TIME ZONE 'Asia/Taipei'
	                ELSE (($2::date)::timestamp + (me.end_at AT TIME ZONE 'Asia/Taipei')::time) AT TIME ZONE 'Asia/Taipei'
	           END AS hi
	    FROM rentals me
	    WHERE me.rental_number = $1 AND me.asset_id IS NOT NULL
	  ) w
	  JOIN rentals other ON other.asset_id = w.asset_id AND other.rental_number <> w.rental_number
	  WHERE other.status IN ('pending','approved','active')
	    AND rental_span(other) && tstzrange(w.lo, GREATEST(w.lo, w.hi), '[)')
	)`

// WouldConflict reports whether extending the rental to the requested return
// date would overlap another open rental or reservation of the same assets.
func (r *RentalExtensionRepo) WouldConflict(ctx context.Context, rentalNumber int, requested time.Time) (bool, error) {
	return extensionConflicts(ctx, r.pool, rentalNumber, requested)
}

func extensionConflicts(ctx context.Context, q rowQuerier, rentalNumber int, requested time.Time) (bool, error) {
	var conflict bool
	err := q.QueryRow(ctx, extensionConflictSQL, rentalNumber, requested).Scan(&conflict)
	return conflict, err
}

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
	// Serialise with anything else booking these assets, then re-check: another
	// reservation may have been made since the request was filed.
	if _, err := tx.Exec(ctx,
		`SELECT 1 FROM assets WHERE id IN (SELECT asset_id FROM rentals WHERE rental_number=$1 AND asset_id IS NOT NULL) FOR UPDATE`,
		cur.RentalNumber); err != nil {
		return nil, err
	}
	conflict, err := extensionConflicts(ctx, tx, cur.RentalNumber, cur.RequestedExpectedReturn)
	if err != nil {
		return nil, err
	}
	if conflict {
		return nil, ErrExtensionConflict
	}
	// A timed booking (end_at set) moves its end to the requested date at the
	// same clock time, so its span really grows; date-based ones follow expected_return.
	tag, err := tx.Exec(ctx,
		`UPDATE rentals SET expected_return=$1,
		        end_at = CASE WHEN end_at IS NULL THEN NULL
		                 ELSE (($1::date)::timestamp + (end_at AT TIME ZONE 'Asia/Taipei')::time) AT TIME ZONE 'Asia/Taipei' END,
		        updated_at=now()
		 WHERE rental_number=$2 AND status='active'`,
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
