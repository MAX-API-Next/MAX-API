package constant

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPath2RelayModeAlphaSearch(t *testing.T) {
	tests := []string{
		"/v1/alpha/search",
		"/v1/alpha/search?foo=1",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			require.Equal(t, RelayModeAlphaSearch, Path2RelayMode(path))
		})
	}
}
