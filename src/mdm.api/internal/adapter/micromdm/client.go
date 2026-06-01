package micromdm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/anthropics/mdm-server/internal/domain"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth("micromdm", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

func (c *Client) ListDevices(ctx context.Context) ([]*domain.Device, error) {
	data, code, err := c.do(ctx, http.MethodPost, "/v1/devices", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("micromdm: list devices returned %d: %s", code, data)
	}
	var resp struct {
		Devices []struct {
			UDID             string `json:"udid"`
			SerialNumber     string `json:"serial_number"`
			DeviceName       string `json:"device_name"`
			Model            string `json:"model"`
			ModelName        string `json:"model_name"`
			OSVersion        string `json:"os_version"`
			LastSeen         string `json:"last_seen"`
			EnrollmentStatus bool   `json:"enrollment_status"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	devices := make([]*domain.Device, len(resp.Devices))
	for i, d := range resp.Devices {
		lastSeen := time.Now()
		if d.LastSeen != "" {
			if t, err := time.Parse(time.RFC3339, d.LastSeen); err == nil {
				lastSeen = t
			}
		}
		model := d.Model
		if model == "" {
			model = d.ModelName
		}
		status := "unenrolled"
		if d.EnrollmentStatus {
			status = "enrolled"
		}
		devices[i] = &domain.Device{
			UDID:             d.UDID,
			SerialNumber:     d.SerialNumber,
			DeviceName:       d.DeviceName,
			Model:            model,
			OSVersion:        d.OSVersion,
			LastSeen:         lastSeen,
			EnrollmentStatus: status,
		}
	}
	return devices, nil
}

func (c *Client) GetDevice(ctx context.Context, udid string) (*domain.Device, error) {
	data, code, err := c.do(ctx, http.MethodGet, "/v1/devices/"+udid, nil)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("micromdm: get device returned %d", code)
	}
	var d domain.Device
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (c *Client) SendCommand(ctx context.Context, payload map[string]interface{}) (*domain.CommandResult, error) {
	data, code, err := c.do(ctx, http.MethodPost, "/v1/commands", payload)
	if err != nil {
		return nil, err
	}
	// Parse command_uuid from MicroMDM response
	var resp struct {
		Payload struct {
			CommandUUID string `json:"command_uuid"`
		} `json:"payload"`
	}
	_ = json.Unmarshal(data, &resp)
	return &domain.CommandResult{
		CommandUUID: resp.Payload.CommandUUID,
		StatusCode:  code,
		RawResponse: string(data),
	}, nil
}

func (c *Client) SendPush(ctx context.Context, udid string) error {
	_, code, err := c.do(ctx, http.MethodGet, "/push/"+udid, nil)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("micromdm: push returned %d", code)
	}
	return nil
}

func (c *Client) ClearQueue(ctx context.Context, udid string) (*domain.CommandResult, error) {
	data, code, err := c.do(ctx, http.MethodDelete, "/v1/commands/"+udid, nil)
	if err != nil {
		return nil, err
	}
	return &domain.CommandResult{StatusCode: code, RawResponse: string(data)}, nil
}

func (c *Client) InspectQueue(ctx context.Context, udid string) (string, error) {
	data, code, err := c.do(ctx, http.MethodGet, "/v1/commands/"+udid, nil)
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("micromdm: inspect queue returned %d", code)
	}
	return string(data), nil
}

func (c *Client) SyncDEP(ctx context.Context) error {
	_, code, err := c.do(ctx, http.MethodPost, "/v1/dep/syncnow", nil)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("micromdm: dep sync returned %d", code)
	}
	return nil
}

// DefineDEPProfile sends a full DEP profile body to MicroMDM, which forwards
// it to Apple DEP. The template is an opaque JSON object (the same shape
// mdmctl apply dep-profiles consumes) — we merge `serials` into its `devices`
// field before sending. Returns the Apple-assigned profile UUID.
//
// Each call creates a NEW profile UUID on Apple's side and assigns the listed
// serials to it. Pre-existing assignments on other serials are NOT modified
// (the new profile UUID only attaches to the serials in this request).
func (c *Client) DefineDEPProfile(ctx context.Context, template map[string]interface{}, serials []string) (string, error) {
	if len(serials) == 0 {
		return "", fmt.Errorf("micromdm: DefineDEPProfile needs at least one serial")
	}
	// Defensive copy so we don't mutate the caller's template.
	body := make(map[string]interface{}, len(template)+1)
	for k, v := range template {
		body[k] = v
	}
	body["devices"] = serials

	data, code, err := c.do(ctx, http.MethodPut, "/v1/dep/profiles", body)
	if err != nil {
		return "", err
	}
	if code < 200 || code >= 300 {
		return "", fmt.Errorf("micromdm: define dep profile returned %d: %s", code, data)
	}
	// MicroMDM returns Apple's response verbatim, typically:
	//   { "profile_uuid": "...", "devices": { "<serial>": "ASSIGNED" } }
	var resp struct {
		ProfileUUID string                 `json:"profile_uuid"`
		Devices     map[string]interface{} `json:"devices"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("micromdm: decode define dep profile response: %w (body: %s)", err, data)
	}
	if resp.ProfileUUID == "" {
		return "", fmt.Errorf("micromdm: define dep profile returned no profile_uuid (body: %s)", data)
	}
	return resp.ProfileUUID, nil
}
