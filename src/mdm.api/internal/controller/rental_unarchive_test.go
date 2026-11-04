package controller

import (
	"testing"
)

// Archiving hides rentals from the normal list; unarchiving brings them back.
func TestRentals_UnarchiveRestoresArchivedRentals(t *testing.T) {
	e := newWebEnv(t)
	id1, no1 := e.rentCar(e.asset)
	e.webCall("POST", "/api/rentals/"+id1+"/reject", nil)
	id2, no2 := e.rentCar(e.asset2())
	e.webCall("POST", "/api/rentals/"+id2+"/reject", nil)

	listed := func(archived bool) map[int]bool {
		path := "/api/rentals"
		if archived {
			path += "?show_archived=true"
		}
		_, out := e.webCall("GET", path, nil)
		got := map[int]bool{}
		for _, x := range out["rentals"].([]interface{}) {
			row := x.(map[string]interface{})
			got[int(row["rental_number"].(float64))] = row["is_archived"].(bool)
		}
		return got
	}

	if st, _ := e.webCall("POST", "/api/rentals-archive", map[string]interface{}{"ids": []string{id1, id2}}); st != 200 {
		t.Fatalf("archive: %d", st)
	}
	if got := listed(false); len(got) != 0 {
		t.Fatalf("archived rentals should be hidden: %v", got)
	}
	if got := listed(true); !got[no1] || !got[no2] {
		t.Fatalf("show_archived should list both, flagged: %v", got)
	}

	// Restore one of them: only that one comes back.
	st, out := e.webCall("POST", "/api/rentals-unarchive", map[string]interface{}{"ids": []string{id1}})
	if st != 200 || out["count"] != float64(1) {
		t.Fatalf("unarchive: %d %v", st, out)
	}
	if got := listed(false); len(got) != 1 || got[no1] {
		t.Fatalf("only the restored rental is back and no longer archived: %v", got)
	}
	// Restoring something that is not archived (or twice) restores nothing and is not an error.
	if st, out := e.webCall("POST", "/api/rentals-unarchive", map[string]interface{}{"ids": []string{id1}}); st != 200 || out["count"] != float64(0) {
		t.Fatalf("second unarchive: %d %v", st, out)
	}
	// Bad input.
	for name, body := range map[string]interface{}{
		"no ids":       map[string]interface{}{"ids": []string{}},
		"not a uuid":   map[string]interface{}{"ids": []string{"abc"}},
		"sql in an id": map[string]interface{}{"ids": []string{id2 + "' OR '1'='1"}},
	} {
		if st, _ := e.webCall("POST", "/api/rentals-unarchive", body); st != 400 {
			t.Errorf("%s: %d, want 400", name, st)
		}
	}
	if st, _ := e.webCall("GET", "/api/rentals-unarchive", nil); st != 405 {
		t.Errorf("GET: %d", st)
	}
	if got := listed(false); len(got) != 1 {
		t.Fatalf("bad requests must not restore anything: %v", got)
	}
}
