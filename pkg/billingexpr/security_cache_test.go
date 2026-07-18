package billingexpr

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunExprWithRequestBlocksSensitiveValues(t *testing.T) {
	request := RequestInput{
		Headers: map[string]string{
			"Authorization":  "Bearer secret-token",
			"Anthropic-Beta": "fast-mode",
		},
		Body: []byte(`{"messages":[{"role":"user","content":"private prompt"}],"service_tier":"fast"}`),
	}

	tests := []struct {
		name string
		expr string
	}{
		{name: "authorization header", expr: `header("authorization") == "" ? 1 : 999`},
		{name: "unknown auth-like header", expr: `header("x-auth") == "" ? 1 : 999`},
		{name: "message content", expr: `param("messages.0.content") == nil ? 1 : 999`},
		{name: "whole message", expr: `param("messages.0") == nil ? 1 : 999`},
		{name: "whole request", expr: `param("@this") == nil ? 1 : 999`},
		{name: "unknown body root", expr: `param("contents.*") == nil ? 1 : 999`},
		{name: "gjson query", expr: `param("messages.#(role==\"user\")") == nil ? 1 : 999`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, _, err := RunExprWithRequest(tt.expr, TokenParams{}, request)
			require.NoError(t, err)
			assert.Equal(t, float64(1), cost)
		})
	}

	cost, _, err := RunExprWithRequest(
		`has(header("anthropic-beta"), "fast-mode") && param("messages.#") == 1 && param("service_tier") == "fast" ? 1 : 999`,
		TokenParams{},
		request,
	)
	require.NoError(t, err)
	assert.Equal(t, float64(1), cost)
}

func TestCompileCacheEvictsIncrementallyAtCapacity(t *testing.T) {
	InvalidateCache()
	t.Cleanup(InvalidateCache)

	for i := 0; i < maxCacheSize+10; i++ {
		_, err := CompileFromCache(fmt.Sprintf("p + %d", i))
		require.NoError(t, err)
	}

	assert.Len(t, cache, maxCacheSize)
}
