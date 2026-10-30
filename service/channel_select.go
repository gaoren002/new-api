package service

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func GetChannelConstraints(c *gin.Context) *dto.ChannelConstraints {
	if c == nil {
		return &dto.ChannelConstraints{}
	}
	if existing, ok := common.GetContextKeyType[*dto.ChannelConstraints](c, constant.ContextKeyChannelConstraints); ok && existing != nil {
		return existing
	}
	constraints := &dto.ChannelConstraints{}
	common.SetContextKey(c, constant.ContextKeyChannelConstraints, constraints)
	return constraints
}

func AppendTaskPluginIdentityFilter(c *gin.Context, pluginKey string) {
	if c == nil {
		return
	}
	channelTypes, pluginKeys := pinnedTaskPluginIdentities(c, pluginKey)
	GetChannelConstraints(c).AddFilter(dto.ChannelFilter{
		Kind:                   dto.FilterTaskPluginIdentity,
		TaskPluginKey:          pluginKey,
		TaskPluginChannelTypes: channelTypes,
		TaskPluginKeys:         pluginKeys,
	})
}

type RetryParam struct {
	Ctx         *gin.Context
	TokenGroup  string
	ModelName   string
	RequestPath string
	Retry       *int
	// attempts lists the channels this request was sent to, in order.
	attempts []channelAttempt
	// lastChannelRetries is the RetryTimes budget spent repeating the last
	// channel after every candidate had one attempt.
	lastChannelRetries int
}

type channelAttempt struct {
	channelID int
	group     string
}

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) IncreaseRetry() {
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

// RecordAttempt notes the channel and group an attempt was sent to, so the
// following selections skip that channel.
func (p *RetryParam) RecordAttempt(channelID int, group string) {
	p.attempts = append(p.attempts, channelAttempt{channelID: channelID, group: group})
}

// HasFallbackChannel reports whether the fallback chain still has a channel
// the request has not tried. Repeating the last channel does not count.
func (p *RetryParam) HasFallbackChannel() bool {
	channel, _, repeatsLast, err := p.nextChannel()
	return err == nil && channel != nil && !repeatsLast
}

// AttemptsLeft is the retry budget handed to DecideRelayRetry: 1 when the
// fallback chain can make another attempt, 0 when it is exhausted.
func (p *RetryParam) AttemptsLeft() int {
	if channel, _, _, err := p.nextChannel(); err == nil && channel != nil {
		return 1
	}
	return 0
}

// CacheGetRandomSatisfiedChannel selects the channel for the next attempt of a
// request and returns the group it was selected from.
//
// The first attempt takes the highest priority tier, weighted by channel
// weight. Every later attempt skips the channels the request already tried and
// takes the highest tier left, so a failing request walks down the priority
// chain trying each candidate once. These attempts are free: only when no
// untried candidate is left does the request repeat its last channel, at most
// RetryTimes times.
//
// For an "auto" token with cross-group retry the chain runs through the auto
// groups in order. Without it the chain stays in the first auto group that
// serves the model.
func CacheGetRandomSatisfiedChannel(param *RetryParam) (*model.Channel, string, error) {
	channel, group, repeatsLast, err := param.nextChannel()
	if err != nil || channel == nil {
		return nil, group, err
	}
	if repeatsLast {
		param.lastChannelRetries++
	}
	if param.TokenGroup == "auto" {
		common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, group)
	}
	return channel, group, nil
}

// nextChannel picks the channel for the next attempt without changing the
// retry state. repeatsLast is true when the pick repeats the last channel.
func (p *RetryParam) nextChannel() (channel *model.Channel, group string, repeatsLast bool, err error) {
	filters := GetChannelConstraints(p.Ctx).Filters
	groups, err := p.chainGroups(filters)
	if err != nil {
		return nil, p.TokenGroup, false, err
	}
	tried := make([]int, 0, len(p.attempts))
	for _, attempt := range p.attempts {
		tried = append(tried, attempt.channelID)
	}
	var open []int
	for _, circuit := range model.OpenChannelCircuits() {
		open = append(open, circuit.ChannelID)
	}
	if channel, group, err = p.pickChannel(groups, append(slices.Clone(tried), open...), filters); err != nil || channel != nil {
		return channel, group, false, err
	}
	// Channels with an open circuit sit out while any healthy channel serves the
	// model, even one this request already tried, so the retry budget goes to a
	// healthy channel. Only when every candidate is open are they tried too.
	if len(open) > 0 {
		healthy, _, healthyErr := p.pickChannel(groups, open, filters)
		if healthyErr == nil && healthy == nil {
			if channel, group, err = p.pickChannel(groups, tried, filters); err != nil || channel != nil {
				return channel, group, false, err
			}
		}
	}
	if len(groups) > 0 {
		group = groups[0]
	}
	if len(p.attempts) == 0 || p.lastChannelRetries >= common.RetryTimes {
		return nil, group, false, nil
	}
	last := p.attempts[len(p.attempts)-1]
	channel, err = model.CacheGetChannel(last.channelID)
	if err != nil || channel.Status != common.ChannelStatusEnabled {
		// The last channel was disabled after it failed; nothing is left to try.
		return nil, last.group, false, nil
	}
	return channel, last.group, true, nil
}

// pickChannel selects from the first of groups that has a candidate outside
// excluded.
func (p *RetryParam) pickChannel(groups []string, excluded []int, filters []dto.ChannelFilter) (*model.Channel, string, error) {
	for _, group := range groups {
		channel, err := model.GetRandomSatisfiedChannel(group, p.ModelName, excluded, filters)
		if err != nil || channel != nil {
			return channel, group, err
		}
	}
	return nil, "", nil
}

// chainGroups lists the groups the fallback chain may select from, in order.
func (p *RetryParam) chainGroups(filters []dto.ChannelFilter) ([]string, error) {
	if p.TokenGroup != "auto" {
		return []string{p.TokenGroup}, nil
	}
	userGroup := common.GetContextKeyString(p.Ctx, constant.ContextKeyUserGroup)
	autoGroups := GetRequestAutoGroups(p.Ctx, userGroup)
	if len(autoGroups) == 0 {
		return nil, errors.New("auto groups is not enabled")
	}
	if common.GetContextKeyBool(p.Ctx, constant.ContextKeyTokenCrossGroupRetry) {
		return autoGroups, nil
	}
	for _, group := range autoGroups {
		channel, err := model.GetRandomSatisfiedChannel(group, p.ModelName, nil, filters)
		if err != nil {
			return nil, err
		}
		if channel != nil {
			return []string{group}, nil
		}
	}
	return nil, nil
}

func pinnedTaskPluginIdentities(c *gin.Context, expected string) ([]int, []string) {
	if c == nil || expected == "" {
		return nil, nil
	}
	if value, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint); exists {
		pinned, ok := value.(jsplugin.PinnedEndpoint)
		if ok && pinned.Generation != nil && len(pinned.Candidates) > 1 {
			expectedFound := false
			channelTypes := make([]int, 0, len(pinned.Candidates))
			pluginKeys := make([]string, 0, len(pinned.Candidates))
			seen := make(map[int]struct{}, len(pinned.Candidates))
			for _, candidate := range pinned.Candidates {
				if candidate.Plugin == nil {
					continue
				}
				if candidate.Plugin.Meta.Key == expected {
					expectedFound = true
				}
				pluginKeys = append(pluginKeys, candidate.Plugin.Meta.Key)
				for _, channelType := range candidate.Plugin.Meta.ChannelTypes {
					if channelType == 0 || channelType == constant.ChannelTypeTaskPlugin {
						continue
					}
					if _, duplicate := seen[channelType]; duplicate {
						continue
					}
					if plugin, indexed := pinned.Generation.GetByChannelType(channelType); indexed && plugin == candidate.Plugin {
						seen[channelType] = struct{}{}
						channelTypes = append(channelTypes, channelType)
					}
				}
			}
			if expectedFound {
				return channelTypes, pluginKeys
			}
		}
	}
	value, exists := c.Get(jsplugin.ContextKeyPinnedPlugin)
	pinned, ok := value.(jsplugin.PinnedPlugin)
	if !exists || !ok || pinned.Generation == nil || pinned.Plugin == nil || pinned.Plugin.Meta.Key != expected {
		return nil, nil
	}
	channelTypes := make([]int, 0, len(pinned.Plugin.Meta.ChannelTypes))
	for _, channelType := range pinned.Plugin.Meta.ChannelTypes {
		if channelType == 0 || channelType == constant.ChannelTypeTaskPlugin {
			continue
		}
		channelTypes = append(channelTypes, channelType)
	}
	return channelTypes, []string{expected}
}

// ChannelSelectError explains why SelectChannelForRequest found no channel.
// Callers render it for their transport: the HTTP distributor localizes
// MessageID with its own helpers and the Responses WebSocket relay wraps it in
// a NewAPIError. Message is set instead of MessageID when the text is a fixed
// error code that clients match on.
type ChannelSelectError struct {
	StatusCode int
	Code       types.ErrorCode
	MessageID  string
	Params     map[string]any
	Message    string
	// FilterKind and Channel identify a candidate rejected by request filters.
	FilterKind dto.ChannelFilterKind
	Channel    *model.Channel
	// NoAvailableChannel marks the "no channel for this group and model"
	// outcome so the distributor can name the claiming task plugin.
	NoAvailableChannel bool
}

// SelectChannelForRequest resolves the channel for one attempt with the rules
// shared by the HTTP distributor and the Responses WebSocket relay: a pinned
// channel wins, then session affinity (first attempt only), then a random
// eligible channel; every candidate must satisfy the request's channel
// filters. The group the channel was chosen from is returned for auto-group
// callers. The caller still applies SetupContextForSelectedChannel.
func SelectChannelForRequest(c *gin.Context, modelName string, retry *RetryParam) (*model.Channel, string, *ChannelSelectError) {
	constraints := GetChannelConstraints(c)
	if pin, found, overridden := constraints.ResolvedPin(); found {
		for _, lost := range overridden {
			logger.LogWarn(c, common.LogText(
				"channel pin overridden: winning_source=%s winning_channel_id=%d overridden_source=%s overridden_channel_id=%d",
				pin.Source, pin.ChannelId, lost.Source, lost.ChannelId,
			))
		}
		channel, err := model.CacheGetChannel(pin.ChannelId)
		if err != nil {
			return nil, "", pinnedChannelUnavailable(pin, http.StatusBadRequest, i18n.MsgDistributorInvalidChannelId)
		}
		if channel.Status != common.ChannelStatusEnabled {
			return nil, "", pinnedChannelUnavailable(pin, http.StatusForbidden, i18n.MsgDistributorChannelDisabled)
		}
		if ok, kind := model.ChannelSatisfiesFilters(channel, modelName, constraints.Filters); !ok {
			return nil, "", &ChannelSelectError{
				StatusCode: http.StatusBadRequest, Code: types.ErrorCode(kind), MessageID: i18n.MsgDistributorNoAvailableChannel,
				Params:     map[string]any{"Group": common.GetContextKeyString(c, constant.ContextKeyUsingGroup), "Model": modelName},
				FilterKind: kind, Channel: channel,
			}
		}
		return channel, "", nil
	}

	usingGroup := retry.TokenGroup
	var channel *model.Channel
	var selectGroup string
	if len(retry.attempts) == 0 {
		if preferredChannelID, found := GetPreferredChannelByAffinity(c, modelName, usingGroup); found {
			affinityUsable := false
			preferred, err := model.CacheGetChannel(preferredChannelID)
			affinitySatisfied := false
			// A strict session stays on its channel; any other session leaves a
			// channel whose circuit is open and rebinds where it succeeds.
			usable := err == nil && preferred != nil && preferred.Status == common.ChannelStatusEnabled &&
				(RequestPolicy(c).SessionMode == "strict" || !model.IsChannelCircuitOpen(preferred.Id))
			if usable {
				affinitySatisfied, _ = model.ChannelSatisfiesFilters(preferred, modelName, constraints.Filters)
			}
			if affinitySatisfied {
				if usingGroup == "auto" {
					userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
					for _, g := range GetRequestAutoGroups(c, userGroup) {
						if model.IsChannelEnabledForGroupModel(g, modelName, preferred.Id) {
							selectGroup = g
							common.SetContextKey(c, constant.ContextKeyAutoGroup, g)
							channel = preferred
							affinityUsable = true
							MarkChannelAffinityUsed(c, g, preferred.Id)
							break
						}
					}
				} else if model.IsChannelEnabledForGroupModel(usingGroup, modelName, preferred.Id) {
					channel = preferred
					selectGroup = usingGroup
					affinityUsable = true
					MarkChannelAffinityUsed(c, usingGroup, preferred.Id)
				}
			}
			if !affinityUsable && !ShouldKeepChannelAffinityOnChannelDisabled() {
				ClearCurrentChannelAffinityCache(c)
			}
			if !affinityUsable && RequestPolicy(c).SessionMode == "strict" {
				return nil, "", &ChannelSelectError{StatusCode: http.StatusServiceUnavailable, Message: "strict_session_binding_unavailable"}
			}
		}
	}

	if channel == nil {
		var err error
		channel, selectGroup, err = CacheGetRandomSatisfiedChannel(retry)
		if err != nil {
			showGroup := usingGroup
			if usingGroup == "auto" {
				showGroup = fmt.Sprintf("auto(%s)", selectGroup)
			}
			return nil, selectGroup, &ChannelSelectError{
				StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeModelNotFound, MessageID: i18n.MsgDistributorGetChannelFailed,
				Params: map[string]any{"Group": showGroup, "Model": modelName, "Error": err.Error()},
			}
		}
		if channel == nil {
			return nil, selectGroup, &ChannelSelectError{
				StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeModelNotFound, MessageID: i18n.MsgDistributorNoAvailableChannel,
				Params: map[string]any{"Group": usingGroup, "Model": modelName}, NoAvailableChannel: true,
			}
		}
	}
	if ok, kind := model.ChannelSatisfiesFilters(channel, modelName, constraints.Filters); !ok {
		return nil, selectGroup, &ChannelSelectError{
			StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeModelNotFound, MessageID: i18n.MsgDistributorNoAvailableChannel,
			Params:     map[string]any{"Group": common.GetContextKeyString(c, constant.ContextKeyUsingGroup), "Model": modelName},
			FilterKind: kind, Channel: channel, NoAvailableChannel: true,
		}
	}
	return channel, selectGroup, nil
}

// Origin-task pins report a fixed code so task polling can tell a retired
// channel from a malformed request.
func pinnedChannelUnavailable(pin dto.ChannelPin, statusCode int, messageID string) *ChannelSelectError {
	if pin.Source == dto.PinSourceOriginTask {
		return &ChannelSelectError{StatusCode: http.StatusBadRequest, Code: "origin_task_channel_disabled", Message: "origin_task_channel_disabled"}
	}
	return &ChannelSelectError{StatusCode: statusCode, MessageID: messageID}
}

// AppendUsedChannel records an attempted channel in the request's channel
// trail, which the retry log and the consume log's admin_info both read.
func AppendUsedChannel(c *gin.Context, channelID int) {
	c.Set("use_channel", append(c.GetStringSlice("use_channel"), fmt.Sprintf("%d", channelID)))
}
