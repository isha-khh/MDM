package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type TripNoticeScheduleRepo struct{ pool *pgxpool.Pool }

func NewTripNoticeScheduleRepo(pool *pgxpool.Pool) *TripNoticeScheduleRepo {
	return &TripNoticeScheduleRepo{pool: pool}
}

func (r *TripNoticeScheduleRepo) Get(ctx context.Context) (*domain.TripNoticeSchedule, error) {
	s := &domain.TripNoticeSchedule{}
	err := r.pool.QueryRow(ctx, `
		SELECT enabled, send_hour, last_run_date, last_run_at, last_run_summary, updated_at
		FROM trip_notice_schedule WHERE id='default'`).
		Scan(&s.Enabled, &s.SendHour, &s.LastRunDate, &s.LastRunAt, &s.LastRunSummary, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *TripNoticeScheduleRepo) Save(ctx context.Context, enabled bool, sendHour int, by string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trip_notice_schedule SET enabled=$1, send_hour=$2, updated_at=now(), updated_by=$3
		WHERE id='default'`, enabled, sendHour, by)
	return err
}

func (r *TripNoticeScheduleRepo) ClaimRun(ctx context.Context, today string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE trip_notice_schedule SET last_run_date=$1, last_run_at=now()
		WHERE id='default' AND enabled AND last_run_date <> $1`, today)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *TripNoticeScheduleRepo) ReleaseRun(ctx context.Context, today string) error {
	_, err := r.pool.Exec(ctx, `UPDATE trip_notice_schedule SET last_run_date='' WHERE id='default' AND last_run_date=$1`, today)
	return err
}

func (r *TripNoticeScheduleRepo) RecordRun(ctx context.Context, summary string) error {
	_, err := r.pool.Exec(ctx, `UPDATE trip_notice_schedule SET last_run_summary=$1 WHERE id='default'`, summary)
	return err
}
