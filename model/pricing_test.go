package model

import (
	"strconv"
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/setting/task_billing_setting"
	"github.com/stretchr/testify/require"
)

func TestGetPricingEndpointTypesForAdvancedCustomAbilityUsesConfiguredRoutes(t *testing.T) {
	config := &dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{
			{
				IncomingPath: "/v1/alpha/search",
				UpstreamPath: "/v1/alpha/search",
				Converter:    dto.AdvancedCustomConverterNone,
			},
			{
				IncomingPath: "/v1/responses",
				UpstreamPath: "/v1/responses",
				Converter:    dto.AdvancedCustomConverterNone,
			},
		},
	}
	require.NoError(t, config.Validate())

	ability := AbilityWithChannel{
		Ability:     Ability{ChannelId: 17, Model: "gpt-advanced-alpha"},
		ChannelType: constant.ChannelTypeAdvancedCustom,
	}
	configs := map[int]*dto.AdvancedCustomConfig{
		17: config,
	}

	endpointTypes := getPricingEndpointTypesForAbility(ability, configs)

	require.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAIAlphaSearch,
		constant.EndpointTypeOpenAIResponse,
	}, endpointTypes)
}

func TestNewTaskRateCardPricingExposesStructuredMinimaxBilling(t *testing.T) {
	pricing := newTaskRateCardPricing("minimax/minimax-h3", &task_billing_setting.RateCard{
		Vendor:      "minimax",
		BillingType: task_billing_setting.MinimaxBillingType,
		BillingConfig: map[string]any{
			"schema_version":               1,
			"mode":                         "bounded_actual",
			"currency":                     "USD",
			"output_unit_price":            map[string]any{"768P": "0.08", "2K": "0.13"},
			"input_video_unit_price":       map[string]any{"768P": "0.08", "2K": "0.13"},
			"input_video_max_seconds":      float64(15),
			"input_image_free_count":       float64(5),
			"input_image_extra_unit_price": "0.04",
			"input_audio_unit_price":       "0",
		},
	})

	require.NotNil(t, pricing)
	require.Equal(t, task_billing_setting.MinimaxBillingType, pricing.BillingType)
	require.Equal(t, "USD", pricing.Currency)
	require.Equal(t, "second", pricing.Unit)
	require.Equal(t, "output_duration", pricing.QuantityField)
	require.True(t, pricing.Strict)
	require.Equal(t, 0.08, pricing.MinUnitPrice)
	require.Equal(t, 0.13, pricing.MaxUnitPrice)
	require.Empty(t, pricing.Rows)
	require.Len(t, pricing.Components, 6)
	require.Equal(t, TaskRateCardPricingComponent{
		Key:         "output_video",
		Variant:     "768P",
		Unit:        "second",
		UnitPrice:   "0.08",
		MinQuantity: int64Pointer(4),
		MaxQuantity: int64Pointer(15),
	}, pricing.Components[0])
	require.Equal(t, TaskRateCardPricingComponent{
		Key:          "input_image",
		Unit:         "image",
		UnitPrice:    "0.04",
		FreeQuantity: int64Pointer(5),
		MaxQuantity:  int64Pointer(9),
	}, pricing.Components[4])
}

func TestNewTaskRateCardPricingOmitsNonPositiveInputVideoLimit(t *testing.T) {
	for _, limit := range []int64{0, -1} {
		t.Run(strconv.FormatInt(limit, 10), func(t *testing.T) {
			pricing := newTaskRateCardPricing("minimax/minimax-h3", &task_billing_setting.RateCard{
				Vendor:      "minimax",
				BillingType: task_billing_setting.MinimaxBillingType,
				BillingConfig: map[string]any{
					"schema_version":               1,
					"mode":                         "bounded_actual",
					"currency":                     "USD",
					"output_unit_price":            map[string]any{"768P": "0.08", "2K": "0.13"},
					"input_video_unit_price":       map[string]any{"768P": "0.08", "2K": "0.13"},
					"input_video_max_seconds":      limit,
					"input_image_extra_unit_price": "0.04",
					"input_audio_unit_price":       "0",
				},
			})

			require.NotNil(t, pricing)
			require.Len(t, pricing.Components, 6)
			require.Nil(t, pricing.Components[2].MaxQuantity)
			require.Nil(t, pricing.Components[3].MaxQuantity)
		})
	}
}

func TestNewTaskRateCardPricingFallsBackToRowsForUnknownBillingType(t *testing.T) {
	pricing := newTaskRateCardPricing("vendor/custom", &task_billing_setting.RateCard{
		Vendor:      "vendor",
		BillingType: "future_billing_type",
		Rows: []task_billing_setting.RateCardRow{
			{ID: "base", Match: map[string]string{"quality": "standard"}, UnitPrice: 0.42},
		},
	})

	require.NotNil(t, pricing)
	require.Empty(t, pricing.BillingType)
	require.Len(t, pricing.Rows, 1)
	require.Equal(t, "base", pricing.Rows[0].ID)
	require.Equal(t, 0.42, pricing.Rows[0].UnitPrice)
}

func TestNewTaskRateCardPricingFallsBackToRowsWhenStructuredDisplayIsInvalid(t *testing.T) {
	pricing := newTaskRateCardPricing("minimax/minimax-h3", &task_billing_setting.RateCard{
		Vendor:      "minimax",
		BillingType: task_billing_setting.MinimaxBillingType,
		BillingConfig: map[string]any{
			"output_unit_price": map[string]any{"768P": "invalid"},
		},
		Rows: []task_billing_setting.RateCardRow{
			{ID: "legacy", Match: map[string]string{"resolution": "768P"}, UnitPrice: 0.25},
		},
	})

	require.NotNil(t, pricing)
	require.Empty(t, pricing.BillingType)
	require.Len(t, pricing.Rows, 1)
	require.Equal(t, "legacy", pricing.Rows[0].ID)
}

func TestNewTaskRateCardPricingOmitsUnsupportedCardWithoutRows(t *testing.T) {
	require.Nil(t, newTaskRateCardPricing("vendor/custom", &task_billing_setting.RateCard{
		BillingType: "future_billing_type",
	}))
}

func int64Pointer(value int64) *int64 {
	return &value
}
