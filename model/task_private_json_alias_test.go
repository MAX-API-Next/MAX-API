// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func TestTaskPrivateHistoryUsesJSONFieldFolding(t *testing.T) {
	var private TaskPrivateData
	require.NoError(t, private.Scan(`{"b\u0130lling_source":"legacy-history","Billing_Source":"wallet","token_id":7}`))
	require.Equal(t, "wallet", private.BillingSource)
	private.ResultURL = "updated-result"
	raw, err := common.Marshal(private)
	require.NoError(t, err)
	var object map[string]any
	require.NoError(t, common.UnmarshalUseNumber(raw, &object))
	require.Equal(t, "legacy-history", object["b\u0130lling_source"])
	require.Equal(t, "wallet", object["billing_source"])
}

func TestTaskPrivateBillingAliasesCannotChooseAnIdentity(t *testing.T) {
	for _, raw := range []string{
		`{"token_id":11,"Token_ID":22}`,
		`{"subscription_id":11,"Subscription_ID":22}`,
		`{"billing_source":"wallet","Billing_Source":"subscription"}`,
		`{"billing_context":{"model_ratio":1,"Model_Ratio":2}}`,
	} {
		private := TaskPrivateData{TokenId: 7}
		require.Error(t, private.Scan(raw), raw)
		require.Equal(t, 7, private.TokenId, "failed decoding remains atomic")
	}
	var equal TaskPrivateData
	require.NoError(t, equal.Scan(`{"token_id":11,"Token_ID":11,"billing_context":{"other_ratios":{"duration":1,"Duration":2}}}`))
	require.Equal(t, 11, equal.TokenId)
	require.Equal(t, map[string]float64{"duration": 1, "Duration": 2}, equal.BillingContext.OtherRatios)
}

func TestTaskPrivateFactMapsRejectConflictingExactMembers(t *testing.T) {
	for _, raw := range []string{
		`{"billing_context":{"other_ratios":{"duration":1,"duration":2}}}`,
		`{"billing_context":{"task_billing":{"fields":{"duration":1,"duration":2}}}}`,
	} {
		var private TaskPrivateData
		require.Error(t, private.Scan(raw), raw)
	}
	var equal TaskPrivateData
	require.NoError(t, equal.Scan(`{"billing_context":{"other_ratios":{"duration":1,"duration":1,"Duration":2}}}`))
	require.Equal(t, map[string]float64{"duration": 1, "Duration": 2}, equal.BillingContext.OtherRatios)
}
