package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type usageHubSettingRepoStub struct {
	values map[string]string
}

func newUsageHubSettingRepoStub() *usageHubSettingRepoStub {
	return &usageHubSettingRepoStub{values: make(map[string]string)}
}

func (s *usageHubSettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	v, ok := s.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: v}, nil
}

func (s *usageHubSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	v, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return v, nil
}

func (s *usageHubSettingRepoStub) Set(_ context.Context, key, value string) error {
	s.values[key] = value
	return nil
}

func (s *usageHubSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v, ok := s.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func (s *usageHubSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	for k, v := range settings {
		s.values[k] = v
	}
	return nil
}

func (s *usageHubSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.values))
	for k, v := range s.values {
		out[k] = v
	}
	return out, nil
}

func (s *usageHubSettingRepoStub) Delete(_ context.Context, key string) error {
	delete(s.values, key)
	return nil
}

type usageHubAccountRepoStub struct {
	AccountRepository
	accounts     []Account
	updatedExtra map[int64]map[string]any
}

func (r *usageHubAccountRepoStub) ListByPlatform(_ context.Context, platform string) ([]Account, error) {
	var out []Account
	for _, acc := range r.accounts {
		if acc.Platform == platform {
			out = append(out, acc)
		}
	}
	return out, nil
}

func (r *usageHubAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			cp := r.accounts[i]
			return &cp, nil
		}
	}
	return nil, ErrAccountNotFound
}

func (r *usageHubAccountRepoStub) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	if r.updatedExtra == nil {
		r.updatedExtra = make(map[int64]map[string]any)
	}
	if r.updatedExtra[id] == nil {
		r.updatedExtra[id] = make(map[string]any)
	}
	for k, v := range updates {
		r.updatedExtra[id][k] = v
	}
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			if r.accounts[i].Extra == nil {
				r.accounts[i].Extra = make(map[string]any)
			}
			for k, v := range updates {
				r.accounts[i].Extra[k] = v
			}
		}
	}
	return nil
}

func makeFakeCodexIDToken(t *testing.T, email, accountID, planType string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(map[string]any{
		"email": email,
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id":                accountID,
			"chatgpt_plan_type":                 planType,
			"chatgpt_user_id":                   "user-123",
			"chatgpt_subscription_active_start": "2026-01-01T00:00:00Z",
			"chatgpt_subscription_active_until": "2026-12-31T00:00:00Z",
		},
	})
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return header + "." + payload + ".sig"
}

func TestUsageHubService_ConfigAndKeyLifecycle(t *testing.T) {
	ctx := context.Background()
	settingRepo := newUsageHubSettingRepoStub()
	svc := NewUsageHubService(settingRepo, &usageHubAccountRepoStub{}, nil)

	// 默认关闭，无密钥
	cfg, err := svc.GetConfig(ctx)
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	require.False(t, cfg.HasKey)
	require.ErrorIs(t, svc.ValidateManagementKey(ctx, "anything"), ErrUsageHubDisabled)

	// 首次开启时自动生成密钥
	enable := true
	cfg, err = svc.UpdateConfig(ctx, UpdateUsageHubConfigInput{Enabled: &enable})
	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	require.True(t, cfg.HasKey)
	require.True(t, strings.HasPrefix(cfg.GeneratedKey, UsageHubKeyPrefix))
	require.NotEmpty(t, cfg.MaskedKey)

	// 正确密钥校验通过，错误密钥返回 401
	require.NoError(t, svc.ValidateManagementKey(ctx, cfg.GeneratedKey))
	require.ErrorIs(t, svc.ValidateManagementKey(ctx, "wrong-key"), ErrUsageHubUnauthorized)

	// 轮换密钥后旧密钥失效
	oldKey := cfg.GeneratedKey
	newKey, masked, err := svc.RegenerateKey(ctx)
	require.NoError(t, err)
	require.NotEqual(t, oldKey, newKey)
	require.NotEmpty(t, masked)
	require.ErrorIs(t, svc.ValidateManagementKey(ctx, oldKey), ErrUsageHubUnauthorized)
	require.NoError(t, svc.ValidateManagementKey(ctx, newKey))

	// 删除密钥后自动关闭 Hub
	require.NoError(t, svc.DeleteKey(ctx))
	cfg, err = svc.GetConfig(ctx)
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	require.False(t, cfg.HasKey)
	require.ErrorIs(t, svc.ValidateManagementKey(ctx, newKey), ErrUsageHubDisabled)
}

func TestUsageHubService_ListAuthFilesAndOverview(t *testing.T) {
	ctx := context.Background()
	settingRepo := newUsageHubSettingRepoStub()
	parentID := int64(1)
	resetAt := time.Now().Add(2 * time.Hour)

	repo := &usageHubAccountRepoStub{
		accounts: []Account{
			{
				ID:          1,
				Name:        "Codex Main",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Status:      StatusActive,
				Schedulable: true,
				Credentials: map[string]any{
					"id_token": makeFakeCodexIDToken(t, "alice@example.com", "acct-alice", "plus"),
				},
				Extra: map[string]any{
					"codex_5h_used_percent": 35.0,
					"codex_7d_used_percent": 60.0,
				},
			},
			{
				ID:               2,
				Name:             "Codex RateLimited",
				Platform:         PlatformOpenAI,
				Type:             AccountTypeOAuth,
				Status:           StatusActive,
				Schedulable:      false,
				RateLimitedAt:    &resetAt,
				RateLimitResetAt: &resetAt,
				Credentials: map[string]any{
					"email":              "bob@example.com",
					"chatgpt_account_id": "acct-bob",
				},
				Extra: map[string]any{
					"chatgpt_plan_type":     "pro",
					"codex_5h_used_percent": 100.0,
				},
			},
			{
				ID:          3,
				Name:        "Codex Disabled",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Status:      StatusDisabled,
				Schedulable: false,
				Credentials: map[string]any{
					"email":              "disabled@example.com",
					"chatgpt_account_id": "acct-disabled",
				},
			},
			{
				// 影子账号应被过滤
				ID:              4,
				Name:            "Codex Spark Shadow",
				Platform:        PlatformOpenAI,
				Type:            AccountTypeOAuth,
				Status:          StatusActive,
				ParentAccountID: &parentID,
			},
			{
				// API Key 账号应被过滤
				ID:       5,
				Name:     "OpenAI API Key",
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Status:   StatusActive,
			},
		},
	}

	svc := NewUsageHubService(settingRepo, repo, nil)

	// 默认不包含已禁用账号，但限流/停调的活跃账号应保持 disabled=false 以便查询额度
	res, err := svc.ListAuthFiles(ctx)
	require.NoError(t, err)
	require.Len(t, res.Files, 2)

	require.Equal(t, "sub2api-codex-1", res.Files[0].AuthIndex)
	require.Equal(t, "codex", res.Files[0].Provider)
	require.Equal(t, "alice@example.com", res.Files[0].Email)
	require.Equal(t, "acct-alice", res.Files[0].ChatGPTAccountID)
	require.Equal(t, "plus", res.Files[0].PlanType)
	require.NotNil(t, res.Files[0].IDToken)
	require.Equal(t, "acct-alice", res.Files[0].IDToken.ChatGPTAccountID)
	require.Equal(t, "2026-12-31T00:00:00Z", res.Files[0].IDToken.SubActiveUntil)
	require.False(t, res.Files[0].Disabled)
	require.False(t, res.Files[0].Unavailable)

	require.Equal(t, "sub2api-codex-2", res.Files[1].AuthIndex)
	require.False(t, res.Files[1].Disabled)
	require.True(t, res.Files[1].Unavailable)

	// 开启包含已禁用账号后，返回全部 3 个非影子 Codex OAuth 账号
	includeDisabled := true
	_, err = svc.UpdateConfig(ctx, UpdateUsageHubConfigInput{IncludeDisabled: &includeDisabled})
	require.NoError(t, err)
	res, err = svc.ListAuthFiles(ctx)
	require.NoError(t, err)
	require.Len(t, res.Files, 3)
	require.True(t, res.Files[2].Disabled)

	// 汇总看板校验
	overview, err := svc.ListAccountsOverview(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, overview.Summary.TotalAccounts)
	require.Equal(t, 3, overview.Summary.ExposedAccounts)
	require.Equal(t, 1, overview.Summary.HealthyAccounts)
	require.Equal(t, 1, overview.Summary.RateLimitedAccounts)
	require.Equal(t, 1, overview.Summary.DisabledAccounts)
	require.NotNil(t, overview.Summary.Avg5hUsedPercent)
	require.InDelta(t, 67.5, *overview.Summary.Avg5hUsedPercent, 0.01)
}

func TestUsageHubService_ExecuteAPICall_SecurityAndCaching(t *testing.T) {
	ctx := context.Background()
	settingRepo := newUsageHubSettingRepoStub()
	futureExp := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	repo := &usageHubAccountRepoStub{
		accounts: []Account{
			{
				ID:          11,
				Name:        "Codex Live",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Status:      StatusActive,
				Schedulable: true,
				Credentials: map[string]any{
					"access_token":       "real-secret-access-token",
					"expires_at":         futureExp,
					"chatgpt_account_id": "acct-live-11",
					"email":              "live@example.com",
				},
			},
		},
	}

	var hitCount atomic.Int32
	var seenAuth, seenAccountID, seenPath string

	// 使用纯内存 RoundTripper 模拟 chatgpt.com 上游响应，不依赖本地监听端口
	tokenProvider := NewOpenAITokenProvider(repo, nil, nil)
	privacyFactory := func(_ string) (*req.Client, error) {
		c := req.C()
		c.GetClient().Transport = usageHubRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			hitCount.Add(1)
			seenAuth = r.Header.Get("Authorization")
			seenAccountID = r.Header.Get("Chatgpt-Account-Id")
			seenPath = r.URL.Path
			hdr := make(http.Header)
			hdr.Set("Content-Type", "application/json")
			hdr.Set("Set-Cookie", "secret=should-strip")
			body := `{"plan_type":"plus","rate_limit":{"allowed":true,"limit_reached":false,"primary_window":{"used_percent":42,"limit_window_seconds":18000,"reset_after_seconds":3600}}}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     hdr,
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    r,
			}, nil
		})
		return c, nil
	}
	quotaSvc := NewOpenAIQuotaService(repo, nil, tokenProvider, privacyFactory, nil)
	svc := NewUsageHubService(settingRepo, repo, quotaSvc)

	// 1. 拒绝 SSRF 任意域名
	_, err := svc.ExecuteAPICall(ctx, UsageHubAPICallRequest{
		AuthIndex: json.RawMessage(`"sub2api-codex-11"`),
		Method:    "GET",
		URL:       "https://evil.example.com/steal",
		Header:    map[string]string{"Authorization": "Bearer $TOKEN$"},
	})
	require.ErrorIs(t, err, ErrUsageHubForbiddenURL)

	// 2. 拒绝非 GET 写操作（即使目标是 chatgpt.com）
	_, err = svc.ExecuteAPICall(ctx, UsageHubAPICallRequest{
		AuthIndex: json.RawMessage(`"sub2api-codex-11"`),
		Method:    "POST",
		URL:       "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume",
		Header:    map[string]string{"Authorization": "Bearer $TOKEN$"},
	})
	require.ErrorIs(t, err, ErrUsageHubForbiddenURL)

	// 3. 合法请求：替换 $TOKEN$、自动补齐 Chatgpt-Account-Id、剔除 Set-Cookie、自动回写快照
	resp, err := svc.ExecuteAPICall(ctx, UsageHubAPICallRequest{
		AuthIndex: json.RawMessage(`"sub2api-codex-11"`),
		Method:    "GET",
		URL:       "https://chatgpt.com/backend-api/wham/usage",
		Header:    map[string]string{"Authorization": "Bearer $TOKEN$"},
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "Bearer real-secret-access-token", seenAuth)
	require.Equal(t, "acct-live-11", seenAccountID)
	require.Equal(t, "/backend-api/wham/usage", seenPath)
	require.Contains(t, resp.Body, `"used_percent":42`)
	require.NotContains(t, resp.Header, "Set-Cookie")
	require.Equal(t, int32(1), hitCount.Load())
	require.Equal(t, 42.0, repo.updatedExtra[11]["codex_5h_used_percent"])

	// 4. 短时间内再次请求命中缓存，不再重复打上游
	resp2, err := svc.ExecuteAPICall(ctx, UsageHubAPICallRequest{
		AuthIndex: json.RawMessage(`11`),
		Method:    "GET",
		URL:       "https://chatgpt.com/backend-api/wham/usage",
	})
	require.NoError(t, err)
	require.Equal(t, resp.Body, resp2.Body)
	require.Equal(t, int32(1), hitCount.Load())
}

type usageHubRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f usageHubRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
