package handler

import (
	"errors"
	"net/http"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// UsageHubHandler 同时提供：
// 1. 兼容 CLIProxyAPI Management API v0 的只读用量 Hub 端点（/v0/management/*），
//    供 T3 Code（Settings → Providers → Usage providers → Add hub）直接对接；
// 2. 登录用户的自助用量 Hub 配置与 Magpie 数据源管理端点（/api/v1/user/usage-hub/*）。
type UsageHubHandler struct {
	usageHubService *service.UsageHubService
}

// NewUsageHubHandler 创建 CLIProxyAPI 兼容层与用户自助用量 Hub 处理器。
func NewUsageHubHandler(usageHubService *service.UsageHubService) *UsageHubHandler {
	return &UsageHubHandler{
		usageHubService: usageHubService,
	}
}

// ListAuthFiles 对应 GET /v0/management/auth-files。
// 返回 CLIProxyAPI 原生 JSON 格式：{"files": [...]}。
func (h *UsageHubHandler) ListAuthFiles(c *gin.Context) {
	principal, ok := h.authenticateManagementKey(c)
	if !ok {
		return
	}

	res, err := h.usageHubService.ListAuthFilesForPrincipal(c.Request.Context(), principal)
	if err != nil {
		writeUsageHubError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// APICall 对应 POST /v0/management/api-call。
// 返回 CLIProxyAPI 原生 JSON 格式：{"status_code": ..., "header": {...}, "body": "..."}。
func (h *UsageHubHandler) APICall(c *gin.Context) {
	principal, ok := h.authenticateManagementKey(c)
	if !ok {
		return
	}

	var req service.UsageHubAPICallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	res, err := h.usageHubService.ExecuteAPICallForPrincipal(c.Request.Context(), principal, req)
	if err != nil {
		writeUsageHubError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// GetMyConfig 获取当前用户的自助用量 Hub 配置与脱敏密钥状态。
// GET /api/v1/user/usage-hub/config
func (h *UsageHubHandler) GetMyConfig(c *gin.Context) {
	userID, isAdmin, ok := extractUsageHubUser(c)
	if !ok {
		return
	}
	cfg, err := h.usageHubService.GetUserConfig(c.Request.Context(), userID, isAdmin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// UpdateMyConfig 保存当前用户的自助用量 Hub 配置与自定义 Magpie 源列表。
// PUT /api/v1/user/usage-hub/config
func (h *UsageHubHandler) UpdateMyConfig(c *gin.Context) {
	userID, isAdmin, ok := extractUsageHubUser(c)
	if !ok {
		return
	}
	var req service.UpdateUserUsageHubConfigInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.usageHubService.UpdateUserConfig(c.Request.Context(), userID, isAdmin, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// RegenerateMyKey 为当前用户生成或轮换专属 Hub 密钥。
// POST /api/v1/user/usage-hub/key/regenerate
func (h *UsageHubHandler) RegenerateMyKey(c *gin.Context) {
	userID, isAdmin, ok := extractUsageHubUser(c)
	if !ok {
		return
	}
	key, maskedKey, err := h.usageHubService.RegenerateUserKey(c.Request.Context(), userID, isAdmin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"key":        key,
		"masked_key": maskedKey,
	})
}

// DeleteMyKey 吊销当前用户的专属 Hub 密钥并关闭个人 Hub。
// DELETE /api/v1/user/usage-hub/key
func (h *UsageHubHandler) DeleteMyKey(c *gin.Context) {
	userID, _, ok := extractUsageHubUser(c)
	if !ok {
		return
	}
	if err := h.usageHubService.DeleteUserKey(c.Request.Context(), userID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Personal usage hub key deleted"})
}

// TestMyMagpieSource 测试用户配置的外部 Magpie 数据源连通性并返回可识别平台统计。
// POST /api/v1/user/usage-hub/magpie/test
func (h *UsageHubHandler) TestMyMagpieSource(c *gin.Context) {
	userID, isAdmin, ok := extractUsageHubUser(c)
	if !ok {
		return
	}
	var req service.TestMagpieSourceInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	res, err := h.usageHubService.TestUserMagpieSource(c.Request.Context(), userID, isAdmin, req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, res)
}

// PreviewMyAuthFiles 预览当前用户合并后的 /v0/management/auth-files 响应与告警。
// GET /api/v1/user/usage-hub/preview-auth-files
func (h *UsageHubHandler) PreviewMyAuthFiles(c *gin.Context) {
	userID, isAdmin, ok := extractUsageHubUser(c)
	if !ok {
		return
	}
	res, err := h.usageHubService.PreviewUserAuthFiles(c.Request.Context(), userID, isAdmin)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, res)
}

func extractUsageHubUser(c *gin.Context) (userID int64, isAdmin bool, ok bool) {
	subject, exists := middleware2.GetAuthSubjectFromContext(c)
	if !exists || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return 0, false, false
	}
	role, hasRole := middleware2.GetUserRoleFromContext(c)
	return subject.UserID, hasRole && strings.EqualFold(role, service.RoleAdmin), true
}

func (h *UsageHubHandler) authenticateManagementKey(c *gin.Context) (*service.UsageHubPrincipal, bool) {
	if h == nil || h.usageHubService == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "usage hub is not configured"})
		return nil, false
	}

	token := extractUsageHubManagementKey(c)
	principal, err := h.usageHubService.AuthenticateManagementKey(c.Request.Context(), token)
	if err != nil {
		writeUsageHubError(c, err)
		return nil, false
	}
	return principal, true
}

func extractUsageHubManagementKey(c *gin.Context) string {
	// 同时兼容 CLIProxyAPI 文档中支持的 X-Management-Key 与 Authorization: Bearer 两种头。
	if key := strings.TrimSpace(c.GetHeader("X-Management-Key")); key != "" {
		return key
	}
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(authHeader) >= 7 && strings.EqualFold(authHeader[:7], "Bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	return ""
}

func writeUsageHubError(c *gin.Context, err error) {
	if err == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	var appErr *infraerrors.ApplicationError
	if errors.As(err, &appErr) && appErr != nil {
		code := int(appErr.Code)
		if code < 400 || code > 599 {
			code = http.StatusInternalServerError
		}
		msg := strings.TrimSpace(appErr.Message)
		if msg == "" {
			msg = http.StatusText(code)
		}
		c.JSON(code, gin.H{"error": msg})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}
