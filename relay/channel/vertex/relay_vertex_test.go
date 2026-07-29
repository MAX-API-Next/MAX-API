package vertex

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetModelRegionIgnoresNonStringConfiguration(t *testing.T) {
	require.Equal(t, "global", GetModelRegion(`{"model-a":123,"default":false}`, "model-a"))
	require.Equal(t, "us-central1", GetModelRegion(`{"model-a":123,"default":"us-central1"}`, "model-a"))
}
