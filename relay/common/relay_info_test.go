package common

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestReasoningEffortFromRequest(t *testing.T) {
	chatReasoning, err := common.Marshal(map[string]any{"effort": " high "})
	require.NoError(t, err)
	tests := []struct {
		name    string
		request dto.Request
		want    string
	}{
		{name: "chat scalar", request: &dto.GeneralOpenAIRequest{ReasoningEffort: " low "}, want: "low"},
		{name: "chat object", request: &dto.GeneralOpenAIRequest{Reasoning: chatReasoning}, want: "high"},
		{name: "responses", request: &dto.OpenAIResponsesRequest{Reasoning: &dto.Reasoning{Effort: "medium"}}, want: "medium"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, reasoningEffortFromRequest(tc.request))
		})
	}
}

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestTaskSubmitReqPreservesExplicitZeroDuration(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{"model":"video-model","duration":0}`), &req))

	require.NotNil(t, req.Duration)
	require.Equal(t, 0, *req.Duration)

	data, err := common.Marshal(req)
	require.NoError(t, err)
	require.Contains(t, string(data), `"duration":0`)
}

func TestTaskSubmitReqResolvedSeconds(t *testing.T) {
	duration := 8
	req := TaskSubmitReq{
		Duration: &duration,
		Seconds:  "12",
	}

	seconds, err := req.ResolvedSeconds()
	require.NoError(t, err)
	require.Equal(t, 8, seconds)

	req = TaskSubmitReq{Seconds: "12"}
	seconds, err = req.ResolvedSeconds()
	require.NoError(t, err)
	require.Equal(t, 12, seconds)

	req = TaskSubmitReq{Seconds: "abc"}
	_, err = req.ResolvedSeconds()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid seconds value: abc")
}

func TestTaskSubmitReqResolvedSecondsOrDefault(t *testing.T) {
	zero := 0
	req := TaskSubmitReq{Duration: &zero}

	seconds, err := req.ResolvedSecondsOrDefault(5)
	require.NoError(t, err)
	require.Equal(t, 5, seconds)
}
