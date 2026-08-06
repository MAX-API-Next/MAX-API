package controller

import (
	"net/http"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/stretchr/testify/require"
)

func setupChannelUpdateControllerTestDB(t *testing.T) *model.Channel {
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
	return channel
}

func TestUpdateChannelIgnoresMassAssignedRuntimeFields(t *testing.T) {
	channel := setupChannelUpdateControllerTestDB(t)

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
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
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
	channel := setupChannelUpdateControllerTestDB(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id":     channel.Id,
		"status": common.ChannelStatusEnabled,
	}, 1)
	UpdateChannel(ctx)
	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)

	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, stored.Status)
	require.Equal(t, "origin-channel", stored.Name)
	require.Equal(t, "origin-key", stored.Key)
	require.Equal(t, "gpt-origin", stored.Models)
	require.Equal(t, "default", stored.Group)
	require.NotNil(t, stored.BaseURL)
	require.Equal(t, "https://origin.example.com", *stored.BaseURL)
}

func TestUpdateChannelRejectsUnknownStatus(t *testing.T) {
	channel := setupChannelUpdateControllerTestDB(t)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/channel/", map[string]any{
		"id":     channel.Id,
		"status": common.ChannelStatusUnknown,
	}, 1)
	UpdateChannel(ctx)
	response := decodeAPIResponse(t, recorder)
	require.False(t, response.Success)

	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
}
