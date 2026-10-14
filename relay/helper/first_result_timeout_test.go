package helper

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFirstResultTimeoutErrorRequiresMissingFirstResponse(t *testing.T) {
	info := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)

	err := FirstResultTimeoutError(nil, info)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeChannelResponseTimeExceeded, err.GetErrorCode())

	info.SetFirstResponseTime()
	require.Nil(t, FirstResultTimeoutError(nil, info))

	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	require.Nil(t, FirstResultTimeoutError(nil, info))
}

func TestFirstResultTimeoutCannotReplayCommittedMetadataOrPing(t *testing.T) {
	for _, committed := range []string{"none", "metadata", "ping"} {
		t.Run(committed, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
			info.EnableFirstResultTracking()
			if committed == "metadata" {
				info.SetFirstResponseTime()
			}
			if committed == "ping" {
				_, _ = c.Writer.WriteString(": PING\n\n")
			}
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
			apiErr := FirstResultTimeoutError(c, info)
			require.NotNil(t, apiErr)
			require.Equal(t, committed != "none", types.IsSkipRetryError(apiErr))
			info.SetFirstResultTime()
			require.Nil(t, FirstResultTimeoutError(c, info), "output-idle is not a first-result timeout")
		})
	}
}

func TestFirstResultTimeoutErrorHandlesNilState(t *testing.T) {
	require.Nil(t, FirstResultTimeoutError(nil, nil))
	require.Nil(t, FirstResultTimeoutError(nil, &relaycommon.RelayInfo{}))
}
