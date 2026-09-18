package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type MailSettingsRepo struct{ pool *pgxpool.Pool }

func NewMailSettingsRepo(pool *pgxpool.Pool) *MailSettingsRepo {
	return &MailSettingsRepo{pool: pool}
}

func (r *MailSettingsRepo) Get(ctx context.Context) (*domain.MailSettings, error) {
	m := &domain.MailSettings{}
	err := r.pool.QueryRow(ctx, `
		SELECT smtp_enabled, smtp_host, smtp_port, smtp_username, smtp_password,
		       smtp_from, smtp_from_name, smtp_tls, smtp_ca_cert, smtp_insecure_skip_verify,
		       incoming_enabled, incoming_protocol, incoming_host, incoming_port,
		       incoming_username, incoming_password, incoming_tls, incoming_mailbox,
		       updated_at, updated_by
		FROM mail_settings WHERE id='default'`).Scan(
		&m.SMTPEnabled, &m.SMTPHost, &m.SMTPPort, &m.SMTPUsername, &m.SMTPPassword,
		&m.SMTPFrom, &m.SMTPFromName, &m.SMTPTLS, &m.SMTPCACert, &m.SMTPInsecureSkipVerify,
		&m.IncomingEnabled, &m.IncomingProtocol, &m.IncomingHost, &m.IncomingPort,
		&m.IncomingUsername, &m.IncomingPassword, &m.IncomingTLS, &m.IncomingMailbox,
		&m.UpdatedAt, &m.UpdatedBy,
	)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (r *MailSettingsRepo) Upsert(ctx context.Context, m *domain.MailSettings, updatedBy string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE mail_settings SET
		  smtp_enabled=$1, smtp_host=$2, smtp_port=$3, smtp_username=$4, smtp_password=$5,
		  smtp_from=$6, smtp_from_name=$7, smtp_tls=$8, smtp_ca_cert=$9, smtp_insecure_skip_verify=$10,
		  incoming_enabled=$11, incoming_protocol=$12, incoming_host=$13, incoming_port=$14,
		  incoming_username=$15, incoming_password=$16, incoming_tls=$17, incoming_mailbox=$18,
		  updated_at=now(), updated_by=$19
		WHERE id='default'`,
		m.SMTPEnabled, m.SMTPHost, m.SMTPPort, m.SMTPUsername, m.SMTPPassword,
		m.SMTPFrom, m.SMTPFromName, m.SMTPTLS, m.SMTPCACert, m.SMTPInsecureSkipVerify,
		m.IncomingEnabled, m.IncomingProtocol, m.IncomingHost, m.IncomingPort,
		m.IncomingUsername, m.IncomingPassword, m.IncomingTLS, m.IncomingMailbox,
		updatedBy,
	)
	return err
}
