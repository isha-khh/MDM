package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/mdm-server/internal/domain"
)

type fakeClients struct {
	byHash  map[string]*domain.IntegrationClient
	touched []string
}

func (f *fakeClients) ClientByKeyHash(_ context.Context, h string) (*domain.IntegrationClient, error) {
	return f.byHash[h], nil
}
func (f *fakeClients) TouchClient(_ context.Context, id string) { f.touched = append(f.touched, id) }

func TestGenerateKey(t *testing.T) {
	k1, h1, p1, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, _, _, _ := GenerateKey()
	if k1 == k2 {
		t.Fatal("keys must differ")
	}
	if !strings.HasPrefix(k1, KeyPrefix) || len(k1) < 40 || !strings.HasPrefix(k1, p1) || len(p1) >= len(k1) {
		t.Fatalf("key %q prefix %q", k1, p1)
	}
	if h1 != HashKey(k1) || len(h1) != 64 || strings.Contains(h1, k1) {
		t.Fatalf("hash %q", h1)
	}
}

func req(remote, auth string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest("GET", "/api/v1/reservations", nil)
	r.RemoteAddr = remote
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestClientIP_TrustsTheHeaderOnlyFromTheProxy(t *testing.T) {
	a, err := NewIntegrationAuth(&fakeClients{}, []string{"10.0.0.0/8", "127.0.0.1/32", " "})
	if err != nil {
		t.Fatal(err)
	}
	cf := map[string]string{"CF-Connecting-IP": "203.0.113.9"}
	for name, c := range map[string]struct{ remote, want string }{
		"from the tunnel daemon":     {"10.1.2.3:5555", "203.0.113.9"},
		"loopback":                   {"127.0.0.1:5555", "203.0.113.9"},
		"from anywhere else":         {"198.51.100.20:5555", "198.51.100.20"},
		"ipv6 peer outside the list": {"[2001:db8::1]:5555", "2001:db8::1"},
	} {
		if got := a.ClientIP(req(c.remote, "", cf)); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
	if got := a.ClientIP(req("10.1.2.3:1", "", map[string]string{"CF-Connecting-IP": "garbage"})); got != "10.1.2.3" {
		t.Errorf("a malformed header is ignored: %s", got)
	}
	if got := a.ClientIP(req("10.1.2.3:1", "", map[string]string{"X-Forwarded-For": "203.0.113.9"})); got != "10.1.2.3" {
		t.Errorf("X-Forwarded-For is not used: %s", got)
	}
	if _, err := NewIntegrationAuth(&fakeClients{}, []string{"not-a-cidr"}); err == nil {
		t.Error("a bad proxy CIDR should be reported")
	}
}

func TestAuthenticate(t *testing.T) {
	key, hash, _, _ := GenerateKey()
	client := &domain.IntegrationClient{ID: "c1", Name: "dispatch", Enabled: true}
	store := &fakeClients{byHash: map[string]*domain.IntegrationClient{hash: client}}
	a, _ := NewIntegrationAuth(store, []string{"10.0.0.0/8"})

	status := func(r *http.Request) int {
		_, _, err := a.Authenticate(r)
		if err == nil {
			return 200
		}
		return err.(*domain.APIError).Status
	}
	for name, c := range map[string]struct {
		auth string
		want int
	}{
		"missing":      {"", 401},
		"wrong scheme": {"Basic " + key, 401},
		"empty bearer": {"Bearer ", 401},
		"unknown key":  {"Bearer mdm_nope", 401},
		"good":         {"Bearer " + key, 200},
		"scheme case":  {"bearer " + key, 200},
	} {
		if got := status(req("10.0.0.1:1", c.auth, nil)); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
	if len(store.touched) != 2 {
		t.Errorf("successful calls are recorded: %v", store.touched)
	}

	client.IPAllowlist = []string{"203.0.113.0/24"}
	if got := status(req("10.0.0.1:1", "Bearer "+key, map[string]string{"CF-Connecting-IP": "198.51.100.1"})); got != 403 {
		t.Errorf("outside the allowlist: %d", got)
	}
	if got := status(req("10.0.0.1:1", "Bearer "+key, map[string]string{"CF-Connecting-IP": "203.0.113.50"})); got != 200 {
		t.Errorf("inside the allowlist: %d", got)
	}
	// Someone reaching the port directly cannot claim an allowed address.
	if got := status(req("198.51.100.99:1", "Bearer "+key, map[string]string{"CF-Connecting-IP": "203.0.113.50"})); got != 403 {
		t.Errorf("spoofed header from an untrusted peer: %d", got)
	}
	client.Enabled = false
	if got := status(req("10.0.0.1:1", "Bearer "+key, map[string]string{"CF-Connecting-IP": "203.0.113.50"})); got != 403 {
		t.Errorf("disabled: %d", got)
	}
}
