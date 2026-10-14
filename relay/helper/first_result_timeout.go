package helper

import (
	"errors"
	"net/http"

	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
)

// FirstResultTimeoutError converts a stream scanner timeout that happened
// before a generation payload into the API error used by relay settlement and
// retry handling. Even metadata or ping delivery forbids transparent replay.
func FirstResultTimeoutError(c *gin.Context, info *relaycommon.RelayInfo) *types.MaxAPIError {
	if info == nil || info.StreamStatus == nil ||
		info.StreamStatus.EndReason != relaycommon.StreamEndReasonTimeout ||
		info.HasRecordedChannelFirstResult() {
		return nil
	}
	apiErr := types.NewOpenAIError(
		errors.New("upstream stream timed out before the first result"),
		types.ErrorCodeChannelResponseTimeExceeded,
		http.StatusRequestTimeout,
	)
	if info.HasRecordedChannelFirstResponse() || (c != nil && c.Writer != nil && c.Writer.Written()) {
		types.ErrOptionWithSkipRetry()(apiErr)
	}
	return apiErr
}
