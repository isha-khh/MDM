package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

// IntegrationRepo stores the callers of the integration API (their hashed API
// keys and limits) and the resources they may reserve.
type IntegrationRepo struct{ pool *pgxpool.Pool }

func NewIntegrationRepo(pool *pgxpool.Pool) *IntegrationRepo { return &IntegrationRepo{pool: pool} }

const integrationClientCols = `id, name, key_prefix, enabled, ip_allowlist, auto_create_email_domains, created_at, rotated_at, last_used_at`

func scanIntegrationClient(row pgx.Row) (*domain.IntegrationClient, error) {
	c := &domain.IntegrationClient{}
	err := row.Scan(&c.ID, &c.Name, &c.KeyPrefix, &c.Enabled, &c.IPAllowlist, &c.AutoCreateEmailDomains,
		&c.CreatedAt, &c.RotatedAt, &c.LastUsedAt)
	return c, err
}

// ClientByKeyHash finds the client owning an API key (by the SHA-256 of the
// key). A nil client with a nil error means no such key.
func (r *IntegrationRepo) ClientByKeyHash(ctx context.Context, keyHash string) (*domain.IntegrationClient, error) {
	c, err := scanIntegrationClient(r.pool.QueryRow(ctx,
		`SELECT `+integrationClientCols+` FROM integration_clients WHERE key_hash=$1`, keyHash))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// TouchClient records that the client just made a request (at most once a minute).
func (r *IntegrationRepo) TouchClient(ctx context.Context, id string) {
	r.pool.Exec(ctx,
		`UPDATE integration_clients SET last_used_at=now()
		 WHERE id=$1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, id)
}

// PrimaryClient is the one client the settings screen manages (the dispatch
// system); nil when no key has been issued yet.
func (r *IntegrationRepo) PrimaryClient(ctx context.Context) (*domain.IntegrationClient, error) {
	c, err := scanIntegrationClient(r.pool.QueryRow(ctx,
		`SELECT `+integrationClientCols+` FROM integration_clients ORDER BY created_at LIMIT 1`))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// SetKey issues a key for the primary client: the first call creates it, later
// calls rotate it (the old key stops working at once; limits are kept).
func (r *IntegrationRepo) SetKey(ctx context.Context, name, keyHash, keyPrefix string) (*domain.IntegrationClient, error) {
	existing, err := r.PrimaryClient(ctx)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return scanIntegrationClient(r.pool.QueryRow(ctx,
			`INSERT INTO integration_clients (name, key_hash, key_prefix) VALUES ($1,$2,$3) RETURNING `+integrationClientCols,
			name, keyHash, keyPrefix))
	}
	return scanIntegrationClient(r.pool.QueryRow(ctx,
		`UPDATE integration_clients SET key_hash=$2, key_prefix=$3, rotated_at=now(), last_used_at=NULL
		 WHERE id=$1 RETURNING `+integrationClientCols, existing.ID, keyHash, keyPrefix))
}

// ClientUpdate changes the limits of a client; nil fields stay as they are.
type ClientUpdate struct {
	Enabled                *bool
	IPAllowlist            *[]string
	AutoCreateEmailDomains *[]string
}

func (r *IntegrationRepo) UpdateClient(ctx context.Context, id string, u ClientUpdate) (*domain.IntegrationClient, error) {
	return scanIntegrationClient(r.pool.QueryRow(ctx,
		`UPDATE integration_clients SET
		     enabled                   = COALESCE($2, enabled),
		     ip_allowlist              = COALESCE($3, ip_allowlist),
		     auto_create_email_domains = COALESCE($4, auto_create_email_domains)
		 WHERE id=$1 RETURNING `+integrationClientCols, id, u.Enabled, u.IPAllowlist, u.AutoCreateEmailDomains))
}

const integrationResourceCols = `ir.resource_id, ir.service_type, ir.asset_id, COALESCE(a.asset_number,''), COALESCE(a.name,''), ir.allowed_cities, ir.enabled`

func scanIntegrationResource(row pgx.Row) (*domain.IntegrationResource, error) {
	res := &domain.IntegrationResource{}
	err := row.Scan(&res.ResourceID, &res.ServiceType, &res.AssetID, &res.AssetNumber, &res.AssetName, &res.AllowedCities, &res.Enabled)
	return res, err
}

const integrationResourceFrom = ` FROM integration_resources ir JOIN assets a ON a.id = ir.asset_id`

// ListResources returns every mapped resource (enabled or not), by id.
func (r *IntegrationRepo) ListResources(ctx context.Context) ([]*domain.IntegrationResource, error) {
	return r.queryResources(ctx, ` ORDER BY ir.resource_id`)
}

// EnabledResources returns the enabled resources of a service type, by id.
func (r *IntegrationRepo) EnabledResources(ctx context.Context, serviceType string) ([]*domain.IntegrationResource, error) {
	return r.queryResources(ctx, ` WHERE ir.enabled AND ir.service_type=$1 ORDER BY ir.resource_id`, serviceType)
}

// Resource looks up an enabled resource by id; nil when there is none.
func (r *IntegrationRepo) Resource(ctx context.Context, resourceID string) (*domain.IntegrationResource, error) {
	res, err := scanIntegrationResource(r.pool.QueryRow(ctx,
		`SELECT `+integrationResourceCols+integrationResourceFrom+` WHERE ir.enabled AND ir.resource_id=$1`, resourceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return res, err
}

func (r *IntegrationRepo) queryResources(ctx context.Context, tail string, args ...interface{}) ([]*domain.IntegrationResource, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+integrationResourceCols+integrationResourceFrom+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.IntegrationResource
	for rows.Next() {
		res, err := scanIntegrationResource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, rows.Err()
}

// ErrAssetNotFound is returned when a resource is mapped to an unknown asset.
var ErrAssetNotFound = errors.New("asset not found")

// UpsertResource creates or updates a resource mapping.
func (r *IntegrationRepo) UpsertResource(ctx context.Context, res *domain.IntegrationResource) error {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assets WHERE id=$1)`, res.AssetID).Scan(&exists); err != nil {
		return fmt.Errorf("check asset: %w", err)
	}
	if !exists {
		return ErrAssetNotFound
	}
	if res.AllowedCities == nil {
		res.AllowedCities = []string{}
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO integration_resources (resource_id, service_type, asset_id, allowed_cities, enabled)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (resource_id) DO UPDATE SET service_type=$2, asset_id=$3, allowed_cities=$4, enabled=$5`,
		res.ResourceID, res.ServiceType, res.AssetID, res.AllowedCities, res.Enabled)
	return err
}

// DeleteResource removes a mapping (the rentals made through it stay).
func (r *IntegrationRepo) DeleteResource(ctx context.Context, resourceID string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM integration_resources WHERE resource_id=$1`, resourceID)
	return tag.RowsAffected() > 0, err
}
