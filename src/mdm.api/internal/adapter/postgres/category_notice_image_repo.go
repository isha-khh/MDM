package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type CategoryNoticeImageRepo struct{ pool *pgxpool.Pool }

func NewCategoryNoticeImageRepo(pool *pgxpool.Pool) *CategoryNoticeImageRepo {
	return &CategoryNoticeImageRepo{pool: pool}
}

func (r *CategoryNoticeImageRepo) Create(ctx context.Context, img *domain.CategoryNoticeImage) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO category_notice_images (category_id, content, content_type, size, uploaded_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		img.CategoryID, img.Content, img.ContentType, img.Size, img.UploadedBy,
	).Scan(&id)
	return id, err
}

func (r *CategoryNoticeImageRepo) Get(ctx context.Context, id string) (*domain.CategoryNoticeImage, error) {
	img := &domain.CategoryNoticeImage{ID: id}
	err := r.pool.QueryRow(ctx,
		`SELECT category_id, content, content_type, size, uploaded_by, created_at FROM category_notice_images WHERE id=$1`,
		id,
	).Scan(&img.CategoryID, &img.Content, &img.ContentType, &img.Size, &img.UploadedBy, &img.CreatedAt)
	if err != nil {
		return nil, err
	}
	return img, nil
}
