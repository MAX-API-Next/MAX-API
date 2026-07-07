package helper

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStreamDataHelpersHandleNilContextOrWriter(t *testing.T) {
	claudeResp := dto.ClaudeResponse{Type: "message_delta"}
	responsesResp := dto.ResponsesStreamResponse{Type: "response.output_text.delta"}

	for _, c := range []*gin.Context{nil, &gin.Context{}} {
		require.ErrorContains(t, ClaudeData(c, claudeResp), "context or writer is nil")
		require.NotPanics(t, func() {
			ClaudeChunkData(c, claudeResp, "{}")
		})
		require.ErrorContains(t, ResponseChunkData(c, responsesResp, "{}"), "context or writer is nil")
	}
}
