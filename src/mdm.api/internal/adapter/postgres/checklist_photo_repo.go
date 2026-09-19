package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type ChecklistPhotoRepo struct{ pool *pgxpool.Pool }

func NewChecklistPhotoRepo(pool *pgxpool.Pool) *ChecklistPhotoRepo {
	return &ChecklistPhotoRepo{pool: pool}
}

func (r *ChecklistPhotoRepo) Create(ctx context.Context, photo *domain.ChecklistPhoto) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO checklist_photos (rental_number, item_key, content, content_type, size, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		photo.RentalNumber, photo.ItemKey, photo.Content, photo.ContentType, photo.Size, photo.UploadedBy,
	).Scan(&id)
	return id, err
}

func (r *ChecklistPhotoRepo) Get(ctx context.Context, id string) (*domain.ChecklistPhoto, error) {
	photo := &domain.ChecklistPhoto{ID: id}
	err := r.pool.QueryRow(ctx,
		`SELECT rental_number, item_key, content, content_type, size, uploaded_by, created_at FROM checklist_photos WHERE id=$1`,
		id,
	).Scan(&photo.RentalNumber, &photo.ItemKey, &photo.Content, &photo.ContentType, &photo.Size, &photo.UploadedBy, &photo.CreatedAt)
	if err != nil {
		return nil, err
	}
	return photo, nil
}
