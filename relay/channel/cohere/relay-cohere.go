package cohere

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/relay/helper"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/MAX-API-Next/MAX-API/types"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/tidwall/gjson"
)

func requestOpenAI2Cohere(textRequest dto.GeneralOpenAIRequest) *CohereRequest {
	cohereReq := CohereRequest{
		Model:       textRequest.Model,
		ChatHistory: []ChatHistory{},
		Message:     "",
		Stream:      lo.FromPtrOr(textRequest.Stream, false),
		MaxTokens:   textRequest.GetMaxTokens(),
	}
	if common.CohereSafetySetting != "NONE" {
		cohereReq.SafetyMode = common.CohereSafetySetting
	}
	if cohereReq.MaxTokens == 0 {
		cohereReq.MaxTokens = 4000
	}
	for _, msg := range textRequest.Messages {
		if msg.Role == "user" {
			cohereReq.Message = msg.StringContent()
		} else {
			var role string
			if msg.Role == "assistant" {
				role = "CHATBOT"
			} else if msg.Role == "system" {
				role = "SYSTEM"
			} else {
				role = "USER"
			}
			cohereReq.ChatHistory = append(cohereReq.ChatHistory, ChatHistory{
				Role:    role,
				Message: msg.StringContent(),
			})
		}
	}

	return &cohereReq
}

func requestConvertRerank2Cohere(rerankRequest dto.RerankRequest) *CohereRerankRequest {
	topN := lo.FromPtrOr(rerankRequest.TopN, 1)
	if topN <= 0 {
		topN = 1
	}
	cohereReq := CohereRerankRequest{
		Query:           rerankRequest.Query,
		Documents:       rerankRequest.Documents,
		Model:           rerankRequest.Model,
		TopN:            topN,
		ReturnDocuments: true,
	}
	return &cohereReq
}

func stopReasonCohere2OpenAI(reason string) string {
	switch reason {
	case "COMPLETE":
		return "stop"
	case "MAX_TOKENS":
		return "max_tokens"
	default:
		return reason
	}
}

// The event renderer can hide errors and short writes. Retain the first failure
// so rejected text cannot enter the fallback completion estimate.
type cohereStreamWriteObserver struct {
	gin.ResponseWriter
	err error
}

func (w *cohereStreamWriteObserver) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.ResponseWriter.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func (w *cohereStreamWriteObserver) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *cohereStreamWriteObserver) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func cohereStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	writeObserver := &cohereStreamWriteObserver{ResponseWriter: c.Writer}
	c.Writer = writeObserver
	defer func() {
		if writeObserver.err == nil {
			c.Writer = writeObserver.ResponseWriter
		}
	}()
	responseId := helper.GetResponseID(c)
	createdTime := common.GetTimestamp()
	usage := &dto.Usage{}
	var promptReported, completionReported bool
	responseText := ""
	scanner := helper.NewStreamScanner(resp.Body)
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := strings.Index(string(data), "\n"); i >= 0 {
			return i + 1, data[0:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	})
	dataChan := make(chan string)
	stopChan := make(chan struct{}, 1)
	done := make(chan struct{})
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		defer func() {
			select {
			case stopChan <- struct{}{}:
			case <-done:
			}
		}()
		for scanner.Scan() {
			data := scanner.Text()
			select {
			case dataChan <- data:
			case <-done:
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case <-done:
				return
			default:
			}
			common.SysLog("error reading stream: " + err.Error())
		}
	}()
	helper.SetEventStreamHeaders(c)
	c.Stream(func(w io.Writer) bool {
		select {
		case data := <-dataChan:
			data = strings.TrimSuffix(data, "\r")
			var cohereResp CohereResponse
			err := json.Unmarshal([]byte(data), &cohereResp)
			if err != nil {
				common.SysLog("error unmarshalling stream response: " + err.Error())
				return true
			}
			var openaiResp dto.ChatCompletionsStreamResponse
			openaiResp.Id = responseId
			openaiResp.Created = createdTime
			openaiResp.Object = "chat.completion.chunk"
			openaiResp.Model = info.UpstreamModelName
			if cohereResp.IsFinished {
				finishReason := stopReasonCohere2OpenAI(cohereResp.FinishReason)
				openaiResp.Choices = []dto.ChatCompletionsStreamResponseChoice{
					{
						Delta:        dto.ChatCompletionsStreamResponseChoiceDelta{},
						Index:        0,
						FinishReason: &finishReason,
					},
				}
				if cohereResp.Response != nil {
					counts := gjson.Get(data, "response.meta.billed_units")
					if counts.Get("input_tokens").Type == gjson.Number {
						usage.PromptTokens = cohereResp.Response.Meta.BilledUnits.InputTokens
						promptReported = true
					}
					if counts.Get("output_tokens").Type == gjson.Number {
						usage.CompletionTokens = cohereResp.Response.Meta.BilledUnits.OutputTokens
						completionReported = true
					}
				}
			} else {
				openaiResp.Choices = []dto.ChatCompletionsStreamResponseChoice{
					{
						Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
							Role:    "assistant",
							Content: &cohereResp.Text,
						},
						Index: 0,
					},
				}
			}
			jsonStr, err := json.Marshal(openaiResp)
			if err != nil {
				common.SysLog("error marshalling stream response: " + err.Error())
				return true
			}
			err = helper.StringData(c, string(jsonStr))
			if err == nil {
				err = writeObserver.err
			}
			if err != nil {
				common.SysLog("error writing stream response: " + err.Error())
				return false
			}
			info.SetFirstResponseTime()
			if !cohereResp.IsFinished {
				responseText += cohereResp.Text
			}
			return true
		case <-stopChan:
			c.Render(-1, common.CustomEvent{Data: "data: [DONE]"})
			return false
		case <-c.Request.Context().Done():
			return false
		}
	})
	close(done)
	service.CloseResponseBodyGracefully(resp)
	<-producerDone
	if !promptReported || !completionReported {
		estimated := service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens())
		if !promptReported {
			usage.PromptTokens = estimated.PromptTokens
		}
		if !completionReported {
			usage.CompletionTokens = estimated.CompletionTokens
		}
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return usage, nil
}

func cohereHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.MaxAPIError) {
	createdTime := common.GetTimestamp()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.CloseResponseBodyGracefully(resp)
	var cohereResp CohereResponseResult
	err = json.Unmarshal(responseBody, &cohereResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	usage := dto.Usage{}
	usage.PromptTokens = cohereResp.Meta.BilledUnits.InputTokens
	usage.CompletionTokens = cohereResp.Meta.BilledUnits.OutputTokens
	usage.TotalTokens = cohereResp.Meta.BilledUnits.InputTokens + cohereResp.Meta.BilledUnits.OutputTokens

	var openaiResp dto.TextResponse
	openaiResp.Id = cohereResp.ResponseId
	openaiResp.Created = createdTime
	openaiResp.Object = "chat.completion"
	openaiResp.Model = info.UpstreamModelName
	openaiResp.Usage = usage

	openaiResp.Choices = []dto.OpenAITextResponseChoice{
		{
			Index:        0,
			Message:      dto.Message{Content: cohereResp.Text, Role: "assistant"},
			FinishReason: stopReasonCohere2OpenAI(cohereResp.FinishReason),
		},
	}

	jsonResponse, err := json.Marshal(openaiResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, writeErr := c.Writer.Write(jsonResponse); writeErr != nil {
		common.SysLog("failed to write Cohere response: " + writeErr.Error())
	} else {
		info.SetFirstResponseTime()
	}
	return &usage, nil
}

func cohereRerankHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.MaxAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.CloseResponseBodyGracefully(resp)
	var cohereResp CohereRerankResponseResult
	err = common.Unmarshal(responseBody, &cohereResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	usage := dto.Usage{PromptTokens: cohereResp.Meta.BilledUnits.InputTokens, CompletionTokens: cohereResp.Meta.BilledUnits.OutputTokens}
	if gjson.GetBytes(responseBody, "meta.billed_units.input_tokens").Type != gjson.Number {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	var rerankResp dto.RerankResponse
	rerankResp.Results = cohereResp.Results
	rerankResp.Usage = usage

	jsonResponse, err := json.Marshal(rerankResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, err = c.Writer.Write(jsonResponse)
	if err != nil {
		common.SysLog("failed to write Cohere rerank response: " + err.Error())
	} else {
		info.SetFirstResponseTime()
	}
	return &usage, nil
}
