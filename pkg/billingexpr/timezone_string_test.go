package billingexpr_test

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/stretchr/testify/require"
)

// The visual editor emits JSON-quoted strings. The Go expression runtime must
// preserve those strings and its established unknown-timezone UTC fallback.
func TestTimeFunctionsJSONQuotedTimezoneCompatibility(t *testing.T) {
	for _, timezone := range []string{"UTC", `Test/"quote`, `Test/back\`, "Test/\nline", "Test/\tvalue"} {
		t.Run(timezone, func(t *testing.T) {
			quoted, err := common.Marshal(timezone)
			require.NoError(t, err)
			expression := "hour(" + string(quoted) + `) >= 0 ? tier("base", p * 1 + c * 0) : tier("fallback", p * 2 + c * 0)`
			cost, _, err := billingexpr.RunExpr(expression, billingexpr.TokenParams{P: 100})
			require.NoError(t, err)
			require.Equal(t, float64(100), cost)
		})
	}
}
