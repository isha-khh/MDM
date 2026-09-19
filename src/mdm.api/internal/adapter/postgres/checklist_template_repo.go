package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type ChecklistTemplateRepo struct{ pool *pgxpool.Pool }

func NewChecklistTemplateRepo(pool *pgxpool.Pool) *ChecklistTemplateRepo {
	return &ChecklistTemplateRepo{pool: pool}
}

// Get returns categoryID's own explicit template, or pgx.ErrNoRows if this
// category has no row of its own. Pass nil for the global default template
// (category_id IS NULL).
func (r *ChecklistTemplateRepo) Get(ctx context.Context, categoryID *string) (*domain.ChecklistTemplate, error) {
	tpl := &domain.ChecklistTemplate{CategoryID: categoryID}
	var itemsJSON []byte
	var row pgx.Row
	if categoryID == nil {
		row = r.pool.QueryRow(ctx, `SELECT id, items, updated_by, updated_at FROM checklist_templates WHERE category_id IS NULL`)
	} else {
		row = r.pool.QueryRow(ctx, `SELECT id, items, updated_by, updated_at FROM checklist_templates WHERE category_id=$1`, *categoryID)
	}
	if err := row.Scan(&tpl.ID, &itemsJSON, &tpl.UpdatedBy, &tpl.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(itemsJSON, &tpl.Items); err != nil {
		return nil, err
	}
	return tpl, nil
}

// Upsert replaces the item list for categoryID (nil = the global default
// template).
func (r *ChecklistTemplateRepo) Upsert(ctx context.Context, categoryID *string, items []domain.ChecklistItem, updatedBy string) error {
	if items == nil {
		items = []domain.ChecklistItem{}
	}
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		return err
	}
	if categoryID == nil {
		// category_id is nullable and UNIQUE, but Postgres never treats two
		// NULLs as conflicting under a unique constraint — ON CONFLICT
		// (category_id) simply can't target this row. UPDATE it directly;
		// the migration always seeds exactly one NULL row, but insert one
		// as a fallback if it's somehow missing.
		tag, err := r.pool.Exec(ctx,
			`UPDATE checklist_templates SET items=$1, updated_by=$2, updated_at=now() WHERE category_id IS NULL`,
			itemsJSON, updatedBy)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			_, err = r.pool.Exec(ctx,
				`INSERT INTO checklist_templates (category_id, items, updated_by) VALUES (NULL, $1, $2)`,
				itemsJSON, updatedBy)
		}
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO checklist_templates (category_id, items, updated_by, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (category_id) DO UPDATE SET items=$2, updated_by=$3, updated_at=now()`,
		*categoryID, itemsJSON, updatedBy)
	return err
}

// ResolveForCategory walks up categoryID's ancestor chain (using the
// caller-supplied flat category list) and returns the first ancestor's
// explicit template, falling back to the global default (category_id IS
// NULL) if nothing in the chain has one.
func (r *ChecklistTemplateRepo) ResolveForCategory(ctx context.Context, categories []*domain.Category, categoryID string) ([]domain.ChecklistItem, error) {
	byID := make(map[string]*domain.Category, len(categories))
	for _, cat := range categories {
		byID[cat.ID] = cat
	}

	cur := categoryID
	visited := map[string]bool{}
	for cur != "" && !visited[cur] {
		visited[cur] = true

		tpl, err := r.Get(ctx, &cur)
		if err == nil {
			return tpl.Items, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}

		cat, ok := byID[cur]
		if !ok || cat.ParentID == nil {
			break
		}
		cur = *cat.ParentID
	}

	tpl, err := r.Get(ctx, nil)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return []domain.ChecklistItem{}, nil
		}
		return nil, err
	}
	return tpl.Items, nil
}

// ResolveMerged resolves each of categoryIDs independently and unions the
// results by item key (first occurrence wins), matching the "merge union"
// policy for a batch whose assets span multiple categories.
func (r *ChecklistTemplateRepo) ResolveMerged(ctx context.Context, categories []*domain.Category, categoryIDs []string) ([]domain.ChecklistItem, error) {
	if len(categoryIDs) == 0 {
		return r.ResolveForCategory(ctx, categories, "")
	}
	seen := map[string]bool{}
	var merged []domain.ChecklistItem
	for _, cid := range categoryIDs {
		if cid == "" {
			continue
		}
		items, err := r.ResolveForCategory(ctx, categories, cid)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if seen[it.Key] {
				continue
			}
			seen[it.Key] = true
			merged = append(merged, it)
		}
	}
	if merged == nil {
		// No asset had a resolvable category at all — fall back to the
		// global default so the return dialog never renders empty.
		return r.ResolveForCategory(ctx, categories, "")
	}
	return merged, nil
}
