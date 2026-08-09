package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/logger"
	"github.com/MAX-API-Next/MAX-API/types"
)

const maxUpstreamErrorBodyBytes = 1 << 20

func MidjourneyErrorWrapper(code int, desc string) *dto.MidjourneyResponse {
	return &dto.MidjourneyResponse{
		Code:        code,
		Description: desc,
	}
}

func MidjourneyErrorWithStatusCodeWrapper(code int, desc string, statusCode int) *dto.MidjourneyResponseWithStatusCode {
	return &dto.MidjourneyResponseWithStatusCode{
		StatusCode: statusCode,
		Response:   *MidjourneyErrorWrapper(code, desc),
	}
}

//// OpenAIErrorWrapper wraps an error into an OpenAIErrorWithStatusCode
//func OpenAIErrorWrapper(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	text := err.Error()
//	lowerText := strings.ToLower(text)
//	if !strings.HasPrefix(lowerText, "get file base64 from url") && !strings.HasPrefix(lowerText, "mime type is not supported") {
//		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
//			common.SysLog(fmt.Sprintf("error: %s", text))
//			text = "请求上游地址失败"
//		}
//	}
//	openAIError := dto.OpenAIError{
//		Message: text,
//		Type:    "max_api_error",
//		Code:    code,
//	}
//	return &dto.OpenAIErrorWithStatusCode{
//		Error:      openAIError,
//		StatusCode: statusCode,
//	}
//}
//
//func OpenAIErrorWrapperLocal(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	openaiErr := OpenAIErrorWrapper(err, code, statusCode)
//	openaiErr.LocalError = true
//	return openaiErr
//}

func ClaudeErrorWrapper(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	text := err.Error()
	lowerText := strings.ToLower(text)
	if !strings.HasPrefix(lowerText, "get file base64 from url") {
		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
			common.SysLog(fmt.Sprintf("error: %s", text))
			text = "请求上游地址失败"
		}
	}
	claudeError := types.ClaudeError{
		Message: text,
		Type:    "max_api_error",
	}
	return &dto.ClaudeErrorWithStatusCode{
		Error:      claudeError,
		StatusCode: statusCode,
	}
}

func ClaudeErrorWrapperLocal(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	claudeErr := ClaudeErrorWrapper(err, code, statusCode)
	claudeErr.LocalError = true
	return claudeErr
}

func RelayErrorHandler(ctx context.Context, resp *http.Response, showBodyWhenFail bool) (maxApiErr *types.MaxAPIError) {
	maxApiErr = types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, resp.StatusCode)

	responseBody, truncated, err := readUpstreamErrorBody(resp.Body)
	if err != nil {
		return
	}
	CloseResponseBodyGracefully(resp)
	var errResponse dto.GeneralErrorResponse
	responseBodyText := string(responseBody)
	if truncated {
		responseBodyText += fmt.Sprintf("...[truncated after %d bytes]", maxUpstreamErrorBodyBytes)
	}
	responseBodyPreview := common.LocalLogPreview(responseBodyText)
	buildErrWithBody := func(message string) error {
		if message == "" {
			return fmt.Errorf("bad response status code %d, body: %s", resp.StatusCode, responseBodyText)
		}
		return fmt.Errorf("bad response status code %d, message: %s, body: %s", resp.StatusCode, message, responseBodyText)
	}

	err = common.Unmarshal(responseBody, &errResponse)
	if err != nil {
		if showBodyWhenFail {
			maxApiErr.Err = buildErrWithBody("")
		} else {
			logger.LogError(ctx, fmt.Sprintf("bad response status code %d, body: %s", resp.StatusCode, responseBodyPreview))
			maxApiErr.Err = fmt.Errorf("bad response status code %d", resp.StatusCode)
		}
		return
	}

	if common.GetJsonType(errResponse.Error) == "object" {
		// General format error (OpenAI, Anthropic, Gemini, etc.)
		oaiError := errResponse.TryToOpenAIError()
		if oaiError != nil {
			maxApiErr = types.WithOpenAIError(*oaiError, resp.StatusCode)
			if showBodyWhenFail {
				maxApiErr.Err = buildErrWithBody(maxApiErr.Error())
			}
			return
		}
	}
	maxApiErr = types.NewOpenAIError(errors.New(errResponse.ToMessage()), types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
	if showBodyWhenFail {
		maxApiErr.Err = buildErrWithBody(maxApiErr.Error())
	}
	return
}

func TaskErrorFromUpstreamResponse(ctx context.Context, resp *http.Response, code string) *dto.TaskError {
	if resp == nil {
		return TaskErrorWrapperLocal(errors.New("task upstream response is nil"), code, http.StatusBadGateway)
	}
	responseBody, truncated, err := readUpstreamErrorBody(resp.Body)
	CloseResponseBodyGracefully(resp)
	if err != nil {
		return TaskErrorWrapper(err, code, resp.StatusCode)
	}

	message := safeUpstreamTaskErrorMessage(responseBody)
	if message == "" {
		logger.LogError(ctx, fmt.Sprintf("task upstream bad response status code %d, body_bytes=%d, truncated=%t", resp.StatusCode, len(responseBody), truncated))
		message = fmt.Sprintf("bad upstream response status code %d", resp.StatusCode)
	} else if truncated {
		message += fmt.Sprintf("...[truncated after %d bytes]", maxUpstreamErrorBodyBytes)
	}
	message = common.SanitizePersistedLogContent(common.MaskSensitiveInfo(message))
	if message == "" {
		message = fmt.Sprintf("bad upstream response status code %d", resp.StatusCode)
	}
	return &dto.TaskError{
		Code:       code,
		Message:    message,
		StatusCode: resp.StatusCode,
		Error:      errors.New(message),
	}
}

func safeUpstreamTaskErrorMessage(responseBody []byte) string {
	var errResponse dto.GeneralErrorResponse
	if err := common.Unmarshal(responseBody, &errResponse); err != nil {
		return ""
	}
	if openAIError := errResponse.TryToOpenAIError(); openAIError != nil {
		return openAIError.Message
	}
	if errResponse.Message != "" {
		return errResponse.Message
	}
	if errResponse.Msg != "" {
		return errResponse.Msg
	}
	if errResponse.Err != "" {
		return errResponse.Err
	}
	if errResponse.ErrorMsg != "" {
		return errResponse.ErrorMsg
	}
	if errResponse.Detail != "" {
		return errResponse.Detail
	}
	if errResponse.Header.Message != "" {
		return errResponse.Header.Message
	}
	if errResponse.Response.Error.Message != "" {
		return errResponse.Response.Error.Message
	}
	if len(errResponse.Error) > 0 && common.GetJsonType(errResponse.Error) == "string" {
		var message string
		if err := common.Unmarshal(errResponse.Error, &message); err == nil {
			return message
		}
	}
	return ""
}

func readUpstreamErrorBody(body io.Reader) ([]byte, bool, error) {
	if body == nil {
		return nil, false, nil
	}
	responseBody, err := io.ReadAll(io.LimitReader(body, maxUpstreamErrorBodyBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(responseBody) <= maxUpstreamErrorBodyBytes {
		return responseBody, false, nil
	}
	return responseBody[:maxUpstreamErrorBodyBytes], true, nil
}

func ResetStatusCode(maxApiErr *types.MaxAPIError, statusCodeMappingStr string) {
	if maxApiErr == nil {
		return
	}
	maxApiErr.StatusCode = MapStatusCode(maxApiErr.StatusCode, statusCodeMappingStr)
}

func ResetTaskStatusCode(taskErr *dto.TaskError, statusCodeMappingStr string) {
	if taskErr == nil {
		return
	}
	mappedStatusCode := MapStatusCode(taskErr.StatusCode, statusCodeMappingStr)
	if mappedStatusCode == taskErr.StatusCode {
		return
	}
	if taskErr.UpstreamStatusCode == 0 {
		taskErr.UpstreamStatusCode = taskErr.StatusCode
	}
	taskErr.StatusCode = mappedStatusCode
}

func MapStatusCode(statusCode int, statusCodeMappingStr string) int {
	if statusCode == http.StatusOK {
		return statusCode
	}
	rawMapping, err := decodeStatusCodeMapping(statusCodeMappingStr)
	if err != nil {
		return statusCode
	}
	value, ok := rawMapping[strconv.Itoa(statusCode)]
	if !ok {
		return statusCode
	}
	mappedStatusCode, ok := parseStatusCodeMappingValue(value)
	if !ok || mappedStatusCode < http.StatusContinue || mappedStatusCode > 599 {
		return statusCode
	}
	return mappedStatusCode
}

func ValidateStatusCodeMapping(statusCodeMappingStr string) error {
	_, err := parseStatusCodeMapping(statusCodeMappingStr)
	return err
}

func parseStatusCodeMapping(statusCodeMappingStr string) (map[int]int, error) {
	rawMapping, err := decodeStatusCodeMapping(statusCodeMappingStr)
	if err != nil {
		return nil, err
	}

	statusCodeMapping := make(map[int]int, len(rawMapping))
	for source, value := range rawMapping {
		normalizedSource := strings.TrimSpace(source)
		sourceCode, err := strconv.Atoi(normalizedSource)
		if err != nil || sourceCode < http.StatusContinue || sourceCode > 599 || strconv.Itoa(sourceCode) != normalizedSource {
			return nil, fmt.Errorf("invalid source status code %q", source)
		}
		targetCode, ok := parseStatusCodeMappingValue(value)
		if !ok || targetCode < http.StatusContinue || targetCode > 599 {
			return nil, fmt.Errorf("invalid target status code for %q", source)
		}
		if _, exists := statusCodeMapping[sourceCode]; exists {
			return nil, fmt.Errorf("duplicate source status code %d", sourceCode)
		}
		statusCodeMapping[sourceCode] = targetCode
	}
	return statusCodeMapping, nil
}

func decodeStatusCodeMapping(statusCodeMappingStr string) (map[string]any, error) {
	trimmed := strings.TrimSpace(statusCodeMappingStr)
	if trimmed == "" || trimmed == "{}" {
		return map[string]any{}, nil
	}

	rawMapping := make(map[string]any)
	if err := common.Unmarshal([]byte(trimmed), &rawMapping); err != nil {
		return nil, fmt.Errorf("invalid JSON object: %w", err)
	}
	if rawMapping == nil {
		return nil, fmt.Errorf("mapping must be a JSON object")
	}
	return rawMapping, nil
}

func parseStatusCodeMappingValue(value any) (int, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return 0, false
		}
		statusCode, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return statusCode, true
	case float64:
		if v != math.Trunc(v) {
			return 0, false
		}
		return int(v), true
	case int:
		return v, true
	case json.Number:
		statusCode, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, false
		}
		return statusCode, true
	default:
		return 0, false
	}
}

func TaskErrorWrapperLocal(err error, code string, statusCode int) *dto.TaskError {
	openaiErr := TaskErrorWrapper(err, code, statusCode)
	openaiErr.LocalError = true
	return openaiErr
}

func TaskErrorWrapper(err error, code string, statusCode int) *dto.TaskError {
	text := err.Error()
	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
		common.SysLog(fmt.Sprintf("error: %s", text))
		//text = "请求上游地址失败"
		text = common.MaskSensitiveInfo(text)
	}
	//避免暴露内部错误
	taskError := &dto.TaskError{
		Code:       code,
		Message:    text,
		StatusCode: statusCode,
		Error:      err,
	}

	return taskError
}

// TaskErrorFromAPIError 将 PreConsumeBilling 返回的 MaxAPIError 转换为 TaskError。
func TaskErrorFromAPIError(apiErr *types.MaxAPIError) *dto.TaskError {
	if apiErr == nil {
		return nil
	}
	message := apiErr.Error()
	return &dto.TaskError{
		Code:       string(apiErr.GetErrorCode()),
		Message:    message,
		StatusCode: apiErr.StatusCode,
		Error:      apiErr.Err,
	}
}

func TaskErrorLocalFromAPIError(apiErr *types.MaxAPIError) *dto.TaskError {
	taskErr := TaskErrorFromAPIError(apiErr)
	if taskErr != nil {
		taskErr.LocalError = true
	}
	return taskErr
}
