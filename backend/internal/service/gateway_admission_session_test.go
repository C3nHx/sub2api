//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type gatewayAdmissionSessionStoreStub struct {
	GatewayAdmissionStore
	userRequest        UserLeaseRequest
	userResult         UserLeaseResult
	userResults        []UserLeaseResult
	targetRequest      TargetLeaseRequest
	targetResult       TargetLeaseResult
	targetBlocked      bool
	targetAttempted    chan struct{}
	targetAcquireReady chan struct{}
	targetAcquireStart chan struct{}
	userAcquireReady   chan struct{}
	userAcquireStart   chan struct{}
	userAcquireCalls   atomic.Int32
	releaseCalls       atomic.Int32
	targetReleaseCalls atomic.Int32
	renewUserCalls     atomic.Int32
	renewTargetCalls   atomic.Int32
}

func (s *gatewayAdmissionSessionStoreStub) TryAcquireUserLease(_ context.Context, request UserLeaseRequest) (UserLeaseResult, error) {
	call := s.userAcquireCalls.Add(1)
	s.userRequest = request
	if call > 1 && s.userAcquireReady != nil {
		if s.userAcquireStart != nil {
			select {
			case s.userAcquireStart <- struct{}{}:
			default:
			}
		}
		<-s.userAcquireReady
	}
	if index := int(call) - 1; index < len(s.userResults) {
		return s.userResults[index], nil
	}
	return s.userResult, nil
}

func (s *gatewayAdmissionSessionStoreStub) ReleaseUserLease(context.Context, int64, string) error {
	s.releaseCalls.Add(1)
	return nil
}

func (s *gatewayAdmissionSessionStoreStub) RenewUserLease(context.Context, int64, string, AdmissionClass) (bool, error) {
	s.renewUserCalls.Add(1)
	return true, nil
}

func (s *gatewayAdmissionSessionStoreStub) TryAcquireTargetLease(_ context.Context, request TargetLeaseRequest) (TargetLeaseResult, error) {
	s.targetRequest = request
	if s.targetAcquireReady != nil {
		if s.targetAcquireStart != nil {
			select {
			case s.targetAcquireStart <- struct{}{}:
			default:
			}
		}
		<-s.targetAcquireReady
	}
	if s.targetBlocked {
		if s.targetAttempted != nil {
			select {
			case s.targetAttempted <- struct{}{}:
			default:
			}
		}
		return TargetLeaseResult{}, nil
	}
	if s.targetResult != (TargetLeaseResult{}) {
		return s.targetResult, nil
	}
	return TargetLeaseResult{Acquired: true}, nil
}

func (s *gatewayAdmissionSessionStoreStub) ReleaseTargetLease(context.Context, string, int64, string) error {
	s.targetReleaseCalls.Add(1)
	return nil
}

func (s *gatewayAdmissionSessionStoreStub) RenewTargetLease(context.Context, string, int64, string) (bool, error) {
	s.renewTargetCalls.Add(1)
	return true, nil
}

func TestGatewayAdmissionBeginOwnsAndReleasesUserLease(t *testing.T) {
	store := &gatewayAdmissionSessionStoreStub{
		userResult: UserLeaseResult{Acquired: true, Class: AdmissionClassExtra},
	}
	admission := NewGatewayAdmission(store, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())

	session, err := admission.Begin(ctx, GatewayAdmissionRequest{
		UserID:        808,
		StandardLimit: 1,
		ExtraLimit:    2,
		Settings: ExtraConcurrencyRuntimeSettings{
			WaitTimeoutSeconds: 1,
		},
	})

	require.NoError(t, err)
	require.NotNil(t, session)
	require.Equal(t, AdmissionClassExtra, session.Class())
	require.Equal(t, int64(808), store.userRequest.UserID)
	require.Equal(t, 1, store.userRequest.StandardLimit)
	require.Equal(t, 2, store.userRequest.ExtraLimit)
	require.NotEmpty(t, store.userRequest.RequestID)

	cancel()
	require.Eventually(t, func() bool {
		return store.releaseCalls.Load() == 1
	}, time.Second, 10*time.Millisecond)
	session.Close()
	session.Close()
	require.Equal(t, int32(1), store.releaseCalls.Load())
}

func TestGatewayAdmissionBeginReturnsQueueFullWithoutWaiting(t *testing.T) {
	store := &gatewayAdmissionSessionStoreStub{
		userResult: UserLeaseResult{QueueFull: true},
	}
	admission := NewGatewayAdmission(store, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := admission.Begin(ctx, GatewayAdmissionRequest{
		UserID:        809,
		StandardLimit: 1,
		ExtraLimit:    1,
		Settings: ExtraConcurrencyRuntimeSettings{
			WaitTimeoutSeconds: 1,
		},
	})

	var queueFull *GatewayAdmissionQueueFullError
	require.ErrorAs(t, err, &queueFull)
	require.Equal(t, int32(1), store.releaseCalls.Load())
}

func TestGatewayAdmissionBeginStandardOnlyTimeoutUsesStandardError(t *testing.T) {
	store := &gatewayAdmissionSessionStoreStub{}
	admission := NewGatewayAdmission(store, nil, nil)

	_, err := admission.Begin(context.Background(), GatewayAdmissionRequest{
		UserID:        810,
		StandardLimit: 1,
		ExtraLimit:    0,
		Settings: ExtraConcurrencyRuntimeSettings{
			WaitTimeoutSeconds: 1,
		},
	})

	var timeout *GatewayAdmissionTimeoutError
	require.ErrorAs(t, err, &timeout)
	require.Equal(t, "user", timeout.SlotType)
}

func TestGatewayAdmissionSessionNextTargetOwnsAtomicTargetLease(t *testing.T) {
	account := Account{
		ID:          42,
		Platform:    PlatformAnthropic,
		Priority:    1,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 3,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	gatewayService := &GatewayService{
		accountRepo:        repo,
		cache:              &mockGatewayCacheForPlatform{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(&mockConcurrencyCache{}),
	}
	store := &gatewayAdmissionSessionStoreStub{
		userResult: UserLeaseResult{Acquired: true, Class: AdmissionClassStandard},
	}
	admission := NewGatewayAdmission(store, gatewayService, gatewayService)
	session, err := admission.Begin(context.Background(), GatewayAdmissionRequest{
		UserID:        909,
		StandardLimit: 1,
		ExtraLimit:    1,
		Settings: ExtraConcurrencyRuntimeSettings{
			WaitTimeoutSeconds: 1,
		},
	})
	require.NoError(t, err)

	target, err := session.NextTarget(context.Background(), GatewayTargetRequest{
		Model: "claude-3-5-sonnet-20241022",
	})

	require.NoError(t, err)
	require.NotNil(t, target)
	require.NotNil(t, target.Account)
	require.Equal(t, int64(42), target.Account.ID)
	require.Equal(t, AdmissionClassStandard, target.Class)
	require.Equal(t, int64(42), store.targetRequest.AccountID)
	require.Equal(t, 3, store.targetRequest.AccountLimit)

	session.ReleaseTarget()
	session.ReleaseTarget()
	require.Equal(t, int32(1), store.targetReleaseCalls.Load())
	session.Close()
	session.Close()
	require.Equal(t, int32(1), store.releaseCalls.Load())
	require.Equal(t, int32(1), store.targetReleaseCalls.Load())
}

func TestGatewayAdmissionSessionImmediateTargetDoesNotRefreshUserLease(t *testing.T) {
	target, session, store := newAdmittedTargetForDispatchTest(t)
	defer session.Close()

	require.NotNil(t, target)
	require.Equal(t, int32(1), store.userAcquireCalls.Load())
}

func TestGatewayAdmissionSessionPromotesWaitedExtraBeforeImmediateTarget(t *testing.T) {
	account := Account{
		ID:          49,
		Platform:    PlatformAnthropic,
		Priority:    1,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	gatewayService := &GatewayService{
		accountRepo:        repo,
		cache:              &mockGatewayCacheForPlatform{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(&mockConcurrencyCache{}),
	}
	store := &gatewayAdmissionSessionStoreStub{
		userResults: []UserLeaseResult{
			{},
			{Acquired: true, Class: AdmissionClassExtra},
			{Acquired: true, Class: AdmissionClassStandard},
		},
	}
	admission := NewGatewayAdmission(store, gatewayService, gatewayService)
	session, err := admission.Begin(context.Background(), GatewayAdmissionRequest{
		UserID:        916,
		StandardLimit: 1,
		ExtraLimit:    1,
		Settings: ExtraConcurrencyRuntimeSettings{
			WaitTimeoutSeconds: 1,
		},
	})
	require.NoError(t, err)
	defer session.Close()
	require.True(t, session.Waited())
	require.Equal(t, AdmissionClassExtra, session.Class())

	target, err := session.NextTarget(context.Background(), GatewayTargetRequest{
		Model: "claude-3-5-sonnet-20241022",
	})

	require.NoError(t, err)
	require.Equal(t, AdmissionClassStandard, target.Class)
	require.Equal(t, AdmissionClassStandard, store.targetRequest.Class)
	require.Equal(t, int32(3), store.userAcquireCalls.Load())
}

func TestGatewayAdmissionSessionReturnsExtraTimeoutFromTargetStore(t *testing.T) {
	account := Account{
		ID:          43,
		Platform:    PlatformAnthropic,
		Priority:    1,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	gatewayService := &GatewayService{
		accountRepo:        repo,
		cache:              &mockGatewayCacheForPlatform{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(&mockConcurrencyCache{}),
	}
	store := &gatewayAdmissionSessionStoreStub{
		userResult:   UserLeaseResult{Acquired: true, Class: AdmissionClassExtra},
		targetResult: TargetLeaseResult{Expired: true},
	}
	admission := NewGatewayAdmission(store, gatewayService, gatewayService)
	session, err := admission.Begin(context.Background(), GatewayAdmissionRequest{
		UserID:        910,
		StandardLimit: 1,
		ExtraLimit:    1,
		Settings: ExtraConcurrencyRuntimeSettings{
			WaitTimeoutSeconds: 1,
		},
	})
	require.NoError(t, err)
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = session.NextTarget(ctx, GatewayTargetRequest{Model: "claude-3-5-sonnet-20241022"})

	var unavailable *ExtraConcurrencyUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.True(t, unavailable.Timeout)
	require.False(t, errors.Is(err, context.DeadlineExceeded))
}

func TestGatewayAdmissionSessionExtraCapacityFailureStillHonorsWaitTimeout(t *testing.T) {
	account := Account{
		ID:          46,
		Platform:    PlatformAnthropic,
		Priority:    1,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	gatewayService := &GatewayService{
		accountRepo:        repo,
		cache:              &mockGatewayCacheForPlatform{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(&mockConcurrencyCache{}),
	}
	store := &gatewayAdmissionSessionStoreStub{
		userResult: UserLeaseResult{Acquired: true, Class: AdmissionClassExtra},
	}
	admission := NewGatewayAdmission(
		store,
		gatewayService,
		admissionCapacitySourceFunc(func(context.Context, string) (AdmissionCapacitySnapshot, error) {
			return AdmissionCapacitySnapshot{}, errors.New("capacity snapshot unavailable")
		}),
	)
	session, err := admission.Begin(context.Background(), GatewayAdmissionRequest{
		UserID:        913,
		StandardLimit: 1,
		ExtraLimit:    1,
		Settings: ExtraConcurrencyRuntimeSettings{
			WaitTimeoutSeconds: 1,
		},
	})
	require.NoError(t, err)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	_, err = session.NextTarget(ctx, GatewayTargetRequest{Model: "claude-3-5-sonnet-20241022"})

	var unavailable *ExtraConcurrencyUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.False(t, errors.Is(err, context.DeadlineExceeded))
}

func TestGatewayAdmissionSessionCancelWaitingTargetRemovesQueueEntry(t *testing.T) {
	account := Account{
		ID:          45,
		Platform:    PlatformAnthropic,
		Priority:    1,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	gatewayService := &GatewayService{
		accountRepo:        repo,
		cache:              &mockGatewayCacheForPlatform{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(&mockConcurrencyCache{}),
	}
	store := &gatewayAdmissionSessionStoreStub{
		userResult:      UserLeaseResult{Acquired: true, Class: AdmissionClassStandard},
		targetBlocked:   true,
		targetAttempted: make(chan struct{}, 1),
	}
	admission := NewGatewayAdmission(store, gatewayService, gatewayService)
	session, err := admission.Begin(context.Background(), GatewayAdmissionRequest{
		UserID:        912,
		StandardLimit: 1,
		ExtraLimit:    1,
		Settings: ExtraConcurrencyRuntimeSettings{
			WaitTimeoutSeconds: 1,
		},
	})
	require.NoError(t, err)
	defer session.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, nextErr := session.NextTarget(ctx, GatewayTargetRequest{
			Model: "claude-3-5-sonnet-20241022",
		})
		done <- nextErr
	}()
	<-store.targetAttempted
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
	require.Equal(t, int32(1), store.targetReleaseCalls.Load())
}

func TestGatewayAdmissionSessionCloseReleasesTargetAcquiredConcurrently(t *testing.T) {
	account := Account{
		ID:          47,
		Platform:    PlatformAnthropic,
		Priority:    1,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	gatewayService := &GatewayService{
		accountRepo:        repo,
		cache:              &mockGatewayCacheForPlatform{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(&mockConcurrencyCache{}),
	}
	store := &gatewayAdmissionSessionStoreStub{
		userResult:         UserLeaseResult{Acquired: true, Class: AdmissionClassStandard},
		targetAcquireReady: make(chan struct{}),
		targetAcquireStart: make(chan struct{}, 1),
	}
	admission := NewGatewayAdmission(store, gatewayService, gatewayService)
	session, err := admission.Begin(context.Background(), GatewayAdmissionRequest{
		UserID:        914,
		StandardLimit: 1,
		ExtraLimit:    1,
		Settings: ExtraConcurrencyRuntimeSettings{
			WaitTimeoutSeconds: 1,
		},
	})
	require.NoError(t, err)

	targets := make(chan *AdmittedTarget, 1)
	errs := make(chan error, 1)
	go func() {
		target, nextErr := session.NextTarget(context.Background(), GatewayTargetRequest{
			Model: "claude-3-5-sonnet-20241022",
		})
		targets <- target
		errs <- nextErr
	}()
	<-store.targetAcquireStart

	session.Close()
	close(store.targetAcquireReady)

	require.Error(t, <-errs)
	require.Nil(t, <-targets)
	require.Equal(t, int32(1), store.targetReleaseCalls.Load())
	require.Equal(t, int32(1), store.releaseCalls.Load())
}

func TestGatewayAdmissionSessionCloseReleasesUserLeaseRefreshedConcurrently(t *testing.T) {
	account := Account{
		ID:          48,
		Platform:    PlatformAnthropic,
		Priority:    1,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	gatewayService := &GatewayService{
		accountRepo:        repo,
		cache:              &mockGatewayCacheForPlatform{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(&mockConcurrencyCache{}),
	}
	store := &gatewayAdmissionSessionStoreStub{
		userResult:       UserLeaseResult{Acquired: true, Class: AdmissionClassStandard},
		targetBlocked:    true,
		userAcquireReady: make(chan struct{}),
		userAcquireStart: make(chan struct{}, 1),
	}
	admission := NewGatewayAdmission(store, gatewayService, gatewayService)
	session, err := admission.Begin(context.Background(), GatewayAdmissionRequest{
		UserID:        915,
		StandardLimit: 1,
		ExtraLimit:    1,
		Settings: ExtraConcurrencyRuntimeSettings{
			WaitTimeoutSeconds: 1,
		},
	})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, nextErr := session.NextTarget(context.Background(), GatewayTargetRequest{
			Model: "claude-3-5-sonnet-20241022",
		})
		done <- nextErr
	}()
	<-store.userAcquireStart

	session.Close()
	close(store.userAcquireReady)

	require.ErrorIs(t, <-done, context.Canceled)
	require.Equal(t, int32(2), store.releaseCalls.Load())
}

func newAdmittedTargetForDispatchTest(t *testing.T) (*AdmittedTarget, *GatewayAdmissionSession, *gatewayAdmissionSessionStoreStub) {
	t.Helper()

	account := Account{
		ID:          44,
		Platform:    PlatformAnthropic,
		Priority:    1,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
	}
	repo := &mockAccountRepoForPlatform{
		accounts:     []Account{account},
		accountsByID: map[int64]*Account{account.ID: &account},
	}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	gatewayService := &GatewayService{
		accountRepo:        repo,
		cache:              &mockGatewayCacheForPlatform{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(&mockConcurrencyCache{}),
	}
	store := &gatewayAdmissionSessionStoreStub{
		userResult: UserLeaseResult{Acquired: true, Class: AdmissionClassExtra},
	}
	admission := NewGatewayAdmission(store, gatewayService, gatewayService)
	session, err := admission.Begin(context.Background(), GatewayAdmissionRequest{
		UserID:        911,
		StandardLimit: 1,
		ExtraLimit:    1,
		Settings: ExtraConcurrencyRuntimeSettings{
			WaitTimeoutSeconds: 1,
		},
	})
	require.NoError(t, err)
	target, err := session.NextTarget(context.Background(), GatewayTargetRequest{
		Model: "claude-3-5-sonnet-20241022",
	})
	require.NoError(t, err)
	return target, session, store
}

func TestAdmittedTargetDispatchRechecksBeforeUpstreamAndReleases(t *testing.T) {
	target, session, store := newAdmittedTargetForDispatchTest(t)
	defer session.Close()
	session.waited.Store(true)

	eligibilityErr := errors.New("balance changed while waiting")
	recheckCalls := 0
	upstreamCalls := 0
	err := target.Dispatch(
		context.Background(),
		func(context.Context) error {
			recheckCalls++
			return eligibilityErr
		},
		func(context.Context, *Account) error {
			upstreamCalls++
			return nil
		},
	)

	require.ErrorIs(t, err, eligibilityErr)
	require.Equal(t, 1, recheckCalls)
	require.Zero(t, upstreamCalls)
	require.Equal(t, int32(1), store.targetReleaseCalls.Load())
}

func TestAdmittedTargetDispatchCanOnlyRunOnce(t *testing.T) {
	target, session, store := newAdmittedTargetForDispatchTest(t)
	defer session.Close()

	upstreamCalls := 0
	dispatch := func(context.Context, *Account) error {
		upstreamCalls++
		return nil
	}
	require.NoError(t, target.Dispatch(context.Background(), nil, dispatch))

	err := target.Dispatch(context.Background(), nil, dispatch)

	require.Error(t, err)
	require.Equal(t, 1, upstreamCalls)
	require.Equal(t, int32(1), store.targetReleaseCalls.Load())
}

func TestAdmittedTargetDispatchRenewsLeasesWhileUpstreamIsRunning(t *testing.T) {
	target, session, store := newAdmittedTargetForDispatchTest(t)
	defer session.Close()
	session.admission.renewInterval = 10 * time.Millisecond

	upstreamStarted := make(chan struct{})
	finishUpstream := make(chan struct{})
	dispatchDone := make(chan error, 1)
	go func() {
		dispatchDone <- target.Dispatch(
			context.Background(),
			nil,
			func(context.Context, *Account) error {
				close(upstreamStarted)
				<-finishUpstream
				return nil
			},
		)
	}()
	<-upstreamStarted

	require.Eventually(t, func() bool {
		return store.renewUserCalls.Load() > 0 && store.renewTargetCalls.Load() > 0
	}, time.Second, 10*time.Millisecond)
	close(finishUpstream)
	require.NoError(t, <-dispatchDone)
	require.Equal(t, int32(1), store.targetReleaseCalls.Load())
}
