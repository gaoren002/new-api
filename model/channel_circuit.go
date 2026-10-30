package model

import (
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// channelCircuit is one channel's run of consecutive relay failures on this
// instance. Once the run reaches common.ChannelCircuitFailureThreshold the
// circuit opens: channel selection skips the channel for every group and model
// another channel can serve, and the probe loop tests it until it recovers.
// The state is per instance and in memory; nothing is persisted.
type channelCircuit struct {
	failures    int
	lastFailure string
	openedAt    time.Time
	probedAt    time.Time
}

// ChannelCircuit describes an open circuit for the admin API and the probe loop.
type ChannelCircuit struct {
	ChannelID   int    `json:"channel_id"`
	Failures    int    `json:"failures"`
	LastFailure string `json:"last_failure"`
	OpenedAt    int64  `json:"opened_at"`
	ProbedAt    int64  `json:"probed_at,omitempty"`
}

var (
	channelCircuitsLock sync.RWMutex
	channelCircuits     = map[int]*channelCircuit{}
)

// RecordChannelFailure extends the channel's failure run and reports whether
// this failure opened its circuit.
func RecordChannelFailure(channelID int, reason string) bool {
	threshold := common.ChannelCircuitFailureThreshold
	if threshold <= 0 || channelID <= 0 {
		return false
	}
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	circuit := channelCircuits[channelID]
	if circuit == nil {
		circuit = &channelCircuit{}
		channelCircuits[channelID] = circuit
	}
	circuit.failures++
	circuit.lastFailure = reason
	if circuit.openedAt.IsZero() && circuit.failures >= threshold {
		circuit.openedAt = time.Now()
		return true
	}
	return false
}

// RecordChannelSuccess ends the channel's failure run and reports whether that
// closed an open circuit.
func RecordChannelSuccess(channelID int) bool {
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	circuit := channelCircuits[channelID]
	if circuit == nil {
		return false
	}
	delete(channelCircuits, channelID)
	return !circuit.openedAt.IsZero()
}

// RecordChannelProbeFailure notes a failed background test, so the next one
// waits a full probe interval.
func RecordChannelProbeFailure(channelID int) {
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	if circuit := channelCircuits[channelID]; circuit != nil {
		circuit.probedAt = time.Now()
	}
}

// ForgetChannelCircuit drops the channel's state, for example when the channel
// is no longer enabled and cannot be probed.
func ForgetChannelCircuit(channelID int) {
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	delete(channelCircuits, channelID)
}

// ResetChannelCircuits closes every circuit, for example when the circuit
// breaker is turned off.
func ResetChannelCircuits() {
	channelCircuitsLock.Lock()
	defer channelCircuitsLock.Unlock()
	clear(channelCircuits)
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
	for channelID, circuit := range channelCircuits {
		if circuit.openedAt.IsZero() {
			continue
		}
		item := ChannelCircuit{ChannelID: channelID, Failures: circuit.failures, LastFailure: circuit.lastFailure, OpenedAt: circuit.openedAt.Unix()}
		if !circuit.probedAt.IsZero() {
			item.ProbedAt = circuit.probedAt.Unix()
		}
		open = append(open, item)
	}
	sort.Slice(open, func(i, j int) bool {
		if open[i].OpenedAt != open[j].OpenedAt {
			return open[i].OpenedAt < open[j].OpenedAt
		}
		return open[i].ChannelID < open[j].ChannelID
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
	circuit := channelCircuits[channelID]
	return circuit != nil && !circuit.openedAt.IsZero()
}
