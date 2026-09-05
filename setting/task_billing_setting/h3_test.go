package task_billing_setting

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func withH3BillingProfiles(t *testing.T, profiles map[string]H3BillingConfig) {
	t.Helper()
	original := GetH3BillingProfilesCopy()
	data, err := common.Marshal(profiles)
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{
		"h3_profiles": string(data),
	}))
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{
			"h3_profiles": mustMarshalH3Profiles(original),
		}))
	})
}

func mustMarshalH3Profiles(profiles map[string]H3BillingConfig) string {
	data, err := common.Marshal(profiles)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestBuildH3BillingPlanUsesOneAggregateInputVideoCap(t *testing.T) {
	withQuotaPerUnit(t, 1000)
	plan, err := BuildH3BillingPlan(H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 3,
	}, 1)
	require.NoError(t, err)
	reserve, err := QuoteH3Reserve(plan)
	require.NoError(t, err)
	// 5 output seconds + one aggregate 15-second input-video cap.
	require.EqualValues(t, 20, reserve.OutputSeconds+reserve.InputVideoSeconds)
	require.EqualValues(t, 1600, reserve.Quota)
}

func TestBuildH3BillingPlanReadsMinimaxRuleFromUnifiedRateCards(t *testing.T) {
	withQuotaPerUnit(t, 1000)
	original := GetRateCardsCopy()
	profile := defaultH3BillingConfig()
	profile.OutputUnitPrice["768P"] = "0.10"
	data, err := common.Marshal(map[string]RateCard{
		MinimaxBillingRuleKey: {
			Vendor:        "minimax",
			BillingType:   MinimaxBillingType,
			BillingConfig: encodeBillingConfig(profile),
		},
	})
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{"rate_cards": string(data)}))
	t.Cleanup(func() {
		data, marshalErr := common.Marshal(original)
		require.NoError(t, marshalErr)
		require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{"rate_cards": string(data)}))
	})

	plan, err := BuildH3BillingPlanForModels(H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	}, 1, "MiniMax-H3")
	require.NoError(t, err)
	require.Equal(t, H3BillingSource, plan.Source)
	require.Equal(t, MinimaxBillingRuleKey, plan.RuleKey)
	require.EqualValues(t, 500, plan.EstimateQuota)
}

func TestBuildH3BillingPlanDoesNotChargeFreeImagesOrAudio(t *testing.T) {
	withQuotaPerUnit(t, 1000)
	plan, err := BuildH3BillingPlan(H3BillingInput{
		Resolution: "2K", OutputDurationSeconds: 5, InputImageCount: 5, InputAudioCount: 1,
	}, 1)
	require.NoError(t, err)
	require.EqualValues(t, 650, plan.EstimateQuota)
	require.EqualValues(t, 650, plan.ReserveQuota)

	usage := &types.TaskUsage{
		OutputDurationMs:     int64Ptr(5_000),
		InputAudioDurationMs: int64Ptr(6_000),
		InputImageCount:      int64Ptr(5),
		Completeness:         types.TaskUsageCompletenessComplete,
	}
	final, err := QuoteH3Final(plan, usage)
	require.NoError(t, err)
	require.EqualValues(t, 650, final.Quota)
	require.EqualValues(t, 6, final.InputAudioSeconds)
}

func TestQuoteH3FinalRejectsMissingOrConflictingUsage(t *testing.T) {
	withQuotaPerUnit(t, 1000)
	plan, err := BuildH3BillingPlan(H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1,
	}, 1)
	require.NoError(t, err)

	_, err = QuoteH3Final(plan, &types.TaskUsage{
		OutputDurationMs: int64Ptr(5_000),
		Completeness:     types.TaskUsageCompletenessPartial,
	})
	require.Error(t, err)

	_, err = QuoteH3Final(plan, &types.TaskUsage{
		OutputDurationMs:     int64Ptr(5_000),
		InputVideoDurationMs: int64Ptr(0),
		Completeness:         types.TaskUsageCompletenessComplete,
	})
	require.ErrorContains(t, err, "input_image_count is missing")

	_, err = QuoteH3Final(plan, &types.TaskUsage{
		OutputDurationMs:     int64Ptr(5_000),
		InputVideoDurationMs: int64Ptr(16_000),
		InputImageCount:      int64Ptr(0),
		Completeness:         types.TaskUsageCompletenessComplete,
	})
	require.Error(t, err)
}

func TestQuoteH3FinalSupportsFractionalMediaSeconds(t *testing.T) {
	withQuotaPerUnit(t, 1000)
	plan, err := BuildH3BillingPlan(H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: 1, InputAudioCount: 1,
	}, 1)
	require.NoError(t, err)

	final, err := QuoteH3Final(plan, &types.TaskUsage{
		OutputDurationMs:     int64Ptr(5_000),
		InputVideoDurationMs: int64Ptr(7_500),
		InputAudioDurationMs: int64Ptr(2_250),
		InputImageCount:      int64Ptr(0),
		Completeness:         types.TaskUsageCompletenessComplete,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1000, final.Quota)
	require.Equal(t, int64(7_500), final.InputVideoDurationMs)
	require.Equal(t, "7.5", final.Components[1].QuantityDecimal)
}

func TestQuoteH3FinalRejectsImageCountMismatch(t *testing.T) {
	withQuotaPerUnit(t, 1000)
	plan, err := BuildH3BillingPlan(H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputImageCount: 2,
	}, 1)
	require.NoError(t, err)

	_, err = QuoteH3Final(plan, &types.TaskUsage{
		OutputDurationMs: int64Ptr(5_000),
		InputImageCount:  int64Ptr(1),
		Completeness:     types.TaskUsageCompletenessComplete,
	})
	require.Error(t, err)
}

func TestH3BillingConfigValidationRejectsIncompleteProfile(t *testing.T) {
	raw := `{"minimax_h3_v2":{"schema_version":1,"mode":"bounded_actual","currency":"USD","output_unit_price":{"768P":"0.08"},"input_video_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_max_seconds":15,"input_image_free_count":5,"input_image_extra_unit_price":"0.04","input_audio_unit_price":"0"}}`
	require.Error(t, ValidateH3ProfilesJSON(raw))
}

func TestH3BillingConfigValidationRejectsEmptyProfiles(t *testing.T) {
	require.Error(t, ValidateH3ProfilesJSON(""))
	require.Error(t, ValidateH3ProfilesJSON("   "))
}

func TestH3BillingConfigValidationRejectsProviderLimitOverflow(t *testing.T) {
	profiles := defaultH3BillingProfiles()
	profile := profiles[H3BillingProfileKey]
	profile.InputVideoMaxSeconds = 16
	profiles[H3BillingProfileKey] = profile
	raw, err := common.Marshal(profiles)
	require.NoError(t, err)
	require.Error(t, ValidateH3ProfilesJSON(string(raw)))

	profile.InputVideoMaxSeconds = 15
	profile.InputImageFreeCount = 10
	profiles[H3BillingProfileKey] = profile
	raw, err = common.Marshal(profiles)
	require.NoError(t, err)
	require.Error(t, ValidateH3ProfilesJSON(string(raw)))
}

func TestH3BillingConfigValidationRejectsPaidAudioInSchemaV1(t *testing.T) {
	profiles := defaultH3BillingProfiles()
	profile := profiles[H3BillingProfileKey]
	profile.InputAudioUnitPrice = "0.01"
	profiles[H3BillingProfileKey] = profile
	raw, err := common.Marshal(profiles)
	require.NoError(t, err)
	require.Error(t, ValidateH3ProfilesJSON(string(raw)))
}

func TestH3BillingPlanKeepsFrozenPricesAfterConfigReload(t *testing.T) {
	withQuotaPerUnit(t, 1000)
	original := GetH3BillingProfilesCopy()
	withH3BillingProfiles(t, original)

	oldPlan, err := BuildH3BillingPlan(H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	}, 1)
	require.NoError(t, err)
	oldQuote, err := QuoteH3Reserve(oldPlan)
	require.NoError(t, err)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 2000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	updated := GetH3BillingProfilesCopy()
	profile := updated[H3BillingProfileKey]
	profile.OutputUnitPrice["768P"] = "0.20"
	updated[H3BillingProfileKey] = profile
	require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{
		"h3_profiles": mustMarshalH3Profiles(updated),
	}))

	newPlan, err := BuildH3BillingPlan(H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	}, 1)
	require.NoError(t, err)
	newQuote, err := QuoteH3Reserve(newPlan)
	require.NoError(t, err)
	require.EqualValues(t, 400, oldQuote.Quota)
	require.EqualValues(t, 2000, newQuote.Quota)
	require.NotEqual(t, oldPlan.ConfigHash, newPlan.ConfigHash)

	oldQuoteAgain, err := QuoteH3Reserve(oldPlan)
	require.NoError(t, err)
	require.Equal(t, oldQuote, oldQuoteAgain)
}

func TestPreviewH3BillingUsesDraftWithoutChangingGlobalConfig(t *testing.T) {
	withQuotaPerUnit(t, 1000)
	original := GetH3BillingProfilesCopy()
	draft := cloneH3BillingConfig(original[H3BillingProfileKey])
	draft.OutputUnitPrice["768P"] = "0.10"
	draft.InputVideoUnitPrice["768P"] = "0.10"

	preview, err := PreviewH3Billing(draft, H3BillingInput{
		Resolution:            "768P",
		OutputDurationSeconds: 5,
		InputVideoCount:       1,
	}, 1, &types.TaskUsage{
		OutputDurationMs:     int64Ptr(5_000),
		InputVideoDurationMs: int64Ptr(7_500),
		InputImageCount:      int64Ptr(0),
		Completeness:         types.TaskUsageCompletenessComplete,
	})
	require.NoError(t, err)
	require.Equal(t, "0.5", preview.Estimate.Price)
	require.Equal(t, "2", preview.Reserve.Price)
	require.Equal(t, "1.25", preview.Final.Price)
	require.Equal(t, -750, *preview.AdjustmentQuota)
	require.Equal(t, 750, *preview.RefundQuota)
	require.NotEqual(t, hashH3BillingConfig(original[H3BillingProfileKey]), preview.ConfigHash)
	require.Equal(t, original, GetH3BillingProfilesCopy())
}

func TestPreviewH3BillingRejectsInvalidDraftAndRequestBounds(t *testing.T) {
	profile, ok := GetH3BillingProfileCopy(H3BillingProfileKey)
	require.True(t, ok)

	invalidProfile := cloneH3BillingConfig(*profile)
	invalidProfile.InputAudioUnitPrice = "0.01"
	_, err := PreviewH3Billing(invalidProfile, H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5,
	}, 1, nil)
	require.Error(t, err)

	_, err = PreviewH3Billing(*profile, H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputImageCount: H3MaxInputImageCount + 1,
	}, 1, nil)
	require.Error(t, err)

	_, err = PreviewH3Billing(*profile, H3BillingInput{
		Resolution: "768P", OutputDurationSeconds: 5, InputVideoCount: H3MaxInputVideoCount + 1,
	}, 1, nil)
	require.Error(t, err)
}

func int64Ptr(value int64) *int64 {
	return &value
}
