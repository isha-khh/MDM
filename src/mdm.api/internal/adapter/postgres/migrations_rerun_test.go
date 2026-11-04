package postgres

import (
	"context"
	"testing"

	"github.com/anthropics/mdm-server/internal/testutil/pgtest"
)

// The server runs every migration again at each start. Data already in the
// database must come out of that untouched — in particular two separate
// rentals of one person (same purpose, same day) must not be merged into one
// rental_number, which showed an approved rental as "rejected".
func TestMigrationsRerunLeaveRentalNumbersAlone(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := newSeed(t, pool)
	car := s.asset("CAR-RERUN")

	// Two requests by the same person, same purpose, same day: #81 rejected, #82 approved.
	for _, r := range []struct {
		no     int
		status string
		ret    string
	}{{81, "rejected", "2026-11-05"}, {82, "approved", "2026-11-04"}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO rentals (asset_id, borrower_id, borrower_name, status, purpose, period, borrow_date, expected_return, rental_number)
			 VALUES ($1, $2, 'borrower', $3, 'P71 進場', 'full', '2026-11-04'::date::timestamp AT TIME ZONE 'UTC', $4::date, $5)`,
			car, s.user, r.status, r.ret, r.no); err != nil {
			t.Fatal(err)
		}
	}

	pgtest.Migrate(t, pool) // a server restart

	rows, err := pool.Query(ctx, `SELECT rental_number, status FROM rentals ORDER BY rental_number`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[int]string{}
	for rows.Next() {
		var n int
		var st string
		rows.Scan(&n, &st)
		got[n] = st
	}
	if len(got) != 2 || got[81] != "rejected" || got[82] != "approved" {
		t.Fatalf("a restart changed the rental numbers: %v (want 81 rejected, 82 approved)", got)
	}
}
