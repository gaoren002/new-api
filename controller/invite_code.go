package controller

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type IssueInviteCodeByBotRequest struct {
	QQUserId         string `json:"qq_user_id" binding:"required"`
	QQGroupId        string `json:"qq_group_id"`
	Source           string `json:"source"`
	Note             string `json:"note"`
	ExpiresInMinutes int    `json:"expires_in_minutes"`
}

func requireInviteCode(inviteCode string) (string, error) {
	normalized := model.NormalizeInviteCode(inviteCode)
	if !common.InviteCodeRegisterEnabled {
		return normalized, nil
	}
	if normalized == "" {
		return "", errors.New("当前站点已开启邀请码注册，请先入群获取邀请码")
	}
	return normalized, nil
}

func consumeInviteCodeIfNeededTx(tx *gorm.DB, inviteCode string, user *model.User) error {
	normalized, err := requireInviteCode(inviteCode)
	if err != nil {
		return err
	}
	if !common.InviteCodeRegisterEnabled {
		return nil
	}
	_, err = model.ConsumeInviteCodeTx(tx, normalized, user.Id, user.Username)
	return err
}

func getInviteBotSecret(c *gin.Context) string {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader != "" && strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	return strings.TrimSpace(c.GetHeader("X-Invite-Bot-Secret"))
}

func buildInviteRegisterLink(c *gin.Context, code string) string {
	baseURL := strings.TrimSpace(system_setting.ServerAddress)
	if baseURL == "" {
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		baseURL = scheme + "://" + c.Request.Host
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return baseURL + "/register?invite_code=" + url.QueryEscape(code)
}

func IssueInviteCodeByBot(c *gin.Context) {
	if strings.TrimSpace(common.InviteBotSecret) == "" {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "邀请码机器人接口未启用",
		})
		return
	}
	if getInviteBotSecret(c) != strings.TrimSpace(common.InviteBotSecret) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "机器人鉴权失败",
		})
		return
	}

	var req IssueInviteCodeByBotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}

	invite, err := model.IssueInviteCode(
		req.Source,
		req.QQUserId,
		req.QQGroupId,
		req.Note,
		req.ExpiresInMinutes,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"code":         invite.Code,
		"expires_at":   invite.ExpiresAt,
		"invite_link":  buildInviteRegisterLink(c, invite.Code),
		"qq_user_id":   invite.IssuedTo,
		"qq_group_id":  invite.GroupId,
		"source":       invite.Source,
		"issued_at":    invite.CreatedAt,
		"reusable_ttl": model.GetInviteCodeExpireMinutes(),
	})
}
