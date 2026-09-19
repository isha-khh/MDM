package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type CategoryNoticeRepo struct{ pool *pgxpool.Pool }

func NewCategoryNoticeRepo(pool *pgxpool.Pool) *CategoryNoticeRepo {
	return &CategoryNoticeRepo{pool: pool}
}

func hashNoticeContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Get returns the category's own explicit notice, or pgx.ErrNoRows if this
// category has never had one set (callers doing inheritance resolution must
// keep walking up to the parent in that case; see ResolveForCategory).
func (r *CategoryNoticeRepo) Get(ctx context.Context, categoryID string) (*domain.CategoryNotice, error) {
	notice := &domain.CategoryNotice{}
	err := r.pool.QueryRow(ctx,
		`SELECT category_id, content, updated_by, updated_at FROM category_notices WHERE category_id=$1`,
		categoryID,
	).Scan(&notice.CategoryID, &notice.Content, &notice.UpdatedBy, &notice.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return notice, nil
}

func (r *CategoryNoticeRepo) Upsert(ctx context.Context, categoryID string, content string, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO category_notices (category_id, content, updated_by, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (category_id) DO UPDATE SET
		  content = $2, updated_by = $3, updated_at = now()`,
		categoryID, content, updatedBy)
	return err
}

func (r *CategoryNoticeRepo) Delete(ctx context.Context, categoryID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM category_notices WHERE category_id=$1`, categoryID)
	return err
}

// ResolveForCategory walks up categoryID's ancestor chain (using the
// caller-supplied flat category list) and returns the first ancestor's
// explicit notice, or ok=false if nothing in the chain has one.
func (r *CategoryNoticeRepo) ResolveForCategory(ctx context.Context, categories []*domain.Category, categoryID string) (notice *domain.CategoryNotice, ok bool, err error) {
	byID := make(map[string]*domain.Category, len(categories))
	for _, cat := range categories {
		byID[cat.ID] = cat
	}

	cur := categoryID
	visited := map[string]bool{}
	for cur != "" && !visited[cur] {
		visited[cur] = true

		n, err := r.Get(ctx, cur)
		if err == nil {
			return n, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, false, err
		}

		cat, ok := byID[cur]
		if !ok || cat.ParentID == nil {
			break
		}
		cur = *cat.ParentID
	}
	return nil, false, nil
}

// PendingAck is one notice from ResolvePending that the given user still
// needs to read and agree to before their rental request can be submitted.
type PendingAck struct {
	CategoryID string `json:"category_id"`
	Content    string `json:"content"`
}

// ResolvePending resolves each of categoryIDs to its (possibly inherited)
// notice, deduplicates by the resolved ancestor category (so a batch whose
// assets share an inherited notice only shows it once), and drops any whose
// content the user has already acknowledged unchanged. An empty result means
// nothing needs to be shown.
func (r *CategoryNoticeRepo) ResolvePending(ctx context.Context, categories []*domain.Category, categoryIDs []string, userID string) ([]PendingAck, error) {
	seen := map[string]bool{}
	var pending []PendingAck
	for _, cid := range categoryIDs {
		if cid == "" {
			continue
		}
		notice, ok, err := r.ResolveForCategory(ctx, categories, cid)
		if err != nil {
			return nil, err
		}
		if !ok || seen[notice.CategoryID] {
			continue
		}
		seen[notice.CategoryID] = true

		acked, err := r.isAcked(ctx, userID, notice.CategoryID, notice.Content)
		if err != nil {
			return nil, err
		}
		if acked {
			continue
		}
		pending = append(pending, PendingAck{CategoryID: notice.CategoryID, Content: notice.Content})
	}
	if pending == nil {
		pending = []PendingAck{}
	}
	return pending, nil
}

func (r *CategoryNoticeRepo) isAcked(ctx context.Context, userID, categoryID, content string) (bool, error) {
	var hash string
	err := r.pool.QueryRow(ctx,
		`SELECT content_hash FROM category_notice_acks WHERE user_id=$1 AND category_id=$2`,
		userID, categoryID,
	).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return hash == hashNoticeContent(content), nil
}

// Ack records that userID has agreed to categoryID's notice as it currently
// reads. Re-resolves the content itself (rather than trusting a
// client-supplied copy) so the stored hash always matches what's actually in
// category_notices.
func (r *CategoryNoticeRepo) Ack(ctx context.Context, categoryID, userID string) error {
	notice, err := r.Get(ctx, categoryID)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO category_notice_acks (user_id, category_id, content_hash, acked_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (user_id, category_id) DO UPDATE SET
		  content_hash = $3, acked_at = now()`,
		userID, categoryID, hashNoticeContent(notice.Content))
	return err
}
