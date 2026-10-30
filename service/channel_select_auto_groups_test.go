package service

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelSelectAutoGroupsTest(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalRetryTimes := common.RetryTimes
	originalAutoGroups := setting.AutoGroups2JsonString()
	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalMaxTokenAutoGroups := setting.GetMaxTokenAutoGroups()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.ChannelModelCircuit{}))
	model.DB = db
	common.MemoryCacheEnabled = true
	common.RetryTimes = 0

	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`[]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2}`))
	require.NoError(t, setting.UpdateMaxTokenAutoGroups("2"))

	t.Cleanup(func() {
		model.DB = originalDB
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		common.RetryTimes = originalRetryTimes
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(fmt.Sprintf("%d", originalMaxTokenAutoGroups)))

		if originalMemoryCacheEnabled && originalDB != nil &&
			originalDB.Migrator().HasTable(&model.Channel{}) && originalDB.Migrator().HasTable(&model.Ability{}) {
			model.InitChannelCache()
		}
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	return db
}

// createChannelSelectAutoGroupsChannel adds an enabled channel serving modelName
// in every comma-separated group at the given priority.
func createChannelSelectAutoGroupsChannel(t *testing.T, db *gorm.DB, id int, groups, modelName string, priority int64) {
	t.Helper()
	weight := uint(100)
	require.NoError(t, db.Create(&model.Channel{
		Id:       id,
		Type:     constant.ChannelTypeOpenAI,
		Key:      fmt.Sprintf("key-%d", id),
		Status:   common.ChannelStatusEnabled,
		Name:     fmt.Sprintf("channel-%d", id),
		Weight:   &weight,
		Models:   modelName,
		Group:    groups,
		Priority: &priority,
	}).Error)
	for group := range strings.SplitSeq(groups, ",") {
		require.NoError(t, db.Create(&model.Ability{
			Group:     group,
			Model:     modelName,
			ChannelId: id,
			Enabled:   true,
			Priority:  &priority,
			Weight:    weight,
		}).Error)
	}
}

func TestCacheGetRandomSatisfiedChannelUsesTokenAutoGroupsWhenGlobalAutoIsEmpty(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "auto-groups-runtime-model"
	createChannelSelectAutoGroupsChannel(t, db, 2101, "vip", modelName, 0)
	createChannelSelectAutoGroupsChannel(t, db, 2102, "default", modelName, 0)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)

	retry := 0
	param := &RetryParam{
		Ctx:         ctx,
		TokenGroup:  "auto",
		ModelName:   modelName,
		RequestPath: "/v1/chat/completions",
		Retry:       &retry,
	}

	first, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, 2101, first.Id)
	assert.Equal(t, "vip", selectedGroup)
	assert.Equal(t, "vip", common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
	assert.Empty(t, setting.GetAutoGroups(), "the selection must not depend on the global Auto list")

	param.RecordAttempt(first.Id, selectedGroup)
	second, selectedGroup, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, 2102, second.Id)
	assert.Equal(t, "default", selectedGroup)
	assert.Equal(t, "default", common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup))
}

// runFallbackChain selects and records attempts until the chain is exhausted
// and returns the channel and group of every attempt.
func runFallbackChain(t *testing.T, param *RetryParam) (channels []int, groups []string) {
	t.Helper()
	for range 20 {
		if param.AttemptsLeft() == 0 {
			break
		}
		channel, group, err := CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, err)
		require.NotNil(t, channel, "AttemptsLeft promised another attempt")
		param.RecordAttempt(channel.Id, group)
		channels, groups = append(channels, channel.Id), append(groups, group)
	}
	next, _, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	assert.Nil(t, next, "an exhausted chain selects nothing")
	return channels, groups
}

func TestFallbackChainTriesEveryChannelOnceThenRetriesOnlyTheLast(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "fallback-chain-model"
	createChannelSelectAutoGroupsChannel(t, db, 2201, "default", modelName, 30)
	createChannelSelectAutoGroupsChannel(t, db, 2202, "default", modelName, 20)
	createChannelSelectAutoGroupsChannel(t, db, 2203, "default", modelName, 20)
	createChannelSelectAutoGroupsChannel(t, db, 2204, "default", modelName, 10)
	createChannelSelectAutoGroupsChannel(t, db, 2205, "single", modelName, 0)
	model.InitChannelCache()
	common.RetryTimes = 2

	for _, tc := range []struct {
		name  string
		group string
		check func(t *testing.T, channels []int)
	}{
		{name: "priority chain", group: "default", check: func(t *testing.T, channels []int) {
			require.Len(t, channels, 6, "four channels once each, then RetryTimes on the last")
			assert.Equal(t, 2201, channels[0])
			assert.ElementsMatch(t, []int{2202, 2203}, channels[1:3], "both channels of the shared tier are tried before the next tier")
			assert.Equal(t, []int{2204, 2204, 2204}, channels[3:])
		}},
		{name: "single channel keeps the full retry budget", group: "single", check: func(t *testing.T, channels []int) {
			assert.Equal(t, []int{2205, 2205, 2205}, channels)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			channels, _ := runFallbackChain(t, &RetryParam{Ctx: ctx, TokenGroup: tc.group, ModelName: modelName, Retry: common.GetPointer(0)})
			tc.check(t, channels)
		})
	}
}

func TestFallbackChainStopsWhenTheLastChannelIsDisabled(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "fallback-chain-disabled-model"
	createChannelSelectAutoGroupsChannel(t, db, 2211, "default", modelName, 10)
	createChannelSelectAutoGroupsChannel(t, db, 2212, "default", modelName, 0)
	model.InitChannelCache()
	common.RetryTimes = 2

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	param := &RetryParam{Ctx: ctx, TokenGroup: "default", ModelName: modelName, Retry: common.GetPointer(0)}
	for _, want := range []int{2211, 2212} {
		channel, group, err := CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, err)
		require.NotNil(t, channel)
		require.Equal(t, want, channel.Id)
		param.RecordAttempt(channel.Id, group)
	}
	// The failed last channel gets disabled before its retry.
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 2212).Update("status", common.ChannelStatusAutoDisabled).Error)
	model.InitChannelCache()
	assert.Equal(t, 0, param.AttemptsLeft())
}

func TestFallbackChainAcrossAutoGroups(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "fallback-chain-auto-model"
	createChannelSelectAutoGroupsChannel(t, db, 2221, "vip", modelName, 10)
	createChannelSelectAutoGroupsChannel(t, db, 2222, "vip,default", modelName, 5)
	createChannelSelectAutoGroupsChannel(t, db, 2223, "default", modelName, 0)
	model.InitChannelCache()
	common.RetryTimes = 1

	for _, tc := range []struct {
		name       string
		crossGroup bool
		channels   []int
		groups     []string
	}{
		{name: "cross-group retry walks every group and skips channels already tried", crossGroup: true,
			channels: []int{2221, 2222, 2223, 2223}, groups: []string{"vip", "vip", "default", "default"}},
		{name: "without cross-group retry the chain stays in the first group", crossGroup: false,
			channels: []int{2221, 2222, 2222}, groups: []string{"vip", "vip", "vip"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip", "default"})
			common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, tc.crossGroup)
			channels, groups := runFallbackChain(t, &RetryParam{Ctx: ctx, TokenGroup: "auto", ModelName: modelName, Retry: common.GetPointer(0)})
			assert.Equal(t, tc.channels, channels)
			assert.Equal(t, tc.groups, groups)
			assert.Equal(t, tc.groups[len(tc.groups)-1], common.GetContextKeyString(ctx, constant.ContextKeyAutoGroup), "billing follows the group of the last attempt")
		})
	}
}

func TestFallbackAvailableOnlyWhileAnotherChannelCanTakeOver(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "fallback-available-model"
	createChannelSelectAutoGroupsChannel(t, db, 2231, "default", modelName, 10)
	createChannelSelectAutoGroupsChannel(t, db, 2232, "default", modelName, 0)
	model.InitChannelCache()
	common.RetryTimes = 2

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	param := &RetryParam{Ctx: ctx, TokenGroup: "default", ModelName: modelName, Retry: common.GetPointer(0)}
	param.RecordAttempt(2231, "default")
	assert.True(t, FallbackAvailable(ctx, param), "the second channel can take over")
	param.RecordAttempt(2232, "default")
	assert.False(t, FallbackAvailable(ctx, param), "retrying the last channel is not a fallback")
	assert.Equal(t, 1, param.AttemptsLeft(), "the last channel keeps its retry budget")

	for _, tc := range []struct {
		name  string
		setup func(*gin.Context)
	}{
		{name: "strict session", setup: func(c *gin.Context) { c.Set(ginKeyChannelAffinitySkipRetry, true) }},
		{name: "single attempt pin", setup: func(c *gin.Context) {
			GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 2231, Source: dto.PinSourceToken, Rank: dto.PinRankToken, RetryMode: dto.PinRetrySingleAttempt})
		}},
	} {
		t.Run(tc.name+" never falls back", func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			tc.setup(c)
			p := &RetryParam{Ctx: c, TokenGroup: "default", ModelName: modelName, Retry: common.GetPointer(0)}
			p.RecordAttempt(2231, "default")
			assert.False(t, FallbackAvailable(c, p))
		})
	}
}

func TestOpenCircuitSitsOutWhileAHealthyChannelServes(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "circuit-chain-model"
	createChannelSelectAutoGroupsChannel(t, db, 2241, "default", modelName, 30)
	createChannelSelectAutoGroupsChannel(t, db, 2242, "default", modelName, 20)
	createChannelSelectAutoGroupsChannel(t, db, 2243, "single", modelName, 0)
	model.InitChannelCache()
	common.RetryTimes = 1
	previousThreshold := common.ChannelCircuitFailureThreshold
	common.ChannelCircuitFailureThreshold = 3
	t.Cleanup(func() {
		common.ChannelCircuitFailureThreshold = previousThreshold
		require.NoError(t, model.ResetChannelCircuits())
	})
	openCircuit := func(channelID int, modelName string) {
		for i := range 3 {
			assert.Equal(t, i == 2, model.RecordChannelFailure(channelID, modelName, "first_token_timeout (504)"), "the third failure in a row opens the circuit")
		}
	}
	chain := func(group string) []int {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		channels, _ := runFallbackChain(t, &RetryParam{Ctx: ctx, TokenGroup: group, ModelName: modelName, Retry: common.GetPointer(0)})
		return channels
	}

	openCircuit(2241, "another-model")
	assert.Equal(t, []int{2241, 2242, 2242}, chain("default"), "a circuit open for another model leaves this one alone")

	openCircuit(2241, modelName)
	assert.Equal(t, []int{2242, 2242}, chain("default"), "the open channel sits out even after the healthy one failed; the retry stays on the healthy one")

	openCircuit(2243, modelName)
	assert.Equal(t, []int{2243, 2243}, chain("single"), "a channel with no alternative keeps serving its model")

	openCircuit(2242, modelName)
	assert.Equal(t, []int{2241, 2242, 2242}, chain("default"), "with every candidate open they are all tried by priority")

	assert.True(t, model.RecordChannelSuccess(2241, modelName), "a success closes the circuit")
	assert.False(t, model.IsChannelCircuitOpen(2241, modelName))
	assert.True(t, model.IsChannelCircuitOpen(2241, "another-model"))
	assert.Equal(t, []int{2241, 2241}, chain("default"))

	common.ChannelCircuitFailureThreshold = 0
	assert.False(t, model.IsChannelCircuitOpen(2242, modelName), "turning the breaker off closes every circuit")
	assert.Empty(t, model.OpenChannelCircuits())
	assert.False(t, model.RecordChannelFailure(2241, modelName, "bad_response_status_code (502)"))
}
