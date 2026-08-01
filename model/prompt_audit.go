package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrPromptAuditQueueFull = errors.New("prompt audit queue is full")

const (
	PromptAuditJobStaging    = "staging"
	PromptAuditJobQueued     = "queued"
	PromptAuditJobProcessing = "processing"
	PromptAuditJobRetry      = "retry"
	PromptAuditJobDone       = "done"
	PromptAuditJobFailed     = "failed"
)

var promptAuditQueueAdmission = func() chan struct{} {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return gate
}()

var promptAuditJobClaimAdmission = func() chan struct{} {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return gate
}()

var errPromptAuditJobClaimConflict = errors.New("prompt audit job claim conflict")

type PromptAuditQueueLock struct {
	Id int `gorm:"primaryKey"`
}

func (PromptAuditQueueLock) TableName() string { return "prompt_audit_queue_locks" }

type PromptAuditJob struct {
	Id            int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	CreatedAt     int64  `json:"created_at" gorm:"index"`
	UpdatedAt     int64  `json:"updated_at"`
	Status        string `json:"status" gorm:"size:24;index:idx_prompt_audit_jobs_schedule,priority:1"`
	Attempts      int    `json:"attempts"`
	MaxAttempts   int    `json:"max_attempts"`
	NextAttemptAt int64  `json:"next_attempt_at" gorm:"index:idx_prompt_audit_jobs_schedule,priority:2"`
	LeaseUntil    int64  `json:"lease_until" gorm:"index"`
	ClaimToken    string `json:"-" gorm:"size:64;index"`
	ExecutionMode string `json:"execution_mode" gorm:"size:24;index"`
	ConfigVersion int64  `json:"config_version"`
	RequestId     string `json:"request_id" gorm:"size:128;index"`
	UserId        int    `json:"user_id" gorm:"index"`
	TokenId       int    `json:"token_id" gorm:"index"`
	GroupName     string `json:"group" gorm:"column:group_name;size:128;index"`
	PromptHash    string `json:"prompt_hash" gorm:"size:64;index"`
	SnapshotJSON  string `json:"-" gorm:"type:text"`
	ErrorCode     string `json:"error_code,omitempty" gorm:"size:64"`
	ErrorMessage  string `json:"error_message,omitempty" gorm:"type:text"`
}

func (PromptAuditJob) TableName() string { return "prompt_audit_jobs" }

type PromptAuditEvent struct {
	Id                    int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	CreatedAt             int64  `json:"created_at" gorm:"index"`
	JobId                 *int64 `json:"job_id,omitempty" gorm:"uniqueIndex"`
	RequestId             string `json:"request_id" gorm:"size:128;index"`
	UserId                int    `json:"user_id" gorm:"index"`
	Username              string `json:"username" gorm:"size:128"`
	UserEmail             string `json:"user_email" gorm:"size:255"`
	TokenId               int    `json:"token_id" gorm:"index"`
	TokenName             string `json:"token_name" gorm:"size:128"`
	GroupName             string `json:"group" gorm:"column:group_name;size:128;index"`
	Provider              string `json:"provider" gorm:"size:128"`
	Endpoint              string `json:"endpoint" gorm:"size:255"`
	Protocol              string `json:"protocol" gorm:"size:64"`
	Model                 string `json:"model" gorm:"size:255;index"`
	PromptHash            string `json:"prompt_hash" gorm:"size:64;index"`
	RedactedPreview       string `json:"redacted_preview" gorm:"type:text"`
	FullPrompt            string `json:"full_prompt" gorm:"type:text"`
	PromptLength          int    `json:"prompt_length"`
	MessageCount          int    `json:"message_count"`
	Stage                 string `json:"stage" gorm:"size:32"`
	Decision              string `json:"decision" gorm:"size:24;index"`
	RiskLevel             string `json:"risk_level" gorm:"size:24;index"`
	Action                string `json:"action" gorm:"size:24"`
	Safety                string `json:"safety" gorm:"size:24"`
	CategoriesJSON        string `json:"-" gorm:"type:text"`
	MatchedScannersJSON   string `json:"-" gorm:"type:text"`
	UnknownCategoriesJSON string `json:"-" gorm:"type:text"`
	ScannerScoresJSON     string `json:"-" gorm:"type:text"`
	ScannerEvidenceJSON   string `json:"-" gorm:"type:text"`
	ScannerBackend        string `json:"scanner_backend" gorm:"size:64"`
	ScannerVersion        string `json:"scanner_version" gorm:"size:255"`
	GuardEndpointId       string `json:"guard_endpoint_id" gorm:"size:128"`
	ConfigVersion         int64  `json:"config_version"`
	PolicyId              string `json:"policy_id" gorm:"size:64"`
	PolicyVersion         int    `json:"policy_version"`
	ChunkTotal            int    `json:"chunk_total"`
	LatencyMS             int64  `json:"latency_ms"`
	ErrorCode             string `json:"error_code,omitempty" gorm:"size:64"`
}

func (PromptAuditEvent) TableName() string { return "prompt_audit_events" }

type PromptAuditEventFilter struct {
	Decision   string `json:"decision"`
	RiskLevel  string `json:"risk_level"`
	Group      string `json:"group"`
	UserId     int    `json:"user_id"`
	TokenId    int    `json:"token_id"`
	Model      string `json:"model"`
	RequestId  string `json:"request_id"`
	PromptHash string `json:"prompt_hash"`
	Endpoint   string `json:"endpoint"`
	Keyword    string `json:"keyword"`
	StartTime  int64  `json:"start_time"`
	EndTime    int64  `json:"end_time"`
}

type PromptAuditQueueStats struct {
	Staging    int64 `json:"staging"`
	Queued     int64 `json:"queued"`
	Processing int64 `json:"processing"`
	Retry      int64 `json:"retry"`
	Done       int64 `json:"done"`
	Failed     int64 `json:"failed"`
	Active     int64 `json:"active"`
}

type PromptAuditDeletePreview struct {
	MatchedCount  int64                  `json:"matched_count"`
	FilterSummary PromptAuditEventFilter `json:"filter_summary"`
	SnapshotMaxID int64                  `json:"snapshot_max_id"`
	FilterHash    string                 `json:"filter_hash"`
}

type PromptAuditDeleteResult struct {
	DeletedEvents int64   `json:"deleted_events"`
	DeletedJobs   int64   `json:"deleted_jobs"`
	JobIDs        []int64 `json:"-"`
}

func CreatePromptAuditJob(job *PromptAuditJob, queueCapacity int) error {
	return CreatePromptAuditJobContext(context.Background(), job, queueCapacity)
}

func CreatePromptAuditJobContext(ctx context.Context, job *PromptAuditJob, queueCapacity int) error {
	if job == nil {
		return errors.New("prompt audit job is required")
	}
	if queueCapacity < 1 {
		return ErrPromptAuditQueueFull
	}
	now := common.GetTimestamp()
	job.CreatedAt, job.UpdatedAt = now, now
	job.Status, job.MaxAttempts = PromptAuditJobStaging, 3
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-promptAuditQueueAdmission:
	}
	defer func() { promptAuditQueueAdmission <- struct{}{} }()
	db := DB.WithContext(ctx)
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&PromptAuditQueueLock{Id: 1}).Error; err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var admissionLock PromptAuditQueueLock
		if err := lockForUpdate(tx).First(&admissionLock, 1).Error; err != nil {
			return err
		}
		var active int64
		if err := tx.Model(&PromptAuditJob{}).
			Where("status IN ?", []string{PromptAuditJobStaging, PromptAuditJobQueued, PromptAuditJobProcessing, PromptAuditJobRetry}).
			Count(&active).Error; err != nil {
			return err
		}
		if active >= int64(queueCapacity) {
			return ErrPromptAuditQueueFull
		}
		return tx.Create(job).Error
	})
}

func PublishPromptAuditJob(id int64) error {
	return PublishPromptAuditJobContext(context.Background(), id)
}

func PublishPromptAuditJobContext(ctx context.Context, id int64) error {
	result := DB.WithContext(ctx).Model(&PromptAuditJob{}).Where("id = ? AND status = ?", id, PromptAuditJobStaging).
		Updates(map[string]any{"status": PromptAuditJobQueued, "updated_at": common.GetTimestamp()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("prompt audit staging job is unavailable")
	}
	return nil
}

func MarkPromptAuditJobFailed(id int64, code, message string) error {
	return MarkPromptAuditJobFailedContext(context.Background(), id, code, message)
}

func MarkPromptAuditJobFailedContext(ctx context.Context, id int64, code, message string) error {
	return DB.WithContext(ctx).Model(&PromptAuditJob{}).Where("id = ?", id).Updates(map[string]any{
		"status": PromptAuditJobFailed, "updated_at": common.GetTimestamp(),
		"error_code": strings.TrimSpace(code), "error_message": stablePromptAuditError(message),
	}).Error
}

func ClaimPromptAuditJob(lease time.Duration) (*PromptAuditJob, bool, error) {
	return ClaimPromptAuditJobContext(context.Background(), lease)
}

func ClaimPromptAuditJobContext(ctx context.Context, lease time.Duration) (*PromptAuditJob, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	case <-promptAuditJobClaimAdmission:
	}
	defer func() { promptAuditJobClaimAdmission <- struct{}{} }()

	now := common.GetTimestamp()
	leaseUntil := now + int64(lease.Seconds())
	for attempt := 0; attempt < 8; attempt++ {
		var claimedJob *PromptAuditJob
		err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			candidate, err := nextPromptAuditJobForClaim(tx, now)
			if err != nil {
				return err
			}
			claimToken := common.GetRandomString(32)
			query := tx.Model(&PromptAuditJob{}).Where("id = ? AND status = ?", candidate.Id, candidate.Status)
			if candidate.Status == PromptAuditJobRetry {
				query = query.Where("next_attempt_at <= ?", now)
			}
			result := query.Updates(map[string]any{
				"status": PromptAuditJobProcessing, "claim_token": claimToken,
				"lease_until": leaseUntil, "attempts": gorm.Expr("attempts + 1"), "updated_at": now,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errPromptAuditJobClaimConflict
			}
			candidate.Status = PromptAuditJobProcessing
			candidate.ClaimToken = claimToken
			candidate.LeaseUntil = leaseUntil
			candidate.Attempts++
			candidate.UpdatedAt = now
			claimedJob = candidate
			return nil
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		if errors.Is(err, errPromptAuditJobClaimConflict) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		return claimedJob, true, nil
	}
	return nil, false, nil
}

func nextPromptAuditJobForClaim(tx *gorm.DB, now int64) (*PromptAuditJob, error) {
	// Keep retries and fresh jobs as separate schedule-index scans. Combining
	// them with OR plus ORDER BY id makes PostgreSQL scan the primary key from
	// the oldest terminal job, which becomes prohibitively slow as history grows.
	var candidate PromptAuditJob
	result := lockPromptAuditJobForClaim(tx).
		Where("status = ? AND next_attempt_at <= ?", PromptAuditJobRetry, now).
		Order("next_attempt_at ASC").Order("id ASC").Limit(1).Find(&candidate)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 1 {
		return &candidate, nil
	}

	result = lockPromptAuditJobForClaim(tx).
		Where("status = ?", PromptAuditJobQueued).
		Order("next_attempt_at ASC").Order("id ASC").Limit(1).Find(&candidate)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &candidate, nil
}

func lockPromptAuditJobForClaim(tx *gorm.DB) *gorm.DB {
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		// PostgreSQL 9.6+ can let concurrent workers claim different rows instead
		// of waiting on the oldest available job. MySQL 5.7 uses the portable lock.
		return tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	}
	return lockForUpdate(tx)
}

func RefreshPromptAuditJobLease(id int64, claimToken string, lease time.Duration) error {
	result := DB.Model(&PromptAuditJob{}).
		Where("id = ? AND status = ? AND claim_token = ?", id, PromptAuditJobProcessing, claimToken).
		Updates(map[string]any{"lease_until": common.GetTimestamp() + int64(lease.Seconds()), "updated_at": common.GetTimestamp()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("prompt audit job lease lost")
	}
	return nil
}

func CompletePromptAuditJob(job *PromptAuditJob, event *PromptAuditEvent, store bool) error {
	if job == nil {
		return errors.New("prompt audit job is required")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if store && event != nil {
			jobID := job.Id
			event.JobId = &jobID
			if event.CreatedAt == 0 {
				event.CreatedAt = common.GetTimestamp()
			}
			if err := tx.Create(event).Error; err != nil {
				return err
			}
		}
		result := tx.Model(&PromptAuditJob{}).
			Where("id = ? AND status = ? AND claim_token = ?", job.Id, PromptAuditJobProcessing, job.ClaimToken).
			Updates(map[string]any{"status": PromptAuditJobDone, "claim_token": "", "lease_until": 0, "updated_at": common.GetTimestamp(), "error_code": "", "error_message": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("prompt audit job lease lost")
		}
		return nil
	})
}

func RetryPromptAuditJob(job *PromptAuditJob, code, message string, retryable bool) error {
	if job == nil {
		return errors.New("prompt audit job is required")
	}
	status := PromptAuditJobFailed
	next := int64(0)
	if retryable && job.Attempts < job.MaxAttempts {
		status = PromptAuditJobRetry
		delays := []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}
		index := job.Attempts - 1
		if index < 0 {
			index = 0
		}
		if index >= len(delays) {
			index = len(delays) - 1
		}
		next = common.GetTimestamp() + int64(delays[index].Seconds())
	}
	result := DB.Model(&PromptAuditJob{}).
		Where("id = ? AND status = ? AND claim_token = ?", job.Id, PromptAuditJobProcessing, job.ClaimToken).
		Updates(map[string]any{
			"status": status, "next_attempt_at": next, "claim_token": "", "lease_until": 0,
			"updated_at": common.GetTimestamp(), "error_code": strings.TrimSpace(code), "error_message": stablePromptAuditError(message),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("prompt audit job lease lost")
	}
	return nil
}

func ReclaimPromptAuditJobs() (int64, error) {
	now := common.GetTimestamp()
	var reclaimed int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		staging := tx.Model(&PromptAuditJob{}).
			Where("status = ? AND updated_at < ?", PromptAuditJobStaging, now-120).
			Updates(map[string]any{
				"status": PromptAuditJobFailed, "updated_at": now,
				"error_code": "staging_timeout", "error_message": "prompt audit staging job expired",
			})
		if staging.Error != nil {
			return staging.Error
		}
		reclaimed += staging.RowsAffected

		retry := tx.Model(&PromptAuditJob{}).
			Where("status = ? AND lease_until > 0 AND lease_until < ? AND attempts < max_attempts", PromptAuditJobProcessing, now).
			Updates(map[string]any{
				"status": PromptAuditJobRetry, "next_attempt_at": now, "claim_token": "", "lease_until": 0,
				"updated_at": now, "error_code": "processing_lease_expired", "error_message": "prompt audit worker lease expired",
			})
		if retry.Error != nil {
			return retry.Error
		}
		reclaimed += retry.RowsAffected

		failed := tx.Model(&PromptAuditJob{}).
			Where("status = ? AND lease_until > 0 AND lease_until < ? AND attempts >= max_attempts", PromptAuditJobProcessing, now).
			Updates(map[string]any{
				"status": PromptAuditJobFailed, "claim_token": "", "lease_until": 0,
				"updated_at": now, "error_code": "processing_lease_expired", "error_message": "prompt audit worker lease expired",
			})
		if failed.Error != nil {
			return failed.Error
		}
		reclaimed += failed.RowsAffected
		return nil
	})
	return reclaimed, err
}

func PromptAuditQueueStatistics() (PromptAuditQueueStats, error) {
	var rows []struct {
		Status string
		Count  int64
	}
	if err := DB.Model(&PromptAuditJob{}).Select("status, count(*) AS count").Group("status").Scan(&rows).Error; err != nil {
		return PromptAuditQueueStats{}, err
	}
	stats := PromptAuditQueueStats{}
	for _, row := range rows {
		switch row.Status {
		case PromptAuditJobStaging:
			stats.Staging = row.Count
		case PromptAuditJobQueued:
			stats.Queued = row.Count
		case PromptAuditJobProcessing:
			stats.Processing = row.Count
		case PromptAuditJobRetry:
			stats.Retry = row.Count
		case PromptAuditJobDone:
			stats.Done = row.Count
		case PromptAuditJobFailed:
			stats.Failed = row.Count
		}
	}
	stats.Active = stats.Staging + stats.Queued + stats.Processing + stats.Retry
	return stats, nil
}

func RecordBlockingPromptAuditResult(job *PromptAuditJob, event *PromptAuditEvent, store bool) error {
	if job == nil || event == nil {
		return errors.New("prompt audit blocking result is required")
	}
	if DB == nil {
		return errors.New("prompt audit database is unavailable")
	}
	now := common.GetTimestamp()
	job.CreatedAt, job.UpdatedAt = now, now
	job.Status, job.Attempts, job.MaxAttempts = PromptAuditJobDone, 1, 1
	job.ExecutionMode = "blocking"
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		if !store {
			return nil
		}
		event.JobId = &job.Id
		if event.CreatedAt == 0 {
			event.CreatedAt = now
		}
		return tx.Create(event).Error
	})
}

func ListPromptAuditEvents(filter PromptAuditEventFilter, page, pageSize int) ([]PromptAuditEvent, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	query := applyPromptAuditEventFilter(DB.Model(&PromptAuditEvent{}), filter)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var events []PromptAuditEvent
	err := query.Omit("full_prompt").Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&events).Error
	return events, total, err
}

func GetPromptAuditEvent(id int64) (*PromptAuditEvent, error) {
	var event PromptAuditEvent
	if err := DB.First(&event, id).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

func DeletePromptAuditEvents(ids []int64) (PromptAuditDeleteResult, error) {
	ids = canonicalPromptAuditIDs(ids)
	if len(ids) == 0 {
		return PromptAuditDeleteResult{}, nil
	}
	if len(ids) > 500 {
		return PromptAuditDeleteResult{}, errors.New("prompt audit delete batch exceeds 500 events")
	}
	result := PromptAuditDeleteResult{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		jobIDs := make([]int64, 0, len(ids))
		if err := tx.Model(&PromptAuditEvent{}).Where("id IN ? AND job_id IS NOT NULL", ids).Distinct().Pluck("job_id", &jobIDs).Error; err != nil {
			return err
		}
		deleted := tx.Where("id IN ?", ids).Delete(&PromptAuditEvent{})
		if deleted.Error != nil {
			return deleted.Error
		}
		result.DeletedEvents = deleted.RowsAffected
		result.JobIDs = canonicalPromptAuditIDs(jobIDs)
		if len(jobIDs) > 0 {
			orphaned := tx.Where("id IN ? AND status <> ? AND NOT EXISTS (?)", jobIDs, PromptAuditJobProcessing,
				tx.Model(&PromptAuditEvent{}).Select("1").Where("prompt_audit_events.job_id = prompt_audit_jobs.id")).Delete(&PromptAuditJob{})
			if orphaned.Error != nil {
				return orphaned.Error
			}
			result.DeletedJobs = orphaned.RowsAffected
		}
		return nil
	})
	return result, err
}

func DeletePromptAuditEventsByFilter(filter PromptAuditEventFilter, maxID int64) (PromptAuditDeleteResult, error) {
	if err := validatePromptAuditDeleteFilter(filter); err != nil {
		return PromptAuditDeleteResult{}, err
	}
	if maxID <= 0 {
		return PromptAuditDeleteResult{}, nil
	}
	total := PromptAuditDeleteResult{}
	for {
		var ids []int64
		query := applyPromptAuditEventFilter(DB.Model(&PromptAuditEvent{}), filter).Where("id <= ?", maxID)
		if err := query.Order("id ASC").Limit(200).Pluck("id", &ids).Error; err != nil {
			return total, err
		}
		if len(ids) == 0 {
			total.JobIDs = canonicalPromptAuditIDs(total.JobIDs)
			return total, nil
		}
		deleted, err := DeletePromptAuditEvents(ids)
		if err != nil {
			return total, err
		}
		total.DeletedEvents += deleted.DeletedEvents
		total.DeletedJobs += deleted.DeletedJobs
		total.JobIDs = append(total.JobIDs, deleted.JobIDs...)
		if len(ids) < 200 {
			total.JobIDs = canonicalPromptAuditIDs(total.JobIDs)
			return total, nil
		}
	}
}

func PromptAuditDeletePreviewForFilter(filter PromptAuditEventFilter) (PromptAuditDeletePreview, error) {
	filter = canonicalPromptAuditEventFilter(filter)
	if err := validatePromptAuditDeleteFilter(filter); err != nil {
		return PromptAuditDeletePreview{}, err
	}
	preview := PromptAuditDeletePreview{FilterSummary: filter}
	query := applyPromptAuditEventFilter(DB.Model(&PromptAuditEvent{}), filter)
	if err := query.Count(&preview.MatchedCount).Error; err != nil {
		return PromptAuditDeletePreview{}, err
	}
	if preview.MatchedCount > 0 {
		if err := query.Select("max(id)").Scan(&preview.SnapshotMaxID).Error; err != nil {
			return PromptAuditDeletePreview{}, err
		}
	}
	preview.FilterHash = PromptAuditFilterHash(filter, preview.SnapshotMaxID)
	return preview, nil
}

func applyPromptAuditEventFilter(query *gorm.DB, filter PromptAuditEventFilter) *gorm.DB {
	filter = canonicalPromptAuditEventFilter(filter)
	if filter.Decision != "" {
		query = query.Where("decision = ?", filter.Decision)
	}
	if filter.RiskLevel != "" {
		query = query.Where("risk_level = ?", filter.RiskLevel)
	}
	if filter.Group != "" {
		query = query.Where("group_name = ?", filter.Group)
	}
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.TokenId > 0 {
		query = query.Where("token_id = ?", filter.TokenId)
	}
	if filter.Model != "" {
		query = query.Where("model = ?", filter.Model)
	}
	if filter.RequestId != "" {
		query = query.Where("request_id = ?", filter.RequestId)
	}
	if filter.PromptHash != "" {
		query = query.Where("prompt_hash = ?", strings.ToLower(strings.TrimSpace(filter.PromptHash)))
	}
	if filter.Endpoint != "" {
		query = query.Where("endpoint = ?", strings.TrimSpace(filter.Endpoint))
	}
	if filter.Keyword != "" {
		keyword := "%" + strings.TrimSpace(filter.Keyword) + "%"
		query = query.Where("request_id LIKE ? OR prompt_hash LIKE ? OR redacted_preview LIKE ? OR username LIKE ? OR user_email LIKE ? OR token_name LIKE ?", keyword, keyword, keyword, keyword, keyword, keyword)
	}
	if filter.StartTime > 0 {
		query = query.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("created_at <= ?", filter.EndTime)
	}
	return query
}

func PromptAuditFilterHash(filter PromptAuditEventFilter, maxID int64) string {
	payload := struct {
		Filter        PromptAuditEventFilter `json:"filter"`
		SnapshotMaxID int64                  `json:"snapshot_max_id"`
	}{Filter: canonicalPromptAuditEventFilter(filter), SnapshotMaxID: maxID}
	raw, _ := json.Marshal(payload)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func canonicalPromptAuditEventFilter(filter PromptAuditEventFilter) PromptAuditEventFilter {
	filter.Decision = strings.ToLower(strings.TrimSpace(filter.Decision))
	filter.RiskLevel = strings.ToLower(strings.TrimSpace(filter.RiskLevel))
	filter.Group = strings.TrimSpace(filter.Group)
	filter.Model = strings.TrimSpace(filter.Model)
	filter.RequestId = strings.TrimSpace(filter.RequestId)
	filter.PromptHash = strings.ToLower(strings.TrimSpace(filter.PromptHash))
	filter.Endpoint = strings.TrimSpace(filter.Endpoint)
	filter.Keyword = strings.TrimSpace(filter.Keyword)
	return filter
}

func validatePromptAuditDeleteFilter(filter PromptAuditEventFilter) error {
	if filter.StartTime <= 0 || filter.EndTime <= 0 || filter.StartTime >= filter.EndTime {
		return errors.New("prompt audit filter delete requires a valid explicit time range")
	}
	return nil
}

func canonicalPromptAuditIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func stablePromptAuditError(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "prompt audit processing failed"
	}
	if len(message) > 512 {
		return message[:512]
	}
	return message
}

func PromptAuditJobPayloadKey(id int64) string {
	return fmt.Sprintf("newapi:prompt_audit:payload:%d", id)
}
