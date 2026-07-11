package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	gatewayAdmissionPollInterval  = 20 * time.Millisecond
	gatewayAdmissionRenewInterval = 5 * time.Minute
	gatewayAdmissionRenewTimeout  = 2 * time.Second
)

type gatewayAdmissionLeaseTTLProvider interface {
	GatewayAdmissionLeaseTTL() time.Duration
}

type GatewayAdmission struct {
	store          GatewayAdmissionStore
	gatewayService *GatewayService
	capacitySource AdmissionCapacitySource
	renewInterval  time.Duration
}

type GatewayAdmissionRequest struct {
	UserID        int64
	StandardLimit int
	ExtraLimit    int
	Settings      ExtraConcurrencyRuntimeSettings
}

type GatewayAdmissionSession struct {
	admission *GatewayAdmission
	store     GatewayAdmissionStore
	request   GatewayAdmissionRequest
	requestID string
	waited    atomic.Bool
	closeOnce sync.Once

	mu            sync.Mutex
	class         AdmissionClass
	unlimited     bool
	closed        bool
	targetRelease func()
}

type GatewayTargetRequest struct {
	GroupID            *int64
	SessionKey         string
	Model              string
	MetadataUserID     string
	ExcludedAccountIDs map[int64]struct{}
	Selector           GatewayTargetSelector
}

type GatewayTargetSelector interface {
	Select(ctx context.Context, claimer TargetClaimer) (*AccountSelectionResult, error)
}

type GatewayTargetSelectorFunc func(context.Context, TargetClaimer) (*AccountSelectionResult, error)

func (f GatewayTargetSelectorFunc) Select(ctx context.Context, claimer TargetClaimer) (*AccountSelectionResult, error) {
	return f(ctx, claimer)
}

type AdmittedTarget struct {
	Account    *Account
	Class      AdmissionClass
	session    *GatewayAdmissionSession
	dispatched atomic.Bool
}

func NewGatewayAdmission(store GatewayAdmissionStore, gatewayService *GatewayService, capacitySource AdmissionCapacitySource) *GatewayAdmission {
	if capacitySource == nil && gatewayService != nil {
		capacitySource = gatewayService
	}
	return &GatewayAdmission{
		store:          store,
		gatewayService: gatewayService,
		capacitySource: capacitySource,
		renewInterval:  gatewayAdmissionRenewIntervalForStore(store),
	}
}

func gatewayAdmissionRenewIntervalForStore(store GatewayAdmissionStore) time.Duration {
	provider, ok := store.(gatewayAdmissionLeaseTTLProvider)
	if !ok {
		return gatewayAdmissionRenewInterval
	}
	interval := provider.GatewayAdmissionLeaseTTL() / 3
	if interval <= 0 {
		return gatewayAdmissionRenewInterval
	}
	return interval
}

func (a *GatewayAdmission) Begin(ctx context.Context, request GatewayAdmissionRequest) (*GatewayAdmissionSession, error) {
	if a == nil || a.store == nil {
		return nil, fmt.Errorf("gateway admission is unavailable")
	}
	if request.UserID <= 0 {
		return nil, fmt.Errorf("invalid gateway admission user")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	requestID := generateRequestID()
	waitTimeout := time.Duration(request.Settings.WaitTimeoutSeconds) * time.Second
	if waitTimeout <= 0 {
		waitTimeout = 30 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()

	waited := false
	for {
		result, err := a.store.TryAcquireUserLease(waitCtx, UserLeaseRequest{
			RequestID:     requestID,
			UserID:        request.UserID,
			StandardLimit: request.StandardLimit,
			ExtraLimit:    request.ExtraLimit,
			MaxWaiting:    gatewayAdmissionMaxWaiting(request),
			WaitTimeout:   waitTimeout,
		})
		if err != nil {
			a.releaseUserState(request.UserID, requestID)
			return nil, err
		}
		if result.QueueFull {
			a.releaseUserState(request.UserID, requestID)
			return nil, &GatewayAdmissionQueueFullError{}
		}
		if result.Acquired {
			session := &GatewayAdmissionSession{
				admission: a,
				store:     a.store,
				request:   request,
				requestID: requestID,
				class:     result.Class,
				unlimited: result.Unlimited,
			}
			session.waited.Store(waited)
			context.AfterFunc(ctx, session.Close)
			return session, nil
		}

		waited = true
		timer := time.NewTimer(gatewayAdmissionPollInterval)
		select {
		case <-waitCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			a.releaseUserState(request.UserID, requestID)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			class := AdmissionClassStandard
			if request.ExtraLimit > 0 {
				class = AdmissionClassExtra
			}
			return nil, gatewayAdmissionWaitTimeoutError(class, "user")
		case <-timer.C:
		}
	}
}

func (a *GatewayAdmission) releaseUserState(userID int64, requestID string) {
	if a == nil || a.store == nil {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = a.store.ReleaseUserLease(releaseCtx, userID, requestID)
}

func (s *GatewayAdmissionSession) Class() AdmissionClass {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.class
}

func (s *GatewayAdmissionSession) Waited() bool {
	return s != nil && s.waited.Load()
}

func (s *GatewayAdmissionSession) NextTarget(ctx context.Context, request GatewayTargetRequest) (*AdmittedTarget, error) {
	if s == nil || s.admission == nil || (request.Selector == nil && s.admission.gatewayService == nil) {
		return nil, fmt.Errorf("gateway admission target selection is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	waitTimeout := time.Duration(s.request.Settings.WaitTimeoutSeconds) * time.Second
	if waitTimeout <= 0 {
		waitTimeout = 30 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, waitTimeout)
	defer cancel()
	s.ReleaseTarget()
	claimer := &gatewayAdmissionTargetClaimer{
		store:          s.store,
		capacitySource: s.admission.capacitySource,
		requestID:      s.requestID,
		settings:       s.request.Settings,
	}
	targetAcquired := false
	defer func() {
		if !targetAcquired {
			claimer.ReleasePending()
		}
	}()
	class := s.Class()
	refreshUser := s.Waited()

	for {
		if s.isClosed() {
			return nil, context.Canceled
		}
		if refreshUser {
			var err error
			class, err = s.refreshUserLease(waitCtx)
			if err != nil {
				if waitCtx.Err() != nil {
					return nil, gatewayAdmissionTargetWaitError(ctx, class)
				}
				return nil, err
			}
		}
		claimer.class = class
		var selection *AccountSelectionResult
		var err error
		if request.Selector != nil {
			selection, err = request.Selector.Select(waitCtx, claimer)
		} else {
			selection, err = s.admission.gatewayService.selectAccountWithTargetClaimer(
				waitCtx,
				request.GroupID,
				request.SessionKey,
				request.Model,
				request.ExcludedAccountIDs,
				request.MetadataUserID,
				s.request.UserID,
				claimer,
			)
		}
		if err != nil && waitCtx.Err() != nil {
			return nil, gatewayAdmissionTargetWaitError(ctx, class)
		}
		if claimErr := claimer.Err(); claimErr != nil {
			return nil, claimErr
		}
		if err != nil {
			return nil, err
		}
		if selection == nil || selection.Account == nil {
			return nil, fmt.Errorf("gateway admission selected no target")
		}
		if selection.Acquired {
			targetAcquired = true
			if !s.setTargetRelease(selection.ReleaseFunc) {
				return nil, context.Canceled
			}
			return &AdmittedTarget{Account: selection.Account, Class: class, session: s}, nil
		}

		s.waited.Store(true)
		timer := time.NewTimer(gatewayAdmissionPollInterval)
		select {
		case <-waitCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, gatewayAdmissionTargetWaitError(ctx, class)
		case <-timer.C:
			refreshUser = true
		}
	}
}

func gatewayAdmissionTargetWaitError(parent context.Context, class AdmissionClass) error {
	if parent != nil {
		if err := parent.Err(); err != nil {
			return err
		}
	}
	return gatewayAdmissionWaitTimeoutError(class, "account")
}

func (t *AdmittedTarget) Dispatch(
	ctx context.Context,
	recheck func(context.Context) error,
	upstream func(context.Context, *Account) error,
) error {
	if t == nil || t.Account == nil || t.session == nil {
		return fmt.Errorf("gateway admission target is unavailable")
	}
	if !t.dispatched.CompareAndSwap(false, true) {
		return fmt.Errorf("gateway admission target was already dispatched")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	defer t.session.ReleaseTarget()

	if t.session.Waited() && recheck != nil {
		if err := recheck(ctx); err != nil {
			return err
		}
	}
	if upstream == nil {
		return fmt.Errorf("gateway admission upstream dispatch is unavailable")
	}
	stopRenewal := t.session.startRenewal(ctx, t.Account)
	defer stopRenewal()
	return upstream(ctx, t.Account)
}

func (s *GatewayAdmissionSession) startRenewal(ctx context.Context, account *Account) func() {
	if s == nil || s.admission == nil || s.store == nil || account == nil || s.admission.renewInterval <= 0 {
		return func() {}
	}
	renewCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(s.admission.renewInterval)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				s.renewHeldLeases(renewCtx, account)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (s *GatewayAdmissionSession) renewHeldLeases(ctx context.Context, account *Account) {
	renewCtx, cancel := context.WithTimeout(ctx, gatewayAdmissionRenewTimeout)
	defer cancel()

	s.mu.Lock()
	class := s.class
	unlimited := s.unlimited
	s.mu.Unlock()
	if !unlimited {
		_, _ = s.store.RenewUserLease(renewCtx, s.request.UserID, s.requestID, class)
	}
	_, _ = s.store.RenewTargetLease(renewCtx, account.Platform, account.ID, s.requestID)
}

func (s *GatewayAdmissionSession) refreshUserLease(ctx context.Context) (AdmissionClass, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return "", context.Canceled
	}
	if s.unlimited {
		class := s.class
		s.mu.Unlock()
		return class, nil
	}
	s.mu.Unlock()

	result, err := s.store.TryAcquireUserLease(ctx, UserLeaseRequest{
		RequestID:     s.requestID,
		UserID:        s.request.UserID,
		StandardLimit: s.request.StandardLimit,
		ExtraLimit:    s.request.ExtraLimit,
		MaxWaiting:    gatewayAdmissionMaxWaiting(s.request),
		WaitTimeout:   time.Duration(s.request.Settings.WaitTimeoutSeconds) * time.Second,
	})
	if err != nil {
		return "", err
	}
	if !result.Acquired {
		return "", fmt.Errorf("gateway admission user lease was lost")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.admission.releaseUserState(s.request.UserID, s.requestID)
		return "", context.Canceled
	}
	s.class = result.Class
	s.unlimited = result.Unlimited
	class := s.class
	s.mu.Unlock()
	return class, nil
}

func gatewayAdmissionMaxWaiting(request GatewayAdmissionRequest) int {
	totalLimit := max(request.StandardLimit, 0) + max(request.ExtraLimit, 0)
	if totalLimit <= 0 {
		return 0
	}
	return max(CalculateMaxWait(totalLimit)-totalLimit, 1)
}

func (s *GatewayAdmissionSession) isClosed() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *GatewayAdmissionSession) setTargetRelease(release func()) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		if release != nil {
			release()
		}
		return false
	}
	s.targetRelease = release
	s.mu.Unlock()
	return true
}

func (s *GatewayAdmissionSession) ReleaseTarget() {
	if s == nil {
		return
	}
	s.mu.Lock()
	release := s.targetRelease
	s.targetRelease = nil
	s.mu.Unlock()
	if release != nil {
		release()
	}
}

func (s *GatewayAdmissionSession) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		release := s.targetRelease
		s.targetRelease = nil
		unlimited := s.unlimited
		s.mu.Unlock()
		if release != nil {
			release()
		}
		if unlimited || s.store == nil {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.store.ReleaseUserLease(releaseCtx, s.request.UserID, s.requestID)
	})
}
