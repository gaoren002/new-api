package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const CurrentDataConsentAgreementVersion = "v1"

const DefaultDataConsentAgreementContent = `《SuperAPI 数据授权协议》

为了持续改善 SuperAPI 的产品体验、服务质量和国产模型能力，我们会在你明确同意后，依据《中华人民共和国个人信息保护法》《中华人民共和国数据安全法》《中华人民共和国网络安全法》等相关法律法规，在合法、正当、必要和诚信原则下处理与你使用服务相关的数据。

一、授权范围
你同意 SuperAPI 收集和处理你在使用服务过程中产生的对话请求内容、模型返回内容、调用时间、使用模型、消耗额度、错误信息、请求标识等服务运行数据。我们不会主动要求你提供身份证件号码、生物识别信息、金融账户、精确定位、医疗健康等敏感个人信息；请不要在对话中提交无关敏感信息。

二、使用目的
相关数据将用于服务稳定性分析、问题排查、用户体验优化、计费核对、安全风控、模型效果评估，以及在脱敏、去标识化或匿名化处理后用于改善国产模型，包括但不限于模型评测、能力优化、训练数据质量分析和安全对齐改进。

三、处理原则与安全措施
我们将尽量控制数据处理范围，采取访问控制、权限分级、传输加密、存储保护、日志审计等合理措施保护数据安全。未经你的单独同意或法律法规允许，我们不会将可识别到你的个人信息对外提供。

四、你的权利
你可以在个人设置中接受、拒绝或撤回本授权。拒绝、未签署或撤回授权后，我们不会继续收集你的对话内容用于上述改进目的，但不影响为履行服务、计费、安全和合规所必需的数据处理。你也可以依法行使查询、复制、更正、删除等个人信息权益。

五、价格影响
在管理员启用本功能后，接受授权的请求按 0.95 倍价格计费；拒绝或未签署授权的请求按 1.0 倍价格计费。管理员调整倍率或协议版本后，以页面展示为准。`

type DataConsentSetting struct {
	Enabled            bool    `json:"enabled"`
	AcceptedMultiplier float64 `json:"accepted_multiplier"`
	DeclinedMultiplier float64 `json:"declined_multiplier"`
	AgreementVersion   string  `json:"agreement_version"`
	AgreementContent   string  `json:"agreement_content"`
}

var dataConsentSetting = DataConsentSetting{
	Enabled:            false,
	AcceptedMultiplier: 0.95,
	DeclinedMultiplier: 1.0,
	AgreementVersion:   CurrentDataConsentAgreementVersion,
	AgreementContent:   DefaultDataConsentAgreementContent,
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
		return 1.0
	}
	return dataConsentSetting.DeclinedMultiplier
}

func GetDataConsentAgreementContent() string {
	content := strings.TrimSpace(dataConsentSetting.AgreementContent)
	if content == "" {
		return DefaultDataConsentAgreementContent
	}
	return content
}
