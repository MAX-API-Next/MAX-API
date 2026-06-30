package constant

import "testing"

func TestEnvironmentBackedDefaultsAreInitialized(t *testing.T) {
	checks := map[string]bool{
		"StreamingTimeout":                StreamingTimeout == DefaultStreamingTimeout,
		"DifyDebug":                       DifyDebug,
		"MaxFileDownloadMB":               MaxFileDownloadMB == DefaultMaxFileDownloadMB,
		"StreamScannerMaxBufferMB":        StreamScannerMaxBufferMB == DefaultStreamScannerMaxBufferMB,
		"ForceStreamOption":               ForceStreamOption,
		"CountToken":                      CountToken,
		"GetMediaToken":                   GetMediaToken,
		"GetMediaTokenNotStream":          !GetMediaTokenNotStream,
		"UpdateTask":                      UpdateTask,
		"MaxRequestBodyMB":                MaxRequestBodyMB == DefaultMaxRequestBodyMB,
		"AnonymousRequestBodyLimitKB":     AnonymousRequestBodyLimitKB == DefaultAnonymousRequestBodyLimitKB,
		"AzureDefaultAPIVersion":          AzureDefaultAPIVersion == DefaultAzureDefaultAPIVersion,
		"NotifyLimitCount":                NotifyLimitCount == DefaultNotifyLimitCount,
		"NotificationLimitDurationMinute": NotificationLimitDurationMinute == DefaultNotificationLimitDurationMinute,
		"GenerateDefaultToken":            !GenerateDefaultToken,
		"ErrorLogEnabled":                 !ErrorLogEnabled,
		"TaskQueryLimit":                  TaskQueryLimit == DefaultTaskQueryLimit,
		"TaskTimeoutMinutes":              TaskTimeoutMinutes == DefaultTaskTimeoutMinutes,
	}
	for name, ok := range checks {
		if !ok {
			t.Fatalf("%s package default does not match expected init default", name)
		}
	}
}
