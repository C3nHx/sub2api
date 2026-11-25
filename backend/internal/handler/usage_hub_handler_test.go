package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type usageHubHandlerSettingRepoStub struct {
	values map[string]string
}

func (s *usageHubHandlerSettingRepoStub) Get(_ context.Context, key string) (*service.Setting, error) {
	v, ok := s.values[key]
	if !ok {
		return nil, service.ErrSettingNotFound
	}
	return &service.Setting{Key: key, Value: v}, nil
}

func (s *usageHubHandlerSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	v, ok := s.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return v, nil
}

func (s *usageHubHandlerSettingRepoStub) Set(_ context.Context, key, value string) error {
	s.values[key] = value
	return nil
}

func (s *usageHubHandlerSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := s.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func (s *usageHubHandlerSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	for k, v := range settings {
		s.values[k] = v
	}
	return nil
}

func (s *usageHubHandlerSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	return s.values, nil
}

func (s *usageHubHandlerSettingRepoStub) Delete(_ context.Context, key string) error {
	delete(s.values, key)
	return nil
}

type usageHubHandlerAccountRepoStub struct {
	service.AccountRepository
	accounts []service.Account
}

func (r *usageHubHandlerAccountRepoStub) ListByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	var out []service.Account
	for _, a := range r.accounts {
		if a.Platform == platform {
			out = append(out, a)
		}
	}
	return out, nil
}

func TestUsageHubHandler_AuthAndAuthFilesFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	settingRepo := &usageHubHandlerSettingRepoStub{values: map[string]string{}}
	accountRepo := &usageHubHandlerAccountRepoStub{
		accounts: []service.Account{
			{
				ID:          7,
				Name:        "Codex Pool #7",
				Platform:    service.PlatformOpenAI,
				Type:        service.AccountTypeOAuth,
				Status:      service.StatusActive,
				Schedulable: true,
				Credentials: map[string]any{
					"email":              "dev@example.com",
					"chatgpt_account_id": "acct-7",
				},
			},
		},
	}

	svc := service.NewUsageHubService(settingRepo, accountRepo, nil)
	h := NewUsageHubHandler(svc)

	r := gin.New()
	r.GET("/v0/management/auth-files", h.ListAuthFiles)
	r.POST("/v0/management/api-call", h.APICall)

	// 未开启时返回 404
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)

	// 开启并自动生成密钥
	enable := true
	cfg, err := svc.UpdateConfig(context.Background(), service.UpdateUsageHubConfigInput{Enabled: &enable})
	require.NoError(t, err)
	key := cfg.GeneratedKey

	// 错误密钥返回 401
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	// Authorization: Bearer 校验成功且返回 CLIProxyAPI 原生顶层 {"files": [...]}
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Files []service.UsageHubAuthFile `json:"files"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Files, 1)
	require.Equal(t, "codex", body.Files[0].Provider)
	require.Equal(t, "sub2api-codex-7", body.Files[0].AuthIndex)

	// X-Management-Key 同样可鉴权
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)
	req.Header.Set("X-Management-Key", key)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// 非法 JSON 请求体返回 400
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v0/management/api-call", bytes.NewBufferString("{bad"))
	req.Header.Set("X-Management-Key", key)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
