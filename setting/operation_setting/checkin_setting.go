package operation_setting

import (
	"math"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type CheckinTier struct {
	MinUsedCNY int     `json:"min_used_cny"`
	MinCNY     float64 `json:"min_cny"`
	MaxCNY     float64 `json:"max_cny"`
}

// CheckinSetting 签到功能配置
type CheckinSetting struct {
	Enabled      bool          `json:"enabled"`       // 是否启用签到功能
	MinQuota     int           `json:"min_quota"`     // 签到最小额度奖励
	MaxQuota     int           `json:"max_quota"`     // 签到最大额度奖励
	Tiered       bool          `json:"tiered"`        // 是否按累计使用额度分层发放
	Tiers        []CheckinTier `json:"tiers"`         // 分层签到奖励配置
	FallbackMode string        `json:"fallback_mode"` // 无匹配分层时的处理方式：legacy 或 none
}

var defaultCheckinTiers = []CheckinTier{
	{MinUsedCNY: 0, MinCNY: 0, MaxCNY: 0.2},
	{MinUsedCNY: 100, MinCNY: 0, MaxCNY: 1},
	{MinUsedCNY: 300, MinCNY: 0, MaxCNY: 2},
	{MinUsedCNY: 1000, MinCNY: 0, MaxCNY: 5},
}

// 默认配置
var checkinSetting = CheckinSetting{
	Enabled:      false, // 默认关闭
	MinQuota:     1000,  // 默认最小额度 1000 (约 0.002 USD)
	MaxQuota:     10000, // 默认最大额度 10000 (约 0.02 USD)
	Tiered:       false,
	Tiers:        defaultCheckinTiers,
	FallbackMode: "legacy",
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("checkin_setting", &checkinSetting)
}

// GetCheckinSetting 获取签到配置
func GetCheckinSetting() *CheckinSetting {
	return &checkinSetting
}

// IsCheckinEnabled 是否启用签到功能
func IsCheckinEnabled() bool {
	return checkinSetting.Enabled
}

// GetCheckinQuotaRange 获取签到额度范围
func GetCheckinQuotaRange() (min, max int) {
	return checkinSetting.MinQuota, checkinSetting.MaxQuota
}

func GetCheckinTiers() []CheckinTier {
	return normalizeCheckinTiers(checkinSetting.Tiers)
}

func IsCheckinTieredEnabled() bool {
	return checkinSetting.Tiered
}

func GetCheckinFallbackMode() string {
	switch checkinSetting.FallbackMode {
	case "none":
		return "none"
	default:
		return "legacy"
	}
}

func GetCheckinTierForUsedQuota(usedQuota int) (CheckinTier, bool) {
	tiers := GetCheckinTiers()
	if len(tiers) == 0 {
		return CheckinTier{}, false
	}
	usedCNY := QuotaToCNY(usedQuota)
	var matched CheckinTier
	ok := false
	for _, tier := range tiers {
		if usedCNY >= float64(tier.MinUsedCNY) {
			matched = tier
			ok = true
			continue
		}
		break
	}
	return matched, ok
}

func CNYToQuota(amountCNY float64) int {
	if amountCNY <= 0 {
		return 0
	}
	rate := USDExchangeRate
	if rate <= 0 {
		rate = 7.3
	}
	quota := amountCNY / rate * common.QuotaPerUnit
	if quota <= 0 {
		return 0
	}
	return int(math.Round(quota))
}

func QuotaToCNY(quota int) float64 {
	if quota <= 0 {
		return 0
	}
	rate := USDExchangeRate
	if rate <= 0 {
		rate = 7.3
	}
	return float64(quota) / common.QuotaPerUnit * rate
}

func normalizeCheckinTiers(tiers []CheckinTier) []CheckinTier {
	normalized := make([]CheckinTier, 0, len(tiers))
	for _, tier := range tiers {
		if tier.MinUsedCNY < 0 {
			tier.MinUsedCNY = 0
		}
		if tier.MinCNY < 0 {
			tier.MinCNY = 0
		}
		if tier.MaxCNY < 0 {
			tier.MaxCNY = 0
		}
		if tier.MaxCNY < tier.MinCNY {
			tier.MinCNY, tier.MaxCNY = tier.MaxCNY, tier.MinCNY
		}
		normalized = append(normalized, tier)
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return normalized[i].MinUsedCNY < normalized[j].MinUsedCNY
	})
	return normalized
}
