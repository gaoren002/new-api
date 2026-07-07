package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

type UpdateUserDataConsentRequest struct {
	Status string `json:"status"`
}

func UpdateUserDataConsent(c *gin.Context) {
	var req UpdateUserDataConsentRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}

	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status != dto.DataConsentStatusAccepted && status != dto.DataConsentStatusDeclined {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if !operation_setting.IsDataConsentEnabled() {
		common.ApiErrorT(c, "The data authorization agreement is not enabled")
		return
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	settings := user.GetSetting()
	settings.DataConsentStatus = status
	settings.DataConsentVersion = operation_setting.GetDataConsentAgreementVersion()
	settings.DataConsentUpdatedAt = common.GetTimestamp()
	// 只写 setting 列：不能让读取到的旧用户快照覆盖并发变更的额度、状态或分组。
	if err := model.UpdateUserSetting(user.Id, settings); err != nil {
		common.ApiErrorT(c, "Update failed")
		return
	}

	common.ApiSuccess(c, gin.H{
		"status":     settings.DataConsentStatus,
		"version":    settings.DataConsentVersion,
		"updated_at": settings.DataConsentUpdatedAt,
	})
}
