package model

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func usePromptAuditTestDB(t *testing.T) {
	t.Helper()
	previousDB := DB
	dsn := fmt.Sprintf("file:prompt-audit-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	require.NoError(t, db.AutoMigrate(&Option{}, &PromptAuditQueueLock{}, &PromptAuditJob{}, &PromptAuditEvent{}))
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		_ = sqlDB.Close()
	})
}

func TestPromptAuditQueueCapacityIsAtomic(t *testing.T) {
	usePromptAuditTestDB(t)
	var created atomic.Int64
	var wg sync.WaitGroup
	for index := 0; index < 16; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			job := &PromptAuditJob{RequestId: fmt.Sprintf("req-%d", index), SnapshotJSON: `{}`}
			if CreatePromptAuditJob(job, 3) == nil {
				created.Add(1)
			}
		}(index)
	}
	wg.Wait()
	require.Equal(t, int64(3), created.Load())
	stats, err := PromptAuditQueueStatistics()
	require.NoError(t, err)
	require.Equal(t, int64(3), stats.Active)
}

func TestClaimPromptAuditJobsIsAtomicAndPrioritizesDueRetries(t *testing.T) {
	usePromptAuditTestDB(t)
	now := time.Now().Unix()
	jobs := []PromptAuditJob{
		{Status: PromptAuditJobDone, CreatedAt: now - 10, UpdatedAt: now - 10},
		{Status: PromptAuditJobQueued, CreatedAt: now, UpdatedAt: now, MaxAttempts: 3},
		{Status: PromptAuditJobRetry, CreatedAt: now, UpdatedAt: now, MaxAttempts: 3, NextAttemptAt: now + 60},
		{Status: PromptAuditJobRetry, CreatedAt: now, UpdatedAt: now, MaxAttempts: 3, NextAttemptAt: now - 1},
	}
	require.NoError(t, DB.Create(&jobs).Error)

	claimed, ok, err := ClaimPromptAuditJob(time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, jobs[3].Id, claimed.Id)
	require.Equal(t, PromptAuditJobProcessing, claimed.Status)
	require.Equal(t, 1, claimed.Attempts)
	require.NotEmpty(t, claimed.ClaimToken)

	claimed, ok, err = ClaimPromptAuditJob(time.Minute)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, jobs[1].Id, claimed.Id)

	claimed, ok, err = ClaimPromptAuditJob(time.Minute)
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, claimed)
}

func TestClaimPromptAuditJobsDoesNotDuplicateConcurrentClaims(t *testing.T) {
	usePromptAuditTestDB(t)
	now := time.Now().Unix()
	jobs := make([]PromptAuditJob, 8)
	for index := range jobs {
		jobs[index] = PromptAuditJob{Status: PromptAuditJobQueued, CreatedAt: now, UpdatedAt: now, MaxAttempts: 3}
	}
	require.NoError(t, DB.Create(&jobs).Error)

	type claimResult struct {
		id      int64
		claimed bool
		err     error
	}
	results := make(chan claimResult, len(jobs))
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	for range jobs {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			job, claimed, err := ClaimPromptAuditJob(time.Minute)
			result := claimResult{claimed: claimed, err: err}
			if job != nil {
				result.id = job.Id
			}
			results <- result
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	unique := make(map[int64]struct{}, len(jobs))
	for result := range results {
		require.NoError(t, result.err)
		require.True(t, result.claimed)
		require.NotZero(t, result.id)
		unique[result.id] = struct{}{}
	}
	require.Len(t, unique, len(jobs))
}

func TestPromptAuditClaimLockUsesSupportedDialectSemantics(t *testing.T) {
	dummyDB, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	buildSQL := func() string {
		var job PromptAuditJob
		return lockPromptAuditJobForClaim(dummyDB).Where("id = ?", 1).Find(&job).Statement.SQL.String()
	}

	t.Cleanup(func() {
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	})
	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypeSQLite)
	assert.Contains(t, buildSQL(), "FOR UPDATE SKIP LOCKED")

	common.SetDatabaseTypes(common.DatabaseTypeMySQL, common.DatabaseTypeSQLite)
	mysqlSQL := buildSQL()
	assert.Contains(t, mysqlSQL, "FOR UPDATE")
	assert.NotContains(t, mysqlSQL, "SKIP LOCKED")

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	assert.NotContains(t, buildSQL(), "FOR UPDATE")
}

func TestPromptAuditFilterDeleteUsesSnapshotAndRemovesOrphanJobs(t *testing.T) {
	usePromptAuditTestDB(t)
	now := time.Now().Unix()
	jobIDs := make([]int64, 0, 2)
	for index := 1; index <= 2; index++ {
		job := PromptAuditJob{Status: PromptAuditJobDone, CreatedAt: now, UpdatedAt: now}
		require.NoError(t, DB.Create(&job).Error)
		jobIDs = append(jobIDs, job.Id)
		event := PromptAuditEvent{CreatedAt: now, JobId: &job.Id, Decision: "critical", RiskLevel: "critical", GroupName: "strict", PromptHash: fmt.Sprintf("hash-%d", index)}
		require.NoError(t, DB.Create(&event).Error)
	}
	filter := PromptAuditEventFilter{Decision: " critical ", Group: "strict", StartTime: now - 60, EndTime: now + 60}
	preview, err := PromptAuditDeletePreviewForFilter(filter)
	require.NoError(t, err)
	require.Equal(t, int64(2), preview.MatchedCount)
	require.Len(t, preview.FilterHash, 64)
	require.Equal(t, preview.FilterHash, PromptAuditFilterHash(preview.FilterSummary, preview.SnapshotMaxID))
	deleted, err := DeletePromptAuditEventsByFilter(filter, preview.SnapshotMaxID)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted.DeletedEvents)
	require.Equal(t, int64(2), deleted.DeletedJobs)
	require.Equal(t, jobIDs, deleted.JobIDs)
}

func TestUpdateJSONOptionCAS(t *testing.T) {
	usePromptAuditTestDB(t)
	previous := setting.PromptAuditConfigJSON
	t.Cleanup(func() { setting.PromptAuditConfigJSON = previous })
	config := setting.DefaultPromptAuditStorageConfig()
	config.ConfigVersion = 2
	raw, err := json.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, UpdateJSONOptionCAS(setting.PromptAuditConfigOptionKey, 1, string(raw)))
	require.ErrorIs(t, UpdateJSONOptionCAS(setting.PromptAuditConfigOptionKey, 1, string(raw)), ErrOptionVersionConflict)
}

func TestRecordBlockingPromptAuditResultAlwaysCreatesDoneJob(t *testing.T) {
	usePromptAuditTestDB(t)
	job := &PromptAuditJob{RequestId: "blocking-pass", SnapshotJSON: `{"redacted_preview":"***"}`}
	event := &PromptAuditEvent{RequestId: "blocking-pass", Decision: "pass", RiskLevel: "low"}
	require.NoError(t, RecordBlockingPromptAuditResult(job, event, false))
	require.NotZero(t, job.Id)
	require.Equal(t, PromptAuditJobDone, job.Status)
	require.Equal(t, "blocking", job.ExecutionMode)
	var eventCount int64
	require.NoError(t, DB.Model(&PromptAuditEvent{}).Count(&eventCount).Error)
	require.Zero(t, eventCount)

	riskJob := &PromptAuditJob{RequestId: "blocking-risk", SnapshotJSON: `{"redacted_preview":"***"}`}
	riskEvent := &PromptAuditEvent{RequestId: "blocking-risk", Decision: "critical", RiskLevel: "critical"}
	require.NoError(t, RecordBlockingPromptAuditResult(riskJob, riskEvent, false))
	require.NotNil(t, riskEvent.JobId)
	require.Equal(t, riskJob.Id, *riskEvent.JobId)
}

func TestReclaimPromptAuditJobsHandlesStagingAndExhaustedLeases(t *testing.T) {
	usePromptAuditTestDB(t)
	now := time.Now().Unix()
	jobs := []PromptAuditJob{
		{Status: PromptAuditJobStaging, CreatedAt: now - 300, UpdatedAt: now - 300, MaxAttempts: 3},
		{Status: PromptAuditJobProcessing, CreatedAt: now - 300, UpdatedAt: now - 300, Attempts: 1, MaxAttempts: 3, LeaseUntil: now - 1, ClaimToken: "retry"},
		{Status: PromptAuditJobProcessing, CreatedAt: now - 300, UpdatedAt: now - 300, Attempts: 3, MaxAttempts: 3, LeaseUntil: now - 1, ClaimToken: "failed"},
	}
	require.NoError(t, DB.Create(&jobs).Error)
	reclaimed, err := ReclaimPromptAuditJobs()
	require.NoError(t, err)
	require.Equal(t, int64(3), reclaimed)
	for index := range jobs {
		require.NoError(t, DB.First(&jobs[index], jobs[index].Id).Error)
	}
	require.Equal(t, PromptAuditJobFailed, jobs[0].Status)
	require.Equal(t, "staging_timeout", jobs[0].ErrorCode)
	require.Equal(t, PromptAuditJobRetry, jobs[1].Status)
	require.Empty(t, jobs[1].ClaimToken)
	require.Equal(t, PromptAuditJobFailed, jobs[2].Status)
}
