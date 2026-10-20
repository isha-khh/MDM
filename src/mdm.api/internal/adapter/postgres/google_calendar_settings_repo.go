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
		SELECT client_id, client_secret_enc, refresh_token_enc, redirect_uris, connected_account, updated_at, updated_by
		FROM google_calendar_settings WHERE id='default'`).
		Scan(&s.ClientID, &s.ClientSecretEnc, &s.RefreshTokenEnc, &s.RedirectURIs, &s.ConnectedAccount, &s.UpdatedAt, &s.UpdatedBy)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *GoogleCalendarSettingsRepo) SaveClient(ctx context.Context, clientID, clientSecretEnc string, redirectURIs []string, updatedBy string) error {
	if redirectURIs == nil {
		redirectURIs = []string{} // pgx sends a nil slice as NULL, which the column rejects
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE google_calendar_settings
		SET client_id=$1, client_secret_enc=$2, redirect_uris=$3,
		    refresh_token_enc='', connected_account='', updated_at=now(), updated_by=$4
		WHERE id='default'`, clientID, clientSecretEnc, redirectURIs, updatedBy)
	return err
}

func (r *GoogleCalendarSettingsRepo) SaveRefreshToken(ctx context.Context, refreshTokenEnc, connectedAccount, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE google_calendar_settings
		SET refresh_token_enc=$1, connected_account=$2, updated_at=now(), updated_by=$3
		WHERE id='default'`, refreshTokenEnc, connectedAccount, updatedBy)
	return err
}

func (r *GoogleCalendarSettingsRepo) ClearRefreshToken(ctx context.Context, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE google_calendar_settings
		SET refresh_token_enc='', connected_account='', updated_at=now(), updated_by=$1
		WHERE id='default'`, updatedBy)
	return err
}

func (r *GoogleCalendarSettingsRepo) Clear(ctx context.Context, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE google_calendar_settings
		SET client_id='', client_secret_enc='', refresh_token_enc='', redirect_uris='{}',
		    connected_account='', updated_at=now(), updated_by=$1
		WHERE id='default'`, updatedBy)
	return err
}
