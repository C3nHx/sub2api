package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type usageHubUserRepoStub struct {
	records map[int64]*UserUsageHubRecord
}

func newUsageHubUserRepoStub() *usageHubUserRepoStub {
	return &usageHubUserRepoStub{
		records: map[int64]*UserUsageHubRecord{},
	}
}

func (r *usageHubUserRepoStub) GetByUserID(_ context.Context, userID int64) (*UserUsageHubRecord, error) {
	rec, ok := r.records[userID]
	if !ok {
		return nil, ErrUsageHubUserConfigNotFound
	}
	cp := *rec
	cp.MagpieSources = append([]UsageHubMagpieSourceRecord(nil), rec.MagpieSources...)
	return &cp, nil
}

func (r *usageHubUserRepoStub) GetByTokenHash(_ context.Context, tokenHash string) (*UserUsageHubRecord, error) {
	for _, rec := range r.records {
		if rec.TokenHash != "" && rec.TokenHash == tokenHash && rec.UserStatus != StatusDisabled {
			cp := *rec
			cp.MagpieSources = append([]UsageHubMagpieSourceRecord(nil), rec.MagpieSources...)
			return &cp, nil
		}
	}
	return nil, ErrUsageHubUserConfigNotFound
}

func (r *usageHubUserRepoStub) Upsert(_ context.Context, record *UserUsageHubRecord) error {
	cp := *record
	cp.MagpieSources = append([]UsageHubMagpieSourceRecord(nil), record.MagpieSources...)
	r.records[record.UserID] = &cp
	return nil
}

func (r *usageHubUserRepoStub) DeleteToken(_ context.Context, userID int64) error {
	if rec, ok := r.records[userID]; ok {
		rec.Enabled = false
		rec.TokenHash = ""
		rec.TokenMasked = ""
	}
	return nil
}

func (r *usageHubUserRepoStub) CountEnabled(_ context.Context) (int64, error) {
	var n int64
	for _, rec := range r.records {
		if rec.Enabled && rec.TokenHash != "" {
			n++
		}
	}
	return n, nil
}

type usageHubEncryptorStub struct{}

func (usageHubEncryptorStub) Encrypt(plaintext string) (string, error) {
	return "enc:" + plaintext, nil
}

func (usageHubEncryptorStub) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "enc:"), nil
}

func TestUsageHubService_UserSelfServiceLifecycleAndDualAuth(t *testing.T) {
	settingRepo := newUsageHubSettingRepoStub()
	accountRepo := &usageHubAccountRepoStub{
		accounts: []Account{
			{
				ID:       101,
				Name:     "Site Codex #1",
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Status:   StatusActive,
				Credentials: map[string]any{
					"email":              "site1@example.com",
					"chatgpt_account_id": "acct-site-1",
				},
			},
		},
	}
	userRepo := newUsageHubUserRepoStub()

	svc := NewUsageHubService(settingRepo, accountRepo, nil).WithUserHub(userRepo, usageHubEncryptorStub{})

	// 用户首次开启自助 Hub 时自动生成 hubu-... 密钥，并只存哈希。
	enable := true
	userCfg, err := svc.UpdateUserConfig(context.Background(), 42, false, UpdateUserUsageHubConfigInput{
		Enabled:            &enable,
		IncludeDisabled:    &enable, // 非管理员尝试开启会被强制收口为 false
		ExposeResetCredits: &enable, // 非管理员尝试开启会被强制收口为 false
	})
	require.NoError(t, err)
	require.True(t, userCfg.Enabled)
	require.True(t, userCfg.HasKey)
	require.True(t, strings.HasPrefix(userCfg.GeneratedKey, UsageHubUserKeyPrefix))
	require.False(t, userCfg.IncludeDisabled)
	require.False(t, userCfg.ExposeResetCredits)

	stored, err := userRepo.GetByUserID(context.Background(), 42)
	require.NoError(t, err)
	require.NotEqual(t, userCfg.GeneratedKey, stored.TokenHash)
	require.Equal(t, hashUsageHubToken(userCfg.GeneratedKey), stored.TokenHash)

	// 用户自助 Key 可直接通过 AuthenticateManagementKey 鉴权并读取全站 Codex 账号。
	principal, err := svc.AuthenticateManagementKey(context.Background(), userCfg.GeneratedKey)
	require.NoError(t, err)
	require.Equal(t, "user", principal.Kind)
	require.Equal(t, int64(42), principal.UserID)
	require.False(t, principal.ExposeResetCredits)

	authFiles, err := svc.ListAuthFilesForPrincipal(context.Background(), principal)
	require.NoError(t, err)
	require.Len(t, authFiles.Files, 1)
	require.Equal(t, "sub2api-codex-101", authFiles.Files[0].AuthIndex)

	// 同时保留兼容全局管理员旧 Key（hub-...）。
	globalCfg, err := svc.UpdateConfig(context.Background(), UpdateUsageHubConfigInput{Enabled: &enable})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(globalCfg.GeneratedKey, UsageHubKeyPrefix))
	require.Equal(t, int64(1), globalCfg.ActiveSelfServiceUsers)

	globalPrincipal, err := svc.AuthenticateManagementKey(context.Background(), globalCfg.GeneratedKey)
	require.NoError(t, err)
	require.Equal(t, "global", globalPrincipal.Kind)
	require.True(t, globalPrincipal.IsAdmin)

	// 关闭自助总开关后，普通用户 Key 失效，而全局 Key 仍有效。
	disable := false
	_, err = svc.UpdateConfig(context.Background(), UpdateUsageHubConfigInput{SelfServiceEnabled: &disable})
	require.NoError(t, err)

	_, err = svc.AuthenticateManagementKey(context.Background(), userCfg.GeneratedKey)
	require.ErrorIs(t, err, ErrUsageHubDisabled)

	_, err = svc.AuthenticateManagementKey(context.Background(), globalCfg.GeneratedKey)
	require.NoError(t, err)

	// 吊销用户 Key 后自动关闭。
	require.NoError(t, svc.DeleteUserKey(context.Background(), 42))
	reloaded, err := svc.GetUserConfig(context.Background(), 42, false)
	require.NoError(t, err)
	require.False(t, reloaded.Enabled)
	require.False(t, reloaded.HasKey)
}

func TestUsageHubService_MagpieURLValidationAndSSRFGuard(t *testing.T) {
	// 普通用户禁止 HTTP 与内网地址。
	_, err := validateMagpieBaseURL("http://magpie:3425", false)
	require.Error(t, err)

	_, err = validateMagpieBaseURL("https://127.0.0.1:3425", false)
	require.Error(t, err)

	_, err = validateMagpieBaseURL("https://169.254.169.254/latest/meta-data", false)
	require.Error(t, err)

	// 公网 HTTPS 允许，并自动去除误贴的 /v1/magpie/quotas 路径。
	normalized, err := validateMagpieBaseURL("https://ellm.zhzy.fun/magpie/v1/magpie/quotas?kept=1", false)
	require.NoError(t, err)
	require.Equal(t, "https://ellm.zhzy.fun/magpie", normalized)

	// 管理员允许内网 HTTP（如 Docker 内网 http://magpie:3425）。
	adminNormalized, err := validateMagpieBaseURL("http://magpie:3425/", true)
	require.NoError(t, err)
	require.Equal(t, "http://magpie:3425", adminNormalized)
}

func TestUsageHubService_MagpieToCPAReshaping_CodexClaudeAntigravity(t *testing.T) {
	settingRepo := newUsageHubSettingRepoStub()
	accountRepo := &usageHubAccountRepoStub{
		accounts: []Account{
			{
				ID:       1,
				Name:     "Sub2API Codex #1",
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Status:   StatusActive,
				Credentials: map[string]any{
					"email":              "sub2api@example.com",
					"chatgpt_account_id": "acct-sub2api-1",
				},
			},
		},
	}
	userRepo := newUsageHubUserRepoStub()
	svc := NewUsageHubService(settingRepo, accountRepo, &OpenAIQuotaService{}).
		WithUserHub(userRepo, usageHubEncryptorStub{})

	reset5h := time.Date(2026, 11, 25, 16, 0, 0, 0, time.UTC)
	reset7d := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	magpiePayload := []map[string]any{
		{
			"provider": "codex",
			"name":     "Codex",
			"kind":     "subscription",
			"plan":     "pro",
			"user":     "codex-mp@example.com",
			"alias":    "My Codex",
			"windows": []map[string]any{
				{"name": "5 hours", "used": 35.5, "remaining": 64.5, "resetsAt": reset5h.Format(time.RFC3339)},
				{"name": "7 days", "used": 12.0, "remaining": 88.0, "resetsAt": reset7d.Format(time.RFC3339)},
			},
		},
		{
			"provider": "claude",
			"name":     "Claude Code",
			"kind":     "subscription",
			"plan":     "max",
			"user":     "claude-mp@example.com",
			"windows": []map[string]any{
				{"name": "5 hours", "used": 62, "remaining": 38, "resetsAt": reset5h.Format(time.RFC3339)},
				{"name": "7 days", "used": 41, "remaining": 59, "resetsAt": reset7d.Format(time.RFC3339)},
				{"name": "Weekly Opus", "used": 15, "remaining": 85, "resetsAt": reset7d.Format(time.RFC3339)},
			},
		},
		{
			"provider": "antigravity",
			"name":     "Antigravity",
			"kind":     "subscription",
			"plan":     "pro",
			"user":     "agy-mp@example.com",
			"windows": []map[string]any{
				{"name": "5 hours", "pool": "Gemini", "used": 25, "remaining": 75, "resetsAt": reset5h.Format(time.RFC3339)},
				{"name": "7 days", "pool": "Claude & GPT", "used": 80, "remaining": 20, "resetsAt": reset7d.Format(time.RFC3339)},
			},
		},
		{
			"provider": "deepseek",
			"name":     "DeepSeek Key",
			"kind":     "balance",
			"balance":  "$10.00",
			"windows":  []map[string]any{},
		},
	}
	magpieJSON, err := json.Marshal(magpiePayload)
	require.NoError(t, err)

	var fetchCalls int
	svc.magpieClientFactory = func(_ bool) (*http.Client, error) {
		return &http.Client{
			Transport: usageHubRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				fetchCalls++
				require.Equal(t, "Bearer mgp-ro-secret", r.Header.Get("Authorization"))
				require.Equal(t, "/magpie/v1/magpie/quotas", r.URL.Path)
				require.Equal(t, "1", r.URL.Query().Get("kept"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body:       io.NopCloser(strings.NewReader(string(magpieJSON))),
				}, nil
			}),
		}, nil
	}

	enable := true
	sources := []UsageHubMagpieSourceInput{
		{
			ID:        "home",
			Name:      "Home Magpie",
			Enabled:   true,
			BaseURL:   "https://ellm.zhzy.fun/magpie",
			APIKey:    "mgp-ro-secret",
			Providers: []string{"codex", "claude", "antigravity"},
		},
	}
	cfg, err := svc.UpdateUserConfig(context.Background(), 7, false, UpdateUserUsageHubConfigInput{
		Enabled:       &enable,
		MagpieSources: &sources,
	})
	require.NoError(t, err)
	require.Len(t, cfg.MagpieSources, 1)
	require.True(t, cfg.MagpieSources[0].HasAPIKey)

	// 验证测试连通接口也能准确返回 3 个平台。
	testRes, err := svc.TestUserMagpieSource(context.Background(), 7, false, TestMagpieSourceInput{SourceID: "home"})
	require.NoError(t, err)
	require.True(t, testRes.OK)
	require.Equal(t, 4, testRes.TotalRawQuotas)
	require.Equal(t, 3, testRes.MatchedCount)
	require.Equal(t, 1, testRes.ProviderCounts["codex"])
	require.Equal(t, 1, testRes.ProviderCounts["claude"])
	require.Equal(t, 1, testRes.ProviderCounts["antigravity"])

	// 验证 auth-files 合并了 1 个 sub2api + 3 个 Magpie 账号。
	principal, err := svc.AuthenticateManagementKey(context.Background(), cfg.GeneratedKey)
	require.NoError(t, err)

	filesResp, err := svc.ListAuthFilesForPrincipal(context.Background(), principal)
	require.NoError(t, err)
	require.Len(t, filesResp.Files, 4)

	byProvider := map[string]UsageHubAuthFile{}
	for _, f := range filesResp.Files {
		if strings.HasPrefix(f.AuthIndex, "magpie:") {
			byProvider[f.Provider] = f
		}
	}
	require.Contains(t, byProvider, "codex")
	require.Contains(t, byProvider, "claude")
	require.Contains(t, byProvider, "antigravity")
	require.NotEmpty(t, byProvider["antigravity"].ProjectID)

	// 1. Codex: GET /backend-api/wham/usage -> 合成 OpenAI wham 结构，并拒绝 rate-limit-reset-credits。
	codexResp, err := svc.ExecuteAPICallForPrincipal(context.Background(), principal, UsageHubAPICallRequest{
		AuthIndex: json.RawMessage(`"` + byProvider["codex"].AuthIndex + `"`),
		Method:    http.MethodGet,
		URL:       "https://chatgpt.com/backend-api/wham/usage",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, codexResp.StatusCode)
	var codexBody struct {
		PlanType  string `json:"plan_type"`
		RateLimit struct {
			PrimaryWindow   struct{ UsedPercent float64 `json:"used_percent"` } `json:"primary_window"`
			SecondaryWindow struct{ UsedPercent float64 `json:"used_percent"` } `json:"secondary_window"`
		} `json:"rate_limit"`
	}
	require.NoError(t, json.Unmarshal([]byte(codexResp.Body), &codexBody))
	require.Equal(t, "pro", codexBody.PlanType)
	require.Equal(t, 35.5, codexBody.RateLimit.PrimaryWindow.UsedPercent)
	require.Equal(t, 12.0, codexBody.RateLimit.SecondaryWindow.UsedPercent)

	_, err = svc.ExecuteAPICallForPrincipal(context.Background(), principal, UsageHubAPICallRequest{
		AuthIndex: json.RawMessage(`"` + byProvider["codex"].AuthIndex + `"`),
		Method:    http.MethodGet,
		URL:       "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits",
	})
	require.ErrorIs(t, err, ErrUsageHubForbiddenURL)

	// 2. Claude: GET https://api.anthropic.com/api/oauth/usage -> 合成 five_hour + seven_day + Opus limits。
	claudeResp, err := svc.ExecuteAPICallForPrincipal(context.Background(), principal, UsageHubAPICallRequest{
		AuthIndex: json.RawMessage(`"` + byProvider["claude"].AuthIndex + `"`),
		Method:    http.MethodGet,
		URL:       "https://api.anthropic.com/api/oauth/usage",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, claudeResp.StatusCode)
	var claudeBody struct {
		FiveHour struct{ Utilization float64 `json:"utilization"` } `json:"five_hour"`
		SevenDay struct{ Utilization float64 `json:"utilization"` } `json:"seven_day"`
		Limits   []struct {
			Kind    string  `json:"kind"`
			Percent float64 `json:"percent"`
			Scope   struct {
				Model struct{ DisplayName string `json:"display_name"` } `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
	}
	require.NoError(t, json.Unmarshal([]byte(claudeResp.Body), &claudeBody))
	require.Equal(t, 62.0, claudeBody.FiveHour.Utilization)
	require.Equal(t, 41.0, claudeBody.SevenDay.Utilization)
	require.Len(t, claudeBody.Limits, 1)
	require.Equal(t, "weekly_scoped", claudeBody.Limits[0].Kind)
	require.Equal(t, "Opus", claudeBody.Limits[0].Scope.Model.DisplayName)

	// 3. Antigravity: POST retrieveUserQuotaSummary -> groups[].buckets[]，且 userinfo 返回 404 保留邮箱展示。
	agyResp, err := svc.ExecuteAPICallForPrincipal(context.Background(), principal, UsageHubAPICallRequest{
		AuthIndex: json.RawMessage(`"` + byProvider["antigravity"].AuthIndex + `"`),
		Method:    http.MethodPost,
		URL:       "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
		Data:      `{"project":"` + byProvider["antigravity"].ProjectID + `"}`,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, agyResp.StatusCode)
	var agyBody struct {
		Groups []struct {
			DisplayName string `json:"displayName"`
			Buckets     []struct {
				BucketID          string  `json:"bucketId"`
				Window            string  `json:"window"`
				RemainingFraction float64 `json:"remainingFraction"`
			} `json:"buckets"`
		} `json:"groups"`
	}
	require.NoError(t, json.Unmarshal([]byte(agyResp.Body), &agyBody))
	require.Len(t, agyBody.Groups, 2)
	require.Equal(t, "gemini-5h", agyBody.Groups[0].Buckets[0].BucketID)
	require.InDelta(t, 0.75, agyBody.Groups[0].Buckets[0].RemainingFraction, 0.001)
	require.Equal(t, "3p-weekly", agyBody.Groups[1].Buckets[0].BucketID)
	require.InDelta(t, 0.20, agyBody.Groups[1].Buckets[0].RemainingFraction, 0.001)

	userInfoResp, err := svc.ExecuteAPICallForPrincipal(context.Background(), principal, UsageHubAPICallRequest{
		AuthIndex: json.RawMessage(`"` + byProvider["antigravity"].AuthIndex + `"`),
		Method:    http.MethodGet,
		URL:       "https://www.googleapis.com/oauth2/v2/userinfo",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, userInfoResp.StatusCode)

	// 缓存生效验证：除 TestUserMagpieSource 的 1 次直连 + auth-files 的 1 次缓存拉取外，后续 3 次 api-call 全部命中缓存。
	require.Equal(t, 2, fetchCalls)

	// 4. 验证 sub2api 账号在未开启 ExposeResetCredits 时，同样拦截 rate-limit-reset-credits。
	_, err = svc.ExecuteAPICallForPrincipal(context.Background(), principal, UsageHubAPICallRequest{
		AuthIndex: json.RawMessage(`"sub2api-codex-1"`),
		Method:    http.MethodGet,
		URL:       "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits",
	})
	require.ErrorIs(t, err, ErrUsageHubForbiddenURL)
}
