package controller

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/relay"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/taskcommon"
	"github.com/MAX-API-Next/MAX-API/setting/system_setting"
	"github.com/tidwall/gjson"
)

func getGeminiVideoURL(ctx context.Context, channel *model.Channel, task *model.Task, apiKey string) (string, error) {
	if channel == nil || task == nil {
		return "", fmt.Errorf("invalid channel or task")
	}

	if url := extractGeminiVideoURLFromTaskData(task); url != "" {
		return ensureAPIKey(url, apiKey), nil
	}

	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	adaptor := relay.GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channel.Type)))
	if adaptor == nil {
		return "", fmt.Errorf("gemini task adaptor not found")
	}

	if apiKey == "" {
		return "", fmt.Errorf("api key not available for task")
	}

	proxy := channel.GetSetting().Proxy
	query := taskcommon.WithTaskQueryContext(ctx, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
		"model":   task.Properties.UpstreamModelName,
	})
	resp, err := adaptor.FetchTask(baseURL, apiKey, taskcommon.WithTaskProtocolConfig(query, channel.GetOtherSettings()), proxy)
	if err != nil {
		return "", fmt.Errorf("fetch task failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("video status endpoint returned %d", resp.StatusCode)
	}
	body, err := readVideoProviderStatus(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read task response failed: %w", err)
	}

	taskInfo, parsed, parseErr := taskcommon.ParseConfiguredTaskResult(body, channel.GetOtherSettings())
	if parseErr != nil {
		return "", fmt.Errorf("parse configured task result failed: %w", parseErr)
	}
	if !parsed {
		taskInfo, parseErr = adaptor.ParseTaskResult(body)
	}
	if parseErr == nil && taskInfo != nil && taskInfo.RemoteUrl != "" {
		return ensureAPIKey(taskInfo.RemoteUrl, apiKey), nil
	}
	if parseErr == nil && taskInfo != nil && taskInfo.Url != "" {
		return ensureAPIKey(taskInfo.Url, apiKey), nil
	}

	if url := extractGeminiVideoURLFromPayload(body); url != "" {
		return ensureAPIKey(url, apiKey), nil
	}

	if parseErr != nil {
		return "", fmt.Errorf("parse task result failed: %w", parseErr)
	}

	return "", fmt.Errorf("gemini video url not found")
}

func extractGeminiVideoURLFromTaskData(task *model.Task) string {
	if task == nil || len(task.Data) == 0 {
		return ""
	}
	var payload map[string]any
	if err := common.Unmarshal(task.Data, &payload); err != nil {
		return ""
	}
	return extractGeminiVideoURLFromMap(payload)
}

func extractGeminiVideoURLFromPayload(body []byte) string {
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return extractGeminiVideoURLFromMap(payload)
}

func extractGeminiVideoURLFromMap(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if uri, ok := payload["uri"].(string); ok && uri != "" {
		return uri
	}
	if resp, ok := payload["response"].(map[string]any); ok {
		if uri := extractGeminiVideoURLFromResponse(resp); uri != "" {
			return uri
		}
	}
	return ""
}

func extractGeminiVideoURLFromResponse(resp map[string]any) string {
	if resp == nil {
		return ""
	}
	if gvr, ok := resp["generateVideoResponse"].(map[string]any); ok {
		if uri := extractGeminiVideoURLFromGeneratedSamples(gvr); uri != "" {
			return uri
		}
	}
	if videos, ok := resp["videos"].([]any); ok {
		for _, video := range videos {
			if vm, ok := video.(map[string]any); ok {
				if uri, ok := vm["uri"].(string); ok && uri != "" {
					return uri
				}
			}
		}
	}
	if uri, ok := resp["video"].(string); ok && uri != "" {
		return uri
	}
	if uri, ok := resp["uri"].(string); ok && uri != "" {
		return uri
	}
	return ""
}

func extractGeminiVideoURLFromGeneratedSamples(gvr map[string]any) string {
	if gvr == nil {
		return ""
	}
	if samples, ok := gvr["generatedSamples"].([]any); ok {
		for _, sample := range samples {
			if sm, ok := sample.(map[string]any); ok {
				if video, ok := sm["video"].(map[string]any); ok {
					if uri, ok := video["uri"].(string); ok && uri != "" {
						return uri
					}
				}
			}
		}
	}
	return ""
}

func getVertexVideoURL(ctx context.Context, channel *model.Channel, task *model.Task) (string, error) {
	if channel == nil || task == nil {
		return "", fmt.Errorf("invalid channel or task")
	}
	if url := strings.TrimSpace(task.GetResultURL()); url != "" && !isTaskProxyContentURL(url, task.TaskID) {
		return url, nil
	}
	if url := extractVertexVideoURLFromTaskData(task); url != "" {
		return url, nil
	}
	return fetchVideoTaskResultURL(ctx, channel, task)
}

func fetchVideoTaskResultURL(ctx context.Context, channel *model.Channel, task *model.Task) (string, error) {
	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	adaptor := relay.GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channel.Type)))
	if adaptor == nil {
		return "", fmt.Errorf("video task adaptor not found")
	}

	key := getVertexTaskKey(channel, task)
	if key == "" {
		return "", fmt.Errorf("video provider key not available for task")
	}

	query := taskcommon.WithTaskQueryContext(ctx, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
		"model":   task.Properties.UpstreamModelName,
	})
	settings := channel.GetOtherSettings()
	resp, err := adaptor.FetchTask(baseURL, key, taskcommon.WithTaskProtocolConfig(query, settings), channel.GetSetting().Proxy)
	if err != nil {
		return "", fmt.Errorf("fetch task failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("video status endpoint returned %d", resp.StatusCode)
	}
	body, err := readVideoProviderStatus(resp.Body)
	if err != nil {
		if errors.Is(err, errVideoProviderStatusTooLarge) {
			if chosen := configuredInlineVideoFromPrefix(body, settings); chosen != "" {
				return chosen, nil
			}
		}
		return "", fmt.Errorf("read task response failed: %w", err)
	}

	taskInfo, parsed, parseErr := taskcommon.ParseConfiguredTaskResult(body, settings)
	if parseErr != nil {
		return "", fmt.Errorf("parse configured task result failed: %w", parseErr)
	}
	if !parsed {
		taskInfo, parseErr = adaptor.ParseTaskResult(body)
	}
	if parseErr == nil && taskInfo != nil && strings.TrimSpace(taskInfo.Url) != "" {
		return taskInfo.Url, nil
	}
	if channel.Type == constant.ChannelTypeVertexAi {
		if url := extractVertexVideoURLFromPayload(body); url != "" {
			return url, nil
		}
	}
	if parseErr != nil {
		return "", fmt.Errorf("parse task result failed: %w", parseErr)
	}
	return "", fmt.Errorf("video result URL not found")
}

var errVideoProviderStatusTooLarge = errors.New("video status response exceeds size limit")

func readVideoProviderStatus(reader io.Reader) ([]byte, error) {
	limit := int64(base64.StdEncoding.EncodedLen(maxVideoDataURLDecodedBytes)) + (1 << 20)
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return body[:limit], errVideoProviderStatusTooLarge
	}
	return body, nil
}

// On a truncated status response, serve only a complete inline value at the
// explicitly preferred result path. Unrelated fields can follow the read limit.
func configuredInlineVideoFromPrefix(body []byte, settings dto.ChannelOtherSettings) string {
	if !taskcommon.UseConfiguredTaskProtocol(settings) || settings.TaskProtocolConfig == nil ||
		len(settings.TaskProtocolConfig.ResultURLPaths) == 0 {
		return ""
	}
	result := gjson.GetBytes(body, settings.TaskProtocolConfig.ResultURLPaths[0])
	if result.Type != gjson.String || !strings.HasPrefix(result.Raw, `"`) || !strings.HasPrefix(result.Str, "data:") ||
		!common.ValidJSON(common.StringToByteSlice(result.Raw)) {
		return ""
	}
	return result.Str
}

func isTaskProxyContentURL(raw string, taskID string) bool {
	result, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || taskID == "" {
		return false
	}
	if result.Scheme == "" && result.Host == "" {
		return result.Path == model.TaskVideoProxyPath(taskID)
	}
	bases := append([]string{system_setting.ServerAddress}, strings.Split(common.GetEnvOrDefaultString("TASK_VIDEO_PROXY_LEGACY_BASE_URLS", ""), ",")...)
	for _, base := range bases {
		local, err := url.Parse(strings.TrimRight(strings.TrimSpace(base), "/") + model.TaskVideoProxyPath(taskID))
		if err == nil && local.Host != "" && result.Path == local.Path &&
			strings.EqualFold(result.Scheme, local.Scheme) && strings.EqualFold(result.Host, local.Host) {
			return true
		}
	}
	return false
}

func getVertexTaskKey(channel *model.Channel, task *model.Task) string {
	if task != nil {
		if key := strings.TrimSpace(task.PrivateData.Key); key != "" {
			return key
		}
	}
	if channel == nil {
		return ""
	}
	keys := channel.GetKeys()
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			return key
		}
	}
	return strings.TrimSpace(channel.Key)
}

func extractVertexVideoURLFromTaskData(task *model.Task) string {
	if task == nil || len(task.Data) == 0 {
		return ""
	}
	return extractVertexVideoURLFromPayload(task.Data)
}

func extractVertexVideoURLFromPayload(body []byte) string {
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return ""
	}
	resp, ok := payload["response"].(map[string]any)
	if !ok || resp == nil {
		return ""
	}

	if videos, ok := resp["videos"].([]any); ok && len(videos) > 0 {
		if video, ok := videos[0].(map[string]any); ok && video != nil {
			if b64, _ := video["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
				mime, _ := video["mimeType"].(string)
				enc, _ := video["encoding"].(string)
				return buildVideoDataURL(mime, enc, b64)
			}
		}
	}
	if b64, _ := resp["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
		enc, _ := resp["encoding"].(string)
		return buildVideoDataURL("", enc, b64)
	}
	if video, _ := resp["video"].(string); strings.TrimSpace(video) != "" {
		if strings.HasPrefix(video, "data:") || strings.HasPrefix(video, "http://") || strings.HasPrefix(video, "https://") {
			return video
		}
		enc, _ := resp["encoding"].(string)
		return buildVideoDataURL("", enc, video)
	}
	return ""
}

func buildVideoDataURL(mimeType string, encoding string, base64Data string) string {
	mime := strings.TrimSpace(mimeType)
	if mime == "" {
		enc := strings.TrimSpace(encoding)
		if enc == "" {
			enc = "mp4"
		}
		if strings.Contains(enc, "/") {
			mime = enc
		} else {
			mime = "video/" + enc
		}
	}
	return "data:" + mime + ";base64," + base64Data
}

func ensureAPIKey(uri, key string) string {
	if key == "" || uri == "" {
		return uri
	}
	if strings.Contains(uri, "key=") {
		return uri
	}
	if strings.Contains(uri, "?") {
		return fmt.Sprintf("%s&key=%s", uri, key)
	}
	return fmt.Sprintf("%s?key=%s", uri, key)
}
