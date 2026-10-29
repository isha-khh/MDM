package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type CategoryRentalRuleRepo struct{ pool *pgxpool.Pool }

func NewCategoryRentalRuleRepo(pool *pgxpool.Pool) *CategoryRentalRuleRepo {
	return &CategoryRentalRuleRepo{pool: pool}
}

// Get returns the category's own explicit rule row, or pgx.ErrNoRows if this
// category has never had one set (which is NOT the same as "rule is false" —
// callers doing inheritance resolution must keep walking up to the parent in
// that case; see ResolveDailyTrackingRequired).
func (r *CategoryRentalRuleRepo) Get(ctx context.Context, categoryID string) (*domain.CategoryRentalRule, error) {
	rule := &domain.CategoryRentalRule{}
	err := r.pool.QueryRow(ctx,
		`SELECT category_id, daily_tracking_required, half_day_enabled, reservation_enabled, updated_by, updated_at FROM category_rental_rules WHERE category_id=$1`,
		categoryID,
	).Scan(&rule.CategoryID, &rule.DailyTrackingRequired, &rule.HalfDayEnabled, &rule.ReservationEnabled, &rule.UpdatedBy, &rule.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return rule, nil
}

// Upsert updates only the fields that are non-nil, leaving the other one as
// it was (or at its default when the row is new).
func (r *CategoryRentalRuleRepo) Upsert(ctx context.Context, categoryID string, dailyTrackingRequired, halfDayEnabled, reservationEnabled *bool, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO category_rental_rules (category_id, daily_tracking_required, half_day_enabled, reservation_enabled, updated_by, updated_at)
		VALUES ($1, COALESCE($2, FALSE), COALESCE($3, FALSE), COALESCE($4, FALSE), $5, now())
		ON CONFLICT (category_id) DO UPDATE SET
		  daily_tracking_required = COALESCE($2, category_rental_rules.daily_tracking_required),
		  half_day_enabled = COALESCE($3, category_rental_rules.half_day_enabled),
		  reservation_enabled = COALESCE($4, category_rental_rules.reservation_enabled),
		  updated_by = $5, updated_at = now()`,
		categoryID, dailyTrackingRequired, halfDayEnabled, reservationEnabled, updatedBy)
	return err
}

// ResolveDailyTrackingRequired walks up categoryID's ancestor chain and
// returns the first explicit rule's value (see resolve). No rule anywhere in
// the chain resolves to false.
func (r *CategoryRentalRuleRepo) ResolveDailyTrackingRequired(ctx context.Context, categories []*domain.Category, categoryID string) (bool, error) {
	return r.resolve(ctx, categories, categoryID, func(rule *domain.CategoryRentalRule) bool { return rule.DailyTrackingRequired })
}

// ResolveHalfDayEnabled is the half-day counterpart of
// ResolveDailyTrackingRequired, using the same inheritance walk.
func (r *CategoryRentalRuleRepo) ResolveHalfDayEnabled(ctx context.Context, categories []*domain.Category, categoryID string) (bool, error) {
	return r.resolve(ctx, categories, categoryID, func(rule *domain.CategoryRentalRule) bool { return rule.HalfDayEnabled })
}

// ResolveReservationEnabled is the reservation counterpart, with the same
// inheritance walk.
func (r *CategoryRentalRuleRepo) ResolveReservationEnabled(ctx context.Context, categories []*domain.Category, categoryID string) (bool, error) {
	return r.resolve(ctx, categories, categoryID, func(rule *domain.CategoryRentalRule) bool { return rule.ReservationEnabled })
}

// resolve walks up categoryID's ancestor chain — using the caller-supplied
// flat category list so this doesn't re-query the categories table on every
// call — and returns pick() of the first ancestor's explicit rule
// (categoryID itself first, then its parent, ...). A tree with no rule
// anywhere in the chain resolves to false, the right default for the vast
// majority of categories.
func (r *CategoryRentalRuleRepo) resolve(ctx context.Context, categories []*domain.Category, categoryID string, pick func(*domain.CategoryRentalRule) bool) (bool, error) {
	byID := make(map[string]*domain.Category, len(categories))
	for _, cat := range categories {
		byID[cat.ID] = cat
	}

	cur := categoryID
	visited := map[string]bool{}
	for cur != "" && !visited[cur] {
		visited[cur] = true

		rule, err := r.Get(ctx, cur)
		if err == nil {
			return pick(rule), nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}

		cat, ok := byID[cur]
		if !ok || cat.ParentID == nil {
			break
		}
		cur = *cat.ParentID
	}
	return false, nil
}
