package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const (
	usageHubMaxMagpieSources   = 5
	usageHubMagpieTimeout      = 15 * time.Second
	usageHubMagpieMaxBodyBytes = 2 * 1024 * 1024
	usageHubMagpieAuthPrefix   = "magpie:"

	usageHubProviderCodex       = "codex"
	usageHubProviderClaude      = "claude"
	usageHubProviderAntigravity = "antigravity"

	anthropicOAuthUsagePath = "/api/oauth/usage"
	antigravitySummaryPath  = "/v1internal:retrieveUserQuotaSummary"
	googleUserInfoPath      = "/oauth2/v2/userinfo"
)

var (
	// ErrUsageHubUserConfigNotFound 用户尚未初始化自助用量 Hub 配置。
	ErrUsageHubUserConfigNotFound = infraerrors.NotFound("USAGE_HUB_USER_CONFIG_NOT_FOUND", "user usage hub config not found")
	// ErrUsageHubSelfServiceDisabled 管理员未开启用户自助用量 Hub 功能。
	ErrUsageHubSelfServiceDisabled = infraerrors.Forbidden("USAGE_HUB_SELF_SERVICE_DISABLED", "self-service usage hub is disabled by administrator")
	// ErrUsageHubMagpieDisabled 管理员未开启自定义外部 Magpie 接入功能。
	ErrUsageHubMagpieDisabled = infraerrors.Forbidden("USAGE_HUB_MAGPIE_DISABLED", "custom magpie sources are disabled by administrator")

	usageHubSupportedMagpieProviders = []string{
		usageHubProviderCodex,
		usageHubProviderClaude,
		usageHubProviderAntigravity,
	}
)

// UserUsageHubRepository 定义用户自助用量 Hub 配置的持久化接口。
type UserUsageHubRepository interface {
	GetByUserID(ctx context.Context, userID int64) (*UserUsageHubRecord, error)
	GetByTokenHash(ctx context.Context, tokenHash string) (*UserUsageHubRecord, error)
	Upsert(ctx context.Context, record *UserUsageHubRecord) error
	DeleteToken(ctx context.Context, userID int64) error
	CountEnabled(ctx context.Context) (int64, error)
}

// UsageHubMagpieSourceRecord 持久化在数据库中的外部 Magpie 数据源配置（密钥以密文存储）。
type UsageHubMagpieSourceRecord struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Enabled         bool     `json:"enabled"`
	BaseURL         string   `json:"base_url"`
	APIKeyEncrypted string   `json:"api_key_encrypted,omitempty"`
	APIKeyMasked    string   `json:"api_key_masked,omitempty"`
	Providers       []string `json:"providers"`
}

// UserUsageHubRecord 数据库中的单用户自助 Hub 记录。
type UserUsageHubRecord struct {
	UserID             int64
	UserRole           string
	UserStatus         string
	Enabled            bool
	TokenHash          string
	TokenMasked        string
	IncludeSub2API     bool
	IncludeDisabled    bool
	ExposeResetCredits bool
	MagpieSources      []UsageHubMagpieSourceRecord
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// UsageHubPrincipal 描述通过 /v0/management/* 鉴权后的调用主体（全站管理员旧 Key 或用户自助 Key）。
type UsageHubPrincipal struct {
	Kind               string // "global" 或 "user"
	UserID             int64
	IsAdmin            bool
	IncludeSub2API     bool
	IncludeDisabled    bool
	ExposeResetCredits bool
	MagpieSources      []usageHubResolvedMagpieSource
}

type usageHubResolvedMagpieSource struct {
	ID        string
	Name      string
	BaseURL   string
	APIKey    string
	Providers []string
}

// UsageHubMagpieSourceView 返回给前端的外部 Magpie 源视图（仅包含脱敏密钥摘要）。
type UsageHubMagpieSourceView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Enabled      bool     `json:"enabled"`
	BaseURL      string   `json:"base_url"`
	HasAPIKey    bool     `json:"has_api_key"`
	MaskedAPIKey string   `json:"masked_api_key"`
	Providers    []string `json:"providers"`
}

// UsageHubMagpieSourceInput 用户提交保存的外部 Magpie 源入参。
type UsageHubMagpieSourceInput struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Enabled   bool     `json:"enabled"`
	BaseURL   string   `json:"base_url"`
	APIKey    string   `json:"api_key"` // 编辑已有源时留空表示保留原密钥
	Providers []string `json:"providers"`
}

// UserUsageHubConfig 用户自助 Hub 页面读取的完整配置视图。
type UserUsageHubConfig struct {
	SelfServiceEnabled    bool                       `json:"self_service_enabled"`
	AllowMagpie           bool                       `json:"allow_magpie"`
	AllowPrivateMagpieURL bool                       `json:"allow_private_magpie_url"`
	IsAdmin               bool                       `json:"is_admin"`
	Enabled               bool                       `json:"enabled"`
	HasKey                bool                       `json:"has_key"`
	MaskedKey             string                     `json:"masked_key"`
	GeneratedKey          string                     `json:"generated_key,omitempty"`
	IncludeSub2API        bool                       `json:"include_sub2api"`
	IncludeDisabled       bool                       `json:"include_disabled"`
	ExposeResetCredits    bool                       `json:"expose_reset_credits"`
	HubPath               string                     `json:"hub_path"`
	AuthFilesPath         string                     `json:"auth_files_path"`
	APICallPath           string                     `json:"api_call_path"`
	SupportedPlatforms    []string                   `json:"supported_platforms"`
	MagpiePlatforms       []string                   `json:"magpie_platforms"`
	MaxMagpieSources      int                        `json:"max_magpie_sources"`
	CacheTTLSeconds       int                        `json:"cache_ttl_seconds"`
	MagpieSources         []UsageHubMagpieSourceView `json:"magpie_sources"`
}

// UpdateUserUsageHubConfigInput 用户更新个人自助 Hub 配置的入参。
type UpdateUserUsageHubConfigInput struct {
	Enabled            *bool                       `json:"enabled"`
	IncludeSub2API     *bool                       `json:"include_sub2api"`
	IncludeDisabled    *bool                       `json:"include_disabled"`
	ExposeResetCredits *bool                       `json:"expose_reset_credits"`
	MagpieSources      *[]UsageHubMagpieSourceInput `json:"magpie_sources"`
}

// TestMagpieSourceInput 用户测试外部 Magpie 源连通性的入参。
type TestMagpieSourceInput struct {
	SourceID  string   `json:"source_id"`
	BaseURL   string   `json:"base_url"`
	APIKey    string   `json:"api_key"`
	Providers []string `json:"providers"`
}

// TestMagpieSourceResult 测试外部 Magpie 源的结果。
type TestMagpieSourceResult struct {
	OK             bool           `json:"ok"`
	NormalizedURL  string         `json:"normalized_url"`
	TotalRawQuotas int            `json:"total_raw_quotas"`
	MatchedCount   int            `json:"matched_count"`
	ProviderCounts map[string]int `json:"provider_counts"`
	SampleLabels   []string       `json:"sample_labels"`
	Error          string         `json:"error,omitempty"`
}

// UsageHubPreviewWarning 预览合并 auth-files 时的非致命警告（例如某个 Magpie 源暂时不可达）。
type UsageHubPreviewWarning struct {
	SourceID   string `json:"source_id"`
	SourceName string `json:"source_name"`
	Message    string `json:"message"`
}

// UsageHubPreviewResponse 用户自助页预览当前合并后的 CPA auth-files 响应。
type UsageHubPreviewResponse struct {
	Files    []UsageHubAuthFile       `json:"files"`
	Warnings []UsageHubPreviewWarning `json:"warnings,omitempty"`
}

// magpieQuotaEntry 对应 Magpie GET /v1/magpie/quotas?kept=1 返回的单条额度卡片。
type magpieQuotaEntry struct {
	Provider string             `json:"provider"`
	Name     string             `json:"name"`
	Kind     string             `json:"kind"`
	Plan     string             `json:"plan"`
	User     string             `json:"user"`
	Alias    string             `json:"alias"`
	From     string             `json:"from"`
	Error    string             `json:"error"`
	AsOf     *time.Time         `json:"asOf"`
	ReadAt   *time.Time         `json:"readAt"`
	Until    *time.Time         `json:"until"`
	Windows  []magpieQuotaSpan  `json:"windows"`
	Balance  string             `json:"balance"`
	Resets   *magpieResetCredit `json:"resets"`
}

type magpieQuotaSpan struct {
	Unlimited bool       `json:"unlimited"`
	Name      string     `json:"name"`
	Used      float64    `json:"used"`
	Remaining float64    `json:"remaining"`
	ResetsAt  *time.Time `json:"resetsAt"`
	Display   string     `json:"display"`
	Text      bool       `json:"text"`
	Lane      string     `json:"lane"`
	Pool      string     `json:"pool"`
	Family    string     `json:"family"`
}

type magpieResetCredit struct {
	Available int `json:"available"`
	Count     int `json:"count"`
}

type usageHubMagpieCacheEntry struct {
	entries   []magpieQuotaEntry
	timestamp time.Time
}

type magpieMatchedAccount struct {
	Source      usageHubResolvedMagpieSource
	CPAProvider string
	AccountKey  string
	Entry       magpieQuotaEntry
	AuthFile    UsageHubAuthFile
}

// GetUserConfig 获取当前用户的自助用量 Hub 配置。
func (s *UsageHubService) GetUserConfig(ctx context.Context, userID int64, isAdmin bool) (*UserUsageHubConfig, error) {
	if s == nil || s.userHubRepo == nil {
		return nil, infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "user usage hub service is not configured")
	}
	if userID <= 0 {
		return nil, infraerrors.Unauthorized("UNAUTHORIZED", "invalid user")
	}

	policy := s.loadHubPolicy(ctx)
	record, err := s.userHubRepo.GetByUserID(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrUsageHubUserConfigNotFound) {
			return nil, fmt.Errorf("load user usage hub config: %w", err)
		}
		record = defaultUserUsageHubRecord(userID, isAdmin)
	}

	return s.buildUserConfigResponse(record, policy, isAdmin, ""), nil
}

// UpdateUserConfig 保存当前用户的自助用量 Hub 开关、暴露项和自定义 Magpie 数据源。
func (s *UsageHubService) UpdateUserConfig(ctx context.Context, userID int64, isAdmin bool, input UpdateUserUsageHubConfigInput) (*UserUsageHubConfig, error) {
	if s == nil || s.userHubRepo == nil {
		return nil, infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "user usage hub service is not configured")
	}
	if userID <= 0 {
		return nil, infraerrors.Unauthorized("UNAUTHORIZED", "invalid user")
	}

	policy := s.loadHubPolicy(ctx)
	if !isAdmin && !policy.SelfServiceEnabled {
		return nil, ErrUsageHubSelfServiceDisabled
	}

	record, err := s.userHubRepo.GetByUserID(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrUsageHubUserConfigNotFound) {
			return nil, fmt.Errorf("load user usage hub config: %w", err)
		}
		record = defaultUserUsageHubRecord(userID, isAdmin)
	}

	if input.Enabled != nil {
		record.Enabled = *input.Enabled
	}
	if input.IncludeSub2API != nil {
		record.IncludeSub2API = *input.IncludeSub2API
	}
	if input.IncludeDisabled != nil {
		// 仅管理员允许开启查看已禁用账号，普通用户固定为 false。
		if isAdmin {
			record.IncludeDisabled = *input.IncludeDisabled
		} else {
			record.IncludeDisabled = false
		}
	}
	if input.ExposeResetCredits != nil {
		// 仅管理员允许在个人 Hub 中开启重置卡查询端点；普通用户始终隐藏以防误触。
		if isAdmin {
			record.ExposeResetCredits = *input.ExposeResetCredits
		} else {
			record.ExposeResetCredits = false
		}
	}

	if input.MagpieSources != nil {
		if !isAdmin && !policy.AllowMagpie && len(*input.MagpieSources) > 0 {
			return nil, ErrUsageHubMagpieDisabled
		}
		nextSources, parseErr := s.normalizeAndEncryptMagpieSources(*input.MagpieSources, record.MagpieSources, isAdmin)
		if parseErr != nil {
			return nil, parseErr
		}
		record.MagpieSources = nextSources
	}

	var generatedKey string
	if record.Enabled && strings.TrimSpace(record.TokenHash) == "" {
		rawKey, genErr := generateUsageHubUserKey()
		if genErr != nil {
			return nil, fmt.Errorf("generate user usage hub key: %w", genErr)
		}
		record.TokenHash = hashUsageHubToken(rawKey)
		record.TokenMasked = maskUsageHubKey(rawKey)
		generatedKey = rawKey
	}

	if err := s.userHubRepo.Upsert(ctx, record); err != nil {
		return nil, err
	}
	s.invalidateUserMagpieCache(userID)

	return s.buildUserConfigResponse(record, policy, isAdmin, generatedKey), nil
}

// RegenerateUserKey 为当前用户生成或轮换个人自助 Hub 密钥（明文仅返回一次）。
func (s *UsageHubService) RegenerateUserKey(ctx context.Context, userID int64, isAdmin bool) (string, string, error) {
	if s == nil || s.userHubRepo == nil {
		return "", "", infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "user usage hub service is not configured")
	}
	if userID <= 0 {
		return "", "", infraerrors.Unauthorized("UNAUTHORIZED", "invalid user")
	}

	policy := s.loadHubPolicy(ctx)
	if !isAdmin && !policy.SelfServiceEnabled {
		return "", "", ErrUsageHubSelfServiceDisabled
	}

	record, err := s.userHubRepo.GetByUserID(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrUsageHubUserConfigNotFound) {
			return "", "", fmt.Errorf("load user usage hub config: %w", err)
		}
		record = defaultUserUsageHubRecord(userID, isAdmin)
		record.Enabled = true
	}

	rawKey, err := generateUsageHubUserKey()
	if err != nil {
		return "", "", fmt.Errorf("generate user usage hub key: %w", err)
	}
	record.TokenHash = hashUsageHubToken(rawKey)
	record.TokenMasked = maskUsageHubKey(rawKey)

	if err := s.userHubRepo.Upsert(ctx, record); err != nil {
		return "", "", err
	}
	return rawKey, record.TokenMasked, nil
}

// DeleteUserKey 吊销当前用户的个人 Hub 密钥并关闭该用户的 Hub 访问。
func (s *UsageHubService) DeleteUserKey(ctx context.Context, userID int64) error {
	if s == nil || s.userHubRepo == nil {
		return infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "user usage hub service is not configured")
	}
	if userID <= 0 {
		return infraerrors.Unauthorized("UNAUTHORIZED", "invalid user")
	}
	s.invalidateUserMagpieCache(userID)
	return s.userHubRepo.DeleteToken(ctx, userID)
}

// TestUserMagpieSource 测试用户填写或已保存的 Magpie 数据源连通性，并返回识别到的 CPA 平台统计。
func (s *UsageHubService) TestUserMagpieSource(ctx context.Context, userID int64, isAdmin bool, input TestMagpieSourceInput) (*TestMagpieSourceResult, error) {
	if s == nil {
		return nil, infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "usage hub service is not configured")
	}
	policy := s.loadHubPolicy(ctx)
	if !isAdmin && !policy.SelfServiceEnabled {
		return nil, ErrUsageHubSelfServiceDisabled
	}
	if !isAdmin && !policy.AllowMagpie {
		return nil, ErrUsageHubMagpieDisabled
	}

	baseURL := strings.TrimSpace(input.BaseURL)
	apiKey := strings.TrimSpace(input.APIKey)
	providers := normalizeMagpieProviders(input.Providers)

	// 若指定了已保存的 source_id 且未传入新密钥/地址，则复用已加密保存的配置。
	if sourceID := strings.TrimSpace(input.SourceID); sourceID != "" && s.userHubRepo != nil && userID > 0 {
		if rec, err := s.userHubRepo.GetByUserID(ctx, userID); err == nil && rec != nil {
			for _, existing := range rec.MagpieSources {
				if existing.ID != sourceID {
					continue
				}
				if baseURL == "" {
					baseURL = existing.BaseURL
				}
				if apiKey == "" && existing.APIKeyEncrypted != "" {
					decrypted, decErr := s.decryptMagpieAPIKey(existing.APIKeyEncrypted)
					if decErr != nil {
						return nil, infraerrors.InternalServer("USAGE_HUB_DECRYPT_FAILED", "failed to decrypt saved magpie api key")
					}
					apiKey = decrypted
				}
				if len(input.Providers) == 0 && len(existing.Providers) > 0 {
					providers = normalizeMagpieProviders(existing.Providers)
				}
				break
			}
		}
	}

	normalizedURL, err := validateMagpieBaseURL(baseURL, isAdmin)
	if err != nil {
		return &TestMagpieSourceResult{
			OK:             false,
			ProviderCounts: map[string]int{},
			SampleLabels:   []string{},
			Error:          err.Error(),
		}, nil
	}

	entries, err := s.fetchMagpieQuotasDirect(ctx, normalizedURL, apiKey, isAdmin)
	if err != nil {
		return &TestMagpieSourceResult{
			OK:             false,
			NormalizedURL:  normalizedURL,
			ProviderCounts: map[string]int{},
			SampleLabels:   []string{},
			Error:          err.Error(),
		}, nil
	}

	src := usageHubResolvedMagpieSource{
		ID:        usageHubFirstNonEmpty(strings.TrimSpace(input.SourceID), "test"),
		Name:      "Magpie",
		BaseURL:   normalizedURL,
		APIKey:    apiKey,
		Providers: providers,
	}
	matched := buildMagpieMatchedAccounts(src, entries)

	counts := map[string]int{}
	samples := make([]string, 0, 6)
	for _, item := range matched {
		counts[item.CPAProvider]++
		if len(samples) < 6 {
			label := item.AuthFile.Label
			if item.AuthFile.PlanType != "" {
				label = fmt.Sprintf("%s (%s)", label, item.AuthFile.PlanType)
			}
			samples = append(samples, fmt.Sprintf("[%s] %s", item.CPAProvider, label))
		}
	}

	return &TestMagpieSourceResult{
		OK:             true,
		NormalizedURL:  normalizedURL,
		TotalRawQuotas: len(entries),
		MatchedCount:   len(matched),
		ProviderCounts: counts,
		SampleLabels:   samples,
	}, nil
}

// PreviewUserAuthFiles 预览当前用户的个人 Hub 合并输出（sub2api + 自定义 Magpie 整形结果）。
func (s *UsageHubService) PreviewUserAuthFiles(ctx context.Context, userID int64, isAdmin bool) (*UsageHubPreviewResponse, error) {
	if s == nil || s.userHubRepo == nil {
		return nil, infraerrors.InternalServer("USAGE_HUB_NOT_CONFIGURED", "user usage hub service is not configured")
	}
	policy := s.loadHubPolicy(ctx)
	if !isAdmin && !policy.SelfServiceEnabled {
		return nil, ErrUsageHubSelfServiceDisabled
	}

	record, err := s.userHubRepo.GetByUserID(ctx, userID)
	if err != nil {
		if !errors.Is(err, ErrUsageHubUserConfigNotFound) {
			return nil, fmt.Errorf("load user usage hub config: %w", err)
		}
		record = defaultUserUsageHubRecord(userID, isAdmin)
	}

	principal := s.buildPrincipalFromUserRecord(record, policy, isAdmin)
	res, warnings, err := s.listAuthFilesWithWarnings(ctx, principal)
	if err != nil {
		return nil, err
	}
	return &UsageHubPreviewResponse{
		Files:    res.Files,
		Warnings: warnings,
	}, nil
}

type usageHubPolicy struct {
	GlobalEnabled         bool
	GlobalIncludeDisabled bool
	GlobalExposeReset     bool
	GlobalManagementKey   string
	SelfServiceEnabled    bool
	AllowMagpie           bool
}

func (s *UsageHubService) loadHubPolicy(ctx context.Context) usageHubPolicy {
	policy := usageHubPolicy{
		SelfServiceEnabled: true,
		AllowMagpie:        true,
	}
	if s == nil || s.settingRepo == nil {
		return policy
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyUsageHubEnabled,
		SettingKeyUsageHubManagementKey,
		SettingKeyUsageHubIncludeDisabled,
		SettingKeyUsageHubSelfServiceEnabled,
		SettingKeyUsageHubAllowMagpie,
		SettingKeyUsageHubExposeResetCredits,
	})
	if err != nil {
		return policy
	}

	policy.GlobalEnabled = strings.EqualFold(strings.TrimSpace(values[SettingKeyUsageHubEnabled]), "true")
	policy.GlobalManagementKey = strings.TrimSpace(values[SettingKeyUsageHubManagementKey])
	policy.GlobalIncludeDisabled = strings.EqualFold(strings.TrimSpace(values[SettingKeyUsageHubIncludeDisabled]), "true")
	policy.GlobalExposeReset = strings.EqualFold(strings.TrimSpace(values[SettingKeyUsageHubExposeResetCredits]), "true")

	if raw := strings.TrimSpace(values[SettingKeyUsageHubSelfServiceEnabled]); raw != "" {
		policy.SelfServiceEnabled = strings.EqualFold(raw, "true")
	}
	if raw := strings.TrimSpace(values[SettingKeyUsageHubAllowMagpie]); raw != "" {
		policy.AllowMagpie = strings.EqualFold(raw, "true")
	}
	return policy
}

func defaultUserUsageHubRecord(userID int64, isAdmin bool) *UserUsageHubRecord {
	role := RoleUser
	if isAdmin {
		role = RoleAdmin
	}
	return &UserUsageHubRecord{
		UserID:             userID,
		UserRole:           role,
		UserStatus:         StatusActive,
		Enabled:            false,
		IncludeSub2API:     true,
		IncludeDisabled:    false,
		ExposeResetCredits: false,
		MagpieSources:      []UsageHubMagpieSourceRecord{},
	}
}

func (s *UsageHubService) buildPrincipalFromUserRecord(record *UserUsageHubRecord, policy usageHubPolicy, isAdmin bool) *UsageHubPrincipal {
	p := &UsageHubPrincipal{
		Kind:               "user",
		UserID:             record.UserID,
		IsAdmin:            isAdmin,
		IncludeSub2API:     record.IncludeSub2API,
		IncludeDisabled:    record.IncludeDisabled && isAdmin,
		ExposeResetCredits: record.ExposeResetCredits && isAdmin,
	}
	if isAdmin || policy.AllowMagpie {
		for _, src := range record.MagpieSources {
			if !src.Enabled || strings.TrimSpace(src.BaseURL) == "" {
				continue
			}
			apiKey := ""
			if src.APIKeyEncrypted != "" {
				decrypted, err := s.decryptMagpieAPIKey(src.APIKeyEncrypted)
				if err != nil {
					continue
				}
				apiKey = decrypted
			}
			p.MagpieSources = append(p.MagpieSources, usageHubResolvedMagpieSource{
				ID:        src.ID,
				Name:      usageHubFirstNonEmpty(src.Name, "Magpie"),
				BaseURL:   src.BaseURL,
				APIKey:    apiKey,
				Providers: normalizeMagpieProviders(src.Providers),
			})
		}
	}
	return p
}

func (s *UsageHubService) buildUserConfigResponse(record *UserUsageHubRecord, policy usageHubPolicy, isAdmin bool, generatedKey string) *UserUsageHubConfig {
	views := make([]UsageHubMagpieSourceView, 0, len(record.MagpieSources))
	for _, src := range record.MagpieSources {
		views = append(views, UsageHubMagpieSourceView{
			ID:           src.ID,
			Name:         src.Name,
			Enabled:      src.Enabled,
			BaseURL:      src.BaseURL,
			HasAPIKey:    strings.TrimSpace(src.APIKeyEncrypted) != "",
			MaskedAPIKey: src.APIKeyMasked,
			Providers:    normalizeMagpieProviders(src.Providers),
		})
	}

	return &UserUsageHubConfig{
		SelfServiceEnabled:    policy.SelfServiceEnabled,
		AllowMagpie:           policy.AllowMagpie || isAdmin,
		AllowPrivateMagpieURL: isAdmin,
		IsAdmin:               isAdmin,
		Enabled:               record.Enabled,
		HasKey:                strings.TrimSpace(record.TokenHash) != "",
		MaskedKey:             record.TokenMasked,
		GeneratedKey:          generatedKey,
		IncludeSub2API:        record.IncludeSub2API,
		IncludeDisabled:       record.IncludeDisabled && isAdmin,
		ExposeResetCredits:    record.ExposeResetCredits && isAdmin,
		HubPath:               "/v0/management",
		AuthFilesPath:         "/v0/management/auth-files",
		APICallPath:           "/v0/management/api-call",
		SupportedPlatforms:    []string{usageHubProviderCodex},
		MagpiePlatforms:       append([]string(nil), usageHubSupportedMagpieProviders...),
		MaxMagpieSources:      usageHubMaxMagpieSources,
		CacheTTLSeconds:       int(s.cacheTTL.Seconds()),
		MagpieSources:         views,
	}
}

func (s *UsageHubService) normalizeAndEncryptMagpieSources(
	inputs []UsageHubMagpieSourceInput,
	existing []UsageHubMagpieSourceRecord,
	isAdmin bool,
) ([]UsageHubMagpieSourceRecord, error) {
	if len(inputs) > usageHubMaxMagpieSources {
		return nil, infraerrors.BadRequest("USAGE_HUB_TOO_MANY_MAGPIE_SOURCES", fmt.Sprintf("at most %d magpie sources are allowed", usageHubMaxMagpieSources))
	}

	existingByID := make(map[string]UsageHubMagpieSourceRecord, len(existing))
	for _, item := range existing {
		existingByID[item.ID] = item
	}

	seenIDs := make(map[string]struct{}, len(inputs))
	out := make([]UsageHubMagpieSourceRecord, 0, len(inputs))

	for i, raw := range inputs {
		normalizedURL, err := validateMagpieBaseURL(raw.BaseURL, isAdmin)
		if err != nil {
			return nil, infraerrors.BadRequest("USAGE_HUB_INVALID_MAGPIE_URL", fmt.Sprintf("magpie source #%d invalid url: %v", i+1, err))
		}

		id := sanitizeMagpieSourceID(raw.ID)
		if id == "" {
			genID, genErr := generateMagpieSourceID()
			if genErr != nil {
				return nil, fmt.Errorf("generate magpie source id: %w", genErr)
			}
			id = genID
		}
		if _, dup := seenIDs[id]; dup {
			genID, genErr := generateMagpieSourceID()
			if genErr != nil {
				return nil, fmt.Errorf("generate magpie source id: %w", genErr)
			}
			id = genID
		}
		seenIDs[id] = struct{}{}

		name := strings.TrimSpace(raw.Name)
		if name == "" {
			if parsed, pErr := url.Parse(normalizedURL); pErr == nil && parsed.Host != "" {
				name = parsed.Host
			} else {
				name = fmt.Sprintf("Magpie #%d", i+1)
			}
		}
		if len([]rune(name)) > 48 {
			name = string([]rune(name)[:48])
		}

		var encryptedKey, maskedKey string
		plainKey := strings.TrimSpace(raw.APIKey)
		if plainKey != "" {
			enc, encErr := s.encryptMagpieAPIKey(plainKey)
			if encErr != nil {
				return nil, infraerrors.InternalServer("USAGE_HUB_ENCRYPT_FAILED", "failed to encrypt magpie api key")
			}
			encryptedKey = enc
			maskedKey = maskUsageHubKey(plainKey)
		} else if prev, ok := existingByID[id]; ok {
			encryptedKey = prev.APIKeyEncrypted
			maskedKey = prev.APIKeyMasked
		}

		out = append(out, UsageHubMagpieSourceRecord{
			ID:              id,
			Name:            name,
			Enabled:         raw.Enabled,
			BaseURL:         normalizedURL,
			APIKeyEncrypted: encryptedKey,
			APIKeyMasked:    maskedKey,
			Providers:       normalizeMagpieProviders(raw.Providers),
		})
	}

	return out, nil
}

func (s *UsageHubService) encryptMagpieAPIKey(plaintext string) (string, error) {
	if s.encryptor != nil {
		return s.encryptor.Encrypt(plaintext)
	}
	return plaintext, nil
}

func (s *UsageHubService) decryptMagpieAPIKey(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if s.encryptor != nil {
		return s.encryptor.Decrypt(ciphertext)
	}
	return ciphertext, nil
}

// validateMagpieBaseURL 校验并标准化外部 Magpie 网关地址。
// 普通用户仅允许公网 HTTPS，防止 SSRF 探测内网服务；管理员允许内网 HTTP（如 http://magpie:3425）。
func validateMagpieBaseURL(raw string, allowPrivate bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("magpie base_url is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("invalid magpie url")
	}
	if parsed.User != nil || parsed.Opaque != "" {
		return "", errors.New("userinfo is not allowed in magpie url")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		parsed.RawQuery = ""
		parsed.Fragment = ""
	}
	// 如果用户直接粘贴了完整的 /v1/magpie/quotas 路径，自动截回网关根路径。
	cleanPath := strings.TrimRight(parsed.Path, "/")
	for _, suffix := range []string{"/v1/magpie/quotas/cards", "/v1/magpie/quotas", "/v1/magpie/accounts"} {
		if strings.HasSuffix(strings.ToLower(cleanPath), suffix) {
			cleanPath = cleanPath[:len(cleanPath)-len(suffix)]
			break
		}
	}
	parsed.Path = cleanPath

	normalized, err := urlvalidator.ValidateHTTPURL(parsed.String(), allowPrivate, urlvalidator.ValidationOptions{
		AllowPrivate: allowPrivate,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimRight(normalized, "/"), nil
}

func (s *UsageHubService) getMagpieHTTPClient(allowPrivate bool) (*http.Client, error) {
	if s.magpieClientFactory != nil {
		return s.magpieClientFactory(allowPrivate)
	}
	base, err := httpclient.GetClient(httpclient.Options{
		Timeout:            usageHubMagpieTimeout,
		ValidateResolvedIP: !allowPrivate,
		AllowPrivateHosts:  allowPrivate,
	})
	if err != nil {
		return nil, err
	}
	cloned := *base
	// 禁止自动跟随重定向，避免通过 302 跳转绕过 SSRF 校验。
	cloned.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &cloned, nil
}

func (s *UsageHubService) fetchMagpieQuotasCached(
	ctx context.Context,
	userID int64,
	src usageHubResolvedMagpieSource,
	allowPrivate bool,
) ([]magpieQuotaEntry, error) {
	keyHash := hashUsageHubToken(src.APIKey)
	cacheKey := fmt.Sprintf("u:%d|s:%s|url:%s|k:%s", userID, src.ID, src.BaseURL, keyHash[:16])

	if cached := s.getCachedMagpieQuotas(cacheKey); cached != nil {
		return cached, nil
	}

	result, err, _ := s.magpieSF.Do(cacheKey, func() (any, error) {
		if cached := s.getCachedMagpieQuotas(cacheKey); cached != nil {
			return cached, nil
		}
		entries, fetchErr := s.fetchMagpieQuotasDirect(ctx, src.BaseURL, src.APIKey, allowPrivate)
		if fetchErr != nil {
			return nil, fetchErr
		}
		s.magpieCache.Store(cacheKey, &usageHubMagpieCacheEntry{
			entries:   entries,
			timestamp: time.Now(),
		})
		return entries, nil
	})
	if err != nil {
		return nil, err
	}
	entries, ok := result.([]magpieQuotaEntry)
	if !ok {
		return nil, errors.New("unexpected magpie cache payload")
	}
	return entries, nil
}

func (s *UsageHubService) fetchMagpieQuotasDirect(
	ctx context.Context,
	baseURL string,
	apiKey string,
	allowPrivate bool,
) ([]magpieQuotaEntry, error) {
	client, err := s.getMagpieHTTPClient(allowPrivate)
	if err != nil {
		return nil, fmt.Errorf("init magpie http client: %w", err)
	}

	root := strings.TrimRight(baseURL, "/")
	// 优先读取 Magpie 缓存快照（?kept=1，零上游开销）；若刚启动缓存为空则回退拉取一次实时快照。
	entries, err := fetchMagpieQuotasEndpoint(ctx, client, root+"/v1/magpie/quotas?kept=1", apiKey)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		if fresh, freshErr := fetchMagpieQuotasEndpoint(ctx, client, root+"/v1/magpie/quotas", apiKey); freshErr == nil && len(fresh) > 0 {
			return fresh, nil
		}
	}
	return entries, nil
}

func fetchMagpieQuotasEndpoint(
	ctx context.Context,
	client *http.Client,
	endpoint string,
	apiKey string,
) ([]magpieQuotaEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build magpie request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Sub2API-UsageHub")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("magpie request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, fmt.Errorf("magpie endpoint returned redirect HTTP %d (redirects are disabled)", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, usageHubMagpieMaxBodyBytes+1)
	rawBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read magpie response: %w", err)
	}
	if len(rawBody) > usageHubMagpieMaxBodyBytes {
		return nil, errors.New("magpie response exceeds 2 MiB limit")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := extractMagpieErrorMessage(rawBody)
		if msg != "" {
			return nil, fmt.Errorf("magpie returned HTTP %d: %s", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("magpie returned HTTP %d", resp.StatusCode)
	}

	entries, err := decodeMagpieQuotasPayload(rawBody)
	if err != nil {
		return nil, fmt.Errorf("decode magpie quotas json: %w", err)
	}
	return entries, nil
}

// decodeMagpieQuotasPayload 兼容解析 Magpie HTTP 网关返回的 {"object":"list","data":[...]} 包装结构以及裸数组 [...] 格式。
func decodeMagpieQuotasPayload(rawBody []byte) ([]magpieQuotaEntry, error) {
	trimmed := strings.TrimSpace(string(rawBody))
	if trimmed == "" || trimmed == "null" {
		return []magpieQuotaEntry{}, nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var entries []magpieQuotaEntry
		if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
			return nil, err
		}
		if entries == nil {
			return []magpieQuotaEntry{}, nil
		}
		return entries, nil
	}

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Quotas json.RawMessage `json:"quotas"`
		Items  json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return nil, err
	}

	listRaw := envelope.Data
	if len(listRaw) == 0 {
		listRaw = envelope.Quotas
	}
	if len(listRaw) == 0 {
		listRaw = envelope.Items
	}
	if len(listRaw) == 0 {
		return nil, errors.New("missing list field (data/quotas/items) in magpie response")
	}

	listTrimmed := strings.TrimSpace(string(listRaw))
	if listTrimmed == "" || listTrimmed == "null" {
		return []magpieQuotaEntry{}, nil
	}

	var entries []magpieQuotaEntry
	if err := json.Unmarshal(listRaw, &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		return []magpieQuotaEntry{}, nil
	}
	return entries, nil
}

func (s *UsageHubService) getCachedMagpieQuotas(cacheKey string) []magpieQuotaEntry {
	ttl := s.cacheTTL
	if ttl <= 0 {
		return nil
	}
	raw, ok := s.magpieCache.Load(cacheKey)
	if !ok {
		return nil
	}
	entry, ok := raw.(*usageHubMagpieCacheEntry)
	if !ok || entry == nil {
		return nil
	}
	if time.Since(entry.timestamp) >= ttl {
		return nil
	}
	return entry.entries
}

func (s *UsageHubService) invalidateUserMagpieCache(userID int64) {
	prefix := fmt.Sprintf("u:%d|", userID)
	s.magpieCache.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
			s.magpieCache.Delete(k)
		}
		return true
	})
}

func buildMagpieMatchedAccounts(src usageHubResolvedMagpieSource, entries []magpieQuotaEntry) []magpieMatchedAccount {
	allowedProviders := make(map[string]struct{}, len(src.Providers))
	for _, p := range normalizeMagpieProviders(src.Providers) {
		allowedProviders[p] = struct{}{}
	}

	occurrence := make(map[string]int, len(entries))
	matched := make([]magpieMatchedAccount, 0, len(entries))

	for _, entry := range entries {
		cpaProvider := normalizeMagpieProvider(entry.Provider)
		if cpaProvider == "" {
			continue
		}
		if _, ok := allowedProviders[cpaProvider]; !ok {
			continue
		}
		// 仅保留订阅/套餐额度窗口类卡片，过滤纯余额钱包且无任何窗口的记录。
		if strings.EqualFold(strings.TrimSpace(entry.Kind), "balance") && len(filterMagpieMetricWindows(entry.Windows)) == 0 {
			continue
		}

		baseSig := strings.Join([]string{
			cpaProvider,
			strings.ToLower(strings.TrimSpace(entry.User)),
			strings.ToLower(strings.TrimSpace(entry.Alias)),
			strings.ToLower(strings.TrimSpace(entry.Name)),
			strings.ToLower(strings.TrimSpace(entry.From)),
		}, "|")
		occ := occurrence[baseSig]
		occurrence[baseSig] = occ + 1

		keyDigest := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", baseSig, occ)))
		accountKey := hex.EncodeToString(keyDigest[:8])
		authIndex := formatMagpieAuthIndex(src.ID, cpaProvider, accountKey)

		displayIdentity := usageHubFirstNonEmpty(entry.Alias, entry.User, entry.Name, fmt.Sprintf("%s-%s", cpaProvider, accountKey[:6]))
		email := usageHubFirstNonEmpty(entry.User, entry.Alias, entry.Name)
		label := fmt.Sprintf("[%s] %s", usageHubFirstNonEmpty(src.Name, "Magpie"), displayIdentity)

		fileName := buildMagpieAuthFileName(src.Name, cpaProvider, displayIdentity, accountKey[:6])
		status := "active"
		statusMsg := strings.TrimSpace(entry.Error)
		unavailable := false
		if statusMsg != "" && len(filterMagpieMetricWindows(entry.Windows)) == 0 {
			status = "error"
			unavailable = true
		}

		var updatedAt string
		switch {
		case entry.ReadAt != nil && !entry.ReadAt.IsZero():
			updatedAt = entry.ReadAt.UTC().Format(time.RFC3339)
		case entry.AsOf != nil && !entry.AsOf.IsZero():
			updatedAt = entry.AsOf.UTC().Format(time.RFC3339)
		}

		af := UsageHubAuthFile{
			ID:            fileName,
			AuthIndex:     authIndex,
			Name:          fileName,
			Type:          cpaProvider,
			Provider:      cpaProvider,
			Label:         label,
			Status:        status,
			StatusMessage: statusMsg,
			Disabled:      false,
			Unavailable:   unavailable,
			RuntimeOnly:   false,
			Source:        "magpie",
			Email:         email,
			Account:       displayIdentity,
			AccountType:   "oauth",
			PlanType:      strings.TrimSpace(entry.Plan),
			UpdatedAt:     updatedAt,
			Modtime:       updatedAt,
		}

		switch cpaProvider {
		case usageHubProviderCodex:
			var until string
			if entry.Until != nil && !entry.Until.IsZero() {
				until = entry.Until.UTC().Format(time.RFC3339)
			}
			af.IDToken = &UsageHubIDTokenClaims{
				PlanType:       strings.TrimSpace(entry.Plan),
				Email:          email,
				SubActiveUntil: until,
			}
		case usageHubProviderAntigravity:
			// T3 Code 的 Antigravity Hub 解析器要求 project_id 非空，否则会直接报错跳过。
			af.ProjectID = fmt.Sprintf("magpie-%s-%s", sanitizeMagpieSourceID(src.ID), accountKey[:8])
		}

		matched = append(matched, magpieMatchedAccount{
			Source:      src,
			CPAProvider: cpaProvider,
			AccountKey:  accountKey,
			Entry:       entry,
			AuthFile:    af,
		})
	}

	return matched
}

// executeMagpieAPICall 将 T3 Code 发给 magpie:* 虚拟账号的 /v0/management/api-call 请求，
// 实时合成为对应平台的标准上游 JSON 响应（Codex / Claude / Antigravity），全程不触碰任何 Magpie 上游 Token。
func (s *UsageHubService) executeMagpieAPICall(
	ctx context.Context,
	principal *UsageHubPrincipal,
	authIndex string,
	req UsageHubAPICallRequest,
) (*UsageHubAPICallResponse, error) {
	if principal == nil {
		return nil, ErrUsageHubUnauthorized
	}
	sourceID, cpaProvider, accountKey, ok := parseMagpieAuthIndex(authIndex)
	if !ok {
		return nil, infraerrors.BadRequest("USAGE_HUB_INVALID_AUTH_INDEX", "invalid magpie auth_index")
	}

	var targetSource *usageHubResolvedMagpieSource
	for i := range principal.MagpieSources {
		if principal.MagpieSources[i].ID == sourceID {
			targetSource = &principal.MagpieSources[i]
			break
		}
	}
	if targetSource == nil {
		return nil, ErrUsageHubAccountNotFound
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	parsedURL, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || parsedURL == nil || !strings.EqualFold(parsedURL.Scheme, "https") {
		return nil, ErrUsageHubForbiddenURL
	}
	host := strings.ToLower(strings.TrimSpace(parsedURL.Hostname()))
	path := parsedURL.Path

	// 先校验允许的虚拟上游端点，拦截一切非白名单路径与写操作。
	if !isAllowedMagpieVirtualEndpoint(cpaProvider, method, host, path) {
		return nil, ErrUsageHubForbiddenURL
	}

	// Antigravity 的 googleapis userinfo 端点主动返回 404，让 T3 Code 保留 auth-files 中的邮箱标识显示。
	if cpaProvider == usageHubProviderAntigravity && host == "www.googleapis.com" && path == googleUserInfoPath {
		return &UsageHubAPICallResponse{
			StatusCode: http.StatusNotFound,
			Header:     map[string][]string{"Content-Type": {"application/json"}},
			Body:       `{"error":"userinfo_not_exposed_for_magpie"}`,
		}, nil
	}

	entries, err := s.fetchMagpieQuotasCached(ctx, principal.UserID, *targetSource, principal.IsAdmin)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "USAGE_HUB_MAGPIE_FETCH_FAILED", "failed to query magpie quotas: %v", err)
	}

	matched := buildMagpieMatchedAccounts(*targetSource, entries)
	var account *magpieMatchedAccount
	for i := range matched {
		if matched[i].CPAProvider == cpaProvider && matched[i].AccountKey == accountKey {
			account = &matched[i]
			break
		}
	}
	if account == nil {
		return nil, ErrUsageHubAccountNotFound
	}

	if errMsg := strings.TrimSpace(account.Entry.Error); errMsg != "" && len(filterMagpieMetricWindows(account.Entry.Windows)) == 0 {
		errPayload, _ := json.Marshal(map[string]string{"error": errMsg})
		return &UsageHubAPICallResponse{
			StatusCode: http.StatusBadGateway,
			Header:     map[string][]string{"Content-Type": {"application/json"}},
			Body:       string(errPayload),
		}, nil
	}

	var bodyObj any
	switch cpaProvider {
	case usageHubProviderCodex:
		bodyObj = synthesizeCodexUsageFromMagpie(account.Entry)
	case usageHubProviderClaude:
		bodyObj = synthesizeClaudeUsageFromMagpie(account.Entry)
	case usageHubProviderAntigravity:
		bodyObj = synthesizeAntigravitySummaryFromMagpie(account.Entry)
	default:
		return nil, ErrUsageHubForbiddenURL
	}

	rawBody, err := json.Marshal(bodyObj)
	if err != nil {
		return nil, fmt.Errorf("marshal synthesized magpie usage: %w", err)
	}

	return &UsageHubAPICallResponse{
		StatusCode: http.StatusOK,
		Header:     map[string][]string{"Content-Type": {"application/json"}},
		Body:       string(rawBody),
	}, nil
}

func isAllowedMagpieVirtualEndpoint(cpaProvider, method, host, path string) bool {
	switch cpaProvider {
	case usageHubProviderCodex:
		// 对 Magpie 的 Codex 账号只放行 /wham/usage，不放行 rate-limit-reset-credits（因此 T3 Code 不会露出重置卡按钮）。
		return method == http.MethodGet && host == "chatgpt.com" && path == "/backend-api/wham/usage"
	case usageHubProviderClaude:
		return method == http.MethodGet && host == "api.anthropic.com" && path == anthropicOAuthUsagePath
	case usageHubProviderAntigravity:
		if method == http.MethodPost && (host == "daily-cloudcode-pa.googleapis.com" || host == "cloudcode-pa.googleapis.com") && path == antigravitySummaryPath {
			return true
		}
		if method == http.MethodGet && host == "www.googleapis.com" && path == googleUserInfoPath {
			return true
		}
		return false
	default:
		return false
	}
}

// synthesizeCodexUsageFromMagpie 将 Magpie 的 Codex 卡片映射为 OpenAI /backend-api/wham/usage 结构。
func synthesizeCodexUsageFromMagpie(entry magpieQuotaEntry) map[string]any {
	windows := filterMagpieMetricWindows(entry.Windows)

	var primary, secondary *magpieQuotaSpan
	var unassigned []*magpieQuotaSpan

	for i := range windows {
		w := &windows[i]
		kind := classifyMagpieWindowKind(w.Name)
		switch {
		case kind == "session" && primary == nil:
			primary = w
		case kind == "weekly" && secondary == nil:
			secondary = w
		default:
			unassigned = append(unassigned, w)
		}
	}
	for _, w := range unassigned {
		if primary == nil {
			primary = w
		} else if secondary == nil {
			secondary = w
		}
	}

	now := time.Now().UTC()
	limitReached := false
	rateLimit := map[string]any{
		"allowed":       true,
		"limit_reached": false,
	}
	if primary != nil {
		pw := buildCodexWindowPayload(*primary, 5*3600, now)
		rateLimit["primary_window"] = pw
		if normalizeMagpieUsedPercent(*primary) >= 100 {
			limitReached = true
		}
	}
	if secondary != nil {
		sw := buildCodexWindowPayload(*secondary, 7*24*3600, now)
		rateLimit["secondary_window"] = sw
		if normalizeMagpieUsedPercent(*secondary) >= 100 {
			limitReached = true
		}
	}
	if limitReached {
		rateLimit["allowed"] = false
		rateLimit["limit_reached"] = true
	}

	payload := map[string]any{
		"plan_type":  normalizeCodexPlanType(entry.Plan),
		"rate_limit": rateLimit,
	}
	if email := strings.TrimSpace(entry.User); email != "" {
		payload["email"] = email
	}
	return payload
}

func buildCodexWindowPayload(w magpieQuotaSpan, defaultDurationSec int64, now time.Time) map[string]any {
	used := normalizeMagpieUsedPercent(w)
	out := map[string]any{
		"used_percent":         used,
		"limit_window_seconds": defaultDurationSec,
	}
	if w.ResetsAt != nil && !w.ResetsAt.IsZero() {
		resetUnix := w.ResetsAt.UTC().Unix()
		out["reset_at"] = resetUnix
		after := resetUnix - now.Unix()
		if after < 0 {
			after = 0
		}
		out["reset_after_seconds"] = after
	}
	return out
}

// synthesizeClaudeUsageFromMagpie 将 Magpie 的 Claude 卡片映射为 Anthropic /api/oauth/usage 结构。
func synthesizeClaudeUsageFromMagpie(entry magpieQuotaEntry) map[string]any {
	windows := filterMagpieMetricWindows(entry.Windows)

	var fiveHour, sevenDay *magpieQuotaSpan
	var scoped []map[string]any
	var unassigned []*magpieQuotaSpan

	for i := range windows {
		w := &windows[i]
		modelLabel := extractClaudeScopedModelName(w.Name)
		if modelLabel != "" {
			item := map[string]any{
				"kind":    "weekly_scoped",
				"percent": normalizeMagpieUsedPercent(*w),
				"scope": map[string]any{
					"model": map[string]any{
						"display_name": modelLabel,
					},
				},
			}
			if w.ResetsAt != nil && !w.ResetsAt.IsZero() {
				item["resets_at"] = w.ResetsAt.UTC().Format(time.RFC3339)
			} else {
				item["resets_at"] = nil
			}
			scoped = append(scoped, item)
			continue
		}

		kind := classifyMagpieWindowKind(w.Name)
		switch {
		case kind == "session" && fiveHour == nil:
			fiveHour = w
		case kind == "weekly" && sevenDay == nil:
			sevenDay = w
		default:
			unassigned = append(unassigned, w)
		}
	}
	for _, w := range unassigned {
		if fiveHour == nil {
			fiveHour = w
		} else if sevenDay == nil {
			sevenDay = w
		}
	}

	payload := map[string]any{
		"extra_usage": nil,
	}
	if fiveHour != nil {
		payload["five_hour"] = buildClaudeWindowPayload(*fiveHour)
	}
	if sevenDay != nil {
		payload["seven_day"] = buildClaudeWindowPayload(*sevenDay)
	}
	if len(scoped) > 0 {
		payload["limits"] = scoped
	}
	return payload
}

func buildClaudeWindowPayload(w magpieQuotaSpan) map[string]any {
	var resetsAt any
	if w.ResetsAt != nil && !w.ResetsAt.IsZero() {
		resetsAt = w.ResetsAt.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"utilization": normalizeMagpieUsedPercent(w),
		"resets_at":   resetsAt,
	}
}

// synthesizeAntigravitySummaryFromMagpie 将 Magpie 的 Antigravity 卡片映射为
// Google Cloud Code retrieveUserQuotaSummary 结构（与 T3 Code antigravityUsageLimits.ts 对齐）。
func synthesizeAntigravitySummaryFromMagpie(entry magpieQuotaEntry) map[string]any {
	windows := filterMagpieMetricWindows(entry.Windows)

	type agyGroup struct {
		DisplayName string           `json:"displayName,omitempty"`
		Buckets     []map[string]any `json:"buckets"`
	}

	groupOrder := make([]string, 0, 4)
	groupMap := make(map[string]*agyGroup)
	usedBucketIDs := make(map[string]int, len(windows))

	for i, w := range windows {
		familyKey, groupDisplay, bucketPrefix := classifyAntigravityFamily(w)
		windowKind := classifyMagpieWindowKind(w.Name)

		var winToken, suffix string
		switch windowKind {
		case "session":
			winToken = "5h"
			suffix = "5h"
		case "weekly":
			winToken = "weekly"
			suffix = "weekly"
		default:
			suffix = sanitizeMagpieSourceID(w.Name)
			if suffix == "" {
				suffix = fmt.Sprintf("w%d", i+1)
			}
		}

		bucketID := bucketPrefix + suffix
		if count := usedBucketIDs[bucketID]; count > 0 {
			bucketID = fmt.Sprintf("%s-%d", bucketID, count+1)
		}
		usedBucketIDs[bucketPrefix+suffix]++

		used := normalizeMagpieUsedPercent(w)
		remainingFraction := math.Max(0, math.Min(1, 1.0-(used/100.0)))

		bucket := map[string]any{
			"bucketId":          bucketID,
			"remainingFraction": math.Round(remainingFraction*10000) / 10000,
		}
		if winToken != "" {
			bucket["window"] = winToken
		} else if name := strings.TrimSpace(w.Name); name != "" {
			bucket["displayName"] = name
		}
		if w.ResetsAt != nil && !w.ResetsAt.IsZero() {
			bucket["resetTime"] = w.ResetsAt.UTC().Format(time.RFC3339)
		}

		grp, exists := groupMap[familyKey]
		if !exists {
			grp = &agyGroup{DisplayName: groupDisplay}
			groupMap[familyKey] = grp
			groupOrder = append(groupOrder, familyKey)
		}
		grp.Buckets = append(grp.Buckets, bucket)
	}

	groups := make([]agyGroup, 0, len(groupOrder))
	for _, key := range groupOrder {
		groups = append(groups, *groupMap[key])
	}
	return map[string]any{
		"groups": groups,
	}
}

func classifyAntigravityFamily(w magpieQuotaSpan) (familyKey, displayName, bucketPrefix string) {
	combined := strings.ToLower(strings.TrimSpace(w.Pool + " " + w.Family + " " + w.Name))
	switch {
	case strings.Contains(combined, "argon"):
		return "gemini-argon", "Gemini Argon", "gemini-argon-"
	case strings.Contains(combined, "carbon"):
		return "gemini-carbon", "Gemini Carbon", "gemini-carbon-"
	case strings.Contains(combined, "claude") || strings.Contains(combined, "gpt") || strings.Contains(combined, "3p"):
		return "3p", "Claude & GPT", "3p-"
	case strings.Contains(combined, "gemini"):
		return "gemini", "Gemini", "gemini-"
	default:
		label := usageHubFirstNonEmpty(w.Pool, w.Family, w.Name, "Antigravity")
		slug := sanitizeMagpieSourceID(label)
		if slug == "" {
			slug = "quota"
		}
		return slug, label, slug + "-"
	}
}

func filterMagpieMetricWindows(windows []magpieQuotaSpan) []magpieQuotaSpan {
	out := make([]magpieQuotaSpan, 0, len(windows))
	for _, w := range windows {
		if w.Text || strings.TrimSpace(w.Lane) != "" {
			continue
		}
		out = append(out, w)
	}
	return out
}

func normalizeMagpieUsedPercent(w magpieQuotaSpan) float64 {
	if w.Unlimited {
		return 0
	}
	used := w.Used
	if used == 0 && w.Remaining > 0 && w.Remaining < 100 {
		used = 100 - w.Remaining
	}
	if math.IsNaN(used) || math.IsInf(used, 0) {
		return 0
	}
	if used < 0 {
		return 0
	}
	if used > 100 {
		return 100
	}
	return math.Round(used*100) / 100
}

func classifyMagpieWindowKind(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(lower, "5h") ||
		strings.Contains(lower, "5-hour") ||
		strings.Contains(lower, "5 hour") ||
		strings.Contains(lower, "5 hours") ||
		strings.Contains(lower, "session") ||
		strings.Contains(lower, "primary"):
		return "session"
	case strings.Contains(lower, "7d") ||
		strings.Contains(lower, "7-day") ||
		strings.Contains(lower, "7 day") ||
		strings.Contains(lower, "7 days") ||
		strings.Contains(lower, "week") ||
		strings.Contains(lower, "secondary"):
		return "weekly"
	default:
		return "other"
	}
}

func extractClaudeScopedModelName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return ""
	}
	for _, pair := range []struct {
		token string
		label string
	}{
		{"opus", "Opus"},
		{"sonnet", "Sonnet"},
		{"haiku", "Haiku"},
	} {
		if strings.Contains(lower, pair.token) {
			return pair.label
		}
	}
	return ""
}

func normalizeCodexPlanType(plan string) string {
	trimmed := strings.ToLower(strings.TrimSpace(plan))
	if trimmed == "" {
		return "plus"
	}
	return trimmed
}

func normalizeMagpieProvider(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	// 远程 Magpie 卡片的 provider 格式为 "<remote>/<provider>"（如 "office/codex"），取末段归一化。
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 && idx+1 < len(trimmed) {
		trimmed = strings.TrimSpace(trimmed[idx+1:])
	}
	switch trimmed {
	case "codex", "openai", "chatgpt":
		return usageHubProviderCodex
	case "claude", "anthropic", "claude-code", "claude_code":
		return usageHubProviderClaude
	case "antigravity", "agy", "google-antigravity":
		return usageHubProviderAntigravity
	default:
		return ""
	}
}

func normalizeMagpieProviders(raw []string) []string {
	seen := make(map[string]struct{}, len(usageHubSupportedMagpieProviders))
	for _, item := range raw {
		p := normalizeMagpieProvider(item)
		if p != "" {
			seen[p] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return append([]string(nil), usageHubSupportedMagpieProviders...)
	}
	ordered := make([]string, 0, len(seen))
	for _, p := range usageHubSupportedMagpieProviders {
		if _, ok := seen[p]; ok {
			ordered = append(ordered, p)
		}
	}
	return ordered
}

func formatMagpieAuthIndex(sourceID, cpaProvider, accountKey string) string {
	return fmt.Sprintf("%s%s:%s:%s", usageHubMagpieAuthPrefix, sourceID, cpaProvider, accountKey)
}

func parseMagpieAuthIndex(raw string) (sourceID, cpaProvider, accountKey string, ok bool) {
	if !strings.HasPrefix(raw, usageHubMagpieAuthPrefix) {
		return "", "", "", false
	}
	rest := strings.TrimPrefix(raw, usageHubMagpieAuthPrefix)
	parts := strings.Split(rest, ":")
	if len(parts) != 3 {
		return "", "", "", false
	}
	sourceID = strings.TrimSpace(parts[0])
	cpaProvider = normalizeMagpieProvider(parts[1])
	accountKey = strings.TrimSpace(parts[2])
	if sourceID == "" || cpaProvider == "" || accountKey == "" {
		return "", "", "", false
	}
	return sourceID, cpaProvider, accountKey, true
}

func buildMagpieAuthFileName(sourceName, cpaProvider, identity, shortHash string) string {
	srcSlug := sanitizeMagpieSourceID(sourceName)
	if srcSlug == "" {
		srcSlug = "magpie"
	}
	idSlug := sanitizeAuthFileToken(identity)
	if idSlug == "" {
		return fmt.Sprintf("magpie-%s-%s-%s.json", srcSlug, cpaProvider, shortHash)
	}
	return fmt.Sprintf("magpie-%s-%s-%s-%s.json", srcSlug, cpaProvider, idSlug, shortHash)
}

func sanitizeMagpieSourceID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	cleaned := strings.Trim(b.String(), "-_")
	if len(cleaned) > 32 {
		cleaned = cleaned[:32]
	}
	return cleaned
}

func generateMagpieSourceID() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "mp_" + hex.EncodeToString(buf), nil
}

func generateUsageHubUserKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return UsageHubUserKeyPrefix + hex.EncodeToString(buf), nil
}

func hashUsageHubToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func extractMagpieErrorMessage(raw []byte) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &parsed) == nil {
		if msg := strings.TrimSpace(parsed.Error.Message); msg != "" {
			return msg
		}
		if msg := strings.TrimSpace(parsed.Message); msg != "" {
			return msg
		}
	}
	text := strings.TrimSpace(string(raw))
	if len(text) > 160 {
		text = text[:160] + "..."
	}
	return text
}

func sortUsageHubWarnings(warnings []UsageHubPreviewWarning) {
	sort.Slice(warnings, func(i, j int) bool {
		return warnings[i].SourceID < warnings[j].SourceID
	})
}
