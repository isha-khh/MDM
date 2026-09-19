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

type RentalRepo struct{ pool *pgxpool.Pool }

func NewRentalRepo(pool *pgxpool.Pool) *RentalRepo { return &RentalRepo{pool: pool} }

const rentalSelectColumns = `r.id, r.asset_id, r.device_udid, r.borrower_id, r.borrower_name, r.approver_id, r.approver_name,
	             r.status, r.purpose, r.borrow_date, r.expected_return, r.actual_return, r.notes,
	             r.created_at, r.updated_at,
	             COALESCE(d.device_name,'') as device_name, COALESCE(d.serial_number,'') as device_serial,
	             COALESCE(a.asset_number,'') as asset_number, COALESCE(a.name,'') as asset_name,
	             a.custodian_id, COALESCE(a.custodian_name,'') as custodian_name,
	             r.rental_number, r.is_archived, r.return_checklist, r.return_notes, r.multi_day_reason, a.category_id`

const rentalFromJoin = `FROM rentals r
	LEFT JOIN assets a ON a.id = r.asset_id
	LEFT JOIN devices d ON d.udid = COALESCE(r.device_udid, a.device_udid)`

func scanRental(rows interface {
	Scan(dest ...interface{}) error
}) (*domain.Rental, error) {
	rental := &domain.Rental{}
	var assetID, approverID *string
	var deviceUdid *string
	var expectedReturn, actualReturn *time.Time
	var checklistJSON []byte
	err := rows.Scan(
		&rental.ID, &assetID, &deviceUdid, &rental.BorrowerID, &rental.BorrowerName,
		&approverID, &rental.ApproverName,
		&rental.Status, &rental.Purpose, &rental.BorrowDate, &expectedReturn, &actualReturn, &rental.Notes,
		&rental.CreatedAt, &rental.UpdatedAt,
		&rental.DeviceName, &rental.DeviceSerial,
		&rental.AssetNumber, &rental.AssetName,
		&rental.CustodianID, &rental.CustodianName,
		&rental.RentalNumber, &rental.IsArchived, &checklistJSON, &rental.ReturnNotes,
		&rental.MultiDayReason, &rental.CategoryID,
	)
	if err != nil {
		return nil, err
	}
	rental.AssetID = assetID
	if deviceUdid != nil {
		rental.DeviceUdid = *deviceUdid
	}
	rental.ApproverID = approverID
	rental.ExpectedReturn = expectedReturn
	rental.ActualReturn = actualReturn
	if len(checklistJSON) > 0 {
		json.Unmarshal(checklistJSON, &rental.ReturnChecklist)
	}
	return rental, nil
}

func (r *RentalRepo) List(ctx context.Context, status, deviceUdid string, showArchived bool) ([]*domain.Rental, error) {
	q := `SELECT ` + rentalSelectColumns + ` ` + rentalFromJoin + ` WHERE 1=1`
	args := []interface{}{}
	idx := 1
	if status != "" {
		q += fmt.Sprintf(` AND r.status=$%d`, idx)
		args = append(args, status)
		idx++
	}
	if deviceUdid != "" {
		q += fmt.Sprintf(` AND r.device_udid=$%d`, idx)
		args = append(args, deviceUdid)
		idx++
	}
	if !showArchived {
		q += ` AND r.is_archived = false`
	}
	q += ` ORDER BY r.rental_number DESC`

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rentals []*domain.Rental
	for rows.Next() {
		rental, err := scanRental(rows)
		if err != nil {
			continue
		}
		rentals = append(rentals, rental)
	}
	return rentals, nil
}

func (r *RentalRepo) Create(ctx context.Context, rental *domain.Rental) (string, error) {
	var id string
	var udid interface{}
	if rental.DeviceUdid != "" {
		udid = rental.DeviceUdid
	}
	// BorrowDate is now caller-supplied (Phase 1 of 租借 2.1: fillable at
	// creation, unrestricted range) rather than relying on the column's
	// DEFAULT now(). Callers that don't set it explicitly get today, same as
	// the old default behaved.
	borrowDate := rental.BorrowDate
	if borrowDate.IsZero() {
		borrowDate = time.Now()
	}
	err := r.pool.QueryRow(ctx,
		`INSERT INTO rentals (asset_id, device_udid, borrower_id, borrower_name, purpose, borrow_date, expected_return, notes, rental_number, multi_day_reason)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		rental.AssetID, udid, rental.BorrowerID, rental.BorrowerName, rental.Purpose,
		borrowDate, rental.ExpectedReturn, rental.Notes, rental.RentalNumber, rental.MultiDayReason,
	).Scan(&id)
	return id, err
}

func (r *RentalRepo) GetByID(ctx context.Context, id string) (*domain.Rental, error) {
	rental := &domain.Rental{}
	var rentalNumber int
	var deviceUdid, assetID *string
	var expectedReturn *time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT asset_id, device_udid, status, rental_number, borrow_date, expected_return FROM rentals WHERE id=$1`, id,
	).Scan(&assetID, &deviceUdid, &rental.Status, &rentalNumber, &rental.BorrowDate, &expectedReturn)
	if err != nil {
		return nil, err
	}
	rental.ID = id
	rental.AssetID = assetID
	if deviceUdid != nil {
		rental.DeviceUdid = *deviceUdid
	}
	rental.RentalNumber = rentalNumber
	rental.ExpectedReturn = expectedReturn
	return rental, nil
}

func (r *RentalRepo) NextRentalNumber(ctx context.Context) (int, error) {
	var num int
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(MAX(rental_number), 0) + 1 FROM rentals`).Scan(&num)
	return num, err
}

func (r *RentalRepo) UpdateStatusByNumber(ctx context.Context, rentalNumber int, fromStatus, toStatus string, approverID *string, approverName string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE rentals SET status=$1, approver_id=$2, approver_name=$3, updated_at=now() WHERE rental_number=$4 AND status=$5`,
		toStatus, approverID, approverName, rentalNumber, fromStatus)
	return err
}

func (r *RentalRepo) ActivateByNumber(ctx context.Context, rentalNumber int) error {
	// borrow_date is no longer reset to now() here: Phase 1 of 租借 2.1 made
	// it a caller-supplied, locked-after-creation field (see Create), so
	// activation must not silently overwrite whatever the requester entered.
	_, err := r.pool.Exec(ctx,
		`UPDATE rentals SET status='active', updated_at=now() WHERE rental_number=$1 AND status='approved'`,
		rentalNumber)
	return err
}

func (r *RentalRepo) ReturnByNumber(ctx context.Context, rentalNumber int, checklist []byte, notes string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE rentals SET status='returned', actual_return=now(), updated_at=now(),
		 return_checklist=$1, return_notes=$2
		 WHERE rental_number=$3 AND status='active'`,
		checklist, notes, rentalNumber)
	return err
}

func (r *RentalRepo) DeleteByNumber(ctx context.Context, rentalNumber int) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM rentals WHERE rental_number=$1`, rentalNumber)
	return err
}

func (r *RentalRepo) Archive(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	q := fmt.Sprintf("UPDATE rentals SET is_archived = true, updated_at = now() WHERE id IN (%s)", strings.Join(placeholders, ","))
	_, err := r.pool.Exec(ctx, q, args...)
	return err
}

func (r *RentalRepo) ListDeviceUdidsByNumber(ctx context.Context, rentalNumber int) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT device_udid FROM rentals WHERE rental_number=$1 AND device_udid IS NOT NULL`, rentalNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var udids []string
	for rows.Next() {
		var udid string
		rows.Scan(&udid)
		udids = append(udids, udid)
	}
	return udids, nil
}

// ListAssetIDsByNumber returns all asset_ids rented under a single rental_number.
// Preferred over ListDeviceUdidsByNumber because it covers standalone assets too.
func (r *RentalRepo) ListAssetIDsByNumber(ctx context.Context, rentalNumber int) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT asset_id FROM rentals WHERE rental_number=$1 AND asset_id IS NOT NULL`, rentalNumber)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids, nil
}

func (r *RentalRepo) GetBorrowerInfo(ctx context.Context, rentalID string) (string, string, error) {
	var borrowerID, borrowerName string
	err := r.pool.QueryRow(ctx,
		`SELECT borrower_id, borrower_name FROM rentals WHERE id=$1`, rentalID,
	).Scan(&borrowerID, &borrowerName)
	return borrowerID, borrowerName, err
}

// ListOverdue returns active rentals whose expected_return is before today (grouped by rental_number, one row per group).
func (r *RentalRepo) ListOverdue(ctx context.Context) ([]*domain.Rental, error) {
	q := `SELECT DISTINCT ON (r.rental_number) ` + rentalSelectColumns + ` ` + rentalFromJoin + `
	      WHERE r.status = 'active' AND r.expected_return IS NOT NULL AND r.expected_return < CURRENT_DATE
	      ORDER BY r.rental_number, r.created_at`

	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rentals []*domain.Rental
	for rows.Next() {
		rental, err := scanRental(rows)
		if err != nil {
			continue
		}
		rentals = append(rentals, rental)
	}
	return rentals, nil
}

func (r *RentalRepo) CheckDeviceAvailability(ctx context.Context, udid string) (string, bool, bool, error) {
	var assetStatus string
	var isRented, isLostMode bool
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(a.asset_status,'available'),
		        EXISTS(SELECT 1 FROM rentals rl WHERE rl.device_udid=$1 AND rl.status IN ('pending','approved','active')),
		        d.is_lost_mode
		 FROM devices d LEFT JOIN assets a ON a.device_udid=d.udid WHERE d.udid=$1`, udid,
	).Scan(&assetStatus, &isRented, &isLostMode)
	return assetStatus, isRented, isLostMode, err
}
