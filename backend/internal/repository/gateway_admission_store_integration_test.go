//go:build integration

package repository

import (
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayAdmissionStoreAllocatesStandardBeforeExtraAcrossInstances(t *testing.T) {
	rdb := testRedis(t)
	firstStore := NewGatewayAdmissionStore(rdb, time.Minute)
	secondStore := NewGatewayAdmissionStore(rdb, time.Minute)

	standard, err := firstStore.TryAcquireUserLease(t.Context(), service.UserLeaseRequest{
		RequestID:     "standard-request",
		UserID:        42,
		StandardLimit: 1,
		ExtraLimit:    1,
	})
	require.NoError(t, err)
	require.True(t, standard.Acquired)
	require.Equal(t, service.AdmissionClassStandard, standard.Class)

	extra, err := secondStore.TryAcquireUserLease(t.Context(), service.UserLeaseRequest{
		RequestID:     "extra-request",
		UserID:        42,
		StandardLimit: 1,
		ExtraLimit:    1,
	})
	require.NoError(t, err)
	require.True(t, extra.Acquired)
	require.Equal(t, service.AdmissionClassExtra, extra.Class)

	blocked, err := secondStore.TryAcquireUserLease(t.Context(), service.UserLeaseRequest{
		RequestID:     "next-request",
		UserID:        42,
		StandardLimit: 1,
		ExtraLimit:    1,
	})
	require.NoError(t, err)
	require.False(t, blocked.Acquired)

	require.NoError(t, firstStore.ReleaseUserLease(t.Context(), 42, "standard-request"))

	promoted, err := secondStore.TryAcquireUserLease(t.Context(), service.UserLeaseRequest{
		RequestID:     "extra-request",
		UserID:        42,
		StandardLimit: 1,
		ExtraLimit:    1,
	})
	require.NoError(t, err)
	require.True(t, promoted.Acquired)
	require.Equal(t, service.AdmissionClassStandard, promoted.Class)
}

func TestGatewayAdmissionStoreExtraTargetPreservesPlatformReserve(t *testing.T) {
	rdb := testRedis(t)
	firstStore := NewGatewayAdmissionStore(rdb, time.Minute)
	secondStore := NewGatewayAdmissionStore(rdb, time.Minute)

	first, err := firstStore.TryAcquireTargetLease(t.Context(), service.TargetLeaseRequest{
		RequestID:        "first-extra-request",
		Platform:         service.PlatformAnthropic,
		AccountID:        101,
		AccountLimit:     2,
		PlatformCapacity: 2,
		ReservedSlots:    1,
		Class:            service.AdmissionClassExtra,
	})
	require.NoError(t, err)
	require.True(t, first.Acquired)

	second, err := secondStore.TryAcquireTargetLease(t.Context(), service.TargetLeaseRequest{
		RequestID:        "second-extra-request",
		Platform:         service.PlatformAnthropic,
		AccountID:        101,
		AccountLimit:     2,
		PlatformCapacity: 2,
		ReservedSlots:    1,
		Class:            service.AdmissionClassExtra,
	})
	require.NoError(t, err)
	require.False(t, second.Acquired)

	require.NoError(t, firstStore.ReleaseTargetLease(
		t.Context(),
		service.PlatformAnthropic,
		101,
		"first-extra-request",
	))

	retried, err := secondStore.TryAcquireTargetLease(t.Context(), service.TargetLeaseRequest{
		RequestID:        "second-extra-request",
		Platform:         service.PlatformAnthropic,
		AccountID:        101,
		AccountLimit:     2,
		PlatformCapacity: 2,
		ReservedSlots:    1,
		Class:            service.AdmissionClassExtra,
	})
	require.NoError(t, err)
	require.True(t, retried.Acquired)
}

func TestGatewayAdmissionStoreSharesAccountCapacityWithLegacyConcurrency(t *testing.T) {
	rdb := testRedis(t)
	legacy := NewConcurrencyCache(rdb, 1, 60)
	store := NewGatewayAdmissionStore(rdb, time.Minute)
	const accountID int64 = 109

	acquired, err := legacy.AcquireAccountSlot(t.Context(), accountID, 1, "legacy-request")
	require.NoError(t, err)
	require.True(t, acquired)

	request := service.TargetLeaseRequest{
		RequestID:        "gateway-admission-request",
		Platform:         service.PlatformAnthropic,
		AccountID:        accountID,
		AccountLimit:     1,
		PlatformCapacity: 2,
		Class:            service.AdmissionClassStandard,
	}
	blocked, err := store.TryAcquireTargetLease(t.Context(), request)
	require.NoError(t, err)
	require.False(t, blocked.Acquired)

	require.NoError(t, legacy.ReleaseAccountSlot(t.Context(), accountID, "legacy-request"))
	admitted, err := store.TryAcquireTargetLease(t.Context(), request)
	require.NoError(t, err)
	require.True(t, admitted.Acquired)

	competing, err := legacy.AcquireAccountSlot(t.Context(), accountID, 1, "legacy-competitor")
	require.NoError(t, err)
	require.False(t, competing)
	require.NoError(t, store.ReleaseTargetLease(t.Context(), request.Platform, accountID, request.RequestID))
}

func TestGatewayAdmissionStoreStandardWaiterBlocksEarlierExtraWaiter(t *testing.T) {
	store := NewGatewayAdmissionStore(testRedis(t), time.Minute)
	baseRequest := service.TargetLeaseRequest{
		Platform:         service.PlatformAnthropic,
		AccountID:        202,
		AccountLimit:     1,
		PlatformCapacity: 1,
	}

	activeRequest := baseRequest
	activeRequest.RequestID = "active-standard"
	activeRequest.Class = service.AdmissionClassStandard
	active, err := store.TryAcquireTargetLease(t.Context(), activeRequest)
	require.NoError(t, err)
	require.True(t, active.Acquired)

	extraRequest := baseRequest
	extraRequest.RequestID = "earlier-extra-waiter"
	extraRequest.Class = service.AdmissionClassExtra
	extra, err := store.TryAcquireTargetLease(t.Context(), extraRequest)
	require.NoError(t, err)
	require.False(t, extra.Acquired)

	standardRequest := baseRequest
	standardRequest.RequestID = "later-standard-waiter"
	standardRequest.Class = service.AdmissionClassStandard
	standard, err := store.TryAcquireTargetLease(t.Context(), standardRequest)
	require.NoError(t, err)
	require.False(t, standard.Acquired)

	require.NoError(t, store.ReleaseTargetLease(
		t.Context(),
		baseRequest.Platform,
		baseRequest.AccountID,
		activeRequest.RequestID,
	))

	extra, err = store.TryAcquireTargetLease(t.Context(), extraRequest)
	require.NoError(t, err)
	require.False(t, extra.Acquired)

	standard, err = store.TryAcquireTargetLease(t.Context(), standardRequest)
	require.NoError(t, err)
	require.True(t, standard.Acquired)
}

func TestGatewayAdmissionStoreExpiredStandardWaiterStopsBlockingExtra(t *testing.T) {
	store := NewGatewayAdmissionStore(testRedis(t), time.Minute)
	baseRequest := service.TargetLeaseRequest{
		Platform:         service.PlatformAnthropic,
		AccountID:        303,
		AccountLimit:     1,
		PlatformCapacity: 1,
	}

	activeRequest := baseRequest
	activeRequest.RequestID = "active-standard"
	activeRequest.Class = service.AdmissionClassStandard
	active, err := store.TryAcquireTargetLease(t.Context(), activeRequest)
	require.NoError(t, err)
	require.True(t, active.Acquired)

	standardRequest := baseRequest
	standardRequest.RequestID = "expired-standard-waiter"
	standardRequest.Class = service.AdmissionClassStandard
	standardRequest.WaitTimeout = 20 * time.Millisecond
	standard, err := store.TryAcquireTargetLease(t.Context(), standardRequest)
	require.NoError(t, err)
	require.False(t, standard.Acquired)

	extraRequest := baseRequest
	extraRequest.RequestID = "extra-waiter"
	extraRequest.Class = service.AdmissionClassExtra
	extraRequest.WaitTimeout = time.Second
	extra, err := store.TryAcquireTargetLease(t.Context(), extraRequest)
	require.NoError(t, err)
	require.False(t, extra.Acquired)

	time.Sleep(40 * time.Millisecond)
	require.NoError(t, store.ReleaseTargetLease(
		t.Context(),
		baseRequest.Platform,
		baseRequest.AccountID,
		activeRequest.RequestID,
	))

	extra, err = store.TryAcquireTargetLease(t.Context(), extraRequest)
	require.NoError(t, err)
	require.True(t, extra.Acquired)
}

func TestGatewayAdmissionStoreRenewKeepsUserAndTargetLeasesAlive(t *testing.T) {
	store := NewGatewayAdmissionStore(testRedis(t), 2*time.Second)
	requestID := "long-running-request"

	userLease, err := store.TryAcquireUserLease(t.Context(), service.UserLeaseRequest{
		RequestID:     requestID,
		UserID:        404,
		StandardLimit: 1,
		ExtraLimit:    1,
	})
	require.NoError(t, err)
	require.True(t, userLease.Acquired)
	require.Equal(t, service.AdmissionClassStandard, userLease.Class)

	targetRequest := service.TargetLeaseRequest{
		RequestID:        requestID,
		Platform:         service.PlatformAnthropic,
		AccountID:        404,
		AccountLimit:     1,
		PlatformCapacity: 1,
		Class:            service.AdmissionClassStandard,
	}
	targetLease, err := store.TryAcquireTargetLease(t.Context(), targetRequest)
	require.NoError(t, err)
	require.True(t, targetLease.Acquired)

	time.Sleep(1200 * time.Millisecond)
	renewed, err := store.RenewUserLease(
		t.Context(),
		404,
		requestID,
		service.AdmissionClassStandard,
	)
	require.NoError(t, err)
	require.True(t, renewed)
	renewed, err = store.RenewTargetLease(
		t.Context(),
		service.PlatformAnthropic,
		404,
		requestID,
	)
	require.NoError(t, err)
	require.True(t, renewed)

	time.Sleep(1200 * time.Millisecond)
	competingUser, err := store.TryAcquireUserLease(t.Context(), service.UserLeaseRequest{
		RequestID:     "competing-request",
		UserID:        404,
		StandardLimit: 1,
	})
	require.NoError(t, err)
	require.False(t, competingUser.Acquired)

	competingTarget := targetRequest
	competingTarget.RequestID = "competing-request"
	competingTarget.Class = service.AdmissionClassStandard
	competingLease, err := store.TryAcquireTargetLease(t.Context(), competingTarget)
	require.NoError(t, err)
	require.False(t, competingLease.Acquired)
}

func TestGatewayAdmissionStoreUserLeaseContentionAcrossRedisClients(t *testing.T) {
	clients := testRedisClients(t, 2)
	stores := []service.GatewayAdmissionStore{
		NewGatewayAdmissionStore(clients[0], time.Minute),
		NewGatewayAdmissionStore(clients[1], time.Minute),
	}

	const attempts = 32
	start := make(chan struct{})
	results := make(chan struct {
		lease service.UserLeaseResult
		err   error
	}, attempts)
	for i := range attempts {
		go func(attempt int) {
			<-start
			lease, err := stores[attempt%len(stores)].TryAcquireUserLease(
				t.Context(),
				service.UserLeaseRequest{
					RequestID:     fmt.Sprintf("request-%02d", attempt),
					UserID:        505,
					StandardLimit: 1,
					ExtraLimit:    1,
				},
			)
			results <- struct {
				lease service.UserLeaseResult
				err   error
			}{lease: lease, err: err}
		}(i)
	}
	close(start)

	standardCount := 0
	extraCount := 0
	for range attempts {
		result := <-results
		require.NoError(t, result.err)
		if !result.lease.Acquired {
			continue
		}
		switch result.lease.Class {
		case service.AdmissionClassStandard:
			standardCount++
		case service.AdmissionClassExtra:
			extraCount++
		default:
			t.Fatalf("unexpected admission class %q", result.lease.Class)
		}
	}

	require.Equal(t, 1, standardCount)
	require.Equal(t, 1, extraCount)
}

func TestGatewayAdmissionStoreUnlimitedStandardNeverConsumesExtra(t *testing.T) {
	store := NewGatewayAdmissionStore(testRedis(t), time.Minute)

	for i := range 3 {
		lease, err := store.TryAcquireUserLease(t.Context(), service.UserLeaseRequest{
			RequestID:     fmt.Sprintf("unlimited-request-%d", i),
			UserID:        606,
			StandardLimit: 0,
			ExtraLimit:    1,
		})
		require.NoError(t, err)
		require.True(t, lease.Acquired)
		require.Equal(t, service.AdmissionClassStandard, lease.Class)
		require.True(t, lease.Unlimited)
	}
}

func TestGatewayAdmissionStoreUserWaitersAreFIFO(t *testing.T) {
	store := NewGatewayAdmissionStore(testRedis(t), time.Minute)
	request := service.UserLeaseRequest{
		UserID:        707,
		StandardLimit: 1,
	}

	request.RequestID = "active-request"
	active, err := store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.True(t, active.Acquired)

	request.RequestID = "earlier-waiter"
	earlier, err := store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.False(t, earlier.Acquired)

	request.RequestID = "later-waiter"
	later, err := store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.False(t, later.Acquired)

	require.NoError(t, store.ReleaseUserLease(t.Context(), 707, "active-request"))

	later, err = store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.False(t, later.Acquired)

	request.RequestID = "earlier-waiter"
	earlier, err = store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.True(t, earlier.Acquired)
	require.Equal(t, service.AdmissionClassStandard, earlier.Class)
}

func TestGatewayAdmissionStoreRejectsUserWaiterWhenMaxWaitingReached(t *testing.T) {
	store := NewGatewayAdmissionStore(testRedis(t), time.Minute)
	request := service.UserLeaseRequest{
		UserID:        708,
		StandardLimit: 1,
		MaxWaiting:    1,
	}

	request.RequestID = "active-request"
	active, err := store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.True(t, active.Acquired)

	request.RequestID = "accepted-waiter"
	accepted, err := store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.False(t, accepted.Acquired)
	require.False(t, accepted.QueueFull)

	request.RequestID = "rejected-waiter"
	rejected, err := store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.False(t, rejected.Acquired)
	require.True(t, rejected.QueueFull)
}

func TestGatewayAdmissionStoreExpiredUserWaiterDoesNotBlockQueue(t *testing.T) {
	store := NewGatewayAdmissionStore(testRedis(t), time.Minute)
	request := service.UserLeaseRequest{
		UserID:        709,
		StandardLimit: 1,
		MaxWaiting:    2,
		WaitTimeout:   time.Second,
	}

	request.RequestID = "active-request"
	active, err := store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.True(t, active.Acquired)

	request.RequestID = "crashed-waiter"
	request.WaitTimeout = 50 * time.Millisecond
	crashed, err := store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.False(t, crashed.Acquired)

	request.RequestID = "live-waiter"
	request.WaitTimeout = time.Second
	live, err := store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.False(t, live.Acquired)

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, store.ReleaseUserLease(t.Context(), 709, "active-request"))

	live, err = store.TryAcquireUserLease(t.Context(), request)
	require.NoError(t, err)
	require.True(t, live.Acquired)
	require.Equal(t, service.AdmissionClassStandard, live.Class)
}
