package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/mdm-server/internal/domain"
	"github.com/anthropics/mdm-server/internal/port"
)

type WebhookHandler struct {
	broker  port.EventBroker
	devices port.DeviceRepository
	mdm     port.MicroMDMClient
}

func NewWebhookHandler(broker port.EventBroker, devices port.DeviceRepository, mdm port.MicroMDMClient) *WebhookHandler {
	return &WebhookHandler{broker: broker, devices: devices, mdm: mdm}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body error", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	h.ProcessEvent(raw)
	w.WriteHeader(http.StatusOK)
}

func (h *WebhookHandler) ProcessEvent(raw map[string]interface{}) {
	event := &domain.MDMEvent{
		ID:        time.Now().Format("20060102150405.000"),
		Timestamp: time.Now(),
	}

	if ack, ok := raw["acknowledge_event"]; ok {
		event.EventType = "acknowledge"
		if m, ok := ack.(map[string]interface{}); ok {
			event.UDID, _ = m["udid"].(string)
			event.CommandUUID, _ = m["command_uuid"].(string)
			event.Status, _ = m["status"].(string)
			if rawB64, ok := m["raw_payload"].(string); ok {
				event.RawPayload = rawB64
				if event.Status == "Acknowledged" {
					h.parseAndStoreResponse(event.UDID, rawB64)
				}
			}
		}
	} else if checkin, ok := raw["checkin_event"]; ok {
		event.EventType = "checkin"
		if m, ok := checkin.(map[string]interface{}); ok {
			event.UDID, _ = m["udid"].(string)
			serial, _ := m["serial_number"].(string)
			h.handleCheckin(event.UDID, serial)
		}
	}

	log.Printf("[webhook] event=%s udid=%s cmd=%s status=%s", event.EventType, event.UDID, event.CommandUUID, event.Status)
	h.broker.Publish(event)
}

// handleCheckin records a device that checks in. Without this, a freshly
// enrolled device never lands in the DB until someone triggers a manual sync.
// For a device we've never seen, it also queries full DeviceInformation so the
// record is populated automatically (the response arrives via acknowledge_event
// and is stored by parseAndStoreResponse).
func (h *WebhookHandler) handleCheckin(udid, serial string) {
	if udid == "" || h.devices == nil {
		return
	}

	_, err := h.devices.GetByUDID(context.Background(), udid)
	isNew := err != nil

	if err := h.devices.Upsert(context.Background(), &domain.Device{
		UDID:             udid,
		SerialNumber:     serial,
		LastSeen:         time.Now(),
		EnrollmentStatus: "enrolled",
	}); err != nil {
		log.Printf("[webhook] checkin upsert %s: %v", udid, err)
		return
	}

	if isNew && h.mdm != nil {
		log.Printf("[webhook] new device checked in: udid=%s serial=%s — querying DeviceInformation", udid, serial)
		go func() {
			payload := map[string]interface{}{
				"udid":         udid,
				"request_type": "DeviceInformation",
				"queries":      deviceInfoQueries(),
			}
			if _, err := h.mdm.SendCommand(context.Background(), payload); err != nil {
				log.Printf("[webhook] checkin device info query %s: %v", udid, err)
				return
			}
			_ = h.mdm.SendPush(context.Background(), udid)
		}()
	}
}

// parseAndStoreResponse decodes base64 plist XML and stores structured data in DB.
func (h *WebhookHandler) parseAndStoreResponse(udid, rawB64 string) {
	if udid == "" || rawB64 == "" || h.devices == nil {
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		return
	}

	xmlStr := string(decoded)

	device := &domain.Device{
		UDID:             udid,
		LastSeen:         time.Now(),
		EnrollmentStatus: "enrolled",
		BatteryLevel:     -1, // -1 means don't update
		Details:          make(map[string]interface{}),
	}

	updated := false

	// --- DeviceInformation (QueryResponses) ---
	if strings.Contains(xmlStr, "<key>QueryResponses</key>") {
		device.DeviceName = extractPlistValue(xmlStr, "DeviceName")
		if v := extractPlistValue(xmlStr, "ModelName"); v != "" {
			device.Model = v
		} else {
			device.Model = extractPlistValue(xmlStr, "Model")
		}
		device.OSVersion = extractPlistValue(xmlStr, "OSVersion")
		device.SerialNumber = extractPlistValue(xmlStr, "SerialNumber")
		device.IsSupervised = extractPlistBool(xmlStr, "IsSupervised")
		device.IsLostMode = extractPlistBool(xmlStr, "IsMDMLostModeEnabled")

		// Store full query responses in details
		device.Details["device_info"] = map[string]interface{}{
			"device_name":    device.DeviceName,
			"model":          device.Model,
			"os_version":     device.OSVersion,
			"serial_number":  device.SerialNumber,
			"build_version":  extractPlistValue(xmlStr, "BuildVersion"),
			"product_name":   extractPlistValue(xmlStr, "ProductName"),
			"wifi_mac":       extractPlistValue(xmlStr, "WiFiMAC"),
			"bluetooth_mac":  extractPlistValue(xmlStr, "BluetoothMAC"),
			"is_supervised":  device.IsSupervised,
			"is_lost_mode":   device.IsLostMode,
			"updated_at":     time.Now().Format(time.RFC3339),
		}
		updated = true
	}

	// --- InstalledApplicationList ---
	if strings.Contains(xmlStr, "<key>InstalledApplicationList</key>") {
		device.Details["installed_apps_raw"] = rawB64
		device.Details["installed_apps_updated"] = time.Now().Format(time.RFC3339)
		updated = true
	}

	// --- ProfileList ---
	if strings.Contains(xmlStr, "<key>ProfileList</key>") {
		device.Details["profiles_raw"] = rawB64
		device.Details["profiles_updated"] = time.Now().Format(time.RFC3339)
		updated = true
	}

	// --- SecurityInfo ---
	if strings.Contains(xmlStr, "<key>SecurityInfo</key>") {
		device.Details["security_raw"] = rawB64
		device.Details["security_updated"] = time.Now().Format(time.RFC3339)
		updated = true
	}

	// --- CertificateList ---
	if strings.Contains(xmlStr, "<key>CertificateList</key>") {
		device.Details["certs_raw"] = rawB64
		device.Details["certs_updated"] = time.Now().Format(time.RFC3339)
		updated = true
	}

	// --- AvailableOSUpdates ---
	if strings.Contains(xmlStr, "<key>AvailableOSUpdates</key>") {
		device.Details["updates_raw"] = rawB64
		device.Details["updates_updated"] = time.Now().Format(time.RFC3339)
		updated = true
	}

	// --- DeviceLocation (Lost Mode location response) ---
	if strings.Contains(xmlStr, "<key>Latitude</key>") && strings.Contains(xmlStr, "<key>Longitude</key>") {
		lat := extractPlistRealValue(xmlStr, "Latitude")
		lng := extractPlistRealValue(xmlStr, "Longitude")
		device.Details["device_location"] = map[string]interface{}{
			"latitude":   lat,
			"longitude":  lng,
			"updated_at": time.Now().Format(time.RFC3339),
		}
		updated = true
		log.Printf("[webhook] device %s location: lat=%s lng=%s", udid, lat, lng)
	}

	// --- EnableLostMode / DisableLostMode ---
	// Update is_lost_mode directly (Upsert doesn't cover this without details)
	if strings.Contains(xmlStr, "<string>EnableLostMode</string>") {
		h.setLostMode(udid, true)
	}
	if strings.Contains(xmlStr, "<string>DisableLostMode</string>") {
		h.setLostMode(udid, false)
	}

	if !updated {
		return
	}

	if err := h.devices.Upsert(context.Background(), device); err != nil {
		log.Printf("[webhook] device update %s: %v", udid, err)
	} else {
		log.Printf("[webhook] device %s stored: name=%s model=%s os=%s details_keys=%d",
			udid, device.DeviceName, device.Model, device.OSVersion, len(device.Details))
	}
}

func (h *WebhookHandler) setLostMode(udid string, enabled bool) {
	if err := h.devices.SetLostMode(context.Background(), udid, enabled); err != nil {
		log.Printf("[webhook] set lost mode %s=%v: %v", udid, enabled, err)
	} else {
		log.Printf("[webhook] device %s lost_mode=%v", udid, enabled)
	}
}

// extractPlistValue finds <key>name</key><string>value</string> in plist XML.
func extractPlistValue(xml, key string) string {
	keyTag := "<key>" + key + "</key>"
	pos := strings.Index(xml, keyTag)
	if pos < 0 {
		return ""
	}
	after := xml[pos+len(keyTag):]
	sStart := strings.Index(after, "<string>")
	sEnd := strings.Index(after, "</string>")
	if sStart >= 0 && sEnd > sStart {
		return after[sStart+8 : sEnd]
	}
	return ""
}

// extractPlistBool finds <key>name</key> followed by <true/> or <false/>.
func extractPlistBool(xml, key string) bool {
	keyTag := "<key>" + key + "</key>"
	pos := strings.Index(xml, keyTag)
	if pos < 0 {
		return false
	}
	after := xml[pos+len(keyTag):]
	// Skip whitespace
	trimmed := strings.TrimSpace(after)
	return strings.HasPrefix(trimmed, "<true")
}

// extractPlistRealValue finds <key>name</key><real>value</real> in plist XML.
func extractPlistRealValue(xml, key string) string {
	keyTag := "<key>" + key + "</key>"
	pos := strings.Index(xml, keyTag)
	if pos < 0 {
		return ""
	}
	after := xml[pos+len(keyTag):]
	sStart := strings.Index(after, "<real>")
	sEnd := strings.Index(after, "</real>")
	if sStart >= 0 && sEnd > sStart {
		return after[sStart+6 : sEnd]
	}
	return ""
}
