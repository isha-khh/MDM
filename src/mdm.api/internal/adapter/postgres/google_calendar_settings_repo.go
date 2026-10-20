package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type GoogleCalendarSettingsRepo struct{ pool *pgxpool.Pool }

func NewGoogleCalendarSettingsRepo(pool *pgxpool.Pool) *GoogleCalendarSettingsRepo {
	return &GoogleCalendarSettingsRepo{pool: pool}
}

func (r *GoogleCalendarSettingsRepo) Get(ctx context.Context) (*domain.GoogleCalendarSettings, error) {
	s := &domain.GoogleCalendarSettings{}
	err := r.pool.QueryRow(ctx, `
		SELECT client_id, client_secret_enc, refresh_token_enc, updated_at, updated_by
		FROM google_calendar_settings WHERE id='default'`).
		Scan(&s.ClientID, &s.ClientSecretEnc, &s.RefreshTokenEnc, &s.UpdatedAt, &s.UpdatedBy)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *GoogleCalendarSettingsRepo) SaveClient(ctx context.Context, clientID, clientSecretEnc, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE google_calendar_settings
		SET client_id=$1, client_secret_enc=$2, refresh_token_enc='', updated_at=now(), updated_by=$3
		WHERE id='default'`, clientID, clientSecretEnc, updatedBy)
	return err
}

func (r *GoogleCalendarSettingsRepo) Clear(ctx context.Context, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE google_calendar_settings
		SET client_id='', client_secret_enc='', refresh_token_enc='', updated_at=now(), updated_by=$1
		WHERE id='default'`, updatedBy)
	return err
}
