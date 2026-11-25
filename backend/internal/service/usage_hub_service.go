package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	openaipkg "github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

const (
	usageHubAuthIndexPrefix       = "sub2api-codex-"
	usageHubDefaultCacheTTL       = 30 * time.Second
	usageHubBatchRefreshLimit     = 4
	usageHubMaxResponseHeaderSize = 64
)

var (
	// ErrUsageHubDisabled 用量 Hub 未开启或未配置密钥。
	ErrUsageHubDisabled = infraerrors.New(http.StatusNotFound, "USAGE_HUB_DISABLED", "usage hub is not enabled")
	// ErrUsageHubUnauthorized 用量 Hub 管理密钥无效。
	ErrUsageHubUnauthorized = infraerrors.New(http.StatusUnauthorized, "USAGE_HUB_UNAUTHORIZED", "invalid management key")
	// ErrUsageHubForbiddenURL 上游请求 URL 或 HTTP 方法不在允许的只读额度白名单内。
	ErrUsageHubForbiddenURL = infraerrors.New(http.StatusForbidden, "USAGE_HUB_URL_FORBIDDEN", "upstream endpoint is not allowed by usage hub")
	// ErrUsageHubAccountNotFound 未找到匹配的 Codex OAuth 账号。
	ErrUsageHubAccountNotFound = infraerrors.New(http.StatusNotFound, "USAGE_HUB_ACCOUNT_NOT_FOUND", "codex oauth account not found")
)

// usageHubAllowedUpstreamPaths 严格限定 POST /v0/management/api-call 可访问的官方只读额度路径，
// 避免管理密钥泄露后通过 $TOKEN$ 替换造成 SSRF 或凭据外泄。
var usageHubAllowedUpstreamPaths = []string{
	"/backend-api/wham/usage",
	"/backend-api/wham/rate-limit-reset-credits",
	"/backend-api/accounts/check/v4-2023-04-27",
	"/backend-api/subscriptions",
}

// UsageHubConfig 管理端读取的用量 Hub 配置视图（敏感密钥仅返回脱敏摘要）。
type UsageHubConfig struct {
	Enabled             bool     `json:"enabled"`
	IncludeDisabled     bool     `json:"include_disabled"`
	HasKey              bool     `json:"has_key"`
	MaskedKey           string   `json:"masked_key"`
	GeneratedKey        string   `json:"generated_key,omitempty"` // 仅在自动初始化或轮换密钥时返回一次明文
	HubPath             string   `json:"hub_path"`
	AuthFilesPath       string   `json:"auth_files_path"`
	APICallPath         string   `json:"api_call_path"`
	SupportedPlatforms  []string `json:"supported_platforms"`
	AllowedUpstreamURLs []string `json:"allowed_upstream_urls"`
	CacheTTLSeconds     int      `json:"cache_ttl_seconds"`
}

// UpdateUsageHubConfigInput 管理端更新用量 Hub 配置的入参。
type UpdateUsageHubConfigInput struct {
	Enabled         *bool `json:"enabled"`
	IncludeDisabled *bool `json:"include_disabled"`
}

// UsageHubIDTokenClaims 返回给 CLIProxyAPI 客户端（如 T3 Code）的脱敏 ID Token 声明结构。
// 仅包含非敏感身份与订阅元数据，绝不暴露 access_token / refresh_token / 原始签名凭据。
type UsageHubIDTokenClaims struct {
	ChatGPTAccountID string `json:"chatgpt_account_id,omitempty"`
	ChatGPTUserID    string `json:"chatgpt_user_id,omitempty"`
	PlanType         string `json:"plan_type,omitempty"`
	Email            string `json:"email,omitempty"`
	SubActiveStart   string `json:"chatgpt_subscription_active_start,omitempty"`
	SubActiveUntil   string `json:"chatgpt_subscription_active_until,omitempty"`
}

// UsageHubAuthFile 对应 CLIProxyAPI GET /v0/management/auth-files 的单条账号项。
type UsageHubAuthFile struct {
	ID               string                 `json:"id"`
	AuthIndex        string                 `json:"auth_index"`
	Name             string                 `json:"name"`
	Type             string                 `json:"type"`
	Provider         string                 `json:"provider"`
	Label            string                 `json:"label"`
	Status           string                 `json:"status"`
	StatusMessage    string                 `json:"status_message"`
	Disabled         bool                   `json:"disabled"`
	Unavailable      bool                   `json:"unavailable"`
	RuntimeOnly      bool                   `json:"runtime_only"`
	Source           string                 `json:"source"`
	Email            string                 `json:"email,omitempty"`
	Account          string                 `json:"account,omitempty"`
	AccountType      string                 `json:"account_type"`
	PlanType         string                 `json:"plan_type,omitempty"`
	ChatGPTAccountID string                 `json:"chatgpt_account_id,omitempty"`
	AccountID        string                 `json:"account_id,omitempty"`
	IDToken          *UsageHubIDTokenClaims `json:"id_token,omitempty"`
	CreatedAt        string                 `json:"created_at,omitempty"`
	UpdatedAt        string                 `json:"updated_at,omitempty"`
	Modtime          string                 `json:"modtime,omitempty"`
}

// UsageHubAuthFilesResponse 对应 GET /v0/management/auth-files 响应体。
type UsageHubAuthFilesResponse struct {
	Files []UsageHubAuthFile `json:"files"`
}

// UsageHubAPICallRequest 对应 POST /v0/management/api-call 请求体。
// 同时兼容 CLIProxyAPI 新旧客户端的字段命名（auth_index/authIndex、data/body）。
type UsageHubAPICallRequest struct {
	AuthIndex    json.RawMessage   `json:"auth_index"`
	AuthIndexAlt json.RawMessage   `json:"authIndex"`
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	Header       map[string]string `json:"header"`
	Data         string            `json:"data"`
	Body         string            `json:"body"`
}

// UsageHubAPICallResponse 对应 POST /v0/management/api-call 响应体。
type UsageHubAPICallResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	Body       string              `json:"body"`
}

// UsageHubAccountItem 管理端「用量 Hub」页面展示的单条 Codex OAuth 账号汇总信息。
type UsageHubAccountItem struct {
	ID               int64                        `json:"id"`
	Name             string                       `json:"name"`
	Email            string                       `json:"email,omitempty"`
	AuthIndex        string                       `json:"auth_index"`
	AuthFileName     string                       `json:"auth_file_name"`
	ChatGPTAccountID string                       `json:"chatgpt_account_id,omitempty"`
	PlanType         string                       `json:"plan_type,omitempty"`
	Status           string                       `json:"status"`
	Schedulable      bool                         `json:"schedulable"`
	ExposedToHub     bool                         `json:"exposed_to_hub"`
	RateLimited      bool                         `json:"rate_limited"`
	RateLimitResetAt *time.Time                   `json:"rate_limit_reset_at,omitempty"`
	ErrorMessage     string                       `json:"error_message,omitempty"`
	FiveHour         *UsageProgress               `json:"five_hour,omitempty"`
	SevenDay         *UsageProgress               `json:"seven_day,omitempty"`
	ResetCredits     *OpenAIRateLimitResetCredits `json:"reset_credits,omitempty"`
	Credits          *OpenAICredits               `json:"credits,omitempty"`
	UpdatedAt        *time.Time                   `json:"updated_at,omitempty"`
	LastUsedAt       *time.Time                   `json:"last_used_at,omitempty"`
}

// UsageHubSummary 管理端用量 Hub 聚合统计指标。
type UsageHubSummary struct {
	TotalAccounts       int      `json:"total_accounts"`
	ExposedAccounts     int      `json:"exposed_accounts"`
	HealthyAccounts     int      `json:"healthy_accounts"`
	RateLimitedAccounts int      `json:"rate_limited_accounts"`
	ErrorAccounts       int      `json:"error_accounts"`
	DisabledAccounts    int      `json:"disabled_accounts"`
	Known5hCount        int      `json:"known_5h_count"`
	Known7dCount        int      `json:"known_7d_count"`
	Avg5hUsedPercent    *float64 `json:"avg_5h_used_percent,omitempty"`
	Avg7dUsedPercent    *float64 `json:"avg_7d_used_percent,omitempty"`
	TotalResetCredits   int      `json:"total_reset_credits"`
}

// UsageHubOverviewResponse 管理端用量 Hub 列表与聚合统计响应。
type UsageHubOverviewResponse struct {
	Summary  UsageHubSummary       `json:"summary"`
	Accounts []UsageHubAccountItem `json:"accounts"`
}

// UsageHubBatchRefreshError 批量刷新单账号失败详情。
type UsageHubBatchRefreshError struct {
	AccountID int64  `json:"account_id"`
	Name      string `json:"name"`
	Error     string `json:"error"`
}

// UsageHubBatchRefreshResponse 管理端批量刷新 Codex OAuth 额度响应。
type UsageHubBatchRefreshResponse struct {
	Refreshed int                         `json:"refreshed"`
	Failed    int                         `json:"failed"`
	Errors    []UsageHubBatchRefreshError `json:"errors,omitempty"`
	Overview  *UsageHubOverviewResponse   `json:"overview"`
}

type usageHubAPICallCacheEntry struct {
	response  *UsageHubAPICallResponse
	timestamp time.Time
}

// UsageHubService 提供面向 T3 Code 等客户端的 CLIProxyAPI 兼容用量 Hub 服务，
// 以及管理后台的全站 Codex OAuth 额度聚合视图。
type UsageHubService struct {
	settingRepo  SettingRepository
	accountRepo  AccountRepository
	quotaService *OpenAIQuotaService

	cacheTTL     time.Duration
	apiCallCache sync.Map // cacheKey -> *usageHubAPICallCacheEntry
	apiCallSF    singleflight.Group
}

// NewUsageHubService 创建 UsageHubService 实例。
func NewUsageHubService(
	settingRepo SettingRepository,
	accountRepo AccountRepository,
	quotaService *OpenAIQuotaService,
) *UsageHubService {
	return &UsageHubService{
		settingRepo:  settingRepo,
		accountRepo:  accountRepo,
		quotaService: quotaService,
		cacheTTL:     usageHubDefaultCacheTTL,
	}
}

// GetConfig 获取用量 Hub 当前配置与脱敏密钥状态。
func (s *UsageHubService) GetConfig(ctx context.Context) (*UsageHubConfig, error) {
	if s == nil || s.settingRepo == nil {
		return nil, infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "usage hub service is not configured")
	}

	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyUsageHubEnabled,
		SettingKeyUsageHubManagementKey,
		SettingKeyUsageHubIncludeDisabled,
	})
	if err != nil {
		return nil, fmt.Errorf("load usage hub settings: %w", err)
	}

	rawKey := strings.TrimSpace(values[SettingKeyUsageHubManagementKey])
	enabled := strings.EqualFold(strings.TrimSpace(values[SettingKeyUsageHubEnabled]), "true")
	includeDisabled := strings.EqualFold(strings.TrimSpace(values[SettingKeyUsageHubIncludeDisabled]), "true")

	return s.buildConfigResponse(enabled, includeDisabled, rawKey, ""), nil
}

// UpdateConfig 更新用量 Hub 开关与暴露范围配置。
// 若首次开启且尚未生成过管理密钥，会自动签发一份密钥并在本次响应中返回明文。
func (s *UsageHubService) UpdateConfig(ctx context.Context, input UpdateUsageHubConfigInput) (*UsageHubConfig, error) {
	if s == nil || s.settingRepo == nil {
		return nil, infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "usage hub service is not configured")
	}

	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyUsageHubEnabled,
		SettingKeyUsageHubManagementKey,
		SettingKeyUsageHubIncludeDisabled,
	})
	if err != nil {
		return nil, fmt.Errorf("load usage hub settings: %w", err)
	}

	enabled := strings.EqualFold(strings.TrimSpace(values[SettingKeyUsageHubEnabled]), "true")
	includeDisabled := strings.EqualFold(strings.TrimSpace(values[SettingKeyUsageHubIncludeDisabled]), "true")
	rawKey := strings.TrimSpace(values[SettingKeyUsageHubManagementKey])

	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if input.IncludeDisabled != nil {
		includeDisabled = *input.IncludeDisabled
	}

	updates := map[string]string{
		SettingKeyUsageHubEnabled:         strconv.FormatBool(enabled),
		SettingKeyUsageHubIncludeDisabled: strconv.FormatBool(includeDisabled),
	}

	var generatedKey string
	if enabled && rawKey == "" {
		newKey, genErr := generateUsageHubManagementKey()
		if genErr != nil {
			return nil, fmt.Errorf("generate usage hub key: %w", genErr)
		}
		rawKey = newKey
		generatedKey = newKey
		updates[SettingKeyUsageHubManagementKey] = newKey
	}

	if err := s.settingRepo.SetMultiple(ctx, updates); err != nil {
		return nil, fmt.Errorf("save usage hub settings: %w", err)
	}

	return s.buildConfigResponse(enabled, includeDisabled, rawKey, generatedKey), nil
}

// RegenerateKey 生成或轮换用量 Hub 专属管理密钥，明文仅在本次响应返回一次。
func (s *UsageHubService) RegenerateKey(ctx context.Context) (string, string, error) {
	if s == nil || s.settingRepo == nil {
		return "", "", infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "usage hub service is not configured")
	}

	key, err := generateUsageHubManagementKey()
	if err != nil {
		return "", "", fmt.Errorf("generate usage hub key: %w", err)
	}

	if err := s.settingRepo.Set(ctx, SettingKeyUsageHubManagementKey, key); err != nil {
		return "", "", fmt.Errorf("save usage hub key: %w", err)
	}

	return key, maskUsageHubKey(key), nil
}

// DeleteKey 删除用量 Hub 管理密钥并自动关闭对外 Hub 访问。
func (s *UsageHubService) DeleteKey(ctx context.Context) error {
	if s == nil || s.settingRepo == nil {
		return infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "usage hub service is not configured")
	}

	if err := s.settingRepo.Set(ctx, SettingKeyUsageHubEnabled, "false"); err != nil {
		return fmt.Errorf("disable usage hub: %w", err)
	}
	if err := s.settingRepo.Delete(ctx, SettingKeyUsageHubManagementKey); err != nil && !errors.Is(err, ErrSettingNotFound) {
		return fmt.Errorf("delete usage hub key: %w", err)
	}
	return nil
}

// ValidateManagementKey 校验对外 CLIProxyAPI 兼容请求携带的管理密钥。
func (s *UsageHubService) ValidateManagementKey(ctx context.Context, candidate string) error {
	if s == nil || s.settingRepo == nil {
		return ErrUsageHubDisabled
	}

	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyUsageHubEnabled,
		SettingKeyUsageHubManagementKey,
	})
	if err != nil {
		return fmt.Errorf("load usage hub auth config: %w", err)
	}

	enabled := strings.EqualFold(strings.TrimSpace(values[SettingKeyUsageHubEnabled]), "true")
	storedKey := strings.TrimSpace(values[SettingKeyUsageHubManagementKey])
	if !enabled || storedKey == "" {
		return ErrUsageHubDisabled
	}

	candidate = strings.TrimSpace(candidate)
	if candidate == "" || len(candidate) != len(storedKey) {
		return ErrUsageHubUnauthorized
	}
	if subtle.ConstantTimeCompare([]byte(candidate), []byte(storedKey)) != 1 {
		return ErrUsageHubUnauthorized
	}
	return nil
}

// ListAuthFiles 生成符合 CLIProxyAPI GET /v0/management/auth-files 规范的全站 Codex OAuth 账号列表。
func (s *UsageHubService) ListAuthFiles(ctx context.Context) (*UsageHubAuthFilesResponse, error) {
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "usage hub service is not configured")
	}

	includeDisabled := s.isIncludeDisabledEnabled(ctx)
	accounts, err := s.listCodexOAuthAccounts(ctx)
	if err != nil {
		return nil, err
	}

	files := make([]UsageHubAuthFile, 0, len(accounts))
	for i := range accounts {
		acc := &accounts[i]
		if !includeDisabled && acc.Status == StatusDisabled {
			continue
		}
		files = append(files, buildUsageHubAuthFile(acc))
	}

	return &UsageHubAuthFilesResponse{Files: files}, nil
}

// ExecuteAPICall 实现受白名单保护的 CLIProxyAPI POST /v0/management/api-call，
// 使用账号已刷新且绑定代理的客户端向上游官方额度接口发起只读请求。
func (s *UsageHubService) ExecuteAPICall(ctx context.Context, req UsageHubAPICallRequest) (*UsageHubAPICallResponse, error) {
	if s == nil || s.accountRepo == nil || s.quotaService == nil {
		return nil, infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "usage hub service is not configured")
	}

	accountID, err := parseUsageHubAuthIndex(req.AuthIndex, req.AuthIndexAlt)
	if err != nil {
		return nil, infraerrors.BadRequest("USAGE_HUB_INVALID_AUTH_INDEX", err.Error())
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet {
		return nil, ErrUsageHubForbiddenURL
	}

	normalizedURL, parsedURL, err := validateUsageHubUpstreamURL(req.URL)
	if err != nil {
		return nil, err
	}

	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return nil, ErrUsageHubAccountNotFound
	}
	if !isUsageHubCodexAccount(account) {
		return nil, ErrUsageHubAccountNotFound
	}
	if !s.isIncludeDisabledEnabled(ctx) && account.Status == StatusDisabled {
		return nil, ErrUsageHubAccountNotFound
	}

	// 按账号 + 标准化 URL 命中短时缓存，避免客户端短时间并发刷新触发上游 429。
	cacheKey := fmt.Sprintf("%d|%s", accountID, normalizedURL)
	if cached := s.getCachedAPICall(cacheKey); cached != nil {
		return cached, nil
	}

	result, flightErr, _ := s.apiCallSF.Do(cacheKey, func() (any, error) {
		if cached := s.getCachedAPICall(cacheKey); cached != nil {
			return cached, nil
		}

		resp, callErr := s.executeUpstreamCodexCall(ctx, accountID, normalizedURL, parsedURL.Path, req.Header)
		if callErr != nil {
			return nil, callErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			s.apiCallCache.Store(cacheKey, &usageHubAPICallCacheEntry{
				response:  resp,
				timestamp: time.Now(),
			})
		}
		return resp, nil
	})
	if flightErr != nil {
		return nil, flightErr
	}

	apiResp, ok := result.(*UsageHubAPICallResponse)
	if !ok || apiResp == nil {
		return nil, infraerrors.New(http.StatusBadGateway, "USAGE_HUB_EMPTY_RESPONSE", "upstream call returned empty response")
	}
	return apiResp, nil
}

// ListAccountsOverview 返回管理端「用量 Hub」页面所需的全站 Codex OAuth 账号列表与汇总统计。
func (s *UsageHubService) ListAccountsOverview(ctx context.Context) (*UsageHubOverviewResponse, error) {
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "usage hub service is not configured")
	}

	includeDisabled := s.isIncludeDisabledEnabled(ctx)
	accounts, err := s.listCodexOAuthAccounts(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	items := make([]UsageHubAccountItem, 0, len(accounts))
	summary := UsageHubSummary{
		TotalAccounts: len(accounts),
	}

	var sum5h, sum7d float64
	for i := range accounts {
		acc := &accounts[i]
		item := buildUsageHubAccountItem(acc, includeDisabled, now)
		items = append(items, item)

		if item.ExposedToHub {
			summary.ExposedAccounts++
		}
		switch {
		case item.Status == StatusDisabled:
			summary.DisabledAccounts++
		case item.Status == StatusError:
			summary.ErrorAccounts++
		case item.RateLimited:
			summary.RateLimitedAccounts++
		default:
			summary.HealthyAccounts++
		}
		if item.FiveHour != nil && !item.FiveHour.UtilizationUnknown {
			sum5h += item.FiveHour.Utilization
			summary.Known5hCount++
		}
		if item.SevenDay != nil && !item.SevenDay.UtilizationUnknown {
			sum7d += item.SevenDay.Utilization
			summary.Known7dCount++
		}
		if item.ResetCredits != nil && item.ResetCredits.AvailableCount > 0 {
			summary.TotalResetCredits += item.ResetCredits.AvailableCount
		}
	}

	if summary.Known5hCount > 0 {
		avg := sum5h / float64(summary.Known5hCount)
		summary.Avg5hUsedPercent = &avg
	}
	if summary.Known7dCount > 0 {
		avg := sum7d / float64(summary.Known7dCount)
		summary.Avg7dUsedPercent = &avg
	}

	return &UsageHubOverviewResponse{
		Summary:  summary,
		Accounts: items,
	}, nil
}

// RefreshAccountQuota 主动查询指定 Codex OAuth 账号的实时额度并同步快照到账号 Extra。
func (s *UsageHubService) RefreshAccountQuota(ctx context.Context, accountID int64) (*UsageHubAccountItem, error) {
	if s == nil || s.accountRepo == nil || s.quotaService == nil {
		return nil, infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "usage hub service is not configured")
	}

	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil || !isUsageHubCodexAccount(account) {
		return nil, ErrUsageHubAccountNotFound
	}

	usage, err := s.quotaService.QueryUsage(ctx, accountID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	s.persistCodexUsageSnapshot(ctx, accountID, usage, now)
	s.invalidateAccountCache(accountID)

	refreshed, reloadErr := s.accountRepo.GetByID(ctx, accountID)
	if reloadErr == nil && refreshed != nil {
		account = refreshed
	} else {
		// DB 重载失败时回退到内存合并，保证返回最新快照
		if updates := buildOpenAIAutoResetUsageUpdates(usage, now); len(updates) > 0 {
			mergeAccountExtra(account, updates)
		}
	}

	item := buildUsageHubAccountItem(account, s.isIncludeDisabledEnabled(ctx), now)
	return &item, nil
}

// RefreshAllAccountsQuota 批量主动刷新全站（或指定列表）Codex OAuth 账号的实时额度。
func (s *UsageHubService) RefreshAllAccountsQuota(ctx context.Context, accountIDs []int64) (*UsageHubBatchRefreshResponse, error) {
	if s == nil || s.accountRepo == nil || s.quotaService == nil {
		return nil, infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "usage hub service is not configured")
	}

	accounts, err := s.listCodexOAuthAccounts(ctx)
	if err != nil {
		return nil, err
	}

	filterSet := make(map[int64]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		if id > 0 {
			filterSet[id] = struct{}{}
		}
	}

	targets := make([]Account, 0, len(accounts))
	for _, acc := range accounts {
		if len(filterSet) > 0 {
			if _, ok := filterSet[acc.ID]; !ok {
				continue
			}
		} else if acc.Status == StatusDisabled {
			// 默认批量刷新跳过手动禁用的账号，避免无意义的上游请求
			continue
		}
		targets = append(targets, acc)
	}

	var (
		mu        sync.Mutex
		refreshed int
		errs      []UsageHubBatchRefreshError
	)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(usageHubBatchRefreshLimit)

	for _, target := range targets {
		acc := target
		g.Go(func() error {
			usage, qErr := s.quotaService.QueryUsage(gctx, acc.ID)
			if qErr != nil {
				mu.Lock()
				errs = append(errs, UsageHubBatchRefreshError{
					AccountID: acc.ID,
					Name:      acc.Name,
					Error:     qErr.Error(),
				})
				mu.Unlock()
				return nil
			}
			s.persistCodexUsageSnapshot(gctx, acc.ID, usage, time.Now())
			s.invalidateAccountCache(acc.ID)
			mu.Lock()
			refreshed++
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	sort.Slice(errs, func(i, j int) bool {
		return errs[i].AccountID < errs[j].AccountID
	})

	overview, err := s.ListAccountsOverview(ctx)
	if err != nil {
		return nil, err
	}

	return &UsageHubBatchRefreshResponse{
		Refreshed: refreshed,
		Failed:    len(errs),
		Errors:    errs,
		Overview:  overview,
	}, nil
}

func (s *UsageHubService) executeUpstreamCodexCall(
	ctx context.Context,
	accountID int64,
	normalizedURL string,
	urlPath string,
	clientHeaders map[string]string,
) (*UsageHubAPICallResponse, error) {
	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.quotaService.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return nil, err
	}

	client, err := s.quotaService.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "USAGE_HUB_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()
	agentIdentity := s.quotaService.isAgentIdentityAccount(ctx, accountID)

	var (
		statusCode int
		respHeader map[string][]string
		respBody   string
	)

	for recovered := false; ; {
		baseHeaders, expectedTaskID, headerErr := s.quotaService.buildCodexQuotaHeaders(callCtx, accountID, accessToken, chatGPTAccountID, fedRAMP)
		if headerErr != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "USAGE_HUB_AUTH_FAILED", "failed to build upstream authentication: %v", headerErr)
		}

		mergedHeaders := mergeUsageHubCodexHeaders(baseHeaders, clientHeaders, accessToken, chatGPTAccountID, agentIdentity)
		resp, reqErr := client.R().
			SetContext(callCtx).
			SetHeaders(mergedHeaders).
			Get(normalizedURL)
		if reqErr != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "USAGE_HUB_REQUEST_FAILED", "upstream request failed: %v", reqErr)
		}

		statusCode = resp.StatusCode
		respBody = resp.String()
		if !resp.IsSuccessState() && agentIdentity && !recovered && isAgentIdentityTaskInvalidHTTPResponse(statusCode, []byte(respBody)) {
			recovered = true
			if recErr := s.quotaService.recoverAgentIdentityTask(ctx, accountID, expectedTaskID); recErr != nil {
				return nil, infraerrors.Newf(http.StatusBadGateway, "USAGE_HUB_AUTH_FAILED", "agent identity task recovery failed: %v", recErr)
			}
			continue
		}

		respHeader = sanitizeUsageHubResponseHeaders(resp.Header)
		break
	}

	// 当命中 /backend-api/wham/usage 且成功返回时，顺便轻量同步快照到账号 Extra，让后台页面也保持最新。
	if statusCode >= 200 && statusCode < 300 && urlPath == "/backend-api/wham/usage" {
		s.trySyncWhamUsageBody(ctx, accountID, respBody)
	}

	return &UsageHubAPICallResponse{
		StatusCode: statusCode,
		Header:     respHeader,
		Body:       respBody,
	}, nil
}

func (s *UsageHubService) trySyncWhamUsageBody(ctx context.Context, accountID int64, body string) {
	if strings.TrimSpace(body) == "" {
		return
	}
	var payload OpenAIQuotaUsage
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return
	}
	now := time.Now()
	payload.FetchedAt = now.Unix()
	s.persistCodexUsageSnapshot(ctx, accountID, &payload, now)
}

func (s *UsageHubService) persistCodexUsageSnapshot(ctx context.Context, accountID int64, usage *OpenAIQuotaUsage, now time.Time) {
	if s == nil || s.accountRepo == nil || usage == nil {
		return
	}
	updates := buildOpenAIAutoResetUsageUpdates(usage, now)
	if updates == nil {
		updates = make(map[string]any)
	}
	if planType := strings.TrimSpace(usage.PlanType); planType != "" {
		updates["chatgpt_plan_type"] = planType
	}
	if usage.Credits != nil {
		updates[openaiQuotaCreditsKey] = openAICreditsSnapshot{
			Credits:   usage.Credits,
			FetchedAt: usage.FetchedAt,
		}
	}
	if usage.RateLimitResetCredits != nil && (usage.RateLimitResetCredits.AvailableCount == 0 || len(usage.RateLimitResetCredits.Credits) > 0) {
		updates[openaiQuotaResetCreditsKey] = usage.RateLimitResetCredits
	}
	if len(updates) == 0 {
		return
	}
	if err := s.accountRepo.UpdateExtra(ctx, accountID, updates); err != nil {
		slog.Warn("usage_hub_persist_snapshot_failed", "account_id", accountID, "error", err)
	}
}

func (s *UsageHubService) getCachedAPICall(cacheKey string) *UsageHubAPICallResponse {
	ttl := s.cacheTTL
	if ttl <= 0 {
		return nil
	}
	raw, ok := s.apiCallCache.Load(cacheKey)
	if !ok {
		return nil
	}
	entry, ok := raw.(*usageHubAPICallCacheEntry)
	if !ok || entry == nil || entry.response == nil {
		return nil
	}
	if time.Since(entry.timestamp) >= ttl {
		return nil
	}
	return entry.response
}

func (s *UsageHubService) invalidateAccountCache(accountID int64) {
	prefix := fmt.Sprintf("%d|", accountID)
	s.apiCallCache.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
			s.apiCallCache.Delete(k)
		}
		return true
	})
}

func (s *UsageHubService) isIncludeDisabledEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyUsageHubIncludeDisabled)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(raw), "true")
}

func (s *UsageHubService) listCodexOAuthAccounts(ctx context.Context) ([]Account, error) {
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return nil, fmt.Errorf("list openai accounts: %w", err)
	}

	filtered := make([]Account, 0, len(accounts))
	for _, acc := range accounts {
		if isUsageHubCodexAccount(&acc) {
			filtered = append(filtered, acc)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})
	return filtered, nil
}

func (s *UsageHubService) buildConfigResponse(enabled, includeDisabled bool, rawKey, generatedKey string) *UsageHubConfig {
	allowedURLs := make([]string, 0, len(usageHubAllowedUpstreamPaths))
	for _, p := range usageHubAllowedUpstreamPaths {
		allowedURLs = append(allowedURLs, "https://chatgpt.com"+p)
	}
	return &UsageHubConfig{
		Enabled:             enabled,
		IncludeDisabled:     includeDisabled,
		HasKey:              rawKey != "",
		MaskedKey:           maskUsageHubKey(rawKey),
		GeneratedKey:        generatedKey,
		HubPath:             "/v0/management",
		AuthFilesPath:       "/v0/management/auth-files",
		APICallPath:         "/v0/management/api-call",
		SupportedPlatforms:  []string{"codex"},
		AllowedUpstreamURLs: allowedURLs,
		CacheTTLSeconds:     int(s.cacheTTL.Seconds()),
	}
}

func isUsageHubCodexAccount(account *Account) bool {
	return account != nil &&
		account.Platform == PlatformOpenAI &&
		account.Type == AccountTypeOAuth &&
		!account.IsShadow()
}

type extractedCodexIdentity struct {
	Email            string
	ChatGPTAccountID string
	ChatGPTUserID    string
	PlanType         string
	SubActiveStart   string
	SubActiveUntil   string
}

func extractCodexAccountIdentity(account *Account) extractedCodexIdentity {
	var id extractedCodexIdentity
	if account == nil {
		return id
	}

	id.Email = strings.TrimSpace(account.GetCredential("email"))
	id.ChatGPTAccountID = strings.TrimSpace(account.GetChatGPTAccountID())
	if id.ChatGPTAccountID == "" {
		id.ChatGPTAccountID = strings.TrimSpace(account.GetCredential("organization_id"))
	}
	id.ChatGPTUserID = strings.TrimSpace(account.GetCredential("chatgpt_user_id"))
	id.PlanType = usageHubFirstNonEmpty(
		account.GetExtraString("chatgpt_plan_type"),
		account.GetCredential("plan_type"),
		account.GetExtraString("codex_plan_type"),
		account.GetExtraString("plan_type"),
	)

	if rawIDToken := strings.TrimSpace(account.GetOpenAIIDToken()); rawIDToken != "" {
		if claims, err := openaipkg.DecodeIDToken(rawIDToken); err == nil && claims != nil {
			userInfo := claims.GetUserInfo()
			if id.Email == "" {
				id.Email = strings.TrimSpace(userInfo.Email)
			}
			if id.ChatGPTAccountID == "" {
				id.ChatGPTAccountID = strings.TrimSpace(userInfo.ChatGPTAccountID)
			}
			if id.ChatGPTUserID == "" {
				id.ChatGPTUserID = strings.TrimSpace(userInfo.ChatGPTUserID)
			}
			if id.PlanType == "" {
				id.PlanType = strings.TrimSpace(userInfo.PlanType)
			}
		}
		if start, until := extractSubscriptionTimelineFromJWT(rawIDToken); start != "" || until != "" {
			id.SubActiveStart = start
			id.SubActiveUntil = until
		}
	}

	if id.Email == "" {
		id.Email = strings.TrimSpace(account.GetExtraString("email"))
	}
	return id
}

func extractSubscriptionTimelineFromJWT(jwtStr string) (start, until string) {
	parts := strings.Split(jwtStr, ".")
	if len(parts) != 3 {
		return "", ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ""
	}
	var raw map[string]any
	if err := json.Unmarshal(decoded, &raw); err != nil {
		return "", ""
	}
	authObj, _ := raw["https://api.openai.com/auth"].(map[string]any)
	if authObj == nil {
		return "", ""
	}
	start, _ = authObj["chatgpt_subscription_active_start"].(string)
	until, _ = authObj["chatgpt_subscription_active_until"].(string)
	return strings.TrimSpace(start), strings.TrimSpace(until)
}

func buildUsageHubAuthFile(account *Account) UsageHubAuthFile {
	identity := extractCodexAccountIdentity(account)
	authIndex := formatUsageHubAuthIndex(account.ID)
	fileName := buildUsageHubAuthFileName(account.ID, identity.Email)
	label := usageHubFirstNonEmpty(account.Name, identity.Email, fmt.Sprintf("Codex #%d", account.ID))
	accountDisplay := usageHubFirstNonEmpty(identity.Email, label)

	// disabled 仅在账号被显式禁用时为 true；限流/自动暂停调度保持 disabled=false，
	// 确保 T3 Code 仍会向 /wham/usage 拉取其 100% 占用率与重置倒计时。
	disabled := account.Status == StatusDisabled
	unavailable := disabled ||
		account.Status == StatusError ||
		account.IsRateLimited() ||
		(account.TempUnschedulableUntil != nil && time.Now().Before(*account.TempUnschedulableUntil))

	status := "active"
	statusMessage := strings.TrimSpace(account.ErrorMessage)
	switch {
	case disabled:
		status = "disabled"
	case account.Status == StatusError:
		status = "error"
	case account.IsRateLimited():
		if statusMessage == "" && account.RateLimitResetAt != nil {
			statusMessage = fmt.Sprintf("rate limited until %s", account.RateLimitResetAt.UTC().Format(time.RFC3339))
		}
	}

	var idTokenClaims *UsageHubIDTokenClaims
	if identity.ChatGPTAccountID != "" || identity.PlanType != "" || identity.Email != "" || identity.ChatGPTUserID != "" {
		idTokenClaims = &UsageHubIDTokenClaims{
			ChatGPTAccountID: identity.ChatGPTAccountID,
			ChatGPTUserID:    identity.ChatGPTUserID,
			PlanType:         identity.PlanType,
			Email:            identity.Email,
			SubActiveStart:   identity.SubActiveStart,
			SubActiveUntil:   identity.SubActiveUntil,
		}
	}

	var createdAt, updatedAt string
	if !account.CreatedAt.IsZero() {
		createdAt = account.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !account.UpdatedAt.IsZero() {
		updatedAt = account.UpdatedAt.UTC().Format(time.RFC3339)
	}

	return UsageHubAuthFile{
		ID:               fileName,
		AuthIndex:        authIndex,
		Name:             fileName,
		Type:             "codex",
		Provider:         "codex",
		Label:            label,
		Status:           status,
		StatusMessage:    statusMessage,
		Disabled:         disabled,
		Unavailable:      unavailable,
		RuntimeOnly:      false,
		Source:           "memory",
		Email:            identity.Email,
		Account:          accountDisplay,
		AccountType:      "oauth",
		PlanType:         identity.PlanType,
		ChatGPTAccountID: identity.ChatGPTAccountID,
		AccountID:        identity.ChatGPTAccountID,
		IDToken:          idTokenClaims,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
		Modtime:          updatedAt,
	}
}

func buildUsageHubAccountItem(account *Account, includeDisabled bool, now time.Time) UsageHubAccountItem {
	identity := extractCodexAccountIdentity(account)
	usage := &UsageInfo{}
	applyExtraToUsage(usage, account.Extra, now)

	item := UsageHubAccountItem{
		ID:               account.ID,
		Name:             usageHubFirstNonEmpty(account.Name, identity.Email, fmt.Sprintf("Codex #%d", account.ID)),
		Email:            identity.Email,
		AuthIndex:        formatUsageHubAuthIndex(account.ID),
		AuthFileName:     buildUsageHubAuthFileName(account.ID, identity.Email),
		ChatGPTAccountID: identity.ChatGPTAccountID,
		PlanType:         identity.PlanType,
		Status:           account.Status,
		Schedulable:      account.Schedulable,
		ExposedToHub:     includeDisabled || account.Status != StatusDisabled,
		RateLimited:      account.IsRateLimited(),
		RateLimitResetAt: account.RateLimitResetAt,
		ErrorMessage:     strings.TrimSpace(account.ErrorMessage),
		FiveHour:         usage.FiveHour,
		SevenDay:         usage.SevenDay,
		ResetCredits:     extractOpenAIResetCreditsFromExtra(account.Extra),
		Credits:          extractOpenAICreditsFromExtra(account.Extra),
		UpdatedAt:        usage.UpdatedAt,
		LastUsedAt:       account.LastUsedAt,
	}
	return item
}

func extractOpenAIResetCreditsFromExtra(extra map[string]any) *OpenAIRateLimitResetCredits {
	if len(extra) == 0 {
		return nil
	}
	raw, ok := extra[openaiQuotaResetCreditsKey]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var credits OpenAIRateLimitResetCredits
	if err := json.Unmarshal(data, &credits); err != nil {
		return nil
	}
	return &credits
}

func extractOpenAICreditsFromExtra(extra map[string]any) *OpenAICredits {
	if len(extra) == 0 {
		return nil
	}
	raw, ok := extra[openaiQuotaCreditsKey]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var snap openAICreditsSnapshot
	if err := json.Unmarshal(data, &snap); err != nil || snap.Credits == nil {
		return nil
	}
	return snap.Credits
}

func formatUsageHubAuthIndex(accountID int64) string {
	return fmt.Sprintf("%s%d", usageHubAuthIndexPrefix, accountID)
}

func buildUsageHubAuthFileName(accountID int64, email string) string {
	sanitized := sanitizeAuthFileToken(email)
	if sanitized == "" {
		return fmt.Sprintf("codex-account-%d.json", accountID)
	}
	return fmt.Sprintf("codex-%s-%d.json", sanitized, accountID)
}

func sanitizeAuthFileToken(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '@' || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "._-")
}

func parseUsageHubAuthIndex(primary, fallback json.RawMessage) (int64, error) {
	raw := primary
	if len(bytesTrimSpace(raw)) == 0 {
		raw = fallback
	}
	trimmed := string(bytesTrimSpace(raw))
	if trimmed == "" || trimmed == "null" {
		return 0, fmt.Errorf("auth_index is required")
	}

	// 同时兼容 JSON 字符串（如 "sub2api-codex-42" / "42"）与纯数字（42）
	var strVal string
	if err := json.Unmarshal([]byte(trimmed), &strVal); err == nil {
		strVal = strings.TrimSpace(strVal)
		strVal = strings.TrimPrefix(strVal, usageHubAuthIndexPrefix)
		id, parseErr := strconv.ParseInt(strVal, 10, 64)
		if parseErr != nil || id <= 0 {
			return 0, fmt.Errorf("invalid auth_index")
		}
		return id, nil
	}

	var numVal int64
	if err := json.Unmarshal([]byte(trimmed), &numVal); err == nil && numVal > 0 {
		return numVal, nil
	}

	return 0, fmt.Errorf("invalid auth_index")
}

func bytesTrimSpace(raw []byte) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

func validateUsageHubUpstreamURL(rawURL string) (string, *url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", nil, infraerrors.BadRequest("USAGE_HUB_INVALID_URL", "url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, infraerrors.BadRequest("USAGE_HUB_INVALID_URL", "invalid url")
	}
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil || parsed.Opaque != "" {
		return "", nil, ErrUsageHubForbiddenURL
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host != "chatgpt.com" {
		return "", nil, ErrUsageHubForbiddenURL
	}
	if parsed.Port() != "" && parsed.Port() != "443" {
		return "", nil, ErrUsageHubForbiddenURL
	}
	if strings.Contains(parsed.Path, "..") {
		return "", nil, ErrUsageHubForbiddenURL
	}

	allowed := false
	for _, allowedPath := range usageHubAllowedUpstreamPaths {
		if parsed.Path == allowedPath {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", nil, ErrUsageHubForbiddenURL
	}

	normalized := url.URL{
		Scheme:   "https",
		Host:     "chatgpt.com",
		Path:     parsed.Path,
		RawQuery: parsed.RawQuery,
	}
	return normalized.String(), parsed, nil
}

func mergeUsageHubCodexHeaders(
	baseHeaders map[string]string,
	clientHeaders map[string]string,
	accessToken string,
	chatGPTAccountID string,
	agentIdentity bool,
) map[string]string {
	merged := make(map[string]string, len(baseHeaders)+len(clientHeaders))
	for k, v := range baseHeaders {
		merged[strings.ToLower(strings.TrimSpace(k))] = v
	}

	for k, v := range clientHeaders {
		lower := strings.ToLower(strings.TrimSpace(k))
		if lower == "" {
			continue
		}
		// 禁止客户端覆盖逐跳头或 Host，避免破坏传输层与反代安全。
		switch lower {
		case "host", "content-length", "connection", "transfer-encoding", "proxy-authorization", "x-forwarded-for", "x-real-ip":
			continue
		}
		replaced := strings.ReplaceAll(v, "$TOKEN$", accessToken)
		if lower == "authorization" {
			// Agent Identity 账号必须使用内部生成的 assertion，不能被 Bearer 覆盖。
			if agentIdentity {
				continue
			}
			// 若客户端未传有效 Authorization 或仍残留占位符，保留基线头。
			if strings.TrimSpace(replaced) == "" || strings.EqualFold(strings.TrimSpace(replaced), "bearer") {
				continue
			}
			merged[lower] = replaced
			continue
		}
		if lower == "chatgpt-account-id" {
			// 若客户端传空值，则保留账号自身提取的 chatGPTAccountID。
			if strings.TrimSpace(replaced) == "" {
				continue
			}
			merged[lower] = strings.TrimSpace(replaced)
			continue
		}
		merged[lower] = replaced
	}

	if strings.TrimSpace(merged["chatgpt-account-id"]) == "" && chatGPTAccountID != "" {
		merged["chatgpt-account-id"] = chatGPTAccountID
	}
	return merged
}

func sanitizeUsageHubResponseHeaders(raw http.Header) map[string][]string {
	cleaned := make(map[string][]string, len(raw))
	count := 0
	for k, vals := range raw {
		if count >= usageHubMaxResponseHeaderSize {
			break
		}
		lower := strings.ToLower(k)
		if lower == "set-cookie" || lower == "set-cookie2" {
			continue
		}
		cp := make([]string, len(vals))
		copy(cp, vals)
		cleaned[k] = cp
		count++
	}
	return cleaned
}

func generateUsageHubManagementKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return UsageHubKeyPrefix + hex.EncodeToString(buf), nil
}

func maskUsageHubKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) > 14 {
		return key[:10] + "..." + key[len(key)-4:]
	}
	return "****"
}

func usageHubFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
