package middleware

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/anthropics/mdm-server/internal/domain"
)

// KeyPrefix starts every integration API key, so a leaked one is recognisable
// (and can be found by secret scanners).
const KeyPrefix = "mdm_"

// GenerateKey makes a new API key. Only its hash is stored; the key itself is
// shown once. prefix is the part kept to recognise the key in the UI.
func GenerateKey() (key, hash, prefix string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	key = KeyPrefix + base64.RawURLEncoding.EncodeToString(b)
	return key, HashKey(key), key[:len(KeyPrefix)+6], nil
}

// HashKey is how API keys are stored: SHA-256, hex. Keys are random and long,
// so a fast hash is enough and lets the key be looked up directly.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// ClientStore finds the owner of an API key.
type ClientStore interface {
	ClientByKeyHash(ctx context.Context, keyHash string) (*domain.IntegrationClient, error)
	TouchClient(ctx context.Context, id string)
}

// IntegrationAuth authenticates callers of the integration API by bearer API key
// and checks the client's IP allowlist.
type IntegrationAuth struct {
	store   ClientStore
	trusted []*net.IPNet
}

// NewIntegrationAuth builds the authenticator. trustedProxies are the CIDRs of
// the hops (the Cloudflare tunnel daemon) whose CF-Connecting-IP header may be
// believed for the caller's address; from anywhere else it is ignored.
func NewIntegrationAuth(store ClientStore, trustedProxies []string) (*IntegrationAuth, error) {
	a := &IntegrationAuth{store: store}
	for _, c := range trustedProxies {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", c, err)
		}
		a.trusted = append(a.trusted, n)
	}
	return a, nil
}

// ClientIP is the address of the caller. Behind the tunnel the TCP peer is the
// tunnel daemon, and the real address arrives in CF-Connecting-IP — believed
// only when the peer is one of the trusted proxies, since anyone who can reach
// the port directly could otherwise claim any address.
func (a *IntegrationAuth) ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer != nil {
		for _, n := range a.trusted {
			if n.Contains(peer) {
				if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); ip != nil {
					return ip.String()
				}
				break
			}
		}
	}
	return host
}

func authError(status int, code, msg string) *domain.APIError {
	return &domain.APIError{Status: status, Code: code, Message: msg}
}

// Authenticate returns the calling client and its address, or the error to send back.
func (a *IntegrationAuth) Authenticate(r *http.Request) (*domain.IntegrationClient, string, error) {
	h := r.Header.Get("Authorization")
	scheme, key, ok := strings.Cut(h, " ")
	key = strings.TrimSpace(key)
	if !ok || !strings.EqualFold(scheme, "Bearer") || key == "" {
		return nil, "", authError(http.StatusUnauthorized, domain.CodeUnauthorized, "需要 Authorization: Bearer <API key>")
	}
	client, err := a.store.ClientByKeyHash(r.Context(), HashKey(key))
	if err != nil {
		return nil, "", err
	}
	if client == nil {
		return nil, "", authError(http.StatusUnauthorized, domain.CodeUnauthorized, "API key 無效")
	}
	if !client.Enabled {
		return nil, "", authError(http.StatusForbidden, domain.CodeForbidden, "此 API key 已停用")
	}
	ip := a.ClientIP(r)
	if !domain.IPAllowed(ip, client.IPAllowlist) {
		return nil, ip, authError(http.StatusForbidden, domain.CodeForbidden, "來源 IP 不在允許清單內")
	}
	a.store.TouchClient(r.Context(), client.ID)
	return client, ip, nil
}
