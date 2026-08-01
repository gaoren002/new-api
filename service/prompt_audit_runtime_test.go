package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestEnqueuePromptAuditPersistsRedactedJobAndTransientFullPayload(t *testing.T) {
	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:prompt-audit-runtime-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.PromptAuditQueueLock{}, &model.PromptAuditJob{}, &model.PromptAuditEvent{}))
	model.DB = db
	server := miniredis.RunT(t)
	previousRedisEnabled, previousRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled, common.RDB = previousRedisEnabled, previousRDB
	})
	StartPromptAuditService()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, StopPromptAuditService(ctx))
	})
	config := promptAuditTestConfig("http://qwen3guard:11434")
	config.Mode = setting.PromptAuditModeAsync
	config.BlockingLatestTurnOnly = true
	config.ConfigVersion = 4
	config.QueueCapacity = 10
	EnqueuePromptAuditRequest(context.Background(), config, PromptAuditRequest{
		RequestID: "req-async", UserID: 8, Username: "alice", Protocol: "openai_chat_completions",
		Body: []byte(`{"messages":[{"role":"user","content":"old secret prompt"},{"role":"assistant","content":"previous output"},{"role":"user","content":"full secret prompt"}]}`),
	})
	var job model.PromptAuditJob
	require.Eventually(t, func() bool {
		var candidate model.PromptAuditJob
		if db.First(&candidate).Error != nil || candidate.Status == model.PromptAuditJobStaging {
			return false
		}
		job = candidate
		return true
	}, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, model.PromptAuditJobQueued, job.Status)
	require.NotContains(t, job.SnapshotJSON, "full secret prompt")
	payload, err := common.RDB.Get(context.Background(), model.PromptAuditJobPayloadKey(job.Id)).Result()
	require.NoError(t, err)
	require.Contains(t, payload, "full secret prompt")
	require.Contains(t, payload, "previous output")
	require.NotContains(t, payload, "old secret prompt")
	ttl, err := common.RDB.TTL(context.Background(), model.PromptAuditJobPayloadKey(job.Id)).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 29*time.Minute)
}

func TestPromptAuditAsyncWorkersFollowMasterRoleAndModes(t *testing.T) {
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = wasMaster })

	storage := setting.DefaultPromptAuditStorageConfig()
	storage.Mode = setting.PromptAuditModeOff
	require.False(t, promptAuditAsyncWorkersEnabled(storage))

	storage.GroupPolicies["audited"] = setting.PromptAuditGroupPolicy{Mode: setting.PromptAuditModeAsync}
	require.True(t, promptAuditAsyncWorkersEnabled(storage))

	storage.GroupPolicies = nil
	storage.Mode = setting.PromptAuditModeAsync
	require.True(t, promptAuditAsyncWorkersEnabled(storage))

	common.IsMasterNode = false
	require.False(t, promptAuditAsyncWorkersEnabled(storage))
}

func TestPromptAuditRuntimeEnabledFollowsGlobalAndGroupModes(t *testing.T) {
	storage := setting.DefaultPromptAuditStorageConfig()
	storage.Mode = setting.PromptAuditModeOff
	require.False(t, promptAuditRuntimeEnabled(storage))

	storage.GroupPolicies["audited"] = setting.PromptAuditGroupPolicy{Mode: setting.PromptAuditModeBlocking}
	require.True(t, promptAuditRuntimeEnabled(storage))

	storage.GroupPolicies["audited"] = setting.PromptAuditGroupPolicy{Mode: setting.PromptAuditModeOff}
	require.False(t, promptAuditRuntimeEnabled(storage))
}

func TestPromptAuditMetricsExposeLatencyPercentiles(t *testing.T) {
	metrics := &promptAuditMetrics{}
	for _, latency := range []int64{10, 20, 30, 40, 100} {
		metrics.total.Add(1)
		metrics.latencyTotal.Add(latency)
		for current := metrics.latencyMax.Load(); latency > current && !metrics.latencyMax.CompareAndSwap(current, latency); current = metrics.latencyMax.Load() {
		}
		metrics.latencies = append(metrics.latencies, latency)
	}
	samples := append([]int64(nil), metrics.latencies...)
	require.Equal(t, int64(30), promptAuditPercentile(samples, 0.50))
	require.Equal(t, int64(40), promptAuditPercentile(samples, 0.95))
	require.Equal(t, int64(40), promptAuditPercentile(samples, 0.99))
}

func TestPromptAuditIssueSummariesAreDerivedAndRedacted(t *testing.T) {
	const canary = "PROMPT_CANARY_EVIDENCE_SECRET"
	summaries := buildPromptAuditIssueSummaries(
		[]string{"pii"}, []string{"pii"}, []string{"unknown:1234"}, "critical", "Block",
		map[string]float64{"pii": 1}, map[string]string{"pii": canary},
	)
	require.Len(t, summaries, 2)
	raw, err := json.Marshal(summaries)
	require.NoError(t, err)
	require.NotContains(t, string(raw), canary)
	require.Contains(t, string(raw), "evidence_hash")
	require.Contains(t, string(raw), "description")
}
