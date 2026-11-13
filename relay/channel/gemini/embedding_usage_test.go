package gemini

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEmbeddingAuthoritativeUsageAndMissingFallback(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, single := range []bool{false, true} {
			if !native && single {
				continue
			}
			for _, tc := range []struct {
				name, metadata string
				want           int
			}{
				{"reported", `,"usageMetadata":{"promptTokenCount":120,"totalTokenCount":120}`, 120},
				{"zero", `,"usageMetadata":{"promptTokenCount":0}`, 0},
				{"missing", "", 100}, {"empty", `,"usageMetadata":{}`, 100},
				{"negative", `,"usageMetadata":{"promptTokenCount":-1}`, -1},
			} {
				t.Run(tc.name+map[bool]string{true: "/native", false: "/compat"}[native]+map[bool]string{true: "/single", false: "/batch"}[single], func(t *testing.T) {
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
					info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "synthetic-embedding"}}
					info.IsGeminiBatchEmbedding = !single
					info.SetEstimatePromptTokens(100)
					vectors := `"embeddings":[{"values":[0.1]}]`
					if single {
						vectors = `"embedding":{"values":[0.1]}`
					}
					body := "{" + vectors + tc.metadata + "}"
					resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
					if native {
						usage, err := NativeGeminiEmbeddingHandler(c, resp, info)
						if tc.want < 0 {
							require.NotNil(t, err)
							return
						}
						require.Nil(t, err)
						require.Equal(t, tc.want, usage.PromptTokens)
						require.Equal(t, tc.want, usage.TotalTokens)
					} else {
						usage, err := GeminiEmbeddingHandler(c, info, resp)
						if tc.want < 0 {
							require.NotNil(t, err)
							return
						}
						require.Nil(t, err)
						require.Equal(t, tc.want, usage.PromptTokens)
						require.Equal(t, tc.want, usage.TotalTokens)
					}
				})
			}
		}
	}
}
