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
	DefaultDifyDebug                       = true
	DefaultForceStreamOption               = true
	DefaultCountToken                      = true
	DefaultGetMediaToken                   = true
	DefaultGetMediaTokenNotStream          = false
	DefaultUpdateTask                      = true
	DefaultGenerateDefaultToken            = false
	DefaultErrorLogEnabled                 = false
)

var StreamingTimeout = DefaultStreamingTimeout
var DifyDebug = DefaultDifyDebug
var MaxFileDownloadMB = DefaultMaxFileDownloadMB
var StreamScannerMaxBufferMB = DefaultStreamScannerMaxBufferMB
var ForceStreamOption = DefaultForceStreamOption
var CountToken = DefaultCountToken
var GetMediaToken = DefaultGetMediaToken
var GetMediaTokenNotStream = DefaultGetMediaTokenNotStream
var UpdateTask = DefaultUpdateTask
var MaxRequestBodyMB = DefaultMaxRequestBodyMB
var AnonymousRequestBodyLimitKB = DefaultAnonymousRequestBodyLimitKB
var AzureDefaultAPIVersion = DefaultAzureDefaultAPIVersion
var NotifyLimitCount = DefaultNotifyLimitCount
var NotificationLimitDurationMinute = DefaultNotificationLimitDurationMinute
var GenerateDefaultToken = DefaultGenerateDefaultToken
var ErrorLogEnabled = DefaultErrorLogEnabled
var TaskQueryLimit = DefaultTaskQueryLimit
var TaskTimeoutMinutes = DefaultTaskTimeoutMinutes

// temporary variable for sora patch, will be removed in future
var TaskPricePatches []string

// TrustedRedirectDomains is a list of trusted domains for redirect URL validation.
// Domains support subdomain matching (e.g., "example.com" matches "sub.example.com").
var TrustedRedirectDomains []string
