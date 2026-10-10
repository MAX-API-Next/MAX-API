package gemini

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func runGeminiResponsesRevalidation(t *testing.T, frames []string, custom bool) (*dto.Usage, *types.MaxAPIError, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{IsStream: true, DisablePing: true, OriginModelName: "gemini-test",
		RelayFormat: types.RelayFormatOpenAIResponses, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"}}
	info.SetEstimatePromptTokens(10)
	if custom {
		info.Request = &dto.OpenAIResponsesRequest{Tools: []byte(`[{"type":"custom","name":"lookup"}]`)}
	}
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))}
	usage, err := GeminiResponsesStreamHandler(c, info, resp)
	return usage, err, w.Body.String()
}

func TestGeminiResponsesToolSnapshotsEmitCompleteArgumentsOnce(t *testing.T) {
	for _, custom := range []bool{false, true} {
		for _, emptyTerminal := range []bool{false, true} {
			t.Run(fmt.Sprintf("custom_%t/empty_terminal_%t", custom, emptyTerminal), func(t *testing.T) {
				args := `{"q":"max"}`
				if custom {
					args = `{"input":"patch body"}`
				}
				completeCall := fmt.Sprintf(`{"id":"call-1","name":"lookup","args":%s,"willContinue":false}`, args)
				terminalCall := completeCall
				if emptyTerminal {
					terminalCall = `{"willContinue":false}`
				}
				frames := []string{
					fmt.Sprintf(`{"candidates":[{"index":0,"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":%s,"willContinue":true}}]}}]}`, args),
					fmt.Sprintf(`{"candidates":[{"index":0,"content":{"parts":[{"functionCall":%s}]}}]}`, terminalCall),
					fmt.Sprintf(`{"candidates":[{"index":0,"content":{"parts":[{"functionCall":%s}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`, completeCall),
				}
				_, apiErr, body := runGeminiResponsesRevalidation(t, frames, custom)
				require.Nil(t, apiErr)
				var completed gjson.Result
				var doneCount int
				for _, line := range strings.Split(body, "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					data := strings.TrimPrefix(line, "data: ")
					if gjson.Get(data, "type").String() == "response.completed" {
						completed = gjson.Get(data, "response")
					}
					if strings.HasSuffix(gjson.Get(data, "type").String(), "arguments.done") || strings.HasSuffix(gjson.Get(data, "type").String(), "input.done") {
						doneCount++
					}
				}
				require.True(t, completed.Exists(), body)
				require.Equal(t, 1, doneCount, body)
				require.Len(t, completed.Get("output").Array(), 1)
				item := completed.Get("output.0")
				require.Equal(t, "call-1", item.Get("call_id").String())
				if custom {
					require.Equal(t, "patch body", item.Get("input").String())
				} else {
					require.JSONEq(t, args, item.Get("arguments").String())
				}
			})
		}
	}
}

func TestGeminiResponsesUsageDistinguishesMissingNullAndZero(t *testing.T) {
	for _, input := range []struct {
		name, field string
		expected    int
	}{
		{"missing", "", 10}, {"null", `"promptTokenCount":null,`, 10}, {"zero", `"promptTokenCount":0,`, 0},
	} {
		t.Run(input.name, func(t *testing.T) {
			usage, err, _ := runGeminiResponsesRevalidation(t, []string{
				`{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{` + input.field + `"candidatesTokenCount":2}}`,
			}, false)
			require.Nil(t, err)
			require.Equal(t, input.expected, usage.PromptTokens)
			require.Equal(t, 2, usage.CompletionTokens)
			require.NotNil(t, usage.BillingUsage)
			require.Equal(t, input.expected, usage.BillingUsage.GeminiUsageMetadata.PromptTokenCount)
			require.Equal(t, input.expected != 0, usage.BillingUsage.Estimated)
		})
	}
}

func TestGeminiResponsesToolSnapshotsRejectConflictsAndIncompleteCalls(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		frames []string
	}{
		{"changed_arguments", []string{
			`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"a"}}}]}}]}`,
			`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"b"}}}]},"finishReason":"STOP"}]}`,
		}},
		{"changed_name", []string{
			`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{},"willContinue":true}}]}}]}`,
			`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"other","args":{},"willContinue":false}}]},"finishReason":"STOP"}]}`,
		}},
		{"unfinished_at_stop", []string{`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{},"willContinue":true}}]},"finishReason":"STOP"}]}`}},
		{"unfinished_at_eof", []string{`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{},"willContinue":true}}]}}]}`}},
		{"unfinished_after_other_candidate_stop", []string{
			`{"candidates":[{"index":0,"content":{"parts":[{"text":"partial"}]},"finishReason":"STOP"}]}`,
			`{"candidates":[{"index":1,"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{},"willContinue":true}}]}}]}`,
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err, body := runGeminiResponsesRevalidation(t, scenario.frames, false)
			require.NotNil(t, err)
			require.NotContains(t, body, "event: response.completed")
		})
	}
}

func TestGeminiResponsesIndependentCallsAndLatestPendingSnapshot(t *testing.T) {
	_, err, body := runGeminiResponsesRevalidation(t, []string{
		`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"old"},"willContinue":true}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"new"},"willContinue":false}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-2","name":"lookup","args":{"q":"second"}}}]},"finishReason":"STOP"}]}`,
	}, false)
	require.Nil(t, err)
	for _, line := range strings.Split(body, "\n") {
		data := strings.TrimPrefix(line, "data: ")
		if gjson.Get(data, "type").String() != "response.completed" {
			continue
		}
		output := gjson.Get(data, "response.output").Array()
		require.Len(t, output, 2)
		require.Equal(t, "call-1", output[0].Get("call_id").String())
		require.JSONEq(t, `{"q":"new"}`, output[0].Get("arguments").String())
		require.Equal(t, "call-2", output[1].Get("call_id").String())
		require.JSONEq(t, `{"q":"second"}`, output[1].Get("arguments").String())
		return
	}
	t.Fatal("missing response.completed: " + body)
}

func TestGeminiResponsesMissingOutputDoesNotReplaceNumericZeroInput(t *testing.T) {
	for _, field := range []string{"", `,"candidatesTokenCount":null`} {
		usage, err, _ := runGeminiResponsesRevalidation(t, []string{
			`{"candidates":[{"content":{"parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":0` + field + `}}`,
		}, false)
		require.Nil(t, err)
		require.Zero(t, usage.PromptTokens)
		require.Positive(t, usage.CompletionTokens)
		require.True(t, usage.BillingUsage.Estimated)
		require.Zero(t, usage.BillingUsage.GeminiUsageMetadata.PromptTokenCount)
	}
}

func TestGeminiChatToolSnapshotsFinishOnceAndRejectPendingEOF(t *testing.T) {
	for _, scenario := range []struct{ complete, combined bool }{{false, false}, {true, false}, {true, true}} {
		complete := scenario.complete
		t.Run(fmt.Sprintf("complete_%t/combined_%t", complete, scenario.combined), func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{IsStream: true, DisablePing: true, RelayFormat: types.RelayFormatOpenAI, OriginModelName: "gemini-test",
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"}}
			frames := []string{`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"max"},"willContinue":true}}]}}]}`}
			if complete {
				frames = append(frames,
					`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"max"},"willContinue":false}}]}}]}`,
					`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call-1","name":"lookup","args":{"q":"max"}}}]},"finishReason":"STOP"}]}`)
			}
			if scenario.combined {
				frames[1] = strings.Replace(frames[1], `"content":`, `"finishReason":"STOP","content":`, 1)
				frames = frames[:2]
			}
			_, apiErr := GeminiChatStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))})
			if !complete {
				require.NotNil(t, apiErr)
				return
			}
			require.Nil(t, apiErr)
			var arguments strings.Builder
			finishCount := 0
			for _, line := range strings.Split(w.Body.String(), "\n") {
				data := strings.TrimPrefix(line, "data: ")
				for _, choice := range gjson.Get(data, "choices").Array() {
					for _, call := range choice.Get("delta.tool_calls").Array() {
						arguments.WriteString(call.Get("function.arguments").String())
					}
					if choice.Get("finish_reason").Type == gjson.String {
						finishCount++
					}
				}
			}
			require.JSONEq(t, `{"q":"max"}`, arguments.String())
			require.Equal(t, 1, finishCount, w.Body.String())
		})
	}
}
