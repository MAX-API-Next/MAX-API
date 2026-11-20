// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"encoding/json"
	"strings"
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

func TestTaskPrivateHistoricalContainerNamesRetainCase(t *testing.T) {
	var private TaskPrivateData
	const legacy = `{"legacy":{"billing_context":"v1","Billing_Context":"v2"}}`
	require.NoError(t, private.Scan(legacy))
	raw, err := common.Marshal(private)
	require.NoError(t, err)
	require.JSONEq(t, legacy, string(raw))
	require.Error(t, private.Scan(`{"billing_context":{"model_price":1},"Billing_Context":{"model_price":2}}`))
}

func TestTaskUsageEvidenceMovesReplaceableEnvelopeHistoryToArchive(t *testing.T) {
	var private TaskPrivateData
	require.NoError(t, private.Scan(`{"billing_context":{"task_usage_envelope":{"evidence_digest":"old","historical_receipt":{"first":7}}}}`))
	before, err := common.Marshal(private)
	require.NoError(t, err)
	require.Contains(t, string(before), "historical_receipt")
	private.BillingContext.TaskUsageEnvelope.EvidenceDigest = "new"
	encoded, err := common.Marshal(private)
	require.NoError(t, err)
	var decoded struct {
		BillingContext struct {
			Active  map[string]any   `json:"task_usage_envelope"`
			Archive []map[string]any `json:"previous_task_usage_envelopes"`
		} `json:"billing_context"`
	}
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	require.Equal(t, "new", decoded.BillingContext.Active["evidence_digest"])
	require.NotContains(t, decoded.BillingContext.Active, "historical_receipt")
	require.Len(t, decoded.BillingContext.Archive, 1)
	require.Equal(t, "old", decoded.BillingContext.Archive[0]["evidence_digest"])
	require.Contains(t, decoded.BillingContext.Archive[0], "historical_receipt")
}

func TestBillingPlanHistoryStaysWithItsOriginalConfig(t *testing.T) {
	var private TaskPrivateData
	require.NoError(t, private.Scan(`{"billing_context":{"task_billing_plan":{"config_hash":"original","reserve_quota":-0,"historical_receipt":{"retained":true}}}}`))
	encoded, err := common.Marshal(private)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "historical_receipt")
	private.BillingContext.TaskBillingPlan.ReserveQuota = 10
	_, err = common.Marshal(private)
	require.ErrorContains(t, err, "different billing plan")
	private.BillingContext.TaskBillingPlan.ReserveQuota = 0
	private.BillingContext.TaskBillingPlan.ConfigHash = "replacement"
	_, err = common.Marshal(private)
	require.ErrorContains(t, err, "different billing plan")
}

func TestUnrelatedHistoricalVersionsKeepCaseSensitiveKeys(t *testing.T) {
	var private TaskPrivateData
	require.NoError(t, private.Scan(`{"token_id":7,"legacy":{"version":"schema-v1","Version":"provider-v2"}}`))
	private.ResultURL = "updated"
	raw, err := common.Marshal(private)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"version":"schema-v1"`)
	require.Contains(t, string(raw), `"Version":"provider-v2"`)
	require.Error(t, private.Scan(`{"legacy":{"plugin_key":"stored","version":"schema-v1","Version":"provider-v2"}}`))
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
		`{"billing_context":{"task_billing":{"fields":{"duration":"1","duration":"2"}}}}`,
	} {
		var private TaskPrivateData
		require.Error(t, private.Scan(raw), raw)
	}
	var equal TaskPrivateData
	require.NoError(t, equal.Scan(`{"billing_context":{"task_billing":{"fields":{"duration":"1"}}}}`))
	require.NoError(t, equal.Scan(`{"billing_context":{"other_ratios":{"duration":1,"duration":1,"Duration":2}}}`))
	require.Equal(t, map[string]float64{"duration": 1, "Duration": 2}, equal.BillingContext.OtherRatios)
}

func TestTaskPrivateFactMapExactNumericEquality(t *testing.T) {
	for _, raw := range []string{
		`{"billing_context":{"other_ratios":{"duration":1,"duration":100e-2}}}`,
		`{"billing_context":{"other_ratios":{"duration":1.0,"duration":1.00}}}`,
		`{"billing_context":{"other_ratios":{"duration":-0,"duration":0e20}}}`,
		`{"legacy":1e1000000000000000000,"legacy":10e999999999999999999}`,
	} {
		var private TaskPrivateData
		require.NoError(t, private.Scan(raw), raw)
	}
	for _, raw := range []string{
		`{"billing_context":{"other_ratios":{"duration":9007199254740992,"duration":9007199254740993}}}`,
		`{"legacy":1e1000000000000000001,"legacy":10e999999999999999999}`,
	} {
		var private TaskPrivateData
		require.Error(t, private.Scan(raw), raw)
	}
}

func TestTaskPrivateDuplicateContainersUseExactNumericEquality(t *testing.T) {
	for _, raw := range []string{
		`{"billing_context":{"other_ratios":{"duration":1}},"Billing_Context":{"other_ratios":{"duration":1.0}}}`,
		`{"history":[{"ratio":1e0}],"history":[{"ratio":1.00}]}`,
	} {
		var private TaskPrivateData
		require.NoError(t, private.Scan(raw), raw)
	}
	for _, raw := range []string{
		`{"history":[{"ratio":9007199254740992}],"history":[{"ratio":9007199254740993}]}`,
		`{"history":[{"ratio":1}],"history":[{"Ratio":1}]}`,
	} {
		var private TaskPrivateData
		require.Error(t, private.Scan(raw), raw)
	}
}

func TestNestedTypedAliasesAreEquivalentAndKeepCanonicalHistory(t *testing.T) {
	for _, raw := range []string{
		`{"plugin_binding":{"plugin_key":"stored"},"Plugin_Binding":{"Plugin_Key":"stored"}}`,
		`{"billing_context":{"historical_number":1},"Billing_Context":{"historical_number":1.0}}`,
		`{"BILLING_CONTEXT":{"historical_number":1},"Billing_Context":{"historical_number":1.0}}`,
	} {
		var private TaskPrivateData
		for range 24 {
			require.NoError(t, private.Scan(raw))
			result, err := common.Marshal(private)
			require.NoError(t, err)
			if strings.Contains(raw, "historical_number") {
				var fields map[string]any
				require.NoError(t, common.UnmarshalUseNumber(result, &fields))
				billing, ok := fields["billing_context"].(map[string]any)
				require.True(t, ok)
				require.Equal(t, json.Number("1"), billing["historical_number"])
			} else {
				require.Equal(t, "stored", private.PluginBinding.PluginKey)
			}
		}
	}
	var conflicting TaskPrivateData
	require.Error(t, conflicting.Scan(`{"plugin_binding":{"plugin_key":"stored"},"Plugin_Binding":{"Plugin_Key":"other"}}`))
}

func TestTaskPrivateHistoryFollowsUnchangedBillingComponent(t *testing.T) {
	const history = `{"billing_context":{"task_billing_plan":{"components":[{"Key":"alpha","unit":"second","legacy":18446744073709551617},{"key":"beta","unit":"file"}]}}}`
	var private TaskPrivateData
	require.NoError(t, private.Scan(history))
	private.ResultURL = "updated-result"
	components := private.BillingContext.TaskBillingPlan.Components
	components[0], components[1] = components[1], components[0]
	raw, err := common.Marshal(private)
	require.NoError(t, err)
	var read struct {
		ResultURL      string `json:"result_url"`
		BillingContext struct {
			Plan struct {
				Components []map[string]any `json:"components"`
			} `json:"task_billing_plan"`
		} `json:"billing_context"`
	}
	require.NoError(t, common.UnmarshalUseNumber(raw, &read))
	require.Equal(t, "updated-result", read.ResultURL)
	require.Equal(t, "beta", read.BillingContext.Plan.Components[0]["key"])
	require.NotContains(t, read.BillingContext.Plan.Components[0], "legacy")
	require.Equal(t, "alpha", read.BillingContext.Plan.Components[1]["key"])
	require.Equal(t, json.Number("18446744073709551617"), read.BillingContext.Plan.Components[1]["legacy"])
	components[1].Unit = "frame"
	_, err = common.Marshal(private)
	require.ErrorContains(t, err, "cannot match unmodeled task private history")
}
