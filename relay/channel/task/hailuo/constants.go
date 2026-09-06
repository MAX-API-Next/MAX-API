package hailuo

import (
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
)

const (
	ChannelName = "hailuo-video"
)

var ModelList = []string{
	H3Model,
	"MiniMax-Hailuo-2.3",
	"MiniMax-Hailuo-2.3-Fast",
	"MiniMax-Hailuo-02",
	"T2V-01-Director",
	"T2V-01",
	"I2V-01-Director",
	"I2V-01-live",
	"I2V-01",
	"S2V-01",
}

const (
	TextToVideoEndpoint   = "/v1/video_generation"
	QueryTaskEndpoint     = "/v1/query/video_generation"
	H3Model               = constant.TaskModelMiniMaxH3
	H3MaxModel            = "MiniMax-H3-Max"
	H3TextToVideoEndpoint = "/v2/video_generation"
	H3QueryTaskEndpoint   = "/v2/query/video_generation"
)

const (
	StatusSuccess    = 0
	StatusRateLimit  = 1002
	StatusAuthFailed = 1004
	StatusNoBalance  = 1008
	StatusSensitive  = 1026
	StatusParamError = 2013
	StatusInvalidKey = 2049
)

const (
	TaskStatusPreparing  = "Preparing"
	TaskStatusQueueing   = "Queueing"
	TaskStatusProcessing = "Processing"
	TaskStatusSuccess    = "Success"
	TaskStatusFailed     = "Fail"
)

const (
	Resolution512P  = "512P"
	Resolution720P  = "720P"
	Resolution768P  = "768P"
	Resolution1080P = "1080P"
	Resolution2K    = "2K"
)

const (
	DefaultDuration        = 6
	DefaultResolution      = Resolution720P
	H3DefaultDuration      = 5
	H3MinDuration          = task_billing_setting.H3MinOutputDurationSeconds
	H3MaxDuration          = task_billing_setting.H3MaxOutputDurationSeconds
	H3MaxInputVideoSeconds = task_billing_setting.H3MaxInputMediaDurationSeconds
	H3MaxImages            = task_billing_setting.H3MaxInputImageCount
	H3MaxVideos            = task_billing_setting.H3MaxInputVideoCount
	H3MaxAudios            = task_billing_setting.H3MaxInputAudioCount
)
