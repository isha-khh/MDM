package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type DirectorySettingsRepo struct{ pool *pgxpool.Pool }

func NewDirectorySettingsRepo(pool *pgxpool.Pool) *DirectorySettingsRepo {
	return &DirectorySettingsRepo{pool: pool}
}

func (r *DirectorySettingsRepo) Get(ctx context.Context) (*domain.DirectorySettings, error) {
	s := &domain.DirectorySettings{}
	err := r.pool.QueryRow(ctx,
		`SELECT base_url, api_key_enc, updated_at, updated_by FROM directory_settings WHERE id='default'`).
		Scan(&s.BaseURL, &s.APIKeyEnc, &s.UpdatedAt, &s.UpdatedBy)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *DirectorySettingsRepo) Save(ctx context.Context, baseURL, apiKeyEnc, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE directory_settings SET base_url=$1, api_key_enc=$2, updated_at=now(), updated_by=$3
		WHERE id='default'`, baseURL, apiKeyEnc, updatedBy)
	return err
}

func (r *DirectorySettingsRepo) Clear(ctx context.Context, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE directory_settings SET base_url='', api_key_enc='', updated_at=now(), updated_by=$1
		WHERE id='default'`, updatedBy)
	return err
}
