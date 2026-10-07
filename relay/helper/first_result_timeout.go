package helper

import (
	"errors"
	"net/http"

	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

// FirstResultTimeoutError converts a stream scanner timeout that happened
// before any channel response was recorded into the retryable API error used
// by relay settlement and retry handling. A stream that already delivered a
// channel response keeps its existing terminal handling.
func FirstResultTimeoutError(info *relaycommon.RelayInfo) *types.MaxAPIError {
	if info == nil || info.StreamStatus == nil ||
		info.StreamStatus.EndReason != relaycommon.StreamEndReasonTimeout ||
		info.HasRecordedChannelFirstResponse() {
		return nil
	}
	return types.NewOpenAIError(
		errors.New("upstream stream timed out before the first response"),
		types.ErrorCodeChannelResponseTimeExceeded,
		http.StatusRequestTimeout,
	)
}
