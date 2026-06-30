package constant

import "testing"

func TestEnvironmentBackedDefaultsAreInitialized(t *testing.T) {
	checks := map[string]bool{
		"StreamingTimeout":                StreamingTimeout == DefaultStreamingTimeout,
		"DifyDebug":                       DifyDebug == DefaultDifyDebug,
		"MaxFileDownloadMB":               MaxFileDownloadMB == DefaultMaxFileDownloadMB,
		"StreamScannerMaxBufferMB":        StreamScannerMaxBufferMB == DefaultStreamScannerMaxBufferMB,
		"ForceStreamOption":               ForceStreamOption == DefaultForceStreamOption,
		"CountToken":                      CountToken == DefaultCountToken,
		"GetMediaToken":                   GetMediaToken == DefaultGetMediaToken,
		"GetMediaTokenNotStream":          GetMediaTokenNotStream == DefaultGetMediaTokenNotStream,
		"UpdateTask":                      UpdateTask == DefaultUpdateTask,
		"MaxRequestBodyMB":                MaxRequestBodyMB == DefaultMaxRequestBodyMB,
		"AnonymousRequestBodyLimitKB":     AnonymousRequestBodyLimitKB == DefaultAnonymousRequestBodyLimitKB,
		"AzureDefaultAPIVersion":          AzureDefaultAPIVersion == DefaultAzureDefaultAPIVersion,
		"NotifyLimitCount":                NotifyLimitCount == DefaultNotifyLimitCount,
		"NotificationLimitDurationMinute": NotificationLimitDurationMinute == DefaultNotificationLimitDurationMinute,
		"GenerateDefaultToken":            GenerateDefaultToken == DefaultGenerateDefaultToken,
		"ErrorLogEnabled":                 ErrorLogEnabled == DefaultErrorLogEnabled,
		"TaskQueryLimit":                  TaskQueryLimit == DefaultTaskQueryLimit,
		"TaskTimeoutMinutes":              TaskTimeoutMinutes == DefaultTaskTimeoutMinutes,
	}
	for name, ok := range checks {
		if !ok {
			t.Fatalf("%s package default does not match expected init default", name)
		}
	}
}
