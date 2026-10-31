package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMonitorLinkReachesUsersOnlyAsAnHTTPAddress(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	previousDB, previousRedis := model.DB, common.RedisEnabled
	model.DB, common.RedisEnabled = db, false
	common.OptionMapRWMutex.Lock()
	previousMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	setting := operation_setting.GetGeneralSetting()
	previousLink := setting.MonitorLink
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousMap
		common.OptionMapRWMutex.Unlock()
		setting.MonitorLink = previousLink
	})

	const current = "https://status.example.com/p/demo?embed=1"
	tests := []struct {
		name, value string
		saved       bool
	}{
		{"https page with a query", "https://relay.example.com/p/team?embed=1", true},
		{"plain http page with a port", "http://10.0.0.8:8080/", true},
		{"empty value hides the entry", "", true},
		{"script link", "javascript:alert(1)", false},
		{"relative path", "/status", false},
		{"other scheme", "ftp://status.example.com/", false},
		{"scheme without a host", "https:///status", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setting.MonitorLink = current
			body, err := common.Marshal(OptionUpdateRequest{Key: operation_setting.MonitorLinkOptionKey, Value: test.value})
			require.NoError(t, err)
			update := httptest.NewRecorder()
			updateContext, _ := gin.CreateTestContext(update)
			updateContext.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(string(body)))

			UpdateOption(updateContext)

			advertised := current
			if test.saved {
				assert.Contains(t, update.Body.String(), `"success":true`)
				advertised = test.value
			} else {
				assert.Contains(t, update.Body.String(), `"message_key":"Provide a valid URL starting with http:// or https://"`)
			}
			status := httptest.NewRecorder()
			statusContext, _ := gin.CreateTestContext(status)
			statusContext.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
			GetStatus(statusContext)
			var payload struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, common.Unmarshal(status.Body.Bytes(), &payload))
			assert.Equal(t, advertised, payload.Data["monitor_link"])
		})
	}
}
