package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

// RegisterUsageHubRoutes 注册面向 T3 Code 等客户端的 CLIProxyAPI 兼容用量 Hub 路由。
// 认证由 UsageHubHandler 通过独立只读 Management Key 完成。
func RegisterUsageHubRoutes(r *gin.Engine, h *handler.Handlers) {
	if r == nil || h == nil || h.UsageHub == nil {
		return
	}

	mgmt := r.Group("/v0/management")
	{
		mgmt.GET("/auth-files", h.UsageHub.ListAuthFiles)
		mgmt.POST("/api-call", h.UsageHub.APICall)
	}
}
