package ratio_setting

import (
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestUpdateModelRatioRejectsNormalizedDuplicateWithoutReplacing(t *testing.T) {
	original := ModelRatio2JSONString()
	require.NoError(t, UpdateModelRatioByJSONString(`{"stable-model":1.5}`))
	t.Cleanup(func() {
		require.NoError(t, UpdateModelRatioByJSONString(original))
	})

	err := UpdateModelRatioByJSONString(`{"gemini-2.5-flash-thinking-a":1,"gemini-2.5-flash-thinking-b":2}`)

	require.Error(t, err)
	ratio, ok, _ := GetModelRatio("stable-model")
	require.True(t, ok)
	require.Equal(t, 1.5, ratio)
}

func TestUpdateModelRatioLoadsNormalizedAlias(t *testing.T) {
	original := ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateModelRatioByJSONString(original))
	})

	const alias = "gemini-2.5-flash-thinking-a"
	require.NoError(t, UpdateModelRatioByJSONString(`{" `+alias+` ":1.75}`))

	ratio, ok, normalizedName := GetModelRatio(alias)
	require.True(t, ok)
	require.Equal(t, "gemini-2.5-flash-thinking-*", normalizedName)
	require.Equal(t, 1.75, ratio)
}

func TestLoadPricingMapWithoutNormalizerPreservesRawKeys(t *testing.T) {
	pricingMap := types.NewRWMap[string, float64]()

	require.NoError(t, loadPricingMap(pricingMap, `{" raw-model ":2.5}`))

	value, ok := pricingMap.Get(" raw-model ")
	require.True(t, ok)
	require.Equal(t, 2.5, value)
}

func TestValidatePricingMapRejectsOversizedJSONBeforeLoad(t *testing.T) {
	raw := `{"` + strings.Repeat("a", maxPricingMapJSONBytes) + `":1}`

	require.Error(t, ValidatePricingMapJSONString(raw))
}
