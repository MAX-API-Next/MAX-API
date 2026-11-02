// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func shadowChannelForTest(t *testing.T, db *gorm.DB, kind int, name, expression string) TaskPricingShadowRequest {
	t.Helper()
	channel := pricingChannelForTest(t, db, kind, name)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("setting", `{"task_pricing_profile":"new-api-task@1"}`).Error)
	for key, object := range map[string]any{
		"billing_setting.billing_mode": map[string]string{name: "tiered_expr"},
		"billing_setting.billing_expr": map[string]string{name: expression},
		"GroupRatio":                   map[string]float64{"default": 1.5},
	} {
		wire, err := common.Marshal(object)
		require.NoError(t, err)
		require.NoError(t, db.Create(&Option{Key: key, Value: string(wire)}).Error)
	}
	require.NoError(t, db.Create(&Option{Key: "PreConsumedQuota", Value: "700"}).Error)
	return TaskPricingShadowRequest{ModelName: name, ChannelID: channel.Id, Group: "default"}
}

func TestTaskPricingShadowAuthorityThreeFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, expression string
		kind             int
		facts            types.TaskPricingFacts
		amount           float64
		quota            int
	}{
		{"MiniMax-H3", `u("seconds") * 0.1`, constant.ChannelTypeMiniMax, types.TaskPricingFacts{"seconds": 6, "resolution": "768P", "input_images": 2, "input_video_seconds": 3}, 0.6, 900},
		{"kling-v1", `u("units") * 0.1`, constant.ChannelTypeKling, types.TaskPricingFacts{"units": 4}, 0.4, 600},
		{"doubao-seedance-1-5-pro-251215", `u("tokens") / 1000000 * (u("generate_audio") ? 5 : 3)`, constant.ChannelTypeDoubaoVideo, types.TaskPricingFacts{"tokens": 200000, "resolution": "720p", "generate_audio": false}, 0.6, 900},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupModelPricingTest(t)
			request := shadowChannelForTest(t, db, tc.kind, tc.name, tc.expression)
			// Node caches deliberately disagree with DB. They are not an authority.
			cached, err := common.Marshal(map[string]string{tc.name: "999"})
			require.NoError(t, err)
			require.NoError(t, updateOptionMap("billing_setting.billing_expr", string(cached)))
			common.OptionMapRWMutex.Lock()
			common.OptionMap["GroupRatio"], common.OptionMap["PreConsumedQuota"] = `{"default":99}`, "9999"
			before, err := common.Marshal(common.OptionMap)
			common.OptionMapRWMutex.Unlock()
			require.NoError(t, err)
			writes := 0
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("shadow_no_create", func(*gorm.DB) { writes++ }))
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register("shadow_no_update", func(*gorm.DB) { writes++ }))
			require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("shadow_no_delete", func(*gorm.DB) { writes++ }))
			at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			got, err := readTaskPricingShadowContext(db, t.Context(), request, at)
			require.NoError(t, err)
			require.NotNil(t, got)
			require.False(t, got.AdmissionReady)
			require.Contains(t, got.Scope, "shadow")
			require.Equal(t, request.ChannelID, got.ChannelID)
			require.Equal(t, tc.kind, got.ChannelType)
			require.Equal(t, taskpricing.NewAPITask, got.Profile)
			require.Equal(t, taskpricing.Source{Driver: "go_adapter"}, got.Source)
			require.Equal(t, tc.name, got.MappedModel)
			require.Equal(t, tc.expression, got.Selection.Expression)
			require.Equal(t, at, got.PricingAt)
			require.Equal(t, 1.5, got.GroupRatio)
			require.Equal(t, 1000.0, got.QuotaPerUnit)
			require.Equal(t, 700, got.ReservationFloor)
			require.NotEmpty(t, got.Guards.PriceVersion)
			require.NotEmpty(t, got.Guards.BindingVersion)
			require.NotEmpty(t, got.Guards.ContractDigest)
			require.NotEmpty(t, got.TermsVersion)
			facts, err := taskpricing.SubmitFacts(got.Contract, tc.facts)
			require.NoError(t, err)
			quote, err := billingexpr.QuoteTaskExpression(billingexpr.TaskQuoteInput{Profile: got.Profile, Expression: got.Selection.Expression, Usage: facts, PricingAt: got.PricingAt, GroupRatio: got.GroupRatio, QuotaPerUnit: got.QuotaPerUnit})
			require.NoError(t, err)
			// Independent arithmetic: 6*.1*1000*1.5=900;
			// 4*.1*1000*1.5=600; 200000/1M*3*1000*1.5=900.
			require.InDelta(t, tc.amount, quote.Amount, 1e-12)
			require.Equal(t, tc.quota, quote.QuotaAfterGroup)
			require.Zero(t, writes)
			common.OptionMapRWMutex.RLock()
			after, err := common.Marshal(common.OptionMap)
			common.OptionMapRWMutex.RUnlock()
			require.NoError(t, err)
			require.Equal(t, string(before), string(after), "shadow read must not publish node caches")
			cachedExpression, exists := billing_setting.GetBillingExpr(tc.name)
			require.True(t, exists)
			require.Equal(t, "999", cachedExpression)
		})
	}
}

func TestTaskPricingShadowAliasAndSameNamedPlugin(t *testing.T) {
	db := setupModelPricingTest(t)
	request := shadowChannelForTest(t, db, constant.ChannelTypeKling, "advertised-alias", `u("units") * 0.1`)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", request.ChannelID).Update("model_mapping", `{"advertised-alias":"kling-v1"}`).Error)
	require.NoError(t, db.Create(&Option{Key: billing_setting.PluginBillingExprOption, Value: `{"kling::advertised-alias":"999"}`}).Error)
	got, err := ReadTaskPricingShadowContext(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "advertised-alias", got.Selection.Model)
	require.Equal(t, "kling-v1", got.MappedModel)
	require.Empty(t, got.Selection.PluginKey, "same-named plugin price is not the configured Go source")
	require.Equal(t, `u("units") * 0.1`, got.Selection.Expression)
	first := *got
	require.NoError(t, db.Model(&Option{}).Where(map[string]any{"key": "billing_setting.billing_mode"}).Update("value", `{"kling-v1":"tiered_expr"}`).Error)
	require.NoError(t, db.Model(&Option{}).Where(map[string]any{"key": "billing_setting.billing_expr"}).Update("value", `{"kling-v1":"u(\"units\") * 0.2"}`).Error)
	got, err = ReadTaskPricingShadowContext(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "kling-v1", got.Selection.Model, "missing original inherits mapped model")
	require.Equal(t, `u("units") * 0.2`, got.Selection.Expression)
	require.NotEqual(t, first.Guards.PriceVersion, got.Guards.PriceVersion)
	require.Equal(t, `u("units") * 0.1`, first.Selection.Expression, "old returned context is a value snapshot")
	field := got.Contract.UsageSchema["units"]
	field.Unit = "token"
	got.Contract.UsageSchema["units"] = field
	again, err := ReadTaskPricingShadowContext(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "credit", again.Contract.UsageSchema["units"].Unit)
}

func TestTaskPricingShadowRejectsUnusableAuthority(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*gorm.DB, *TaskPricingShadowRequest) error
	}{
		{"legacy-profile", func(db *gorm.DB, r *TaskPricingShadowRequest) error {
			return db.Model(&Channel{}).Where("id = ?", r.ChannelID).Update("setting", `{}`).Error
		}},
		{"invalid-setting", func(db *gorm.DB, r *TaskPricingShadowRequest) error {
			return db.Model(&Channel{}).Where("id = ?", r.ChannelID).Update("setting", `null`).Error
		}},
		{"disabled-channel", func(db *gorm.DB, r *TaskPricingShadowRequest) error {
			return db.Model(&Channel{}).Where("id = ?", r.ChannelID).Update("status", 2).Error
		}},
		{"missing-channel", func(_ *gorm.DB, r *TaskPricingShadowRequest) error { r.ChannelID = 99999; return nil }},
		{"wrong-group", func(db *gorm.DB, r *TaskPricingShadowRequest) error {
			r.Group = "other"
			return db.Model(&Option{}).Where(map[string]any{"key": "GroupRatio"}).Update("value", `{"default":1.5,"other":1}`).Error
		}},
		{"mapped-target-only", func(db *gorm.DB, r *TaskPricingShadowRequest) error {
			return db.Model(&Channel{}).Where("id = ?", r.ChannelID).Updates(map[string]any{"models": "alias", "model_mapping": `{"alias":"kling-v1"}`}).Error
		}},
		{"missing-expression", func(db *gorm.DB, _ *TaskPricingShadowRequest) error {
			return db.Model(&Option{}).Where(map[string]any{"key": "billing_setting.billing_expr"}).Update("value", `{}`).Error
		}},
		{"bad-expression-type", func(db *gorm.DB, _ *TaskPricingShadowRequest) error {
			return db.Model(&Option{}).Where(map[string]any{"key": "billing_setting.billing_expr"}).Update("value", `{"kling-v1":false}`).Error
		}},
		{"bad-group", func(db *gorm.DB, _ *TaskPricingShadowRequest) error {
			return db.Model(&Option{}).Where(map[string]any{"key": "GroupRatio"}).Update("value", `null`).Error
		}},
		{"conflicting-group-alias", func(db *gorm.DB, _ *TaskPricingShadowRequest) error {
			return db.Create(&Option{Key: "group_ratio_setting.group_ratio", Value: `{"default":2}`}).Error
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupModelPricingTest(t)
			request := shadowChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1", `u("units") * 0.1`)
			require.NoError(t, tc.edit(db, &request))
			got, err := ReadTaskPricingShadowContext(t.Context(), request)
			require.Error(t, err)
			require.Nil(t, got, "failure must not return a partial usable context")
		})
	}
}

func TestTaskPricingShadowZeroAndCancellation(t *testing.T) {
	db := setupModelPricingTest(t)
	request := shadowChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1", "0")
	require.NoError(t, db.Model(&Option{}).Where(map[string]any{"key": "GroupRatio"}).Update("value", `{"default":0}`).Error)
	got, err := ReadTaskPricingShadowContext(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "0", got.Selection.Expression)
	require.Zero(t, got.GroupRatio)
	require.Equal(t, 700, got.ReservationFloor, "configured floor is metadata, not a reservation")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err = ReadTaskPricingShadowContext(ctx, request)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
	got, err = readTaskPricingShadowContext(nil, t.Context(), request, time.Now())
	require.Error(t, err)
	require.Nil(t, got)
}

func TestTaskPricingShadowReadFailureReturnsNoPartialResult(t *testing.T) {
	db := setupModelPricingTest(t)
	request := shadowChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1", `u("units") * 0.1`)
	reads := 0
	storageErr := errors.New("synthetic authority read failure")
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("shadow_read_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			reads++
			if reads == 2 {
				tx.AddError(storageErr)
			}
		}
	}))
	got, err := ReadTaskPricingShadowContext(t.Context(), request)
	require.ErrorIs(t, err, storageErr)
	require.Nil(t, got)
	require.Equal(t, 2, reads)
}

func TestTaskPricingShadowDetectsChangedReadInputs(t *testing.T) {
	for _, changed := range []string{"price", "terms", "channel"} {
		t.Run(changed, func(t *testing.T) {
			db := setupModelPricingTest(t)
			request := shadowChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1", `u("units") * 0.1`)
			injected := false
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("shadow_change_inputs", func(tx *gorm.DB) {
				if injected {
					return
				}
				update := tx.Session(&gorm.Session{NewDB: true})
				if changed == "channel" && tx.Statement.Table == "channels" {
					injected = true
					tx.AddError(update.Model(&Channel{}).Where("id = ?", request.ChannelID).Update("status", 2).Error)
					return
				}
				rows, ok := tx.Statement.Dest.(*[]Option)
				if !ok {
					return
				}
				for _, row := range *rows {
					if changed == "price" && row.Key == "billing_setting.billing_expr" {
						injected = true
						tx.AddError(update.Model(&Option{}).Where(map[string]any{"key": row.Key}).Update("value", `{"kling-v1":"u(\"units\") * 0.2"}`).Error)
						return
					}
					if changed == "terms" && row.Key == "GroupRatio" {
						injected = true
						tx.AddError(update.Model(&Option{}).Where(map[string]any{"key": row.Key}).Update("value", `{"default":2}`).Error)
						return
					}
				}
			}))
			// The fixture changes a row after its first read in this transaction.
			// This proves mismatch rejection, not real concurrent cross-DB isolation.
			got, err := ReadTaskPricingShadowContext(t.Context(), request)
			require.True(t, injected)
			require.ErrorIs(t, err, ErrModelPricingConflict)
			require.Nil(t, got)
		})
	}
}

func TestTaskPricingShadowCapturesNodeUnitAndExcludesChannelSecrets(t *testing.T) {
	db := setupModelPricingTest(t)
	request := shadowChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1", `u("units") * 0.1`)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", request.ChannelID).Update("header_override", `{"Authorization":"synthetic-private-channel-marker"}`).Error)
	changed := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("shadow_unit_change", func(tx *gorm.DB) {
		rows, ok := tx.Statement.Dest.(*[]Option)
		if !ok || changed {
			return
		}
		for _, row := range *rows {
			if row.Key == "GroupRatio" {
				changed = true
				common.QuotaPerUnit = 2000
				return
			}
		}
	}))
	first, err := ReadTaskPricingShadowContext(t.Context(), request)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, 1000.0, first.QuotaPerUnit)
	second, err := ReadTaskPricingShadowContext(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, 2000.0, second.QuotaPerUnit)
	require.NotEqual(t, first.TermsVersion, second.TermsVersion, "different captured units must have different terms identities")
	wire, err := common.Marshal(first)
	require.NoError(t, err)
	require.NotContains(t, string(wire), "synthetic-private-channel-marker")
}
