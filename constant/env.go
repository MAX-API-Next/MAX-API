package constant

const (
	DefaultStreamingTimeout                = 300
	DefaultMaxFileDownloadMB               = 64
	DefaultStreamScannerMaxBufferMB        = 128
	DefaultMaxRequestBodyMB                = 128
	DefaultAnonymousRequestBodyLimitKB     = 512
	DefaultAzureDefaultAPIVersion          = "2025-04-01-preview"
	DefaultNotifyLimitCount                = 2
	DefaultNotificationLimitDurationMinute = 10
	DefaultTaskQueryLimit                  = 1000
	DefaultTaskTimeoutMinutes              = 1440
)

var StreamingTimeout = DefaultStreamingTimeout
var DifyDebug = true
var MaxFileDownloadMB = DefaultMaxFileDownloadMB
var StreamScannerMaxBufferMB = DefaultStreamScannerMaxBufferMB
var ForceStreamOption = true
var CountToken = true
var GetMediaToken = true
var GetMediaTokenNotStream bool
var UpdateTask = true
var MaxRequestBodyMB = DefaultMaxRequestBodyMB
var AnonymousRequestBodyLimitKB = DefaultAnonymousRequestBodyLimitKB
var AzureDefaultAPIVersion = DefaultAzureDefaultAPIVersion
var NotifyLimitCount = DefaultNotifyLimitCount
var NotificationLimitDurationMinute = DefaultNotificationLimitDurationMinute
var GenerateDefaultToken bool
var ErrorLogEnabled bool
var TaskQueryLimit = DefaultTaskQueryLimit
var TaskTimeoutMinutes = DefaultTaskTimeoutMinutes

// temporary variable for sora patch, will be removed in future
var TaskPricePatches []string

// TrustedRedirectDomains is a list of trusted domains for redirect URL validation.
// Domains support subdomain matching (e.g., "example.com" matches "sub.example.com").
var TrustedRedirectDomains []string
