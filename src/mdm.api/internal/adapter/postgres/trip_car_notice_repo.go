package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type TripCarNoticeRepo struct{ pool *pgxpool.Pool }

func NewTripCarNoticeRepo(pool *pgxpool.Pool) *TripCarNoticeRepo {
	return &TripCarNoticeRepo{pool: pool}
}

func (r *TripCarNoticeRepo) Claim(ctx context.Context, n *domain.TripCarNotice, by string) (int64, bool, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO trip_car_notices
			(calendar_id, event_id, event_start, event_summary, recipient_email, recipient_account, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7)
		ON CONFLICT (calendar_id, event_id, event_start, recipient_email) DO UPDATE
			SET status = 'pending', error = '', event_summary = EXCLUDED.event_summary,
			    created_by = EXCLUDED.created_by, updated_at = now()
			WHERE trip_car_notices.status = 'failed'
			   OR (trip_car_notices.status = 'pending' AND trip_car_notices.updated_at < now() - interval '30 minutes')
		RETURNING id`,
		n.CalendarID, n.EventID, n.EventStart, n.EventSummary, n.RecipientEmail, n.RecipientAccount, by).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil // already sent, or being sent
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func (r *TripCarNoticeRepo) MarkSent(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trip_car_notices SET status='sent', error='', sent_at=now(), updated_at=now() WHERE id=$1`, id)
	return err
}

func (r *TripCarNoticeRepo) MarkFailed(ctx context.Context, id int64, reason string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trip_car_notices SET status='failed', error=$2, updated_at=now() WHERE id=$1`, id, reason)
	return err
}

func (r *TripCarNoticeRepo) ListFromDate(ctx context.Context, date string) ([]domain.TripCarNotice, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, calendar_id, event_id, event_start, event_summary, recipient_email, recipient_account, status, error, sent_at
		FROM trip_car_notices WHERE event_start >= $1 ORDER BY id`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TripCarNotice
	for rows.Next() {
		var n domain.TripCarNotice
		if err := rows.Scan(&n.ID, &n.CalendarID, &n.EventID, &n.EventStart, &n.EventSummary, &n.RecipientEmail, &n.RecipientAccount, &n.Status, &n.Error, &n.SentAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
