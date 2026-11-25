package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// UsageHubHandler 处理管理后台「用量 Hub」配置与全站 Codex OAuth 额度查询接口。
type UsageHubHandler struct {
	usageHubService *service.UsageHubService
}

// NewUsageHubHandler 创建管理端用量 Hub 处理器。
func NewUsageHubHandler(usageHubService *service.UsageHubService) *UsageHubHandler {
	return &UsageHubHandler{
		usageHubService: usageHubService,
	}
}

// GetConfig 获取用量 Hub 配置与密钥状态。
// GET /api/v1/admin/usage-hub/config
func (h *UsageHubHandler) GetConfig(c *gin.Context) {
	cfg, err := h.usageHubService.GetConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// UpdateConfig 更新用量 Hub 开关及暴露选项。
// PUT /api/v1/admin/usage-hub/config
func (h *UsageHubHandler) UpdateConfig(c *gin.Context) {
	var req service.UpdateUsageHubConfigInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	cfg, err := h.usageHubService.UpdateConfig(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// RegenerateKey 生成或轮换用量 Hub 管理密钥。
// POST /api/v1/admin/usage-hub/key/regenerate
func (h *UsageHubHandler) RegenerateKey(c *gin.Context) {
	key, maskedKey, err := h.usageHubService.RegenerateKey(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"key":        key,
		"masked_key": maskedKey,
	})
}

// DeleteKey 删除用量 Hub 管理密钥并关闭 Hub。
// DELETE /api/v1/admin/usage-hub/key
func (h *UsageHubHandler) DeleteKey(c *gin.Context) {
	if err := h.usageHubService.DeleteKey(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Usage hub management key deleted"})
}

// ListAccounts 获取全站 Codex OAuth 账号的用量 Hub 汇总视图。
// GET /api/v1/admin/usage-hub/accounts
func (h *UsageHubHandler) ListAccounts(c *gin.Context) {
	overview, err := h.usageHubService.ListAccountsOverview(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, overview)
}

// RefreshAccount 主动刷新单个 Codex OAuth 账号的实时额度。
// POST /api/v1/admin/usage-hub/accounts/:id/refresh
func (h *UsageHubHandler) RefreshAccount(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	item, err := h.usageHubService.RefreshAccountQuota(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

// UsageHubRefreshAllRequest 批量刷新请求体（为空时刷新全站未禁用 Codex OAuth 账号）。
type UsageHubRefreshAllRequest struct {
	AccountIDs []int64 `json:"account_ids"`
}

// RefreshAllAccounts 批量主动刷新全站（或指定列表）Codex OAuth 账号实时额度。
// POST /api/v1/admin/usage-hub/accounts/refresh-all
func (h *UsageHubHandler) RefreshAllAccounts(c *gin.Context) {
	var req UsageHubRefreshAllRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "Invalid request: "+err.Error())
			return
		}
	}

	res, err := h.usageHubService.RefreshAllAccountsQuota(c.Request.Context(), req.AccountIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, res)
}

// PreviewAuthFiles 预览当前对外暴露的 /v0/management/auth-files 载荷，供管理员自检排查。
// GET /api/v1/admin/usage-hub/preview-auth-files
func (h *UsageHubHandler) PreviewAuthFiles(c *gin.Context) {
	res, err := h.usageHubService.ListAuthFiles(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, res)
}
