package reasoning

import (
	"regexp"
	"strings"

	"github.com/MAX-API-Next/MAX-API/setting/model_setting"
	"github.com/samber/lo"
)

var EffortSuffixes = []string{"-max", "-xhigh", "-high", "-medium", "-low", "-minimal"}

var OpenAIEffortSuffixes = []string{"-max", "-xhigh", "-high", "-minimal", "-low", "-medium", "-none"}

var openAIModelPattern = regexp.MustCompile(`^(gpt-[a-z0-9][a-z0-9._-]*|o[1-9][a-z0-9._-]*)$`)

func PreserveModelSuffix(modelName string) bool {
	bare := modelName[strings.LastIndex(modelName, "/")+1:]
	return model_setting.ShouldPreserveThinkingSuffix(modelName) || bare == "gpt-5.1-codex-max"
}

var DeepSeekV4EffortSuffixes = []string{"-none", "-max"}

// TrimEffortSuffix -> modelName level(low) exists
func TrimEffortSuffix(modelName string) (string, string, bool) {
	return TrimEffortSuffixWithSuffixes(modelName, EffortSuffixes)
}

func TrimEffortSuffixWithSuffixes(modelName string, suffixes []string) (string, string, bool) {
	suffix, found := lo.Find(suffixes, func(s string) bool {
		return strings.HasSuffix(modelName, s)
	})
	if !found {
		return modelName, "", false
	}
	return strings.TrimSuffix(modelName, suffix), strings.TrimPrefix(suffix, "-"), true
}

func ParseOpenAIReasoningEffortFromModelSuffix(modelName string) (string, string) {
	if PreserveModelSuffix(modelName) {
		return "", modelName
	}
	baseModel, effort, ok := TrimEffortSuffixWithSuffixes(modelName, OpenAIEffortSuffixes)
	if !ok || !openAIModelPattern.MatchString(baseModel[strings.LastIndex(baseModel, "/")+1:]) {
		return "", modelName
	}
	return effort, baseModel
}

func ParseDeepSeekV4ThinkingSuffix(modelName string) (baseModel string, thinkingType string, effort string, ok bool) {
	baseModel, suffix, ok := TrimEffortSuffixWithSuffixes(modelName, DeepSeekV4EffortSuffixes)
	if !ok || !strings.HasPrefix(baseModel, "deepseek-v4-") {
		return modelName, "", "", false
	}
	switch suffix {
	case "none":
		return baseModel, "disabled", "", true
	case "max":
		return baseModel, "enabled", "max", true
	default:
		return modelName, "", "", false
	}
}
