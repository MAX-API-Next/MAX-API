package siliconflow

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertImageRequestPreservesExplicitZeroProviderFields(t *testing.T) {
	request := dto.ImageRequest{
		Model:  "black-forest-labs/FLUX.1-schnell",
		Prompt: "draw",
		Extra: map[string]json.RawMessage{
			"batch_size":          []byte("0"),
			"seed":                []byte("0"),
			"num_inference_steps": []byte("0"),
			"guidance_scale":      []byte("0"),
			"cfg":                 []byte("0"),
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := (&Adaptor{}).ConvertImageRequest(c, &relaycommon.RelayInfo{}, request)
	require.NoError(t, err)

	data, err := common.Marshal(converted)
	require.NoError(t, err)
	var body map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(data, &body))
	for _, field := range []string{"batch_size", "seed", "num_inference_steps", "guidance_scale", "cfg"} {
		require.JSONEq(t, "0", string(body[field]), field)
	}
}
