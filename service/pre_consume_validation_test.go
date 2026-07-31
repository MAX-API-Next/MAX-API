package service

import (
	"net/http"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/stretchr/testify/require"
)

func TestPreConsumeBillingValidation(t *testing.T) {
	t.Run("quota_clamp", func(t *testing.T) {
		clamp := &common.QuotaClamp{
			Op:       "test",
			Kind:     common.QuotaClampOverflow,
			Original: float64(common.MaxQuota) + 1,
			Clamped:  common.MaxQuota,
		}

		apiErr := PreConsumeBilling(nil, 1, &relaycommon.RelayInfo{QuotaClamp: clamp})

		require.NotNil(t, apiErr)
		require.Equal(t, clamp, apiErr.Err)
		require.Equal(t, types.ErrorCodeModelPriceError, apiErr.GetErrorCode())
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		require.True(t, types.IsSkipRetryError(apiErr))
	})

	t.Run("negative_quota", func(t *testing.T) {
		apiErr := PreConsumeBilling(nil, -1, &relaycommon.RelayInfo{})

		require.NotNil(t, apiErr)
		require.ErrorContains(t, apiErr.Err, "pre-consume quota cannot be negative: -1")
		require.Equal(t, types.ErrorCodeModelPriceError, apiErr.GetErrorCode())
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		require.True(t, types.IsSkipRetryError(apiErr))
	})
}
