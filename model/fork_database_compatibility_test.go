package model

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Run only against disposable databases; normal unit-test runs skip this suite.
// NEWAPI_REBASE_EXISTING=1 verifies a fixture created by an older release.
func TestForkDatabaseCompatibility(t *testing.T) {
	dsn := os.Getenv("NEWAPI_REBASE_DB_DSN")
	if dsn == "" {
		t.Skip("NEWAPI_REBASE_DB_DSN is not configured")
	}
	logDSN := os.Getenv("NEWAPI_REBASE_LOG_DSN")
	validateForkTestDSN(t, dsn)
	if logDSN != "" {
		validateForkTestDSN(t, logDSN)
	}
	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousSQLitePath := common.IsMasterNode, common.SQLitePath
	previousRedis, previousBatch := common.RedisEnabled, common.BatchUpdateEnabled
	common.IsMasterNode = true
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	t.Setenv("SQL_DSN", dsn)
	t.Setenv("LOG_SQL_DSN", logDSN)
	t.Setenv("SQL_MAX_OPEN_CONNS", "4")
	t.Setenv("SQL_MAX_IDLE_CONNS", "4")
	t.Setenv("SKIP_64BIT_QUOTA_SCHEMA_CHECK", "false")
	if dsn == "local" {
		common.SQLitePath = os.Getenv("NEWAPI_REBASE_SQLITE_PATH")
		if common.SQLitePath == "" {
			common.SQLitePath = filepath.Join(t.TempDir(), "newapi_rebase.sqlite")
		}
		require.Contains(t, filepath.Base(common.SQLitePath), "newapi_rebase")
		t.Setenv("SQL_MAX_OPEN_CONNS", "1")
		t.Setenv("SQL_MAX_IDLE_CONNS", "1")
	}
	t.Cleanup(func() {
		if DB != previousDB {
			_ = CloseDB()
		}
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.IsMasterNode, common.SQLitePath = previousMaster, previousSQLitePath
		common.RedisEnabled, common.BatchUpdateEnabled = previousRedis, previousBatch
		initCol()
	})
	require.NoError(t, InitDB(), "first production startup/migration")
	require.NoError(t, InitLogDB())
	var version string
	versionSQL := "SELECT version()"
	if dsn == "local" {
		versionSQL = "SELECT sqlite_version()"
	}
	require.NoError(t, DB.Raw(versionSQL).Scan(&version).Error)
	t.Logf("engine=%s version=%s existing=%s", common.MainDatabaseType(), version, os.Getenv("NEWAPI_REBASE_EXISTING"))
	if os.Getenv("NEWAPI_REBASE_EXISTING") != "1" {
		seedForkCompatibilityRows(t)
	}
	verifyForkCompatibilityRows(t)
	mainSchema := forkSchemaSnapshot(t, DB, "users", "tokens", "invite_codes", "checkins", "prompt_audit_jobs", "prompt_audit_events", "content_moderation_logs")
	logSchema := forkSchemaSnapshot(t, LOG_DB, "logs")
	beforeMain, beforeLog := DB, LOG_DB
	require.NoError(t, InitDB(), "second production startup/migration")
	require.NoError(t, InitLogDB())
	if sqlDB, err := beforeMain.DB(); err == nil {
		require.NoError(t, sqlDB.Close())
	}
	if beforeLog != beforeMain {
		if sqlDB, err := beforeLog.DB(); err == nil {
			require.NoError(t, sqlDB.Close())
		}
	}
	verifyForkCompatibilityRows(t)
	assert.Equal(t, mainSchema, forkSchemaSnapshot(t, DB, "users", "tokens", "invite_codes", "checkins", "prompt_audit_jobs", "prompt_audit_events", "content_moderation_logs"))
	assert.Equal(t, logSchema, forkSchemaSnapshot(t, LOG_DB, "logs"))
	recorder := &migrationSQLRecorder{}
	DB = DB.Session(&gorm.Session{Logger: recorder})
	require.NoError(t, migrateDB(), "idempotent main migration")
	t.Logf("repeat main migration DDL statements=%d", len(recorder.schemaMutations()))
	assert.Equal(t, mainSchema, forkSchemaSnapshot(t, DB, "users", "tokens", "invite_codes", "checkins", "prompt_audit_jobs", "prompt_audit_events", "content_moderation_logs"))
	LOG_DB = LOG_DB.Session(&gorm.Session{Logger: recorder})
	recorder.reset()
	require.NoError(t, migrateLOGDB(), "idempotent separate log migration")
	t.Logf("repeat log migration DDL statements=%d", len(recorder.schemaMutations()))
	assert.Equal(t, logSchema, forkSchemaSnapshot(t, LOG_DB, "logs"))
	t.Run("uniqueness_and_nullable_audit_events", func(t *testing.T) {
		duplicate := Checkin{UserId: 9100, CheckinDate: "2026-01-01", QuotaAwarded: 99}
		require.Error(t, DB.Create(&duplicate).Error)
		invite := InviteCode{Code: "UNIQUE-CODE"}
		require.NoError(t, DB.Create(&invite).Error)
		require.Error(t, DB.Create(&InviteCode{Code: "UNIQUE-CODE"}).Error)
		job := PromptAuditJob{Status: PromptAuditJobDone}
		require.NoError(t, DB.Create(&job).Error)
		require.NoError(t, DB.Create(&PromptAuditEvent{JobId: &job.Id}).Error)
		require.Error(t, DB.Create(&PromptAuditEvent{JobId: &job.Id}).Error)
		for range 2 {
			require.NoError(t, DB.Create(&PromptAuditEvent{}).Error)
		}
		moderation := ContentModerationLog{InputExcerpt: "中文审核", Flagged: true, CategoryScoresJSON: `{"violence":0.75}`}
		require.NoError(t, CreateContentModerationLog(t.Context(), &moderation))
		var restored ContentModerationLog
		require.NoError(t, DB.First(&restored, moderation.Id).Error)
		assert.Equal(t, moderation.InputExcerpt, restored.InputExcerpt)
		assert.Equal(t, moderation.CategoryScoresJSON, restored.CategoryScoresJSON)
		assert.True(t, restored.Flagged)
		assert.Nil(t, restored.UpstreamLatencyMS)
	})

	t.Run("invite_consumed_once", func(t *testing.T) {
		invite := InviteCode{Code: "MATRIX-RACE", Source: "matrix", IssuedTo: "race", ExpiresAt: common.GetTimestamp() + 3600}
		require.NoError(t, DB.Create(&invite).Error)
		results := runForkConcurrent(4, func(index int) error {
			return DB.Transaction(func(tx *gorm.DB) error {
				_, err := ConsumeInviteCodeTx(tx, "MATRIX-RACE", 9200+index, fmt.Sprintf("race-%d", index))
				return err
			})
		})
		successes := 0
		for _, err := range results {
			if err == nil {
				successes++
			} else {
				assert.Contains(t, err.Error(), "邀请码已被使用")
			}
		}
		require.Equal(t, 1, successes)
		require.NoError(t, DB.First(&invite, invite.Id).Error)
		require.Positive(t, invite.UsedAt)
	})
	t.Run("checkin_awards_once", func(t *testing.T) {
		setting := operation_setting.GetCheckinSetting()
		previous := *setting
		t.Cleanup(func() { *setting = previous })
		setting.Enabled, setting.Tiered = true, false
		setting.MinQuota, setting.MaxQuota = 17, 17
		user := User{Id: 9300, Username: "matrix-checkin", Password: "fixture", Quota: 100, AffCode: "matrix-checkin"}
		require.NoError(t, DB.Create(&user).Error)
		results := runForkConcurrent(4, func(_ int) error {
			_, err := UserCheckin(user.Id)
			return err
		})
		successes := 0
		for _, err := range results {
			if err == nil {
				successes++
			}
		}
		require.Equal(t, 1, successes)
		require.NoError(t, DB.First(&user, user.Id).Error)
		assert.Equal(t, 117, user.Quota)
		var count int64
		require.NoError(t, DB.Model(&Checkin{}).Where("user_id = ?", user.Id).Count(&count).Error)
		assert.EqualValues(t, 1, count)
	})
	t.Run("audit_queue_and_claims", func(t *testing.T) {
		require.NoError(t, DB.Model(&PromptAuditJob{}).Where("status IN ?", []string{PromptAuditJobStaging, PromptAuditJobQueued, PromptAuditJobRetry, PromptAuditJobProcessing}).Update("status", PromptAuditJobDone).Error)
		results := runForkConcurrent(4, func(index int) error {
			return CreatePromptAuditJob(&PromptAuditJob{RequestId: fmt.Sprintf("matrix-capacity-%d", index)}, 2)
		})
		successes := 0
		for _, err := range results {
			if err == nil {
				successes++
			} else {
				assert.ErrorIs(t, err, ErrPromptAuditQueueFull)
			}
		}
		require.Equal(t, 2, successes)
		require.NoError(t, DB.Model(&PromptAuditJob{}).Where("status = ?", PromptAuditJobStaging).Update("status", PromptAuditJobQueued).Error)
		now := common.GetTimestamp()
		retry := PromptAuditJob{Status: PromptAuditJobRetry, NextAttemptAt: now - 1, MaxAttempts: 3}
		require.NoError(t, DB.Create(&retry).Error)
		claimed, ok, err := ClaimPromptAuditJobContext(t.Context(), time.Minute)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, retry.Id, claimed.Id)
		var idsMu sync.Mutex
		ids := map[int64]bool{}
		results = runForkConcurrent(4, func(_ int) error {
			job, ok, err := ClaimPromptAuditJobContext(t.Context(), time.Minute)
			if err == nil && ok {
				idsMu.Lock()
				defer idsMu.Unlock()
				if ids[job.Id] {
					return fmt.Errorf("job %d claimed twice", job.Id)
				}
				ids[job.Id] = true
			}
			return err
		})
		for _, err := range results {
			require.NoError(t, err)
		}
		require.Len(t, ids, 2)
	})
	if dsn != "local" {
		t.Run("legacy_wallet_schema_guard", func(t *testing.T) {
			alter := "ALTER TABLE users ALTER COLUMN quota TYPE INTEGER"
			restore := "ALTER TABLE users ALTER COLUMN quota TYPE BIGINT"
			if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
				alter = "ALTER TABLE users MODIFY quota INT DEFAULT 0"
				restore = "ALTER TABLE users MODIFY quota BIGINT DEFAULT 0"
			}
			require.NoError(t, DB.Exec(alter).Error)
			require.ErrorContains(t, ensureUserQuotaColumns(DB, common.MainDatabaseType()), "32-bit is not supported")
			require.NoError(t, DB.Exec(restore).Error)
			require.NoError(t, ensureUserQuotaColumns(DB, common.MainDatabaseType()))
			verifyForkCompatibilityRows(t)
		})
	}
}

func validateForkTestDSN(t *testing.T, dsn string) {
	t.Helper()
	if dsn == "local" {
		return
	}
	if strings.HasPrefix(dsn, "postgres") {
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		require.Equal(t, "127.0.0.1", parsed.Hostname())
		require.True(t, strings.HasPrefix(strings.TrimPrefix(parsed.Path, "/"), "newapi_rebase_"))
		return
	}
	parsed, err := mysqldriver.ParseDSN(dsn)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(parsed.Addr, "127.0.0.1:"))
	require.True(t, strings.HasPrefix(parsed.DBName, "newapi_rebase_"))
}

func seedForkCompatibilityRows(t *testing.T) {
	t.Helper()
	user := User{Id: 9100, Username: "matrix-preserved", Password: "fixture", Quota: 1234, AffCode: "matrix-preserved", Setting: `{"data_consent":true}`}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&Token{Id: 9100, UserId: 9100, Key: "matrix-preserved-token", Name: "保留令牌"}).Error)
	require.NoError(t, DB.Create(&Checkin{UserId: 9100, CheckinDate: "2026-01-01", QuotaAwarded: 23}).Error)
	require.NoError(t, LOG_DB.Create(&Log{UserId: 9100, Content: "保留日志", Other: `{"request_path":"/v1/chat/completions"}`}).Error)
	require.NoError(t, DB.Create(&InviteCode{Code: "PRESERVED", Source: "matrix", IssuedTo: "preserved", Note: "保留邀请码"}).Error)
	job := PromptAuditJob{Status: PromptAuditJobDone, RequestId: "matrix-preserved", SnapshotJSON: `{"text":"保留审核"}`}
	require.NoError(t, DB.Create(&job).Error)
	require.NoError(t, DB.Create(&PromptAuditEvent{JobId: &job.Id, RequestId: "matrix-preserved", FullPrompt: "保留审核"}).Error)
}

func verifyForkCompatibilityRows(t *testing.T) {
	t.Helper()
	var user User
	require.NoError(t, DB.First(&user, 9100).Error)
	assert.Equal(t, 1234, user.Quota)
	assert.Equal(t, `{"data_consent":true}`, user.Setting)
	var token Token
	require.NoError(t, DB.First(&token, 9100).Error)
	assert.Equal(t, "matrix-preserved-token", token.Key)
	assert.Equal(t, "保留令牌", token.Name)
	var checkin Checkin
	require.NoError(t, DB.Where("user_id = ? AND checkin_date = ?", 9100, "2026-01-01").First(&checkin).Error)
	assert.Equal(t, 23, checkin.QuotaAwarded)
	var log Log
	require.NoError(t, LOG_DB.Where("user_id = ?", 9100).First(&log).Error)
	assert.Equal(t, "保留日志", log.Content)
	assert.Equal(t, `{"request_path":"/v1/chat/completions"}`, log.Other)
	assert.True(t, DB.Migrator().HasIndex(&InviteCode{}, "idx_invite_codes_code"))
	assert.True(t, DB.Migrator().HasIndex(&Checkin{}, "idx_user_checkin_date"))
	assert.True(t, DB.Migrator().HasIndex(&PromptAuditJob{}, "idx_prompt_audit_jobs_schedule"))
	assert.True(t, DB.Migrator().HasIndex(&PromptAuditEvent{}, "idx_prompt_audit_events_job_id"))
	if os.Getenv("NEWAPI_REBASE_SOURCE") != "release" {
		var invite InviteCode
		require.NoError(t, DB.Where("code = ?", "PRESERVED").First(&invite).Error)
		assert.Equal(t, "保留邀请码", invite.Note)
		var event PromptAuditEvent
		require.NoError(t, DB.Where("request_id = ?", "matrix-preserved").First(&event).Error)
		assert.Equal(t, "保留审核", event.FullPrompt)
	}
	if os.Getenv("NEWAPI_REBASE_SOURCE") == "fork" {
		var moderation ContentModerationLog
		require.NoError(t, DB.Where("request_id = ?", "matrix-preserved").First(&moderation).Error)
		assert.Equal(t, "保留内容审核", moderation.InputExcerpt)
		assert.True(t, moderation.Flagged)
	}
}

func runForkConcurrent(count int, run func(int) error) []error {
	start := make(chan struct{})
	results := make([]error, count)
	var group sync.WaitGroup
	for index := range count {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results[index] = run(index)
		}()
	}
	close(start)
	group.Wait()
	return results
}

func forkSchemaSnapshot(t *testing.T, db *gorm.DB, tables ...string) map[string][]string {
	t.Helper()
	result := map[string][]string{}
	for _, table := range tables {
		columns, err := db.Migrator().ColumnTypes(table)
		require.NoError(t, err)
		for _, column := range columns {
			nullable, nullableOK := column.Nullable()
			length, lengthOK := column.Length()
			defaultValue, defaultOK := column.DefaultValue()
			result[table] = append(result[table], fmt.Sprintf("column:%s:%s:nullable=%t/%t:length=%d/%t:default=%s/%t", column.Name(), column.DatabaseTypeName(), nullable, nullableOK, length, lengthOK, defaultValue, defaultOK))
		}
		if db.Dialector.Name() == "sqlite" {
			var indexes []struct{ Name, SQL string }
			require.NoError(t, db.Raw("SELECT name, COALESCE(sql, '') AS sql FROM sqlite_master WHERE type = 'index' AND tbl_name = ? ORDER BY name", table).Scan(&indexes).Error)
			for _, index := range indexes {
				result[table] = append(result[table], fmt.Sprintf("index:%s:%s", index.Name, index.SQL))
			}
		} else {
			indexes, err := db.Migrator().GetIndexes(table)
			require.NoError(t, err)
			for _, index := range indexes {
				unique, ok := index.Unique()
				result[table] = append(result[table], fmt.Sprintf("index:%s:%v:unique=%t/%t", index.Name(), index.Columns(), unique, ok))
			}
		}
		sort.Strings(result[table])
	}
	return result
}
