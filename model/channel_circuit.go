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

// ChannelModelCircuit is one channel's run of consecutive relay failures for
// one model, shared by every instance through the database. Once the run
// reaches common.ChannelCircuitFailureThreshold the circuit opens: channel
// selection skips the channel for that model while another channel serves it,
// and the probe loop tests the channel with that model until it recovers. The
// channel keeps serving its other models. A success deletes the row.
type ChannelModelCircuit struct {
	ChannelID   int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	Model       string `json:"model" gorm:"type:varchar(255);primaryKey"`
	Failures    int    `json:"failures"`
	LastFailure string `json:"last_failure" gorm:"type:text"`
	OpenedAt    int64  `json:"opened_at"`
	ProbedAt    int64  `json:"probed_at,omitempty"`
}

// channelCircuitSyncInterval bounds how long a circuit opened or closed by one
// instance takes to apply on the others.
const channelCircuitSyncInterval = 2 * time.Second

type channelModelKey struct {
	channelID int
	model     string
}

// channelCircuits is this instance's copy of the table. Channel selection
// reads only the copy; writes go to the table first and then to the copy.
var (
	channelCircuitsLock sync.RWMutex
	channelCircuits     = map[channelModelKey]ChannelModelCircuit{}
)

// RecordChannelFailure extends the failure run of the channel for the model
// and reports whether this failure opened the circuit. When several instances
// see the failure that reaches the threshold, exactly one of them opens it.
func RecordChannelFailure(channelID int, modelName string, reason string) bool {
	threshold := common.ChannelCircuitFailureThreshold
	if threshold <= 0 || channelID <= 0 || modelName == "" {
		return false
	}
	countFailure := map[string]any{"failures": gorm.Expr("failures + 1"), "last_failure": reason}
	result := DB.Model(&ChannelModelCircuit{}).Where("channel_id = ? AND model = ?", channelID, modelName).Updates(countFailure)
	if result.Error == nil && result.RowsAffected == 0 {
		// The first failure of a run creates the row. Another instance may
		// create it at the same moment, so the update still does the counting.
		result = DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&ChannelModelCircuit{ChannelID: channelID, Model: modelName})
		if result.Error == nil {
			result = DB.Model(&ChannelModelCircuit{}).Where("channel_id = ? AND model = ?", channelID, modelName).Updates(countFailure)
		}
	}
	now := common.GetTimestamp()
	opened := false
	if result.Error == nil && !IsChannelCircuitOpen(channelID, modelName) {
		result = DB.Model(&ChannelModelCircuit{}).
			Where("channel_id = ? AND model = ? AND opened_at = 0 AND failures >= ?", channelID, modelName, threshold).
			Update("opened_at", now)
		opened = result.Error == nil && result.RowsAffected > 0
	}
	if result.Error != nil {
		common.SysError(common.LogText("channel #%d circuit for %s: failed to record a failure: %s", channelID, modelName, result.Error.Error()))
		return false
	}
	key := channelModelKey{channelID, modelName}
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	circuit := channelCircuits[key]
	circuit.ChannelID, circuit.Model = channelID, modelName
	circuit.Failures++
	circuit.LastFailure = reason
	if opened {
		circuit.OpenedAt = now
	}
	channelCircuits[key] = circuit
	return opened
}

// RecordChannelSuccess ends the failure run of the channel for the model and
// reports whether that closed an open circuit. A pair without a failure run
// costs no query.
func RecordChannelSuccess(channelID int, modelName string) bool {
	key := channelModelKey{channelID, modelName}
	channelCircuitsLock.RLock()
	circuit, found := channelCircuits[key]
	channelCircuitsLock.RUnlock()
	if !found {
		return false
	}
	result := DB.Where("channel_id = ? AND model = ?", channelID, modelName).Delete(&ChannelModelCircuit{})
	if result.Error != nil {
		common.SysError(common.LogText("channel #%d circuit for %s: failed to record a success: %s", channelID, modelName, result.Error.Error()))
		return false
	}
	channelCircuitsLock.Lock()
	delete(channelCircuits, key)
	channelCircuitsLock.Unlock()
	return circuit.OpenedAt > 0 && result.RowsAffected > 0
}

// ClaimChannelCircuitProbe reserves the background test of an open circuit
// last tested at probedAt. Every instance runs the probe loop; only the one
// whose claim succeeds tests the channel.
func ClaimChannelCircuitProbe(channelID int, modelName string, probedAt int64) bool {
	now := common.GetTimestamp()
	result := DB.Model(&ChannelModelCircuit{}).
		Where("channel_id = ? AND model = ? AND opened_at > 0 AND probed_at = ?", channelID, modelName, probedAt).
		Update("probed_at", now)
	if result.Error != nil {
		common.SysError(common.LogText("channel #%d circuit for %s: failed to claim a background test: %s", channelID, modelName, result.Error.Error()))
		return false
	}
	if result.RowsAffected == 0 {
		return false
	}
	key := channelModelKey{channelID, modelName}
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	if circuit, found := channelCircuits[key]; found {
		circuit.ProbedAt = now
		channelCircuits[key] = circuit
	}
	return true
}

// RecordChannelProbeFailure notes a failed background test, so the next one
// waits a full probe interval.
func RecordChannelProbeFailure(channelID int, modelName string) {
	now := common.GetTimestamp()
	if err := DB.Model(&ChannelModelCircuit{}).Where("channel_id = ? AND model = ?", channelID, modelName).Update("probed_at", now).Error; err != nil {
		common.SysError(common.LogText("channel #%d circuit for %s: failed to record a background test: %s", channelID, modelName, err.Error()))
		return
	}
	key := channelModelKey{channelID, modelName}
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	if circuit, found := channelCircuits[key]; found {
		circuit.ProbedAt = now
		channelCircuits[key] = circuit
	}
}

// ForgetChannelCircuit drops the state of the channel for the model, for
// example when the channel is disabled or no longer serves the model.
func ForgetChannelCircuit(channelID int, modelName string) {
	if err := DB.Where("channel_id = ? AND model = ?", channelID, modelName).Delete(&ChannelModelCircuit{}).Error; err != nil {
		common.SysError(common.LogText("channel #%d circuit for %s: failed to forget: %s", channelID, modelName, err.Error()))
		return
	}
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	delete(channelCircuits, channelModelKey{channelID, modelName})
}

// ResetChannelCircuits closes every circuit on every instance.
func ResetChannelCircuits() error {
	err := DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&ChannelModelCircuit{}).Error
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
	var circuits []ChannelModelCircuit
	if err := DB.Find(&circuits).Error; err != nil {
		return err
	}
	if len(circuits) > 0 && common.ChannelCircuitFailureThreshold <= 0 {
		return ResetChannelCircuits()
	}
	loaded := make(map[channelModelKey]ChannelModelCircuit, len(circuits))
	for _, circuit := range circuits {
		loaded[channelModelKey{circuit.ChannelID, circuit.Model}] = circuit
	}
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	channelCircuits = loaded
	return nil
}

// OpenChannelCircuits lists the open circuits, oldest first. It is empty while
// the circuit breaker is turned off.
func OpenChannelCircuits() []ChannelModelCircuit {
	if common.ChannelCircuitFailureThreshold <= 0 {
		return []ChannelModelCircuit{}
	}
	channelCircuitsLock.RLock()
	open := make([]ChannelModelCircuit, 0)
	for _, circuit := range channelCircuits {
		if circuit.OpenedAt > 0 {
			open = append(open, circuit)
		}
	}
	channelCircuitsLock.RUnlock()
	slices.SortFunc(open, func(a, b ChannelModelCircuit) int {
		return cmp.Or(cmp.Compare(a.OpenedAt, b.OpenedAt), cmp.Compare(a.ChannelID, b.ChannelID), cmp.Compare(a.Model, b.Model))
	})
	return open
}

// IsChannelCircuitOpen reports whether the circuit of the channel for the
// model is open.
func IsChannelCircuitOpen(channelID int, modelName string) bool {
	if common.ChannelCircuitFailureThreshold <= 0 {
		return false
	}
	channelCircuitsLock.RLock()
	defer channelCircuitsLock.RUnlock()
	return channelCircuits[channelModelKey{channelID, modelName}].OpenedAt > 0
}
