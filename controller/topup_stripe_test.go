package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
	"gorm.io/gorm"
)

func setupStripeWebhookTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldUsingSQLite := common.UsingSQLite
	oldUsingMySQL := common.UsingMySQL
	oldUsingPostgreSQL := common.UsingPostgreSQL

	common.RedisEnabled = false
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.SubscriptionOrder{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		common.UsingSQLite = oldUsingSQLite
		common.UsingMySQL = oldUsingMySQL
		common.UsingPostgreSQL = oldUsingPostgreSQL
	})
	return db
}

func completedStripeEvent(tradeNo string) stripe.Event {
	return stripe.Event{
		Type: stripe.EventTypeCheckoutSessionCompleted,
		Data: &stripe.EventData{Object: map[string]interface{}{
			"client_reference_id": tradeNo,
			"customer":            "cus_test",
			"status":              "complete",
			"payment_status":      "paid",
		}},
	}
}

func TestStripeSessionCompletedReturnsRetryableErrorWhenRechargeFails(t *testing.T) {
	db := setupStripeWebhookTestDB(t)
	topUp := model.TopUp{
		Id:              8101,
		UserId:          9999,
		Money:           1,
		TradeNo:         "stripe-retryable-failure",
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, db.Create(&topUp).Error)

	err := sessionCompleted(context.Background(), completedStripeEvent(topUp.TradeNo), "127.0.0.1")
	require.Error(t, err)
	var stored model.TopUp
	require.NoError(t, db.First(&stored, topUp.Id).Error)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
}

func TestStripeSessionCompletedAcknowledgesCompletedTopUpReplay(t *testing.T) {
	db := setupStripeWebhookTestDB(t)
	topUp := model.TopUp{
		Id:              8102,
		UserId:          9999,
		Money:           1,
		TradeNo:         "stripe-completed-replay",
		PaymentProvider: model.PaymentProviderStripe,
		Status:          common.TopUpStatusSuccess,
	}
	require.NoError(t, db.Create(&topUp).Error)

	require.NoError(t, sessionCompleted(context.Background(), completedStripeEvent(topUp.TradeNo), "127.0.0.1"))
}

func TestStripeSessionCompletedReturnsRetryableErrorWhenOrderLookupFails(t *testing.T) {
	db := setupStripeWebhookTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	err = sessionCompleted(context.Background(), completedStripeEvent("stripe-database-unavailable"), "127.0.0.1")
	require.Error(t, err)
}
