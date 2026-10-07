package helper

import (
	"testing"

	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestFirstResultTimeoutErrorRequiresMissingFirstResponse(t *testing.T) {
	info := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)

	err := FirstResultTimeoutError(info)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeChannelResponseTimeExceeded, err.GetErrorCode())

	info.SetFirstResponseTime()
	require.Nil(t, FirstResultTimeoutError(info))

	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	require.Nil(t, FirstResultTimeoutError(info))
}

func TestFirstResultTimeoutErrorHandlesNilState(t *testing.T) {
	require.Nil(t, FirstResultTimeoutError(nil))
	require.Nil(t, FirstResultTimeoutError(&relaycommon.RelayInfo{}))
}
