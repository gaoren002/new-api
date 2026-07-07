package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const defaultInviteCodeLength = 8

type InviteCode struct {
	Id             int    `json:"id"`
	Code           string `json:"code" gorm:"type:varchar(32);uniqueIndex"`
	Source         string `json:"source" gorm:"type:varchar(32);index"`
	IssuedTo       string `json:"issued_to" gorm:"type:varchar(64);index"`
	GroupId        string `json:"group_id" gorm:"type:varchar(64);index"`
	Note           string `json:"note" gorm:"type:varchar(255)"`
	CreatedAt      int64  `json:"created_at" gorm:"index"`
	ExpiresAt      int64  `json:"expires_at" gorm:"index"`
	UsedAt         int64  `json:"used_at" gorm:"index"`
	UsedByUserId   int    `json:"used_by_user_id" gorm:"index"`
	UsedByUsername string `json:"used_by_username" gorm:"type:varchar(64)"`
	Disabled       bool   `json:"disabled" gorm:"default:false;index"`
}

func NormalizeInviteCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func GetInviteCodeExpireMinutes() int {
	if common.InviteCodeExpireMinutes <= 0 {
		return 30
	}
	return common.InviteCodeExpireMinutes
}

func generateInviteCodeValue(length int) (string, error) {
	if length <= 0 {
		length = defaultInviteCodeLength
	}
	for i := 0; i < 10; i++ {
		code := NormalizeInviteCode(common.GetRandomString(length))
		var count int64
		if err := DB.Model(&InviteCode{}).Where("code = ?", code).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return code, nil
		}
	}
	return "", errors.New("生成邀请码失败，请稍后重试")
}

func findUsedInviteCode(source string, issuedTo string) (*InviteCode, error) {
	if issuedTo == "" {
		return nil, nil
	}
	var invite InviteCode
	err := DB.Where("source = ? AND issued_to = ? AND used_at > 0", source, issuedTo).
		Order("id desc").
		First(&invite).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &invite, nil
}

func findActiveUnusedInviteCode(source string, issuedTo string, now int64) (*InviteCode, error) {
	if issuedTo == "" {
		return nil, nil
	}
	var invite InviteCode
	err := DB.Where(
		"source = ? AND issued_to = ? AND disabled = ? AND used_at = 0 AND (expires_at = 0 OR expires_at > ?)",
		source,
		issuedTo,
		false,
		now,
	).Order("id desc").First(&invite).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &invite, nil
}

func duplicateInviteIssueError(source string, reason string) error {
	if source == "qq" {
		switch reason {
		case "used":
			return errors.New("该QQ已使用过邀请码，不可重复申请")
		case "active":
			return errors.New("该QQ已有有效邀请码，请勿重复申请")
		}
		return errors.New("该QQ不可重复申请邀请码")
	}
	switch reason {
	case "used":
		return errors.New("该账号已使用过邀请码，不可重复申请")
	case "active":
		return errors.New("该账号已有有效邀请码，请勿重复申请")
	}
	return errors.New("该账号不可重复申请邀请码")
}

func IssueInviteCode(source string, issuedTo string, groupId string, note string, expiresInMinutes int) (*InviteCode, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "qq"
	}
	issuedTo = strings.TrimSpace(issuedTo)
	groupId = strings.TrimSpace(groupId)
	note = strings.TrimSpace(note)
	now := common.GetTimestamp()
	usedInvite, err := findUsedInviteCode(source, issuedTo)
	if err != nil {
		return nil, err
	}
	if usedInvite != nil {
		return nil, duplicateInviteIssueError(source, "used")
	}

	activeInvite, err := findActiveUnusedInviteCode(source, issuedTo, now)
	if err != nil {
		return nil, err
	}
	if activeInvite != nil {
		return activeInvite, nil
	}

	code, err := generateInviteCodeValue(defaultInviteCodeLength)
	if err != nil {
		return nil, err
	}

	ttl := expiresInMinutes
	if ttl <= 0 {
		ttl = GetInviteCodeExpireMinutes()
	}
	invite := &InviteCode{
		Code:      code,
		Source:    source,
		IssuedTo:  issuedTo,
		GroupId:   groupId,
		Note:      note,
		CreatedAt: now,
		ExpiresAt: now + int64(ttl)*60,
	}
	if err := DB.Create(invite).Error; err != nil {
		return nil, err
	}
	return invite, nil
}

func ConsumeInviteCodeTx(tx *gorm.DB, code string, userId int, username string) (*InviteCode, error) {
	normalized := NormalizeInviteCode(code)
	if normalized == "" {
		return nil, errors.New("邀请码不能为空")
	}

	var invite InviteCode
	query := lockForUpdate(tx).Where("code = ?", normalized)
	if err := query.First(&invite).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("邀请码无效或不存在")
		}
		return nil, err
	}

	now := common.GetTimestamp()
	switch {
	case invite.Disabled:
		return nil, errors.New("邀请码已失效")
	case invite.UsedAt > 0:
		return nil, errors.New("邀请码已被使用")
	case invite.ExpiresAt > 0 && invite.ExpiresAt < now:
		return nil, errors.New("邀请码已过期")
	}

	updates := map[string]any{
		"used_at":          now,
		"used_by_user_id":  userId,
		"used_by_username": strings.TrimSpace(username),
	}
	if err := tx.Model(&invite).Updates(updates).Error; err != nil {
		return nil, err
	}

	invite.UsedAt = now
	invite.UsedByUserId = userId
	invite.UsedByUsername = strings.TrimSpace(username)
	return &invite, nil
}
