package model

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type failingAuthFlowReader struct{}

func (failingAuthFlowReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func setupAuthFlowTestDB(t *testing.T) {
	t.Helper()
	originalDB := DB
	db, err := gorm.Open(sqlite.Open("file:auth_flow_test?mode=memory&cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&AuthFlow{}))
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestAuthFlowIsProviderBoundAndConsumedOnce(t *testing.T) {
	setupAuthFlowTestDB(t)
	token, created, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeOAuth, Provider: "github", Intent: AuthFlowIntentLogin,
		Payload: `{"affiliate_code":"invite"}`, ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.Len(t, token, 43)
	require.NotEqual(t, token, created.TokenHash)

	_, err = GetAuthFlow(token, AuthFlowMatch{Purpose: AuthFlowPurposeOAuth, Provider: "discord"})
	require.ErrorIs(t, err, ErrAuthFlowInvalid)

	const consumers = 2
	errorsByConsumer := make([]error, consumers)
	var wg sync.WaitGroup
	for i := 0; i < consumers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, errorsByConsumer[index] = ConsumeAuthFlow(token, AuthFlowMatch{
				Purpose: AuthFlowPurposeOAuth, Provider: "github", Intent: AuthFlowIntentLogin,
			})
		}(i)
	}
	wg.Wait()

	successes := 0
	consumed := 0
	for _, consumeErr := range errorsByConsumer {
		switch {
		case consumeErr == nil:
			successes++
		case errors.Is(consumeErr, ErrAuthFlowConsumed):
			consumed++
		default:
			t.Fatalf("unexpected consume error: %v", consumeErr)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, consumed)
}

func TestAuthFlowExpiryIsEnforced(t *testing.T) {
	setupAuthFlowTestDB(t)
	token, flow, err := CreateAuthFlow(AuthFlowCreate{
		Purpose: AuthFlowPurposeOAuth, Provider: "oidc", Intent: AuthFlowIntentLogin,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	require.NoError(t, DB.Model(&AuthFlow{}).Where("id = ?", flow.Id).Update("expires_at", time.Now().Add(-time.Second)).Error)

	_, err = ConsumeAuthFlow(token, AuthFlowMatch{Purpose: AuthFlowPurposeOAuth, Provider: "oidc"})
	require.ErrorIs(t, err, ErrAuthFlowExpired)
}

func TestGenerateAuthFlowTokenReturnsEntropyFailure(t *testing.T) {
	token, err := generateAuthFlowToken(failingAuthFlowReader{})
	require.Empty(t, token)
	require.ErrorContains(t, err, "entropy unavailable")
}
