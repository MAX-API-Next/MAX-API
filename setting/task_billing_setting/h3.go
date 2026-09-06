package task_billing_setting

import (
	"crypto/sha256"
	"fmt"
	"math"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/shopspring/decimal"
)

const (
	H3BillingSource       = "minimax"
	MinimaxBillingRuleKey = "minimax/minimax-h3"
	MinimaxBillingType    = RateCardBillingTypeMinimax
	LegacyH3BillingSource = "minimax_hailuo"
	H3BillingProfileKey   = "minimax_h3_v2" // Deprecated legacy option key.
	H3BillingMode         = types.TaskBillingPlanModeBoundedActual

	H3MinOutputDurationSeconds     int64 = 4
	H3MaxOutputDurationSeconds     int64 = 15
	H3MaxInputMediaDurationSeconds int64 = 15
	H3MaxInputVideoCount           int64 = 3
	H3MaxInputAudioCount           int64 = 3
	H3MaxInputImageCount           int64 = 9
)

type H3BillingConfig struct {
	SchemaVersion            int               `json:"schema_version"`
	Mode                     string            `json:"mode"`
	Currency                 string            `json:"currency"`
	OutputUnitPrice          map[string]string `json:"output_unit_price"`
	InputVideoUnitPrice      map[string]string `json:"input_video_unit_price"`
	InputVideoMaxSeconds     int64             `json:"input_video_max_seconds"`
	InputImageFreeCount      int64             `json:"input_image_free_count"`
	InputImageExtraUnitPrice string            `json:"input_image_extra_unit_price"`
	InputAudioUnitPrice      string            `json:"input_audio_unit_price"`
}

// H3BillingInput contains only request facts normalized by the H3 adaptor.
// It deliberately does not contain raw provider JSON or media contents.
type H3BillingInput struct {
	Resolution            string
	OutputDurationSeconds int64
	InputVideoCount       int64
	InputAudioCount       int64
	InputImageCount       int64
}

type H3BillingQuoteComponent struct {
	Key             string `json:"key"`
	Unit            string `json:"unit"`
	Quantity        int64  `json:"quantity"`
	QuantityDecimal string `json:"quantity_decimal,omitempty"`
	UnitPrice       string `json:"unit_price"`
	Price           string `json:"price"`
}

type H3BillingQuote struct {
	Stage                string                    `json:"stage"`
	Price                string                    `json:"price"`
	Quota                int                       `json:"quota"`
	OutputSeconds        int64                     `json:"output_seconds"`
	InputVideoSeconds    int64                     `json:"input_video_seconds"`
	InputAudioSeconds    int64                     `json:"input_audio_seconds"`
	OutputDurationMs     int64                     `json:"output_duration_ms"`
	InputVideoDurationMs int64                     `json:"input_video_duration_ms"`
	InputAudioDurationMs int64                     `json:"input_audio_duration_ms"`
	InputImageCount      int64                     `json:"input_image_count"`
	Components           []H3BillingQuoteComponent `json:"components"`
}

type H3BillingPreview struct {
	ConfigHash      string          `json:"config_hash"`
	QuotaPerUnit    float64         `json:"quota_per_unit"`
	GroupRatio      float64         `json:"group_ratio"`
	Estimate        *H3BillingQuote `json:"estimate"`
	Reserve         *H3BillingQuote `json:"reserve"`
	Final           *H3BillingQuote `json:"final,omitempty"`
	AdjustmentQuota *int            `json:"adjustment_quota,omitempty"`
	RefundQuota     *int            `json:"refund_quota,omitempty"`
}

func defaultH3BillingProfiles() map[string]H3BillingConfig {
	return map[string]H3BillingConfig{
		H3BillingProfileKey: {
			SchemaVersion:            1,
			Mode:                     H3BillingMode,
			Currency:                 "USD",
			OutputUnitPrice:          map[string]string{"768P": "0.08", "2K": "0.13"},
			InputVideoUnitPrice:      map[string]string{"768P": "0.08", "2K": "0.13"},
			InputVideoMaxSeconds:     H3MaxInputMediaDurationSeconds,
			InputImageFreeCount:      5,
			InputImageExtraUnitPrice: "0.04",
			InputAudioUnitPrice:      "0",
		},
	}
}

func defaultH3BillingConfig() H3BillingConfig {
	return defaultH3BillingProfiles()[H3BillingProfileKey]
}

func encodeBillingConfig(config H3BillingConfig) map[string]any {
	data, err := common.Marshal(config)
	if err != nil {
		return nil
	}
	var result map[string]any
	if err := common.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
}

func decodeMinimaxBillingConfig(raw map[string]any) (H3BillingConfig, error) {
	if len(raw) == 0 {
		return H3BillingConfig{}, fmt.Errorf("MiniMax billing_config cannot be empty")
	}
	data, err := common.Marshal(raw)
	if err != nil {
		return H3BillingConfig{}, fmt.Errorf("marshal MiniMax billing_config: %w", err)
	}
	var config H3BillingConfig
	if err := common.Unmarshal(data, &config); err != nil {
		return H3BillingConfig{}, fmt.Errorf("decode MiniMax billing_config: %w", err)
	}
	return config, nil
}

func isMinimaxH3Model(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), constant.TaskModelMiniMaxH3)
}

func newH3BillingProfileMap(profiles map[string]H3BillingConfig) *types.RWMap[string, H3BillingConfig] {
	m := types.NewRWMap[string, H3BillingConfig]()
	m.AddAll(profiles)
	return m
}

func GetH3BillingProfilesCopy() map[string]H3BillingConfig {
	if taskBillingSetting.H3Profiles == nil {
		return map[string]H3BillingConfig{}
	}
	profiles := taskBillingSetting.H3Profiles.ReadAll()
	result := make(map[string]H3BillingConfig, len(profiles))
	for key, profile := range profiles {
		result[key] = cloneH3BillingConfig(profile)
	}
	return result
}

func GetH3BillingProfileCopy(key string) (*H3BillingConfig, bool) {
	if taskBillingSetting.H3Profiles == nil {
		return nil, false
	}
	profile, ok := taskBillingSetting.H3Profiles.Get(strings.TrimSpace(key))
	if !ok {
		return nil, false
	}
	profile = cloneH3BillingConfig(profile)
	return &profile, true
}

func ValidateH3ProfilesJSON(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("H3 billing profiles cannot be empty")
	}
	var profiles map[string]H3BillingConfig
	if err := common.UnmarshalJsonStr(raw, &profiles); err != nil {
		return err
	}
	return validateH3Profiles(profiles)
}

func validateH3Profiles(profiles map[string]H3BillingConfig) error {
	for key, profile := range profiles {
		if err := validateH3BillingConfig(key, profile); err != nil {
			return err
		}
	}
	if _, ok := profiles[H3BillingProfileKey]; !ok {
		return fmt.Errorf("H3 billing profile %s is required", H3BillingProfileKey)
	}
	return nil
}

func validateH3BillingConfig(key string, profile H3BillingConfig) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("H3 billing profile key cannot be empty")
	}
	if profile.SchemaVersion != 1 {
		return fmt.Errorf("H3 billing profile %s has unsupported schema_version", key)
	}
	if profile.Mode != H3BillingMode {
		return fmt.Errorf("H3 billing profile %s has unsupported mode", key)
	}
	if strings.TrimSpace(profile.Currency) == "" {
		return fmt.Errorf("H3 billing profile %s currency cannot be empty", key)
	}
	if profile.InputVideoMaxSeconds <= 0 || profile.InputVideoMaxSeconds > H3MaxInputMediaDurationSeconds {
		return fmt.Errorf("H3 billing profile %s input_video_max_seconds must be between 1 and %d", key, H3MaxInputMediaDurationSeconds)
	}
	if profile.InputImageFreeCount < 0 || profile.InputImageFreeCount > H3MaxInputImageCount {
		return fmt.Errorf("H3 billing profile %s input_image_free_count must be between 0 and %d", key, H3MaxInputImageCount)
	}
	if err := validateResolutionPrices(key, "output_unit_price", profile.OutputUnitPrice); err != nil {
		return err
	}
	if err := validateResolutionPrices(key, "input_video_unit_price", profile.InputVideoUnitPrice); err != nil {
		return err
	}
	for name, price := range map[string]string{
		"input_image_extra_unit_price": profile.InputImageExtraUnitPrice,
		"input_audio_unit_price":       profile.InputAudioUnitPrice,
	} {
		parsedPrice, err := nonNegativePrice(price)
		if err != nil {
			return fmt.Errorf("H3 billing profile %s %s: %w", key, name, err)
		}
		if name == "input_audio_unit_price" && !parsedPrice.IsZero() {
			return fmt.Errorf("H3 billing profile %s input_audio_unit_price must be zero in schema version 1", key)
		}
	}
	return nil
}

func validateResolutionPrices(key, field string, prices map[string]string) error {
	for _, resolution := range []string{"768P", "2K"} {
		price, ok := prices[resolution]
		if !ok {
			return fmt.Errorf("H3 billing profile %s %s must define %s", key, field, resolution)
		}
		if _, err := nonNegativePrice(price); err != nil {
			return fmt.Errorf("H3 billing profile %s %s[%s]: %w", key, field, resolution, err)
		}
	}
	return nil
}

func nonNegativePrice(value string) (decimal.Decimal, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return decimal.Zero, fmt.Errorf("price cannot be empty")
	}
	price, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, fmt.Errorf("price must be a decimal string")
	}
	if price.IsNegative() {
		return decimal.Zero, fmt.Errorf("price cannot be negative")
	}
	return price, nil
}

func BuildH3BillingPlan(input H3BillingInput, groupRatio float64) (*types.TaskBillingPlan, error) {
	profile, ok := GetH3BillingProfileCopy(H3BillingProfileKey)
	if !ok {
		return nil, fmt.Errorf("H3 billing profile %s is not configured", H3BillingProfileKey)
	}
	return buildH3BillingPlan(*profile, input, groupRatio, LegacyH3BillingSource, H3BillingProfileKey)
}

// BuildH3BillingPlanForModels selects an explicitly configured structured
// MiniMax rule from the unified task rate-card option. The legacy profile is
// the fallback when no unified MiniMax card was saved.
func BuildH3BillingPlanForModels(input H3BillingInput, groupRatio float64, models ...string) (*types.TaskBillingPlan, error) {
	card, key := findMinimaxRateCard(models...)
	if card != nil {
		profile, err := decodeMinimaxBillingConfig(card.BillingConfig)
		if err != nil {
			return nil, fmt.Errorf("rate card %s: %w", key, err)
		}
		return buildH3BillingPlan(profile, input, groupRatio, H3BillingSource, key)
	}
	return BuildH3BillingPlan(input, groupRatio)
}

func PreviewH3Billing(config H3BillingConfig, input H3BillingInput, groupRatio float64, usage *types.TaskUsage) (*H3BillingPreview, error) {
	plan, err := buildH3BillingPlan(config, input, groupRatio, LegacyH3BillingSource, H3BillingProfileKey)
	if err != nil {
		return nil, err
	}
	estimate, err := QuoteH3Estimate(plan)
	if err != nil {
		return nil, err
	}
	reserve, err := QuoteH3Reserve(plan)
	if err != nil {
		return nil, err
	}
	preview := &H3BillingPreview{
		ConfigHash:   plan.ConfigHash,
		QuotaPerUnit: plan.QuotaPerUnit,
		GroupRatio:   plan.GroupRatio,
		Estimate:     estimate,
		Reserve:      reserve,
	}
	if usage == nil {
		return preview, nil
	}
	finalQuote, err := QuoteH3Final(plan, usage)
	if err != nil {
		return nil, err
	}
	adjustmentQuota := finalQuote.Quota - reserve.Quota
	refundQuota := reserve.Quota - finalQuote.Quota
	if refundQuota < 0 {
		refundQuota = 0
	}
	preview.Final = finalQuote
	preview.AdjustmentQuota = &adjustmentQuota
	preview.RefundQuota = &refundQuota
	return preview, nil
}

func buildH3BillingPlan(profile H3BillingConfig, input H3BillingInput, groupRatio float64, source, ruleKey string) (*types.TaskBillingPlan, error) {
	if err := validateH3BillingConfig(ruleKey, profile); err != nil {
		return nil, err
	}
	resolution := strings.ToUpper(strings.TrimSpace(input.Resolution))
	if resolution != "768P" && resolution != "2K" {
		return nil, fmt.Errorf("unsupported H3 resolution %q", input.Resolution)
	}
	if input.OutputDurationSeconds < H3MinOutputDurationSeconds || input.OutputDurationSeconds > H3MaxOutputDurationSeconds ||
		input.InputVideoCount < 0 || input.InputVideoCount > H3MaxInputVideoCount ||
		input.InputAudioCount < 0 || input.InputAudioCount > H3MaxInputAudioCount ||
		input.InputImageCount < 0 || input.InputImageCount > H3MaxInputImageCount {
		return nil, fmt.Errorf("H3 billing request facts are invalid")
	}
	if math.IsNaN(groupRatio) || math.IsInf(groupRatio, 0) || groupRatio < 0 {
		return nil, fmt.Errorf("H3 group ratio must be finite and non-negative")
	}
	if math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) || common.QuotaPerUnit <= 0 {
		return nil, fmt.Errorf("H3 quota per unit must be finite and positive")
	}

	videoReserve := int64(0)
	if input.InputVideoCount > 0 {
		videoReserve = profile.InputVideoMaxSeconds
	}
	plan := &types.TaskBillingPlan{
		Source:                         source,
		RuleKey:                        ruleKey,
		SchemaVersion:                  profile.SchemaVersion,
		Mode:                           profile.Mode,
		ConfigHash:                     hashH3BillingConfig(profile),
		Currency:                       profile.Currency,
		GroupRatio:                     groupRatio,
		QuotaPerUnit:                   common.QuotaPerUnit,
		Resolution:                     resolution,
		RequestedOutputDurationSeconds: input.OutputDurationSeconds,
		InputVideoCount:                input.InputVideoCount,
		InputAudioCount:                input.InputAudioCount,
		InputImageCount:                input.InputImageCount,
		Components: []types.TaskBillingPlanComponent{
			{Key: "output_video", Unit: "second", UnitPrice: profile.OutputUnitPrice[resolution], RequestedQuantity: input.OutputDurationSeconds, ReservedQuantity: input.OutputDurationSeconds},
			{Key: "input_video", Unit: "second", UnitPrice: profile.InputVideoUnitPrice[resolution], ReservedQuantity: videoReserve},
			{Key: "input_image", Unit: "image", UnitPrice: profile.InputImageExtraUnitPrice, RequestedQuantity: input.InputImageCount, ReservedQuantity: input.InputImageCount, FreeQuantity: profile.InputImageFreeCount},
			{Key: "input_audio", Unit: "second", UnitPrice: profile.InputAudioUnitPrice, ReservedQuantity: 0},
		},
	}

	estimate, err := quoteH3Plan(plan, types.TaskBillingPlanStageEstimate, input.OutputDurationSeconds, 0, 0, input.InputImageCount)
	if err != nil {
		return nil, err
	}
	reserve, err := quoteH3Plan(plan, types.TaskBillingPlanStageReserve, input.OutputDurationSeconds, videoReserve, 0, input.InputImageCount)
	if err != nil {
		return nil, err
	}
	plan.EstimateQuota = estimate.Quota
	plan.ReserveQuota = reserve.Quota
	return plan, nil
}

func QuoteH3Estimate(plan *types.TaskBillingPlan) (*H3BillingQuote, error) {
	if plan == nil {
		return nil, fmt.Errorf("H3 billing plan is required")
	}
	return quoteH3Plan(plan, types.TaskBillingPlanStageEstimate, plan.RequestedOutputDurationSeconds, 0, 0, plan.InputImageCount)
}

func QuoteH3Reserve(plan *types.TaskBillingPlan) (*H3BillingQuote, error) {
	if plan == nil {
		return nil, fmt.Errorf("H3 billing plan is required")
	}
	videoSeconds := int64(0)
	if plan.InputVideoCount > 0 {
		for _, component := range plan.Components {
			if component.Key == "input_video" {
				videoSeconds = component.ReservedQuantity
				break
			}
		}
	}
	return quoteH3Plan(plan, types.TaskBillingPlanStageReserve, plan.RequestedOutputDurationSeconds, videoSeconds, 0, plan.InputImageCount)
}

func QuoteH3Final(plan *types.TaskBillingPlan, usage *types.TaskUsage) (*H3BillingQuote, error) {
	if plan == nil || usage == nil {
		return nil, fmt.Errorf("H3 billing plan and usage are required")
	}
	if err := validateH3BillingPlan(plan); err != nil {
		return nil, err
	}
	if usage.Completeness != types.TaskUsageCompletenessComplete {
		return nil, fmt.Errorf("H3 usage is not automatically billable: %s", usage.Completeness)
	}
	outputMs, err := usageMilliseconds(usage.OutputDurationMs, "output_duration_ms", true, H3MinOutputDurationSeconds, H3MaxOutputDurationSeconds)
	if err != nil {
		return nil, err
	}
	if outputMs > plan.RequestedOutputDurationSeconds*1000 {
		return nil, fmt.Errorf("H3 output usage exceeds the requested duration")
	}
	videoMs, err := usageMilliseconds(usage.InputVideoDurationMs, "input_video_duration_ms", plan.InputVideoCount > 0, 0, H3MaxInputMediaDurationSeconds)
	if err != nil {
		return nil, err
	}
	videoReserve := int64(0)
	for _, component := range plan.Components {
		if component.Key == "input_video" {
			videoReserve = component.ReservedQuantity
			break
		}
	}
	if videoMs > videoReserve*1000 {
		return nil, fmt.Errorf("H3 input video usage exceeds the reserved maximum")
	}
	if plan.InputVideoCount == 0 && videoMs > 0 {
		return nil, fmt.Errorf("H3 provider reported input video usage without a request video")
	}
	audioMs, err := usageMilliseconds(usage.InputAudioDurationMs, "input_audio_duration_ms", plan.InputAudioCount > 0, 0, H3MaxInputMediaDurationSeconds)
	if err != nil {
		return nil, err
	}
	if plan.InputAudioCount == 0 && audioMs > 0 {
		return nil, fmt.Errorf("H3 provider reported input audio usage without a request audio")
	}
	if usage.InputImageCount == nil {
		return nil, fmt.Errorf("H3 input_image_count is missing")
	}
	if *usage.InputImageCount < 0 {
		return nil, fmt.Errorf("H3 input_image_count cannot be negative")
	}
	imageCount := *usage.InputImageCount
	if imageCount != plan.InputImageCount {
		return nil, fmt.Errorf("H3 input_image_count does not match the request snapshot")
	}
	quote, err := quoteH3PlanMilliseconds(plan, types.TaskBillingPlanStageFinal, outputMs, videoMs, audioMs, imageCount)
	if err != nil {
		return nil, err
	}
	if quote.Quota > plan.ReserveQuota {
		return nil, fmt.Errorf("H3 final quota exceeds reserved quota")
	}
	return quote, nil
}

func quoteH3Plan(plan *types.TaskBillingPlan, stage string, outputSeconds, videoSeconds, audioSeconds, imageCount int64) (*H3BillingQuote, error) {
	if outputSeconds > math.MaxInt64/1000 || videoSeconds > math.MaxInt64/1000 || audioSeconds > math.MaxInt64/1000 {
		return nil, fmt.Errorf("H3 billing quantities are out of range")
	}
	return quoteH3PlanMilliseconds(plan, stage, outputSeconds*1000, videoSeconds*1000, audioSeconds*1000, imageCount)
}

func quoteH3PlanMilliseconds(plan *types.TaskBillingPlan, stage string, outputMs, videoMs, audioMs, imageCount int64) (*H3BillingQuote, error) {
	if err := validateH3BillingPlan(plan); err != nil {
		return nil, err
	}
	if outputMs < 0 || videoMs < 0 || audioMs < 0 || imageCount < 0 {
		return nil, fmt.Errorf("H3 billing quantities cannot be negative")
	}
	outputPrice, err := planComponentPrice(plan, "output_video")
	if err != nil {
		return nil, err
	}
	videoPrice, err := planComponentPrice(plan, "input_video")
	if err != nil {
		return nil, err
	}
	audioPrice, err := planComponentPrice(plan, "input_audio")
	if err != nil {
		return nil, err
	}
	imagePrice, err := planComponentPrice(plan, "input_image")
	if err != nil {
		return nil, err
	}
	imageFree := planComponentFreeQuantity(plan, "input_image")
	imageBillable := imageCount - imageFree
	if imageBillable < 0 {
		imageBillable = 0
	}

	outputQuantity := decimal.NewFromInt(outputMs).Div(decimal.NewFromInt(1000))
	videoQuantity := decimal.NewFromInt(videoMs).Div(decimal.NewFromInt(1000))
	audioQuantity := decimal.NewFromInt(audioMs).Div(decimal.NewFromInt(1000))
	price := outputQuantity.Mul(outputPrice)
	price = price.Add(videoQuantity.Mul(videoPrice))
	price = price.Add(audioQuantity.Mul(audioPrice))
	price = price.Add(decimal.NewFromInt(imageBillable).Mul(imagePrice))
	quotaDecimal := price.Mul(decimal.NewFromFloat(plan.QuotaPerUnit)).Mul(decimal.NewFromFloat(plan.GroupRatio))
	quota, err := common.QuotaFromDecimalStrict(quotaDecimal)
	if err != nil {
		return nil, err
	}
	return &H3BillingQuote{
		Stage:                stage,
		Price:                price.String(),
		Quota:                quota,
		OutputSeconds:        outputMs / 1000,
		InputVideoSeconds:    videoMs / 1000,
		InputAudioSeconds:    audioMs / 1000,
		OutputDurationMs:     outputMs,
		InputVideoDurationMs: videoMs,
		InputAudioDurationMs: audioMs,
		InputImageCount:      imageCount,
		Components: []H3BillingQuoteComponent{
			{Key: "output_video", Unit: "second", Quantity: outputMs / 1000, QuantityDecimal: outputQuantity.String(), UnitPrice: outputPrice.String(), Price: outputQuantity.Mul(outputPrice).String()},
			{Key: "input_video", Unit: "second", Quantity: videoMs / 1000, QuantityDecimal: videoQuantity.String(), UnitPrice: videoPrice.String(), Price: videoQuantity.Mul(videoPrice).String()},
			{Key: "input_audio", Unit: "second", Quantity: audioMs / 1000, QuantityDecimal: audioQuantity.String(), UnitPrice: audioPrice.String(), Price: audioQuantity.Mul(audioPrice).String()},
			{Key: "input_image", Unit: "image", Quantity: imageBillable, QuantityDecimal: decimal.NewFromInt(imageBillable).String(), UnitPrice: imagePrice.String(), Price: decimal.NewFromInt(imageBillable).Mul(imagePrice).String()},
		},
	}, nil
}

func validateH3BillingPlan(plan *types.TaskBillingPlan) error {
	if plan == nil {
		return fmt.Errorf("H3 billing plan identity is invalid")
	}
	if plan.Source != H3BillingSource && plan.Source != LegacyH3BillingSource {
		return fmt.Errorf("H3 billing plan source is invalid")
	}
	if strings.TrimSpace(plan.RuleKey) == "" {
		return fmt.Errorf("H3 billing plan rule key is invalid")
	}
	if plan.SchemaVersion != 1 {
		return fmt.Errorf("H3 billing plan schema version is invalid")
	}
	if plan.Mode != H3BillingMode {
		return fmt.Errorf("H3 billing plan mode is invalid")
	}
	if strings.TrimSpace(plan.ConfigHash) == "" {
		return fmt.Errorf("H3 billing plan config hash is invalid")
	}
	if plan.GroupRatio < 0 || math.IsNaN(plan.GroupRatio) || math.IsInf(plan.GroupRatio, 0) {
		return fmt.Errorf("H3 billing plan group ratio is invalid")
	}
	if plan.QuotaPerUnit <= 0 || math.IsNaN(plan.QuotaPerUnit) || math.IsInf(plan.QuotaPerUnit, 0) {
		return fmt.Errorf("H3 billing plan quota per unit is invalid")
	}
	if plan.Resolution != "768P" && plan.Resolution != "2K" {
		return fmt.Errorf("H3 billing plan resolution is invalid")
	}
	if plan.RequestedOutputDurationSeconds < H3MinOutputDurationSeconds || plan.RequestedOutputDurationSeconds > H3MaxOutputDurationSeconds {
		return fmt.Errorf("H3 billing plan requested output duration is invalid")
	}
	if plan.InputVideoCount < 0 {
		return fmt.Errorf("H3 billing plan input video count is invalid")
	}
	if plan.InputAudioCount < 0 {
		return fmt.Errorf("H3 billing plan input audio count is invalid")
	}
	if plan.InputImageCount < 0 {
		return fmt.Errorf("H3 billing plan input image count is invalid")
	}
	seen := make(map[string]struct{}, len(plan.Components))
	allowed := map[string]struct{}{"output_video": {}, "input_video": {}, "input_image": {}, "input_audio": {}}
	for _, component := range plan.Components {
		if _, ok := allowed[component.Key]; !ok {
			return fmt.Errorf("H3 billing plan has unsupported component %s", component.Key)
		}
		if _, ok := seen[component.Key]; ok {
			return fmt.Errorf("H3 billing plan has duplicate component %s", component.Key)
		}
		seen[component.Key] = struct{}{}
		if _, err := nonNegativePrice(component.UnitPrice); err != nil || component.RequestedQuantity < 0 || component.ReservedQuantity < 0 || component.FreeQuantity < 0 {
			return fmt.Errorf("H3 billing plan component %s is invalid", component.Key)
		}
		if component.Key == "output_video" && (component.RequestedQuantity != plan.RequestedOutputDurationSeconds || component.ReservedQuantity < component.RequestedQuantity) {
			return fmt.Errorf("H3 output component does not match the request snapshot")
		}
		if component.Key == "input_image" && (component.RequestedQuantity != plan.InputImageCount || component.ReservedQuantity < component.RequestedQuantity) {
			return fmt.Errorf("H3 image component does not match the request snapshot")
		}
		if component.Key == "input_video" && (component.ReservedQuantity < component.RequestedQuantity || component.ReservedQuantity > H3MaxInputMediaDurationSeconds) {
			return fmt.Errorf("H3 input video component reservation is invalid")
		}
	}
	for _, key := range []string{"output_video", "input_video", "input_image", "input_audio"} {
		if _, ok := seen[key]; !ok {
			return fmt.Errorf("H3 billing plan is missing component %s", key)
		}
	}
	return nil
}

func usageMilliseconds(value *int64, field string, required bool, minimum, maximum int64) (int64, error) {
	if value == nil {
		if required {
			return 0, fmt.Errorf("H3 %s is missing", field)
		}
		return 0, nil
	}
	if *value < 0 {
		return 0, fmt.Errorf("H3 %s must be a non-negative millisecond value", field)
	}
	if *value < minimum*1000 || *value > maximum*1000 {
		return 0, fmt.Errorf("H3 %s is outside the allowed range", field)
	}
	return *value, nil
}

func planComponentPrice(plan *types.TaskBillingPlan, key string) (decimal.Decimal, error) {
	for _, component := range plan.Components {
		if component.Key == key {
			return nonNegativePrice(component.UnitPrice)
		}
	}
	return decimal.Zero, fmt.Errorf("H3 billing plan is missing component %s", key)
}

func planComponentFreeQuantity(plan *types.TaskBillingPlan, key string) int64 {
	for _, component := range plan.Components {
		if component.Key == key {
			return component.FreeQuantity
		}
	}
	return 0
}

func hashH3BillingConfig(config H3BillingConfig) string {
	data, err := common.Marshal(config)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", hash)
}

func cloneH3BillingConfig(config H3BillingConfig) H3BillingConfig {
	config.OutputUnitPrice = cloneStringMap(config.OutputUnitPrice)
	config.InputVideoUnitPrice = cloneStringMap(config.InputVideoUnitPrice)
	return config
}
