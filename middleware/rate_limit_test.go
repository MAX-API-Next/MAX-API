package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

var rateLimitTestRun uint64

func TestCriticalRateLimitUsesSeparateRouteBuckets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testRun := atomic.AddUint64(&rateLimitTestRun, 1)

	oldRedisEnabled := common.RedisEnabled
	oldCriticalRateLimitEnable := common.CriticalRateLimitEnable
	oldCriticalRateLimitNum := common.CriticalRateLimitNum
	oldCriticalRateLimitDuration := common.CriticalRateLimitDuration
	oldCriticalRouteRateLimitNum := common.CriticalRouteRateLimitNum
	oldCriticalRouteRateLimitDuration := common.CriticalRouteRateLimitDuration
	common.RedisEnabled = false
	common.CriticalRateLimitEnable = true
	common.CriticalRateLimitNum = 3
	common.CriticalRateLimitDuration = 60
	common.CriticalRouteRateLimitNum = 1
	common.CriticalRouteRateLimitDuration = 60
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.CriticalRateLimitEnable = oldCriticalRateLimitEnable
		common.CriticalRateLimitNum = oldCriticalRateLimitNum
		common.CriticalRateLimitDuration = oldCriticalRateLimitDuration
		common.CriticalRouteRateLimitNum = oldCriticalRouteRateLimitNum
		common.CriticalRouteRateLimitDuration = oldCriticalRouteRateLimitDuration
	})

	engine := gin.New()
	engine.POST("/login", CriticalRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	engine.POST("/reset-password", CriticalRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = fmt.Sprintf("[2001:db8:%x::1]:1234", testRun)
		engine.ServeHTTP(recorder, req)
		return recorder
	}

	require.Equal(t, http.StatusNoContent, request("/login").Code)
	require.Equal(t, http.StatusNoContent, request("/reset-password").Code)
	limited := request("/login")
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.Equal(t, "CT:POST:/login", limited.Header().Get("X-RateLimit-Policy"))
}

func TestCriticalRateLimitEnforcesAggregateIPBucket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testRun := atomic.AddUint64(&rateLimitTestRun, 1)

	oldRedisEnabled := common.RedisEnabled
	oldCriticalRateLimitEnable := common.CriticalRateLimitEnable
	oldCriticalRateLimitNum := common.CriticalRateLimitNum
	oldCriticalRateLimitDuration := common.CriticalRateLimitDuration
	oldCriticalRouteRateLimitNum := common.CriticalRouteRateLimitNum
	oldCriticalRouteRateLimitDuration := common.CriticalRouteRateLimitDuration
	common.RedisEnabled = false
	common.CriticalRateLimitEnable = true
	common.CriticalRateLimitNum = 1
	common.CriticalRateLimitDuration = 60
	common.CriticalRouteRateLimitNum = 10
	common.CriticalRouteRateLimitDuration = 60
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.CriticalRateLimitEnable = oldCriticalRateLimitEnable
		common.CriticalRateLimitNum = oldCriticalRateLimitNum
		common.CriticalRateLimitDuration = oldCriticalRateLimitDuration
		common.CriticalRouteRateLimitNum = oldCriticalRouteRateLimitNum
		common.CriticalRouteRateLimitDuration = oldCriticalRouteRateLimitDuration
	})

	engine := gin.New()
	engine.POST("/login", CriticalRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	engine.POST("/reset-password", CriticalRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = fmt.Sprintf("[2001:db8:%x::5]:1234", testRun)
		engine.ServeHTTP(recorder, req)
		return recorder
	}

	require.Equal(t, http.StatusNoContent, request("/login").Code)
	limited := request("/reset-password")
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.Equal(t, "CT", limited.Header().Get("X-RateLimit-Policy"))
}

func TestLoginRateLimitIsPerAccountInsteadOfSharedIPBucket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testRun := atomic.AddUint64(&rateLimitTestRun, 1)

	oldRedisEnabled := common.RedisEnabled
	oldLoginRateLimitEnable := common.LoginRateLimitEnable
	oldLoginRateLimitNum := common.LoginRateLimitNum
	oldLoginRateLimitDuration := common.LoginRateLimitDuration
	common.RedisEnabled = false
	common.LoginRateLimitEnable = true
	common.LoginRateLimitNum = 1
	common.LoginRateLimitDuration = 60
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.LoginRateLimitEnable = oldLoginRateLimitEnable
		common.LoginRateLimitNum = oldLoginRateLimitNum
		common.LoginRateLimitDuration = oldLoginRateLimitDuration
	})

	engine := gin.New()
	engine.POST("/login", LoginRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := func(username string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(fmt.Sprintf(`{"username":%q,"password":"not-recorded"}`, username)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = fmt.Sprintf("[2001:db8:%x::2]:1234", testRun)
		engine.ServeHTTP(recorder, req)
		return recorder
	}

	require.Equal(t, http.StatusNoContent, request("alice").Code)
	require.Equal(t, http.StatusNoContent, request("bob").Code)
	require.Equal(t, http.StatusTooManyRequests, request("alice").Code)
}

func TestRateLimitResponseIncludesRetryInformation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testRun := atomic.AddUint64(&rateLimitTestRun, 1)

	oldRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
	})

	engine := gin.New()
	engine.GET("/", rateLimitFactory(1, 60, fmt.Sprintf("test-%d", testRun)), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	request := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = fmt.Sprintf("[2001:db8:%x::3]:1234", testRun)
		engine.ServeHTTP(recorder, req)
		return recorder
	}

	require.Equal(t, http.StatusNoContent, request().Code)
	limited := request()
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.NotEmpty(t, limited.Header().Get("Retry-After"))
	require.Equal(t, "0", limited.Header().Get("X-RateLimit-Remaining"))
	require.Contains(t, limited.Body.String(), "rate_limit_exceeded")
}

func TestBootstrapStatusUsesItsOwnGlobalAPIBucket(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testRun := atomic.AddUint64(&rateLimitTestRun, 1)

	oldRedisEnabled := common.RedisEnabled
	oldGlobalAPIRateLimitEnable := common.GlobalApiRateLimitEnable
	oldGlobalAPIRateLimitNum := common.GlobalApiRateLimitNum
	oldGlobalAPIRateLimitDuration := common.GlobalApiRateLimitDuration
	common.RedisEnabled = false
	common.GlobalApiRateLimitEnable = true
	common.GlobalApiRateLimitNum = 1
	common.GlobalApiRateLimitDuration = 60
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.GlobalApiRateLimitEnable = oldGlobalAPIRateLimitEnable
		common.GlobalApiRateLimitNum = oldGlobalAPIRateLimitNum
		common.GlobalApiRateLimitDuration = oldGlobalAPIRateLimitDuration
	})

	engine := gin.New()
	api := engine.Group("/api")
	api.Use(GlobalAPIRateLimit())
	api.GET("/status", PublicStatusRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	api.GET("/notice", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	request := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = fmt.Sprintf("[2001:db8:%x::4]:1234", testRun)
		engine.ServeHTTP(recorder, req)
		return recorder
	}

	require.Equal(t, http.StatusNoContent, request("/api/status").Code)
	require.Equal(t, http.StatusNoContent, request("/api/notice").Code)
	require.Equal(t, http.StatusTooManyRequests, request("/api/status").Code)
}

func TestRedisRateLimitEnforcesWindowAndReturnsRetryInformation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testRun := atomic.AddUint64(&rateLimitTestRun, 1)
	server := miniredis.RunT(t)
	oldRedisEnabled := common.RedisEnabled
	oldRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, common.RDB.Close())
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRDB
	})

	engine := gin.New()
	engine.GET("/", rateLimitFactory(1, 60, fmt.Sprintf("redis-test-%d", testRun)), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	request := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = fmt.Sprintf("[2001:db8:%x::6]:1234", testRun)
		engine.ServeHTTP(recorder, req)
		return recorder
	}

	require.Equal(t, http.StatusNoContent, request().Code)
	limited := request()
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.NotEmpty(t, limited.Header().Get("Retry-After"))
	server.FastForward(61 * time.Second)
	require.Equal(t, http.StatusNoContent, request().Code)
}

func TestRedisRateLimitIsAtomicUnderConcurrentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testRun := atomic.AddUint64(&rateLimitTestRun, 1)
	server := miniredis.RunT(t)
	oldRedisEnabled := common.RedisEnabled
	oldRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		require.NoError(t, common.RDB.Close())
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRDB
	})

	engine := gin.New()
	engine.GET("/", rateLimitFactory(5, 60, fmt.Sprintf("redis-concurrent-test-%d", testRun)), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	var allowed int32
	done := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		go func() {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = fmt.Sprintf("[2001:db8:%x::7]:1234", testRun)
			engine.ServeHTTP(recorder, req)
			if recorder.Code == http.StatusNoContent {
				atomic.AddInt32(&allowed, 1)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
	require.Equal(t, int32(5), atomic.LoadInt32(&allowed))
}

func TestRedisRateLimitFailureFailsOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server, err := miniredis.Run()
	require.NoError(t, err)
	address := server.Addr()
	server.Close()

	oldRedisEnabled := common.RedisEnabled
	oldRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{
		Addr:        address,
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() {
		require.NoError(t, common.RDB.Close())
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRDB
	})

	engine := gin.New()
	engine.GET("/", rateLimitFactory(1, 60, "redis-failure-test"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestRedisRateLimitUsesBoundedTimeoutAndFailsOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case connection := <-accepted:
			_ = connection.Close()
		default:
		}
	})

	oldRedisEnabled := common.RedisEnabled
	oldRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{
		Addr:         listener.Addr().String(),
		DialTimeout:  100 * time.Millisecond,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		MaxRetries:   -1,
	})
	t.Cleanup(func() {
		require.NoError(t, common.RDB.Close())
		common.RedisEnabled = oldRedisEnabled
		common.RDB = oldRDB
	})

	engine := gin.New()
	engine.GET("/", rateLimitFactory(1, 60, "redis-timeout-test"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	startedAt := time.Now()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Less(t, time.Since(startedAt), time.Second)
	require.Equal(t, http.StatusNoContent, recorder.Code)
}
