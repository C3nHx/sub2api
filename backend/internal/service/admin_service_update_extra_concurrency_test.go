//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminService_UpdateUserExtraConcurrencyRecordsAdjustment(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{
		ID:               7,
		Email:            "extra-update@test.com",
		PasswordHash:     "hash",
		Role:             RoleUser,
		Status:           StatusActive,
		Concurrency:      5,
		ExtraConcurrency: 1,
	}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	redeemRepo := &balanceRedeemRepoStub{redeemRepoStub: &redeemRepoStub{}}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       redeemRepo,
		authCacheInvalidator: invalidator,
	}
	extraConcurrency := 3

	updated, err := svc.UpdateUser(context.Background(), 7, &UpdateUserInput{
		ExtraConcurrency: &extraConcurrency,
	})

	require.NoError(t, err)
	require.Equal(t, 5, updated.Concurrency)
	require.Equal(t, 3, updated.ExtraConcurrency)
	require.Equal(t, []int64{7}, invalidator.userIDs)
	require.Len(t, redeemRepo.created, 1)
	require.Equal(t, AdjustmentTypeAdminExtraConcurrency, redeemRepo.created[0].Type)
	require.Equal(t, float64(2), redeemRepo.created[0].Value)
}

func TestAdminService_UpdateUserRejectsNegativeExtraConcurrency(t *testing.T) {
	svc := &adminServiceImpl{userRepo: &userRepoStub{}}
	extraConcurrency := -1

	updated, err := svc.UpdateUser(context.Background(), 7, &UpdateUserInput{
		ExtraConcurrency: &extraConcurrency,
	})

	require.Nil(t, updated)
	require.ErrorContains(t, err, "extra concurrency must be non-negative")
}
