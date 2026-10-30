package controller

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// channelCircuitProbeTimeout bounds one background test, so a stalled channel
// cannot hold up the probes of the others.
const channelCircuitProbeTimeout = time.Minute

// RunChannelCircuitProbes tests every open circuit once per
// common.ChannelCircuitProbeSeconds, the same way the channel test button does
// but with the model of the circuit, and closes the circuit on the first
// successful test. Every instance runs this loop; the claim in the shared
// table lets one of them test each round.
func RunChannelCircuitProbes(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for _, circuit := range model.OpenChannelCircuits() {
			if common.GetTimestamp()-max(circuit.OpenedAt, circuit.ProbedAt) < int64(common.ChannelCircuitProbeSeconds) {
				continue
			}
			if !model.ClaimChannelCircuitProbe(circuit.ChannelID, circuit.Model, circuit.ProbedAt) {
				continue
			}
			channel, err := model.GetChannelById(circuit.ChannelID, true)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				common.SysError(common.LogText("channel circuit probe skipped: %s", err.Error()))
				continue
			}
			served := false
			if err == nil && channel.Status == common.ChannelStatusEnabled {
				models := channel.GetModels()
				served = slices.Contains(models, circuit.Model) || slices.Contains(models, ratio_setting.RoutingMatchModelName(circuit.Model))
			}
			if !served {
				// A deleted or disabled channel, or one that no longer serves the
				// model, is out of selection for it anyway.
				model.ForgetChannelCircuit(circuit.ChannelID, circuit.Model)
				continue
			}
			testUserID, err := resolveChannelTestUserID(nil)
			if err != nil {
				common.SysError(common.LogText("channel circuit probe skipped: %s", err.Error()))
				break
			}
			probeCtx, cancel := context.WithTimeout(ctx, channelCircuitProbeTimeout)
			result := testChannel(probeCtx, channel, testUserID, circuit.Model, "", shouldUseStreamForAutomaticChannelTest(channel))
			cancel()
			if result.localErr == nil && result.newAPIError == nil {
				if model.RecordChannelSuccess(channel.Id, circuit.Model) {
					common.SysLog(common.LogText("channel #%d circuit closed for %s: background test succeeded", channel.Id, circuit.Model))
				}
				continue
			}
			model.RecordChannelProbeFailure(channel.Id, circuit.Model)
			reason := "unknown error"
			if result.newAPIError != nil {
				reason = result.newAPIError.Error()
			} else if result.localErr != nil {
				reason = result.localErr.Error()
			}
			common.SysLog(common.LogText("channel #%d circuit stays open for %s: background test failed: %s", channel.Id, circuit.Model, common.LocalLogPreview(reason)))
		}
	}
}

// GetChannelCircuits lists the open circuits, which every instance shares.
// Each one names a channel and the model it is skipped for.
func GetChannelCircuits(c *gin.Context) {
	common.ApiSuccess(c, gin.H{
		"failure_threshold": common.ChannelCircuitFailureThreshold,
		"probe_seconds":     common.ChannelCircuitProbeSeconds,
		"items":             model.OpenChannelCircuits(),
	})
}
