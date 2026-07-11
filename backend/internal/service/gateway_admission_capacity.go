package service

import (
	"context"
	"fmt"
)

type AdmissionCapacitySnapshot struct {
	TotalConcurrency   int
	AccountConcurrency map[int64]int
}

type AdmissionCapacitySource interface {
	AdmissionCapacity(ctx context.Context, platform string) (AdmissionCapacitySnapshot, error)
}

func (s *GatewayService) AdmissionCapacity(ctx context.Context, platform string) (AdmissionCapacitySnapshot, error) {
	if s == nil || s.accountRepo == nil {
		return AdmissionCapacitySnapshot{}, fmt.Errorf("gateway admission capacity source is unavailable")
	}

	accounts, err := s.accountRepo.ListSchedulableByPlatform(ctx, platform)
	if err != nil {
		return AdmissionCapacitySnapshot{}, fmt.Errorf("list gateway admission capacity for %s: %w", platform, err)
	}

	snapshot := AdmissionCapacitySnapshot{
		AccountConcurrency: make(map[int64]int, len(accounts)),
	}
	for i := range accounts {
		account := &accounts[i]
		if account.ID <= 0 || account.Platform != platform || !account.IsSchedulable() || account.Concurrency <= 0 {
			continue
		}
		if _, exists := snapshot.AccountConcurrency[account.ID]; exists {
			continue
		}
		snapshot.AccountConcurrency[account.ID] = account.Concurrency
		snapshot.TotalConcurrency += account.Concurrency
	}
	return snapshot, nil
}
