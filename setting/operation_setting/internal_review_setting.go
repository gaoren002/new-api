package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type InternalReviewSetting struct {
	Enabled        bool   `json:"enabled"`
	Endpoint       string `json:"endpoint"`
	BearerToken    string `json:"bearer_token"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	FailClosed     bool   `json:"fail_closed"`
	Scope          string `json:"scope"`
	ModelFilter    string `json:"model_filter"`
}

var internalReviewSetting = InternalReviewSetting{
	Enabled:        false,
	Endpoint:       "",
	BearerToken:    "",
	TimeoutSeconds: 10,
	FailClosed:     true,
	Scope:          "text",
	ModelFilter:    "",
}

func init() {
	config.GlobalConfig.Register("internal_review", &internalReviewSetting)
}

func GetInternalReviewSetting() *InternalReviewSetting {
	return &internalReviewSetting
}

func IsInternalReviewEnabled() bool {
	return internalReviewSetting.Enabled
}

func GetInternalReviewEndpoint() string {
	return strings.TrimSpace(internalReviewSetting.Endpoint)
}

func GetInternalReviewBearerToken() string {
	return strings.TrimSpace(internalReviewSetting.BearerToken)
}

func GetInternalReviewTimeoutSeconds() int {
	if internalReviewSetting.TimeoutSeconds <= 0 {
		return 10
	}
	if internalReviewSetting.TimeoutSeconds > 120 {
		return 120
	}
	return internalReviewSetting.TimeoutSeconds
}

func ShouldInternalReviewFailClosed() bool {
	return internalReviewSetting.FailClosed
}

func GetInternalReviewScopes() map[string]bool {
	raw := strings.TrimSpace(internalReviewSetting.Scope)
	scopes := make(map[string]bool)
	for _, item := range strings.Split(raw, ",") {
		scope := strings.ToLower(strings.TrimSpace(item))
		if scope == "" {
			continue
		}
		scopes[scope] = true
	}
	return scopes
}

func IsInternalReviewScopeEnabled(scope string) bool {
	scopes := GetInternalReviewScopes()
	if scopes["all"] {
		return true
	}
	return scopes[strings.ToLower(strings.TrimSpace(scope))]
}

func GetInternalReviewModelFilter() string {
	return strings.TrimSpace(internalReviewSetting.ModelFilter)
}
