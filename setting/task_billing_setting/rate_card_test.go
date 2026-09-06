package task_billing_setting

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func withQuotaPerUnit(t *testing.T, value float64) {
	t.Helper()
	old := common.QuotaPerUnit
	common.QuotaPerUnit = value
	t.Cleanup(func() {
		common.QuotaPerUnit = old
	})
}

func TestCalculateKlingV3OmniNoVideoWithAudio(t *testing.T) {
	withQuotaPerUnit(t, 1000)

	input := types.TaskBillingInput{
		Model: "kling/kling-v3-omni-video-generation",
	}
	input.SetNumber("duration", 5)
	input.SetField("quality", "pro")
	input.SetField("has_video_input", "false")
	input.SetField("has_audio", "true")

	got, err := Calculate(input, 1)
	if err != nil {
		t.Fatalf("Calculate returned error: %v", err)
	}
	if got == nil {
		t.Fatal("Calculate returned nil")
	}
	if got.RowID != "pro_no_video_audio" {
		t.Fatalf("row = %q, want pro_no_video_audio", got.RowID)
	}
	if got.UnitPrice != 1.0 || got.TotalPrice != 5.0 || got.Quota != 5000 {
		t.Fatalf("unexpected billing result: %+v", got)
	}
}

func TestCalculateKlingV3OmniRejectsUnpricedVideoAudio(t *testing.T) {
	withQuotaPerUnit(t, 1000)

	input := types.TaskBillingInput{
		Model: "kling/kling-v3-omni-video-generation",
	}
	input.SetNumber("duration", 5)
	input.SetField("quality", "pro")
	input.SetField("has_video_input", "true")
	input.SetField("has_audio", "true")

	_, err := Calculate(input, 1)
	if err == nil {
		t.Fatal("expected an error for unconfigured video+audio price row")
	}
	if !strings.Contains(err.Error(), "no configured price row") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCalculateUnknownModelFallsBack(t *testing.T) {
	input := types.TaskBillingInput{Model: "unknown-video-model"}
	input.SetNumber("duration", 5)

	got, err := Calculate(input, 1)
	if err != nil {
		t.Fatalf("Calculate returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("Calculate returned %+v, want nil", got)
	}
}

func TestValidateRateCardsAcceptsStructuredMinimaxRule(t *testing.T) {
	raw := `{"minimax/minimax-h3":{"vendor":"minimax","billing_type":"minimax","billing_config":{"schema_version":1,"mode":"bounded_actual","currency":"USD","output_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_max_seconds":15,"input_image_free_count":5,"input_image_extra_unit_price":"0.04","input_audio_unit_price":"0"}}}`
	require.NoError(t, ValidateRateCardsJSON(raw))
}

func TestGetRateCardsCopyPreservesStructuredRowsRoundTrip(t *testing.T) {
	original := GetRateCardsCopy()
	raw := `{"minimax/minimax-h3":{"vendor":"minimax","billing_type":"minimax","billing_config":{"schema_version":1,"mode":"bounded_actual","currency":"USD","output_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_max_seconds":15,"input_image_free_count":5,"input_image_extra_unit_price":"0.04","input_audio_unit_price":"0"}}}`
	require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{"rate_cards": raw}))
	t.Cleanup(func() {
		data, err := common.Marshal(original)
		require.NoError(t, err)
		require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{"rate_cards": string(data)}))
	})

	copied := GetRateCardsCopy()
	card, ok := copied[MinimaxBillingRuleKey]
	require.True(t, ok)
	require.Nil(t, card.Rows)

	data, err := common.Marshal(copied)
	require.NoError(t, err)
	require.Contains(t, string(data), `"rows":null`)
	require.NoError(t, ValidateRateCardsJSON(string(data)))
}

func TestValidateRateCardsRejectsLegacyFieldsOnStructuredMinimaxRule(t *testing.T) {
	legacyFields := map[string]func(*RateCard){
		"unit":             func(card *RateCard) { card.Unit = "second" },
		"quantity_field":   func(card *RateCard) { card.QuantityField = "duration" },
		"default_quantity": func(card *RateCard) { card.DefaultQuantity = 1 },
		"strict":           func(card *RateCard) { card.Strict = true },
		"defaults":         func(card *RateCard) { card.Defaults = map[string]string{"quality": "high"} },
		"rows": func(card *RateCard) {
			card.Rows = []RateCardRow{{Match: map[string]string{"quality": "high"}, UnitPrice: 1}}
		},
	}

	for name, mutate := range legacyFields {
		t.Run(name, func(t *testing.T) {
			card := RateCard{
				Vendor:        "minimax",
				BillingType:   MinimaxBillingType,
				BillingConfig: encodeBillingConfig(defaultH3BillingConfig()),
			}
			mutate(&card)
			data, err := common.Marshal(map[string]RateCard{MinimaxBillingRuleKey: card})
			require.NoError(t, err)
			require.Error(t, ValidateRateCardsJSON(string(data)))
		})
	}
}

func TestValidateRateCardsRejectsExplicitZeroLegacyFieldsOnStructuredMinimaxRule(t *testing.T) {
	legacyFields := []string{
		`"unit":""`,
		`"quantity_field":""`,
		`"default_quantity":0`,
		`"strict":false`,
		`"defaults":{}`,
		`"rows":[]`,
	}
	for _, field := range legacyFields {
		raw := fmt.Sprintf(`{"minimax/minimax-h3":{"vendor":"minimax","billing_type":"minimax","billing_config":{"schema_version":1,"mode":"bounded_actual","currency":"USD","output_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_max_seconds":15,"input_image_free_count":5,"input_image_extra_unit_price":"0.04","input_audio_unit_price":"0"},%s}}`, field)
		require.ErrorContains(t, ValidateRateCardsJSON(raw), "structured billing cannot define")
	}
}

func TestValidateRateCardsRequiresCanonicalStructuredMinimaxKey(t *testing.T) {
	structured := RateCard{
		Vendor:        "minimax",
		BillingType:   MinimaxBillingType,
		BillingConfig: encodeBillingConfig(defaultH3BillingConfig()),
	}

	for _, key := range []string{MinimaxBillingRuleKey, "MiniMax-H3"} {
		data, err := common.Marshal(map[string]RateCard{key: structured})
		require.NoError(t, err)
		require.NoError(t, ValidateRateCardsJSON(string(data)), key)
	}

	for _, key := range []string{"minimax-h3", " MINIMAX/MINIMAX-H3 ", " MiniMax-H3 "} {
		data, err := common.Marshal(map[string]RateCard{key: structured})
		require.NoError(t, err)
		require.ErrorContains(t, ValidateRateCardsJSON(string(data)), "only supports MiniMax-H3", key)
	}
}

func TestValidateRateCardsRejectsStructuredNonH3Model(t *testing.T) {
	raw := `{"MiniMax-Hailuo-2.3":{"vendor":"minimax","billing_type":"minimax","billing_config":{"schema_version":1,"mode":"bounded_actual","currency":"USD","output_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_unit_price":{"768P":"0.08","2K":"0.13"},"input_video_max_seconds":15,"input_image_free_count":5,"input_image_extra_unit_price":"0.04","input_audio_unit_price":"0"}}}`
	require.ErrorContains(t, ValidateRateCardsJSON(raw), "only supports MiniMax-H3")
}

func TestHasRateCardIgnoresStructuredNonH3ExactKey(t *testing.T) {
	original := GetRateCardsCopy()
	data, err := common.Marshal(map[string]RateCard{
		"MiniMax-Hailuo-2.3": {
			Vendor:        "minimax",
			BillingType:   MinimaxBillingType,
			BillingConfig: encodeBillingConfig(defaultH3BillingConfig()),
		},
	})
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{"rate_cards": string(data)}))
	t.Cleanup(func() {
		data, marshalErr := common.Marshal(original)
		require.NoError(t, marshalErr)
		require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{"rate_cards": string(data)}))
	})

	require.False(t, HasRateCard("MiniMax-Hailuo-2.3"))
}

func TestCalculateSkipsStructuredMinimaxRule(t *testing.T) {
	withQuotaPerUnit(t, 1000)

	original := GetRateCardsCopy()
	structured := RateCard{
		Vendor:        "minimax",
		BillingType:   MinimaxBillingType,
		BillingConfig: encodeBillingConfig(defaultH3BillingConfig()),
	}
	data, err := common.Marshal(map[string]RateCard{"minimax/minimax-h3": structured})
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{"rate_cards": string(data)}))
	t.Cleanup(func() {
		data, marshalErr := common.Marshal(original)
		require.NoError(t, marshalErr)
		require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{"rate_cards": string(data)}))
	})

	got, err := Calculate(types.TaskBillingInput{Model: "minimax/minimax-h3"}, 1)
	require.NoError(t, err)
	require.Nil(t, got)
	require.False(t, HasRateCard("MiniMax-H3"))
}

func TestHasRateCardIgnoresStructuredWildcardKey(t *testing.T) {
	original := GetRateCardsCopy()
	data, err := common.Marshal(map[string]RateCard{
		"*": {
			Vendor:        "minimax",
			BillingType:   MinimaxBillingType,
			BillingConfig: encodeBillingConfig(defaultH3BillingConfig()),
		},
	})
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{"rate_cards": string(data)}))
	t.Cleanup(func() {
		data, marshalErr := common.Marshal(original)
		require.NoError(t, marshalErr)
		require.NoError(t, config.UpdateConfigFromMap(&taskBillingSetting, map[string]string{"rate_cards": string(data)}))
	})

	// This bypasses the config validation boundary to guard against stale or
	// programmatically loaded maps reintroducing structured wildcard cards.
	require.False(t, HasRateCard("MiniMax-H3"))
	require.False(t, HasRateCard("MiniMax-Hailuo-2.3"))
}

func TestRateCardsSupportConcurrentReloads(t *testing.T) {
	setting := TaskBillingSetting{RateCards: newRateCardMap(nil)}
	raw := `{"model":{"rows":[{"match":{"quality":"std"},"unit_price":1}]}}`

	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range 200 {
				_, _ = setting.RateCards.Get("model")
			}
		}()
	}

	for range 200 {
		require.NoError(t, config.UpdateConfigFromMap(&setting, map[string]string{"rate_cards": raw}))
	}
	readers.Wait()

	card, ok := setting.RateCards.Get("model")
	require.True(t, ok)
	require.Len(t, card.Rows, 1)
}
