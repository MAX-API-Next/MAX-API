// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"encoding/json"
	"errors"
	"maps"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Double-read equality under READ COMMITTED detects a mixed configuration and
// source snapshot. It does not claim a global cutover or order hashes by time.
func readPricingState(db *gorm.DB, read func(*gorm.DB, pricingMaps, *pricingAuthority) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		values, err := readModelPricingMaps(tx)
		if err != nil {
			return err
		}
		authority, err := readPricingAuthority(tx, false)
		if err != nil {
			return err
		}
		if err := read(tx, values, authority); err != nil {
			return err
		}
		after, err := readModelPricingMaps(tx)
		if err != nil {
			return err
		}
		afterAuthority, err := readPricingAuthority(tx, false)
		if err != nil {
			return err
		}
		if pricingDigest(values) != pricingDigest(after) || authority.signature() != afterAuthority.signature() {
			return ErrModelPricingConflict
		}
		return nil
	})
}
func GetModelPricingSnapshot(names []string) (*ModelPricingSnapshot, error) {
	return getModelPricingSnapshot(DB, names)
}
func getModelPricingSnapshot(db *gorm.DB, names []string) (*ModelPricingSnapshot, error) {
	if len(names) > 2000 {
		return nil, errors.New("too many pricing models requested")
	}
	result := &ModelPricingSnapshot{Entries: []ModelPricingEntry{}, Options: map[string]string{}, EmptyVersion: pricingDigest(map[string]any{}), Authority: "database; equality tokens only; configuration samples are not paid admission"}
	err := readPricingState(db, func(_ *gorm.DB, values pricingMaps, a *pricingAuthority) error {
		set := map[string]bool{}
		if len(names) == 0 {
			for key, entries := range values {
				for name := range entries {
					if key == billing_setting.PluginBillingExprOption {
						_, model, ok := splitPluginPriceKey(name)
						if !ok {
							return errors.New("invalid stored plugin pricing key")
						}
						name = model
					}
					set[name] = true
				}
			}
			for _, channel := range a.channels {
				for _, name := range strings.Split(channel.Models, ",") {
					if name != "" {
						set[name] = true
					}
				}
			}
		} else {
			for _, name := range names {
				if !validPricingName(name) {
					return errors.New("invalid model name")
				}
				set[name] = true
			}
		}
		ordered := []string{}
		for name := range set {
			ordered = append(ordered, name)
		}
		sort.Strings(ordered)
		for _, name := range ordered {
			result.Entries = append(result.Entries, buildModelPricingEntry(values, a, name))
		}
		for key, entries := range values {
			encoded, err := common.Marshal(entries)
			if err != nil {
				return err
			}
			result.Options[key] = string(encoded)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
func PreviewModelPricing(name string, draft types.ModelPricingValues) (*ModelPricingEntry, error) {
	var result ModelPricingEntry
	err := readPricingState(DB, func(_ *gorm.DB, values pricingMaps, a *pricingAuthority) error {
		if err := validateModelPricing(name, draft, modelPricingValues(values, name), a.contexts(name)); err != nil {
			return err
		}
		candidate := clonePricingMaps(values)
		replaceModelPricing(candidate, name, draft)
		result = buildModelPricingEntry(candidate, a, name)
		result.DraftVersion = result.Version
		result.Version = buildModelPricingEntry(values, a, name).Version
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
func clonePricingMaps(values pricingMaps) pricingMaps {
	out := pricingMaps{}
	for key, entries := range values {
		out[key] = map[string]any{}
		for name, value := range entries {
			out[key][name] = value
		}
	}
	return out
}

type ModelPricingQuoteRequest struct {
	ModelName              string                   `json:"model_name"`
	ContextID              string                   `json:"context_id"`
	ExpectedVersion        string                   `json:"expected_version"`
	ExpectedContractDigest string                   `json:"expected_contract_digest"`
	ExpectedBindingVersion string                   `json:"expected_binding_version"`
	Pricing                types.ModelPricingValues `json:"pricing,omitempty"`
	Submit                 types.TaskPricingFacts   `json:"submit"`
	Actual                 types.TaskPricingFacts   `json:"actual,omitempty"`
	Group                  string                   `json:"group"`
	PricingAt              *time.Time               `json:"pricing_at,omitempty"`
	RequestBody            json.RawMessage          `json:"request_body,omitempty"`
	RequestHeaders         map[string]string        `json:"request_headers,omitempty"`
}
type ModelPricingQuote struct {
	Scope            string                    `json:"scope"`
	AdmissionReady   bool                      `json:"admission_ready"`
	PriceVersion     string                    `json:"price_version"`
	ContractDigest   string                    `json:"contract_digest"`
	BindingVersion   string                    `json:"binding_version"`
	TermsVersion     string                    `json:"terms_version"`
	Group            string                    `json:"group"`
	GroupRatio       float64                   `json:"group_ratio"`
	QuotaPerUnit     float64                   `json:"quota_per_unit"`
	ReservationFloor int                       `json:"reservation_floor"`
	Currency         string                    `json:"currency"`
	Preview          taskpricing.PreviewResult `json:"preview"`
}

var quoteTermKeys = []string{"GroupRatio", "group_ratio_setting.group_ratio", "PreConsumedQuota"}

func readQuoteTerms(tx *gorm.DB, group string) (float64, int, string, error) {
	var rows []Option
	if err := tx.Where(map[string]any{"key": quoteTermKeys}).Order(clause.OrderByColumn{Column: clause.Column{Name: "key"}}).Find(&rows).Error; err != nil {
		return 0, 0, "", err
	}
	ratios := ratio_setting.GetDefaultGroupRatioCopy()
	floor := 500
	seenRatio := ""
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.Key] {
			return 0, 0, "", errors.New("duplicate quote term rows")
		}
		seen[row.Key] = true
		if row.Key == "PreConsumedQuota" {
			value, err := strconv.Atoi(row.Value)
			if err != nil || value < 0 || value > common.MaxQuota {
				return 0, 0, "", errors.New("invalid reservation floor")
			}
			floor = value
			continue
		}
		var candidate map[string]float64
		if err := common.UnmarshalJsonStr(row.Value, &candidate); err != nil || candidate == nil {
			return 0, 0, "", errors.New("invalid group pricing")
		}
		digest := pricingDigest(candidate)
		if seenRatio != "" && digest != seenRatio {
			return 0, 0, "", errors.New("group pricing aliases disagree")
		}
		seenRatio = digest
		ratios = candidate
	}
	ratio, exists := ratios[group]
	if !exists || group == "auto" || strings.HasPrefix(group, "auto:") || ratio < 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return 0, 0, "", errors.New("unknown or invalid configured quote group")
	}
	return ratio, floor, pricingDigest(struct {
		Rows  []Option
		QPU   float64
		Group string
		Ratio float64
		Floor int
	}{rows, common.QuotaPerUnit, group, ratio, floor}), nil
}
func QuoteModelPricing(request ModelPricingQuoteRequest) (*ModelPricingQuote, error) {
	if !validPricingName(request.ModelName) || request.ContextID == "" || request.Group == "" {
		return nil, errors.New("model, host context and configured group required")
	}
	var result *ModelPricingQuote
	err := readPricingState(DB, func(tx *gorm.DB, values pricingMaps, a *pricingAuthority) error {
		entry := buildModelPricingEntry(values, a, request.ModelName)
		if request.ExpectedVersion != entry.Version || request.ExpectedContractDigest != entry.ContractDigest || request.ExpectedBindingVersion != entry.BindingVersion {
			return ErrModelPricingConflict
		}
		context, exists := findPricingContext(entry.Contexts, request.ContextID)
		if !exists || !context.Available || context.Contract == nil {
			return errors.New("host source has no available phase contract for quoting")
		}
		if context.ChannelID > 0 && !slices.Contains(context.Groups, request.Group) {
			return errors.New("configured channel does not serve this quote group")
		}
		candidate := clonePricingMaps(values)
		if request.Pricing != nil {
			if err := validateModelPricing(request.ModelName, request.Pricing, entry.Configured, entry.Contexts); err != nil {
				return err
			}
			replaceModelPricing(candidate, request.ModelName, request.Pricing)
		}
		if err := validateModelPricing(request.ModelName, modelPricingValues(candidate, request.ModelName), entry.Configured, entry.Contexts); err != nil {
			return err
		}
		selected, err := taskpricing.SelectExpression(taskPriceMaps(candidate), context.source, request.ModelName, context.MappedModel)
		if err != nil {
			return err
		}
		if selected.Model != request.ModelName {
			draft := modelPricingValues(candidate, selected.Model)
			if err := validateModelPricing(selected.Model, draft, draft, a.contexts(selected.Model)); err != nil {
				return err
			}
		}
		ratio, floor, terms, err := readQuoteTerms(tx, request.Group)
		if err != nil {
			return err
		}
		at := time.Now().UTC()
		if request.PricingAt != nil {
			at = *request.PricingAt
		}
		// The outer guard covers all model contexts; the core checks the ONE
		// selected contract. These equality tokens deliberately prove different objects.
		selectedDigest, err := taskpricing.ContractDigest(*context.Contract)
		if err != nil {
			return err
		}
		guards := taskpricing.Guards{PriceVersion: entry.Version, ContractDigest: selectedDigest, BindingVersion: entry.BindingVersion}
		preview, err := taskpricing.Preview(taskpricing.PreviewInput{Profile: taskpricing.NewAPITask, Prices: taskPriceMaps(candidate), Source: context.source, OriginalModel: request.ModelName, MappedModel: context.MappedModel, Contract: *context.Contract, Expected: guards, Current: guards, Submit: request.Submit, Actual: request.Actual, Request: billingexpr.RequestInput{Body: request.RequestBody, Headers: request.RequestHeaders}, PricingAt: at, QuotaPerUnit: common.QuotaPerUnit, GroupRatio: ratio, ReservationFloor: floor})
		if err != nil {
			return err
		}
		_, _, after, err := readQuoteTerms(tx, request.Group)
		if err != nil {
			return err
		}
		if terms != after {
			return ErrModelPricingConflict
		}
		result = &ModelPricingQuote{Scope: "configuration_sample; configured group only; no user-specific multiplier or paid admission", PriceVersion: entry.Version, ContractDigest: entry.ContractDigest, BindingVersion: entry.BindingVersion, TermsVersion: terms, Group: request.Group, GroupRatio: ratio, QuotaPerUnit: common.QuotaPerUnit, ReservationFloor: floor, Currency: "USD", Preview: preview}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Used by the legacy full tiered editor: transform inside the authoritative
// transaction rather than starting from a possibly stale node OptionMap.
type LegacyTieredPricingItem struct {
	Enabled bool
	Expr    string
}

func UpdateLegacyTieredPricing(config map[string]LegacyTieredPricingItem) error {
	_, err := updateLegacyTieredPricing(config)
	return err
}

func updateLegacyTieredPricing(config map[string]LegacyTieredPricingItem) (*ModelPricingCommit, error) {
	return mutateModelPricing(DB, func(values pricingMaps, a *pricingAuthority) ([]string, error) {
		prior := clonePricingMaps(values)
		for name, mode := range values["billing_setting.billing_mode"] {
			if mode == "tiered_expr" {
				delete(values["billing_setting.billing_mode"], name)
				delete(values["billing_setting.billing_expr"], name)
			}
		}
		for name, item := range config {
			if !validPricingName(name) {
				return nil, errors.New("invalid model name")
			}
			delete(values["billing_setting.billing_mode"], name)
			delete(values["billing_setting.billing_expr"], name)
			if item.Enabled {
				values["billing_setting.billing_mode"][name] = "tiered_expr"
				values["billing_setting.billing_expr"][name] = item.Expr
			}
		}
		names := map[string]bool{}
		for _, snapshot := range []pricingMaps{prior, values} {
			for _, key := range []string{"billing_setting.billing_mode", "billing_setting.billing_expr"} {
				for name := range snapshot[key] {
					names[name] = true
				}
			}
		}
		ordered := slices.Sorted(maps.Keys(names))
		changed := []string{}
		for _, name := range ordered {
			old, draft := modelPricingValues(prior, name), modelPricingValues(values, name)
			if pricingDigest(old) == pricingDigest(draft) {
				continue
			}
			if protectedTaskPricing(old, a.contexts(name)) || protectedTaskPricing(draft, nil) {
				return nil, ErrModelPricingLegacyWrite
			}
			if err := validateLegacyModelPricing(name, draft, old, a.contexts(name)); err != nil {
				return nil, err
			}
			changed = append(changed, name)
		}
		return changed, nil
	}, nil)
}
