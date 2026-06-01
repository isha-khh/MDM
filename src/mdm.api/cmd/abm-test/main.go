// abm-test is a one-off tool to verify that Apple Business Manager API
// credentials work: signs a client assertion JWT, exchanges it for an
// access token, then lists organization devices. Prints a summary so we
// know whether the API can be the foundation for fully-automated DEP
// device discovery + assignment.
//
// Required env:
//   ABM_KEY_PATH    path to the EC P-256 private key PEM (PKCS#8 or SEC1)
//   ABM_CLIENT_ID   the API account's client ID (usually "BUSINESSAPI.<UUID>")
//   ABM_KEY_ID      the key ID (KID) shown next to the public key in ABM
//
// Optional:
//   ABM_TEAM_ID     the team_id claim for `iss` (defaults to ABM_CLIENT_ID;
//                   in Apple's official sample these are the same value)
//   ABM_SCOPE       OAuth scope (default "business.api")
package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
)

const (
	tokenURL = "https://account.apple.com/auth/oauth2/v2/token"
	apiBase  = "https://api-business.apple.com"
	audience = "https://account.apple.com/auth/oauth2/v2/token"
)

func main() {
	// Load .env from cwd (same behaviour as the main server). Errors are
	// non-fatal — env vars from the shell still win.
	if err := godotenv.Load(); err != nil {
		log.Printf("note: no .env loaded (%v); reading from process env", err)
	}

	keyPath := os.Getenv("ABM_KEY_PATH")
	clientID := os.Getenv("ABM_CLIENT_ID")
	keyID := os.Getenv("ABM_KEY_ID")
	teamID := envOr("ABM_TEAM_ID", clientID) // Apple's sample uses same value for both
	scope := envOr("ABM_SCOPE", "business.api")

	missing := []string{}
	if keyPath == "" {
		missing = append(missing, "ABM_KEY_PATH")
	}
	if clientID == "" {
		missing = append(missing, "ABM_CLIENT_ID")
	}
	if keyID == "" {
		missing = append(missing, "ABM_KEY_ID")
	}
	if len(missing) > 0 {
		log.Fatalf("missing env: %s", strings.Join(missing, ", "))
	}

	fmt.Printf("• key path  = %s\n", keyPath)
	fmt.Printf("• client id = %s\n", clientID)
	fmt.Printf("• team id   = %s%s\n", teamID, ternary(teamID == clientID, " (= client id)", ""))
	fmt.Printf("• key id    = %s\n", keyID)
	fmt.Printf("• scope     = %s\n\n", scope)

	priv, err := loadECKey(keyPath)
	if err != nil {
		log.Fatalf("load key: %v", err)
	}

	assertion, err := signAssertion(priv, clientID, teamID, keyID)
	if err != nil {
		log.Fatalf("sign jwt: %v", err)
	}
	fmt.Printf("✓ signed client assertion (len=%d)\n", len(assertion))

	accessToken, expiresIn, err := exchangeToken(assertion, clientID, scope)
	if err != nil {
		log.Fatalf("token exchange: %v", err)
	}
	fmt.Printf("✓ access token granted, expires in %ds (len=%d)\n\n", expiresIn, len(accessToken))

	// ── /v1/orgDevices ─────────────────────────────────────────────
	devices, raw, err := listOrgDevices(accessToken)
	if err != nil {
		log.Fatalf("list orgDevices: %v", err)
	}
	fmt.Printf("✓ /v1/orgDevices returned %d device(s) (page 1)\n", len(devices))

	if len(devices) == 0 {
		fmt.Println("first 800 bytes of response (no devices parsed):")
		fmt.Println(string(raw[:min(len(raw), 800)]))
	} else {
		families := map[string]int{}
		statuses := map[string]int{}
		assigned, unassigned := 0, 0
		for _, d := range devices {
			fam, _ := d.Attributes["productFamily"].(string)
			if fam == "" {
				fam = "<unknown>"
			}
			families[fam]++
			if st, _ := d.Attributes["status"].(string); st != "" {
				statuses[st]++
			}
			if hasAssignedServer(d) {
				assigned++
			} else {
				unassigned++
			}
		}
		fmt.Println("\nby productFamily:")
		for fam, c := range families {
			fmt.Printf("  %-12s %d\n", fam, c)
		}
		fmt.Println("by status:")
		for st, c := range statuses {
			fmt.Printf("  %-12s %d\n", st, c)
		}
		fmt.Printf("assignedServer: %d assigned, %d unassigned\n", assigned, unassigned)

		fmt.Println("\nfirst 3 devices:")
		for i, d := range devices {
			if i >= 3 {
				break
			}
			attr := d.Attributes
			srv := "<none>"
			if rel, ok := d.Relationships["assignedServer"].(map[string]interface{}); ok {
				if data, ok := rel["data"].(map[string]interface{}); ok {
					if id, _ := data["id"].(string); id != "" {
						srv = id
					}
				}
			}
			fmt.Printf("  - serial=%v  family=%v  model=%v  status=%v  mdmServer=%s\n",
				attr["serialNumber"], attr["productFamily"], attr["deviceModel"], attr["status"], srv)
		}

		// Dump first device's raw JSON so we can see what shape Apple actually returns.
		if b, err := json.MarshalIndent(devices[0], "", "  "); err == nil {
			fmt.Println("\nraw JSON of first device (so we can see the real relationship shape):")
			fmt.Println(string(b))
		}
	}

	// Cross-reference with MicroMDM if creds are in env (same .env).
	mdmURL := os.Getenv("MICROMDM_URL")
	mdmKey := os.Getenv("MICROMDM_API_KEY")
	if mdmURL != "" && mdmKey != "" && len(devices) > 0 {
		fmt.Println()
		abmSerials := map[string]bool{}
		for _, d := range devices {
			if s, ok := d.Attributes["serialNumber"].(string); ok && s != "" {
				abmSerials[s] = true
			}
		}
		microSerials, err := fetchMicroMDMSerials(mdmURL, mdmKey)
		if err != nil {
			log.Printf("× MicroMDM cross-check: %v", err)
		} else {
			abmOnly := []string{}
			for s := range abmSerials {
				if !microSerials[s] {
					abmOnly = append(abmOnly, s)
				}
			}
			microOnly := []string{}
			for s := range microSerials {
				if !abmSerials[s] {
					microOnly = append(microOnly, s)
				}
			}
			fmt.Printf("✓ MicroMDM has %d devices; ABM has %d\n", len(microSerials), len(abmSerials))
			fmt.Printf("  in ABM but NOT in MicroMDM (= new devices to onboard): %d\n", len(abmOnly))
			for _, s := range abmOnly {
				fmt.Printf("    - %s\n", s)
			}
			fmt.Printf("  in MicroMDM but NOT in ABM (= released/stale): %d\n", len(microOnly))
			for _, s := range microOnly {
				fmt.Printf("    - %s\n", s)
			}
		}
	}

	// ── /v1/mdmServers ─────────────────────────────────────────────
	fmt.Println()
	servers, _, err := listMDMServers(accessToken)
	if err != nil {
		log.Printf("× list mdmServers: %v", err)
	} else {
		fmt.Printf("✓ /v1/mdmServers returned %d server(s)\n", len(servers))
		for _, s := range servers {
			fmt.Printf("  - id=%s  name=%v  type=%v\n", s.ID, s.Attributes["serverName"], s.Attributes["serverType"])
		}

		// For each server, list which devices are assigned to it. This is the
		// efficient way to learn assignments (one call per server, vs N calls).
		for _, s := range servers {
			serials, raw, err := listMDMServerDeviceSerials(accessToken, s.ID)
			if err != nil {
				log.Printf("× mdmServers/%s/devices: %v", s.ID, err)
				continue
			}
			fmt.Printf("\n✓ mdmServers/%s/devices returned %d serial(s) (page 1)\n", s.ID, len(serials))

			// Cross-check against the orgDevices set: which devices are NOT
			// assigned to this server? Those need POST /v1/orgDeviceActivities.
			assigned := map[string]bool{}
			for _, sn := range serials {
				assigned[sn] = true
			}
			unassignedToThisServer := []string{}
			for _, d := range devices {
				sn, _ := d.Attributes["serialNumber"].(string)
				if sn != "" && !assigned[sn] {
					unassignedToThisServer = append(unassignedToThisServer, sn)
				}
			}
			fmt.Printf("  in ABM but NOT assigned to this server: %d\n", len(unassignedToThisServer))
			for i, sn := range unassignedToThisServer {
				if i >= 10 {
					fmt.Printf("    ... (+%d more)\n", len(unassignedToThisServer)-i)
					break
				}
				fmt.Printf("    - %s\n", sn)
			}

			// Show first 200 bytes of raw response so we can see the shape
			// of the linkages response (id=serial, type=orgDevices probably).
			if len(serials) > 0 {
				snippet := raw
				if len(snippet) > 400 {
					snippet = snippet[:400]
				}
				fmt.Printf("  raw (first 400b): %s\n", snippet)
			}
		}
	}
}

func hasAssignedServer(d orgDevice) bool {
	rel, ok := d.Relationships["assignedServer"].(map[string]interface{})
	if !ok {
		return false
	}
	data, ok := rel["data"].(map[string]interface{})
	if !ok {
		return false
	}
	id, _ := data["id"].(string)
	return id != ""
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// ─── crypto + jwt ────────────────────────────────────────────────────

func loadECKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	// Try PKCS#8 first (modern), then fall back to SEC1 ("EC PRIVATE KEY").
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ec, ok := k.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("PKCS#8 key is not EC")
		}
		return ec, nil
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

func signAssertion(priv *ecdsa.PrivateKey, clientID, teamID, keyID string) (string, error) {
	// Per Apple's official Python sample:
	//   sub = client_id, iss = team_id (often the same value in practice).
	// exp may be up to 180 days from iat; we use 180 seconds — short-lived
	// assertions are safer and the access token returned is the long-lived one.
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": teamID,
		"sub": clientID,
		"aud": audience,
		"iat": now.Unix(),
		"exp": now.Add(180 * time.Second).Unix(),
		"jti": newJTI(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = keyID
	return tok.SignedString(priv)
}

func newJTI() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// ─── http ───────────────────────────────────────────────────────────

func exchangeToken(assertion, clientID, scope string) (string, int, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	form.Set("client_assertion", assertion)
	form.Set("scope", scope)

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	var r struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", 0, fmt.Errorf("decode: %w (body: %s)", err, body)
	}
	return r.AccessToken, r.ExpiresIn, nil
}

type orgDevice struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"`
	Attributes    map[string]interface{} `json:"attributes"`
	Relationships map[string]interface{} `json:"relationships"`
}

type mdmServer struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Attributes map[string]interface{} `json:"attributes"`
}

func listOrgDevices(token string) ([]orgDevice, []byte, error) {
	// Ask for the fields we actually need; productFamily is the key for Mac/iOS routing.
	// NOTE: Apple rejects ?include= on this endpoint (400 PARAMETER_ERROR.ILLEGAL),
	// so we rely on the default linkage shape inside relationships.assignedServer.
	url := apiBase + "/v1/orgDevices?limit=100" +
		"&fields%5BorgDevices%5D=serialNumber,productFamily,deviceModel,status,addedToOrgDateTime,assignedServer"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, body, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	var r struct {
		Data []orgDevice            `json:"data"`
		Meta map[string]interface{} `json:"meta"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, body, err
	}
	return r.Data, body, nil
}

// fetchMicroMDMSerials calls MicroMDM's /v1/devices with Basic Auth and
// returns a set of serial numbers it knows about.
func fetchMicroMDMSerials(baseURL, apiKey string) (map[string]bool, error) {
	req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/devices", strings.NewReader("{}"))
	req.SetBasicAuth("micromdm", apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("MicroMDM HTTP %d: %s", resp.StatusCode, body)
	}
	var r struct {
		Devices []struct {
			Serial string `json:"serial_number"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(r.Devices))
	for _, d := range r.Devices {
		if d.Serial != "" {
			out[d.Serial] = true
		}
	}
	return out, nil
}

// listMDMServerDeviceSerials returns the device IDs (= serials in linkage form)
// currently assigned to a specific MDM server in ABM. This avoids the N+1 problem
// of asking each orgDevice for its assignedServer relationship one at a time.
func listMDMServerDeviceSerials(token, serverID string) ([]string, []byte, error) {
	// Apple only allows GET_RELATIONSHIP (not GET_RELATED) for this relationship,
	// so we use /relationships/devices instead of /devices.
	url := apiBase + "/v1/mdmServers/" + serverID + "/relationships/devices?limit=100"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, body, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	// Response is a JSON:API linkages doc: { "data": [ { "id": "<serial>", "type": "orgDevices" }, ... ] }
	var r struct {
		Data []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, body, err
	}
	out := make([]string, 0, len(r.Data))
	for _, d := range r.Data {
		out = append(out, d.ID)
	}
	return out, body, nil
}

func listMDMServers(token string) ([]mdmServer, []byte, error) {
	url := apiBase + "/v1/mdmServers?fields%5BmdmServers%5D=serverName,serverType,createdDateTime,updatedDateTime"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, body, fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	var r struct {
		Data []mdmServer `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, body, err
	}
	return r.Data, body, nil
}

// ─── util ───────────────────────────────────────────────────────────

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
