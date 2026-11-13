package gemini

import (
	"errors"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
)

func geminiEmbeddingUsage(c *gin.Context, info *relaycommon.RelayInfo, metadata *dto.GeminiEmbeddingUsageMetadata) (*dto.Usage, error) {
	if upstream := metadata.GeminiUsageMetadata(); upstream != nil {
		if upstream.PromptTokenCount < 0 {
			return nil, errors.New("negative Gemini embedding usage")
		}
		usage := buildUsageFromGeminiMetadata(*upstream, info.GetEstimatePromptTokens())
		return &usage, nil
	}
	return service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens()), nil
}
