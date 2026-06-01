package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type AppRepo struct{ pool *pgxpool.Pool }

func NewAppRepo(pool *pgxpool.Pool) *AppRepo { return &AppRepo{pool: pool} }

func (r *AppRepo) ListManagedApps(ctx context.Context) ([]*domain.ManagedApp, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT ma.id, ma.name, ma.bundle_id, ma.app_type, ma.itunes_store_id, ma.manifest_url,
		        ma.purchased_qty, ma.notes, ma.created_at, ma.updated_at,
		        (SELECT COUNT(*) FROM device_apps da WHERE da.app_id = ma.id) as installed_count,
		        ma.icon_url, ma.supported_platforms
		 FROM managed_apps ma ORDER BY ma.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []*domain.ManagedApp
	for rows.Next() {
		a := &domain.ManagedApp{}
		if err := rows.Scan(&a.ID, &a.Name, &a.BundleID, &a.AppType, &a.ItunesStoreID, &a.ManifestURL,
			&a.PurchasedQty, &a.Notes, &a.CreatedAt, &a.UpdatedAt, &a.InstalledCount, &a.IconURL, &a.SupportedPlatforms); err != nil {
			continue
		}
		apps = append(apps, a)
	}
	return apps, nil
}

func (r *AppRepo) GetManagedApp(ctx context.Context, id string) (*domain.ManagedApp, error) {
	a := &domain.ManagedApp{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, bundle_id, app_type, itunes_store_id, manifest_url, purchased_qty, notes, icon_url, supported_platforms
		 FROM managed_apps WHERE id=$1`, id,
	).Scan(&a.ID, &a.Name, &a.BundleID, &a.AppType, &a.ItunesStoreID, &a.ManifestURL, &a.PurchasedQty, &a.Notes, &a.IconURL, &a.SupportedPlatforms)
	return a, err
}

func (r *AppRepo) CreateManagedApp(ctx context.Context, app *domain.ManagedApp) (string, error) {
	platforms := app.SupportedPlatforms
	if platforms == "" {
		platforms = "ios,ipados"
	}
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO managed_apps (name, bundle_id, app_type, itunes_store_id, manifest_url, purchased_qty, notes, icon_url, supported_platforms)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		app.Name, app.BundleID, app.AppType, app.ItunesStoreID, app.ManifestURL, app.PurchasedQty, app.Notes, app.IconURL, platforms,
	).Scan(&id)
	return id, err
}

func (r *AppRepo) UpdateManagedApp(ctx context.Context, id string, fields map[string]interface{}) error {
	allowed := []string{"name", "bundle_id", "app_type", "itunes_store_id", "manifest_url", "purchased_qty", "notes", "icon_url", "supported_platforms"}
	sets := []string{}
	args := []interface{}{}
	idx := 1
	for _, k := range allowed {
		if v, ok := fields[k]; ok {
			sets = append(sets, fmt.Sprintf("%s=$%d", k, idx))
			args = append(args, v)
			idx++
		}
	}
	if len(sets) == 0 {
		return nil
	}
	q := fmt.Sprintf("UPDATE managed_apps SET %s, updated_at=now() WHERE id=$%d", strings.Join(sets, ", "), idx)
	args = append(args, id)
	_, err := r.pool.Exec(ctx, q, args...)
	return err
}

func (r *AppRepo) DeleteManagedApp(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM managed_apps WHERE id=$1`, id)
	return err
}

// SetPurchasedQtyByItunesID updates the purchased_qty of any managed_app
// whose itunes_store_id matches. Returns the number of rows updated.
// Used by the VPP sync flow.
func (r *AppRepo) SetPurchasedQtyByItunesID(ctx context.Context, itunesStoreID string, qty int) (int, error) {
	if itunesStoreID == "" {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE managed_apps SET purchased_qty=$1, updated_at=now() WHERE itunes_store_id=$2`,
		qty, itunesStoreID)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *AppRepo) ListDeviceApps(ctx context.Context, deviceUdid string) ([]*domain.DeviceApp, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT da.id, da.device_udid, da.app_id, da.installed_at,
		        ma.name, ma.bundle_id, ma.app_type
		 FROM device_apps da JOIN managed_apps ma ON da.app_id = ma.id
		 WHERE da.device_udid = $1 ORDER BY da.installed_at DESC`, deviceUdid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*domain.DeviceApp
	for rows.Next() {
		d := &domain.DeviceApp{}
		if err := rows.Scan(&d.ID, &d.DeviceUdid, &d.AppID, &d.InstalledAt, &d.AppName, &d.BundleID, &d.AppType); err != nil {
			continue
		}
		items = append(items, d)
	}
	return items, nil
}

func (r *AppRepo) InstalledCount(ctx context.Context, appID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM device_apps WHERE app_id=$1`, appID).Scan(&count)
	return count, err
}

func (r *AppRepo) IsInstalledOn(ctx context.Context, deviceUdid, appID string) (bool, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM device_apps WHERE device_udid=$1 AND app_id=$2`, deviceUdid, appID).Scan(&count)
	return count > 0, err
}

func (r *AppRepo) CreatePendingCommand(ctx context.Context, cmd *domain.PendingAppCommand) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO pending_app_commands (command_uuid, action, device_udid, app_id) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		cmd.CommandUUID, cmd.Action, cmd.DeviceUdid, cmd.AppID)
	return err
}

func (r *AppRepo) DeletePendingCommand(ctx context.Context, commandUUID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM pending_app_commands WHERE command_uuid=$1`, commandUUID)
	return err
}

func (r *AppRepo) GetPendingCommand(ctx context.Context, commandUUID string) (*domain.PendingAppCommand, error) {
	cmd := &domain.PendingAppCommand{}
	err := r.pool.QueryRow(ctx,
		`SELECT action, device_udid, app_id FROM pending_app_commands WHERE command_uuid=$1`,
		commandUUID,
	).Scan(&cmd.Action, &cmd.DeviceUdid, &cmd.AppID)
	if err != nil {
		return nil, err
	}
	cmd.CommandUUID = commandUUID
	return cmd, nil
}

func (r *AppRepo) AddDeviceApp(ctx context.Context, deviceUdid, appID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO device_apps (device_udid, app_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		deviceUdid, appID)
	return err
}

func (r *AppRepo) RemoveDeviceApp(ctx context.Context, deviceUdid, appID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM device_apps WHERE device_udid=$1 AND app_id=$2`,
		deviceUdid, appID)
	return err
}

func (r *AppRepo) ListBundleMap(ctx context.Context) (map[string]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, bundle_id FROM managed_apps`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var id, bid string
		if err := rows.Scan(&id, &bid); err == nil && bid != "" {
			m[bid] = id
		}
	}
	return m, nil
}

func (r *AppRepo) SyncDeviceApp(ctx context.Context, deviceUdid, appID string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO device_apps (device_udid, app_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		deviceUdid, appID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *AppRepo) ListAppsNeedingIcons(ctx context.Context) ([]struct{ ID, BundleID, ItunesID string }, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, bundle_id, itunes_store_id FROM managed_apps WHERE icon_url = '' AND app_type = 'vpp' AND bundle_id != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var apps []struct{ ID, BundleID, ItunesID string }
	for rows.Next() {
		var a struct{ ID, BundleID, ItunesID string }
		rows.Scan(&a.ID, &a.BundleID, &a.ItunesID)
		apps = append(apps, a)
	}
	return apps, nil
}

func (r *AppRepo) UpdateIcon(ctx context.Context, id, iconURL, itunesID string) error {
	if itunesID != "" {
		_, err := r.pool.Exec(ctx,
			`UPDATE managed_apps SET icon_url=$1, itunes_store_id=$2, updated_at=now() WHERE id=$3`,
			iconURL, itunesID, id)
		return err
	}
	_, err := r.pool.Exec(ctx, `UPDATE managed_apps SET icon_url=$1, updated_at=now() WHERE id=$2`, iconURL, id)
	return err
}

// Ensure compile-time interface compliance.
var _ interface {
	ListManagedApps(ctx context.Context) ([]*domain.ManagedApp, error)
	GetManagedApp(ctx context.Context, id string) (*domain.ManagedApp, error)
	CreateManagedApp(ctx context.Context, app *domain.ManagedApp) (string, error)
	UpdateManagedApp(ctx context.Context, id string, fields map[string]interface{}) error
	DeleteManagedApp(ctx context.Context, id string) error
	ListDeviceApps(ctx context.Context, deviceUdid string) ([]*domain.DeviceApp, error)
	InstalledCount(ctx context.Context, appID string) (int, error)
	IsInstalledOn(ctx context.Context, deviceUdid, appID string) (bool, error)
	CreatePendingCommand(ctx context.Context, cmd *domain.PendingAppCommand) error
	DeletePendingCommand(ctx context.Context, commandUUID string) error
	GetPendingCommand(ctx context.Context, commandUUID string) (*domain.PendingAppCommand, error)
	AddDeviceApp(ctx context.Context, deviceUdid, appID string) error
	RemoveDeviceApp(ctx context.Context, deviceUdid, appID string) error
	ListBundleMap(ctx context.Context) (map[string]string, error)
	SyncDeviceApp(ctx context.Context, deviceUdid, appID string) (bool, error)
	ListAppsNeedingIcons(ctx context.Context) ([]struct{ ID, BundleID, ItunesID string }, error)
	UpdateIcon(ctx context.Context, id, iconURL, itunesID string) error
} = (*AppRepo)(nil)

// suppress unused import
var _ time.Time
