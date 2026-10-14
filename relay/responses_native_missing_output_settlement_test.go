package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type nativeMissingOutputCase struct {
	name, estimate string
	frames         []map[string]any
}

func nativeMissingOutputCases() []nativeMissingOutputCase {
	delta := func(kind, text string) map[string]any {
		return map[string]any{"type": "response." + kind + ".delta", "delta": text, "item_id": "item_0", "output_index": 0}
	}
	item := func(event, kind, id, name, payload string, index int) map[string]any {
		output := map[string]any{"type": kind, "id": id, "name": name}
		if kind == "custom_tool_call" {
			output["input"] = payload
		} else {
			output["arguments"] = payload
		}
		return map[string]any{"type": "response.output_item." + event, "output_index": index, "item": output}
	}
	const arguments = `{"q":"Paris"}`
	const input = "print('generated output')"
	cases := []nativeMissingOutputCase{}
	for _, kind := range []string{"output_text", "reasoning_text", "reasoning_summary_text", "refusal", "function_call_arguments", "custom_tool_call_input"} {
		cases = append(cases, nativeMissingOutputCase{kind, "generated output", []map[string]any{delta(kind, "generated output")}})
	}
	cases = append(cases,
		nativeMissingOutputCase{"function_snapshot", "lookup" + arguments, []map[string]any{item("done", "function_call", "item_0", "lookup", arguments, 0)}},
		nativeMissingOutputCase{"function_snapshots_once", "lookup" + arguments, []map[string]any{
			item("added", "function_call", "item_0", "lookup", arguments, 0), item("done", "function_call", "item_0", "lookup", arguments, 0)}},
		nativeMissingOutputCase{"snapshot_retained_after_empty", "lookup" + arguments, []map[string]any{
			item("added", "function_call", "item_0", "lookup", arguments, 0), item("done", "function_call", "item_0", "lookup", "", 0)}},
		nativeMissingOutputCase{"function_object_arguments", "lookup" + arguments, []map[string]any{
			{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"type": "function_call", "id": "item_0", "name": "lookup", "arguments": map[string]any{"q": "Paris"}}}}},
		nativeMissingOutputCase{"terminal_function_snapshot", "lookup" + arguments, []map[string]any{
			{"type": "response.completed", "response": map[string]any{"id": "resp_missing", "status": "completed", "output": []any{item("done", "function_call", "item_0", "lookup", arguments, 0)["item"]}}}}},
		nativeMissingOutputCase{"function_delta_snapshot_once", "lookup" + arguments, []map[string]any{
			item("added", "function_call", "item_0", "lookup", "", 0), delta("function_call_arguments", arguments), item("done", "function_call", "item_0", "lookup", arguments, 0)}},
		nativeMissingOutputCase{"function_snapshot_completes_delta", "lookup" + arguments, []map[string]any{
			item("added", "function_call", "item_0", "lookup", "", 0), delta("function_call_arguments", `{"q":`), item("done", "function_call", "item_0", "lookup", arguments, 0)}},
		nativeMissingOutputCase{"custom_delta_snapshot_once", "python" + input, []map[string]any{
			item("added", "custom_tool_call", "item_0", "python", "", 0), delta("custom_tool_call_input", input), item("done", "custom_tool_call", "item_0", "python", input, 0)}},
		nativeMissingOutputCase{"tool_name_only", "lookup", []map[string]any{item("added", "function_call", "item_0", "lookup", "", 0)}},
		nativeMissingOutputCase{"function_arguments_done", "lookup" + arguments, []map[string]any{
			item("added", "function_call", "item_0", "lookup", "", 0),
			{"type": "response.function_call_arguments.done", "item_id": "item_0", "output_index": 0, "arguments": arguments}}},
		nativeMissingOutputCase{"custom_input_done", "python" + input, []map[string]any{
			item("added", "custom_tool_call", "item_0", "python", "", 0),
			{"type": "response.custom_tool_call_input.done", "item_id": "item_0", "output_index": 0, "input": input}}},
		nativeMissingOutputCase{"two_tools_zero_index", "lookup" + arguments + "python" + input, []map[string]any{
			item("added", "function_call", "item_0", "lookup", "", 0), delta("function_call_arguments", arguments),
			item("added", "custom_tool_call", "item_1", "python", "", 1),
			{"type": "response.custom_tool_call_input.delta", "item_id": "item_1", "delta": input},
			item("done", "function_call", "item_0", "lookup", arguments, 0), item("done", "custom_tool_call", "item_1", "python", input, 1)}},
		nativeMissingOutputCase{"mixed_text_tool", "visible text" + "lookup" + arguments, []map[string]any{
			delta("output_text", "visible text"), item("added", "function_call", "item_1", "lookup", "", 1),
			{"type": "response.function_call_arguments.delta", "output_index": 1, "item_id": "item_1", "delta": arguments}}},
		nativeMissingOutputCase{"large_delta_above_reservation", strings.Repeat("generated output ", 150), []map[string]any{delta("custom_tool_call_input", strings.Repeat("generated output ", 150))}},
	)
	return cases
}

// Fail one complete SSE frame, while allowing previously delivered frames.
type nativeMissingOutputWriter struct {
	*httptest.ResponseRecorder
	short, triggered bool
	failOn           string
	flush            bool
}

func (w *nativeMissingOutputWriter) Write(data []byte) (int, error) {
	failOn := w.failOn
	if failOn == "" {
		failOn = "unwritten-generated-output"
	}
	if !w.flush && strings.Contains(string(data), failOn) {
		w.triggered = true
		if w.short {
			return w.ResponseRecorder.Write(data[:3])
		}
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(data)
}

func (w *nativeMissingOutputWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *nativeMissingOutputWriter) FlushError() error {
	if w.flush && strings.Contains(w.Body.String(), w.failOn) {
		w.triggered = true
		return io.ErrClosedPipe
	}
	w.ResponseRecorder.Flush()
	return nil
}

func TestNativeResponsesTerminalTailRetainsOriginalFunding(t *testing.T) {
	initRequestReplayTests(t)
	oldAudit := common.LogResponseContentEnabled
	common.LogResponseContentEnabled = true
	t.Cleanup(func() { common.LogResponseContentEnabled = oldAudit })
	setFileSearchLedgerPriceForTest(t, 0.02)
	for _, event := range []string{"response.completed", "response.incomplete", "response.done"} {
		for _, funding := range streamFundingCases() {
			for _, usage := range []string{"missing", "known", "zero"} {
				t.Run(event+"/"+funding.name+"/"+usage, func(t *testing.T) {
					scenario := nativeMissingOutputCase{event, "accepted output", []map[string]any{
						{"type": "response.output_text.delta", "delta": "accepted output"},
						{"type": event, "response": map[string]any{"id": "resp_missing", "status": strings.TrimPrefix(event, "response.")}},
					}}
					runNativeMissingOutputLedger(t, scenario, funding, usage, "after_terminal", false)
				})
			}
		}
	}
	for _, funding := range streamFundingCases() {
		for _, failure := range []string{"terminal_write", "terminal_short", "terminal_flush"} {
			t.Run(funding.name+"/"+failure, func(t *testing.T) {
				scenario := nativeMissingOutputCase{"response.completed", "accepted output", []map[string]any{
					{"type": "response.output_text.delta", "delta": "accepted output"},
					{"type": "response.completed", "response": map[string]any{"id": "resp_missing", "status": "completed"}},
				}}
				runNativeMissingOutputLedger(t, scenario, funding, "known", failure, false)
			})
		}
	}
}

func TestNativeResponsesMissingOutputRetainsOriginalFunding(t *testing.T) {
	initRequestReplayTests(t)
	oldAudit := common.LogResponseContentEnabled
	common.LogResponseContentEnabled = true
	t.Cleanup(func() { common.LogResponseContentEnabled = oldAudit })
	for _, scenario := range nativeMissingOutputCases() {
		for _, funding := range streamFundingCases() {
			for _, usage := range []string{"missing", "known", "zero", "terminal_zero"} {
				t.Run(scenario.name+"/"+funding.name+"/"+usage, func(t *testing.T) {
					runNativeMissingOutputLedger(t, scenario, funding, usage, "eof", false)
				})
			}
		}
	}
	for _, ending := range []string{"malformed", "transport", "terminal_failed", "write", "short"} {
		for _, funding := range streamFundingCases() {
			for _, kind := range []string{"function_call_arguments", "custom_tool_call_input", "reasoning_summary_text", "refusal"} {
				scenario := nativeMissingOutputCase{kind, "generated output", []map[string]any{
					{"type": "response." + kind + ".delta", "item_id": "item_0", "output_index": 0, "delta": "generated output"},
				}}
				t.Run(kind+"/"+funding.name+"/missing/"+ending, func(t *testing.T) {
					runNativeMissingOutputLedger(t, scenario, funding, "missing", ending, false)
				})
				if ending == "write" || ending == "short" {
					t.Run(kind+"/"+funding.name+"/no_delivery/"+ending, func(t *testing.T) {
						runNativeMissingOutputLedger(t, scenario, funding, "missing", ending, true)
					})
				}
			}
		}
	}
	for _, funding := range streamFundingCases() {
		scenario := nativeMissingOutputCases()[4]
		t.Run("missing_function/"+funding.name+"/at_reservation", func(t *testing.T) {
			runNativeMissingOutputLedger(t, scenario, funding, "at_reservation", "eof", false)
		})
		for _, pricing := range []string{"free", "tiered_zero"} {
			scenario := nativeMissingOutputCases()[4]
			t.Run("missing_function/"+funding.name+"/"+pricing, func(t *testing.T) {
				runNativeMissingOutputLedger(t, scenario, funding, "missing", "eof", false, pricing)
			})
		}
	}
}

// Real HTTP -> native adaptor -> Helper -> durable funding and effect seam.
// Assertions cover exact amounts, original funding, logs, all mirrors and replay.
func runNativeMissingOutputLedger(t *testing.T, scenario nativeMissingOutputCase, funding streamFundingCase, usage, ending string, noDelivery bool, pricing ...string) {
	t.Helper()
	db := setupStreamFundingLedger(t, funding)
	response := map[string]any{"id": "resp_missing", "model": "gpt-test", "status": "in_progress", "output": []any{}}
	if usage == "known" {
		response["usage"] = map[string]any{"input_tokens": 4, "output_tokens": 2, "total_tokens": 6}
	} else if usage == "at_reservation" {
		response["usage"] = map[string]any{"input_tokens": 80, "output_tokens": 20, "total_tokens": 100}
	} else if usage == "zero" {
		response["usage"] = map[string]any{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}
	}
	encode := func(v any) string {
		data, err := common.Marshal(v)
		require.NoError(t, err)
		return string(data)
	}
	frames := []string{encode(map[string]any{"type": "response.created", "response": response})}
	hasTerminal := false
	if !noDelivery {
		for _, frame := range scenario.frames {
			if frame["type"] == "response.completed" || frame["type"] == "response.incomplete" || frame["type"] == "response.done" {
				hasTerminal = true
				if usage == "terminal_zero" {
					// A terminal snapshot supplies usage in that one terminal;
					// emitting a second response.completed violates the protocol.
					var terminalFrame map[string]any
					require.NoError(t, common.UnmarshalJsonStr(encode(frame), &terminalFrame))
					terminalFrame["response"].(map[string]any)["usage"] = map[string]any{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}
					frame = terminalFrame
				}
			}
			frames = append(frames, encode(frame))
		}
	}
	if usage == "terminal_zero" && !hasTerminal {
		frames = append(frames, `{"type":"response.completed","response":{"id":"resp_missing","status":"completed","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`)
	}
	switch ending {
	case "malformed":
		frames = append(frames, `{broken`)
	case "terminal_failed":
		frames = append(frames, `{"type":"response.failed","response":{"id":"resp_missing","status":"failed","error":{"code":"server_error","message":"synthetic failure"}}}`)
	case "write", "short":
		frames = append(frames, `{"type":"response.`+scenario.name+`.delta","item_id":"item_0","output_index":0,"delta":"unwritten-generated-output"}`)
	case "after_terminal", "terminal_write", "terminal_short", "terminal_flush":
		frames = append(frames,
			`{"type":"response.completed","response":{"id":"late_response","status":"completed","usage":{"input_tokens":1000,"output_tokens":999,"total_tokens":1999}}}`,
			`{"type":"response.output_item.done","item":{"type":"file_search_call","id":"late_tool","status":"completed"}}`,
			`{"type":"response.output_text.delta","delta":"late output"}`,
			`{"type":"response.failed","response":{"status":"failed","error":{"message":"late failure"}}}`,
			`{broken`)
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if ending == "transport" {
			w.Header().Set("Content-Length", "100000")
		}
		_, _ = fmt.Fprint(w, "data: "+strings.Join(frames, "\n\ndata: ")+"\n\n")
	}))
	t.Cleanup(upstream.Close)
	requestID := "native-missing-" + fmt.Sprintf("%x", len(t.Name())) + "-" + scenario.name
	c, info, recorder := streamFundingContext(t, "responses_native", requestID, upstream.URL, constant.ChannelTypeOpenAI, funding)
	if ending == "after_terminal" {
		info.Request.(*dto.OpenAIResponsesRequest).Tools = []byte(`[{"type":"file_search","vector_store_ids":["vs_synthetic"]}]`)
		info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
			dto.BuildInToolFileSearch: {ToolName: dto.BuildInToolFileSearch},
		}}
	}
	if len(pricing) > 0 {
		if pricing[0] == "free" {
			info.PriceData.ModelRatio = 0
		} else {
			info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ExprString: "0", ExprHash: billingexpr.ExprHashString("0"), GroupRatio: 1, QuotaPerUnit: common.QuotaPerUnit, EstimatedQuotaAfterGroup: 100}
		}
	}
	var writer *nativeMissingOutputWriter
	if ending == "write" || ending == "short" {
		writer = &nativeMissingOutputWriter{ResponseRecorder: recorder, short: ending == "short"}
	} else if strings.HasPrefix(ending, "terminal_") && ending != "terminal_failed" {
		writer = &nativeMissingOutputWriter{ResponseRecorder: recorder, failOn: "event: response.completed", short: ending == "terminal_short", flush: ending == "terminal_flush"}
	}
	if writer != nil {
		failed, _ := gin.CreateTestContext(writer)
		c.Writer = failed.Writer
	}
	require.Nil(t, service.PreConsumeBilling(c, 100, info))
	apiErr := ResponsesHelper(c, info)
	legalTerminal := usage == "terminal_zero" || hasTerminal
	if legalTerminal && (ending == "eof" || ending == "after_terminal") {
		require.Nil(t, apiErr)
	} else {
		require.NotNil(t, apiErr)
	}
	if writer != nil {
		require.True(t, writer.triggered)
		require.NotContains(t, recorder.Body.String(), "unwritten-generated-output")
	}
	if ending == "after_terminal" || strings.HasPrefix(ending, "terminal_") && ending != "terminal_failed" {
		require.NotContains(t, recorder.Body.String(), "late")
		require.NotContains(t, recorder.Body.String(), "1999")
	}
	if ending == "after_terminal" {
		require.Zero(t, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch].CallCount, "post-terminal tools must not add fees")
		require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: "+scenario.name+"\n"))
	}
	service.HandleFailedBilling(c, info, apiErr)
	service.HandleFailedBilling(c, info, apiErr)
	require.EqualValues(t, 1, calls.Load())
	prompt, completion := 10, service.CountTextToken(scenario.estimate, "gpt-test")
	if usage == "known" {
		prompt, completion = 4, 2
	} else if usage == "at_reservation" {
		prompt, completion = 80, 20
	} else if usage == "zero" || usage == "terminal_zero" || noDelivery {
		prompt, completion = 0, 0
	}
	quota := prompt + completion
	if scenario.name == "large_delta_above_reservation" && usage == "missing" {
		require.Greater(t, quota, 100, "exercise a positive funding delta, not just a refund")
	}
	if len(pricing) > 0 {
		quota = 0
	}
	var record model.BillingSettlement
	require.NoError(t, db.Where("operation_key = ?", model.BillingRequestFinalizeOperationKey(requestID)).First(&record).Error)
	require.Equal(t, funding.source, record.Source)
	require.Equal(t, model.BillingSettlementStatusApplied, record.Status)
	require.EqualValues(t, quota-100, record.FundingDelta)
	require.EqualValues(t, quota-100, record.TokenDelta)
	require.EqualValues(t, quota-100, record.AppliedFundingDelta)
	require.EqualValues(t, quota-100, record.AppliedTokenDelta)
	var effect *model.BillingSettlementEffect
	if !noDelivery {
		require.Equal(t, model.BillingSettlementEffectApplied, record.EffectStatus)
		effect = &model.BillingSettlementEffect{}
		require.NoError(t, common.UnmarshalJsonStr(record.EffectPayload, effect))
		require.Equal(t, prompt, effect.PromptTokens)
		require.Equal(t, completion, effect.CompletionTokens)
		require.EqualValues(t, quota, effect.Quota)
		require.True(t, effect.QuotaIsActual)
	}
	assertStreamFundingBalances(t, db, funding, requestID, quota, !noDelivery, prompt, completion)
	if ending == "eof" || ending == "after_terminal" {
		var audit strings.Builder
		for _, frame := range scenario.frames {
			if frame["type"] == "response.output_text.delta" {
				audit.WriteString(frame["delta"].(string))
			}
		}
		require.Equal(t, strings.TrimSpace(audit.String()), info.AuditResponseContent, "audit text must exclude tool arguments, refusal and reasoning estimates")
	}
	before := requestReplaySnapshot(t, db)
	for i := 0; i < 2; i++ {
		delta, alreadyApplied, err := model.ApplyBillingSettlementOnce(requestReplayInput(record, effect))
		require.NoError(t, err)
		require.True(t, alreadyApplied)
		require.EqualValues(t, quota-100, delta)
		if effect != nil {
			require.NoError(t, model.ProcessBillingSettlementEffect(record.OperationKey))
		}
		service.HandleFailedBilling(c, info, apiErr)
	}
	model.ProcessPendingBillingSettlementsOnce()
	require.Equal(t, before, requestReplaySnapshot(t, db), "replay/recovery must preserve every ledger table and original payload")
	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	if !noDelivery {
		require.Len(t, logs, 1)
		require.False(t, strings.Contains(logs[0].Content, "generated output"), "billing estimate must not copy tool or reasoning content into the audit log")
	}
}
