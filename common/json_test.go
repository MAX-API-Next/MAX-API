package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnmarshalUseNumberPreservesTokensAndRejectsTrailingData(t *testing.T) {
	var value map[string]any
	require.NoError(t, UnmarshalUseNumber([]byte(`{"huge":1e400,"zero":0,"precise":9007199254740993}`), &value))
	require.Equal(t, json.Number("1e400"), value["huge"])
	require.Equal(t, json.Number("0"), value["zero"])
	require.Equal(t, json.Number("9007199254740993"), value["precise"])
	for _, raw := range []string{`{} {}`, `{} x`, `{"x":1e}`, ``} {
		require.Error(t, UnmarshalUseNumber([]byte(raw), &value))
	}
}

func TestJsonRawMessageToString(t *testing.T) {
	tests := []struct {
		name string
		data json.RawMessage
		want string
	}{
		{
			name: "object",
			data: json.RawMessage(`{"city":"Paris","days":0,"strict":false}`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "string",
			data: json.RawMessage(`"{\"city\":\"Paris\",\"days\":0,\"strict\":false}"`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "null",
			data: json.RawMessage(`null`),
			want: "",
		},
		{
			name: "empty",
			data: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, JsonRawMessageToString(tt.data))
		})
	}
}
