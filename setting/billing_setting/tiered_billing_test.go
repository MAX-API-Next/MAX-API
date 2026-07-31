package billing_setting

import (
	"sync"
	"testing"

	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestBillingSettingMapsSupportConcurrentReloads(t *testing.T) {
	setting := BillingSetting{
		BillingMode: types.NewRWMap[string, string](),
		BillingExpr: types.NewRWMap[string, string](),
	}

	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range 200 {
				_, _ = setting.BillingMode.Get("model")
				_, _ = setting.BillingExpr.Get("model")
			}
		}()
	}

	for i := 0; i < 200; i++ {
		require.NoError(t, config.UpdateConfigFromMap(&setting, map[string]string{
			BillingModeField: `{"model":"tiered_expr"}`,
			BillingExprField: `{"model":"p + c"}`,
		}))
	}
	readers.Wait()

	mode, ok := setting.BillingMode.Get("model")
	require.True(t, ok)
	require.Equal(t, BillingModeTieredExpr, mode)
	expr, ok := setting.BillingExpr.Get("model")
	require.True(t, ok)
	require.Equal(t, "p + c", expr)
}
