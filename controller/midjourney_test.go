package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/service"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestApplyMidjourneyTaskResponseIgnoresUnknownUpstreamID(t *testing.T) {
	require.NotPanics(t, func() {
		err := applyMidjourneyTaskResponse(context.Background(), map[string]*model.Midjourney{}, dto.MidjourneyDto{
			MjId: "provider-task-not-in-local-batch",
		})
		require.NoError(t, err)
	})
}

func TestFetchMidjourneyTaskUpdatesUsesEnabledChannelKey(t *testing.T) {
	service.InitHttpClient()
	receivedKey := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey <- r.Header.Get("mj-api-secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(upstream.Close)

	oldMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemoryCache })
	baseURL := upstream.URL
	channel := &model.Channel{
		Id: 701, Type: constant.ChannelTypeMidjourney, Key: "disabled-key\nenabled-key",
		Status: common.ChannelStatusEnabled, BaseURL: &baseURL,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true, MultiKeyMode: constant.MultiKeyModeRandom,
			MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled},
		},
	}

	_, err := fetchMidjourneyTaskUpdates(context.Background(), channel, []string{"provider-task"})
	require.NoError(t, err)
	select {
	case key := <-receivedKey:
		require.Equal(t, "enabled-key", key)
	case <-time.After(time.Second):
		t.Fatal("upstream request was not received")
	}
}

func TestUpdateMidjourneyTasksOnceScopesDuplicateProviderIDsByChannel(t *testing.T) {
	service.InitHttpClient()
	serverFor := func(progress, imageURL string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"shared-provider-id","status":"IN_PROGRESS","progress":"` + progress + `","imageUrl":"` + imageURL + `"}]`))
		}))
	}
	upstreamA := serverFor("10%", "https://example.com/a.png")
	upstreamB := serverFor("20%", "https://example.com/b.png")
	t.Cleanup(upstreamA.Close)
	t.Cleanup(upstreamB.Close)

	db, err := gorm.Open(sqlite.Open("file:midjourney_channel_scope?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Midjourney{}, &model.MidjourneyBillingClaim{}))
	oldDB := model.DB
	oldSQLite := common.UsingSQLite
	oldMemoryCache := common.MemoryCacheEnabled
	model.DB = db
	common.UsingSQLite = true
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		common.UsingSQLite = oldSQLite
		common.MemoryCacheEnabled = oldMemoryCache
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	baseURLA, baseURLB := upstreamA.URL, upstreamB.URL
	require.NoError(t, db.Create(&model.Channel{
		Id: 711, Type: constant.ChannelTypeMidjourney, Key: "key-a", Status: common.ChannelStatusEnabled, BaseURL: &baseURLA,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 712, Type: constant.ChannelTypeMidjourney, Key: "key-b", Status: common.ChannelStatusEnabled, BaseURL: &baseURLB,
	}).Error)
	submitTime := time.Now().UnixMilli()
	taskA := model.Midjourney{UserId: 721, ChannelId: 711, MjId: "shared-provider-id", Progress: "0%", Status: "", SubmitTime: submitTime}
	taskB := model.Midjourney{UserId: 722, ChannelId: 712, MjId: "shared-provider-id", Progress: "0%", Status: "", SubmitTime: submitTime}
	require.NoError(t, db.Create(&taskA).Error)
	require.NoError(t, db.Create(&taskB).Error)

	require.NoError(t, updateMidjourneyTasksOnce(context.Background()))

	var storedA, storedB model.Midjourney
	require.NoError(t, db.First(&storedA, taskA.Id).Error)
	require.NoError(t, db.First(&storedB, taskB.Id).Error)
	assert.Equal(t, "10%", storedA.Progress)
	assert.Equal(t, "https://example.com/a.png", storedA.ImageUrl)
	assert.Equal(t, "20%", storedB.Progress)
	assert.Equal(t, "https://example.com/b.png", storedB.ImageUrl)
}

func TestMidjourneyFailureRefundsWalletAndTokenThroughDurableSettlement(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Token{}, &model.Task{}, &model.Midjourney{},
		&model.MidjourneyBillingClaim{}, &model.BillingSettlement{},
		&model.Log{}, &model.BillingLogReceipt{},
	))
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldSQLite, oldRedis := common.UsingSQLite, common.RedisEnabled
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite, common.RedisEnabled = true, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.UsingSQLite, common.RedisEnabled = oldSQLite, oldRedis
	})

	user := model.User{Id: 801, Username: "midjourney-refund-user", AffCode: "mj-refund-user", Quota: 70, Status: common.UserStatusEnabled}
	token := model.Token{
		Id: 802, UserId: user.Id, Key: "midjourney-refund-token", Name: "midjourney-refund-token",
		Status: common.TokenStatusEnabled, RemainQuota: 70, UsedQuota: 30,
	}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&token).Error)
	billingTask := &model.Task{
		TaskID: "task_midjourney_refund", Platform: constant.TaskPlatformMidjourney,
		UserId: user.Id, ChannelId: 803, Quota: 30, Action: constant.MjActionImagine,
		Status: model.TaskStatusNotStart, Progress: "0%",
		PrivateData: model.TaskPrivateData{
			AwaitingUpstreamID: true, BillingSource: model.BillingSettlementSourceWallet,
			BillingRequestId: "midjourney-refund-request", TokenId: token.Id,
		},
	}
	require.NoError(t, db.Create(billingTask).Error)
	mj := &model.Midjourney{
		UserId: user.Id, ChannelId: 803, MjId: "provider-midjourney-refund",
		Action: constant.MjActionImagine, Status: "", Progress: "0%", Quota: 30,
		SubmitTime: 123000, ImageUrl: "https://example.com/original.png",
	}
	created, refundDuplicate, err := model.FinalizeMidjourneySubmission(mj, billingTask, &model.BillingSettlementInput{
		OperationKey: "request:midjourney-refund-request:finalize", Source: model.BillingSettlementSourceWallet,
		UserID: user.Id, TokenID: token.Id,
	}, nil)
	require.NoError(t, err)
	require.True(t, created)
	require.False(t, refundDuplicate)

	err = updateMidjourneyTaskFromResponse(context.Background(), mj, dto.MidjourneyDto{
		MjId: mj.MjId, Status: "FAILURE", Progress: "100%", FailReason: "provider failed",
		SubmitTime: mj.SubmitTime, FinishTime: 1000,
	})
	require.NoError(t, err)

	var storedUser model.User
	require.NoError(t, db.First(&storedUser, user.Id).Error)
	assert.EqualValues(t, 100, storedUser.Quota)
	var storedToken model.Token
	require.NoError(t, db.First(&storedToken, token.Id).Error)
	assert.EqualValues(t, 100, storedToken.RemainQuota)
	assert.Zero(t, storedToken.UsedQuota)
	var storedTask model.Task
	require.NoError(t, db.First(&storedTask, billingTask.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, storedTask.Status)
	assert.Zero(t, storedTask.Quota)
	var storedMJ model.Midjourney
	require.NoError(t, db.First(&storedMJ, mj.Id).Error)
	assert.Equal(t, "FAILURE", storedMJ.Status)
	assert.EqualValues(t, 123000, storedMJ.SubmitTime)
	assert.Equal(t, "https://example.com/original.png", storedMJ.ImageUrl)
}
