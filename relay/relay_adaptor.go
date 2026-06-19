package relay

import (
	"strconv"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/relay/channel"
	"github.com/MAX-API-Next/MAX-API/relay/channel/advancedcustom"
	"github.com/MAX-API-Next/MAX-API/relay/channel/ali"
	"github.com/MAX-API-Next/MAX-API/relay/channel/aws"
	"github.com/MAX-API-Next/MAX-API/relay/channel/baidu"
	"github.com/MAX-API-Next/MAX-API/relay/channel/baidu_v2"
	"github.com/MAX-API-Next/MAX-API/relay/channel/claude"
	"github.com/MAX-API-Next/MAX-API/relay/channel/cloudflare"
	"github.com/MAX-API-Next/MAX-API/relay/channel/codex"
	"github.com/MAX-API-Next/MAX-API/relay/channel/cohere"
	"github.com/MAX-API-Next/MAX-API/relay/channel/coze"
	"github.com/MAX-API-Next/MAX-API/relay/channel/deepseek"
	"github.com/MAX-API-Next/MAX-API/relay/channel/dify"
	"github.com/MAX-API-Next/MAX-API/relay/channel/gemini"
	"github.com/MAX-API-Next/MAX-API/relay/channel/jimeng"
	"github.com/MAX-API-Next/MAX-API/relay/channel/jina"
	"github.com/MAX-API-Next/MAX-API/relay/channel/minimax"
	"github.com/MAX-API-Next/MAX-API/relay/channel/mistral"
	"github.com/MAX-API-Next/MAX-API/relay/channel/mokaai"
	"github.com/MAX-API-Next/MAX-API/relay/channel/moonshot"
	"github.com/MAX-API-Next/MAX-API/relay/channel/ollama"
	"github.com/MAX-API-Next/MAX-API/relay/channel/openai"
	"github.com/MAX-API-Next/MAX-API/relay/channel/palm"
	"github.com/MAX-API-Next/MAX-API/relay/channel/perplexity"
	"github.com/MAX-API-Next/MAX-API/relay/channel/replicate"
	"github.com/MAX-API-Next/MAX-API/relay/channel/siliconflow"
	"github.com/MAX-API-Next/MAX-API/relay/channel/submodel"
	taskali "github.com/MAX-API-Next/MAX-API/relay/channel/task/ali"
	taskdoubao "github.com/MAX-API-Next/MAX-API/relay/channel/task/doubao"
	taskGemini "github.com/MAX-API-Next/MAX-API/relay/channel/task/gemini"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/hailuo"
	taskjimeng "github.com/MAX-API-Next/MAX-API/relay/channel/task/jimeng"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/kling"
	tasksora "github.com/MAX-API-Next/MAX-API/relay/channel/task/sora"
	"github.com/MAX-API-Next/MAX-API/relay/channel/task/suno"
	taskvertex "github.com/MAX-API-Next/MAX-API/relay/channel/task/vertex"
	taskVidu "github.com/MAX-API-Next/MAX-API/relay/channel/task/vidu"
	"github.com/MAX-API-Next/MAX-API/relay/channel/tencent"
	"github.com/MAX-API-Next/MAX-API/relay/channel/vertex"
	"github.com/MAX-API-Next/MAX-API/relay/channel/volcengine"
	"github.com/MAX-API-Next/MAX-API/relay/channel/xai"
	"github.com/MAX-API-Next/MAX-API/relay/channel/xunfei"
	"github.com/MAX-API-Next/MAX-API/relay/channel/zhipu"
	"github.com/MAX-API-Next/MAX-API/relay/channel/zhipu_4v"
	"github.com/gin-gonic/gin"
)

func GetAdaptor(apiType int) channel.Adaptor {
	switch apiType {
	case constant.APITypeAli:
		return &ali.Adaptor{}
	case constant.APITypeAnthropic:
		return &claude.Adaptor{}
	case constant.APITypeBaidu:
		return &baidu.Adaptor{}
	case constant.APITypeGemini:
		return &gemini.Adaptor{}
	case constant.APITypeOpenAI:
		return &openai.Adaptor{}
	case constant.APITypePaLM:
		return &palm.Adaptor{}
	case constant.APITypeTencent:
		return &tencent.Adaptor{}
	case constant.APITypeXunfei:
		return &xunfei.Adaptor{}
	case constant.APITypeZhipu:
		return &zhipu.Adaptor{}
	case constant.APITypeZhipuV4:
		return &zhipu_4v.Adaptor{}
	case constant.APITypeOllama:
		return &ollama.Adaptor{}
	case constant.APITypePerplexity:
		return &perplexity.Adaptor{}
	case constant.APITypeAws:
		return &aws.Adaptor{}
	case constant.APITypeCohere:
		return &cohere.Adaptor{}
	case constant.APITypeDify:
		return &dify.Adaptor{}
	case constant.APITypeJina:
		return &jina.Adaptor{}
	case constant.APITypeCloudflare:
		return &cloudflare.Adaptor{}
	case constant.APITypeSiliconFlow:
		return &siliconflow.Adaptor{}
	case constant.APITypeVertexAi:
		return &vertex.Adaptor{}
	case constant.APITypeMistral:
		return &mistral.Adaptor{}
	case constant.APITypeDeepSeek:
		return &deepseek.Adaptor{}
	case constant.APITypeMokaAI:
		return &mokaai.Adaptor{}
	case constant.APITypeVolcEngine:
		return &volcengine.Adaptor{}
	case constant.APITypeBaiduV2:
		return &baidu_v2.Adaptor{}
	case constant.APITypeOpenRouter:
		return &openai.Adaptor{}
	case constant.APITypeXinference:
		return &openai.Adaptor{}
	case constant.APITypeXai:
		return &xai.Adaptor{}
	case constant.APITypeCoze:
		return &coze.Adaptor{}
	case constant.APITypeJimeng:
		return &jimeng.Adaptor{}
	case constant.APITypeMoonshot:
		return &moonshot.Adaptor{} // Moonshot uses Claude API
	case constant.APITypeSubmodel:
		return &submodel.Adaptor{}
	case constant.APITypeMiniMax:
		return &minimax.Adaptor{}
	case constant.APITypeReplicate:
		return &replicate.Adaptor{}
	case constant.APITypeCodex:
		return &codex.Adaptor{}
	case constant.APITypeAdvancedCustom:
		return &advancedcustom.Adaptor{}
	}
	return nil
}

func GetTaskPlatform(c *gin.Context) constant.TaskPlatform {
	channelType := c.GetInt("channel_type")
	if channelType > 0 {
		return constant.TaskPlatform(strconv.Itoa(channelType))
	}
	return constant.TaskPlatform(c.GetString("platform"))
}

func GetTaskAdaptor(platform constant.TaskPlatform) channel.TaskAdaptor {
	switch platform {
	//case constant.APITypeAIProxyLibrary:
	//	return &aiproxy.Adaptor{}
	case constant.TaskPlatformSuno:
		return &suno.TaskAdaptor{}
	}
	if channelType, err := strconv.ParseInt(string(platform), 10, 64); err == nil {
		switch channelType {
		case constant.ChannelTypeAli:
			return &taskali.TaskAdaptor{}
		case constant.ChannelTypeKling:
			return &kling.TaskAdaptor{}
		case constant.ChannelTypeJimeng:
			return &taskjimeng.TaskAdaptor{}
		case constant.ChannelTypeVertexAi:
			return &taskvertex.TaskAdaptor{}
		case constant.ChannelTypeVidu:
			return &taskVidu.TaskAdaptor{}
		case constant.ChannelTypeDoubaoVideo, constant.ChannelTypeVolcEngine:
			return &taskdoubao.TaskAdaptor{}
		case constant.ChannelTypeSora, constant.ChannelTypeOpenAI:
			return &tasksora.TaskAdaptor{}
		case constant.ChannelTypeGemini:
			return &taskGemini.TaskAdaptor{}
		case constant.ChannelTypeMiniMax:
			return &hailuo.TaskAdaptor{}
		}
	}
	return nil
}
