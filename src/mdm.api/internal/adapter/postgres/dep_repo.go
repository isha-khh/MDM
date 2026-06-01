package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type DEPAssignmentRepo struct{ pool *pgxpool.Pool }

func NewDEPAssignmentRepo(pool *pgxpool.Pool) *DEPAssignmentRepo {
	return &DEPAssignmentRepo{pool: pool}
}

// Get returns the assignment for a serial, or (nil, nil) if not found.
func (r *DEPAssignmentRepo) Get(ctx context.Context, serial string) (*domain.DEPAssignment, error) {
	a := &domain.DEPAssignment{}
	err := r.pool.QueryRow(ctx,
		`SELECT serial_number, product_family, template_family, profile_uuid, applied_at, last_error
		 FROM dep_assignments WHERE serial_number=$1`, serial).
		Scan(&a.SerialNumber, &a.ProductFamily, &a.TemplateFamily, &a.ProfileUUID, &a.AppliedAt, &a.LastError)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// Upsert inserts or replaces an assignment row.
func (r *DEPAssignmentRepo) Upsert(ctx context.Context, a *domain.DEPAssignment) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO dep_assignments (serial_number, product_family, template_family, profile_uuid, applied_at, last_error)
		 VALUES ($1, $2, $3, $4, now(), $5)
		 ON CONFLICT (serial_number) DO UPDATE SET
		   product_family  = EXCLUDED.product_family,
		   template_family = EXCLUDED.template_family,
		   profile_uuid    = EXCLUDED.profile_uuid,
		   applied_at      = now(),
		   last_error      = EXCLUDED.last_error`,
		a.SerialNumber, a.ProductFamily, a.TemplateFamily, a.ProfileUUID, a.LastError)
	return err
}

// ListSerials returns a set of all serials we've already assigned. Used by the
// scheduler to compute the diff against ABM in a single pass.
func (r *DEPAssignmentRepo) ListSerials(ctx context.Context) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT serial_number FROM dep_assignments`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out[s] = true
	}
	return out, nil
}
