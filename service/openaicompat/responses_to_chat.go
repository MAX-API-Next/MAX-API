package openaicompat

import (
	"errors"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
)

const (
	responsesEventCreated                  = "response.created"
	responsesEventCompleted                = "response.completed"
	responsesEventDone                     = "response.done"
	responsesEventIncomplete               = "response.incomplete"
	responsesEventOutputTextDelta          = "response.output_text.delta"
	responsesEventOutputItemAdded          = "response.output_item.added"
	responsesEventOutputItemDone           = "response.output_item.done"
	responsesEventFunctionArgsDelta        = "response.function_call_arguments.delta"
	responsesEventFunctionArgsDone         = "response.function_call_arguments.done"
	responsesEventReasoningSummaryDelta    = "response.reasoning_summary_text.delta"
	responsesEventReasoningSummaryDone     = "response.reasoning_summary_text.done"
	responsesOutputTypeFunctionCall        = "function_call"
	responsesOutputTypeCustomToolCall      = "custom_tool_call"
	responsesOutputTypeMessage             = "message"
	responsesOutputTypeReasoning           = "reasoning"
	responsesIncompleteReasonContentFilter = "content_filter"
	responsesIncompleteReasonMaxTokens     = "max_output_tokens"
)

func ResponsesFinishReasonFromStatus(resp *dto.OpenAIResponsesResponse) (string, bool) {
	if resp == nil {
		return "", false
	}
	if responseStatusString(resp) != "incomplete" {
		return "", false
	}
	reason := ""
	if resp.IncompleteDetails != nil {
		reason = strings.TrimSpace(resp.IncompleteDetails.Reason)
	}
	if reason == responsesIncompleteReasonContentFilter {
		return "content_filter", true
	}
	return "length", true
}

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}

	text := ExtractOutputTextFromResponses(resp)
	reasoning := ExtractReasoningTextFromResponses(resp)

	usage := UsageFromResponsesUsage(resp.Usage)
	created := resp.CreatedAt

	var toolCalls []dto.ToolCallResponse
	if len(resp.Output) > 0 {
		for _, out := range resp.Output {
			if !isResponsesToolOutputType(out.Type) {
				continue
			}
			name := strings.TrimSpace(out.Name)
			if name == "" {
				continue
			}
			callId := strings.TrimSpace(out.CallId)
			if callId == "" {
				callId = strings.TrimSpace(out.ID)
			}
			toolCalls = append(toolCalls, dto.ToolCallResponse{
				ID:   callId,
				Type: "function",
				Function: dto.FunctionResponse{
					Name:      name,
					Arguments: out.ArgumentsString(),
				},
			})
		}
	}

	finishReason := "stop"
	if mappedReason, ok := ResponsesFinishReasonFromStatus(resp); ok {
		finishReason = mappedReason
	} else if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	msg := dto.Message{
		Role:    "assistant",
		Content: text,
	}
	if reasoning != "" {
		msg.ReasoningContent = &reasoning
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
	}

	out := &dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   resp.Model,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: *usage,
	}

	return out, usage, nil
}

func UsageFromResponsesUsage(src *dto.Usage) *dto.Usage {
	usage := &dto.Usage{}
	if src == nil {
		return usage
	}
	if src.InputTokens != 0 {
		usage.PromptTokens = src.InputTokens
		usage.InputTokens = src.InputTokens
	}
	if src.OutputTokens != 0 {
		usage.CompletionTokens = src.OutputTokens
		usage.OutputTokens = src.OutputTokens
	}
	if src.TotalTokens != 0 {
		usage.TotalTokens = src.TotalTokens
	} else {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if src.InputTokensDetails != nil {
		usage.PromptTokensDetails.CachedTokens = src.InputTokensDetails.CachedTokens
		usage.PromptTokensDetails.ImageTokens = src.InputTokensDetails.ImageTokens
		usage.PromptTokensDetails.AudioTokens = src.InputTokensDetails.AudioTokens
	}
	if src.CompletionTokenDetails.ReasoningTokens != 0 {
		usage.CompletionTokenDetails.ReasoningTokens = src.CompletionTokenDetails.ReasoningTokens
	}
	return usage
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder

	// Prefer assistant message outputs.
	for _, out := range resp.Output {
		if out.Type != "message" {
			continue
		}
		if out.Role != "" && out.Role != "assistant" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" && c.Text != "" {
				sb.WriteString(c.Text)
			} else if c.Refusal != "" {
				sb.WriteString(c.Refusal)
			}
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}
	for _, out := range resp.Output {
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			} else if c.Refusal != "" {
				sb.WriteString(c.Refusal)
			}
		}
	}
	return sb.String()
}

func ExtractReasoningTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, out := range resp.Output {
		if out.Type != responsesOutputTypeReasoning {
			continue
		}
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	return sb.String()
}

func responseStatusString(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Status) == 0 {
		return ""
	}
	var status string
	_ = common.Unmarshal(resp.Status, &status)
	return strings.TrimSpace(status)
}

func isResponsesToolOutputType(outputType string) bool {
	return outputType == responsesOutputTypeFunctionCall || outputType == responsesOutputTypeCustomToolCall
}
