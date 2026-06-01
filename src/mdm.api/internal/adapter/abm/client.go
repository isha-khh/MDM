// Package abm is a client for the Apple School and Business Manager API
// (https://api-business.apple.com). It signs ES256 JWTs, exchanges them for
// OAuth bearer tokens at account.apple.com, and exposes the subset of
// endpoints the DEP scheduler needs:
//
//   - ListOrgDevices: every device in the org (full pagination), with
//     productFamily so we can route Mac/iPad/iPhone to different DEP templates.
//   - ListMDMServers / ListMDMServerSerials: which serials are already assigned
//     to our MDM service in ABM (we cross-check, but in practice ABM's default
//     platform assignment handles this automatically).
//
// Token caching: access tokens last 3600s; we keep one and re-sign + re-exchange
// only when it's within 60s of expiry. Safe to call from multiple goroutines.
package abm

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/anthropics/mdm-server/internal/domain"
)

const (
	tokenURL = "https://account.apple.com/auth/oauth2/v2/token"
	apiBase  = "https://api-business.apple.com"
	audience = "https://account.apple.com/auth/oauth2/v2/token"

	// refresh tokens with this much margin so a long-running request that grabbed
	// a near-expiry token can still finish before APNs rejects.
	tokenRefreshMargin = 60 * time.Second
)

type Config struct {
	KeyPath  string // PEM file (SEC1 or PKCS#8)
	ClientID string // BUSINESSAPI.<UUID>
	TeamID   string // usually same value as ClientID in Apple's sample
	KeyID    string // KID shown next to the public key in ABM
	Scope    string // "business.api"
}

type Client struct {
	cfg  Config
	priv *ecdsa.PrivateKey
	http *http.Client

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.KeyPath == "" || cfg.ClientID == "" || cfg.KeyID == "" {
		return nil, errors.New("abm: KeyPath, ClientID, KeyID are required")
	}
	if cfg.TeamID == "" {
		cfg.TeamID = cfg.ClientID
	}
	if cfg.Scope == "" {
		cfg.Scope = "business.api"
	}
	priv, err := loadECKey(cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("abm: load key: %w", err)
	}
	return &Client{
		cfg:  cfg,
		priv: priv,
		http: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// ── domain ──────────────────────────────────────────────────────────────

// Device is an alias for domain.ABMDevice so callers don't need to know about
// the domain package when only using this adapter for tests.
type Device = domain.ABMDevice

type MDMServer struct {
	ID         string
	ServerName string
	ServerType string
}

// ── public API ─────────────────────────────────────────────────────────

// ListOrgDevices returns every device in the org, following pagination links
// until exhausted. Limit per page is 100 (ABM's max).
func (c *Client) ListOrgDevices(ctx context.Context) ([]domain.ABMDevice, error) {
	next := apiBase + "/v1/orgDevices?limit=100" +
		"&fields%5BorgDevices%5D=serialNumber,productFamily,deviceModel,status,addedToOrgDateTime"

	var out []domain.ABMDevice
	for next != "" {
		page, links, err := c.fetchOrgDevicesPage(ctx, next)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		next = links.Next
	}
	return out, nil
}

// ListMDMServers returns the org's registered MDM servers in ABM.
// In practice this is small (usually 1).
func (c *Client) ListMDMServers(ctx context.Context) ([]MDMServer, error) {
	u := apiBase + "/v1/mdmServers?fields%5BmdmServers%5D=serverName,serverType,createdDateTime,updatedDateTime"
	body, err := c.doAuthGET(ctx, u)
	if err != nil {
		return nil, err
	}
	var r struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				ServerName string `json:"serverName"`
				ServerType string `json:"serverType"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("abm: decode mdmServers: %w", err)
	}
	out := make([]MDMServer, len(r.Data))
	for i, s := range r.Data {
		out[i] = MDMServer{ID: s.ID, ServerName: s.Attributes.ServerName, ServerType: s.Attributes.ServerType}
	}
	return out, nil
}

// ListMDMServerSerials returns serial numbers assigned to a given MDM server.
// Uses /relationships/devices (Apple only allows GET_RELATIONSHIP, not GET_RELATED).
// Pagination is supported.
func (c *Client) ListMDMServerSerials(ctx context.Context, serverID string) ([]string, error) {
	next := apiBase + "/v1/mdmServers/" + url.PathEscape(serverID) + "/relationships/devices?limit=100"
	var out []string
	for next != "" {
		body, err := c.doAuthGET(ctx, next)
		if err != nil {
			return nil, err
		}
		var r struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Links struct {
				Next string `json:"next"`
			} `json:"links"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("abm: decode mdmServer devices: %w", err)
		}
		for _, d := range r.Data {
			out = append(out, d.ID)
		}
		next = r.Links.Next
	}
	return out, nil
}

// ── pagination + decode helpers ────────────────────────────────────────

type pageLinks struct {
	Next string `json:"next"`
}

func (c *Client) fetchOrgDevicesPage(ctx context.Context, u string) ([]domain.ABMDevice, pageLinks, error) {
	body, err := c.doAuthGET(ctx, u)
	if err != nil {
		return nil, pageLinks{}, err
	}
	var r struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				SerialNumber       string `json:"serialNumber"`
				DeviceModel        string `json:"deviceModel"`
				ProductFamily      string `json:"productFamily"`
				Status             string `json:"status"`
				AddedToOrgDateTime string `json:"addedToOrgDateTime"`
			} `json:"attributes"`
		} `json:"data"`
		Links pageLinks `json:"links"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, pageLinks{}, fmt.Errorf("abm: decode orgDevices page: %w", err)
	}
	devices := make([]domain.ABMDevice, len(r.Data))
	for i, d := range r.Data {
		serial := d.Attributes.SerialNumber
		if serial == "" {
			serial = d.ID // ABM uses serial as the id in some response shapes
		}
		added, _ := time.Parse(time.RFC3339, d.Attributes.AddedToOrgDateTime)
		devices[i] = domain.ABMDevice{
			Serial:        serial,
			DeviceModel:   d.Attributes.DeviceModel,
			ProductFamily: d.Attributes.ProductFamily,
			Status:        d.Attributes.Status,
			AddedToOrg:    added,
		}
	}
	return devices, r.Links, nil
}

// ── auth ───────────────────────────────────────────────────────────────

func (c *Client) doAuthGET(ctx context.Context, u string) ([]byte, error) {
	token, err := c.accessTokenValue(ctx)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return body, fmt.Errorf("abm: %s -> HTTP %d: %s", u, resp.StatusCode, body)
	}
	return body, nil
}

// accessTokenValue returns the cached token if still fresh, otherwise refreshes.
func (c *Client) accessTokenValue(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.accessToken != "" && time.Until(c.tokenExpiry) > tokenRefreshMargin {
		t := c.accessToken
		c.mu.Unlock()
		return t, nil
	}
	c.mu.Unlock()

	return c.refreshToken(ctx)
}

func (c *Client) refreshToken(ctx context.Context) (string, error) {
	assertion, err := c.signAssertion()
	if err != nil {
		return "", fmt.Errorf("abm: sign jwt: %w", err)
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	form.Set("client_assertion", assertion)
	form.Set("scope", c.cfg.Scope)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("abm: token exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("abm: token HTTP %d: %s", resp.StatusCode, body)
	}
	var r struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("abm: decode token: %w", err)
	}

	c.mu.Lock()
	c.accessToken = r.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	c.mu.Unlock()
	return r.AccessToken, nil
}

func (c *Client) signAssertion() (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": c.cfg.TeamID,
		"sub": c.cfg.ClientID,
		"aud": audience,
		"iat": now.Unix(),
		"exp": now.Add(180 * time.Second).Unix(),
		"jti": newJTI(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = c.cfg.KeyID
	return tok.SignedString(c.priv)
}

func newJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func loadECKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block")
	}
	// PKCS#8 first (modern), then SEC1 ("EC PRIVATE KEY")
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ec, ok := k.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("PKCS#8 key is not EC")
		}
		return ec, nil
	}
	return x509.ParseECPrivateKey(block.Bytes)
}
