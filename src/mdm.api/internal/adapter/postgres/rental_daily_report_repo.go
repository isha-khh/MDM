package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type RentalDailyReportRepo struct{ pool *pgxpool.Pool }

func NewRentalDailyReportRepo(pool *pgxpool.Pool) *RentalDailyReportRepo {
	return &RentalDailyReportRepo{pool: pool}
}

func (r *RentalDailyReportRepo) Create(ctx context.Context, report *domain.RentalDailyReport) (string, error) {
	checklistJSON, err := json.Marshal(report.Checklist)
	if err != nil {
		return "", err
	}
	var id string
	err = r.pool.QueryRow(ctx, `
		INSERT INTO rental_daily_reports (rental_number, report_date, checklist, backfill_reason, reported_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		report.RentalNumber, report.ReportDate, checklistJSON, report.BackfillReason, report.ReportedBy,
	).Scan(&id)
	return id, err
}

// ExistsForDate reports whether rentalNumber already has a report (real-time
// or backfilled) for date — the UNIQUE(rental_number, report_date) constraint
// enforces this at the DB level too, but checking first lets the controller
// return a clean 409 instead of a raw constraint-violation error.
func (r *RentalDailyReportRepo) ExistsForDate(ctx context.Context, rentalNumber int, date time.Time) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM rental_daily_reports WHERE rental_number=$1 AND report_date=$2)`,
		rentalNumber, date,
	).Scan(&exists)
	return exists, err
}

func (r *RentalDailyReportRepo) ListByNumber(ctx context.Context, rentalNumber int) ([]*domain.RentalDailyReport, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, rental_number, report_date, checklist, backfill_reason, reported_by, reported_at
		FROM rental_daily_reports WHERE rental_number=$1 ORDER BY report_date`,
		rentalNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []*domain.RentalDailyReport
	for rows.Next() {
		rep := &domain.RentalDailyReport{}
		var checklistJSON []byte
		if err := rows.Scan(&rep.ID, &rep.RentalNumber, &rep.ReportDate, &checklistJSON, &rep.BackfillReason, &rep.ReportedBy, &rep.ReportedAt); err != nil {
			continue
		}
		if len(checklistJSON) > 0 {
			json.Unmarshal(checklistJSON, &rep.Checklist)
		}
		reports = append(reports, rep)
	}
	return reports, nil
}
