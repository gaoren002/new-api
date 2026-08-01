package service

import (
	"crypto/hmac"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const promptAuditDeleteConfirmationTTL = 5 * time.Minute

type promptAuditDeleteConfirmationPayload struct {
	AdminID       int    `json:"admin_id"`
	FilterHash    string `json:"filter_hash"`
	SnapshotMaxID int64  `json:"snapshot_max_id"`
	ExpiresAt     int64  `json:"expires_at"`
}

func NewPromptAuditDeleteConfirmation(adminID int, filterHash string, snapshotMaxID int64) (token string, expiresAt time.Time, err error) {
	expiresAt = time.Now().UTC().Add(promptAuditDeleteConfirmationTTL)
	payload := promptAuditDeleteConfirmationPayload{
		AdminID: adminID, FilterHash: strings.ToLower(strings.TrimSpace(filterHash)),
		SnapshotMaxID: snapshotMaxID, ExpiresAt: expiresAt.Unix(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", time.Time{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	return encoded + "." + common.GenerateHMAC(encoded), expiresAt, nil
}

func ValidatePromptAuditDeleteConfirmation(token string, adminID int, filterHash string, snapshotMaxID int64) error {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 || !hmac.Equal([]byte(parts[1]), []byte(common.GenerateHMAC(parts[0]))) {
		return errors.New("prompt audit deletion confirmation is invalid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errors.New("prompt audit deletion confirmation is invalid")
	}
	var payload promptAuditDeleteConfirmationPayload
	if json.Unmarshal(raw, &payload) != nil || payload.ExpiresAt < time.Now().UTC().Unix() {
		return errors.New("prompt audit deletion confirmation is invalid or expired")
	}
	if payload.AdminID != adminID || payload.SnapshotMaxID != snapshotMaxID || !strings.EqualFold(payload.FilterHash, strings.TrimSpace(filterHash)) {
		return errors.New("prompt audit deletion confirmation does not match the request")
	}
	return nil
}
