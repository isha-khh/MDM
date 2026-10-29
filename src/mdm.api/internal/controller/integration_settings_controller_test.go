package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/mdm-server/internal/adapter/postgres"
	"github.com/anthropics/mdm-server/internal/middleware"
	"github.com/anthropics/mdm-server/internal/service"
	"github.com/anthropics/mdm-server/internal/testutil/pgtest"
)

func TestIntegrationSettings(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	const secret = "test-secret"
	repo := postgres.NewIntegrationRepo(pool)
	auth := middleware.NewAuthHelper(secret, nil)

	mux := http.NewServeMux()
	NewIntegrationSettingsController(repo, postgres.NewAuditRepo(pool), auth, ":8081").RegisterRoutes(mux)
	web := httptest.NewServer(mux)
	defer web.Close()

	apiAuth, _ := middleware.NewIntegrationAuth(repo, []string{"127.0.0.0/8"})
	svc := service.NewReservationService(repo, postgres.NewReservationRepo(pool), postgres.NewUserRepo(pool))
	api := httptest.NewServer(NewIntegrationController(svc, apiAuth).Handler())
	defer api.Close()

	admin, _, _, _ := middleware.GenerateTokens(secret, "11111111-1111-1111-1111-111111111111", "admin", "admin", "sys_admin")
	plain, _, _, _ := middleware.GenerateTokens(secret, "22222222-2222-2222-2222-222222222222", "bob", "viewer", "user")

	do := func(token, method, path string, body interface{}) (int, map[string]interface{}) {
		t.Helper()
		var rdr bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rdr = *bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, web.URL+path, &rdr)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	apiStatus := func(key string) int {
		req, _ := http.NewRequest("GET", api.URL+"/api/v1/reservations?externalRef=x", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// Only a system administrator may touch any of it.
	for _, c := range [][2]string{{"GET", "/api/settings/integration"}, {"POST", "/api/settings/integration/key"},
		{"PUT", "/api/settings/integration"}, {"PUT", "/api/settings/integration/resources/X"}, {"DELETE", "/api/settings/integration/resources/X"}} {
		if st, _ := do(plain, c[0], c[1], map[string]string{}); st != 403 {
			t.Errorf("%s %s as a plain user: %d", c[0], c[1], st)
		}
		if st, _ := do("", c[0], c[1], map[string]string{}); st != 403 {
			t.Errorf("%s %s without login: %d", c[0], c[1], st)
		}
	}

	st, body := do(admin, "GET", "/api/settings/integration", nil)
	if st != 200 || body["client"] != nil || body["listener_enabled"] != true {
		t.Fatalf("before any key: %d %v", st, body)
	}
	if st, _ := do(admin, "PUT", "/api/settings/integration", map[string]bool{"enabled": false}); st != 409 {
		t.Errorf("settings before a key exists: %d", st)
	}

	// Issue a key: shown once, stored only as a hash, and it works.
	st, body = do(admin, "POST", "/api/settings/integration/key", nil)
	key, _ := body["key"].(string)
	if st != 200 || !strings.HasPrefix(key, "mdm_") {
		t.Fatalf("issue: %d %v", st, body)
	}
	var stored int
	pool.QueryRow(ctx, `SELECT count(*) FROM integration_clients WHERE key_hash=$1 AND key_hash <> $2`, middleware.HashKey(key), key).Scan(&stored)
	if stored != 1 {
		t.Fatal("the key must be stored as its hash")
	}
	if st, body := do(admin, "GET", "/api/settings/integration", nil); st != 200 || strings.Contains(toJSON(body), key) {
		t.Fatalf("the key must not be shown again: %v", body)
	}
	if got := apiStatus(key); got != 200 {
		t.Fatalf("the new key: %d", got)
	}

	// Rotating replaces it at once; the limits stay.
	do(admin, "PUT", "/api/settings/integration", map[string]interface{}{"ip_allowlist": []string{"203.0.113.0/24"}})
	_, body = do(admin, "POST", "/api/settings/integration/key", nil)
	key2, _ := body["key"].(string)
	if key2 == key || apiStatus(key) != 401 || apiStatus(key2) != 403 /* not on the allowlist */ {
		t.Fatalf("rotation: old=%d new=%d", apiStatus(key), apiStatus(key2))
	}
	if c := body["client"].(map[string]interface{}); c["rotated_at"] == nil || len(c["ip_allowlist"].([]interface{})) != 1 {
		t.Errorf("rotation keeps the limits: %v", c)
	}

	// Settings are checked.
	if st, _ := do(admin, "PUT", "/api/settings/integration", map[string]interface{}{"ip_allowlist": []string{"not an ip"}}); st != 400 {
		t.Errorf("bad ip: %d", st)
	}
	if st, _ := do(admin, "PUT", "/api/settings/integration", map[string]interface{}{"auto_create_email_domains": []string{"no_dot"}}); st != 400 {
		t.Errorf("bad domain: %d", st)
	}
	st, body = do(admin, "PUT", "/api/settings/integration", map[string]interface{}{
		"ip_allowlist": []string{}, "auto_create_email_domains": []string{" @Mail.Isha.org.tw ", ""}, "enabled": true})
	c := body["client"].(map[string]interface{})
	if st != 200 || len(c["ip_allowlist"].([]interface{})) != 0 || c["auto_create_email_domains"].([]interface{})[0] != "mail.isha.org.tw" {
		t.Fatalf("update: %d %v", st, body)
	}
	if apiStatus(key2) != 200 {
		t.Error("cleared allowlist lets the key in")
	}
	do(admin, "PUT", "/api/settings/integration", map[string]bool{"enabled": false})
	if apiStatus(key2) != 403 {
		t.Error("a disabled key is refused")
	}

	// Resources.
	var asset string
	pool.QueryRow(ctx, `INSERT INTO assets (asset_number, name, is_rentable) VALUES ('CAR-01','公務車',TRUE) RETURNING id`).Scan(&asset)
	if st, _ := do(admin, "PUT", "/api/settings/integration/resources/bad%20id", map[string]string{"asset_id": asset}); st != 400 {
		t.Errorf("bad resource id: %d", st)
	}
	if st, _ := do(admin, "PUT", "/api/settings/integration/resources/RENTAL_CAR_01", map[string]string{"asset_id": "00000000-0000-0000-0000-000000000000"}); st != 400 {
		t.Errorf("unknown asset: %d", st)
	}
	if st, _ := do(admin, "PUT", "/api/settings/integration/resources/RENTAL_CAR_01", map[string]string{"asset_id": asset, "service_type": "TAXI"}); st != 400 {
		t.Errorf("unknown service type: %d", st)
	}
	st, body = do(admin, "PUT", "/api/settings/integration/resources/RENTAL_CAR_01", map[string]interface{}{
		"asset_id": asset, "allowed_cities": []string{"台南市", " 高雄市 ", ""}})
	rs := body["resources"].([]interface{})
	r0 := rs[0].(map[string]interface{})
	if st != 200 || len(rs) != 1 || r0["asset_number"] != "CAR-01" || r0["service_type"] != "RENTAL_CAR" || r0["enabled"] != true ||
		len(r0["allowed_cities"].([]interface{})) != 2 {
		t.Fatalf("map resource: %d %v", st, body)
	}
	if st, body = do(admin, "DELETE", "/api/settings/integration/resources/RENTAL_CAR_01", nil); st != 200 || len(body["resources"].([]interface{})) != 0 {
		t.Errorf("delete: %d %v", st, body)
	}
	if st, _ = do(admin, "DELETE", "/api/settings/integration/resources/RENTAL_CAR_01", nil); st != 404 {
		t.Errorf("delete twice: %d", st)
	}
	var audited int
	pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action LIKE 'integration_%' AND user_id='11111111-1111-1111-1111-111111111111'`).Scan(&audited)
	if audited < 5 {
		t.Errorf("administrative changes should be audited, found %d", audited)
	}
}

func toJSON(v interface{}) string { b, _ := json.Marshal(v); return string(b) }
