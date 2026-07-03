package taskcommon

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"

	"github.com/gin-gonic/gin"
)

func BuildConfiguredTaskPassThroughBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, bool, error) {
	if info == nil || info.ChannelMeta == nil || !UseConfiguredTaskProtocol(info.ChannelOtherSettings) ||
		!info.ChannelSetting.PassThroughBodyEnabled || !isJSONRequest(c) {
		return nil, false, nil
	}

	var raw map[string]any
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return nil, true, err
	}
	applyConfiguredModel(raw, info)
	data, err := common.Marshal(raw)
	if err != nil {
		return nil, true, err
	}
	return bytes.NewReader(data), true, nil
}

func ApplyTaskParamOverride(requestBody io.Reader, info *relaycommon.RelayInfo) (io.Reader, error) {
	if requestBody == nil || info == nil || len(info.ParamOverride) == 0 {
		return requestBody, nil
	}
	data, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return bytes.NewReader(data), nil
	}
	if !common.IsJsonObject(string(data)) {
		return bytes.NewReader(data), nil
	}
	data, err = applyTaskParamOverride(data, info)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func applyTaskParamOverride(data []byte, info *relaycommon.RelayInfo) ([]byte, error) {
	out, err := relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
	if err != nil {
		return nil, fmt.Errorf("apply param override failed: %w", err)
	}
	return out, nil
}

func applyConfiguredModel(raw map[string]any, info *relaycommon.RelayInfo) {
	if raw == nil || info == nil {
		return
	}
	if info.IsModelMapped {
		raw["model"] = info.UpstreamModelName
		return
	}
	if model, ok := raw["model"].(string); ok && strings.TrimSpace(model) != "" {
		info.UpstreamModelName = strings.TrimSpace(model)
	}
}

func isJSONRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type"))), "application/json")
}
