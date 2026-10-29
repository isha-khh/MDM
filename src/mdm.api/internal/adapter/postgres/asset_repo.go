package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

type AssetRepo struct{ pool *pgxpool.Pool }

func NewAssetRepo(pool *pgxpool.Pool) *AssetRepo { return &AssetRepo{pool: pool} }

// IsCustodianOfAll returns true if the user is the custodian of ALL given device UDIDs.
func (r *AssetRepo) IsCustodianOfAll(ctx context.Context, userID string, udids []string) (bool, error) {
	if len(udids) == 0 {
		return false, nil
	}
	placeholders := make([]string, len(udids))
	args := []interface{}{userID}
	for i, udid := range udids {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args = append(args, udid)
	}
	q := fmt.Sprintf(
		`SELECT COUNT(DISTINCT device_udid) FROM assets WHERE custodian_id = $1 AND device_udid IN (%s)`,
		strings.Join(placeholders, ","),
	)
	var count int
	if err := r.pool.QueryRow(ctx, q, args...).Scan(&count); err != nil {
		return false, err
	}
	return count == len(udids), nil
}

const assetSelectColumns = `a.id, a.device_udid, a.asset_number, a.name, a.spec, a.quantity, a.unit,
    a.acquired_date, a.unit_price, a.purpose, a.assigned_date,
    a.custodian_id, a.custodian_name, a.location, a.asset_category, a.notes,
    a.created_at, a.updated_at,
    COALESCE(d.device_name,'') as device_name, COALESCE(d.serial_number,'') as device_serial,
    a.category_id, COALESCE(c.name,'') as category_name,
    COALESCE(a.asset_status,'available') as asset_status,
    a.disposed_at, a.disposed_by, COALESCE(a.dispose_reason,'') as dispose_reason,
    COALESCE(a.transferred_to,'') as transferred_to, a.transferred_at,
    a.current_holder_id, COALESCE(a.current_holder_name,'') as current_holder_name, a.current_holder_since,
    a.is_rentable`

func scanAsset(row interface {
	Scan(dest ...interface{}) error
}) (*domain.Asset, error) {
	a := &domain.Asset{}
	var acquiredDate, assignedDate *time.Time
	err := row.Scan(
		&a.ID, &a.DeviceUdid, &a.AssetNumber, &a.Name, &a.Spec, &a.Quantity, &a.Unit,
		&acquiredDate, &a.UnitPrice, &a.Purpose, &assignedDate,
		&a.CustodianID, &a.CustodianName, &a.Location, &a.AssetCategory, &a.Notes,
		&a.CreatedAt, &a.UpdatedAt, &a.DeviceName, &a.DeviceSerial,
		&a.CategoryID, &a.CategoryName, &a.AssetStatus,
		&a.DisposedAt, &a.DisposedBy, &a.DisposeReason,
		&a.TransferredTo, &a.TransferredAt,
		&a.CurrentHolderID, &a.CurrentHolderName, &a.CurrentHolderSince,
		&a.IsRentable,
	)
	if err != nil {
		return nil, err
	}
	a.AcquiredDate = acquiredDate
	a.AssignedDate = assignedDate
	return a, nil
}

func (r *AssetRepo) List(ctx context.Context, deviceUdid string) ([]*domain.Asset, error) {
	q := `SELECT ` + assetSelectColumns + `
	      FROM assets a LEFT JOIN devices d ON a.device_udid = d.udid
	      LEFT JOIN categories c ON a.category_id = c.id`
	args := []interface{}{}
	if deviceUdid != "" {
		q += ` WHERE a.device_udid = $1`
		args = append(args, deviceUdid)
	}
	q += ` ORDER BY a.created_at DESC`

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assets []*domain.Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			continue
		}
		assets = append(assets, a)
	}
	return assets, nil
}

func (r *AssetRepo) GetByID(ctx context.Context, id string) (*domain.Asset, error) {
	q := `SELECT ` + assetSelectColumns + `
	      FROM assets a LEFT JOIN devices d ON a.device_udid = d.udid
	      LEFT JOIN categories c ON a.category_id = c.id
	      WHERE a.id = $1`
	return scanAsset(r.pool.QueryRow(ctx, q, id))
}

func (r *AssetRepo) GetByDeviceUdid(ctx context.Context, udid string) (*domain.Asset, error) {
	q := `SELECT ` + assetSelectColumns + `
	      FROM assets a LEFT JOIN devices d ON a.device_udid = d.udid
	      LEFT JOIN categories c ON a.category_id = c.id
	      WHERE a.device_udid = $1 LIMIT 1`
	return scanAsset(r.pool.QueryRow(ctx, q, udid))
}

func (r *AssetRepo) Create(ctx context.Context, asset *domain.Asset) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO assets (device_udid, asset_number, name, spec, quantity, unit, acquired_date, unit_price, purpose, assigned_date, custodian_id, custodian_name, location, asset_category, notes, category_id, is_rentable)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`,
		asset.DeviceUdid, asset.AssetNumber, asset.Name, asset.Spec, asset.Quantity, asset.Unit,
		asset.AcquiredDate, asset.UnitPrice, asset.Purpose, asset.AssignedDate,
		asset.CustodianID, asset.CustodianName, asset.Location, asset.AssetCategory, asset.Notes, asset.CategoryID, asset.IsRentable,
	).Scan(&id)
	return id, err
}

// Update allows editing asset metadata, but NOT custodian_id / custodian_name / assigned_date.
// Those must go through SetCustodian (which appends an asset_custody_logs entry).
// current_holder_* is also excluded — it's written by rental activate/return only.
func (r *AssetRepo) Update(ctx context.Context, id string, fields map[string]interface{}) error {
	allowed := []string{"device_udid", "asset_number", "name", "spec", "quantity", "unit",
		"acquired_date", "unit_price", "purpose",
		"location", "asset_category", "notes", "category_id", "asset_status", "is_rentable",
		"dispose_reason", "transferred_to"}
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
	q := fmt.Sprintf("UPDATE assets SET %s, updated_at=now() WHERE id=$%d", strings.Join(sets, ", "), idx)
	args = append(args, id)
	_, err := r.pool.Exec(ctx, q, args...)
	return err
}

func (r *AssetRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM assets WHERE id=$1`, id)
	return err
}

// UpdateLastReturnLocation records where a rental's stage-2 verify checklist
// said an asset was when it came back (Phase 2c of 租借 2.1) — a "location"
// type checklist answer, distinct from and never overwriting the
// manually-maintained assets.location (storage location) field.
func (r *AssetRepo) UpdateLastReturnLocation(ctx context.Context, assetID string, locationJSON []byte) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE assets SET last_return_location=$1, last_return_at=now() WHERE id=$2`,
		locationJSON, assetID)
	return err
}

// RentalPickableAsset is the lightweight shape returned to the rental asset picker.
// Covers both MDM-linked and standalone assets.
type RentalPickableAsset struct {
	AssetID            string
	AssetNumber        string
	Name               string
	Spec               string
	DeviceUdid         *string
	SerialNumber       string
	Model              string
	OSVersion          string
	AssetStatus        string
	CategoryID         *string
	CategoryName       string
	LastReturnLocation map[string]interface{}
	LastReturnAt       *time.Time
	// BookedSlots lists open half-day rentals as "YYYY-MM-DD am|pm"; such
	// assets are not marked "rented" (the other half of the day is still free).
	BookedSlots []string
	// OpenWholeDay: any open whole-day rental exists (the old "rented" rule).
	// HeldNow: an open whole-day or timed rental holds the asset this moment.
	OpenWholeDay bool
	HeldNow      bool
	// BookedRanges lists every open rental's span as "start|end" in Taipei
	// time ("2006-01-02T15:04"; end empty when open-ended), for showing what a
	// reservation has to avoid.
	BookedRanges []string
}

// ListRentalPickable returns every asset that could potentially be borrowed, plus
// its MDM device info if linked. The asset's own status is returned as stored;
// whether it counts as "rented" depends on its category's reservation setting,
// so the caller decides (see domain.PickableStatus) from OpenWholeDay/HeldNow.
func (r *AssetRepo) ListRentalPickable(ctx context.Context) ([]*RentalPickableAsset, error) {
	q := `SELECT a.id, a.asset_number, a.name, COALESCE(a.spec,''),
	             a.device_udid,
	             COALESCE(d.serial_number,''), COALESCE(d.model,''), COALESCE(d.os_version,''),
	             COALESCE(a.asset_status,'available') as asset_status,
	             a.category_id, COALESCE(c.name,''),
	             EXISTS(SELECT 1 FROM rentals rl WHERE rl.asset_id = a.id AND rl.period = 'full' AND rl.status IN ('pending','approved','active')) as open_whole_day,
	             EXISTS(SELECT 1 FROM rentals rl WHERE rl.asset_id = a.id AND rl.period = 'full' AND rl.status IN ('pending','approved','active') AND rental_span(rl) @> now()) as held_now,
	             a.last_return_location, a.last_return_at,
	             ARRAY(SELECT to_char(rl.borrow_date AT TIME ZONE 'UTC','YYYY-MM-DD') || ' ' || rl.period FROM rentals rl
	                   WHERE rl.asset_id = a.id AND rl.period <> 'full' AND rl.status IN ('pending','approved','active')
	                   ORDER BY rl.borrow_date, rl.period) as booked_slots,
	             ARRAY(SELECT to_char(lower(rental_span(rl)) AT TIME ZONE 'Asia/Taipei','YYYY-MM-DD"T"HH24:MI') || '|' ||
	                          CASE WHEN upper_inf(rental_span(rl)) THEN '' ELSE to_char(upper(rental_span(rl)) AT TIME ZONE 'Asia/Taipei','YYYY-MM-DD"T"HH24:MI') END
	                   FROM rentals rl
	                   WHERE rl.asset_id = a.id AND rl.status IN ('pending','approved','active') AND NOT isempty(rental_span(rl))
	                   ORDER BY lower(rental_span(rl))) as booked_ranges
	      FROM assets a
	      LEFT JOIN devices d ON a.device_udid = d.udid
	      LEFT JOIN categories c ON a.category_id = c.id
	      WHERE a.is_rentable = TRUE
	      ORDER BY a.name, a.asset_number`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*RentalPickableAsset
	for rows.Next() {
		it := &RentalPickableAsset{}
		var lastReturnLocationJSON []byte
		if err := rows.Scan(&it.AssetID, &it.AssetNumber, &it.Name, &it.Spec,
			&it.DeviceUdid,
			&it.SerialNumber, &it.Model, &it.OSVersion,
			&it.AssetStatus, &it.CategoryID, &it.CategoryName, &it.OpenWholeDay, &it.HeldNow,
			&lastReturnLocationJSON, &it.LastReturnAt, &it.BookedSlots, &it.BookedRanges); err != nil {
			continue
		}
		if len(lastReturnLocationJSON) > 0 {
			json.Unmarshal(lastReturnLocationJSON, &it.LastReturnLocation)
		}
		out = append(out, it)
	}
	return out, nil
}

// ListPickable returns every asset regardless of IsRentable, for picker UIs
// (maintenance/disposal requests) that operate on the full inventory rather
// than just the assets available for loan.
func (r *AssetRepo) ListPickable(ctx context.Context) ([]domain.PickableAsset, error) {
	q := `SELECT a.id, a.asset_number, a.name, COALESCE(a.spec,''),
	             a.device_udid,
	             COALESCE(d.serial_number,''), COALESCE(d.model,''), COALESCE(d.os_version,''),
	             COALESCE(a.asset_status,'available') as asset_status,
	             a.category_id, COALESCE(c.name,''),
	             a.last_return_location, a.last_return_at
	      FROM assets a
	      LEFT JOIN devices d ON a.device_udid = d.udid
	      LEFT JOIN categories c ON a.category_id = c.id
	      ORDER BY a.name, a.asset_number`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.PickableAsset
	for rows.Next() {
		it := domain.PickableAsset{}
		var lastReturnLocationJSON []byte
		if err := rows.Scan(&it.AssetID, &it.AssetNumber, &it.Name, &it.Spec,
			&it.DeviceUdid,
			&it.SerialNumber, &it.Model, &it.OSVersion,
			&it.AssetStatus, &it.CategoryID, &it.CategoryName,
			&lastReturnLocationJSON, &it.LastReturnAt); err != nil {
			continue
		}
		if len(lastReturnLocationJSON) > 0 {
			json.Unmarshal(lastReturnLocationJSON, &it.LastReturnLocation)
		}
		out = append(out, it)
	}
	return out, nil
}

// ListAvailableByCategory returns up to `limit` asset IDs that are rentable,
// available (status=available, not rented), and belong to the given category.
//
// "Not rented" depends on the booking: with reservations, nothing already booked
// may overlap its time span; without, the asset must have no open rental.
func (r *AssetRepo) ListAvailableByCategory(ctx context.Context, categoryID string, b domain.Booking, limit int) ([]string, error) {
	conflict, a2, a3 := bookingConflict(b)
	q := `SELECT a.id FROM assets a
	      WHERE a.is_rentable = TRUE
	        AND a.category_id = $1
	        AND COALESCE(a.asset_status,'available') = 'available'
	        AND NOT EXISTS(
	            SELECT 1 FROM rentals rl
	            WHERE rl.asset_id = a.id AND ` + conflict + `
	        )
	      ORDER BY a.asset_number
	      LIMIT $4`
	rows, err := r.pool.Query(ctx, q, categoryID, a2, a3, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// openRentalConflictSQL is a SQL predicate (over rentals alias rl) that is true
// when rl occupies the asset at any moment of [startArg, endArg) — the
// timestamptz parameters of the booking being checked; a NULL end means
// open-ended. rl's own span comes from rental_span() (migration 042), which
// reads its date, period and optional start/end times. Only open rentals
// count: pending, approved and active.
func openRentalConflictSQL(startArg, endArg string) string {
	return `rl.status IN ('pending','approved','active') AND rental_span(rl) && tstzrange(` +
		startArg + `::timestamptz, COALESCE(` + endArg + `::timestamptz, 'infinity'::timestamptz), '[)')`
}

// openRentalLegacyConflictSQL is the rule for categories without reservations:
// any open rental blocks, except that two half-day rentals (am/pm) coexist when
// they fall on different days or different halves of the same day.
func openRentalLegacyConflictSQL(periodArg, dateArg string) string {
	return `rl.status IN ('pending','approved','active') AND NOT (
	    rl.period <> 'full' AND ` + periodArg + ` <> 'full'
	    AND ((rl.borrow_date AT TIME ZONE 'UTC')::date <> ` + dateArg + `::date OR rl.period <> ` + periodArg + `))`
}

// bookingConflict returns the predicate for a booking plus the values for its
// $2 and $3 parameters (period/date, or start/end).
func bookingConflict(b domain.Booking) (sql string, a2, a3 interface{}) {
	if b.Reservation {
		return openRentalConflictSQL("$2", "$3"), b.Start, b.End
	}
	return openRentalLegacyConflictSQL("$2::text", "$3"), b.Period, b.BorrowDate
}

// CheckAssetAvailability mirrors rental.CheckDeviceAvailability but uses asset_id,
// so standalone assets can be checked too.
// isRented is true when an open rental blocks the booking (see Booking).
func (r *AssetRepo) CheckAssetAvailability(ctx context.Context, assetID string, b domain.Booking) (status string, isRented bool, name string, err error) {
	conflict, a1, a2 := bookingConflict(b)
	err = r.pool.QueryRow(ctx,
		`SELECT COALESCE(a.asset_status,'available'),
		        EXISTS(SELECT 1 FROM rentals rl WHERE rl.asset_id=$1 AND `+conflict+`),
		        COALESCE(a.name, a.asset_number, '')
		 FROM assets a WHERE a.id=$1`, assetID, a1, a2,
	).Scan(&status, &isRented, &name)
	return
}

func (r *AssetRepo) UpdateStatus(ctx context.Context, udid string, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE assets SET asset_status=$1, updated_at=now() WHERE device_udid=$2`, status, udid)
	return err
}

func (r *AssetRepo) Dispose(ctx context.Context, id string, disposedBy string, reason string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE assets SET asset_status='retired', disposed_at=now(), disposed_by=$1, dispose_reason=$2, updated_at=now() WHERE id=$3`,
		disposedBy, reason, id)
	return err
}

func (r *AssetRepo) Transfer(ctx context.Context, id string, transferredTo string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE assets SET asset_status='transferred', transferred_to=$1, transferred_at=now(), updated_at=now() WHERE id=$2`,
		transferredTo, id)
	return err
}

// SetCustodian writes the custodian fields directly. Intended to be called only by
// the custody controller, which also appends an asset_custody_logs entry.
// Passing custodianID=nil clears the custodian (revoke).
func (r *AssetRepo) SetCustodian(ctx context.Context, id string, custodianID *string, custodianName string, assignedDate *time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE assets SET custodian_id=$1, custodian_name=$2, assigned_date=$3, updated_at=now() WHERE id=$4`,
		custodianID, custodianName, assignedDate, id)
	return err
}

// SetHolderByUdid records the rental borrower as the current physical holder.
// Does NOT touch custodian — the long-term responsibility chain stays intact.
func (r *AssetRepo) SetHolderByUdid(ctx context.Context, udid string, holderID string, holderName string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE assets SET current_holder_id=$1, current_holder_name=$2, current_holder_since=now(), updated_at=now() WHERE device_udid=$3`,
		holderID, holderName, udid)
	return err
}

// ClearHolderByUdid clears the current holder when a rental is returned.
func (r *AssetRepo) ClearHolderByUdid(ctx context.Context, udid string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE assets SET current_holder_id=NULL, current_holder_name='', current_holder_since=NULL, updated_at=now() WHERE device_udid=$1`,
		udid)
	return err
}

// SetHolderByID records the current holder of an asset identified by asset id.
// Used by the rental flow so standalone (non-MDM) assets can also be borrowed.
func (r *AssetRepo) SetHolderByID(ctx context.Context, assetID string, holderID string, holderName string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE assets SET current_holder_id=$1, current_holder_name=$2, current_holder_since=now(), updated_at=now() WHERE id=$3`,
		holderID, holderName, assetID)
	return err
}

// ClearHolderByID clears the current holder of an asset identified by asset id.
func (r *AssetRepo) ClearHolderByID(ctx context.Context, assetID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE assets SET current_holder_id=NULL, current_holder_name='', current_holder_since=NULL, updated_at=now() WHERE id=$1`,
		assetID)
	return err
}

// UpdateCustodian is kept for backward compat but deprecated.
// New code should use SetCustodian with an accompanying custody log entry.
func (r *AssetRepo) UpdateCustodian(ctx context.Context, udid, custodianID, custodianName string, clearBorrowDate bool) error {
	if clearBorrowDate {
		_, err := r.pool.Exec(ctx,
			`UPDATE assets SET custodian_id=$1, custodian_name=$2, assigned_date=NULL WHERE device_udid=$3`,
			custodianID, custodianName, udid)
		return err
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE assets SET custodian_id=$1, custodian_name=$2, assigned_date=now() WHERE device_udid=$3`,
		custodianID, custodianName, udid)
	return err
}
