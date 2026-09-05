package task_billing_setting

import (
	"fmt"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/config"
	"github.com/MAX-API-Next/MAX-API/types"
)

type RateCardRow struct {
	ID        string            `json:"id,omitempty"`
	Match     map[string]string `json:"match"`
	UnitPrice float64           `json:"unit_price"`
}

type RateCard struct {
	Vendor          string            `json:"vendor,omitempty"`
	Unit            string            `json:"unit,omitempty"`
	QuantityField   string            `json:"quantity_field,omitempty"`
	DefaultQuantity float64           `json:"default_quantity,omitempty"`
	Strict          bool              `json:"strict"`
	Defaults        map[string]string `json:"defaults,omitempty"`
	Rows            []RateCardRow     `json:"rows"`
}

type TaskBillingSetting struct {
	RateCards  *types.RWMap[string, RateCard]        `json:"rate_cards"`
	H3Profiles *types.RWMap[string, H3BillingConfig] `json:"h3_profiles"`
}

var taskBillingSetting = TaskBillingSetting{
	RateCards:  newRateCardMap(defaultRateCards()),
	H3Profiles: newH3BillingProfileMap(defaultH3BillingProfiles()),
}

func init() {
	config.GlobalConfig.Register("task_billing_setting", &taskBillingSetting)
}

func GetRateCardsCopy() map[string]RateCard {
	if taskBillingSetting.RateCards == nil {
		return map[string]RateCard{}
	}
	return cloneRateCards(taskBillingSetting.RateCards.ReadAll())
}

func GetRateCardCopy(models ...string) (*RateCard, string) {
	card, key := findRateCard(models...)
	if card == nil {
		return nil, ""
	}
	cloned := cloneRateCard(*card)
	return &cloned, key
}

func HasRateCard(models ...string) bool {
	card, _ := findRateCard(models...)
	return card != nil
}

func Calculate(input types.TaskBillingInput, groupRatio float64) (*types.TaskBillingResult, error) {
	card, key := findRateCard(input.Model, input.UpstreamModel)
	if card == nil {
		return nil, nil
	}
	fields := mergeFields(*card, input)
	quantity, err := resolveQuantity(*card, input, fields)
	if err != nil {
		return nil, fmt.Errorf("task billing %s: %w", key, err)
	}
	row, err := matchRow(*card, fields)
	if err != nil {
		return nil, fmt.Errorf("task billing %s: %w", key, err)
	}
	if row == nil {
		return nil, nil
	}
	totalPrice := row.UnitPrice * quantity
	quota := common.QuotaFromFloat(totalPrice * common.QuotaPerUnit * groupRatio)
	if totalPrice > 0 && quota <= 0 {
		quota = 1
	}
	matched := cloneStringMap(row.Match)
	return &types.TaskBillingResult{
		RuleKey:        key,
		RowID:          row.ID,
		Unit:           card.Unit,
		Quantity:       quantity,
		UnitPrice:      row.UnitPrice,
		TotalPrice:     totalPrice,
		Quota:          quota,
		Fields:         fields,
		Matched:        matched,
		PerCallBilling: true,
		Trace: []types.TaskBillingTraceItem{
			{Name: "quantity", Value: formatFloat(quantity), Unit: card.Unit},
			{Name: "unit_price", Value: formatFloat(row.UnitPrice), Unit: card.Unit, Price: row.UnitPrice},
			{Name: "total_price", Value: formatFloat(totalPrice), Price: totalPrice},
		},
	}, nil
}

func ValidateRateCardsJSON(raw string) error {
	var rateCards map[string]RateCard
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := common.UnmarshalJsonStr(raw, &rateCards); err != nil {
		return err
	}
	return validateRateCards(rateCards)
}

func validateRateCards(rateCards map[string]RateCard) error {
	for key, card := range rateCards {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("rate card key cannot be empty")
		}
		if len(card.Rows) == 0 {
			return fmt.Errorf("rate card %s has no rows", key)
		}
		for i, row := range card.Rows {
			if row.UnitPrice < 0 || math.IsNaN(row.UnitPrice) || math.IsInf(row.UnitPrice, 0) {
				return fmt.Errorf("rate card %s row %d has invalid unit_price", key, i)
			}
			if len(row.Match) == 0 {
				return fmt.Errorf("rate card %s row %d has empty match", key, i)
			}
		}
	}
	return nil
}

func validQuantity(value float64) bool {
	return value > 0 && value <= math.MaxInt32 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func findRateCard(models ...string) (*RateCard, string) {
	if taskBillingSetting.RateCards == nil {
		return nil, ""
	}
	rateCards := taskBillingSetting.RateCards.ReadAll()
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if card, ok := rateCards[model]; ok {
			return &card, model
		}
	}
	keys := make([]string, 0, len(rateCards))
	for key := range rateCards {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		for _, key := range keys {
			if !strings.Contains(key, "*") {
				continue
			}
			if matchPattern(key, model) {
				card := rateCards[key]
				return &card, key
			}
		}
	}
	return nil, ""
}

func newRateCardMap(rateCards map[string]RateCard) *types.RWMap[string, RateCard] {
	m := types.NewRWMap[string, RateCard]()
	m.AddAll(rateCards)
	return m
}

func matchPattern(pattern, value string) bool {
	matched, err := path.Match(pattern, value)
	if err == nil {
		return matched
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == value
	}
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(value[pos:], part)
		if idx < 0 {
			return false
		}
		if i == 0 && idx != 0 {
			return false
		}
		pos += idx + len(part)
	}
	last := parts[len(parts)-1]
	return last == "" || strings.HasSuffix(value, last)
}

func mergeFields(card RateCard, input types.TaskBillingInput) map[string]string {
	fields := cloneStringMap(card.Defaults)
	for key, value := range input.Fields {
		if strings.TrimSpace(value) == "" {
			continue
		}
		fields[key] = normalizeValue(value)
	}
	if input.Action != "" {
		fields["action"] = normalizeValue(input.Action)
	}
	if input.Platform != "" {
		fields["platform"] = normalizeValue(input.Platform)
	}
	return fields
}

func resolveQuantity(card RateCard, input types.TaskBillingInput, fields map[string]string) (float64, error) {
	if card.QuantityField == "" {
		if validQuantity(card.DefaultQuantity) {
			return card.DefaultQuantity, nil
		}
		return 1, nil
	}
	if input.Numbers != nil {
		if value, ok := input.Numbers[card.QuantityField]; ok && validQuantity(value) {
			return value, nil
		}
	}
	if raw := fields[card.QuantityField]; raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err == nil && validQuantity(value) {
			return value, nil
		}
	}
	if validQuantity(card.DefaultQuantity) {
		return card.DefaultQuantity, nil
	}
	return 0, fmt.Errorf("missing positive quantity field %q", card.QuantityField)
}

func matchRow(card RateCard, fields map[string]string) (*RateCardRow, error) {
	for i := range card.Rows {
		row := &card.Rows[i]
		if rowMatches(row.Match, fields) {
			return row, nil
		}
	}
	if !card.Strict {
		return nil, nil
	}
	return nil, fmt.Errorf("no configured price row for %s", compactFields(fields))
}

func rowMatches(match map[string]string, fields map[string]string) bool {
	for key, expected := range match {
		actual, ok := fields[key]
		if !ok {
			return false
		}
		if normalizeValue(actual) != normalizeValue(expected) {
			return false
		}
	}
	return true
}

func normalizeValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func formatFloat(value float64) string {
	if math.Abs(value-math.Round(value)) < 0.0000001 {
		return strconv.FormatInt(int64(math.Round(value)), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func compactFields(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+fields[key])
	}
	return strings.Join(parts, ", ")
}

func cloneRateCards(src map[string]RateCard) map[string]RateCard {
	dst := make(map[string]RateCard, len(src))
	for key, card := range src {
		dst[key] = cloneRateCard(card)
	}
	return dst
}

func cloneRateCard(card RateCard) RateCard {
	card.Defaults = cloneStringMap(card.Defaults)
	rows := make([]RateCardRow, len(card.Rows))
	for i, row := range card.Rows {
		row.Match = cloneStringMap(row.Match)
		rows[i] = row
	}
	card.Rows = rows
	return card
}

func cloneStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
