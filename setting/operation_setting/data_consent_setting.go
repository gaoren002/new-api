package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const CurrentDataConsentAgreementVersion = "v1"

type DataConsentSetting struct {
	Enabled            bool    `json:"enabled"`
	AcceptedMultiplier float64 `json:"accepted_multiplier"`
	DeclinedMultiplier float64 `json:"declined_multiplier"`
	AgreementVersion   string  `json:"agreement_version"`
}

var dataConsentSetting = DataConsentSetting{
	Enabled:            false,
	AcceptedMultiplier: 0.95,
	DeclinedMultiplier: 1.05,
	AgreementVersion:   CurrentDataConsentAgreementVersion,
}

func init() {
	config.GlobalConfig.Register("data_consent", &dataConsentSetting)
}

func GetDataConsentSetting() *DataConsentSetting {
	return &dataConsentSetting
}

func IsDataConsentEnabled() bool {
	return dataConsentSetting.Enabled
}

func GetDataConsentAgreementVersion() string {
	version := strings.TrimSpace(dataConsentSetting.AgreementVersion)
	if version == "" {
		return CurrentDataConsentAgreementVersion
	}
	return version
}

func GetDataConsentAcceptedMultiplier() float64 {
	if dataConsentSetting.AcceptedMultiplier <= 0 {
		return 0.95
	}
	return dataConsentSetting.AcceptedMultiplier
}

func GetDataConsentDeclinedMultiplier() float64 {
	if dataConsentSetting.DeclinedMultiplier <= 0 {
		return 1.05
	}
	return dataConsentSetting.DeclinedMultiplier
}
