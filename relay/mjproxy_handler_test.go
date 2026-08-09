package relay

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWriteMidjourneyStatusCodeUsesChannelMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("status_code_mapping", `{"429":"503"}`)

	writeMidjourneyStatusCode(c, http.StatusTooManyRequests)

	require.Equal(t, http.StatusServiceUnavailable, c.Writer.Status())
}

func TestRelayMidjourneyImageRejectsUnsignedAndWrongUserURLs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	oldSecret := common.CryptoSecret
	common.CryptoSecret = "midjourney-handler-test-secret"
	db, err := gorm.Open(sqlite.Open("file:mjproxy_auth?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	require.NoError(t, db.Create(&model.Midjourney{
		Id: 1, UserId: 42, MjId: "mj-private-task", ImageUrl: "https://example.com/private.png",
	}).Error)
	t.Cleanup(func() {
		model.DB = oldDB
		common.CryptoSecret = oldSecret
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	router := gin.New()
	router.GET("/mj/image/:id", RelayMidjourneyImage)

	unsigned := httptest.NewRecorder()
	router.ServeHTTP(unsigned, httptest.NewRequest(http.MethodGet, "/mj/image/mj-private-task", nil))
	require.Equal(t, http.StatusForbidden, unsigned.Code)
	require.Contains(t, unsigned.Body.String(), "midjourney_image_authorization_failed")

	now := time.Now()
	expiresAt := now.Add(service.MidjourneyImageURLTTL).Unix()
	wrongUserURL := "/mj/image/mj-private-task?uid=43&expires=" +
		strconv.FormatInt(expiresAt, 10) + "&signature=" +
		service.SignMidjourneyImageURL("mj-private-task", 43, expiresAt)
	wrongUser := httptest.NewRecorder()
	router.ServeHTTP(wrongUser, httptest.NewRequest(http.MethodGet, wrongUserURL, nil))
	require.Equal(t, http.StatusForbidden, wrongUser.Code)
	require.Contains(t, wrongUser.Body.String(), "midjourney_image_authorization_failed")

	require.NoError(t, db.Create(&model.Midjourney{
		Id: 2, UserId: 42, MjId: "mj-private-task", ImageUrl: "https://example.com/duplicate.png",
	}).Error)
	ambiguousURL := "/mj/image/mj-private-task?uid=42&expires=" +
		strconv.FormatInt(expiresAt, 10) + "&signature=" +
		service.SignMidjourneyImageURL("mj-private-task", 42, expiresAt)
	ambiguous := httptest.NewRecorder()
	router.ServeHTTP(ambiguous, httptest.NewRequest(http.MethodGet, ambiguousURL, nil))
	require.Equal(t, http.StatusForbidden, ambiguous.Code)
	require.Contains(t, ambiguous.Body.String(), "midjourney_image_authorization_failed")
}

func TestRelayMidjourneyImageReturnsServerErrorForLookupFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	oldSecret := common.CryptoSecret
	var db *gorm.DB
	callbackRegistered := false
	const callbackName = "test:midjourney-lookup-error"
	t.Cleanup(func() {
		model.DB = oldDB
		common.CryptoSecret = oldSecret
		if db == nil {
			return
		}
		if callbackRegistered {
			_ = db.Callback().Query().Remove(callbackName)
		}
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	common.CryptoSecret = "midjourney-handler-test-secret"
	var err error
	db, err = gorm.Open(sqlite.Open("file:mjproxy_lookup_error?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	callbackHit := false
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "midjourneys" {
			callbackHit = true
			tx.AddError(errors.New("database unavailable"))
		}
	}))
	callbackRegistered = true

	expiresAt := time.Now().Add(service.MidjourneyImageURLTTL).Unix()
	requestURL := "/mj/image/mj-lookup-error?uid=42&expires=" +
		strconv.FormatInt(expiresAt, 10) + "&signature=" +
		service.SignMidjourneyImageURL("mj-lookup-error", 42, expiresAt)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.GET("/mj/image/:id", RelayMidjourneyImage)
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestURL, nil))

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "midjourney_task_lookup_failed")
	require.True(t, callbackHit, "lookup error callback was not invoked")
}

func TestRelayMidjourneyTaskImageSeedUsesOriginChannelKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	receivedKey := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey <- r.Header.Get("mj-api-secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"description":"ok","result":"seed"}`))
	}))
	t.Cleanup(upstream.Close)

	oldDB := model.DB
	oldSQLite := common.UsingSQLite
	oldMemoryCache := common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open("file:mjproxy_origin_channel?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	common.UsingSQLite = true
	common.MemoryCacheEnabled = false
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Midjourney{}))
	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id: 91, Type: constant.ChannelTypeMidjourney, Key: "origin-channel-key",
		Status: common.ChannelStatusEnabled, Name: "origin-midjourney", BaseURL: &baseURL,
	}).Error)
	require.NoError(t, db.Create(&model.Midjourney{
		Id: 92, UserId: 42, ChannelId: 91, MjId: "provider-task-for-seed",
	}).Error)
	service.InitHttpClient()
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldSQLite
		common.MemoryCacheEnabled = oldMemoryCache
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	router := gin.New()
	router.GET("/mj/task/:id/image-seed", func(c *gin.Context) {
		c.Set("id", 42)
		if mjErr := RelayMidjourneyTaskImageSeed(c); mjErr != nil {
			c.JSON(http.StatusBadGateway, mjErr)
		}
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/mj/task/provider-task-for-seed/image-seed", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	select {
	case key := <-receivedKey:
		require.Equal(t, "origin-channel-key", key)
	case <-time.After(time.Second):
		t.Fatal("upstream request was not received")
	}
}

func TestApplyMidjourneyOriginChannelUpdatesBillingMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyChannelId, 11)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeMidjourneyPlus)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://selected.example")
	common.SetContextKey(c, constant.ContextKeyChannelKey, "selected-key")
	info := &relaycommon.RelayInfo{}
	info.InitChannelMeta(c)

	baseURL := "https://origin.example"
	channel := &model.Channel{
		Id: 21, Type: constant.ChannelTypeMidjourney, Key: "origin-key",
		Status: common.ChannelStatusEnabled, Name: "origin", BaseURL: &baseURL,
	}
	require.Nil(t, applyMidjourneyOriginChannel(c, info, channel))

	require.Equal(t, 21, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
	require.Equal(t, constant.ChannelTypeMidjourney, common.GetContextKeyInt(c, constant.ContextKeyChannelType))
	require.Equal(t, "https://origin.example", common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl))
	require.Equal(t, "origin-key", common.GetContextKeyString(c, constant.ContextKeyChannelKey))
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, 21, info.ChannelId)
	require.Equal(t, constant.ChannelTypeMidjourney, info.ChannelType)
	require.Equal(t, "https://origin.example", info.ChannelBaseUrl)
	require.Equal(t, "origin-key", info.ApiKey)
}

func TestClassifyMidjourneySubmissionPreservesUncertainOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		response  dto.MidjourneyResponse
		accepted  bool
		ambiguous bool
	}{
		{name: "normal accepted", status: http.StatusOK, response: dto.MidjourneyResponse{Code: 1, Result: "task-id"}, accepted: true},
		{name: "accepted body wins over server status", status: http.StatusBadGateway, response: dto.MidjourneyResponse{Code: 1, Result: "task-id"}, accepted: true},
		{name: "accepted code without identity", status: http.StatusOK, response: dto.MidjourneyResponse{Code: 1}, ambiguous: true},
		{name: "server error without identity", status: http.StatusInternalServerError, response: dto.MidjourneyResponse{Code: 23}, ambiguous: true},
		{name: "explicit queue rejection", status: http.StatusOK, response: dto.MidjourneyResponse{Code: 23, Result: "queue-marker"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			accepted, ambiguous := classifyMidjourneySubmission(test.status, &test.response)
			require.Equal(t, test.accepted, accepted)
			require.Equal(t, test.ambiguous, ambiguous)
		})
	}
}

func TestRelayMidjourneyNotifyCompletesShadowTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:mjproxy_notify_shadow_task?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(
		&model.Midjourney{}, &model.MidjourneyBillingClaim{}, &model.Task{}, &model.BillingSettlement{},
	))
	t.Cleanup(func() {
		model.DB = oldDB
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	shadowTask := model.Task{
		TaskID: "task_midjourney_notify_success", Platform: constant.TaskPlatformMidjourney,
		UserId: 301, ChannelId: 401, Quota: 30, Status: model.TaskStatusSubmitted,
		Progress: "0%", SubmitTime: time.Now().Add(-2 * time.Hour).Unix(),
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "provider-notify-success", BillingRequestId: "notify-success-request",
		},
	}
	require.NoError(t, db.Create(&shadowTask).Error)
	midjourneyTask := model.Midjourney{
		UserId: 301, ChannelId: 401, MjId: "provider-notify-success",
		Code: 1, Status: "SUCCESS", Progress: "100%", Quota: 30,
		SubmitTime: shadowTask.SubmitTime * 1000, FinishTime: 1700000000000,
	}
	require.NoError(t, db.Create(&midjourneyTask).Error)
	require.NoError(t, db.Create(&model.MidjourneyBillingClaim{
		ChannelID: 401, MjID: midjourneyTask.MjId, UserID: 301,
		BillingTaskID: shadowTask.ID, BillingRequestID: "notify-success-request",
		CreatedAt: time.Now().Unix(),
	}).Error)

	requestBody := []byte(`{"id":"provider-notify-success","status":"SUCCESS","progress":"100%","finishTime":1700000000000}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/notify", bytes.NewReader(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")
	require.Nil(t, RelayMidjourneyNotify(c))

	var storedTask model.Task
	require.NoError(t, db.First(&storedTask, shadowTask.ID).Error)
	require.EqualValues(t, model.TaskStatusSuccess, storedTask.Status)
	require.Equal(t, "100%", storedTask.Progress)
	require.Equal(t, 30, storedTask.Quota)
	require.Empty(t, model.GetTimedOutUnfinishedTasks(time.Now().Unix(), 10))
}

func TestRelayMidjourneyNotifyRejectsAmbiguousProviderID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldDB := model.DB
	db, err := gorm.Open(sqlite.Open("file:mjproxy_notify_ambiguous?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	t.Cleanup(func() {
		model.DB = oldDB
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	sharedID := "provider-notify-shared"
	require.NoError(t, db.Create(&model.Midjourney{
		UserId: 501, ChannelId: 601, MjId: sharedID, Status: "", Progress: "0%",
	}).Error)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId: 502, ChannelId: 602, MjId: sharedID, Status: "", Progress: "0%",
	}).Error)

	requestBody := []byte(`{"id":"provider-notify-shared","status":"SUCCESS","progress":"100%"}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/mj/notify", bytes.NewReader(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")
	mjErr := RelayMidjourneyNotify(c)
	require.NotNil(t, mjErr)
	require.Equal(t, "midjourney_task_ambiguous", mjErr.Description)

	var tasks []model.Midjourney
	require.NoError(t, db.Order("id").Find(&tasks).Error)
	require.Len(t, tasks, 2)
	require.Equal(t, "", tasks[0].Status)
	require.Equal(t, "", tasks[1].Status)
}
