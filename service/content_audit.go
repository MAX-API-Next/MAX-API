package service

import (
	"fmt"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

const Base64AuditPlaceholder = "[base64 omitted]"

func ResponseAuditEnabled() bool {
	return common.LogResponseContentEnabled
}

func contentAuditEnabled() bool {
	return common.LogRequestContentEnabled || common.LogResponseContentEnabled
}

func contentAuditLimit() int {
	if common.LogContentMaxCharacters < 1 {
		return 1
	}
	return common.LogContentMaxCharacters
}

func truncateAuditContent(content string) (string, bool) {
	content = strings.TrimSpace(content)
	limit := contentAuditLimit()
	if content == "" {
		return "", false
	}
	runes := []rune(content)
	if len(runes) <= limit {
		return content, false
	}
	return string(runes[:limit]), true
}

func SetRelayResponseAuditContent(info *relaycommon.RelayInfo, content string) {
	if info == nil || !common.LogResponseContentEnabled {
		return
	}
	content, truncated := truncateAuditContent(content)
	if content == "" {
		return
	}
	info.AuditResponseContent = content
	info.AuditResponseTruncated = truncated
}

func BuildTextResponseAuditContent(response *dto.OpenAITextResponse) string {
	if response == nil {
		return ""
	}
	lines := make([]string, 0, len(response.Choices))
	for _, choice := range response.Choices {
		parts := make([]string, 0, 4)
		if content := strings.TrimSpace(choice.Message.StringContent()); content != "" {
			parts = append(parts, content)
		}
		if reasoning := strings.TrimSpace(choice.Message.GetReasoningContent()); reasoning != "" {
			parts = append(parts, "reasoning: "+reasoning)
		}
		for _, tool := range choice.Message.ParseToolCalls() {
			if tool.Function.Name != "" || tool.Function.Arguments != "" {
				parts = append(parts, fmt.Sprintf("tool_call: %s %s", tool.Function.Name, tool.Function.Arguments))
			}
		}
		if len(parts) > 0 {
			lines = append(lines, strings.Join(parts, "\n"))
		}
	}
	return strings.Join(lines, "\n\n")
}

func BuildImageResponseAuditContent(response *dto.ImageResponse) string {
	if response == nil || len(response.Data) == 0 {
		return ""
	}
	lines := make([]string, 0, len(response.Data)*3)
	for index, image := range response.Data {
		prefix := fmt.Sprintf("image[%d]", index)
		if image.RevisedPrompt != "" {
			lines = append(lines, prefix+" revised_prompt: "+image.RevisedPrompt)
		}
		if image.Url != "" {
			lines = append(lines, prefix+" url: "+image.Url)
		}
		if image.B64Json != "" {
			lines = append(lines, prefix+" b64_json: "+Base64AuditPlaceholder)
		}
	}
	return strings.Join(lines, "\n")
}

func BuildGeminiResponseAuditContent(response *dto.GeminiChatResponse) string {
	return buildGeminiResponseAuditContent(response, true)
}

func BuildGeminiNonTextResponseAuditContent(response *dto.GeminiChatResponse) string {
	return buildGeminiResponseAuditContent(response, false)
}

func buildGeminiResponseAuditContent(response *dto.GeminiChatResponse, includeText bool) string {
	if response == nil || len(response.Candidates) == 0 {
		return ""
	}
	lines := make([]string, 0, len(response.Candidates))
	for _, candidate := range response.Candidates {
		parts := make([]string, 0, len(candidate.Content.Parts))
		for _, part := range candidate.Content.Parts {
			if includeText && part.Text != "" {
				parts = append(parts, part.Text)
			}
			if part.ExecutableCode != nil {
				parts = append(parts, fmt.Sprintf("code: %s\n%s", part.ExecutableCode.Language, part.ExecutableCode.Code))
			}
			if part.CodeExecutionResult != nil {
				parts = append(parts, "code_output: "+part.CodeExecutionResult.Output)
			}
			if part.FunctionCall != nil {
				parts = append(parts, stringifyAuditValue(part.FunctionCall))
			}
			if part.InlineData != nil {
				label := strings.TrimSpace(part.InlineData.MimeType)
				if label == "" {
					label = "media"
				}
				parts = append(parts, fmt.Sprintf("[%s %s]", label, Base64AuditPlaceholder))
			}
		}
		if len(parts) > 0 {
			lines = append(lines, strings.Join(parts, "\n"))
		}
	}
	return strings.Join(lines, "\n\n")
}

func BuildSimpleImageAuditContent(count int) string {
	if count <= 0 {
		return ""
	}
	if count == 1 {
		return "image: generated"
	}
	return fmt.Sprintf("images: generated %d", count)
}

func BuildRawJSONAuditContent(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var payload any
	if err := common.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	bytes, err := common.Marshal(redactAuditJSONValue(payload))
	if err != nil {
		return ""
	}
	return string(bytes)
}

func redactAuditJSONValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(v))
		for key, child := range v {
			if shouldRedactAuditJSONField(key, child) {
				redacted[key] = Base64AuditPlaceholder
				continue
			}
			redacted[key] = redactAuditJSONValue(child)
		}
		return redacted
	case []any:
		redacted := make([]any, len(v))
		for i, child := range v {
			redacted[i] = redactAuditJSONValue(child)
		}
		return redacted
	default:
		return value
	}
}

func shouldRedactAuditJSONField(key string, value any) bool {
	normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch normalized {
	case "b64json", "base64", "imagebase64", "bytesbase64encoded":
		return true
	case "data":
		text, ok := value.(string)
		return ok && isLikelyBase64Payload(text)
	default:
		return false
	}
}

func isLikelyBase64Payload(text string) bool {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "data:") && strings.Contains(text, ";base64,") {
		return true
	}
	if len(text) < 512 {
		return false
	}
	for _, r := range text {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '+' || r == '/' || r == '=':
		default:
			return false
		}
	}
	return true
}

func AppendContentAuditAdminInfo(info *relaycommon.RelayInfo, adminInfo map[string]interface{}) {
	if info == nil || adminInfo == nil || !contentAuditEnabled() {
		return
	}
	if common.LogRequestContentEnabled {
		content, truncated := truncateAuditContent(buildRequestAuditContent(info))
		if content != "" {
			adminInfo["request_content"] = content
			if truncated {
				adminInfo["request_content_truncated"] = true
			}
		}
	}
	if common.LogResponseContentEnabled && info.AuditResponseContent != "" {
		adminInfo["response_content"] = info.AuditResponseContent
		if info.AuditResponseTruncated {
			adminInfo["response_content_truncated"] = true
		}
	}
}

func buildRequestAuditContent(info *relaycommon.RelayInfo) string {
	if info == nil || info.Request == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if info.OriginModelName != "" {
		parts = append(parts, "model: "+info.OriginModelName)
	}
	if path := requestPathForAudit(info); path != "" {
		parts = append(parts, "path: "+path)
	}
	if text := requestTextForAudit(info.Request); text != "" {
		parts = append(parts, text)
	}
	if meta := info.Request.GetTokenCountMeta(); meta != nil && len(meta.Files) > 0 {
		parts = append(parts, fileSummaryForAudit(meta.Files))
	}
	return strings.Join(parts, "\n")
}

func requestPathForAudit(info *relaycommon.RelayInfo) string {
	if info == nil || info.RequestURLPath == "" {
		return ""
	}
	path := info.RequestURLPath
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	return strings.TrimSpace(path)
}

func requestTextForAudit(request dto.Request) string {
	if request == nil {
		return ""
	}
	switch req := request.(type) {
	case *dto.ImageRequest:
		return auditLinesFromPairs([][2]string{
			{"prompt", req.Prompt},
			{"size", req.Size},
			{"quality", req.Quality},
		})
	case *dto.ClaudeRequest:
		lines := make([]string, 0, len(req.Messages)+2)
		if req.Prompt != "" {
			lines = append(lines, "prompt: "+req.Prompt)
		}
		if system := stringifyAuditValue(req.System); system != "" {
			lines = append(lines, "system: "+system)
		}
		for _, message := range req.Messages {
			if text := strings.TrimSpace(message.GetStringContent()); text != "" {
				lines = append(lines, fmt.Sprintf("%s: %s", message.Role, text))
			}
		}
		return strings.Join(lines, "\n")
	default:
		meta := request.GetTokenCountMeta()
		if meta == nil {
			return ""
		}
		return strings.TrimSpace(meta.CombineText)
	}
}

func auditLinesFromPairs(pairs [][2]string) string {
	lines := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		value := strings.TrimSpace(pair[1])
		if value == "" {
			continue
		}
		lines = append(lines, pair[0]+": "+value)
	}
	return strings.Join(lines, "\n")
}

func stringifyAuditValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		bytes, err := common.Marshal(v)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(v))
		}
		return strings.TrimSpace(string(bytes))
	}
}

func fileSummaryForAudit(files []*types.FileMeta) string {
	if len(files) == 0 {
		return ""
	}
	counts := map[types.FileType]int{}
	for _, file := range files {
		if file == nil {
			continue
		}
		counts[file.FileType]++
	}
	if len(counts) == 0 {
		return fmt.Sprintf("attachments: %d", len(files))
	}
	parts := make([]string, 0, len(counts))
	for fileType, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", fileType, count))
	}
	return "attachments: " + strings.Join(parts, ", ")
}
