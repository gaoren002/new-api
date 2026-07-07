package operation_setting

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestCheckinTierForUsedQuota(t *testing.T) {
	oldTiers := checkinSetting.Tiers
	oldRate := USDExchangeRate
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := GetGeneralSetting().QuotaDisplayType
	defer func() {
		checkinSetting.Tiers = oldTiers
		USDExchangeRate = oldRate
		common.QuotaPerUnit = oldQuotaPerUnit
		GetGeneralSetting().QuotaDisplayType = oldDisplayType
	}()

	USDExchangeRate = 10
	common.QuotaPerUnit = 1000
	GetGeneralSetting().QuotaDisplayType = QuotaDisplayTypeCNY
	checkinSetting.Tiers = []CheckinTier{
		{MinUsedCNY: 300, MinCNY: 0, MaxCNY: 2},
		{MinUsedCNY: 0, MinCNY: 0, MaxCNY: 0.2},
		{MinUsedCNY: 100, MinCNY: 0, MaxCNY: 1},
	}

	tier, ok := GetCheckinTierForUsedQuota(20_000) // 20 USD * 10 CNY = 200 CNY
	if !ok {
		t.Fatal("expected matching tier")
	}
	if tier.MinUsedCNY != 100 || tier.MaxCNY != 1 {
		t.Fatalf("tier = %+v, want min_used_cny=100 max_cny=1", tier)
	}

	tier, ok = GetCheckinTierForUsedQuota(40_000) // 400 CNY
	if !ok {
		t.Fatal("expected matching tier")
	}
	if tier.MinUsedCNY != 300 || tier.MaxCNY != 2 {
		t.Fatalf("tier = %+v, want min_used_cny=300 max_cny=2", tier)
	}
}

func TestCheckinTierForUsedQuotaUsesDisplayCurrency(t *testing.T) {
	oldTiers := checkinSetting.Tiers
	oldRate := USDExchangeRate
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := GetGeneralSetting().QuotaDisplayType
	defer func() {
		checkinSetting.Tiers = oldTiers
		USDExchangeRate = oldRate
		common.QuotaPerUnit = oldQuotaPerUnit
		GetGeneralSetting().QuotaDisplayType = oldDisplayType
	}()

	USDExchangeRate = 10
	common.QuotaPerUnit = 1000
	GetGeneralSetting().QuotaDisplayType = QuotaDisplayTypeUSD
	checkinSetting.Tiers = []CheckinTier{
		{MinUsedCNY: 300, MinCNY: 0, MaxCNY: 2},
		{MinUsedCNY: 0, MinCNY: 0, MaxCNY: 0.2},
		{MinUsedCNY: 100, MinCNY: 0, MaxCNY: 1},
	}

	tier, ok := GetCheckinTierForUsedQuota(200_000) // 200 USD, not 2000 CNY.
	if !ok {
		t.Fatal("expected matching tier")
	}
	if tier.MinUsedCNY != 100 || tier.MaxCNY != 1 {
		t.Fatalf("tier = %+v, want min_used_cny=100 max_cny=1", tier)
	}

	GetGeneralSetting().QuotaDisplayType = QuotaDisplayTypeTokens
	tier, ok = GetCheckinTierForUsedQuota(350)
	if !ok {
		t.Fatal("expected matching tier")
	}
	if tier.MinUsedCNY != 300 || tier.MaxCNY != 2 {
		t.Fatalf("tier = %+v, want min_used_cny=300 max_cny=2", tier)
	}
}

func TestCheckinCNYQuotaConversion(t *testing.T) {
	oldRate := USDExchangeRate
	oldQuotaPerUnit := common.QuotaPerUnit
	defer func() {
		USDExchangeRate = oldRate
		common.QuotaPerUnit = oldQuotaPerUnit
	}()

	USDExchangeRate = 7.3
	common.QuotaPerUnit = 500000

	quota := CNYToQuota(0.2)
	want := int(math.Round(0.2 / 7.3 * 500000))
	if quota != want {
		t.Fatalf("CNYToQuota(0.2) = %d, want %d", quota, want)
	}

	gotCNY := QuotaToCNY(CNYToQuota(5))
	if math.Abs(gotCNY-5) > 0.0001 {
		t.Fatalf("round trip CNY = %f, want about 5", gotCNY)
	}
}

func TestCheckinDisplayAmountQuotaConversion(t *testing.T) {
	oldRate := USDExchangeRate
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := GetGeneralSetting().QuotaDisplayType
	oldCustomRate := GetGeneralSetting().CustomCurrencyExchangeRate
	defer func() {
		USDExchangeRate = oldRate
		common.QuotaPerUnit = oldQuotaPerUnit
		GetGeneralSetting().QuotaDisplayType = oldDisplayType
		GetGeneralSetting().CustomCurrencyExchangeRate = oldCustomRate
	}()

	USDExchangeRate = 7.3
	common.QuotaPerUnit = 500000
	GetGeneralSetting().QuotaDisplayType = QuotaDisplayTypeUSD

	minQuota := DisplayAmountToQuota(1)
	maxQuota := DisplayAmountToQuota(5)
	if minQuota != 500000 || maxQuota != 2500000 {
		t.Fatalf("USD display reward quota range = %d..%d, want 500000..2500000", minQuota, maxQuota)
	}
	if got := QuotaToDisplayAmount(2500000); math.Abs(got-5) > 0.0001 {
		t.Fatalf("QuotaToDisplayAmount USD = %f, want about 5", got)
	}

	GetGeneralSetting().QuotaDisplayType = QuotaDisplayTypeCNY
	wantCNY := int(math.Round(1 / 7.3 * 500000))
	if got := DisplayAmountToQuota(1); got != wantCNY {
		t.Fatalf("DisplayAmountToQuota CNY = %d, want %d", got, wantCNY)
	}

	GetGeneralSetting().QuotaDisplayType = QuotaDisplayTypeCustom
	GetGeneralSetting().CustomCurrencyExchangeRate = 2
	if got := DisplayAmountToQuota(4); got != 1000000 {
		t.Fatalf("DisplayAmountToQuota custom = %d, want 1000000", got)
	}

	GetGeneralSetting().QuotaDisplayType = QuotaDisplayTypeTokens
	if got := DisplayAmountToQuota(123.6); got != 124 {
		t.Fatalf("DisplayAmountToQuota tokens = %d, want 124", got)
	}
}
