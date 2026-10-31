// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Contract/transaction reference: New API 4193734a, model/model_pricing_config.go
// (AGPL-3.0). MAX adds host guards, strict task contracts and old-writer protection.
package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrModelPricingConflict = errors.New("model pricing context changed; reload before saving")
var ErrModelPricingLegacyWrite = errors.New("versioned task prices require the model_pricing PATCH API; reload with a compatible editor")
var modelPricingMutationMu sync.Mutex

const modelPricingLockOption = "__model_pricing_lock_v1" // coordination only, never a public price/version
var modelPricingOptionKeys = []string{"AudioCompletionRatio", "AudioRatio", "CacheRatio", "CompletionRatio", "CreateCacheRatio", "ImageRatio", "ModelPrice", "ModelRatio", "billing_setting.billing_expr", "billing_setting.billing_mode", billing_setting.PluginBillingExprOption}

type pricingMaps map[string]map[string]any

type ModelPricingEntry struct {
	ModelName          string                   `json:"model_name"`
	Version            string                   `json:"version"`
	DraftVersion       string                   `json:"draft_version,omitempty"`
	ConfigurationError string                   `json:"configuration_error,omitempty"`
	Configured         types.ModelPricingValues `json:"configured"`
	Effective          types.ModelPricingValues `json:"effective"`
	ContractDigest     string                   `json:"contract_digest"`
	BindingVersion     string                   `json:"binding_version"`
	Contexts           []ModelPricingContext    `json:"contexts"`
	PluginVariants     []ModelPricingContext    `json:"plugin_variants"`
}
type ModelPricingSnapshot struct {
	Entries      []ModelPricingEntry `json:"entries"`
	Options      map[string]string   `json:"options"`
	EmptyVersion string              `json:"empty_version"`
	Authority    string              `json:"authority"`
}
type ModelPricingCommit struct {
	Committed      bool                `json:"committed"`
	CachePublished bool                `json:"cache_published"`
	Entries        []ModelPricingEntry `json:"entries"`
	Warning        string              `json:"warning,omitempty"`
}

func IsModelPricingOption(key string) bool { return slices.Contains(modelPricingOptionKeys, key) }
func pricingDigest(value any) string {
	encoded, err := common.Marshal(value)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}
func defaultPricingMaps() pricingMaps {
	result := pricingMaps{}
	for _, key := range modelPricingOptionKeys {
		result[key] = map[string]any{}
	}
	for key, values := range ratio_setting.GetDefaultPricingMaps() {
		for model, value := range values {
			result[key][model] = value
		}
	}
	return result
}
func readModelPricingMaps(tx *gorm.DB) (pricingMaps, error) {
	var rows []Option
	if err := tx.Where(map[string]any{"key": modelPricingOptionKeys}).Find(&rows).Error; err != nil {
		return nil, err
	}
	values := defaultPricingMaps()
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.Key] {
			return nil, errors.New("duplicate model pricing option rows")
		}
		seen[row.Key] = true
		if len(row.Value) > 2*1024*1024 {
			return nil, errors.New("model pricing option exceeds size limit")
		}
		var entries map[string]any
		if err := common.UnmarshalJsonStr(row.Value, &entries); err != nil || entries == nil {
			return nil, fmt.Errorf("invalid model pricing object: %s", row.Key)
		}
		if strings.HasPrefix(row.Key, "billing_setting.") {
			for _, value := range entries {
				if _, ok := value.(string); !ok {
					return nil, fmt.Errorf("invalid stored string pricing map: %s", row.Key)
				}
			}
		} else if err := validateOptionUpdate(row.Key, row.Value); err != nil {
			return nil, fmt.Errorf("invalid stored numeric pricing map: %s", row.Key)
		}
		values[row.Key] = entries
	}
	return values, nil
}
func modelPricingValues(values pricingMaps, name string) types.ModelPricingValues {
	result := types.ModelPricingValues{}
	for _, key := range modelPricingOptionKeys {
		if key == billing_setting.PluginBillingExprOption {
			variants := map[string]any{}
			for composite, expression := range values[key] {
				plugin, model, ok := splitPluginPriceKey(composite)
				if ok && model == name {
					variants[plugin] = expression
				}
			}
			if len(variants) > 0 {
				result[key] = variants
			}
			continue
		}
		if value, exists := values[key][name]; exists {
			result[key] = value
		}
	}
	return result
}
func splitPluginPriceKey(key string) (string, string, bool) {
	p, m, ok := strings.Cut(key, "::")
	// Plugin keys cannot contain the separator. Model names can, so only the
	// first separator is structural and the complete suffix must round-trip.
	return p, m, ok && p != "" && m != "" && strings.TrimSpace(p) == p && strings.TrimSpace(m) == m
}
func replaceModelPricing(values pricingMaps, name string, draft types.ModelPricingValues) {
	for _, key := range modelPricingOptionKeys {
		if key == billing_setting.PluginBillingExprOption {
			for composite := range values[key] {
				_, model, ok := splitPluginPriceKey(composite)
				if ok && model == name {
					delete(values[key], composite)
				}
			}
			variants, _ := draft[key].(map[string]any)
			for plugin, expression := range variants {
				values[key][plugin+"::"+name] = expression
			}
			continue
		}
		delete(values[key], name)
		if value, exists := draft[key]; exists {
			values[key][name] = value
		}
	}
}
func validPricingName(name string) bool {
	return name != "" && name == strings.TrimSpace(name) && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 256
}

func validateModelPricing(name string, draft, previous types.ModelPricingValues, contexts []ModelPricingContext) error {
	if !validPricingName(name) || draft == nil {
		return errors.New("valid model name and pricing object required")
	}
	for key, value := range draft {
		if !IsModelPricingOption(key) {
			return fmt.Errorf("unsupported pricing field %s", key)
		}
		switch key {
		case billing_setting.PluginBillingExprOption:
			variants, ok := value.(map[string]any)
			if !ok || variants == nil {
				return errors.New("plugin prices must be an object")
			}
			prior, _ := previous[key].(map[string]any)
			for plugin, raw := range variants {
				expression, ok := raw.(string)
				if !ok || strings.TrimSpace(expression) == "" || !validPricingName(plugin) || strings.Contains(plugin, "::") {
					return errors.New("valid plugin key and nonempty expression required")
				}
				context, found := findPricingContext(contexts, "plugin:"+plugin)
				if !found {
					if prior[plugin] == expression {
						continue
					}
					return fmt.Errorf("plugin %s has no declared host schema", plugin)
				}
				if err := validatePricingSchema(expression, context.Schema); err != nil {
					return err
				}
			}
		case "billing_setting.billing_mode":
			if value != "ratio" && value != "tiered_expr" {
				return errors.New("invalid billing mode")
			}
		case "billing_setting.billing_expr":
			expression, ok := value.(string)
			if !ok || strings.TrimSpace(expression) == "" {
				return errors.New("nonempty billing expression required")
			}
			if previous[key] == expression && draft["billing_setting.billing_mode"] != "tiered_expr" {
				continue
			}
			keys, taskErr := billingexpr.TaskUsageKeys(expression)
			if taskErr == nil && len(keys) > 0 {
				if err := validateDefaultTaskPricing(expression, draft, contexts, keys); err != nil {
					return err
				}
			} else if err := billing_setting.SmokeTestExpr(expression); err != nil {
				return err
			}
		default:
			number, ok := value.(float64)
			if !ok || number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
				return fmt.Errorf("%s must be finite and nonnegative", key)
			}
		}
	}
	if draft["billing_setting.billing_mode"] == "tiered_expr" {
		if _, exists := draft["billing_setting.billing_expr"]; !exists {
			return errors.New("billing expression required for tiered_expr")
		}
	}
	return nil
}

// One metadata row in the existing options table serializes pricing writers,
// including first writes. Options.key retains its migrated primary key.
// Unchanged/missing price maps keep their existing compiled-default semantics.
func lockModelPricingRows(tx *gorm.DB) error {
	row := Option{Key: modelPricingLockOption, Value: "1"}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return err
	}
	var locked []Option
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(map[string]any{"key": modelPricingLockOption}).Find(&locked).Error; err != nil {
		return err
	}
	if len(locked) != 1 {
		return errors.New("options key uniqueness missing; migrate before editing prices")
	}
	return nil
}

func mutateModelPricing(db *gorm.DB, mutate func(pricingMaps, *pricingAuthority) ([]string, error), extras map[string]string) (*ModelPricingCommit, error) {
	return mutateModelPricingWithPublisher(db, mutate, extras, publishModelPricingOptions)
}

func publishModelPricingOptions(values map[string]string) error {
	keys := slices.Sorted(maps.Keys(values))
	for _, key := range keys {
		if err := updateOptionMap(key, values[key]); err != nil {
			return err
		}
	}
	return nil
}

func mutateModelPricingWithPublisher(db *gorm.DB, mutate func(pricingMaps, *pricingAuthority) ([]string, error), extras map[string]string, publish func(map[string]string) error) (*ModelPricingCommit, error) {
	modelPricingMutationMu.Lock()
	defer modelPricingMutationMu.Unlock()
	var committed pricingMaps
	var result ModelPricingCommit
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockModelPricingRows(tx); err != nil {
			return err
		}
		values, err := readModelPricingMaps(tx)
		if err != nil {
			return err
		}
		prior := clonePricingMaps(values)
		authority, err := readPricingAuthority(tx, true)
		if err != nil {
			return err
		}
		names, err := mutate(values, authority)
		if err != nil {
			return err
		}
		for _, key := range modelPricingOptionKeys {
			encoded, err := common.Marshal(values[key])
			if err != nil {
				return err
			}
			if len(encoded) > 2*1024*1024 {
				return errors.New("pricing option exceeds authority read size limit")
			}
			if err := validateOptionUpdate(key, string(encoded)); err != nil {
				return err
			}
			if pricingDigest(prior[key]) == pricingDigest(values[key]) {
				continue
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&Option{Key: key, Value: string(encoded)}).Error; err != nil {
				return err
			}
		}
		for key, value := range extras {
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&Option{Key: key, Value: value}).Error; err != nil {
				return err
			}
		}
		for _, name := range names {
			result.Entries = append(result.Entries, buildModelPricingEntry(values, authority, name))
		}
		committed = values
		return nil
	})
	if err != nil {
		return nil, err
	}
	result.Committed = true
	result.CachePublished = true
	publication := maps.Clone(extras)
	if publication == nil {
		publication = map[string]string{}
	}
	for _, key := range modelPricingOptionKeys {
		encoded, _ := common.Marshal(committed[key])
		publication[key] = string(encoded)
	}
	if err := publish(publication); err != nil {
		result.CachePublished = false
		result.Warning = "configuration committed; local cache publication failed; reload authoritative pricing"
	}
	InvalidatePricingCache()
	ratio_setting.InvalidateExposedDataCache()
	return &result, nil
}

func UpdateModelPricing(changes []types.ModelPricingChange) (*ModelPricingCommit, error) {
	return updateModelPricing(DB, changes)
}
func updateModelPricing(db *gorm.DB, changes []types.ModelPricingChange) (*ModelPricingCommit, error) {
	if len(changes) == 0 || len(changes) > 200 {
		return nil, errors.New("expected 1..200 pricing changes")
	}
	seen := map[string]bool{}
	for _, change := range changes {
		if seen[change.ModelName] || !validPricingName(change.ModelName) {
			return nil, errors.New("invalid or duplicate model pricing change")
		}
		seen[change.ModelName] = true
	}
	return mutateModelPricing(db, func(values pricingMaps, authority *pricingAuthority) ([]string, error) {
		names := []string{}
		// Validate every guard against the ORIGINAL snapshot, then apply drafts.
		for _, change := range changes {
			entry := buildModelPricingEntry(values, authority, change.ModelName)
			if change.ExpectedVersion == "" || change.ExpectedVersion != entry.Version || change.ExpectedContractDigest != entry.ContractDigest || change.ExpectedBindingVersion != entry.BindingVersion {
				return nil, ErrModelPricingConflict
			}
		}
		for _, change := range changes {
			previous := modelPricingValues(values, change.ModelName)
			draft := change.Pricing
			if change.Reset != nil && *change.Reset {
				draft = modelPricingValues(defaultPricingMaps(), change.ModelName)
			}
			if err := validateModelPricing(change.ModelName, draft, previous, authority.contexts(change.ModelName)); err != nil {
				return nil, err
			}
			replaceModelPricing(values, change.ModelName, draft)
			names = append(names, change.ModelName)
		}
		return names, nil
	}, nil)
}

// Legacy callers retain their full-map interface only for unprotected models.
// They participate in the SAME DB locks; cached drafts cannot overwrite task prices.
func updateModelPricingOptions(updates map[string]string) (*ModelPricingCommit, error) {
	return mutateModelPricing(DB, func(values pricingMaps, authority *pricingAuthority) ([]string, error) {
		previous := pricingMaps{}
		for key, entries := range values {
			previous[key] = entries
		}
		names := map[string]bool{}
		for key, raw := range updates {
			if !IsModelPricingOption(key) {
				continue
			}
			var entries map[string]any
			if err := common.UnmarshalJsonStr(raw, &entries); err != nil || entries == nil {
				return nil, errors.New("pricing option must be a JSON object")
			}
			for _, source := range []map[string]any{values[key], entries} {
				for name := range source {
					if key == billing_setting.PluginBillingExprOption {
						_, model, ok := splitPluginPriceKey(name)
						if !ok {
							return nil, errors.New("invalid plugin price key")
						}
						name = model
					}
					names[name] = true
				}
			}
			values[key] = entries
		}
		ordered := []string{}
		for name := range names {
			ordered = append(ordered, name)
		}
		sort.Strings(ordered)
		changed := []string{}
		for _, name := range ordered {
			prior, draft := modelPricingValues(previous, name), modelPricingValues(values, name)
			if pricingDigest(prior) == pricingDigest(draft) {
				continue
			}
			if protectedTaskPricing(prior, authority.contexts(name)) || protectedTaskPricing(draft, nil) {
				return nil, ErrModelPricingLegacyWrite
			}
			if err := validateLegacyModelPricing(name, draft, prior, authority.contexts(name)); err != nil {
				return nil, err
			}
			changed = append(changed, name)
		}
		return changed, nil
	}, filterNonPricingOptions(updates))
}
func filterNonPricingOptions(values map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range values {
		if !IsModelPricingOption(key) {
			out[key] = value
		}
	}
	return out
}
func protectedTaskPricing(values types.ModelPricingValues, contexts []ModelPricingContext) bool {
	if variants, ok := values[billing_setting.PluginBillingExprOption].(map[string]any); ok && len(variants) > 0 {
		return true
	}
	if expression, ok := values["billing_setting.billing_expr"].(string); ok {
		if keys, err := billingexpr.TaskUsageKeys(expression); err == nil && len(keys) > 0 {
			return true
		}
	}
	for _, context := range contexts {
		if context.Profile == taskpricing.NewAPITask {
			return true
		}
	}
	return false
}

func modelPricingPublicationError(result *ModelPricingCommit, err error) error {
	if err != nil {
		return err
	}
	if result != nil && !result.CachePublished {
		return errors.New(result.Warning)
	}
	return nil
}

func validateLegacyModelPricing(name string, draft, previous types.ModelPricingValues, contexts []ModelPricingContext) error {
	// Old editors retained dormant strings that are not valid expressions. Keep
	// that compatibility without allowing an active or new task price to bypass validation.
	if raw, exists := draft["billing_setting.billing_expr"]; exists && draft["billing_setting.billing_mode"] != "tiered_expr" {
		if _, ok := raw.(string); !ok {
			return errors.New("billing expression must be a string")
		}
		copy := maps.Clone(draft)
		delete(copy, "billing_setting.billing_expr")
		if err := validateModelPricing(name, copy, previous, contexts); err != nil {
			return fmt.Errorf("model %s: %w", name, err)
		}
		return nil
	}
	if err := validateModelPricing(name, draft, previous, contexts); err != nil {
		return fmt.Errorf("model %s: %w", name, err)
	}
	return nil
}

// Saving a schema-checked draft is weaker than proving quote phases. Defaults
// use a conservative intersection of declared source schemas; an explicit
// plugin override removes that source from default inheritance, not compilation.
func validateDefaultTaskPricing(expression string, draft types.ModelPricingValues, contexts []ModelPricingContext, keys []string) error {
	variants, _ := draft[billing_setting.PluginBillingExprOption].(map[string]any)
	declared := false
	signatures := map[string]string{}
	for _, context := range contexts {
		// A model default is also published to the existing token billing maps.
		// Do not replace a legacy consumer's expression with a task-only u().
		// Source-specific plugin overrides do not change that shared default.
		if context.legacyDefaultConsumer {
			return fmt.Errorf("task default conflicts with legacy channel %d; retain the legacy default and use a plugin override or a separate task model alias", context.ChannelID)
		}
		if context.Schema == nil {
			continue
		}
		declared = true
		if _, overridden := variants[context.PluginKey]; context.PluginKey != "" && overridden {
			continue
		}
		if err := validatePricingSchema(expression, context.Schema); err != nil {
			return err
		}
		if context.Contract != nil {
			if err := taskpricing.ValidateExpressionContract(expression, *context.Contract); err != nil {
				return err
			}
		}
		for _, key := range keys {
			field := context.Schema[key]
			kind := field.Type
			if kind == "" && len(field.Enum) > 0 {
				kind = "enum"
			}
			enum := slices.Clone(field.Enum)
			sort.Strings(enum)
			signature := pricingDigest(types.TaskUsageField{Type: kind, Unit: field.Unit, Enum: enum})
			if previous, exists := signatures[key]; exists && previous != signature {
				return fmt.Errorf("incompatible default fact %q across source schemas; use an explicit plugin override", key)
			}
			signatures[key] = signature
		}
	}
	if !declared {
		return errors.New("task expression has no declared host schema")
	}
	return nil
}
