package ali

import (
	"encoding/json"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

func TestRequestOpenAI2AliPreservesOptionalTopP(t *testing.T) {
	tests := []struct {
		name string
		topP *float64
		want *float64
	}{
		{name: "omitted", topP: nil, want: nil},
		{name: "in range", topP: lo.ToPtr(0.5), want: lo.ToPtr(0.5)},
		{name: "zero", topP: lo.ToPtr(0.0), want: lo.ToPtr(0.01)},
		{name: "negative", topP: lo.ToPtr(-0.1), want: lo.ToPtr(0.01)},
		{name: "one", topP: lo.ToPtr(1.0), want: lo.ToPtr(0.99)},
		{name: "above one", topP: lo.ToPtr(1.2), want: lo.ToPtr(0.99)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestOpenAI2Ali(dto.GeneralOpenAIRequest{Model: "qwen-plus", TopP: tt.topP})
			assert.Equal(t, tt.want, got.TopP)
		})
	}
}

func TestAliProviderRequestsPreserveExplicitZeroValues(t *testing.T) {
	zeroFloat := 0.0
	zeroInt := 0
	zeroUint := uint64(0)
	falseValue := false
	empty := ""

	chatPayload, err := common.Marshal(AliParameters{
		TopP:              &zeroFloat,
		TopK:              &zeroInt,
		Seed:              &zeroUint,
		EnableSearch:      &falseValue,
		IncrementalOutput: &falseValue,
	})
	assert.NoError(t, err)
	assert.JSONEq(t, `{"top_p":0,"top_k":0,"seed":0,"enable_search":false,"incremental_output":false}`, string(chatPayload))

	imagePayload, err := common.Marshal(AliImageRequest{
		Model:          "wan",
		Input:          map[string]string{"prompt": "draw"},
		ResponseFormat: &empty,
		Parameters: AliImageParameters{
			Size:  &empty,
			N:     &zeroInt,
			Steps: &empty,
			Scale: &empty,
		},
	})
	assert.NoError(t, err)
	var imageBody map[string]json.RawMessage
	assert.NoError(t, common.Unmarshal(imagePayload, &imageBody))
	assert.JSONEq(t, `""`, string(imageBody["response_format"]))
	assert.JSONEq(t, `{"size":"","n":0,"steps":"","scale":""}`, string(imageBody["parameters"]))
}

func TestOAIImageToAliOmitsAbsentSize(t *testing.T) {
	var request dto.ImageRequest
	assert.NoError(t, common.Unmarshal([]byte(`{"model":"wanx-v1","prompt":"draw"}`), &request))
	assert.NotNil(t, request.Extra)

	converted, err := oaiImage2AliImageRequest(&relaycommon.RelayInfo{}, request, false)
	assert.NoError(t, err)
	payload, err := common.Marshal(converted.Parameters)
	assert.NoError(t, err)

	var parameters map[string]json.RawMessage
	assert.NoError(t, common.Unmarshal(payload, &parameters))
	assert.NotContains(t, parameters, "size")
}
