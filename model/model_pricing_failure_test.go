// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"errors"
	"fmt"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
)

func TestModelPricingQueryFailureNeverReturnsCachedQuote(t *testing.T) {
	db := setupModelPricingTest(t)
	channel := pricingChannelForTest(t, db, constant.ChannelTypeKling, "kling-v1")
	entry := pricingEntryForTest(t, db, "kling-v1")
	request := ModelPricingQuoteRequest{ModelName: "kling-v1", ContextID: fmt.Sprintf("channel:%d", channel.Id), ExpectedVersion: entry.Version, ExpectedContractDigest: entry.ContractDigest, ExpectedBindingVersion: entry.BindingVersion, Group: "default", Pricing: types.ModelPricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("units") * 0.1`}, Submit: types.TaskPricingFacts{"units": 1.0}}
	request.Group = "vip"
	_, err := QuoteModelPricing(request)
	require.ErrorContains(t, err, "does not serve")
	request.Group = "default"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("pricing_query_fault", func(tx *gorm.DB) { tx.AddError(errors.New("synthetic authority read failure")) }))
	snapshot, err := GetModelPricingSnapshot([]string{"kling-v1"})
	require.Error(t, err)
	require.Nil(t, snapshot)
	quote, err := QuoteModelPricing(request)
	require.Error(t, err)
	require.Nil(t, quote)
	require.NoError(t, db.Callback().Query().Remove("pricing_query_fault"))
}
