package vpp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/anthropics/mdm-server/internal/domain"
)

type Client struct {
	sToken     string
	httpClient *http.Client
}

func NewClient(tokenPath string) (*Client, error) {
	if tokenPath == "" {
		return &Client{httpClient: &http.Client{Timeout: 30 * time.Second}}, nil
	}
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("vpp: read token: %w", err)
	}
	return &Client{
		sToken:     strings.TrimSpace(string(data)),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *Client) AssignLicense(ctx context.Context, adamID string, serialNumbers []string) (string, error) {
	return c.manageLicenses(ctx, adamID, serialNumbers, true)
}

func (c *Client) RevokeLicense(ctx context.Context, adamID string, serialNumbers []string) (string, error) {
	return c.manageLicenses(ctx, adamID, serialNumbers, false)
}

func (c *Client) manageLicenses(ctx context.Context, adamID string, serialNumbers []string, assign bool) (string, error) {
	if c.sToken == "" {
		return "", fmt.Errorf("vpp: no token configured")
	}
	payload := map[string]interface{}{
		"sToken":     c.sToken,
		"adamIdStr":  adamID,
	}
	if assign {
		payload["associateSerialNumbers"] = serialNumbers
	} else {
		payload["disassociateSerialNumbers"] = serialNumbers
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://vpp.itunes.apple.com/mdm/manageVPPLicensesByAdamIdSrv",
		bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vpp: HTTP %d: %s", resp.StatusCode, string(data))
	}

	// Validate Apple VPP response
	var result struct {
		Status       int    `json:"status"`
		ErrorNumber  int    `json:"errorNumber"`
		ErrorMessage string `json:"errorMessage"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("vpp: invalid response: %s", string(data))
	}
	if result.Status != 0 {
		return "", fmt.Errorf("vpp: error %d: %s", result.ErrorNumber, result.ErrorMessage)
	}

	return string(data), nil
}

// GetVPPAssets calls Apple's getVPPAssetsSrv to fetch the org's full VPP
// asset list (one entry per app/book the org has licenses for). Used to
// sync managed_apps.purchased_qty without requiring admin to type it in.
func (c *Client) GetVPPAssets(ctx context.Context) ([]domain.VPPAsset, error) {
	// main.go logs a warning and continues if NewClient fails, leaving us with
	// a typed-nil interface in the controller. Catch it here so we return a
	// proper error instead of nil-deref panicking.
	if c == nil {
		return nil, fmt.Errorf("vpp: client not initialised (VPP_TOKEN_PATH missing or file unreadable)")
	}
	if c.sToken == "" {
		return nil, fmt.Errorf("vpp: no token configured")
	}
	body, _ := json.Marshal(map[string]interface{}{
		"sToken":           c.sToken,
		"includeLicenseCounts": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://vpp.itunes.apple.com/mdm/getVPPAssetsSrv",
		bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vpp: getVPPAssetsSrv HTTP %d: %s", resp.StatusCode, string(data))
	}

	var r struct {
		Status       int    `json:"status"`
		ErrorNumber  int    `json:"errorNumber"`
		ErrorMessage string `json:"errorMessage"`
		Assets       []struct {
			AdamIDStr       string `json:"adamIdStr"`
			ProductTypeName string `json:"productTypeName"`
			TotalCount      int    `json:"totalCount"`
			AssignedCount   int    `json:"assignedCount"`
			RetiredCount    int    `json:"retiredCount"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("vpp: getVPPAssetsSrv decode: %w (body: %s)", err, data)
	}
	if r.Status != 0 {
		return nil, fmt.Errorf("vpp: getVPPAssetsSrv error %d: %s", r.ErrorNumber, r.ErrorMessage)
	}

	out := make([]domain.VPPAsset, len(r.Assets))
	for i, a := range r.Assets {
		out[i] = domain.VPPAsset{
			AdamID:          a.AdamIDStr,
			ProductTypeName: a.ProductTypeName,
			TotalCount:      a.TotalCount,
			AssignedCount:   a.AssignedCount,
			RetiredCount:    a.RetiredCount,
		}
	}
	return out, nil
}
