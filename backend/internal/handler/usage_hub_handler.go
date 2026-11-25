package handler

import (
	"errors"
	"net/http"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// UsageHubHandler 提供兼容 CLIProxyAPI Management API v0 的只读用量 Hub 端点，
// 供 T3 Code（Settings → Providers → Usage providers → Add hub）直接对接。
type UsageHubHandler struct {
	usageHubService *service.UsageHubService
}

// NewUsageHubHandler 创建 CLIProxyAPI 兼容层处理器。
func NewUsageHubHandler(usageHubService *service.UsageHubService) *UsageHubHandler {
	return &UsageHubHandler{
		usageHubService: usageHubService,
	}
}

// ListAuthFiles 对应 GET /v0/management/auth-files。
// 返回 CLIProxyAPI 原生 JSON 格式：{"files": [...]}。
func (h *UsageHubHandler) ListAuthFiles(c *gin.Context) {
	if !h.authenticateManagementKey(c) {
		return
	}

	res, err := h.usageHubService.ListAuthFiles(c.Request.Context())
	if err != nil {
		writeUsageHubError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// APICall 对应 POST /v0/management/api-call。
// 返回 CLIProxyAPI 原生 JSON 格式：{"status_code": ..., "header": {...}, "body": "..."}。
func (h *UsageHubHandler) APICall(c *gin.Context) {
	if !h.authenticateManagementKey(c) {
		return
	}

	var req service.UsageHubAPICallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	res, err := h.usageHubService.ExecuteAPICall(c.Request.Context(), req)
	if err != nil {
		writeUsageHubError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *UsageHubHandler) authenticateManagementKey(c *gin.Context) bool {
	if h == nil || h.usageHubService == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "usage hub is not configured"})
		return false
	}

	token := extractUsageHubManagementKey(c)
	if err := h.usageHubService.ValidateManagementKey(c.Request.Context(), token); err != nil {
		writeUsageHubError(c, err)
		return false
	}
	return true
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
