//go:build unit

package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type extraConcurrencySettingRepository struct {
	waitTimeoutSeconds string
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
	if waitTimeoutSeconds == "" {
		waitTimeoutSeconds = "1"
	}
	return map[string]string{
		service.SettingKeyExtraConcurrencyEnabled:            "true",
		service.SettingKeyExtraConcurrencyWaitTimeoutSeconds: waitTimeoutSeconds,
		service.SettingKeyExtraConcurrencyReservePercent:     "0",
		service.SettingKeyExtraConcurrencyMinReservedSlots:   "0",
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

type expiringTargetAdmissionStore struct{}

func (expiringTargetAdmissionStore) TryAcquireUserLease(context.Context, service.UserLeaseRequest) (service.UserLeaseResult, error) {
	return service.UserLeaseResult{Acquired: true, Class: service.AdmissionClassExtra}, nil
}
func (expiringTargetAdmissionStore) RenewUserLease(context.Context, int64, string, service.AdmissionClass) (bool, error) {
	return true, nil
}
func (expiringTargetAdmissionStore) ReleaseUserLease(context.Context, int64, string) error {
	return nil
}
func (expiringTargetAdmissionStore) TryAcquireTargetLease(context.Context, service.TargetLeaseRequest) (service.TargetLeaseResult, error) {
	return service.TargetLeaseResult{Expired: true}, nil
}
func (expiringTargetAdmissionStore) RenewTargetLease(context.Context, string, int64, string) (bool, error) {
	return true, nil
}
func (expiringTargetAdmissionStore) ReleaseTargetLease(context.Context, string, int64, string) error {
	return nil
}

type waitThenAcquireAdmissionStore struct {
	targetAttempts atomic.Int32
}

func (s *waitThenAcquireAdmissionStore) TryAcquireUserLease(context.Context, service.UserLeaseRequest) (service.UserLeaseResult, error) {
	return service.UserLeaseResult{Acquired: true, Class: service.AdmissionClassExtra}, nil
}
func (s *waitThenAcquireAdmissionStore) RenewUserLease(context.Context, int64, string, service.AdmissionClass) (bool, error) {
	return true, nil
}
func (s *waitThenAcquireAdmissionStore) ReleaseUserLease(context.Context, int64, string) error {
	return nil
}
func (s *waitThenAcquireAdmissionStore) TryAcquireTargetLease(context.Context, service.TargetLeaseRequest) (service.TargetLeaseResult, error) {
	return service.TargetLeaseResult{Acquired: s.targetAttempts.Add(1) > 1}, nil
}
func (s *waitThenAcquireAdmissionStore) RenewTargetLease(context.Context, string, int64, string) (bool, error) {
	return true, nil
}
func (s *waitThenAcquireAdmissionStore) ReleaseTargetLease(context.Context, string, int64, string) error {
	return nil
}

type changingBalanceCache struct {
	service.BillingCache
	reads atomic.Int32
}

type fixedBalanceCache struct {
	service.BillingCache
}

func (fixedBalanceCache) GetUserBalance(context.Context, int64) (float64, error) {
	return 100, nil
}

type countingUserRPMCache struct {
	count          atomic.Int32
	incrementCalls atomic.Int32
}

type exhaustedAPIKeyRepo struct {
	service.APIKeyRepository
	apiKey *service.APIKey
	reads  atomic.Int32
}

func (r *exhaustedAPIKeyRepo) GetByID(context.Context, int64) (*service.APIKey, error) {
	r.reads.Add(1)
	return r.apiKey, nil
}

func (c *countingUserRPMCache) IncrementUserGroupRPM(context.Context, int64, int64) (int, error) {
	return 0, nil
}

func (c *countingUserRPMCache) IncrementUserRPM(context.Context, int64) (int, error) {
	c.incrementCalls.Add(1)
	return int(c.count.Add(1)), nil
}

func (c *countingUserRPMCache) GetUserGroupRPM(context.Context, int64, int64) (int, error) {
	return 0, nil
}

func (c *countingUserRPMCache) GetUserRPM(context.Context, int64) (int, error) {
	return int(c.count.Load()), nil
}

type reusableAdmissionStore struct {
	mu              sync.Mutex
	userRequestID   string
	targetRequestID string
}

type websocketExtraAdmissionStore struct {
	userClaims     atomic.Int32
	targetClaims   atomic.Int32
	userReleases   atomic.Int32
	targetReleases atomic.Int32
}

type blockingFollowUpAdmissionStore struct {
	userClaims     atomic.Int32
	targetClaims   atomic.Int32
	userReleases   atomic.Int32
	targetReleases atomic.Int32

	secondTargetStarted chan struct{}
	secondTargetOnce    sync.Once
}

func (s *websocketExtraAdmissionStore) TryAcquireUserLease(context.Context, service.UserLeaseRequest) (service.UserLeaseResult, error) {
	s.userClaims.Add(1)
	return service.UserLeaseResult{Acquired: true, Class: service.AdmissionClassExtra}, nil
}
func (s *websocketExtraAdmissionStore) RenewUserLease(context.Context, int64, string, service.AdmissionClass) (bool, error) {
	return true, nil
}
func (s *websocketExtraAdmissionStore) ReleaseUserLease(context.Context, int64, string) error {
	s.userReleases.Add(1)
	return nil
}
func (s *websocketExtraAdmissionStore) TryAcquireTargetLease(context.Context, service.TargetLeaseRequest) (service.TargetLeaseResult, error) {
	s.targetClaims.Add(1)
	return service.TargetLeaseResult{Acquired: true}, nil
}
func (s *websocketExtraAdmissionStore) RenewTargetLease(context.Context, string, int64, string) (bool, error) {
	return true, nil
}
func (s *websocketExtraAdmissionStore) ReleaseTargetLease(context.Context, string, int64, string) error {
	s.targetReleases.Add(1)
	return nil
}

func (s *blockingFollowUpAdmissionStore) TryAcquireUserLease(context.Context, service.UserLeaseRequest) (service.UserLeaseResult, error) {
	s.userClaims.Add(1)
	return service.UserLeaseResult{Acquired: true, Class: service.AdmissionClassExtra}, nil
}
func (s *blockingFollowUpAdmissionStore) RenewUserLease(context.Context, int64, string, service.AdmissionClass) (bool, error) {
	return true, nil
}
func (s *blockingFollowUpAdmissionStore) ReleaseUserLease(context.Context, int64, string) error {
	s.userReleases.Add(1)
	return nil
}
func (s *blockingFollowUpAdmissionStore) TryAcquireTargetLease(context.Context, service.TargetLeaseRequest) (service.TargetLeaseResult, error) {
	if s.targetClaims.Add(1) == 1 {
		return service.TargetLeaseResult{Acquired: true}, nil
	}
	s.secondTargetOnce.Do(func() {
		close(s.secondTargetStarted)
	})
	return service.TargetLeaseResult{}, nil
}
func (s *blockingFollowUpAdmissionStore) RenewTargetLease(context.Context, string, int64, string) (bool, error) {
	return true, nil
}
func (s *blockingFollowUpAdmissionStore) ReleaseTargetLease(context.Context, string, int64, string) error {
	s.targetReleases.Add(1)
	return nil
}

func (s *reusableAdmissionStore) TryAcquireUserLease(_ context.Context, request service.UserLeaseRequest) (service.UserLeaseResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.userRequestID == "" {
		s.userRequestID = request.RequestID
	}
	return service.UserLeaseResult{
		Acquired: s.userRequestID == request.RequestID,
		Class:    service.AdmissionClassStandard,
	}, nil
}
func (s *reusableAdmissionStore) RenewUserLease(_ context.Context, _ int64, requestID string, _ service.AdmissionClass) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userRequestID == requestID, nil
}
func (s *reusableAdmissionStore) ReleaseUserLease(_ context.Context, _ int64, requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.userRequestID == requestID {
		s.userRequestID = ""
	}
	return nil
}
func (s *reusableAdmissionStore) TryAcquireTargetLease(_ context.Context, request service.TargetLeaseRequest) (service.TargetLeaseResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.targetRequestID == "" {
		s.targetRequestID = request.RequestID
	}
	return service.TargetLeaseResult{Acquired: s.targetRequestID == request.RequestID}, nil
}
func (s *reusableAdmissionStore) RenewTargetLease(_ context.Context, _ string, _ int64, requestID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.targetRequestID == requestID, nil
}
func (s *reusableAdmissionStore) ReleaseTargetLease(_ context.Context, _ string, _ int64, requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.targetRequestID == requestID {
		s.targetRequestID = ""
	}
	return nil
}

type successfulAnthropicUpstream struct {
	calls atomic.Int32
}

func (u *successfulAnthropicUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"msg_test","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
		)),
	}, nil
}
func (u *successfulAnthropicUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func newSuccessfulExtraConcurrencyHandler(
	t *testing.T,
	group *service.Group,
	account *service.Account,
	upstream service.HTTPUpstream,
) (*GatewayHandler, *helperConcurrencyCacheStub, func()) {
	t.Helper()
	schedulerCache := &fakeSchedulerCache{accounts: []*service.Account{account}}
	schedulerSnapshot := service.NewSchedulerSnapshotService(schedulerCache, nil, nil, nil, nil)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	billingCacheService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	gatewayService := service.NewGatewayService(
		nil,
		&fakeGroupRepo{group: group},
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
	pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
		WorkerCount:      1,
		QueueSize:        1,
		AutoScaleEnabled: false,
	})
	pool.Stop()
	concurrencyCache := &helperConcurrencyCacheStub{}
	h := &GatewayHandler{
		gatewayService:        gatewayService,
		billingCacheService:   billingCacheService,
		concurrencyHelper:     NewConcurrencyHelper(service.NewConcurrencyService(concurrencyCache), SSEPingFormatClaude, 0),
		usageRecordWorkerPool: pool,
		maxAccountSwitches:    1,
		cfg:                   cfg,
	}
	return h, concurrencyCache, billingCacheService.Stop
}

func (c *changingBalanceCache) GetUserBalance(context.Context, int64) (float64, error) {
	if c.reads.Add(1) == 1 {
		return 100, nil
	}
	return 0, nil
}

type fixedAdmissionCapacity struct {
	accountID int64
}

func (c fixedAdmissionCapacity) AdmissionCapacity(context.Context, string) (service.AdmissionCapacitySnapshot, error) {
	return service.AdmissionCapacitySnapshot{
		TotalConcurrency:   1,
		AccountConcurrency: map[int64]int{c.accountID: 1},
	}, nil
}

func TestOpenAIResponsesWebSocketUsesExtraAdmissionInsteadOfLegacyConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		require.NoError(t, err)
		defer func() { _ = conn.CloseNow() }()

		for turn := 1; turn <= 2; turn++ {
			readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
			_, _, err = conn.Read(readCtx)
			cancelRead()
			require.NoError(t, err)

			writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
			response := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_extra_ws_%d","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`, turn)
			err = conn.Write(writeCtx, coderws.MessageText, []byte(response))
			cancelWrite()
			require.NoError(t, err)
		}
		_ = conn.Close(coderws.StatusNormalClosure, "done")
	}))
	defer upstream.Close()

	groupID := int64(5101)
	accountID := int64(6101)
	account := service.Account{
		ID:          accountID,
		Name:        "openai-extra-ws",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-ws-extra", "base_url": upstream.URL},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
			"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
		},
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3

	accountRepo := &openAIWSUsageHandlerAccountRepoStub{account: account}
	billingCacheService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	defer billingCacheService.Stop()
	gatewayService := service.NewOpenAIGatewayService(
		accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCacheService, nil,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	store := &websocketExtraAdmissionStore{}
	legacyCalls := atomic.Int32{}
	legacyCache := &concurrencyCacheMock{
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			legacyCalls.Add(1)
			return false, errors.New("legacy user admission used")
		},
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
			legacyCalls.Add(1)
			return false, errors.New("legacy target admission used")
		},
	}
	h := &OpenAIGatewayHandler{
		gatewayService:      gatewayService,
		billingCacheService: billingCacheService,
		apiKeyService:       &service.APIKeyService{},
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(legacyCache), SSEPingFormatNone, time.Second),
		gatewayAdmission:    service.NewGatewayAdmission(store, nil, fixedAdmissionCapacity{accountID: accountID}),
		maxAccountSwitches:  1,
		cfg:                 cfg,
		settingService:      service.NewSettingService(extraConcurrencySettingRepository{}, cfg),
	}
	apiKey := &service.APIKey{
		ID:      7101,
		UserID:  8101,
		GroupID: &groupID,
		User: &service.User{
			ID:               8101,
			Status:           service.StatusActive,
			Concurrency:      1,
			ExtraConcurrency: 1,
			Balance:          100,
		},
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{
			UserID:           apiKey.UserID,
			Concurrency:      1,
			ExtraConcurrency: 1,
		})
		c.Next()
	})
	router.GET("/openai/v1/responses", h.ResponsesWebSocket)
	handlerServer := httptest.NewServer(router)
	defer handlerServer.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(handlerServer.URL, "http")+"/openai/v1/responses", nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	for turn := 1; turn <= 2; turn++ {
		writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
		err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false}`))
		cancelWrite()
		require.NoError(t, err)

		readCtx, cancelRead := context.WithTimeout(context.Background(), 5*time.Second)
		_, event, readErr := clientConn.Read(readCtx)
		cancelRead()
		require.NoError(t, readErr)
		require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
		require.Equal(t, fmt.Sprintf("resp_extra_ws_%d", turn), gjson.GetBytes(event, "response.id").String())
	}
	require.Zero(t, legacyCalls.Load())
	require.Equal(t, int32(2), store.userClaims.Load())
	require.Equal(t, int32(2), store.targetClaims.Load())
	require.Eventually(t, func() bool {
		return store.userReleases.Load() == 2 && store.targetReleases.Load() == 2
	}, time.Second, 10*time.Millisecond)
}

func TestOpenAIResponsesWebSocketCancelsBlockedFollowUpAdmissionWhenUpstreamCloses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	secondTargetStarted := make(chan struct{})
	upstreamClosed := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		require.NoError(t, err)
		defer func() { _ = conn.CloseNow() }()

		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		_, _, err = conn.Read(readCtx)
		cancelRead()
		require.NoError(t, err)

		writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
		err = conn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_extra_ws_cancel_1","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`))
		cancelWrite()
		require.NoError(t, err)

		select {
		case <-secondTargetStarted:
		case <-r.Context().Done():
			return
		}
		_ = conn.CloseNow()
		close(upstreamClosed)
	}))
	defer upstream.Close()

	groupID := int64(5102)
	accountID := int64(6102)
	account := service.Account{
		ID:          accountID,
		Name:        "openai-extra-ws-cancel",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-ws-extra-cancel", "base_url": upstream.URL},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_enabled": true,
			"openai_apikey_responses_websockets_v2_mode":    service.OpenAIWSIngressModePassthrough,
		},
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3

	accountRepo := &openAIWSUsageHandlerAccountRepoStub{account: account}
	billingCacheService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	defer billingCacheService.Stop()
	gatewayService := service.NewOpenAIGatewayService(
		accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billingCacheService, nil,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	store := &blockingFollowUpAdmissionStore{secondTargetStarted: secondTargetStarted}
	h := &OpenAIGatewayHandler{
		gatewayService:      gatewayService,
		billingCacheService: billingCacheService,
		apiKeyService:       &service.APIKeyService{},
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(&concurrencyCacheMock{}), SSEPingFormatNone, time.Second),
		gatewayAdmission:    service.NewGatewayAdmission(store, nil, fixedAdmissionCapacity{accountID: accountID}),
		maxAccountSwitches:  1,
		cfg:                 cfg,
		settingService: service.NewSettingService(
			extraConcurrencySettingRepository{waitTimeoutSeconds: "5"},
			cfg,
		),
	}
	apiKey := &service.APIKey{
		ID:      7102,
		UserID:  8102,
		GroupID: &groupID,
		User: &service.User{
			ID:               8102,
			Status:           service.StatusActive,
			Concurrency:      1,
			ExtraConcurrency: 1,
			Balance:          100,
		},
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
	}
	handlerCtx, cancelHandler := context.WithCancel(context.Background())
	handlerDone := make(chan struct{})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(handlerCtx)
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{
			UserID:           apiKey.UserID,
			Concurrency:      1,
			ExtraConcurrency: 1,
		})
		c.Next()
	})
	router.GET("/openai/v1/responses", func(c *gin.Context) {
		h.ResponsesWebSocket(c)
		close(handlerDone)
	})
	handlerServer := httptest.NewServer(router)
	defer handlerServer.Close()
	defer cancelHandler()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(handlerServer.URL, "http")+"/openai/v1/responses", nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false}`))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, readErr := clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, readErr)
	require.Equal(t, "resp_extra_ws_cancel_1", gjson.GetBytes(event, "response.id").String())

	writeCtx, cancelWrite = context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false,"previous_response_id":"resp_extra_ws_cancel_1"}`))
	cancelWrite()
	require.NoError(t, err)

	select {
	case <-upstreamClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not close while follow-up admission was blocked")
	}
	require.Eventually(t, func() bool {
		return store.userReleases.Load() == 2 && store.targetReleases.Load() == 2
	}, 500*time.Millisecond, 10*time.Millisecond, "follow-up admission state was not released promptly")
	_ = clientConn.CloseNow()
	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		cancelHandler()
		t.Fatal("websocket handler did not return promptly after upstream closed")
	}
	require.GreaterOrEqual(t, store.userClaims.Load(), int32(2))
	require.GreaterOrEqual(t, store.targetClaims.Load(), int32(2))
}

func TestGatewayHandlerMessagesExtraTargetTimeoutReturnsDistinct429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(2101)
	accountID := int64(1101)
	group := &service.Group{
		ID:       groupID,
		Hydrated: true,
		Platform: service.PlatformAnthropic,
		Status:   service.StatusActive,
	}
	account := &service.Account{
		ID:          accountID,
		Name:        "anthropic-timeout",
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeOAuth,
		Concurrency: 1,
		Priority:    1,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":              "test-token",
			"intercept_warmup_requests": true,
		},
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
	}

	h, cleanup := newTestGatewayHandler(t, group, []*service.Account{account})
	defer cleanup()
	h.settingService = service.NewSettingService(extraConcurrencySettingRepository{}, &config.Config{})
	h.gatewayAdmission = service.NewGatewayAdmission(
		expiringTargetAdmissionStore{},
		h.gatewayService,
		fixedAdmissionCapacity{accountID: accountID},
	)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"max_tokens":256,
		"messages":[{"role":"user","content":[{"type":"text","text":"Warmup"}]}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
	c.Request = req
	apiKey := &service.APIKey{
		ID:      3101,
		UserID:  4101,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User: &service.User{
			ID:               4101,
			Concurrency:      1,
			ExtraConcurrency: 1,
			Balance:          100,
		},
		Group: group,
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{
		UserID:           apiKey.UserID,
		Concurrency:      1,
		ExtraConcurrency: 1,
	})

	h.Messages(c)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "EXTRA_CONCURRENCY_UNAVAILABLE", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
}

func TestGatewayHandlerMessagesWarmupInterceptReleasesExtraAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(2104)
	accountID := int64(1104)
	group := &service.Group{
		ID:       groupID,
		Hydrated: true,
		Platform: service.PlatformAnthropic,
		Status:   service.StatusActive,
	}
	account := &service.Account{
		ID:          accountID,
		Name:        "anthropic-warmup",
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeAPIKey,
		Concurrency: 1,
		Priority:    1,
		Status:      service.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"api_key":                   "upstream-key",
			"base_url":                  "https://api.anthropic.com",
			"intercept_warmup_requests": true,
		},
		Extra:         map[string]any{"anthropic_passthrough": true},
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
	}
	upstream := &successfulAnthropicUpstream{}
	h, concurrencyCache, cleanup := newSuccessfulExtraConcurrencyHandler(t, group, account, upstream)
	defer cleanup()
	h.settingService = service.NewSettingService(extraConcurrencySettingRepository{}, &config.Config{})
	h.gatewayAdmission = service.NewGatewayAdmission(
		&reusableAdmissionStore{},
		h.gatewayService,
		fixedAdmissionCapacity{accountID: accountID},
	)
	apiKey := &service.APIKey{
		ID:      3104,
		UserID:  4104,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User: &service.User{
			ID:               4104,
			Concurrency:      1,
			ExtraConcurrency: 1,
			Balance:          100,
		},
		Group: group,
	}

	for range 2 {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		body := []byte(`{
			"model":"claude-sonnet-4-5",
			"max_tokens":256,
			"messages":[{"role":"user","content":[{"type":"text","text":"Warmup"}]}]
		}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
		c.Request = req
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{
			UserID:           apiKey.UserID,
			Concurrency:      1,
			ExtraConcurrency: 1,
		})

		h.Messages(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, "msg_mock_warmup", gjson.GetBytes(recorder.Body.Bytes(), "id").String())
	}
	require.Zero(t, upstream.calls.Load())
	require.Equal(t, 2, concurrencyCache.apiKeyTrackCalls)
	require.Equal(t, 2, concurrencyCache.apiKeyReleaseCalls)
}

func TestGatewayHandlerMessagesWaitedExtraRequestRechecksBillingBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(2102)
	accountID := int64(1102)
	group := &service.Group{
		ID:       groupID,
		Hydrated: true,
		Platform: service.PlatformAnthropic,
		Status:   service.StatusActive,
	}
	account := &service.Account{
		ID:            accountID,
		Name:          "anthropic-recheck",
		Platform:      service.PlatformAnthropic,
		Type:          service.AccountTypeOAuth,
		Concurrency:   1,
		Priority:      1,
		Status:        service.StatusActive,
		Schedulable:   true,
		Credentials:   map[string]any{"access_token": "test-token"},
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
	}

	h, cleanup := newTestGatewayHandler(t, group, []*service.Account{account})
	defer cleanup()
	h.settingService = service.NewSettingService(extraConcurrencySettingRepository{}, &config.Config{})
	store := &waitThenAcquireAdmissionStore{}
	h.gatewayAdmission = service.NewGatewayAdmission(
		store,
		h.gatewayService,
		fixedAdmissionCapacity{accountID: accountID},
	)
	balanceCache := &changingBalanceCache{}
	billingCacheService := service.NewBillingCacheService(balanceCache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	defer billingCacheService.Stop()
	h.billingCacheService = billingCacheService

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":256,"messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
	c.Request = req
	apiKey := &service.APIKey{
		ID:      3102,
		UserID:  4102,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User: &service.User{
			ID:               4102,
			Concurrency:      1,
			ExtraConcurrency: 1,
			Balance:          100,
		},
		Group: group,
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{
		UserID:           apiKey.UserID,
		Concurrency:      1,
		ExtraConcurrency: 1,
	})

	h.Messages(c)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, "billing_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
	require.Equal(t, int32(2), balanceCache.reads.Load())
}

func TestGatewayHandlerMessagesWaitedExtraRequestCountsRPMOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(2105)
	accountID := int64(1105)
	group := &service.Group{
		ID:       groupID,
		Hydrated: true,
		Platform: service.PlatformAnthropic,
		Status:   service.StatusActive,
	}
	account := &service.Account{
		ID:            accountID,
		Name:          "anthropic-rpm-recheck",
		Platform:      service.PlatformAnthropic,
		Type:          service.AccountTypeAPIKey,
		Concurrency:   1,
		Priority:      1,
		Status:        service.StatusActive,
		Schedulable:   true,
		Credentials:   map[string]any{"api_key": "upstream-key", "base_url": "https://api.anthropic.com"},
		Extra:         map[string]any{"anthropic_passthrough": true},
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
	}
	upstream := &successfulAnthropicUpstream{}
	h, _, cleanup := newSuccessfulExtraConcurrencyHandler(t, group, account, upstream)
	defer cleanup()
	h.settingService = service.NewSettingService(extraConcurrencySettingRepository{}, &config.Config{})
	h.gatewayAdmission = service.NewGatewayAdmission(
		&waitThenAcquireAdmissionStore{},
		h.gatewayService,
		fixedAdmissionCapacity{accountID: accountID},
	)
	rpmCache := &countingUserRPMCache{}
	billingCacheService := service.NewBillingCacheService(
		fixedBalanceCache{},
		nil,
		nil,
		nil,
		rpmCache,
		nil,
		&config.Config{RunMode: config.RunModeStandard},
		nil,
	)
	defer billingCacheService.Stop()
	h.billingCacheService = billingCacheService

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":256,"messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
	c.Request = req
	apiKey := &service.APIKey{
		ID:      3105,
		UserID:  4105,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User: &service.User{
			ID:               4105,
			Concurrency:      1,
			ExtraConcurrency: 1,
			Balance:          100,
			RPMLimit:         1,
		},
		Group: group,
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{
		UserID:           apiKey.UserID,
		Concurrency:      1,
		ExtraConcurrency: 1,
	})

	h.Messages(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "msg_test", gjson.GetBytes(recorder.Body.Bytes(), "id").String())
	require.Equal(t, int32(1), upstream.calls.Load())
	require.Equal(t, int32(1), rpmCache.incrementCalls.Load())
}

func TestGatewayHandlerMessagesWaitedExtraRequestRechecksAPIKeyQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(2106)
	accountID := int64(1106)
	group := &service.Group{
		ID:       groupID,
		Hydrated: true,
		Platform: service.PlatformAnthropic,
		Status:   service.StatusActive,
	}
	account := &service.Account{
		ID:            accountID,
		Name:          "anthropic-api-key-recheck",
		Platform:      service.PlatformAnthropic,
		Type:          service.AccountTypeAPIKey,
		Concurrency:   1,
		Priority:      1,
		Status:        service.StatusActive,
		Schedulable:   true,
		Credentials:   map[string]any{"api_key": "upstream-key", "base_url": "https://api.anthropic.com"},
		Extra:         map[string]any{"anthropic_passthrough": true},
		AccountGroups: []service.AccountGroup{{AccountID: accountID, GroupID: groupID}},
	}
	upstream := &successfulAnthropicUpstream{}
	h, _, cleanup := newSuccessfulExtraConcurrencyHandler(t, group, account, upstream)
	defer cleanup()
	h.settingService = service.NewSettingService(extraConcurrencySettingRepository{}, &config.Config{})
	h.gatewayAdmission = service.NewGatewayAdmission(
		&waitThenAcquireAdmissionStore{},
		h.gatewayService,
		fixedAdmissionCapacity{accountID: accountID},
	)
	repo := &exhaustedAPIKeyRepo{apiKey: &service.APIKey{
		ID:        3106,
		Status:    service.StatusAPIKeyActive,
		Quota:     1,
		QuotaUsed: 1,
	}}
	billingCacheService := service.NewBillingCacheService(
		fixedBalanceCache{},
		nil,
		nil,
		repo,
		nil,
		nil,
		&config.Config{RunMode: config.RunModeStandard},
		nil,
	)
	defer billingCacheService.Stop()
	h.billingCacheService = billingCacheService

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":256,"messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
	c.Request = req
	apiKey := &service.APIKey{
		ID:      3106,
		UserID:  4106,
		GroupID: &groupID,
		Status:  service.StatusAPIKeyActive,
		Quota:   1,
		User: &service.User{
			ID:               4106,
			Concurrency:      1,
			ExtraConcurrency: 1,
			Balance:          100,
		},
		Group: group,
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{
		UserID:           apiKey.UserID,
		Concurrency:      1,
		ExtraConcurrency: 1,
	})

	h.Messages(c)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, "billing_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
	require.Zero(t, upstream.calls.Load())
	require.Equal(t, int32(1), repo.reads.Load())
}

func TestGatewayHandlerMessagesSuccessfulExtraRequestsReleaseAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(2103)
	accountID := int64(1103)
	group := &service.Group{
		ID:       groupID,
		Hydrated: true,
		Platform: service.PlatformAnthropic,
		Status:   service.StatusActive,
	}
	account := &service.Account{
		ID:          accountID,
		Name:        "anthropic-success",
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
	upstream := &successfulAnthropicUpstream{}
	h, concurrencyCache, cleanup := newSuccessfulExtraConcurrencyHandler(t, group, account, upstream)
	defer cleanup()
	h.settingService = service.NewSettingService(extraConcurrencySettingRepository{}, &config.Config{})
	store := &reusableAdmissionStore{}
	h.gatewayAdmission = service.NewGatewayAdmission(
		store,
		h.gatewayService,
		fixedAdmissionCapacity{accountID: accountID},
	)
	apiKey := &service.APIKey{
		ID:      3103,
		UserID:  4103,
		GroupID: &groupID,
		Status:  service.StatusActive,
		User: &service.User{
			ID:               4103,
			Concurrency:      1,
			ExtraConcurrency: 1,
			Balance:          100,
		},
		Group: group,
	}

	for range 2 {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":256,"messages":[{"role":"user","content":"hello"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), ctxkey.Group, group))
		c.Request = req
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{
			UserID:           apiKey.UserID,
			Concurrency:      1,
			ExtraConcurrency: 1,
		})

		h.Messages(c)

		require.Equal(t, http.StatusOK, recorder.Code)
		require.Equal(t, "msg_test", gjson.GetBytes(recorder.Body.Bytes(), "id").String())
	}
	require.Equal(t, int32(2), upstream.calls.Load())
	require.Equal(t, 2, concurrencyCache.apiKeyTrackCalls)
	require.Equal(t, 2, concurrencyCache.apiKeyReleaseCalls)
}
