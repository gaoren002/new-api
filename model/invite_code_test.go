package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueInviteCode_AllowsFirstIssueForQQ(t *testing.T) {
	truncateTables(t)

	invite, err := IssueInviteCode("qq", "2468888866", "1031435539", "group keyword: 邀请码", 30)
	require.NoError(t, err)
	require.NotNil(t, invite)
	assert.Equal(t, "qq", invite.Source)
	assert.Equal(t, "2468888866", invite.IssuedTo)
	assert.Equal(t, "1031435539", invite.GroupId)
}

func TestIssueInviteCode_AllowsReissueAfterExpiredUnusedInvite(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	existing := &InviteCode{
		Code:      "EXPIRED01",
		Source:    "qq",
		IssuedTo:  "2468888866",
		GroupId:   "1031435539",
		Note:      "group keyword: 邀请码",
		CreatedAt: now - 3600,
		ExpiresAt: now - 1800,
	}
	require.NoError(t, DB.Create(existing).Error)

	invite, err := IssueInviteCode("qq", "2468888866", "1031435539", "group keyword: 邀请码", 30)
	require.NoError(t, err)
	require.NotNil(t, invite)
	assert.NotEqual(t, existing.Code, invite.Code)

	var count int64
	require.NoError(t, DB.Model(&InviteCode{}).Where("source = ? AND issued_to = ?", "qq", "2468888866").Count(&count).Error)
	assert.EqualValues(t, 2, count)
}

func TestIssueInviteCode_RejectsRepeatIssueAfterUsedInvite(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	existing := &InviteCode{
		Code:           "USED0001",
		Source:         "qq",
		IssuedTo:       "2468888866",
		GroupId:        "1031435539",
		Note:           "group keyword: 邀请码",
		CreatedAt:      now - 3600,
		ExpiresAt:      now + 1800,
		UsedAt:         now - 60,
		UsedByUserId:   4112,
		UsedByUsername: "nihao",
	}
	require.NoError(t, DB.Create(existing).Error)

	invite, err := IssueInviteCode("qq", "2468888866", "1031435539", "group keyword: 邀请码", 30)
	require.ErrorContains(t, err, "该QQ已使用过邀请码，不可重复申请")
	assert.Nil(t, invite)
}

func TestIssueInviteCode_RejectsRepeatIssueWhenActiveInviteExists(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	existing := &InviteCode{
		Code:      "ACTIVE001",
		Source:    "qq",
		IssuedTo:  "2468888866",
		GroupId:   "1031435539",
		Note:      "group keyword: 邀请码",
		CreatedAt: now - 60,
		ExpiresAt: now + 1800,
	}
	require.NoError(t, DB.Create(existing).Error)

	invite, err := IssueInviteCode("qq", "2468888866", "1031435539", "group keyword: 邀请码", 30)
	require.ErrorContains(t, err, "该QQ已有有效邀请码，请勿重复申请")
	assert.Nil(t, invite)
}
