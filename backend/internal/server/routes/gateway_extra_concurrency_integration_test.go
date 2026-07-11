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
	"github.com/tidwall/gjson"
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
	disabled           bool
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
	enabled := "true"
	if r.disabled {
		enabled = "false"
	}
	return map[string]string{
		service.SettingKeyExtraConcurrencyEnabled:            enabled,
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

type openAIExtraConcurrencyAccountRepository struct {
	service.AccountRepository
	accounts []service.Account
}

func (r openAIExtraConcurrencyAccountRepository) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r openAIExtraConcurrencyAccountRepository) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]service.Account, error) {
	return r.list(groupID, platform), nil
}

func (r openAIExtraConcurrencyAccountRepository) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.list(0, platform), nil
}

func (r openAIExtraConcurrencyAccountRepository) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.list(0, platform), nil
}

func (r openAIExtraConcurrencyAccountRepository) UpdateLastUsed(context.Context, int64) error {
	return nil
}

func (r openAIExtraConcurrencyAccountRepository) BatchUpdateLastUsed(context.Context, map[int64]time.Time) error {
	return nil
}

func (r openAIExtraConcurrencyAccountRepository) UpdateExtra(context.Context, int64, map[string]any) error {
	return nil
}

func (r openAIExtraConcurrencyAccountRepository) list(groupID int64, platform string) []service.Account {
	accounts := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform != platform {
			continue
		}
		if groupID > 0 {
			matched := false
			for _, accountGroupID := range account.GroupIDs {
				if accountGroupID == groupID {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		accounts = append(accounts, account)
	}
	return accounts
}

type openAIExtraConcurrencyCapacity struct {
	accountConcurrency map[int64]int
}

func (c openAIExtraConcurrencyCapacity) AdmissionCapacity(context.Context, string) (service.AdmissionCapacitySnapshot, error) {
	total := 0
	for _, concurrency := range c.accountConcurrency {
		total += concurrency
	}
	return service.AdmissionCapacitySnapshot{
		TotalConcurrency:   total,
		AccountConcurrency: c.accountConcurrency,
	}, nil
}

type blockingOpenAIExtraConcurrencyUpstream struct {
	arrivals chan int64
	release  <-chan struct{}
	calls    atomic.Int32
}

func (u *blockingOpenAIExtraConcurrencyUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.calls.Add(1)
	u.arrivals <- accountID
	<-u.release
	body := `{"id":"resp_extra","object":"response","status":"completed","model":"gpt-5.1","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
	if req != nil && strings.Contains(req.URL.Path, "chat/completions") {
		body = `{"id":"chatcmpl_extra","object":"chat.completion","created":1,"model":"gpt-5.1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func (u *blockingOpenAIExtraConcurrencyUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type retryingOpenAIExtraConcurrencyUpstream struct {
	arrivals chan string
	release  <-chan struct{}
	calls    atomic.Int32
}

type openAIExtraConcurrencyRequestNameKey struct{}
type openAIExtraConcurrencyEndpointKey struct{}

type openAIExtraConcurrencyRequestUser struct {
	userID        int64
	standardLimit int
	extraLimit    int
}

type openAIExtraConcurrencyRequestUserKey struct{}

type observedOpenAIUserLeaseAttempt struct {
	requestName string
	request     service.UserLeaseRequest
	result      service.UserLeaseResult
}

type observedOpenAITargetLeaseAttempt struct {
	requestName string
	request     service.TargetLeaseRequest
	result      service.TargetLeaseResult
}

type observedOpenAITargetLeaseRelease struct {
	requestID  string
	accountID  int64
	releaseErr error
}

type observingOpenAIGatewayAdmissionStore struct {
	service.GatewayAdmissionStore
	userAttempts                chan observedOpenAIUserLeaseAttempt
	targetAttempts              chan observedOpenAITargetLeaseAttempt
	targetReleases              chan observedOpenAITargetLeaseRelease
	targetReleaseBarrier        <-chan struct{}
	targetReleaseBarrierRequest string
	targetReleaseBarrierAccount int64
}

func newObservingOpenAIGatewayAdmissionStore(store service.GatewayAdmissionStore) *observingOpenAIGatewayAdmissionStore {
	return &observingOpenAIGatewayAdmissionStore{
		GatewayAdmissionStore: store,
		userAttempts:          make(chan observedOpenAIUserLeaseAttempt, 256),
		targetAttempts:        make(chan observedOpenAITargetLeaseAttempt, 256),
		targetReleases:        make(chan observedOpenAITargetLeaseRelease, 256),
	}
}

func (s *observingOpenAIGatewayAdmissionStore) TryAcquireUserLease(ctx context.Context, request service.UserLeaseRequest) (service.UserLeaseResult, error) {
	result, err := s.GatewayAdmissionStore.TryAcquireUserLease(ctx, request)
	if err == nil {
		requestName, _ := ctx.Value(openAIExtraConcurrencyRequestNameKey{}).(string)
		select {
		case s.userAttempts <- observedOpenAIUserLeaseAttempt{
			requestName: strings.ToUpper(requestName),
			request:     request,
			result:      result,
		}:
		default:
		}
	}
	return result, err
}

func (s *observingOpenAIGatewayAdmissionStore) ReleaseTargetLease(ctx context.Context, platform string, accountID int64, requestID string) error {
	err := s.GatewayAdmissionStore.ReleaseTargetLease(ctx, platform, accountID, requestID)
	select {
	case s.targetReleases <- observedOpenAITargetLeaseRelease{
		requestID:  requestID,
		accountID:  accountID,
		releaseErr: err,
	}:
	default:
	}
	if requestID == s.targetReleaseBarrierRequest && accountID == s.targetReleaseBarrierAccount && s.targetReleaseBarrier != nil {
		select {
		case <-s.targetReleaseBarrier:
		case <-ctx.Done():
		}
	}
	return err
}

func (s *observingOpenAIGatewayAdmissionStore) TryAcquireTargetLease(ctx context.Context, request service.TargetLeaseRequest) (service.TargetLeaseResult, error) {
	result, err := s.GatewayAdmissionStore.TryAcquireTargetLease(ctx, request)
	if err == nil {
		requestName, _ := ctx.Value(openAIExtraConcurrencyRequestNameKey{}).(string)
		select {
		case s.targetAttempts <- observedOpenAITargetLeaseAttempt{
			requestName: strings.ToUpper(requestName),
			request:     request,
			result:      result,
		}:
		default:
		}
	}
	return result, err
}

func (u *retryingOpenAIExtraConcurrencyUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewReader(body))
	requestName, _ := req.Context().Value(openAIExtraConcurrencyRequestNameKey{}).(string)
	requestName = strings.ToUpper(requestName)
	call := u.calls.Add(1)
	u.arrivals <- requestName
	if call == 1 {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"retry on the same account"}}`)),
		}, nil
	}
	<-u.release
	responseBody := `{"id":"resp_retry","object":"response","status":"completed","model":"gpt-5.1","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
	contentType := "application/json"
	if strings.Contains(req.URL.Path, "chat/completions") {
		responseBody = `{"id":"chatcmpl_retry","object":"chat.completion","created":1,"model":"gpt-5.1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	} else if endpoint, _ := req.Context().Value(openAIExtraConcurrencyEndpointKey{}).(string); endpoint == "messages" {
		contentType = "text/event-stream"
		responseBody = "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_messages_retry\",\"object\":\"response\",\"status\":\"completed\",\"model\":\"gpt-5.1\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\ndata: [DONE]\n\n"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}, nil
}

func (u *retryingOpenAIExtraConcurrencyUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type scriptedOpenAIFailoverArrival struct {
	requestName string
	accountID   int64
}

type scriptedOpenAIFailoverUpstream struct {
	arrivals  chan scriptedOpenAIFailoverArrival
	releaseA1 <-chan struct{}
	releaseA2 <-chan struct{}
	releaseB  <-chan struct{}
	releaseC  <-chan struct{}
	calls     atomic.Int32
}

func (u *scriptedOpenAIFailoverUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	requestName, _ := req.Context().Value(openAIExtraConcurrencyRequestNameKey{}).(string)
	requestName = strings.ToUpper(requestName)
	u.calls.Add(1)
	u.arrivals <- scriptedOpenAIFailoverArrival{requestName: requestName, accountID: accountID}

	switch {
	case requestName == "A" && accountID == 1301:
		<-u.releaseA1
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"switch accounts","type":"server_error"}}`)),
		}, nil
	case requestName == "A" && accountID == 1302:
		<-u.releaseA2
	case requestName == "B":
		<-u.releaseB
	case requestName == "C":
		<-u.releaseC
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_failover","object":"response","status":"completed","model":"gpt-5.1","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
		)),
	}, nil
}

func (u *scriptedOpenAIFailoverUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func requireScriptedOpenAIFailoverArrival(t *testing.T, arrivals <-chan scriptedOpenAIFailoverArrival, requestName string, accountID int64) {
	t.Helper()
	select {
	case arrival := <-arrivals:
		require.Equal(t, requestName, arrival.requestName)
		require.Equal(t, accountID, arrival.accountID)
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s on account %d", requestName, accountID)
	}
}

func requireObservedOpenAIUserLeaseAttempt(
	t *testing.T,
	attempts <-chan observedOpenAIUserLeaseAttempt,
	requestName string,
	acquired bool,
) observedOpenAIUserLeaseAttempt {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	seen := make([]observedOpenAIUserLeaseAttempt, 0, 8)
	for {
		select {
		case attempt := <-attempts:
			seen = append(seen, attempt)
			if attempt.requestName == requestName && attempt.result.Acquired == acquired {
				return attempt
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s user lease acquired=%t; seen=%+v", requestName, acquired, seen)
			return observedOpenAIUserLeaseAttempt{}
		}
	}
}

func requireObservedOpenAITargetLeaseAttempt(
	t *testing.T,
	attempts <-chan observedOpenAITargetLeaseAttempt,
	requestName string,
	accountID int64,
	acquired bool,
) observedOpenAITargetLeaseAttempt {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	seen := make([]observedOpenAITargetLeaseAttempt, 0, 8)
	for {
		select {
		case attempt := <-attempts:
			seen = append(seen, attempt)
			if attempt.requestName == requestName && attempt.request.AccountID == accountID && attempt.result.Acquired == acquired {
				return attempt
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s target account %d acquired=%t; seen=%+v", requestName, accountID, acquired, seen)
			return observedOpenAITargetLeaseAttempt{}
		}
	}
}

func requireObservedOpenAITargetLeaseRelease(
	t *testing.T,
	releases <-chan observedOpenAITargetLeaseRelease,
	requestID string,
	accountID int64,
) observedOpenAITargetLeaseRelease {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case release := <-releases:
			if release.requestID == requestID && release.accountID == accountID {
				return release
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for target account %d release for request %s", accountID, requestID)
			return observedOpenAITargetLeaseRelease{}
		}
	}
}

func requireOpenAIExtraConcurrencyHTTPResponse(t *testing.T, responses <-chan *httptest.ResponseRecorder, requestName string) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case response := <-responses:
		return response
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s HTTP response", requestName)
		return nil
	}
}

type openAIExtraConcurrencyRoutesHarness struct {
	router   *gin.Engine
	store    service.GatewayAdmissionStore
	observer *observingOpenAIGatewayAdmissionStore
	userID   int64
}

func newOpenAIExtraConcurrencyRoutesHarness(
	t *testing.T,
	settings extraConcurrencySettingRepository,
	upstream service.HTTPUpstream,
	accountExtra map[string]any,
	accountOverrides ...[]service.Account,
) *openAIExtraConcurrencyRoutesHarness {
	return newOpenAIExtraConcurrencyRoutesHarnessWithLoadBatch(
		t,
		settings,
		upstream,
		accountExtra,
		true,
		accountOverrides...,
	)
}

func newOpenAIExtraConcurrencyRoutesHarnessWithLoadBatch(
	t *testing.T,
	settings extraConcurrencySettingRepository,
	upstream service.HTTPUpstream,
	accountExtra map[string]any,
	loadBatchEnabled bool,
	accountOverrides ...[]service.Account,
) *openAIExtraConcurrencyRoutesHarness {
	t.Helper()
	if accountExtra == nil {
		accountExtra = map[string]any{"openai_passthrough": true}
	}
	gin.SetMode(gin.TestMode)
	rdb := startAuthRouteRedis(t, context.Background())
	groupID := int64(2301)
	userID := int64(4301)
	accounts := []service.Account{
		{
			ID:          1301,
			Name:        "openai-extra-1",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Concurrency: 1,
			Priority:    0,
			Status:      service.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"api_key": "sk-1", "base_url": "https://api.openai.com"},
			Extra:       accountExtra,
			GroupIDs:    []int64{groupID},
		},
		{
			ID:          1302,
			Name:        "openai-extra-2",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Concurrency: 1,
			Priority:    1,
			Status:      service.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"api_key": "sk-2", "base_url": "https://api.openai.com"},
			Extra:       accountExtra,
			GroupIDs:    []int64{groupID},
		},
	}
	if len(accountOverrides) > 0 {
		accounts = accountOverrides[0]
	}
	group := &service.Group{
		ID:                    groupID,
		Hydrated:              true,
		Platform:              service.PlatformOpenAI,
		Status:                service.StatusActive,
		AllowMessagesDispatch: true,
	}
	accountRepo := openAIExtraConcurrencyAccountRepository{accounts: accounts}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.Scheduling.LoadBatchEnabled = loadBatchEnabled
	legacyConcurrency := service.NewConcurrencyService(repository.NewConcurrencyCache(rdb, 1, 30))
	billingCacheService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCacheService.Stop)
	openAIService := service.NewOpenAIGatewayService(
		accountRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		legacyConcurrency,
		service.NewBillingService(cfg, nil),
		nil,
		billingCacheService,
		upstream,
		&service.DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	settingService := service.NewSettingService(settings, cfg)
	admissionStore := repository.NewGatewayAdmissionStore(rdb, 30*time.Second)
	observingAdmissionStore := newObservingOpenAIGatewayAdmissionStore(admissionStore)
	accountConcurrency := make(map[int64]int, len(accounts))
	for i := range accounts {
		accountConcurrency[accounts[i].ID] = accounts[i].Concurrency
	}
	admission := service.NewGatewayAdmission(
		observingAdmissionStore,
		nil,
		openAIExtraConcurrencyCapacity{accountConcurrency: accountConcurrency},
	)
	openAIHandler := handler.NewOpenAIGatewayHandler(
		openAIService,
		legacyConcurrency,
		admission,
		billingCacheService,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil,
		nil,
		nil,
		nil,
		cfg,
		settingService,
	)
	apiKey := &service.APIKey{
		ID:      3301,
		UserID:  userID,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User: &service.User{
			ID:               userID,
			Status:           service.StatusActive,
			Concurrency:      1,
			ExtraConcurrency: 1,
			Balance:          100,
		},
		Group: group,
	}
	setRequestAuth := func(c *gin.Context) {
		requestUser := openAIExtraConcurrencyRequestUser{
			userID:        userID,
			standardLimit: 1,
			extraLimit:    1,
		}
		if override, ok := c.Request.Context().Value(openAIExtraConcurrencyRequestUserKey{}).(openAIExtraConcurrencyRequestUser); ok {
			requestUser = override
		}
		user := *apiKey.User
		user.ID = requestUser.userID
		user.Concurrency = requestUser.standardLimit
		user.ExtraConcurrency = requestUser.extraLimit
		requestAPIKey := *apiKey
		requestAPIKey.ID = apiKey.ID + requestUser.userID - userID
		requestAPIKey.UserID = requestUser.userID
		requestAPIKey.User = &user
		c.Set(string(servermiddleware.ContextKeyAPIKey), &requestAPIKey)
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{
			UserID:           requestUser.userID,
			Concurrency:      requestUser.standardLimit,
			ExtraConcurrency: requestUser.extraLimit,
		})
	}
	router := gin.New()
	router.POST("/v1/responses", func(c *gin.Context) {
		setRequestAuth(c)
		openAIHandler.Responses(c)
	})
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		setRequestAuth(c)
		openAIHandler.ChatCompletions(c)
	})
	router.POST("/v1/messages", func(c *gin.Context) {
		setRequestAuth(c)
		openAIHandler.Messages(c)
	})
	return &openAIExtraConcurrencyRoutesHarness{
		router:   router,
		store:    admissionStore,
		observer: observingAdmissionStore,
		userID:   userID,
	}
}

func (h *openAIExtraConcurrencyRoutesHarness) responsesRequest(session string) *httptest.ResponseRecorder {
	return h.responsesRequestForUser(session, h.userID, 1, 1)
}

func (h *openAIExtraConcurrencyRoutesHarness) responsesRequestForUser(session string, userID int64, standardLimit int, extraLimit int) *httptest.ResponseRecorder {
	body := `{"model":"gpt-5.1","stream":false,"prompt_cache_key":"` + session + `","input":"` + session + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), openAIExtraConcurrencyRequestNameKey{}, strings.TrimPrefix(session, "request-")))
	req = req.WithContext(context.WithValue(req.Context(), openAIExtraConcurrencyRequestUserKey{}, openAIExtraConcurrencyRequestUser{
		userID:        userID,
		standardLimit: standardLimit,
		extraLimit:    extraLimit,
	}))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.router.ServeHTTP(recorder, req)
	return recorder
}

func (h *openAIExtraConcurrencyRoutesHarness) chatCompletionsRequest(session string) *httptest.ResponseRecorder {
	body := `{"model":"gpt-5.1","stream":false,"prompt_cache_key":"` + session + `","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), openAIExtraConcurrencyRequestNameKey{}, strings.TrimPrefix(session, "request-")))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.router.ServeHTTP(recorder, req)
	return recorder
}

func (h *openAIExtraConcurrencyRoutesHarness) messagesRequest(session string) *httptest.ResponseRecorder {
	return h.messagesRequestWithContext(context.Background(), session)
}

func (h *openAIExtraConcurrencyRoutesHarness) messagesRequestWithContext(ctx context.Context, session string) *httptest.ResponseRecorder {
	body := `{"model":"gpt-5.1","stream":false,"metadata":{"user_id":"` + session + `"},"max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req = req.WithContext(ctx)
	req = req.WithContext(context.WithValue(req.Context(), openAIExtraConcurrencyRequestNameKey{}, strings.TrimPrefix(session, "request-")))
	req = req.WithContext(context.WithValue(req.Context(), openAIExtraConcurrencyEndpointKey{}, "messages"))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	h.router.ServeHTTP(recorder, req)
	return recorder
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

type retryingMessagesExtraConcurrencyUpstream struct {
	arrivals chan string
	release  <-chan struct{}
	calls    atomic.Int32
}

func (u *retryingMessagesExtraConcurrencyUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	requestName, _ := req.Context().Value(openAIExtraConcurrencyRequestNameKey{}).(string)
	requestName = strings.ToUpper(requestName)
	call := u.calls.Add(1)
	u.arrivals <- requestName
	if call == 1 {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"rate_limit_error","message":"retry on the same account"}}`)),
		}, nil
	}
	<-u.release
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"msg_retry","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
		)),
	}, nil
}

func (u *retryingMessagesExtraConcurrencyUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type extraConcurrencyRoutesHarness struct {
	router   *gin.Engine
	upstream *extraConcurrencyUpstream
	store    service.GatewayAdmissionStore
	userID   int64
}

type extraConcurrencyRoutesHarnessOptions struct {
	upstream    service.HTTPUpstream
	credentials map[string]any
}

func newExtraConcurrencyRoutesHarness(
	t *testing.T,
	groupID int64,
	accountID int64,
	userID int64,
	settings extraConcurrencySettingRepository,
	options ...extraConcurrencyRoutesHarnessOptions,
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
	credentials := map[string]any{
		"api_key":  "upstream-key",
		"base_url": "https://api.anthropic.com",
	}
	if len(options) > 0 && options[0].credentials != nil {
		credentials = options[0].credentials
	}
	account := &service.Account{
		ID:            accountID,
		Name:          "anthropic-real-redis",
		Platform:      service.PlatformAnthropic,
		Type:          service.AccountTypeAPIKey,
		Concurrency:   1,
		Priority:      1,
		Status:        service.StatusActive,
		Schedulable:   true,
		Credentials:   credentials,
		Extra:         map[string]any{"anthropic_passthrough": true},
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	billingCacheService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCacheService.Stop)
	defaultUpstream := &extraConcurrencyUpstream{}
	var upstream service.HTTPUpstream = defaultUpstream
	if len(options) > 0 && options[0].upstream != nil {
		upstream = options[0].upstream
	}
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
		upstream: defaultUpstream,
		store:    admissionStore,
		userID:   userID,
	}
}

func (h *extraConcurrencyRoutesHarness) request() *httptest.ResponseRecorder {
	return h.requestWithContent("hello")
}

func (h *extraConcurrencyRoutesHarness) requestWithContent(content string) *httptest.ResponseRecorder {
	requestBody := []byte(`{"model":"claude-sonnet-4-5","max_tokens":256,"messages":[{"role":"user","content":"` + content + `"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(requestBody))
	req = req.WithContext(context.WithValue(req.Context(), openAIExtraConcurrencyRequestNameKey{}, strings.TrimPrefix(content, "request-")))
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

func TestGatewayMessagesSameAccountRetryKeepsTargetLease(t *testing.T) {
	releaseUpstream := make(chan struct{})
	t.Cleanup(func() { close(releaseUpstream) })
	upstream := &retryingMessagesExtraConcurrencyUpstream{
		arrivals: make(chan string, 3),
		release:  releaseUpstream,
	}
	harness := newExtraConcurrencyRoutesHarness(
		t,
		2203,
		1203,
		4203,
		extraConcurrencySettingRepository{},
		extraConcurrencyRoutesHarnessOptions{
			upstream: upstream,
			credentials: map[string]any{
				"api_key":               "upstream-pool-key",
				"base_url":              "https://api.anthropic.com",
				"pool_mode":             true,
				"pool_mode_retry_count": 1,
			},
		},
	)

	responses := make(chan *httptest.ResponseRecorder, 2)
	go func() { responses <- harness.requestWithContent("request-a") }()
	require.Equal(t, "A", <-upstream.arrivals)
	go func() { responses <- harness.requestWithContent("request-b") }()

	select {
	case second := <-upstream.arrivals:
		require.Equal(t, "A", second, "same-account retry must run before the waiting request")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the same-account retry")
	}
	releaseUpstream <- struct{}{}
	select {
	case third := <-upstream.arrivals:
		require.Equal(t, "B", third)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the queued request")
	}
	releaseUpstream <- struct{}{}

	firstResponse := <-responses
	secondResponse := <-responses
	require.Equal(t, http.StatusOK, firstResponse.Code)
	require.Equal(t, http.StatusOK, secondResponse.Code)
	require.Equal(t, int32(3), upstream.calls.Load())
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

func TestOpenAIResponsesExtraConcurrencyDispatchesSecondConcurrentRequest(t *testing.T) {
	releaseUpstream := make(chan struct{})
	upstream := &blockingOpenAIExtraConcurrencyUpstream{
		arrivals: make(chan int64, 2),
		release:  releaseUpstream,
	}
	harness := newOpenAIExtraConcurrencyRoutesHarness(t, extraConcurrencySettingRepository{}, upstream, nil)
	responses := make(chan *httptest.ResponseRecorder, 2)
	go func() { responses <- harness.responsesRequest("first") }()
	firstAccountID := <-upstream.arrivals
	go func() { responses <- harness.responsesRequest("second") }()

	secondReachedBeforeRelease := false
	select {
	case secondAccountID := <-upstream.arrivals:
		secondReachedBeforeRelease = true
		require.NotEqual(t, firstAccountID, secondAccountID)
	case <-time.After(750 * time.Millisecond):
	}
	close(releaseUpstream)
	firstResponse := <-responses
	secondResponse := <-responses

	require.True(t, secondReachedBeforeRelease)
	require.Equal(t, http.StatusOK, firstResponse.Code)
	require.Equal(t, http.StatusOK, secondResponse.Code)
	require.Equal(t, int32(2), upstream.calls.Load())
}

func TestOpenAIResponsesSameAccountRetryKeepsTargetLease(t *testing.T) {
	releaseUpstream := make(chan struct{})
	t.Cleanup(func() { close(releaseUpstream) })
	upstream := &retryingOpenAIExtraConcurrencyUpstream{
		arrivals: make(chan string, 3),
		release:  releaseUpstream,
	}
	accountExtra := map[string]any{
		"openai_passthrough":    false,
		"openai_responses_mode": "force_responses",
	}
	harness := newOpenAIExtraConcurrencyRoutesHarness(
		t,
		extraConcurrencySettingRepository{},
		upstream,
		accountExtra,
		[]service.Account{{
			ID:          1301,
			Name:        "openai-pool-mode",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Concurrency: 1,
			Status:      service.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"api_key":               "sk-pool",
				"base_url":              "https://api.openai.com",
				"pool_mode":             true,
				"pool_mode_retry_count": 1,
			},
			Extra:    accountExtra,
			GroupIDs: []int64{2301},
		}},
	)

	responses := make(chan *httptest.ResponseRecorder, 2)
	go func() { responses <- harness.responsesRequest("request-a") }()
	require.Equal(t, "A", <-upstream.arrivals)
	go func() { responses <- harness.responsesRequest("request-b") }()

	select {
	case second := <-upstream.arrivals:
		require.Equal(t, "A", second, "same-account retry must run before the waiting request")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the same-account retry")
	}
	releaseUpstream <- struct{}{}
	select {
	case third := <-upstream.arrivals:
		require.Equal(t, "B", third)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the queued request")
	}
	releaseUpstream <- struct{}{}

	firstResponse := <-responses
	secondResponse := <-responses
	require.Equal(t, http.StatusOK, firstResponse.Code)
	require.Equal(t, http.StatusOK, secondResponse.Code)
	require.Equal(t, int32(3), upstream.calls.Load())
}

func TestOpenAIMessagesSameAccountRetryKeepsTargetLease(t *testing.T) {
	releaseUpstream := make(chan struct{})
	t.Cleanup(func() { close(releaseUpstream) })
	upstream := &retryingOpenAIExtraConcurrencyUpstream{
		arrivals: make(chan string, 3),
		release:  releaseUpstream,
	}
	accountExtra := map[string]any{
		"openai_passthrough":    false,
		"openai_responses_mode": "force_responses",
	}
	harness := newOpenAIExtraConcurrencyRoutesHarness(
		t,
		extraConcurrencySettingRepository{},
		upstream,
		accountExtra,
		[]service.Account{{
			ID:          1301,
			Name:        "openai-pool-mode-messages",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Concurrency: 1,
			Status:      service.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"api_key":               "sk-pool-messages",
				"base_url":              "https://api.openai.com",
				"pool_mode":             true,
				"pool_mode_retry_count": 1,
			},
			Extra:    accountExtra,
			GroupIDs: []int64{2301},
		}},
	)

	responses := make(chan *httptest.ResponseRecorder, 2)
	go func() { responses <- harness.messagesRequest("request-a") }()
	require.Equal(t, "A", <-upstream.arrivals)
	firstTarget := requireObservedOpenAITargetLeaseAttempt(t, harness.observer.targetAttempts, "A", 1301, true)
	require.Equal(t, service.AdmissionClassStandard, firstTarget.request.Class)
	go func() { responses <- harness.messagesRequest("request-b") }()

	select {
	case second := <-upstream.arrivals:
		require.Equal(t, "A", second, "same-account retry must run before the waiting request")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the same-account retry")
	}
	releaseUpstream <- struct{}{}
	select {
	case third := <-upstream.arrivals:
		require.Equal(t, "B", third)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the queued request")
	}
	releaseUpstream <- struct{}{}

	firstResponse := <-responses
	secondResponse := <-responses
	require.Equal(t, http.StatusOK, firstResponse.Code)
	require.Equal(t, http.StatusOK, secondResponse.Code)
	require.Equal(t, int32(3), upstream.calls.Load())
}

func TestOpenAIMessagesExtraConcurrencyReservationUsesAnthropicErrorWithoutUpstream(t *testing.T) {
	upstream := &extraConcurrencyUpstream{}
	harness := newOpenAIExtraConcurrencyRoutesHarness(t, extraConcurrencySettingRepository{
		waitTimeoutSeconds: 1,
		reservePercent:     100,
		minReservedSlots:   1,
	}, upstream, nil)
	blocker, err := harness.store.TryAcquireUserLease(t.Context(), service.UserLeaseRequest{
		RequestID:     "openai-messages-standard-blocker",
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
		_ = harness.store.ReleaseUserLease(context.Background(), harness.userID, "openai-messages-standard-blocker")
	})

	recorder := harness.messagesRequest("reservation")

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "error", gjson.GetBytes(recorder.Body.Bytes(), "type").String())
	require.Equal(t, "EXTRA_CONCURRENCY_UNAVAILABLE", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
	require.Zero(t, upstream.calls.Load())
}

func TestOpenAIMessagesCanceledDuringSameAccountRetryDoesNotWriteFallback(t *testing.T) {
	releaseUpstream := make(chan struct{})
	t.Cleanup(func() { close(releaseUpstream) })
	upstream := &retryingOpenAIExtraConcurrencyUpstream{
		arrivals: make(chan string, 1),
		release:  releaseUpstream,
	}
	accountExtra := map[string]any{
		"openai_passthrough":    false,
		"openai_responses_mode": "force_responses",
	}
	harness := newOpenAIExtraConcurrencyRoutesHarness(
		t,
		extraConcurrencySettingRepository{},
		upstream,
		accountExtra,
		[]service.Account{{
			ID:          1301,
			Name:        "openai-pool-mode-messages-cancel",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Concurrency: 1,
			Status:      service.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"api_key":               "sk-pool-messages-cancel",
				"base_url":              "https://api.openai.com",
				"pool_mode":             true,
				"pool_mode_retry_count": 1,
			},
			Extra:    accountExtra,
			GroupIDs: []int64{2301},
		}},
	)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	responses := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		responses <- harness.messagesRequestWithContext(requestCtx, "request-cancel")
	}()
	require.Equal(t, "CANCEL", <-upstream.arrivals)
	cancelRequest()

	response := requireOpenAIExtraConcurrencyHTTPResponse(t, responses, "CANCEL")
	require.Empty(t, response.Body.String())
	require.Equal(t, int32(1), upstream.calls.Load())
}

func TestOpenAIResponsesAccountFailoverReleasesOldTargetWithoutReleasingUserSlot(t *testing.T) {
	releaseA1 := make(chan struct{})
	releaseA2 := make(chan struct{})
	releaseB := make(chan struct{})
	releaseC := make(chan struct{})
	t.Cleanup(func() {
		close(releaseA1)
		close(releaseA2)
		close(releaseB)
		close(releaseC)
	})
	upstream := &scriptedOpenAIFailoverUpstream{
		arrivals:  make(chan scriptedOpenAIFailoverArrival, 4),
		releaseA1: releaseA1,
		releaseA2: releaseA2,
		releaseB:  releaseB,
		releaseC:  releaseC,
	}
	accountExtra := map[string]any{
		"openai_passthrough":    false,
		"openai_responses_mode": "force_responses",
	}
	harness := newOpenAIExtraConcurrencyRoutesHarnessWithLoadBatch(
		t,
		extraConcurrencySettingRepository{waitTimeoutSeconds: 10},
		upstream,
		accountExtra,
		false,
		[]service.Account{
			{
				ID:          1301,
				Name:        "openai-failover-primary",
				Platform:    service.PlatformOpenAI,
				Type:        service.AccountTypeAPIKey,
				Concurrency: 1,
				Priority:    0,
				Status:      service.StatusActive,
				Schedulable: true,
				Credentials: map[string]any{"api_key": "sk-primary", "base_url": "https://api.openai.com"},
				Extra:       accountExtra,
				GroupIDs:    []int64{2301},
			},
			{
				ID:          1302,
				Name:        "openai-failover-secondary",
				Platform:    service.PlatformOpenAI,
				Type:        service.AccountTypeAPIKey,
				Concurrency: 1,
				Priority:    1,
				Status:      service.StatusActive,
				Schedulable: true,
				Credentials: map[string]any{"api_key": "sk-secondary", "base_url": "https://api.openai.com"},
				Extra:       accountExtra,
				GroupIDs:    []int64{2301},
			},
		},
	)

	blockerRequest := service.TargetLeaseRequest{
		RequestID:        "openai-failover-secondary-blocker",
		Platform:         service.PlatformOpenAI,
		AccountID:        1302,
		AccountLimit:     1,
		PlatformCapacity: 2,
		Class:            service.AdmissionClassStandard,
		WaitTimeout:      30 * time.Second,
	}
	blocker, err := harness.store.TryAcquireTargetLease(t.Context(), blockerRequest)
	require.NoError(t, err)
	require.True(t, blocker.Acquired)
	t.Cleanup(func() {
		_ = harness.store.ReleaseTargetLease(context.Background(), blockerRequest.Platform, blockerRequest.AccountID, blockerRequest.RequestID)
	})

	aResponses := make(chan *httptest.ResponseRecorder, 1)
	bResponses := make(chan *httptest.ResponseRecorder, 1)
	cResponses := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		aResponses <- harness.responsesRequestForUser("request-a", harness.userID, 1, 0)
	}()
	requireScriptedOpenAIFailoverArrival(t, upstream.arrivals, "A", 1301)
	aInitialTarget := requireObservedOpenAITargetLeaseAttempt(t, harness.observer.targetAttempts, "A", 1301, true)

	go func() {
		bResponses <- harness.responsesRequestForUser("request-b", harness.userID, 1, 0)
	}()
	bWaiting := requireObservedOpenAIUserLeaseAttempt(t, harness.observer.userAttempts, "B", false)
	require.Equal(t, harness.userID, bWaiting.request.UserID)
	require.False(t, bWaiting.result.QueueFull)

	allowAFailover := make(chan struct{})
	t.Cleanup(func() { close(allowAFailover) })
	harness.observer.targetReleaseBarrier = allowAFailover
	harness.observer.targetReleaseBarrierRequest = aInitialTarget.request.RequestID
	harness.observer.targetReleaseBarrierAccount = 1301
	releaseA1 <- struct{}{}
	oldTargetRelease := requireObservedOpenAITargetLeaseRelease(
		t,
		harness.observer.targetReleases,
		aInitialTarget.request.RequestID,
		1301,
	)
	require.NoError(t, oldTargetRelease.releaseErr)

	go func() {
		cResponses <- harness.responsesRequestForUser("request-c", harness.userID+1, 1, 0)
	}()
	requireScriptedOpenAIFailoverArrival(t, upstream.arrivals, "C", 1301)
	select {
	case arrival := <-upstream.arrivals:
		t.Fatalf("request %s unexpectedly reached account %d while A was switching accounts", arrival.requestName, arrival.accountID)
	case <-time.After(150 * time.Millisecond):
	}
	allowAFailover <- struct{}{}

	var aWaitingForSecondary observedOpenAITargetLeaseAttempt
	seenTargetAttempts := make([]observedOpenAITargetLeaseAttempt, 0, 8)
	secondaryWaitTimer := time.NewTimer(3 * time.Second)
	defer secondaryWaitTimer.Stop()
waitForSecondary:
	for {
		select {
		case response := <-aResponses:
			t.Fatalf("A returned before attempting the secondary account: status=%d body=%s", response.Code, response.Body.String())
		case arrival := <-upstream.arrivals:
			t.Fatalf("unexpected upstream arrival before A attempted the secondary account: request=%s account=%d", arrival.requestName, arrival.accountID)
		case attempt := <-harness.observer.targetAttempts:
			seenTargetAttempts = append(seenTargetAttempts, attempt)
			if attempt.request.RequestID == aInitialTarget.request.RequestID && attempt.request.AccountID == 1302 && !attempt.result.Acquired {
				aWaitingForSecondary = attempt
				break waitForSecondary
			}
		case <-secondaryWaitTimer.C:
			t.Fatalf("timed out waiting for A to attempt the secondary account; target attempts=%+v", seenTargetAttempts)
		}
	}
	require.False(t, aWaitingForSecondary.result.Expired)

	releaseC <- struct{}{}
	cResponse := requireOpenAIExtraConcurrencyHTTPResponse(t, cResponses, "C")
	require.Equal(t, http.StatusOK, cResponse.Code)

	require.NoError(t, harness.store.ReleaseTargetLease(
		t.Context(),
		blockerRequest.Platform,
		blockerRequest.AccountID,
		blockerRequest.RequestID,
	))
	requireScriptedOpenAIFailoverArrival(t, upstream.arrivals, "A", 1302)
	select {
	case arrival := <-upstream.arrivals:
		t.Fatalf("request %s unexpectedly reached account %d before A completed", arrival.requestName, arrival.accountID)
	case <-time.After(150 * time.Millisecond):
	}

	releaseA2 <- struct{}{}
	aResponse := requireOpenAIExtraConcurrencyHTTPResponse(t, aResponses, "A")
	require.Equal(t, http.StatusOK, aResponse.Code)
	requireScriptedOpenAIFailoverArrival(t, upstream.arrivals, "B", 1301)
	releaseB <- struct{}{}
	bResponse := requireOpenAIExtraConcurrencyHTTPResponse(t, bResponses, "B")
	require.Equal(t, http.StatusOK, bResponse.Code)
	require.Equal(t, int32(4), upstream.calls.Load())
}

func TestOpenAIChatCompletionsSameAccountRetryKeepsTargetLease(t *testing.T) {
	releaseUpstream := make(chan struct{})
	t.Cleanup(func() { close(releaseUpstream) })
	upstream := &retryingOpenAIExtraConcurrencyUpstream{
		arrivals: make(chan string, 3),
		release:  releaseUpstream,
	}
	accountExtra := map[string]any{
		"openai_passthrough":    false,
		"openai_responses_mode": "force_chat_completions",
	}
	harness := newOpenAIExtraConcurrencyRoutesHarness(
		t,
		extraConcurrencySettingRepository{},
		upstream,
		accountExtra,
		[]service.Account{{
			ID:          1301,
			Name:        "openai-pool-mode-chat",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Concurrency: 1,
			Status:      service.StatusActive,
			Schedulable: true,
			Credentials: map[string]any{
				"api_key":               "sk-pool-chat",
				"base_url":              "https://api.openai.com",
				"pool_mode":             true,
				"pool_mode_retry_count": 1,
			},
			Extra:    accountExtra,
			GroupIDs: []int64{2301},
		}},
	)

	responses := make(chan *httptest.ResponseRecorder, 2)
	go func() { responses <- harness.chatCompletionsRequest("request-a") }()
	require.Equal(t, "A", <-upstream.arrivals)
	go func() { responses <- harness.chatCompletionsRequest("request-b") }()

	select {
	case second := <-upstream.arrivals:
		require.Equal(t, "A", second, "same-account retry must run before the waiting request")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the same-account retry")
	}
	releaseUpstream <- struct{}{}
	select {
	case third := <-upstream.arrivals:
		require.Equal(t, "B", third)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the queued request")
	}
	releaseUpstream <- struct{}{}

	firstResponse := <-responses
	secondResponse := <-responses
	require.Equal(t, http.StatusOK, firstResponse.Code)
	require.Equal(t, http.StatusOK, secondResponse.Code)
	require.Equal(t, int32(3), upstream.calls.Load())
}

func TestOpenAIResponsesExtraConcurrencyTimeoutUsesRealRedisWithoutUpstream(t *testing.T) {
	upstream := &extraConcurrencyUpstream{}
	harness := newOpenAIExtraConcurrencyRoutesHarness(t, extraConcurrencySettingRepository{
		waitTimeoutSeconds: 1,
		reservePercent:     100,
		minReservedSlots:   1,
	}, upstream, nil)
	blocker, err := harness.store.TryAcquireUserLease(t.Context(), service.UserLeaseRequest{
		RequestID:     "openai-standard-blocker",
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
		_ = harness.store.ReleaseUserLease(context.Background(), harness.userID, "openai-standard-blocker")
	})

	recorder := harness.responsesRequest("timeout")

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Contains(t, recorder.Body.String(), "EXTRA_CONCURRENCY_UNAVAILABLE")
	require.Zero(t, upstream.calls.Load())
}

func TestOpenAIChatCompletionsExtraConcurrencyDispatchesSecondConcurrentRequest(t *testing.T) {
	releaseUpstream := make(chan struct{})
	upstream := &blockingOpenAIExtraConcurrencyUpstream{
		arrivals: make(chan int64, 2),
		release:  releaseUpstream,
	}
	harness := newOpenAIExtraConcurrencyRoutesHarness(
		t,
		extraConcurrencySettingRepository{},
		upstream,
		map[string]any{
			"openai_passthrough":    true,
			"openai_responses_mode": "force_chat_completions",
		},
	)
	responses := make(chan *httptest.ResponseRecorder, 2)
	go func() { responses <- harness.chatCompletionsRequest("first-chat") }()
	firstAccountID := <-upstream.arrivals
	go func() { responses <- harness.chatCompletionsRequest("second-chat") }()

	secondReachedBeforeRelease := false
	select {
	case secondAccountID := <-upstream.arrivals:
		secondReachedBeforeRelease = true
		require.NotEqual(t, firstAccountID, secondAccountID)
	case <-time.After(750 * time.Millisecond):
	}
	close(releaseUpstream)
	firstResponse := <-responses
	secondResponse := <-responses

	require.True(t, secondReachedBeforeRelease)
	require.Equal(t, http.StatusOK, firstResponse.Code)
	require.Equal(t, http.StatusOK, secondResponse.Code)
	require.Equal(t, int32(2), upstream.calls.Load())
}

func TestOpenAIChatCompletionsExtraConcurrencyTimeoutUsesRealRedisWithoutUpstream(t *testing.T) {
	upstream := &extraConcurrencyUpstream{}
	harness := newOpenAIExtraConcurrencyRoutesHarness(t, extraConcurrencySettingRepository{
		waitTimeoutSeconds: 1,
		reservePercent:     100,
		minReservedSlots:   1,
	}, upstream, map[string]any{
		"openai_passthrough":    true,
		"openai_responses_mode": "force_chat_completions",
	})
	blocker, err := harness.store.TryAcquireUserLease(t.Context(), service.UserLeaseRequest{
		RequestID:     "openai-chat-standard-blocker",
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
		_ = harness.store.ReleaseUserLease(context.Background(), harness.userID, "openai-chat-standard-blocker")
	})

	recorder := harness.chatCompletionsRequest("chat-timeout")

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Contains(t, recorder.Body.String(), "EXTRA_CONCURRENCY_UNAVAILABLE")
	require.Zero(t, upstream.calls.Load())
}

func TestOpenAIResponsesFeatureDisabledKeepsLegacyStandardLimit(t *testing.T) {
	releaseUpstream := make(chan struct{})
	upstream := &blockingOpenAIExtraConcurrencyUpstream{
		arrivals: make(chan int64, 2),
		release:  releaseUpstream,
	}
	harness := newOpenAIExtraConcurrencyRoutesHarness(t, extraConcurrencySettingRepository{disabled: true}, upstream, nil)
	responses := make(chan *httptest.ResponseRecorder, 2)
	go func() { responses <- harness.responsesRequest("feature-off-first") }()
	<-upstream.arrivals
	go func() { responses <- harness.responsesRequest("feature-off-second") }()

	secondReachedBeforeRelease := false
	select {
	case <-upstream.arrivals:
		secondReachedBeforeRelease = true
	case <-time.After(750 * time.Millisecond):
	}
	close(releaseUpstream)
	firstResponse := <-responses
	secondResponse := <-responses

	require.False(t, secondReachedBeforeRelease)
	require.Equal(t, http.StatusOK, firstResponse.Code)
	require.Equal(t, http.StatusOK, secondResponse.Code)
	require.Equal(t, int32(2), upstream.calls.Load())
}

func TestOpenAIChatCompletionsFeatureDisabledKeepsLegacyStandardLimit(t *testing.T) {
	releaseUpstream := make(chan struct{})
	upstream := &blockingOpenAIExtraConcurrencyUpstream{
		arrivals: make(chan int64, 2),
		release:  releaseUpstream,
	}
	harness := newOpenAIExtraConcurrencyRoutesHarness(t, extraConcurrencySettingRepository{disabled: true}, upstream, map[string]any{
		"openai_passthrough":    true,
		"openai_responses_mode": "force_chat_completions",
	})
	responses := make(chan *httptest.ResponseRecorder, 2)
	go func() { responses <- harness.chatCompletionsRequest("feature-off-chat-first") }()
	<-upstream.arrivals
	go func() { responses <- harness.chatCompletionsRequest("feature-off-chat-second") }()

	secondReachedBeforeRelease := false
	select {
	case <-upstream.arrivals:
		secondReachedBeforeRelease = true
	case <-time.After(750 * time.Millisecond):
	}
	close(releaseUpstream)
	firstResponse := <-responses
	secondResponse := <-responses

	require.False(t, secondReachedBeforeRelease)
	require.Equal(t, http.StatusOK, firstResponse.Code)
	require.Equal(t, http.StatusOK, secondResponse.Code)
	require.Equal(t, int32(2), upstream.calls.Load())
}
