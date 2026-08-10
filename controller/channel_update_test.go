package controller

import (
	"net/http"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelUpdateControllerTestDB(t *testing.T) (*gorm.DB, *model.Channel) {
	t.Helper()

	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Log{}, &model.User{}))

	priority := int64(10)
	weight := uint(20)
	baseURL := "https://origin.example.com"
	tag := "stable"
	channel := &model.Channel{
		Name:               "origin-channel",
		Type:               1,
		Key:                "origin-key",
		Status:             common.ChannelStatusManuallyDisabled,
		Models:             "gpt-origin",
		Group:              "default",
		Priority:           &priority,
		Weight:             &weight,
		BaseURL:            &baseURL,
		Tag:                &tag,
		Balance:            12.5,
		BalanceUpdatedTime: 111,
		UsedQuota:          222,
		CreatedTime:        333,
		TestTime:           444,
		ResponseTime:       555,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 1,
		},
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	return db, channel
}

func TestUpdateChannelIgnoresMassAssignedRuntimeFields(t *testing.T) {
	db, channel := setupChannelUpdateControllerTestDB(t)

	priority := int64(30)
	weight := uint(40)
	body := map[string]any{
		"id":                   channel.Id,
		"name":                 "updated-channel",
		"status":               common.ChannelStatusEnabled,
		"priority":             priority,
		"weight":               weight,
		"balance":              999999.99,
		"balance_updated_time": 999,
		"used_quota":           0,
		"created_time":         999,
		"test_time":            999,
		"response_time":        999,
		"other_info":           `{"status_reason":"forged"}`,
		"channel_info": map[string]any{
			"is_multi_key":   false,
			"multi_key_size": 99,
		},
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/channel/", body, 1)
	UpdateChannel(ctx)
	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, "updated-channel", stored.Name)
	require.Equal(t, common.ChannelStatusEnabled, stored.Status)
	require.EqualValues(t, priority, *stored.Priority)
	require.EqualValues(t, weight, *stored.Weight)
	require.Equal(t, 12.5, stored.Balance)
	require.EqualValues(t, 111, stored.BalanceUpdatedTime)
	require.EqualValues(t, 222, stored.UsedQuota)
	require.EqualValues(t, 333, stored.CreatedTime)
	require.EqualValues(t, 444, stored.TestTime)
	require.EqualValues(t, 555, stored.ResponseTime)
	require.Empty(t, stored.OtherInfo)
	require.True(t, stored.ChannelInfo.IsMultiKey)
	require.Equal(t, 1, stored.ChannelInfo.MultiKeySize)
}

func TestUpdateChannelPartialStatusDoesNotClearConfiguration(t *testing.T) {
	db, channel := setupChannelUpdateControllerTestDB(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id":     channel.Id,
		"status": common.ChannelStatusEnabled,
	}, 1)
	UpdateChannel(ctx)
	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, stored.Status)
	require.Equal(t, "origin-channel", stored.Name)
	require.Equal(t, "origin-key", stored.Key)
	require.Equal(t, "gpt-origin", stored.Models)
	require.Equal(t, "default", stored.Group)
	require.NotNil(t, stored.BaseURL)
	require.Equal(t, "https://origin.example.com", *stored.BaseURL)
}

func TestUpdateChannelPersistsOpenAIOrganization(t *testing.T) {
	db, channel := setupChannelUpdateControllerTestDB(t)

	organization := "org-controller-update"
	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id":                  channel.Id,
		"openai_organization": organization,
	}, 1)
	UpdateChannel(ctx)
	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.NotNil(t, stored.OpenAIOrganization)
	require.Equal(t, organization, *stored.OpenAIOrganization)
}

func TestUpdateChannelRejectsUnknownStatus(t *testing.T) {
	db, channel := setupChannelUpdateControllerTestDB(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id":     channel.Id,
		"status": common.ChannelStatusUnknown,
	}, 1)
	UpdateChannel(ctx)
	response := decodeAPIResponse(t, recorder)
	require.False(t, response.Success)
	require.Contains(t, response.Message, "invalid channel status")

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
}

func TestUpdateChannelAppendsMultiKeysAndUpdatesSize(t *testing.T) {
	db, channel := setupChannelUpdateControllerTestDB(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id":       channel.Id,
		"key":      "second-key\norigin-key",
		"key_mode": "append",
	}, 1)
	UpdateChannel(ctx)
	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, "origin-key\nsecond-key", stored.Key)
	require.Equal(t, 2, stored.ChannelInfo.MultiKeySize)
}

func TestUpdateChannelReplacesMultiKeysAndUpdatesSize(t *testing.T) {
	db, channel := setupChannelUpdateControllerTestDB(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id":       channel.Id,
		"key":      "replacement-a\nreplacement-b",
		"key_mode": "replace",
	}, 1)
	UpdateChannel(ctx)
	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var stored model.Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, "replacement-a\nreplacement-b", stored.Key)
	require.Equal(t, 2, stored.ChannelInfo.MultiKeySize)
}

func TestUpdateChannelWaitsForChannelPollingLock(t *testing.T) {
	_, channel := setupChannelUpdateControllerTestDB(t)

	lock := model.GetChannelPollingLock(channel.Id)
	lock.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			lock.Unlock()
		}
	})

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id":     channel.Id,
		"status": common.ChannelStatusEnabled,
	}, 1)
	done := make(chan struct{})
	go func() {
		UpdateChannel(ctx)
		close(done)
	}()

	select {
	case <-done:
		lock.Unlock()
		locked = false
		t.Fatal("channel update completed while polling lock was held")
	case <-time.After(150 * time.Millisecond):
	}

	lock.Unlock()
	locked = false
	select {
	case <-done:
		response := decodeAPIResponse(t, recorder)
		require.True(t, response.Success, response.Message)
	case <-time.After(2 * time.Second):
		t.Fatal("channel update did not finish after polling lock was released")
	}
}
