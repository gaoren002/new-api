package model

import (
	"cmp"
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelCircuit is one channel's run of consecutive relay failures, shared by
// every instance through the database. Once the run reaches
// common.ChannelCircuitFailureThreshold the circuit opens: channel selection
// skips the channel for every group and model another channel can serve, and
// the probe loop tests it until it recovers. A success deletes the row.
type ChannelCircuit struct {
	ChannelID   int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	Failures    int    `json:"failures"`
	LastFailure string `json:"last_failure" gorm:"type:text"`
	OpenedAt    int64  `json:"opened_at"`
	ProbedAt    int64  `json:"probed_at,omitempty"`
}

// channelCircuitSyncInterval bounds how long a circuit opened or closed by one
// instance takes to apply on the others.
const channelCircuitSyncInterval = 2 * time.Second

// channelCircuits is this instance's copy of the table. Channel selection
// reads only the copy; writes go to the table first and then to the copy.
var (
	channelCircuitsLock sync.RWMutex
	channelCircuits     = map[int]ChannelCircuit{}
)

// RecordChannelFailure extends the channel's failure run and reports whether
// this failure opened its circuit. When several instances see the failure
// that reaches the threshold, exactly one of them opens the circuit.
func RecordChannelFailure(channelID int, reason string) bool {
	threshold := common.ChannelCircuitFailureThreshold
	if threshold <= 0 || channelID <= 0 {
		return false
	}
	countFailure := map[string]any{"failures": gorm.Expr("failures + 1"), "last_failure": reason}
	result := DB.Model(&ChannelCircuit{}).Where("channel_id = ?", channelID).Updates(countFailure)
	if result.Error == nil && result.RowsAffected == 0 {
		// The first failure of a run creates the row. Another instance may
		// create it at the same moment, so the update still does the counting.
		result = DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&ChannelCircuit{ChannelID: channelID})
		if result.Error == nil {
			result = DB.Model(&ChannelCircuit{}).Where("channel_id = ?", channelID).Updates(countFailure)
		}
	}
	now := common.GetTimestamp()
	opened := false
	if result.Error == nil && !IsChannelCircuitOpen(channelID) {
		result = DB.Model(&ChannelCircuit{}).
			Where("channel_id = ? AND opened_at = 0 AND failures >= ?", channelID, threshold).
			Update("opened_at", now)
		opened = result.Error == nil && result.RowsAffected > 0
	}
	if result.Error != nil {
		common.SysError(common.LogText("channel #%d circuit: failed to record a failure: %s", channelID, result.Error.Error()))
		return false
	}
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	circuit := channelCircuits[channelID]
	circuit.ChannelID = channelID
	circuit.Failures++
	circuit.LastFailure = reason
	if opened {
		circuit.OpenedAt = now
	}
	channelCircuits[channelID] = circuit
	return opened
}

// RecordChannelSuccess ends the channel's failure run and reports whether that
// closed an open circuit. A channel without a failure run costs no query.
func RecordChannelSuccess(channelID int) bool {
	channelCircuitsLock.RLock()
	circuit, found := channelCircuits[channelID]
	channelCircuitsLock.RUnlock()
	if !found {
		return false
	}
	result := DB.Where("channel_id = ?", channelID).Delete(&ChannelCircuit{})
	if result.Error != nil {
		common.SysError(common.LogText("channel #%d circuit: failed to record a success: %s", channelID, result.Error.Error()))
		return false
	}
	channelCircuitsLock.Lock()
	delete(channelCircuits, channelID)
	channelCircuitsLock.Unlock()
	return circuit.OpenedAt > 0 && result.RowsAffected > 0
}

// ClaimChannelCircuitProbe reserves the background test of an open circuit
// last tested at probedAt. Every instance runs the probe loop; only the one
// whose claim succeeds tests the channel.
func ClaimChannelCircuitProbe(channelID int, probedAt int64) bool {
	now := common.GetTimestamp()
	result := DB.Model(&ChannelCircuit{}).
		Where("channel_id = ? AND opened_at > 0 AND probed_at = ?", channelID, probedAt).
		Update("probed_at", now)
	if result.Error != nil {
		common.SysError(common.LogText("channel #%d circuit: failed to claim a background test: %s", channelID, result.Error.Error()))
		return false
	}
	if result.RowsAffected == 0 {
		return false
	}
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	if circuit, found := channelCircuits[channelID]; found {
		circuit.ProbedAt = now
		channelCircuits[channelID] = circuit
	}
	return true
}

// RecordChannelProbeFailure notes a failed background test, so the next one
// waits a full probe interval.
func RecordChannelProbeFailure(channelID int) {
	now := common.GetTimestamp()
	if err := DB.Model(&ChannelCircuit{}).Where("channel_id = ?", channelID).Update("probed_at", now).Error; err != nil {
		common.SysError(common.LogText("channel #%d circuit: failed to record a background test: %s", channelID, err.Error()))
		return
	}
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	if circuit, found := channelCircuits[channelID]; found {
		circuit.ProbedAt = now
		channelCircuits[channelID] = circuit
	}
}

// ForgetChannelCircuit drops the channel's state, for example when the channel
// is no longer enabled and cannot be probed.
func ForgetChannelCircuit(channelID int) {
	if err := DB.Where("channel_id = ?", channelID).Delete(&ChannelCircuit{}).Error; err != nil {
		common.SysError(common.LogText("channel #%d circuit: failed to forget: %s", channelID, err.Error()))
		return
	}
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	delete(channelCircuits, channelID)
}

// ResetChannelCircuits closes every circuit on every instance.
func ResetChannelCircuits() error {
	err := DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&ChannelCircuit{}).Error
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	clear(channelCircuits)
	return err
}

// SyncChannelCircuits keeps this instance's copy of the table current, so a
// circuit opened or closed by another instance applies here too.
func SyncChannelCircuits() {
	logged := ""
	for {
		message := ""
		if err := loadChannelCircuits(); err != nil {
			message = err.Error()
		}
		if message != "" && message != logged {
			common.SysError(common.LogText("failed to sync channel circuits: %s", message))
		}
		logged = message
		time.Sleep(channelCircuitSyncInterval)
	}
}

// loadChannelCircuits replaces this instance's copy with the table. While the
// circuit breaker is off it empties the table instead, so turning the breaker
// back on starts with every circuit closed.
func loadChannelCircuits() error {
	var circuits []ChannelCircuit
	if err := DB.Find(&circuits).Error; err != nil {
		return err
	}
	if len(circuits) > 0 && common.ChannelCircuitFailureThreshold <= 0 {
		return ResetChannelCircuits()
	}
	loaded := make(map[int]ChannelCircuit, len(circuits))
	for _, circuit := range circuits {
		loaded[circuit.ChannelID] = circuit
	}
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	channelCircuits = loaded
	return nil
}

// OpenChannelCircuits lists the open circuits, oldest first. It is empty while
// the circuit breaker is turned off.
func OpenChannelCircuits() []ChannelCircuit {
	if common.ChannelCircuitFailureThreshold <= 0 {
		return []ChannelCircuit{}
	}
	channelCircuitsLock.RLock()
	defer channelCircuitsLock.RUnlock()
	open := make([]ChannelCircuit, 0)
	for _, circuit := range channelCircuits {
		if circuit.OpenedAt > 0 {
			open = append(open, circuit)
		}
	}
	slices.SortFunc(open, func(a, b ChannelCircuit) int {
		return cmp.Or(cmp.Compare(a.OpenedAt, b.OpenedAt), cmp.Compare(a.ChannelID, b.ChannelID))
	})
	return open
}

// IsChannelCircuitOpen reports whether the channel's circuit is open.
func IsChannelCircuitOpen(channelID int) bool {
	if common.ChannelCircuitFailureThreshold <= 0 {
		return false
	}
	channelCircuitsLock.RLock()
	defer channelCircuitsLock.RUnlock()
	return channelCircuits[channelID].OpenedAt > 0
}
