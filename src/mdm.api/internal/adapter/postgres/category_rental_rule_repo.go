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
		`SELECT category_id, daily_tracking_required, updated_by, updated_at FROM category_rental_rules WHERE category_id=$1`,
		categoryID,
	).Scan(&rule.CategoryID, &rule.DailyTrackingRequired, &rule.UpdatedBy, &rule.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return rule, nil
}

func (r *CategoryRentalRuleRepo) Upsert(ctx context.Context, categoryID string, dailyTrackingRequired bool, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO category_rental_rules (category_id, daily_tracking_required, updated_by, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (category_id) DO UPDATE SET
		  daily_tracking_required = $2, updated_by = $3, updated_at = now()`,
		categoryID, dailyTrackingRequired, updatedBy)
	return err
}

// ResolveDailyTrackingRequired walks up categoryID's ancestor chain — using
// the caller-supplied flat category list so this doesn't re-query the
// categories table on every call — and returns the first ancestor's explicit
// rule (categoryID itself first, then its parent, grandparent, ...). A
// category tree with no rule set anywhere in the chain resolves to false
// (not subject to daily tracking), which is the correct default for the vast
// majority of asset categories that have nothing to do with vehicles.
func (r *CategoryRentalRuleRepo) ResolveDailyTrackingRequired(ctx context.Context, categories []*domain.Category, categoryID string) (bool, error) {
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
			return rule.DailyTrackingRequired, nil
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
