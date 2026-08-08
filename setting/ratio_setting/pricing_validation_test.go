package ratio_setting

import (
	"strings"
	"testing"

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

func TestValidatePricingMapRejectsOversizedJSONBeforeLoad(t *testing.T) {
	raw := `{"` + strings.Repeat("a", maxPricingMapJSONBytes) + `":1}`

	require.Error(t, ValidatePricingMapJSONString(raw))
}
