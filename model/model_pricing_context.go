// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/setting/billing_setting"
	"github.com/MAX-API-Next/MAX-API/setting/ratio_setting"
	"github.com/MAX-API-Next/MAX-API/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// These are configuration-sample scopes, NOT evidence of executable drivers or
// paid admission. A future task plan must freeze its actual selected binding.
type ModelPricingContext struct {
	ID                    string                          `json:"id"`
	ChannelID             int                             `json:"channel_id,omitempty"`
	Groups                []string                        `json:"groups,omitempty"`
	PluginKey             string                          `json:"plugin_key,omitempty"`
	MappedModel           string                          `json:"mapped_model"`
	Schema                map[string]types.TaskUsageField `json:"usage_schema,omitempty"`
	Contract              *types.TaskFactContract         `json:"contract,omitempty"`
	Profile               string                          `json:"profile,omitempty"`
	Configured            *string                         `json:"configured,omitempty"`
	Effective             *string                         `json:"effective,omitempty"`
	Compatible            bool                            `json:"compatible"`
	SchemaCompatible      bool                            `json:"schema_compatible"`
	Available             bool                            `json:"available"`
	ExecutionReady        bool                            `json:"execution_ready"`
	Reason                string                          `json:"reason,omitempty"`
	source                taskpricing.Source
	binding               any
	legacyDefaultConsumer bool // Advertises this exact model without a known Go task contract.
}
type pricingChannel struct {
	ID             int
	Type           int
	Status         int
	Models         string
	Group          string
	ModelMapping   *string
	Setting        *string
	BaseURL        *string
	ParamOverride  *string
	HeaderOverride *string
	Settings       string
}
type pricingAuthority struct {
	channels     []pricingChannel
	plugins      []taskplugin.StoredPackage
	catalogReady bool
	selfUse      bool
}

func readPricingAuthority(tx *gorm.DB, lock bool) (*pricingAuthority, error) {
	a := &pricingAuthority{}
	var defaults []Option
	defaultQuery := tx.Where(map[string]any{"key": "SelfUseModeEnabled"})
	if lock {
		defaultQuery = defaultQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := defaultQuery.Find(&defaults).Error; err != nil {
		return nil, err
	}
	if len(defaults) > 1 {
		return nil, errors.New("duplicate self-use pricing setting")
	}
	if len(defaults) == 1 {
		if defaults[0].Value != "true" && defaults[0].Value != "false" {
			return nil, errors.New("invalid self-use pricing setting")
		}
		a.selfUse = defaults[0].Value == "true"
	}
	query := tx.Model(&Channel{}).Select("id", "type", "status", "models", "group", "model_mapping", "setting", "base_url", "param_override", "header_override", "settings").Order("id")
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Find(&a.channels).Error; err != nil {
		return nil, err
	}
	if !tx.Migrator().HasTable(&TaskPluginCatalogHead{}) || !tx.Migrator().HasTable(&TaskPluginVersion{}) {
		return a, nil
	}
	a.catalogReady = true
	var head TaskPluginCatalogHead
	q := tx
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.First(&head, 1).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	rows, _, loadErr := loadTaskPluginVersions(tx)
	if loadErr != nil {
		return nil, loadErr
	}
	if errors.Is(err, gorm.ErrRecordNotFound) && len(rows) > 0 {
		return nil, taskplugin.ErrCatalogInvalid
	}
	if err == nil && (head.Revision <= 0 || head.MutationID == "") {
		return nil, taskplugin.ErrCatalogInvalid
	}
	snapshot := taskplugin.CatalogSnapshot{Revision: head.Revision, Packages: rows}
	if err := taskplugin.ValidateCatalogTransition(taskplugin.CatalogSnapshot{}, snapshot); err != nil {
		return nil, err
	}
	a.plugins = rows
	return a, nil
}
func (a *pricingAuthority) signature() string {
	return pricingDigest(struct {
		Channels []pricingChannel
		Plugins  []taskplugin.StoredPackage
		Ready    bool
		SelfUse  bool
	}{a.channels, a.plugins, a.catalogReady, a.selfUse})
}
func (a *pricingAuthority) contexts(model string) []ModelPricingContext {
	result := []ModelPricingContext{}
	for _, channel := range a.channels {
		advertised := slices.Contains(strings.Split(channel.Models, ","), model)
		mapping := map[string]string{}
		mappingValid := channel.ModelMapping == nil || *channel.ModelMapping == "" || common.UnmarshalJsonStr(*channel.ModelMapping, &mapping) == nil
		referenced := false
		for original, target := range mapping {
			if target == model && slices.Contains(strings.Split(channel.Models, ","), original) {
				referenced = true
			}
		}
		if !advertised && !referenced {
			continue
		}
		context := ModelPricingContext{ID: fmt.Sprintf("channel:%d", channel.ID), ChannelID: channel.ID, MappedModel: model, source: taskpricing.Source{Driver: "go_adapter"}, binding: channel, legacyDefaultConsumer: advertised, Reason: "unsupported host pricing contract"}
		context.Groups = strings.Split(channel.Group, ",")
		if channel.Setting != nil {
			var settings map[string]any
			if common.UnmarshalJsonStr(*channel.Setting, &settings) == nil {
				context.Profile, _ = settings["task_pricing_profile"].(string)
			}
		}
		if !mappingValid {
			context.Reason = "invalid channel model mapping"
			result = append(result, context)
			continue
		}
		if mapped, exists := mapping[model]; exists && advertised {
			context.MappedModel = mapped
		}
		family := ""
		switch channel.Type {
		case constant.ChannelTypeMiniMax:
			family = "hailuo"
		case constant.ChannelTypeKling:
			family = "kling"
		case constant.ChannelTypeDoubaoVideo, constant.ChannelTypeVolcEngine:
			family = "doubao"
		}
		if contract, ok := taskpricing.KnownContract(family, context.MappedModel); ok {
			context.legacyDefaultConsumer = false
			context.Contract = &contract
			context.Schema = contract.UsageSchema
			context.Available = channel.Status == 1 && advertised
			context.Reason = ""
			if !context.Available {
				context.Reason = "channel disabled or mapped-target-only; quote its advertised alias"
			}
		}
		result = append(result, context)
	}
	for _, entry := range a.plugins {
		if !entry.Active {
			continue
		}
		pkg, err := taskplugin.LoadPackage(bytes.NewReader(entry.Document), entry.Digest)
		if err != nil {
			continue
		} // snapshot validation above already rejects corruption
		manifest := pkg.Manifest()
		if !slices.Contains(manifest.Models, model) {
			continue
		}
		context := ModelPricingContext{ID: "plugin:" + manifest.Key, PluginKey: manifest.Key, MappedModel: model, Schema: map[string]types.TaskUsageField{}, binding: struct {
			Digest           string
			Enabled, Revoked bool
		}{entry.Digest, entry.Enabled, entry.Revoked}, source: taskpricing.Source{Driver: "task_plugin", PluginKey: manifest.Key}, Reason: "no vetted host phase contract"}
		for key, field := range manifest.UsageSchema {
			context.Schema[key] = types.TaskUsageField{Type: field.Type, Unit: field.Unit, Enum: field.Enum}
		}
		// Matching a familiar plugin key/schema is NOT approval of this pinned
		// package's phase semantics. No executable plugin contract is registered
		// by P1.2; retain schema-only editing and reject quotes until P2 vetting.
		if !entry.Enabled || entry.Revoked {
			context.Reason = "plugin disabled or revoked"
		}
		result = append(result, context)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
func findPricingContext(contexts []ModelPricingContext, id string) (ModelPricingContext, bool) {
	for _, context := range contexts {
		if context.ID == id {
			return context, true
		}
	}
	return ModelPricingContext{}, false
}

func effectiveModelPricing(values pricingMaps, name string) types.ModelPricingValues {
	result := modelPricingValues(values, name)
	alias := ratio_setting.FormatMatchingModelName(name)
	for _, key := range []string{"ModelPrice", "ModelRatio", "CompletionRatio", "AudioRatio", "AudioCompletionRatio"} {
		delete(result, key)
		lookup := values[key]
		if key == "ModelRatio" || key == "CompletionRatio" {
			lookup = map[string]any{}
			for model, value := range values[key] {
				lookup[ratio_setting.FormatMatchingModelName(model)] = value
			}
		}
		if value, exists := lookup[alias]; exists {
			result[key] = value
		} else if strings.HasSuffix(name, ratio_setting.CompactModelSuffix) && (key == "ModelPrice" || key == "ModelRatio") {
			if value, exists := lookup[ratio_setting.CompactWildcardModelKey]; exists {
				result[key] = value
			}
		}
	}
	if result["billing_setting.billing_mode"] == "tiered_expr" {
		return result
	}
	if _, exists := result["ModelPrice"]; exists {
		return result
	}
	completion := map[string]float64{}
	for model, value := range values["CompletionRatio"] {
		if number, ok := value.(float64); ok {
			completion[ratio_setting.FormatMatchingModelName(model)] = number
		}
	}
	result["CompletionRatio"] = ratio_setting.ResolveConfiguredCompletionRatio(name, completion)
	for key, fallback := range map[string]float64{"CacheRatio": 1, "CreateCacheRatio": 1.25, "ImageRatio": 1, "AudioRatio": 1, "AudioCompletionRatio": 1} {
		if _, exists := result[key]; !exists {
			result[key] = fallback
		}
	}
	return result
}
func buildModelPricingEntry(values pricingMaps, a *pricingAuthority, name string) ModelPricingEntry {
	contexts := a.contexts(name)
	contracts := map[string]any{}
	bindings := map[string]any{"catalog_ready": a.catalogReady}
	dependencies := struct {
		Models           map[string]types.ModelPricingValues
		NormalizedRatios map[string]map[string]any
		CompactFallback  types.ModelPricingValues
		SelfUse          bool
	}{Models: map[string]types.ModelPricingValues{}, NormalizedRatios: map[string]map[string]any{}, SelfUse: a.selfUse}
	models := map[string]bool{name: true, ratio_setting.FormatMatchingModelName(name): true}
	for i := range contexts {
		context := &contexts[i]
		contracts[context.ID] = struct {
			Schema   map[string]types.TaskUsageField
			Contract *types.TaskFactContract
		}{context.Schema, context.Contract}
		bindings[context.ID] = context.binding
		models[context.MappedModel] = true
		models[ratio_setting.FormatMatchingModelName(context.MappedModel)] = true
		prices := taskPriceMaps(values)
		if text, ok := prices.PluginBillingExpr[context.PluginKey+"::"+name]; ok && context.PluginKey != "" {
			copy := text
			context.Configured = &copy
		}
		selected, err := taskpricing.SelectExpression(prices, context.source, name, context.MappedModel)
		if err == nil {
			text := selected.Expression
			context.Effective = &text
			context.SchemaCompatible = validatePricingSchema(text, context.Schema) == nil
			if context.Contract != nil {
				context.Compatible = taskpricing.ValidateExpressionContract(text, *context.Contract) == nil
			}
		}
	}
	// Include inherited alias/mapped prices in the equality token, not just the
	// editable model. A changed dependency must invalidate an old quote.
	for model := range models {
		dependencies.Models[model] = modelPricingValues(values, model)
	}
	for _, key := range []string{"ModelRatio", "CompletionRatio"} {
		dependencies.NormalizedRatios[key] = map[string]any{}
		for model, value := range values[key] {
			if models[ratio_setting.FormatMatchingModelName(model)] {
				dependencies.NormalizedRatios[key][model] = value
			}
		}
	}
	if strings.HasSuffix(name, ratio_setting.CompactModelSuffix) {
		dependencies.CompactFallback = modelPricingValues(values, ratio_setting.CompactWildcardModelKey)
	}
	entry := ModelPricingEntry{ModelName: name, Version: pricingDigest(dependencies), Configured: modelPricingValues(values, name), Effective: effectiveModelPricing(values, name), ContractDigest: pricingDigest(contracts), BindingVersion: pricingDigest(bindings), Contexts: contexts, PluginVariants: []ModelPricingContext{}}
	if err := validateModelPricing(name, entry.Configured, entry.Configured, contexts); err != nil {
		entry.ConfigurationError = err.Error()
	}
	if a.selfUse && entry.Effective["billing_setting.billing_mode"] != "tiered_expr" {
		if _, hasPrice := entry.Effective["ModelPrice"]; !hasPrice {
			if _, hasRatio := entry.Effective["ModelRatio"]; !hasRatio {
				entry.Effective["ModelRatio"] = float64(37.5)
			}
		}
	}
	for _, context := range contexts {
		if context.PluginKey != "" {
			entry.PluginVariants = append(entry.PluginVariants, context)
		}
	}
	// Keep stale configured overrides visible even if the source disappeared.
	variants, _ := entry.Configured[billing_setting.PluginBillingExprOption].(map[string]any)
	for plugin, raw := range variants {
		if _, found := findPricingContext(contexts, "plugin:"+plugin); !found {
			expression, _ := raw.(string)
			entry.PluginVariants = append(entry.PluginVariants, ModelPricingContext{ID: "plugin:" + plugin, PluginKey: plugin, MappedModel: name, Configured: &expression, Reason: "source unavailable"})
		}
	}
	sort.Slice(entry.PluginVariants, func(i, j int) bool { return entry.PluginVariants[i].ID < entry.PluginVariants[j].ID })
	return entry
}
func taskPriceMaps(values pricingMaps) types.TaskPricingMaps {
	result := types.TaskPricingMaps{BillingMode: map[string]string{}, BillingExpr: map[string]string{}, PluginBillingExpr: map[string]string{}}
	for key, target := range map[string]map[string]string{"billing_setting.billing_mode": result.BillingMode, "billing_setting.billing_expr": result.BillingExpr, billing_setting.PluginBillingExprOption: result.PluginBillingExpr} {
		for model, value := range values[key] {
			if text, ok := value.(string); ok {
				target[model] = text
			}
		}
	}
	return result
}

func validatePricingSchema(expression string, schema map[string]types.TaskUsageField) error {
	if schema == nil {
		return errors.New("source schema unavailable")
	}
	keys, err := billingexpr.TaskUsageKeys(expression)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, declared := schema[key]; !declared {
			return fmt.Errorf("undeclared task pricing fact %q", key)
		}
	}
	return nil
}
