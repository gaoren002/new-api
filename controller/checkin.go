package controller

import (
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func buildCheckinTierProgress(userId int) (gin.H, any) {
	usedQuota, err := model.GetUserUsedQuota(userId)
	if err != nil {
		return nil, nil
	}

	tiers := operation_setting.GetCheckinTiers()
	usedDisplayAmount := operation_setting.QuotaToDisplayAmount(usedQuota)

	var currentTier any
	currentRewardMinQuota := 0
	currentRewardMaxQuota := 0
	currentTierMinUsedQuota := 0
	if tier, ok := operation_setting.GetCheckinTierForUsedQuota(usedQuota); ok {
		currentTier = tier
		currentRewardMinQuota = operation_setting.DisplayAmountToQuota(tier.MinCNY)
		currentRewardMaxQuota = operation_setting.DisplayAmountToQuota(tier.MaxCNY)
		currentTierMinUsedQuota = operation_setting.DisplayAmountToQuota(float64(tier.MinUsedCNY))
	}

	var nextTier any
	nextRewardMinQuota := 0
	nextRewardMaxQuota := 0
	nextTierMinUsedQuota := 0
	amountToNextTierDisplay := 0.0
	amountToNextTierQuota := 0
	for _, tier := range tiers {
		if usedDisplayAmount < float64(tier.MinUsedCNY) {
			nextTier = tier
			nextRewardMinQuota = operation_setting.DisplayAmountToQuota(tier.MinCNY)
			nextRewardMaxQuota = operation_setting.DisplayAmountToQuota(tier.MaxCNY)
			nextTierMinUsedQuota = operation_setting.DisplayAmountToQuota(float64(tier.MinUsedCNY))
			amountToNextTierDisplay = math.Max(0, float64(tier.MinUsedCNY)-usedDisplayAmount)
			amountToNextTierQuota = operation_setting.DisplayAmountToQuota(amountToNextTierDisplay)
			break
		}
	}

	return gin.H{
		"used_quota":                         usedQuota,
		"used_display_amount":                usedDisplayAmount,
		"current_tier":                       currentTier,
		"current_tier_min_used_quota":        currentTierMinUsedQuota,
		"current_reward_min_quota":           currentRewardMinQuota,
		"current_reward_max_quota":           currentRewardMaxQuota,
		"next_tier":                          nextTier,
		"next_tier_min_used_quota":           nextTierMinUsedQuota,
		"next_reward_min_quota":              nextRewardMinQuota,
		"next_reward_max_quota":              nextRewardMaxQuota,
		"amount_to_next_tier_display_amount": amountToNextTierDisplay,
		"amount_to_next_tier_quota":          amountToNextTierQuota,
	}, currentTier
}

// GetCheckinStatus 获取用户签到状态和历史记录
func GetCheckinStatus(c *gin.Context) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		common.ApiErrorMsg(c, "签到功能未启用")
		return
	}
	userId := c.GetInt("id")
	// 获取月份参数，默认为当前月份
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))

	stats, err := model.GetUserCheckinStats(userId, month)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	var matchedTier any
	var tierProgress any
	if setting.Tiered {
		tierProgress, matchedTier = buildCheckinTierProgress(userId)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"enabled":       setting.Enabled,
			"min_quota":     setting.MinQuota,
			"max_quota":     setting.MaxQuota,
			"tiered":        setting.Tiered,
			"tiers":         operation_setting.GetCheckinTiers(),
			"matched_tier":  matchedTier,
			"tier_progress": tierProgress,
			"stats":         stats,
		},
	})
}

// DoCheckin 执行用户签到
func DoCheckin(c *gin.Context) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		common.ApiErrorMsg(c, "签到功能未启用")
		return
	}

	userId := c.GetInt("id")

	checkin, err := model.UserCheckin(userId)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("用户签到，获得额度 %s", logger.LogQuota(checkin.QuotaAwarded)))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "签到成功",
		"data": gin.H{
			"quota_awarded": checkin.QuotaAwarded,
			"checkin_date":  checkin.CheckinDate},
	})
}
