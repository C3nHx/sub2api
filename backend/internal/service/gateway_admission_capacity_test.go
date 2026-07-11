//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayAdmissionCapacityDeduplicatesSchedulablePlatformAccounts(t *testing.T) {
	accounts := []Account{
		{ID: 1, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true, Concurrency: 2},
		{ID: 1, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true, Concurrency: 2},
		{ID: 2, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: false, Concurrency: 5},
		{ID: 3, Platform: PlatformAnthropic, Status: StatusDisabled, Schedulable: true, Concurrency: 4},
		{ID: 4, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true, Concurrency: 3},
		{ID: 5, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 9},
		{ID: 6, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true, Concurrency: 0},
	}
	repo := &mockAccountRepoForPlatform{
		listPlatformFunc: func(context.Context, string) ([]Account, error) {
			return accounts, nil
		},
	}
	svc := &GatewayService{accountRepo: repo}

	snapshot, err := svc.AdmissionCapacity(context.Background(), PlatformAnthropic)

	require.NoError(t, err)
	require.Equal(t, 5, snapshot.TotalConcurrency)
	require.Equal(t, map[int64]int{1: 2, 4: 3}, snapshot.AccountConcurrency)
}
