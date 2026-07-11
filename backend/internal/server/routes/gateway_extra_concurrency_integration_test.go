//go:build integration

package routes

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type extraConcurrencySchedulerCache struct {
	accounts []*service.Account
}

func (c *extraConcurrencySchedulerCache) GetSnapshot(context.Context, service.SchedulerBucket) ([]*service.Account, bool, error) {
	return c.accounts, true, nil
}
func (c *extraConcurrencySchedulerCache) SetSnapshot(context.Context, service.SchedulerBucket, []service.Account) error {
	return nil
}
func (c *extraConcurrencySchedulerCache) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	for _, account := range c.accounts {
		if account != nil && account.ID == id {
			return account, nil
		}
	}
	return nil, nil
}
func (c *extraConcurrencySchedulerCache) SetAccount(context.Context, *service.Account) error {
	return nil
}
func (c *extraConcurrencySchedulerCache) DeleteAccount(context.Context, int64) error { return nil }
func (c *extraConcurrencySchedulerCache) UpdateLastUsed(context.Context, map[int64]time.Time) error {
	return nil
}
func (c *extraConcurrencySchedulerCache) TryLockBucket(context.Context, service.SchedulerBucket, time.Duration) (bool, error) {
	return true, nil
}
func (c *extraConcurrencySchedulerCache) UnlockBucket(context.Context, service.SchedulerBucket) error {
	return nil
}
func (c *extraConcurrencySchedulerCache) ListBuckets(context.Context) ([]service.SchedulerBucket, error) {
	return nil, nil
}
func (c *extraConcurrencySchedulerCache) GetOutboxWatermark(context.Context) (int64, error) {
	return 0, nil
}
func (c *extraConcurrencySchedulerCache) SetOutboxWatermark(context.Context, int64) error {
	return nil
}

type extraConcurrencyGroupRepository struct {
	group *service.Group
}

func (r *extraConcurrencyGroupRepository) Create(context.Context, *service.Group) error { return nil }
func (r *extraConcurrencyGroupRepository) GetByID(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}
func (r *extraConcurrencyGroupRepository) GetByIDLite(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}
func (r *extraConcurrencyGroupRepository) Update(context.Context, *service.Group) error { return nil }
func (r *extraConcurrencyGroupRepository) Delete(context.Context, int64) error          { return nil }
func (r *extraConcurrencyGroupRepository) DeleteCascade(context.Context, int64) ([]int64, error) {
	return nil, nil
}
func (r *extraConcurrencyGroupRepository) List(context.Context, pagination.PaginationParams) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *extraConcurrencyGroupRepository) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *extraConcurrencyGroupRepository) ListActive(context.Context) ([]service.Group, error) {
	return nil, nil
}
func (r *extraConcurrencyGroupRepository) ListActiveByPlatform(context.Context, string) ([]service.Group, error) {
	return nil, nil
}
func (r *extraConcurrencyGroupRepository) ExistsByName(context.Context, string) (bool, error) {
	return false, nil
}
func (r *extraConcurrencyGroupRepository) GetAccountCount(context.Context, int64) (int64, int64, error) {
	return 0, 0, nil
}
func (r *extraConcurrencyGroupRepository) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (r *extraConcurrencyGroupRepository) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	return nil, nil
}
func (r *extraConcurrencyGroupRepository) BindAccountsToGroup(context.Context, int64, []int64) error {
	return nil
}
func (r *extraConcurrencyGroupRepository) UpdateSortOrders(context.Context, []service.GroupSortOrderUpdate) error {
	return nil
}

type extraConcurrencySettingRepository struct {
	waitTimeoutSeconds int
	reservePercent     float64
	minReservedSlots   int
}

func (extraConcurrencySettingRepository) Get(context.Context, string) (*service.Setting, error) {
	return nil, nil
}
func (extraConcurrencySettingRepository) GetValue(context.Context, string) (string, error) {
	return "", nil
}
func (extraConcurrencySettingRepository) Set(context.Context, string, string) error { return nil }
func (r extraConcurrencySettingRepository) GetMultiple(context.Context, []string) (map[string]string, error) {
	waitTimeoutSeconds := r.waitTimeoutSeconds
	if waitTimeoutSeconds <= 0 {
		waitTimeoutSeconds = 1
	}
	return map[string]string{
		service.SettingKeyExtraConcurrencyEnabled:            "true",
		service.SettingKeyExtraConcurrencyWaitTimeoutSeconds: strconv.Itoa(waitTimeoutSeconds),
		service.SettingKeyExtraConcurrencyReservePercent:     strconv.FormatFloat(r.reservePercent, 'f', -1, 64),
		service.SettingKeyExtraConcurrencyMinReservedSlots:   strconv.Itoa(r.minReservedSlots),
		service.SettingKeyExtraConcurrencyPlatformReserves:   "{}",
	}, nil
}
func (extraConcurrencySettingRepository) SetMultiple(context.Context, map[string]string) error {
	return nil
}
func (extraConcurrencySettingRepository) GetAll(context.Context) (map[string]string, error) {
	return nil, nil
}
func (extraConcurrencySettingRepository) Delete(context.Context, string) error { return nil }

type extraConcurrencyCapacity struct {
	accountID int64
}

func (c extraConcurrencyCapacity) AdmissionCapacity(context.Context, string) (service.AdmissionCapacitySnapshot, error) {
	return service.AdmissionCapacitySnapshot{
		TotalConcurrency:   1,
		AccountConcurrency: map[int64]int{c.accountID: 1},
	}, nil
}

type extraConcurrencyUpstream struct {
	calls atomic.Int32
}

func (u *extraConcurrencyUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"msg_redis","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
		)),
	}, nil
}
func (u *extraConcurrencyUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type extraConcurrencyRoutesHarness struct {
	router   *gin.Engine
	upstream *extraConcurrencyUpstream
	store    service.GatewayAdmissionStore
	userID   int64
}

func newExtraConcurrencyRoutesHarness(
	t *testing.T,
	groupID int64,
	accountID int64,
	userID int64,
	settings extraConcurrencySettingRepository,
) *extraConcurrencyRoutesHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rdb := startAuthRouteRedis(t, context.Background())
	group := &service.Group{
		ID:       groupID,
		Hydrated: true,
		Platform: service.PlatformAnthropic,
		Status:   service.StatusActive,
	}
	account := &service.Account{
		ID:          accountID,
		Name:        "anthropic-real-redis",
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeAPIKey,
		Concurrency: 1,
		Priority:    1,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key":  "upstream-key",
			"base_url": "https://api.anthropic.com",
		},
		Extra:         map[string]any{"anthropic_passthrough": true},
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	billingCacheService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCacheService.Stop)
	upstream := &extraConcurrencyUpstream{}
	schedulerSnapshot := service.NewSchedulerSnapshotService(
		&extraConcurrencySchedulerCache{accounts: []*service.Account{account}},
		nil,
		nil,
		nil,
		nil,
	)
	gatewayService := service.NewGatewayService(
		nil,
		&extraConcurrencyGroupRepository{group: group},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		schedulerSnapshot,
		nil,
		nil,
		&service.RateLimitService{},
		billingCacheService,
		nil,
		upstream,
		&service.DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	settingService := service.NewSettingService(settings, cfg)
	admissionStore := repository.NewGatewayAdmissionStore(rdb, 5*time.Second)
	admission := service.NewGatewayAdmission(
		admissionStore,
		gatewayService,
		extraConcurrencyCapacity{accountID: accountID},
	)
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount:      1,
		QueueSize:        1,
		AutoScaleEnabled: false,
	})
	pool.Stop()
	gatewayHandler := handler.NewGatewayHandler(
		gatewayService,
		nil,
		nil,
		nil,
		nil,
		admission,
		billingCacheService,
		nil,
		nil,
		pool,
		nil,
		nil,
		nil,
		cfg,
		settingService,
	)
	apiKey := &service.APIKey{
		ID:      userID + 1000,
		UserID:  userID,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User: &service.User{
			ID:               userID,
			Concurrency:      1,
			ExtraConcurrency: 1,
			Balance:          100,
		},
		Group: group,
	}
	router := gin.New()
	router.POST("/v1/messages", func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
		c.Set(string(servermiddleware.ContextKeyAPIKey), apiKey)
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{
			UserID:           apiKey.UserID,
			Concurrency:      1,
			ExtraConcurrency: 1,
		})
		gatewayHandler.Messages(c)
	})
	return &extraConcurrencyRoutesHarness{
		router:   router,
		upstream: upstream,
		store:    admissionStore,
		userID:   userID,
	}
}

func (h *extraConcurrencyRoutesHarness) request() *httptest.ResponseRecorder {
	requestBody := []byte(`{"model":"claude-sonnet-4-5","max_tokens":256,"messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.router.ServeHTTP(recorder, req)
	return recorder
}

func TestGatewayMessagesExtraConcurrencyUsesRealRedisAndReleasesAdmission(t *testing.T) {
	harness := newExtraConcurrencyRoutesHarness(t, 2201, 1201, 4201, extraConcurrencySettingRepository{})

	for range 2 {
		recorder := harness.request()

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Contains(t, recorder.Body.String(), `"id":"msg_redis"`)
	}
	require.Equal(t, int32(2), harness.upstream.calls.Load())
}

func TestGatewayMessagesExtraConcurrencyTimeoutUsesRealRedisWithoutUpstream(t *testing.T) {
	harness := newExtraConcurrencyRoutesHarness(t, 2202, 1202, 4202, extraConcurrencySettingRepository{
		waitTimeoutSeconds: 1,
		reservePercent:     100,
		minReservedSlots:   1,
	})
	blocker, err := harness.store.TryAcquireUserLease(t.Context(), service.UserLeaseRequest{
		RequestID:     "standard-blocker",
		UserID:        harness.userID,
		StandardLimit: 1,
		ExtraLimit:    1,
		MaxWaiting:    20,
		WaitTimeout:   2 * time.Second,
	})
	require.NoError(t, err)
	require.True(t, blocker.Acquired)
	require.Equal(t, service.AdmissionClassStandard, blocker.Class)
	t.Cleanup(func() {
		_ = harness.store.ReleaseUserLease(context.Background(), harness.userID, "standard-blocker")
	})

	recorder := harness.request()

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Contains(t, recorder.Body.String(), "EXTRA_CONCURRENCY_UNAVAILABLE")
	require.Zero(t, harness.upstream.calls.Load())
}
