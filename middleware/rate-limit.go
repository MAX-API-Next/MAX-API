package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/gin-gonic/gin"
)

var inMemoryRateLimiter common.InMemoryRateLimiter

var timeFormat = "2006-01-02T15:04:05.000Z"

var defNext = func(c *gin.Context) {
	c.Next()
}

const rollingWindowRateLimitScript = `
local key = KEYS[1]
local duration = tonumber(ARGV[1])
local maximum = tonumber(ARGV[2])

if maximum <= 0 then
  return {1, 0}
end

local now = redis.call('TIME')
local nowMs = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
redis.call('ZREMRANGEBYSCORE', key, '-inf', nowMs - duration)

if redis.call('ZCARD', key) < maximum then
  local sequenceKey = key .. ':sequence'
  local sequence = redis.call('INCR', sequenceKey)
  redis.call('ZADD', key, nowMs, nowMs .. ':' .. sequence)
  local ttl = math.max(1, math.ceil(duration / 1000))
  redis.call('EXPIRE', key, ttl)
  redis.call('EXPIRE', sequenceKey, ttl)
  return {1, 0}
end

local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
local retryMs = duration - (nowMs - tonumber(oldest[2]))
if retryMs < 1000 then
  retryMs = 1000
end
return {0, math.ceil(retryMs / 1000)}
`

type rateLimitKeyFunc func(*gin.Context) (key string, policy string)

func rateLimitFactory(maxRequestNum int, duration int64, mark string) gin.HandlerFunc {
	return rateLimitFactoryWithKey(maxRequestNum, duration, func(c *gin.Context) (string, string) {
		return ipRateLimitKey(mark, c), mark
	})
}

func rateLimitFactoryWithKey(maxRequestNum int, duration int64, keyFunc rateLimitKeyFunc) gin.HandlerFunc {
	if maxRequestNum <= 0 {
		return defNext
	}
	return func(c *gin.Context) {
		key, policy := keyFunc(c)
		applyRateLimit(c, maxRequestNum, duration, key, policy)
	}
}

func ipRateLimitKey(policy string, c *gin.Context) string {
	return "rateLimit:" + policy + ":ip:" + c.ClientIP()
}

func applyRateLimit(c *gin.Context, maxRequestNum int, duration int64, key string, policy string) {
	if maxRequestNum <= 0 {
		return
	}
	if common.RedisEnabled {
		redisRateLimiter(c, maxRequestNum, duration, key, policy)
		return
	}

	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	allowed, retryAfter := inMemoryRateLimiter.RequestWithRetry(key, maxRequestNum, duration)
	if !allowed {
		abortRateLimited(c, maxRequestNum, retryAfter, policy)
	}
}

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string, policy string) {
	ctx := context.Background()
	result, err := common.RDB.Eval(ctx, rollingWindowRateLimitScript, []string{key}, duration*1000, maxRequestNum).Result()
	if err != nil {
		fmt.Println(err.Error())
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}

	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		fmt.Println("unexpected Redis rate limit response")
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	allowed, err := redisRateLimitInt(values[0])
	if err != nil {
		fmt.Println(err.Error())
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if allowed == 1 {
		return
	}
	retryAfter, err := redisRateLimitInt(values[1])
	if err != nil {
		fmt.Println(err.Error())
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if retryAfter < 1 {
		retryAfter = 1
	}
	abortRateLimited(c, maxRequestNum, time.Duration(retryAfter)*time.Second, policy)
}

func redisRateLimitInt(value interface{}) (int64, error) {
	switch value := value.(type) {
	case int64:
		return value, nil
	case int:
		return int64(value), nil
	case string:
		return strconv.ParseInt(value, 10, 64)
	case []byte:
		return strconv.ParseInt(string(value), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis rate limit value %T", value)
	}
}

func abortRateLimited(c *gin.Context, maximum int, retryAfter time.Duration, policy string) {
	retryAfterSeconds := int64(retryAfter / time.Second)
	if retryAfter%time.Second != 0 {
		retryAfterSeconds++
	}
	if retryAfterSeconds < 1 {
		retryAfterSeconds = 1
	}

	c.Header("Retry-After", strconv.FormatInt(retryAfterSeconds, 10))
	c.Header("X-RateLimit-Limit", strconv.Itoa(maximum))
	c.Header("X-RateLimit-Remaining", "0")
	c.Header("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Duration(retryAfterSeconds)*time.Second).Unix(), 10))
	c.Header("X-RateLimit-Policy", policy)
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"success": false,
		"message": fmt.Sprintf("Too many requests. Please try again in %d seconds.", retryAfterSeconds),
		"error": gin.H{
			"type": "rate_limit_error",
			"code": "rate_limit_exceeded",
		},
	})
}

func GlobalWebRateLimit() gin.HandlerFunc {
	if common.GlobalWebRateLimitEnable {
		return rateLimitFactory(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW")
	}
	return defNext
}

// WebFallbackRateLimit limits only SPA HTML fallbacks. Static files are served
// before this point and must not exhaust a browser's login-page request budget.
func WebFallbackRateLimit() gin.HandlerFunc {
	return GlobalWebRateLimit()
}

func isBootstrapStatusRequest(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet {
		return false
	}
	path := c.FullPath()
	if path == "" {
		path = c.Request.URL.Path
	}
	return path == "/api/status" || path == "/api/setup"
}

func GlobalAPIRateLimit() gin.HandlerFunc {
	if !common.GlobalApiRateLimitEnable {
		return defNext
	}
	limiter := rateLimitFactory(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA")
	return func(c *gin.Context) {
		if isBootstrapStatusRequest(c) {
			return
		}
		limiter(c)
	}
}

// PublicStatusRateLimit isolates the login bootstrap endpoints from unrelated
// public API traffic while retaining the configured global API protection.
func PublicStatusRateLimit() gin.HandlerFunc {
	if common.GlobalApiRateLimitEnable {
		return rateLimitFactory(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GS")
	}
	return defNext
}

func criticalRateLimitPolicy(c *gin.Context) string {
	route := c.FullPath()
	if route == "" {
		route = c.Request.URL.Path
	}
	return "CT:" + c.Request.Method + ":" + route
}

func CriticalRateLimit() gin.HandlerFunc {
	if !common.CriticalRateLimitEnable {
		return defNext
	}
	aggregateLimiter := rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
	routeLimiter := rateLimitFactoryWithKey(common.CriticalRouteRateLimitNum, common.CriticalRouteRateLimitDuration, func(c *gin.Context) (string, string) {
		policy := criticalRateLimitPolicy(c)
		return ipRateLimitKey(policy, c), policy
	})
	return func(c *gin.Context) {
		aggregateLimiter(c)
		if c.IsAborted() {
			return
		}
		routeLimiter(c)
	}
}

// LoginRateLimit uses a hashed account identifier. It is intentionally
// independent from CRITICAL_RATE_LIMIT so a shared NAT does not make unrelated
// users exceed the password-login quota together.
func LoginRateLimit() gin.HandlerFunc {
	if !common.LoginRateLimitEnable {
		return defNext
	}
	return func(c *gin.Context) {
		var request struct {
			Username string `json:"username"`
		}
		if err := common.UnmarshalBodyReusable(c, &request); err != nil {
			applyRateLimit(c, common.LoginRateLimitNum, common.LoginRateLimitDuration, ipRateLimitKey("LI:malformed", c), "LI:malformed")
			return
		}

		username := strings.TrimSpace(request.Username)
		if username == "" {
			applyRateLimit(c, common.LoginRateLimitNum, common.LoginRateLimitDuration, ipRateLimitKey("LI:anonymous", c), "LI:anonymous")
			return
		}
		digest := sha256.Sum256([]byte(strings.ToLower(username)))
		policy := "LI:account"
		key := "rateLimit:" + policy + ":" + hex.EncodeToString(digest[:])
		applyRateLimit(c, common.LoginRateLimitNum, common.LoginRateLimitDuration, key, policy)
	}
}

// UserCriticalRateLimit applies the critical-operation limit per authenticated
// user. Use it together with CriticalRateLimit so both account and IP bursts are bounded.
func UserCriticalRateLimit() gin.HandlerFunc {
	if !common.CriticalRateLimitEnable {
		return defNext
	}
	aggregateLimiter := userRateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
	routeLimiter := userRateLimitFactoryWithPolicy(common.CriticalRouteRateLimitNum, common.CriticalRouteRateLimitDuration, criticalRateLimitPolicy)
	return func(c *gin.Context) {
		aggregateLimiter(c)
		if c.IsAborted() {
			return
		}
		routeLimiter(c)
	}
}

func DownloadRateLimit() gin.HandlerFunc {
	return rateLimitFactory(common.DownloadRateLimitNum, common.DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() gin.HandlerFunc {
	return rateLimitFactory(common.UploadRateLimitNum, common.UploadRateLimitDuration, "UP")
}

// userRateLimitFactory creates a rate limiter keyed by authenticated user ID
// instead of client IP, making it resistant to proxy rotation attacks.
// Must be used AFTER authentication middleware (UserAuth).
func userRateLimitFactory(maxRequestNum int, duration int64, mark string) gin.HandlerFunc {
	return userRateLimitFactoryWithPolicy(maxRequestNum, duration, func(*gin.Context) string {
		return mark
	})
}

func userRateLimitFactoryWithPolicy(maxRequestNum int, duration int64, policyFunc func(*gin.Context) string) gin.HandlerFunc {
	if maxRequestNum <= 0 {
		return defNext
	}
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		policy := policyFunc(c)
		key := fmt.Sprintf("rateLimit:%s:user:%d", policy, userId)
		applyRateLimit(c, maxRequestNum, duration, key, policy)
	}
}

// SearchRateLimit returns a per-user rate limiter for search endpoints.
// Configurable via SEARCH_RATE_LIMIT_ENABLE / SEARCH_RATE_LIMIT / SEARCH_RATE_LIMIT_DURATION.
func SearchRateLimit() gin.HandlerFunc {
	if !common.SearchRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(common.SearchRateLimitNum, common.SearchRateLimitDuration, "SR")
}
