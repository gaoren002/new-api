package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
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
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status != dto.DataConsentStatusAccepted && status != dto.DataConsentStatusDeclined {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !operation_setting.IsDataConsentEnabled() {
		common.ApiErrorMsg(c, "数据授权协议功能未开启")
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
	user.SetSetting(settings)
	if err := user.Update(false); err != nil {
		common.ApiErrorI18n(c, i18n.MsgUpdateFailed)
		return
	}

	common.ApiSuccess(c, gin.H{
		"status":     settings.DataConsentStatus,
		"version":    settings.DataConsentVersion,
		"updated_at": settings.DataConsentUpdatedAt,
	})
}
