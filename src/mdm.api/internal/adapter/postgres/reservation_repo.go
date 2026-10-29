package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

// ReservationRepo stores reservations made through the integration API. They
// are ordinary rentals of the assets mapped by integration_resources, so the
// web UI, the conflict rules and the history all see the same rows.
type ReservationRepo struct{ pool *pgxpool.Pool }

func NewReservationRepo(pool *pgxpool.Pool) *ReservationRepo { return &ReservationRepo{pool: pool} }

// reservationDB is what both a pool and a transaction offer.
type reservationDB interface {
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
	Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error)
}

// The rentals table is aliased "rl" so the shared conflict predicate
// (openRentalConflictSQL) can be used in the same query.
const reservationSelect = `SELECT rl.rental_number, rl.external_ref, ir.resource_id, ir.service_type, rl.status,
	       rl.borrower_ref, COALESCE(u.sso_sub,'') <> '', COALESCE(u.username,''), COALESCE(u.email,''), rl.borrower_name,
	       rl.passengers, rl.period, rl.borrow_date, rl.expected_return, rl.start_at, rl.end_at,
	       rl.destination, rl.notes, rl.cancel_reason, rl.created_via, rl.created_at, rl.updated_at
	FROM rentals rl
	JOIN integration_resources ir ON ir.asset_id = rl.asset_id
	LEFT JOIN users u ON u.id = rl.borrower_id`

func scanReservation(row pgx.Row) (*domain.Reservation, error) {
	r := &domain.Reservation{}
	var period string
	var borrowDate time.Time
	var expectedReturn, startAt, endAt *time.Time
	var destination []byte
	err := row.Scan(&r.RentalNumber, &r.ExternalRef, &r.ResourceID, &r.ServiceType, &r.RentalStatus,
		&r.BorrowerRef, &r.BorrowerSSO, &r.BorrowerUser, &r.BorrowerEmail, &r.BorrowerName,
		&r.Passengers, &period, &borrowDate, &expectedReturn, &startAt, &endAt,
		&destination, &r.Note, &r.CancelReason, &r.CreatedVia, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return nil, err
	}
	r.Status = domain.ReservationStatus(r.RentalStatus)
	if r.Passengers == nil {
		r.Passengers = []string{}
	}
	if startAt != nil && endAt != nil {
		r.StartAt, r.EndAt = *startAt, endAt
	} else {
		r.StartAt, r.EndAt = domain.RentalWindow(period, borrowDate, expectedReturn)
	}
	if len(destination) > 0 {
		var d domain.Destination
		if json.Unmarshal(destination, &d) == nil {
			r.Destination = &d
		}
	}
	return r, nil
}

func queryReservations(ctx context.Context, q reservationDB, tail string, args ...interface{}) ([]*domain.Reservation, error) {
	rows, err := q.Query(ctx, reservationSelect+" "+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Reservation
	for rows.Next() {
		r, err := scanReservation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func getReservation(ctx context.Context, q reservationDB, number int) (*domain.Reservation, error) {
	rs, err := queryReservations(ctx, q, `WHERE rl.rental_number=$1 ORDER BY rl.created_at LIMIT 1`, number)
	if err != nil || len(rs) == 0 {
		return nil, err
	}
	return rs[0], nil
}

// Get returns the reservation with the given rental number; nil when none.
func (r *ReservationRepo) Get(ctx context.Context, number int) (*domain.Reservation, error) {
	return getReservation(ctx, r.pool, number)
}

// FindByExternalRef returns the reservations made for a caller's reference,
// live ones first (a cancelled one may share its reference with a later one).
func (r *ReservationRepo) FindByExternalRef(ctx context.Context, ref string) ([]*domain.Reservation, error) {
	return queryReservations(ctx, r.pool,
		`WHERE rl.external_ref=$1 ORDER BY (rl.status = 'cancelled'), rl.created_at DESC`, ref)
}

// ListWindow returns every reservation (whoever made it) overlapping [from, to),
// optionally of a single resource.
func (r *ReservationRepo) ListWindow(ctx context.Context, from, to time.Time, resourceID string) ([]*domain.Reservation, error) {
	return queryReservations(ctx, r.pool,
		`WHERE rental_span(rl) && tstzrange($1::timestamptz, $2::timestamptz, '[)')
		   AND ($3 = '' OR ir.resource_id = $3)
		 ORDER BY lower(rental_span(rl)), rl.rental_number`, from, to, resourceID)
}

// Conflicts returns the open reservations of the resource overlapping [start, end).
func (r *ReservationRepo) Conflicts(ctx context.Context, assetID string, start, end time.Time) ([]*domain.Reservation, error) {
	return conflictsOf(ctx, r.pool, assetID, start, end, "")
}

func conflictsOf(ctx context.Context, q reservationDB, assetID string, start, end time.Time, exceptRentalID string) ([]*domain.Reservation, error) {
	return queryReservations(ctx, q,
		`WHERE rl.asset_id=$1 AND `+openRentalConflictSQL("$2", "$3")+` AND ($4 = '' OR rl.id::text <> $4)
		 ORDER BY lower(rental_span(rl)), rl.rental_number`, assetID, start, end, exceptRentalID)
}

// AssetStatus is the stored status of an asset (empty if unknown).
func (r *ReservationRepo) AssetStatus(ctx context.Context, assetID string) (string, error) {
	var s string
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(asset_status,'available') FROM assets WHERE id=$1`, assetID).Scan(&s)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return s, err
}

// bookable: an asset in one of these statuses can take reservations. 'rented'
// only says it is out right now; whether a span is free is the overlap check.
func bookable(assetStatus string) bool {
	return assetStatus == "available" || assetStatus == "rented"
}

// IdemKey identifies one write of one client; Hash covers what the request asked for.
type IdemKey struct {
	ClientID string
	Key      string
	Hash     string
}

// WriteResult is the stored outcome of a write, replayed to a retry.
type WriteResult struct {
	Status int
	Body   []byte
}

// Actor is who an API write is recorded against.
type Actor struct {
	ClientID   string
	ClientName string
	IP         string
}

// Write runs fn in a transaction guarded by the idempotency key: a retry of an
// earlier request replays its stored result instead of running fn again; the
// same key with a different request is a conflict. Only successful outcomes
// are stored — fn returning an error rolls everything back, and the caller may
// try again with the same key.
func (r *ReservationRepo) Write(ctx context.Context, idem IdemKey, fn func(tx *ReservationTx) (WriteResult, error)) (res WriteResult, replayed bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return res, false, err
	}
	defer tx.Rollback(ctx)

	// Requests with the same key run one after another, so a retry racing the
	// original sees its result.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, idem.ClientID+"/"+idem.Key); err != nil {
		return res, false, err
	}
	var storedHash string
	err = tx.QueryRow(ctx,
		`SELECT request_hash, status_code, response FROM integration_idempotency WHERE client_id=$1 AND idem_key=$2`,
		idem.ClientID, idem.Key).Scan(&storedHash, &res.Status, &res.Body)
	switch {
	case err == nil:
		if storedHash != idem.Hash {
			return WriteResult{}, false, &domain.APIError{Status: http.StatusConflict, Code: domain.CodeIdempotencyConflict,
				Message: "此 Idempotency-Key 已用於內容不同的請求"}
		}
		return res, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return res, false, err
	}

	res, err = fn(&ReservationTx{q: tx})
	if err != nil {
		return WriteResult{}, false, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO integration_idempotency (client_id, idem_key, request_hash, status_code, response) VALUES ($1,$2,$3,$4,$5)`,
		idem.ClientID, idem.Key, idem.Hash, res.Status, res.Body); err != nil {
		return WriteResult{}, false, err
	}
	// Keys are only good for a week; trim as we go.
	tx.Exec(ctx, `DELETE FROM integration_idempotency WHERE created_at < now() - interval '7 days'`)
	if err := tx.Commit(ctx); err != nil {
		return WriteResult{}, false, err
	}
	return res, false, nil
}

// ReservationTx is the set of writes valid inside Write's transaction.
type ReservationTx struct{ q reservationDB }

// NewBorrower asks for a user account to be created for the borrower.
type NewBorrower struct {
	Username    string // wanted login name (taken names get a suffix)
	DisplayName string
	Email       string
}

// Borrower is the person a reservation is for: an existing user, or one to create.
type Borrower struct {
	UserID string
	Name   string       // display name of the existing user
	Create *NewBorrower // set when UserID is empty
	Ref    string       // identifier the caller used (AD account), echoed back
}

// NewReservation is a reservation to make.
type NewReservation struct {
	ExternalRef string
	Resource    *domain.IntegrationResource
	Borrower    Borrower
	Passengers  []string
	Start, End  time.Time
	Destination *domain.Destination
	Note        string
	Actor       Actor
}

// ReservationPatch is a change to a reservation; nil fields stay as they are.
type ReservationPatch struct {
	Start, End  *time.Time
	Borrower    *Borrower
	Passengers  *[]string
	Destination *domain.Destination
	Note        *string
	Actor       Actor
}

func apiErr(status int, code, msg string) *domain.APIError {
	return &domain.APIError{Status: status, Code: code, Message: msg}
}

func conflictError(cs []*domain.Reservation) *domain.APIError {
	infos := make([]domain.ReservationConflict, 0, len(cs))
	for _, c := range cs {
		infos = append(infos, c.Conflict())
	}
	e := apiErr(http.StatusConflict, domain.CodeVehicleUnavailable, "該時段已被預約")
	e.Details = infos
	return e
}

// dateUTC turns a moment into the date convention the rental tables use: the
// Taipei calendar date, stored as midnight UTC.
func dateUTC(t time.Time) time.Time {
	y, m, d := t.In(domain.TaipeiZone).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// returnDate is the last calendar day of a span ending at end (exclusive): a
// span ending exactly at midnight ends the day before.
func returnDate(start, end time.Time) time.Time {
	e := end.In(domain.TaipeiZone)
	if e.After(start) && e.Hour() == 0 && e.Minute() == 0 {
		e = e.AddDate(0, 0, -1)
	}
	return dateUTC(e)
}

func randomSuffix() string {
	b := make([]byte, 3)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// createBorrower inserts a login-less account (the person signs in through SSO,
// which finds it by email) and returns its id.
func (t *ReservationTx) createBorrower(ctx context.Context, nb *NewBorrower) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		username := nb.Username
		if attempt > 0 {
			username += "_" + randomSuffix()
		}
		var id string
		err := t.q.QueryRow(ctx,
			`INSERT INTO users (username, password_hash, role, display_name, email)
			 VALUES ($1, '', 'viewer', $2, $3) ON CONFLICT DO NOTHING RETURNING id`,
			username, nb.DisplayName, nb.Email).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // name taken
		}
		return id, err
	}
	return "", fmt.Errorf("could not find a free username for %q", nb.Username)
}

// resolveBorrower returns the user id and display name for a borrower, creating the account if asked to.
func (t *ReservationTx) resolveBorrower(ctx context.Context, b Borrower) (id, name string, err error) {
	if b.UserID != "" {
		return b.UserID, b.Name, nil
	}
	if b.Create == nil {
		return "", "", errors.New("borrower missing")
	}
	id, err = t.createBorrower(ctx, b.Create)
	return id, b.Create.DisplayName, err
}

func (t *ReservationTx) lockAsset(ctx context.Context, assetID string) (status string, err error) {
	err = t.q.QueryRow(ctx,
		`SELECT COALESCE(asset_status,'available') FROM assets WHERE id=$1 FOR UPDATE`, assetID).Scan(&status)
	return
}

func (t *ReservationTx) audit(ctx context.Context, a Actor, action string, number int, detail interface{}) {
	d, _ := json.Marshal(detail)
	t.q.Exec(ctx,
		`INSERT INTO audit_logs (user_id, username, action, target, detail, module, ip_address, user_agent)
		 VALUES ($1,$2,$3,$4,$5,'rental',$6,'integration-api')`,
		"api:"+a.ClientID, a.ClientName, action, domain.ReservationID(number), string(d), a.IP)
}

func destinationJSON(d *domain.Destination) interface{} {
	if d == nil {
		return nil
	}
	b, _ := json.Marshal(d)
	return string(b)
}

func purposeOf(note string) string {
	if strings.TrimSpace(note) != "" {
		return note
	}
	return "派發系統預約"
}

// Create books the resource: nothing else may overlap its span, and the
// reservation is approved at once (the integration key is the approval).
func (t *ReservationTx) Create(ctx context.Context, in NewReservation) (*domain.Reservation, error) {
	status, err := t.lockAsset(ctx, in.Resource.AssetID)
	if err != nil {
		return nil, err
	}
	if !bookable(status) {
		return nil, apiErr(http.StatusConflict, domain.CodeVehicleUnavailable, "車輛目前無法出借（"+status+"）")
	}

	var existing int
	err = t.q.QueryRow(ctx,
		`SELECT rental_number FROM rentals WHERE external_ref=$1 AND status <> 'cancelled' LIMIT 1`, in.ExternalRef).Scan(&existing)
	if err == nil {
		e := apiErr(http.StatusConflict, domain.CodeDuplicateExternal, "externalRef 已有預約："+domain.ReservationID(existing))
		e.Details = map[string]string{"reservationId": domain.ReservationID(existing)}
		return nil, e
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	conflicts, err := conflictsOf(ctx, t.q, in.Resource.AssetID, in.Start, in.End, "")
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return nil, conflictError(conflicts)
	}

	borrowerID, borrowerName, err := t.resolveBorrower(ctx, in.Borrower)
	if err != nil {
		return nil, err
	}

	// rental_number is a plain counter shared by the web UI; take the lock so
	// two API requests never get the same number.
	if _, err := t.q.Exec(ctx, `SELECT pg_advisory_xact_lock(43001)`); err != nil {
		return nil, err
	}
	var number int
	if err := t.q.QueryRow(ctx, `SELECT COALESCE(MAX(rental_number), 0) + 1 FROM rentals`).Scan(&number); err != nil {
		return nil, err
	}

	borrowDate, expected := dateUTC(in.Start), returnDate(in.Start, in.End)
	multiDay := ""
	if !borrowDate.Equal(expected) {
		multiDay = purposeOf(in.Note)
	}
	passengers := in.Passengers
	if passengers == nil {
		passengers = []string{}
	}
	_, err = t.q.Exec(ctx,
		`INSERT INTO rentals (asset_id, device_udid, borrower_id, borrower_name, approver_name, status, purpose,
		                      borrow_date, expected_return, notes, rental_number, multi_day_reason, period,
		                      start_at, end_at, external_ref, created_via, borrower_ref, passengers, destination)
		 VALUES ($1, (SELECT device_udid FROM assets WHERE id=$1), $2, $3, $4, 'approved', $5,
		         $6, $7, $8, $9, $10, 'full',
		         $11, $12, $13, 'api', NULLIF($14,''), $15, $16::jsonb)`,
		in.Resource.AssetID, borrowerID, borrowerName, in.Actor.ClientName, purposeOf(in.Note),
		borrowDate, expected, in.Note, number, multiDay,
		in.Start, in.End, in.ExternalRef, in.Borrower.Ref, passengers, destinationJSON(in.Destination))
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" && strings.Contains(pe.ConstraintName, "external_ref") {
			return nil, apiErr(http.StatusConflict, domain.CodeDuplicateExternal, "externalRef 已有預約")
		}
		return nil, err
	}
	res, err := getReservation(ctx, t.q, number)
	if err != nil || res == nil {
		return nil, fmt.Errorf("reload reservation %d: %v", number, err)
	}
	t.audit(ctx, in.Actor, "reservation.create", number, map[string]interface{}{
		"externalRef": in.ExternalRef, "resourceId": in.Resource.ResourceID,
		"startAt": in.Start, "endAt": in.End, "borrower": borrowerName,
	})
	return res, nil
}

// lockReservation locks the rental (after its asset, the same order Create
// uses) and returns its id, rental status and asset.
func (t *ReservationTx) lockReservation(ctx context.Context, number int) (id, status, assetID string, err error) {
	err = t.q.QueryRow(ctx,
		`SELECT rl.id, rl.asset_id FROM rentals rl
		 JOIN integration_resources ir ON ir.asset_id = rl.asset_id
		 WHERE rl.rental_number=$1 ORDER BY rl.created_at LIMIT 1`, number).Scan(&id, &assetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", apiErr(http.StatusNotFound, domain.CodeNotFound, "找不到預約 "+domain.ReservationID(number))
	}
	if err != nil {
		return
	}
	if _, err = t.lockAsset(ctx, assetID); err != nil {
		return
	}
	err = t.q.QueryRow(ctx, `SELECT status FROM rentals WHERE id=$1 FOR UPDATE`, id).Scan(&status)
	return
}

// Update changes a reservation that has not been picked up yet.
func (t *ReservationTx) Update(ctx context.Context, number int, p ReservationPatch) (*domain.Reservation, error) {
	id, status, assetID, err := t.lockReservation(ctx, number)
	if err != nil {
		return nil, err
	}
	if status != "pending" && status != "approved" {
		return nil, apiErr(http.StatusUnprocessableEntity, domain.CodeNotModifiable,
			"預約目前為 "+domain.ReservationStatus(status)+"，無法修改")
	}
	cur, err := getReservation(ctx, t.q, number)
	if err != nil || cur == nil {
		return nil, fmt.Errorf("load reservation %d: %v", number, err)
	}

	start, end := cur.StartAt, cur.EndAt
	if p.Start != nil {
		start = *p.Start
	}
	if p.End != nil {
		end = p.End
	}
	if end == nil || !end.After(start) {
		return nil, apiErr(http.StatusBadRequest, domain.CodeValidationFailed, "endAt 必須晚於 startAt")
	}
	if p.Start != nil || p.End != nil {
		conflicts, err := conflictsOf(ctx, t.q, assetID, start, *end, id)
		if err != nil {
			return nil, err
		}
		if len(conflicts) > 0 {
			return nil, conflictError(conflicts)
		}
	}

	var borrowerID, borrowerName *string
	borrowerRef := cur.BorrowerRef
	if p.Borrower != nil {
		uid, name, err := t.resolveBorrower(ctx, *p.Borrower)
		if err != nil {
			return nil, err
		}
		borrowerID, borrowerName = &uid, &name
		ref := p.Borrower.Ref
		borrowerRef = &ref
	}
	passengers := cur.Passengers
	if p.Passengers != nil {
		passengers = *p.Passengers
	}
	dest := cur.Destination
	if p.Destination != nil {
		dest = p.Destination
	}
	note := cur.Note
	if p.Note != nil {
		note = *p.Note
	}

	borrowDate, expected := dateUTC(start), returnDate(start, *end)
	multiDay := ""
	if !borrowDate.Equal(expected) {
		multiDay = purposeOf(note)
	}
	_, err = t.q.Exec(ctx,
		`UPDATE rentals SET
		     borrower_id   = COALESCE($2::uuid, borrower_id),
		     borrower_name = COALESCE($3, borrower_name),
		     borrower_ref  = NULLIF($4,''),
		     passengers = $5, destination = $6::jsonb, notes = $7, purpose = $8,
		     borrow_date = $9, expected_return = $10, start_at = $11, end_at = $12,
		     multi_day_reason = $13, updated_at = now()
		 WHERE id = $1`,
		id, borrowerID, borrowerName, deref(borrowerRef),
		passengers, destinationJSON(dest), note, purposeOf(note),
		borrowDate, expected, start, *end, multiDay)
	if err != nil {
		return nil, err
	}
	res, err := getReservation(ctx, t.q, number)
	if err != nil || res == nil {
		return nil, fmt.Errorf("reload reservation %d: %v", number, err)
	}
	t.audit(ctx, p.Actor, "reservation.update", number, map[string]interface{}{
		"before": cur.Conflict(), "after": res.Conflict(),
		"borrowerChanged": p.Borrower != nil, "passengersChanged": p.Passengers != nil,
	})
	return res, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Cancel cancels a reservation that has not been picked up. Cancelling one
// that is already cancelled is not an error (cancel is idempotent).
func (t *ReservationTx) Cancel(ctx context.Context, number int, reason string, a Actor) (*domain.Reservation, error) {
	id, status, _, err := t.lockReservation(ctx, number)
	if err != nil {
		return nil, err
	}
	switch status {
	case "cancelled", "rejected":
		return getReservation(ctx, t.q, number)
	case "pending", "approved":
	default:
		return nil, apiErr(http.StatusUnprocessableEntity, domain.CodeNotModifiable,
			"預約目前為 "+domain.ReservationStatus(status)+"，無法取消")
	}
	if _, err := t.q.Exec(ctx,
		`UPDATE rentals SET status='cancelled', cancel_reason=$2, cancelled_at=now(), updated_at=now() WHERE id=$1`,
		id, reason); err != nil {
		return nil, err
	}
	t.audit(ctx, a, "reservation.cancel", number, map[string]string{"reason": reason})
	return getReservation(ctx, t.q, number)
}
