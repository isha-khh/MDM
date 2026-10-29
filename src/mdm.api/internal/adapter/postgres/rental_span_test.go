package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anthropics/mdm-server/internal/domain"
)

// These tests run against a real PostgreSQL: the behaviour under test is SQL.
// Set TEST_DATABASE_URL to any database on a server you can create databases
// on (e.g. postgres://user:pass@localhost:5432/postgres); each test run uses
// its own temporary database with every migration applied, and drops it after.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	ap, err := pgxpool.New(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("mdm_test_%d", time.Now().UnixNano())
	if _, err := ap.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		ap.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(admin)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		ap.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		ap.Close()
	})
	files, _ := filepath.Glob("../../db/migrations/*.up.sql")
	sort.Strings(files)
	for _, f := range files {
		sqlText, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		// like the server at boot: a failing migration is logged, not fatal
		if _, err := pool.Exec(ctx, string(sqlText)); err != nil {
			t.Logf("migration %s: %v", filepath.Base(f), err)
		}
	}
	return pool
}

type seed struct {
	t    *testing.T
	pool *pgxpool.Pool
	user string
	n    int
}

func newSeed(t *testing.T, pool *pgxpool.Pool) *seed {
	s := &seed{t: t, pool: pool}
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (username, password_hash) VALUES ('borrower','x') RETURNING id`).Scan(&s.user); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *seed) asset(number string) string {
	var id string
	if err := s.pool.QueryRow(context.Background(),
		`INSERT INTO assets (asset_number, name, is_rentable) VALUES ($1, $1, TRUE) RETURNING id`, number).Scan(&id); err != nil {
		s.t.Fatal(err)
	}
	return id
}

type rentalRow struct {
	status   string
	period   string
	borrow   string  // 'YYYY-MM-DD' (stored as UTC midnight, as the app does)
	ret      *string // expected return date
	startAt  *string // timestamptz literals for a timed booking
	endAt    *string
	rentalNo int
}

func sp(v string) *string { return &v }

func (s *seed) rental(assetID string, r rentalRow) {
	s.t.Helper()
	if r.period == "" {
		r.period = "full"
	}
	if r.rentalNo == 0 {
		s.n++
		r.rentalNo = 1000 + s.n
	}
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO rentals (asset_id, borrower_id, borrower_name, status, period, borrow_date, expected_return, start_at, end_at, rental_number)
		 VALUES ($1, $2, 'borrower', $3, $4, ($5::date)::timestamp AT TIME ZONE 'UTC', $6::date, $7::timestamptz, $8::timestamptz, $9)`,
		assetID, s.user, r.status, r.period, r.borrow, r.ret, r.startAt, r.endAt, r.rentalNo)
	if err != nil {
		s.t.Fatal(err)
	}
}

func tp(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, domain.TaipeiZone)
}
func tptr(t time.Time) *time.Time { return &t }

// free reports whether the asset can be booked for [start, end) in a category
// that allows reservations (time-span overlap decides).
func free(t *testing.T, repo *AssetRepo, asset string, start time.Time, end *time.Time) bool {
	t.Helper()
	_, rented, _, err := repo.CheckAssetAvailability(context.Background(), asset,
		domain.Booking{Reservation: true, Start: start, End: end})
	if err != nil {
		t.Fatal(err)
	}
	return !rented
}

// freeLegacy is the same question for a category without reservations: the
// asset must have no open rental at all (half-days excepted), whatever the dates.
func freeLegacy(t *testing.T, repo *AssetRepo, asset, period, date string) bool {
	t.Helper()
	bd, _ := time.Parse("2006-01-02", date)
	_, rented, _, err := repo.CheckAssetAvailability(context.Background(), asset, domain.NewBooking(period, bd, &bd, false))
	if err != nil {
		t.Fatal(err)
	}
	return !rented
}

func TestConflict_DateBasedRentalsBlockOnlyTheirDays(t *testing.T) {
	pool := testPool(t)
	s, repo := newSeed(t, pool), NewAssetRepo(pool)
	car := s.asset("CAR-1")
	s.rental(car, rentalRow{status: "approved", borrow: "2026-10-30", ret: sp("2026-10-31")}) // Oct 30 and 31, whole days

	cases := []struct {
		name     string
		start    time.Time
		end      *time.Time
		wantFree bool
	}{
		{"the day before", tp(2026, 10, 29, 0, 0), tptr(tp(2026, 10, 30, 0, 0)), true},
		{"first booked day", tp(2026, 10, 30, 8, 0), tptr(tp(2026, 10, 30, 9, 0)), false},
		{"last booked day", tp(2026, 10, 31, 23, 0), tptr(tp(2026, 11, 1, 0, 0)), false},
		{"the day after", tp(2026, 11, 1, 0, 0), tptr(tp(2026, 11, 2, 0, 0)), true},
		{"straddling the start", tp(2026, 10, 29, 20, 0), tptr(tp(2026, 10, 30, 1, 0)), false},
		{"open-ended from before", tp(2026, 10, 20, 0, 0), nil, false},
		{"open-ended from after", tp(2026, 11, 5, 0, 0), nil, true},
	}
	for _, c := range cases {
		if got := free(t, repo, car, c.start, c.end); got != c.wantFree {
			t.Errorf("%s: free=%v, want %v", c.name, got, c.wantFree)
		}
	}
}

func TestConflict_OpenEndedRentalBlocksEverythingAfterItsStart(t *testing.T) {
	pool := testPool(t)
	s, repo := newSeed(t, pool), NewAssetRepo(pool)
	car := s.asset("CAR-2")
	s.rental(car, rentalRow{status: "approved", borrow: "2026-10-30"}) // no return date
	if !free(t, repo, car, tp(2026, 10, 20, 0, 0), tptr(tp(2026, 10, 25, 0, 0))) {
		t.Error("before it starts the asset is free")
	}
	if free(t, repo, car, tp(2027, 3, 1, 0, 0), tptr(tp(2027, 3, 2, 0, 0))) {
		t.Error("an open-ended rental holds the asset indefinitely")
	}
}

func TestConflict_HalfDaysCoexistButNotWithAWholeDay(t *testing.T) {
	pool := testPool(t)
	s, repo := newSeed(t, pool), NewAssetRepo(pool)
	car := s.asset("CAR-3")
	s.rental(car, rentalRow{status: "approved", period: "am", borrow: "2026-10-30", ret: sp("2026-10-30")})
	if !free(t, repo, car, tp(2026, 10, 30, 12, 0), tptr(tp(2026, 10, 31, 0, 0))) {
		t.Error("the afternoon is free when only the morning is booked")
	}
	if free(t, repo, car, tp(2026, 10, 30, 0, 0), tptr(tp(2026, 10, 30, 12, 0))) {
		t.Error("the morning is taken")
	}
	if free(t, repo, car, tp(2026, 10, 30, 0, 0), tptr(tp(2026, 10, 31, 0, 0))) {
		t.Error("a whole day overlaps the booked morning")
	}
}

func TestConflict_TimedBookingsAreCheckedToTheMinute(t *testing.T) {
	pool := testPool(t)
	s, repo := newSeed(t, pool), NewAssetRepo(pool)
	car := s.asset("CAR-4")
	s.rental(car, rentalRow{status: "approved", borrow: "2026-10-30", ret: sp("2026-10-30"),
		startAt: sp("2026-10-30 09:00+08"), endAt: sp("2026-10-30 12:00+08")})
	cases := []struct {
		name       string
		start, end time.Time
		wantFree   bool
	}{
		{"right after, back to back", tp(2026, 10, 30, 12, 0), tp(2026, 10, 30, 17, 0), true},
		{"one minute of overlap", tp(2026, 10, 30, 11, 59), tp(2026, 10, 30, 13, 0), false},
		{"right before, back to back", tp(2026, 10, 30, 7, 0), tp(2026, 10, 30, 9, 0), true},
		{"inside", tp(2026, 10, 30, 10, 0), tp(2026, 10, 30, 11, 0), false},
		{"covering it", tp(2026, 10, 30, 8, 0), tp(2026, 10, 30, 13, 0), false},
		{"another day", tp(2026, 10, 31, 9, 0), tp(2026, 10, 31, 12, 0), true},
	}
	for _, c := range cases {
		if got := free(t, repo, car, c.start, tptr(c.end)); got != c.wantFree {
			t.Errorf("%s: free=%v, want %v", c.name, got, c.wantFree)
		}
	}
}

func TestConflict_OnlyOpenRentalsBlock(t *testing.T) {
	pool := testPool(t)
	s, repo := newSeed(t, pool), NewAssetRepo(pool)
	car := s.asset("CAR-5")
	for _, st := range []string{"returned", "rejected", "cancelled", "pending_return"} {
		s.rental(car, rentalRow{status: st, borrow: "2026-10-30", ret: sp("2026-10-30")})
	}
	if !free(t, repo, car, tp(2026, 10, 30, 8, 0), tptr(tp(2026, 10, 30, 18, 0))) {
		t.Error("returned, rejected and cancelled rentals must not block")
	}
	for _, st := range []string{"pending", "approved", "active"} {
		car2 := s.asset("CAR-5-" + st)
		s.rental(car2, rentalRow{status: st, borrow: "2026-10-30", ret: sp("2026-10-30")})
		if free(t, repo, car2, tp(2026, 10, 30, 8, 0), tptr(tp(2026, 10, 30, 18, 0))) {
			t.Errorf("%s rental should block", st)
		}
	}
}

func TestPickable_StatusDependsOnTheCategorySetting(t *testing.T) {
	pool := testPool(t)
	s, repo := newSeed(t, pool), NewAssetRepo(pool)
	today := time.Now().UTC().Format("2006-01-02")
	later := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02")
	earlier := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")
	earlier2 := time.Now().UTC().AddDate(0, 0, -29).Format("2006-01-02")

	s.asset("A-free")
	future := s.asset("A-future") // reserved next month
	s.rental(future, rentalRow{status: "approved", borrow: later, ret: sp(later)})
	now := s.asset("A-now")
	s.rental(now, rentalRow{status: "approved", borrow: today, ret: sp(today)})
	overdue := s.asset("A-overdue") // still out although it was due long ago
	s.rental(overdue, rentalRow{status: "active", borrow: earlier, ret: sp(earlier2)})

	items, err := repo.ListRentalPickable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status := func(reservation bool) map[string]string {
		out := map[string]string{}
		for _, it := range items {
			out[it.AssetNumber] = domain.PickableStatus(it.AssetStatus, reservation, it.HeldNow, it.OpenWholeDay)
		}
		return out
	}
	// Without reservations the old rule holds: any open rental means "rented".
	old := status(false)
	for k, want := range map[string]string{"A-free": "available", "A-future": "rented", "A-now": "rented", "A-overdue": "rented"} {
		if old[k] != want {
			t.Errorf("no reservations, %s: %q, want %q", k, old[k], want)
		}
	}
	// With reservations only what holds the asset right now is "rented"; one
	// booked for next month is still bookable today.
	res := status(true)
	for k, want := range map[string]string{"A-free": "available", "A-future": "available", "A-now": "rented", "A-overdue": "rented"} {
		if res[k] != want {
			t.Errorf("reservations, %s: %q, want %q", k, res[k], want)
		}
	}
	// The booked spans are listed so the UI can show what to avoid.
	for _, it := range items {
		if it.AssetNumber == "A-future" && (len(it.BookedRanges) != 1 || !strings.HasPrefix(it.BookedRanges[0], later+"T00:00|")) {
			t.Errorf("booked ranges: %v", it.BookedRanges)
		}
	}
}

func TestConflict_WithoutReservationsAnyOpenRentalBlocks(t *testing.T) {
	pool := testPool(t)
	s, repo := newSeed(t, pool), NewAssetRepo(pool)
	car := s.asset("OLD-1")
	s.rental(car, rentalRow{status: "approved", borrow: "2026-11-20", ret: sp("2026-11-21")}) // booked weeks ahead
	if freeLegacy(t, repo, car, "full", "2026-10-30") {
		t.Error("without reservations, a booking weeks away still blocks today (the old behaviour)")
	}
	// the same asset, same dates, in a category with reservations: free
	if !free(t, repo, car, tp(2026, 10, 30, 0, 0), tptr(tp(2026, 10, 31, 0, 0))) {
		t.Error("with reservations the earlier span is free")
	}
	// half-days of different halves still coexist under the old rule
	half := s.asset("OLD-2")
	s.rental(half, rentalRow{status: "approved", period: "am", borrow: "2026-10-30", ret: sp("2026-10-30")})
	if !freeLegacy(t, repo, half, "pm", "2026-10-30") {
		t.Error("the afternoon is free when only the morning is booked")
	}
	if freeLegacy(t, repo, half, "am", "2026-10-30") || freeLegacy(t, repo, half, "full", "2026-10-30") {
		t.Error("the morning and the whole day are taken")
	}
}

func TestCategoryRule_ReservationFlagIsInherited(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	user := newSeed(t, pool).user
	var parent, child, other string
	for _, c := range []struct {
		name   string
		parent *string
		out    *string
	}{{"vehicles", nil, &parent}, {"company car", &parent, &child}, {"laptops", nil, &other}} {
		if err := pool.QueryRow(ctx, `INSERT INTO categories (name, parent_id) VALUES ($1, $2) RETURNING id`, c.name, c.parent).Scan(c.out); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewCategoryRentalRuleRepo(pool)
	cats := []*domain.Category{{ID: parent, Name: "vehicles"}, {ID: child, Name: "company car", ParentID: &parent}, {ID: other, Name: "laptops"}}
	yes := true
	if err := repo.Upsert(ctx, parent, nil, nil, &yes, user); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{parent: true, child: true, other: false} {
		got, err := repo.ResolveReservationEnabled(ctx, cats, id)
		if err != nil || got != want {
			t.Errorf("%s: %v %v, want %v", id, got, err, want)
		}
	}
	// Setting reservations leaves the other rules alone, and a child can opt out.
	no := false
	if err := repo.Upsert(ctx, child, nil, nil, &no, user); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ResolveReservationEnabled(ctx, cats, child); got {
		t.Error("the child's own setting wins over the parent's")
	}
	rule, _ := repo.Get(ctx, parent)
	if rule.DailyTrackingRequired || rule.HalfDayEnabled || !rule.ReservationEnabled {
		t.Errorf("%+v", rule)
	}
}

func TestHeldByAnother_BlocksHandingOverUntilTheCarIsBack(t *testing.T) {
	pool := testPool(t)
	s, repo := newSeed(t, pool), NewRentalRepo(pool)
	car := s.asset("CAR-9")
	s.rental(car, rentalRow{status: "active", borrow: "2026-10-28", ret: sp("2026-10-30"), rentalNo: 800})
	s.rental(car, rentalRow{status: "approved", borrow: "2026-11-02", ret: sp("2026-11-02"), rentalNo: 801}) // reserved behind it
	held, ok, err := repo.HeldByAnother(context.Background(), 801)
	if err != nil || !ok || held != "CAR-9" {
		t.Errorf("801 waits for the car: %q %v %v", held, ok, err)
	}
	if _, ok, _ := repo.HeldByAnother(context.Background(), 800); ok {
		t.Error("the borrower who has it isn't blocked by the reservation behind them")
	}
	pool.Exec(context.Background(), `UPDATE rentals SET status='returned' WHERE rental_number=800`)
	if _, ok, _ := repo.HeldByAnother(context.Background(), 801); ok {
		t.Error("once it is returned the reservation can be handed over")
	}
	pool.Exec(context.Background(), `UPDATE rentals SET status='pending_return' WHERE rental_number=800`)
	if _, ok, _ := repo.HeldByAnother(context.Background(), 801); !ok {
		t.Error("returned-by-borrower but not yet verified still counts as out")
	}
}

func TestExtension_WouldConflictWithALaterReservation(t *testing.T) {
	pool := testPool(t)
	s, ext := newSeed(t, pool), NewRentalExtensionRepo(pool)
	car := s.asset("CAR-6")
	s.rental(car, rentalRow{status: "active", borrow: "2026-10-28", ret: sp("2026-10-30"), rentalNo: 500})
	s.rental(car, rentalRow{status: "approved", borrow: "2026-11-02", ret: sp("2026-11-03"), rentalNo: 501}) // reserved later

	d := func(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		to   time.Time
		want bool
	}{{d(1), false}, {d(2), true}, {d(5), true}} {
		got, err := ext.WouldConflict(context.Background(), 500, c.to)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("extend to %s: conflict=%v, want %v", c.to.Format("01-02"), got, c.want)
		}
	}
	// The reservation itself isn't affected by the check on someone else's rental.
	if got, _ := ext.WouldConflict(context.Background(), 501, d(10)); got {
		t.Error("nothing is booked after the reservation")
	}
}

func TestExtension_ApprovingMovesATimedBookingToTheSameClockTime(t *testing.T) {
	pool := testPool(t)
	s, ext := newSeed(t, pool), NewRentalExtensionRepo(pool)
	car := s.asset("CAR-7")
	s.rental(car, rentalRow{status: "active", borrow: "2026-10-30", ret: sp("2026-10-30"),
		startAt: sp("2026-10-30 09:00+08"), endAt: sp("2026-10-30 17:30+08"), rentalNo: 600})
	id, err := ext.Create(context.Background(), &domain.RentalExtension{
		RentalNumber: 600, RequestedBy: s.user,
		PreviousExpectedReturn: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), RequestedExpectedReturn: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
		Reason: "出差延長",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ext.Approve(context.Background(), id, s.user, "ok"); err != nil {
		t.Fatal(err)
	}
	var endAt time.Time
	var expected time.Time
	if err := pool.QueryRow(context.Background(), `SELECT end_at, expected_return FROM rentals WHERE rental_number=600`).Scan(&endAt, &expected); err != nil {
		t.Fatal(err)
	}
	if !endAt.Equal(tp(2026, 11, 2, 17, 30)) || expected.Format("2006-01-02") != "2026-11-02" {
		t.Errorf("end_at %v, expected_return %v", endAt.In(domain.TaipeiZone), expected)
	}
}

func TestExtension_ApprovalIsRefusedWhenItWouldCollide(t *testing.T) {
	pool := testPool(t)
	s, ext := newSeed(t, pool), NewRentalExtensionRepo(pool)
	car := s.asset("CAR-8")
	s.rental(car, rentalRow{status: "active", borrow: "2026-10-28", ret: sp("2026-10-30"), rentalNo: 700})
	id, err := ext.Create(context.Background(), &domain.RentalExtension{
		RentalNumber: 700, RequestedBy: s.user,
		PreviousExpectedReturn: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), RequestedExpectedReturn: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
		Reason: "延長",
	})
	if err != nil {
		t.Fatal(err)
	}
	// someone reserves the car for Nov 2 after the request was filed
	s.rental(car, rentalRow{status: "approved", borrow: "2026-11-02", ret: sp("2026-11-02"), rentalNo: 701})
	if _, err := ext.Approve(context.Background(), id, s.user, ""); err != ErrExtensionConflict {
		t.Fatalf("want ErrExtensionConflict, got %v", err)
	}
	var ret time.Time
	pool.QueryRow(context.Background(), `SELECT expected_return FROM rentals WHERE rental_number=700`).Scan(&ret)
	if ret.Format("2006-01-02") != "2026-10-30" {
		t.Errorf("a refused approval must not change the rental: %v", ret)
	}
	var status string
	pool.QueryRow(context.Background(), `SELECT status FROM rental_extensions WHERE id=$1`, id).Scan(&status)
	if status != "pending" {
		t.Errorf("the request stays pending so it can be rejected or retried: %s", status)
	}
}
