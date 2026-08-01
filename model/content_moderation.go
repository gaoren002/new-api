package model

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const contentModerationFlaggedHashKey = "newapi:content_moderation:flagged_hashes"

type ContentModerationLog struct {
	Id                    int64   `json:"id" gorm:"primaryKey;autoIncrement"`
	CreatedAt             int64   `json:"created_at" gorm:"index"`
	RequestId             string  `json:"request_id" gorm:"size:128;index"`
	UserId                int     `json:"user_id" gorm:"index"`
	Username              string  `json:"username" gorm:"size:128"`
	UserEmail             string  `json:"user_email" gorm:"size:255"`
	TokenId               int     `json:"token_id" gorm:"index"`
	TokenName             string  `json:"token_name" gorm:"size:128"`
	GroupName             string  `json:"group" gorm:"column:group_name;size:128;index"`
	Endpoint              string  `json:"endpoint" gorm:"size:255;index"`
	Provider              string  `json:"provider" gorm:"size:128"`
	Protocol              string  `json:"protocol" gorm:"size:64"`
	Model                 string  `json:"model" gorm:"size:255;index"`
	Mode                  string  `json:"mode" gorm:"size:24"`
	Action                string  `json:"action" gorm:"size:32;index"`
	Flagged               bool    `json:"flagged" gorm:"index"`
	HighestCategory       string  `json:"highest_category" gorm:"size:64"`
	HighestScore          float64 `json:"highest_score"`
	MatchedKeyword        string  `json:"matched_keyword" gorm:"size:255"`
	CategoryScoresJSON    string  `json:"-" gorm:"type:text"`
	ThresholdSnapshotJSON string  `json:"-" gorm:"type:text"`
	InputHash             string  `json:"input_hash" gorm:"size:64;index"`
	InputExcerpt          string  `json:"input_excerpt" gorm:"type:text"`
	UpstreamLatencyMS     *int    `json:"upstream_latency_ms"`
	QueueDelayMS          *int    `json:"queue_delay_ms"`
	Error                 string  `json:"error" gorm:"type:text"`
	ViolationCount        int     `json:"violation_count"`
	AutoBanned            bool    `json:"auto_banned"`
	EmailSent             bool    `json:"email_sent"`
	UserStatus            int     `json:"user_status" gorm:"-"`
}

func (ContentModerationLog) TableName() string { return "content_moderation_logs" }

type ContentModerationLogFilter struct {
	Result    string
	Group     string
	Endpoint  string
	Search    string
	StartTime int64
	EndTime   int64
}

func CreateContentModerationLog(ctx context.Context, log *ContentModerationLog) error {
	if log == nil {
		return nil
	}
	if log.CreatedAt <= 0 {
		log.CreatedAt = common.GetTimestamp()
	}
	return DB.WithContext(ctx).Create(log).Error
}

func ListContentModerationLogs(filter ContentModerationLogFilter, page, pageSize int) ([]ContentModerationLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	query := DB.Model(&ContentModerationLog{})
	switch strings.ToLower(strings.TrimSpace(filter.Result)) {
	case "hit", "flagged":
		query = query.Where("flagged = ?", true)
	case "blocked", "block":
		query = query.Where("action IN ?", []string{"block", "keyword_block", "hash_block", "cyber_policy"})
	case "pass", "allow":
		query = query.Where("flagged = ? AND error = ?", false, "")
	case "error":
		query = query.Where("error <> ?", "")
	}
	if value := strings.TrimSpace(filter.Group); value != "" {
		query = query.Where("group_name = ?", value)
	}
	if value := strings.TrimSpace(filter.Endpoint); value != "" {
		query = query.Where("endpoint = ?", value)
	}
	if value := strings.TrimSpace(filter.Search); value != "" {
		like := "%" + value + "%"
		query = query.Where("request_id LIKE ? OR username LIKE ? OR user_email LIKE ? OR token_name LIKE ? OR model LIKE ? OR input_excerpt LIKE ?", like, like, like, like, like, like)
	}
	if filter.StartTime > 0 {
		query = query.Where("created_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("created_at <= ?", filter.EndTime)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]ContentModerationLog, 0)
	if err := query.Order("created_at DESC, id DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	userIDs := make([]int, 0)
	seen := map[int]struct{}{}
	for _, item := range items {
		if item.UserId > 0 {
			if _, ok := seen[item.UserId]; !ok {
				seen[item.UserId] = struct{}{}
				userIDs = append(userIDs, item.UserId)
			}
		}
	}
	if len(userIDs) > 0 {
		var users []struct {
			Id     int
			Status int
		}
		if err := DB.Model(&User{}).Select("id", "status").Where("id IN ?", userIDs).Find(&users).Error; err == nil {
			statuses := map[int]int{}
			for _, user := range users {
				statuses[user.Id] = user.Status
			}
			for index := range items {
				items[index].UserStatus = statuses[items[index].UserId]
			}
		}
	}
	return items, total, nil
}

func CountContentModerationViolations(ctx context.Context, userID int, since int64, excludeCyberPolicy bool) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	query := DB.WithContext(ctx).Model(&ContentModerationLog{}).
		Where("user_id = ? AND flagged = ? AND action <> ? AND created_at >= ?", userID, true, "hash_block", since)
	if excludeCyberPolicy {
		query = query.Where("action <> ?", "cyber_policy")
	}
	var lastBan int64
	_ = DB.WithContext(ctx).Model(&ContentModerationLog{}).
		Where("user_id = ? AND auto_banned = ?", userID, true).
		Select("COALESCE(MAX(created_at), 0)").Scan(&lastBan).Error
	if lastBan > 0 {
		query = query.Where("created_at > ?", lastBan)
	}
	var count int64
	return count, query.Count(&count).Error
}

func CleanupContentModerationLogs(ctx context.Context, hitBefore, nonHitBefore int64) (int64, int64, error) {
	hit := DB.WithContext(ctx).Where("flagged = ? AND created_at < ?", true, hitBefore).Delete(&ContentModerationLog{})
	if hit.Error != nil {
		return 0, 0, hit.Error
	}
	nonHit := DB.WithContext(ctx).Where("flagged = ? AND created_at < ?", false, nonHitBefore).Delete(&ContentModerationLog{})
	return hit.RowsAffected, nonHit.RowsAffected, nonHit.Error
}

func SetContentModerationLogEmailSent(ctx context.Context, id int64) error {
	return DB.WithContext(ctx).Model(&ContentModerationLog{}).Where("id = ?", id).Update("email_sent", true).Error
}

func SetUserStatusForContentModeration(userID, status int) error {
	if userID <= 0 || (status != common.UserStatusEnabled && status != common.UserStatusDisabled) {
		return errors.New("invalid content moderation user status update")
	}
	var authVersion int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		if user.Role >= common.RoleAdminUser && status == common.UserStatusDisabled {
			return errors.New("content moderation cannot disable an administrator")
		}
		if user.Status == status {
			authVersion = user.AuthVersion
			return nil
		}
		var err error
		authVersion, err = IncrementUserAuthVersionWithTx(tx, userID)
		if err != nil {
			return err
		}
		return tx.Model(&User{}).Where("id = ?", userID).Update("status", status).Error
	})
	if err != nil {
		return err
	}
	if authVersion > 0 {
		if err := PublishUserAuthCache(userID); err != nil {
			return err
		}
		_, _ = RevokeAllUserSessions(userID, "content_moderation_status_changed")
	}
	return nil
}

func RecordContentModerationHash(ctx context.Context, inputHash string) error {
	if !common.RedisEnabled || common.RDB == nil || strings.TrimSpace(inputHash) == "" {
		return nil
	}
	return common.RDB.SAdd(ctx, contentModerationFlaggedHashKey, strings.TrimSpace(inputHash)).Err()
}

func HasContentModerationHash(ctx context.Context, inputHash string) (bool, error) {
	if !common.RedisEnabled || common.RDB == nil || strings.TrimSpace(inputHash) == "" {
		return false, nil
	}
	return common.RDB.SIsMember(ctx, contentModerationFlaggedHashKey, strings.TrimSpace(inputHash)).Result()
}

func DeleteContentModerationHash(ctx context.Context, inputHash string) (bool, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return false, errors.New("redis is unavailable")
	}
	count, err := common.RDB.SRem(ctx, contentModerationFlaggedHashKey, strings.TrimSpace(inputHash)).Result()
	return count > 0, err
}

func ClearContentModerationHashes(ctx context.Context) (int64, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return 0, errors.New("redis is unavailable")
	}
	count, err := common.RDB.SCard(ctx, contentModerationFlaggedHashKey).Result()
	if err != nil || count == 0 {
		return count, err
	}
	return count, common.RDB.Del(ctx, contentModerationFlaggedHashKey).Err()
}

func CountContentModerationHashes(ctx context.Context) (int64, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return 0, nil
	}
	return common.RDB.SCard(ctx, contentModerationFlaggedHashKey).Result()
}
