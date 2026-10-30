package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// channelCircuitProbeTimeout bounds one background test, so a stalled channel
// cannot hold up the probes of the others.
const channelCircuitProbeTimeout = time.Minute

// RunChannelCircuitProbes tests every channel with an open circuit once per
// common.ChannelCircuitProbeSeconds, the same way the channel test button does,
// and closes the circuit on the first successful test. Each instance probes the
// circuits it opened itself.
func RunChannelCircuitProbes(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		interval := time.Duration(common.ChannelCircuitProbeSeconds) * time.Second
		for _, circuit := range model.OpenChannelCircuits() {
			if time.Since(time.Unix(max(circuit.OpenedAt, circuit.ProbedAt), 0)) < interval {
				continue
			}
			channel, err := model.CacheGetChannel(circuit.ChannelID)
			if err != nil || channel.Status != common.ChannelStatusEnabled {
				// Disabled or deleted channels are out of selection anyway.
				model.ForgetChannelCircuit(circuit.ChannelID)
				continue
			}
			testUserID, err := resolveChannelTestUserID(nil)
			if err != nil {
				common.SysError(common.LogText("channel circuit probe skipped: %s", err.Error()))
				break
			}
			probeCtx, cancel := context.WithTimeout(ctx, channelCircuitProbeTimeout)
			result := testChannel(probeCtx, channel, testUserID, "", "", shouldUseStreamForAutomaticChannelTest(channel))
			cancel()
			if result.localErr == nil && result.newAPIError == nil {
				if model.RecordChannelSuccess(channel.Id) {
					common.SysLog(common.LogText("channel #%d circuit closed: background test succeeded", channel.Id))
				}
				continue
			}
			model.RecordChannelProbeFailure(channel.Id)
			reason := "unknown error"
			if result.newAPIError != nil {
				reason = result.newAPIError.Error()
			} else if result.localErr != nil {
				reason = result.localErr.Error()
			}
			common.SysLog(common.LogText("channel #%d circuit stays open: background test failed: %s", channel.Id, common.LocalLogPreview(reason)))
		}
	}
}

// GetChannelCircuits lists the open channel circuits of the instance serving
// the admin console.
func GetChannelCircuits(c *gin.Context) {
	common.ApiSuccess(c, gin.H{
		"failure_threshold": common.ChannelCircuitFailureThreshold,
		"probe_seconds":     common.ChannelCircuitProbeSeconds,
		"items":             model.OpenChannelCircuits(),
	})
}
