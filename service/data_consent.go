package service

import (
	"math"
	"strings"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const dataConsentAppliedRatioKey = "data_consent_price_multiplier"

type DataConsentBillingInfo struct {
	Enabled          bool
	Status           string
	Authorized       bool
	Multiplier       float64
	AgreementVersion string
	UserVersion      string
}

func DataConsentStateForUserSetting(userSetting dto.UserSetting) DataConsentBillingInfo {
	agreementVersion := operation_setting.GetDataConsentAgreementVersion()
	info := DataConsentBillingInfo{
		Enabled:          operation_setting.IsDataConsentEnabled(),
		Status:           dto.DataConsentStatusDisabled,
		Multiplier:       1,
		AgreementVersion: agreementVersion,
		UserVersion:      strings.TrimSpace(userSetting.DataConsentVersion),
	}
	if !info.Enabled {
		return info
	}

	status := strings.ToLower(strings.TrimSpace(userSetting.DataConsentStatus))
	if userSetting.DataConsentVersion != agreementVersion {
		status = dto.DataConsentStatusUnset
	}
	switch status {
	case dto.DataConsentStatusAccepted:
		info.Status = dto.DataConsentStatusAccepted
		info.Authorized = true
		info.Multiplier = operation_setting.GetDataConsentAcceptedMultiplier()
	case dto.DataConsentStatusDeclined:
		info.Status = dto.DataConsentStatusDeclined
		info.Multiplier = operation_setting.GetDataConsentDeclinedMultiplier()
	default:
		info.Status = dto.DataConsentStatusUnset
		info.Multiplier = operation_setting.GetDataConsentDeclinedMultiplier()
	}
	return info
}

func DataConsentStateForRelayInfo(relayInfo *relaycommon.RelayInfo) DataConsentBillingInfo {
	if relayInfo == nil {
		return DataConsentStateForUserSetting(dto.UserSetting{})
	}
	return DataConsentStateForUserSetting(relayInfo.UserSetting)
}

func DataConsentStateForTaskBillingContext(billingContext *model.TaskBillingContext) DataConsentBillingInfo {
	if billingContext == nil {
		return DataConsentStateForUserSetting(dto.UserSetting{})
	}
	info := DataConsentBillingInfo{
		Enabled:          billingContext.DataConsentEnabled,
		Status:           billingContext.DataConsentStatus,
		Authorized:       billingContext.DataConsentAuthorized,
		Multiplier:       billingContext.DataConsentPriceMultiplier,
		AgreementVersion: billingContext.DataConsentAgreementVersion,
		UserVersion:      billingContext.DataConsentUserVersion,
	}
	if info.Status == "" {
		info.Status = dto.DataConsentStatusDisabled
	}
	if info.Multiplier <= 0 {
		info.Multiplier = 1
	}
	return info
}

func ApplyTaskDataConsentBillingContext(billingContext *model.TaskBillingContext, relayInfo *relaycommon.RelayInfo) {
	if billingContext == nil {
		return
	}
	info := DataConsentStateForRelayInfo(relayInfo)
	billingContext.DataConsentEnabled = info.Enabled
	billingContext.DataConsentStatus = info.Status
	billingContext.DataConsentAuthorized = info.Authorized
	billingContext.DataConsentPriceMultiplier = info.Multiplier
	billingContext.DataConsentAgreementVersion = info.AgreementVersion
	billingContext.DataConsentUserVersion = info.UserVersion
}

func ApplyDataConsentMultiplier(relayInfo *relaycommon.RelayInfo, quota int) int {
	return ApplyDataConsentMultiplierByInfo(quota, DataConsentStateForRelayInfo(relayInfo))
}

func ApplyDataConsentMultiplierToPriceData(relayInfo *relaycommon.RelayInfo) {
	if relayInfo == nil {
		return
	}
	info := DataConsentStateForRelayInfo(relayInfo)
	if !info.Enabled || info.Multiplier == 1 {
		return
	}
	if relayInfo.PriceData.QuotaToPreConsume != 0 {
		relayInfo.PriceData.QuotaToPreConsume = ApplyDataConsentMultiplierByInfo(relayInfo.PriceData.QuotaToPreConsume, info)
	}
	if relayInfo.PriceData.Quota != 0 {
		relayInfo.PriceData.Quota = ApplyDataConsentMultiplierByInfo(relayInfo.PriceData.Quota, info)
	}
}

func IsDataConsentAppliedRatio(key string) bool {
	return key == dataConsentAppliedRatioKey
}

func ApplyDataConsentMultiplierByInfo(quota int, info DataConsentBillingInfo) int {
	if !info.Enabled || quota == 0 || info.Multiplier == 1 {
		return quota
	}
	adjusted := int(math.Round(float64(quota) * info.Multiplier))
	if quota > 0 && adjusted == 0 && info.Multiplier > 0 {
		return 1
	}
	return adjusted
}
