package controller

import (
	"log"
	"strings"

	"github.com/anthropics/mdm-server/internal/secretbox"
)

// newSettingsBox builds the encryptor for secrets stored by the settings
// pages from the raw SETTINGS_ENCRYPTION_KEY value. A missing or malformed key
// doesn't stop the server (these features are optional): the box is nil and
// the message says why, so the page can tell the admin what to fix. feature
// only labels the log line.
func newSettingsBox(encryptionKey, feature string) (*secretbox.Box, string) {
	if strings.TrimSpace(encryptionKey) == "" {
		log.Printf("[%s] SETTINGS_ENCRYPTION_KEY is not set: credential storage disabled", feature)
		return nil, "尚未設定 SETTINGS_ENCRYPTION_KEY，無法加密儲存憑證"
	}
	box, err := secretbox.NewFromString(encryptionKey)
	if err != nil {
		log.Printf("[%s] SETTINGS_ENCRYPTION_KEY is invalid: %v", feature, err)
		return nil, "SETTINGS_ENCRYPTION_KEY 格式不正確（需要 64 個 hex 字元，或 32 bytes 的 base64，可用 openssl rand -base64 32 產生）"
	}
	return box, ""
}
