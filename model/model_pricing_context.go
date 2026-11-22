// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"context"
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
	PreviewOnly           bool                            `json:"preview_only,omitempty"`
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
	// Local to this captured authority; never shared across revisions/nodes.
	validatedPackages    map[string]*taskplugin.Package
	pluginSchemas        map[string][]pricingPluginSchema
	channelContexts      map[string][]ModelPricingContext
	excludedPluginDigest string
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
		return nil, &modelPricingDataError{"SelfUseModeEnabled", errors.New("duplicate self-use pricing setting")}
	}
	if len(defaults) == 1 {
		if defaults[0].Value != "true" && defaults[0].Value != "false" {
			return nil, &modelPricingDataError{"SelfUseModeEnabled", errors.New("invalid self-use pricing setting")}
		}
		a.selfUse = defaults[0].Value == "true"
	}
	// All writers take the head before channel rows. Locking reads below also
	// avoid reusing an older MySQL REPEATABLE READ snapshot after deletion.
	if err := a.readCatalog(tx, lock); err != nil {
		return nil, err
	}
	query := tx.Model(&Channel{}).Select("id", "type", "status", "models", "group", "model_mapping", "setting", "base_url", "param_override", "header_override", "settings").Order("id")
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Find(&a.channels).Error; err != nil {
		return nil, err
	}
	return a, nil
}

func (a *pricingAuthority) readCatalog(tx *gorm.DB, lock bool) error {
	if !tx.Migrator().HasTable(&TaskPluginCatalogHead{}) || !tx.Migrator().HasTable(&TaskPluginVersion{}) {
		return nil
	}
	if lock {
		if err := lockTaskPluginCatalogHead(tx); err != nil {
			return err
		}
	}
	var head TaskPluginCatalogHead
	q := tx
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.First(&head, 1).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	versionQuery := tx
	if lock {
		versionQuery = tx.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	rows, _, loadErr := NewTaskPluginCatalogStore(tx).loadTaskPluginVersions(versionQuery)
	if loadErr != nil {
		return loadErr
	}
	if err == nil && (head.Revision <= 0 || head.MutationID == "") {
		return taskplugin.ErrCatalogInvalid
	}
	receiptQuery := tx.Session(&gorm.Session{NewDB: true})
	if lock {
		receiptQuery = receiptQuery.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	deletions, loadErr := loadTaskPluginDeletions(receiptQuery)
	if loadErr != nil {
		return loadErr
	}
	if !lock {
		var after TaskPluginCatalogHead
		afterErr := tx.Session(&gorm.Session{NewDB: true}).First(&after, 1).Error
		if afterErr != nil && !errors.Is(afterErr, gorm.ErrRecordNotFound) {
			return afterErr
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if !errors.Is(afterErr, gorm.ErrRecordNotFound) {
				return ErrModelPricingConflict
			}
		} else if afterErr != nil || head != after {
			return ErrModelPricingConflict
		}
	}
	if errors.Is(err, gorm.ErrRecordNotFound) && (len(rows) > 0 || len(deletions) > 0) {
		return taskplugin.ErrCatalogInvalid
	}
	if err := validateTaskPluginDeletionHead(head, deletions); err != nil {
		return err
	}
	snapshot := taskplugin.CatalogSnapshot{Revision: head.Revision, Packages: rows, Deletions: deletions}
	validated, err := taskplugin.ValidatedStoredCatalogPackagesContext(tx.Statement.Context, snapshot)
	if err != nil {
		return err
	}
	a.plugins, a.validatedPackages, a.catalogReady = rows, validated, true
	return nil
}
func (a *pricingAuthority) signature() string {
	return pricingDigest(struct {
		Channels []pricingChannel
		Plugins  []taskplugin.StoredPackage
		Ready    bool
		SelfUse  bool
	}{a.channels, a.plugins, a.catalogReady, a.selfUse})
}
func (a *pricingAuthority) contexts(model string) ([]ModelPricingContext, error) {
	return a.contextsContext(context.Background(), model)
}

func (a *pricingAuthority) contextsContext(ctx context.Context, model string) ([]ModelPricingContext, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := []ModelPricingContext{}
	if a.channelContexts != nil {
		result = append(result, a.channelContexts[model]...)
	} else {
		for _, channel := range a.channels {
			references, err := channel.pricingModelReferences(ctx)
			if err != nil {
				return nil, err
			}
			if advertised, found := references.models[model]; found {
				context, err := a.pricingChannelContext(ctx, channel, model, advertised, references)
				if err != nil {
					return nil, err
				}
				result = append(result, context)
			}
		}
	}
	if a.pluginSchemas != nil {
		for _, schema := range a.pluginSchemas[model] {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if schema.entry.Digest != a.excludedPluginDigest {
				result = append(result, schema.pricingContext(model))
			}
		}
	} else {
		for _, entry := range a.plugins {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !entry.Active {
				continue
			}
			pkg, err := a.pricingPluginPackage(ctx, entry)
			if err != nil {
				return nil, err
			}
			schema, err := pkg.ModelUsageSchema(model)
			if err != nil {
				continue
			}
			info := pricingPluginSchema{entry: entry, key: pkg.Manifest().Key, executable: pkg.Executable(), schema: schema}
			result = append(result, info.pricingContext(model))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

type pricingChannelReferences struct {
	models       map[string]bool // Value records whether the model is directly advertised.
	mapping      map[string]string
	mappingValid bool
}

func (channel pricingChannel) advertisedModelName(name string) string {
	if channel.Type == constant.ChannelTypeTaskPlugin {
		return strings.TrimSpace(name)
	}
	return name
}

func (channel pricingChannel) pricingModelReferences(ctx context.Context) (pricingChannelReferences, error) {
	references := pricingChannelReferences{models: make(map[string]bool), mapping: make(map[string]string)}
	for _, model := range strings.Split(channel.Models, ",") {
		if err := ctx.Err(); err != nil {
			return references, err
		}
		references.models[channel.advertisedModelName(model)] = true
	}
	references.mappingValid = channel.ModelMapping == nil || *channel.ModelMapping == "" ||
		common.UnmarshalJsonStr(*channel.ModelMapping, &references.mapping) == nil
	for original, target := range references.mapping {
		if err := ctx.Err(); err != nil {
			return references, err
		}
		if references.models[original] && !references.models[target] {
			references.models[target] = false
		}
	}
	return references, ctx.Err()
}

func (channel pricingChannel) pricingModelContext(model string, advertised bool, references pricingChannelReferences) ModelPricingContext {
	result := ModelPricingContext{ID: fmt.Sprintf("channel:%d", channel.ID), ChannelID: channel.ID, MappedModel: model,
		source: taskpricing.Source{Driver: "go_adapter"}, binding: channel, legacyDefaultConsumer: advertised, Reason: "unsupported host pricing contract"}
	result.Groups = strings.Split(channel.Group, ",")
	if channel.Setting != nil {
		var settings map[string]any
		if common.UnmarshalJsonStr(*channel.Setting, &settings) == nil {
			result.Profile, _ = settings["task_pricing_profile"].(string)
		}
	}
	if !references.mappingValid {
		result.Reason = "invalid channel model mapping"
		return result
	}
	if mapped, exists := references.mapping[model]; exists && advertised {
		result.MappedModel = mapped
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
	if contract, ok := taskpricing.KnownContract(family, result.MappedModel); ok {
		result.legacyDefaultConsumer = false
		result.Contract = &contract
		result.Schema = contract.UsageSchema
		result.Available = channel.Status == 1 && advertised
		result.Reason = ""
		if !result.Available {
			result.Reason = "channel disabled or mapped-target-only; quote its advertised alias"
		}
	}
	return result
}

func (a *pricingAuthority) indexChannelContexts(ctx context.Context, models []string) error {
	relevant := make(map[string]bool, len(models))
	for _, model := range models {
		relevant[model] = true
	}
	index := make(map[string][]ModelPricingContext, len(models))
	for _, channel := range a.channels {
		references, err := channel.pricingModelReferences(ctx)
		if err != nil {
			return err
		}
		for model, advertised := range references.models {
			if err := ctx.Err(); err != nil {
				return err
			}
			if relevant[model] {
				context, err := a.pricingChannelContext(ctx, channel, model, advertised, references)
				if err != nil {
					return err
				}
				index[model] = append(index[model], context)
			}
		}
	}
	a.channelContexts = index
	return ctx.Err()
}

func (a *pricingAuthority) pricingChannelContext(ctx context.Context, channel pricingChannel, model string, advertised bool, references pricingChannelReferences) (ModelPricingContext, error) {
	result := channel.pricingModelContext(model, advertised, references)
	if channel.Type != constant.ChannelTypeTaskPlugin {
		return result, nil
	}
	result.legacyDefaultConsumer = false
	result.Reason = "configured task plugin is unavailable"
	settings, err := ReadTaskPluginChannelSettings(channel.Setting)
	if err != nil || !advertised || !references.mappingValid {
		return result, nil
	}
	result.PluginKey = settings.Key
	result.source = taskpricing.Source{Driver: "task_plugin", PluginKey: settings.Key}
	for _, entry := range a.plugins {
		if !entry.Active || entry.Digest == a.excludedPluginDigest {
			continue
		}
		p, err := a.pricingPluginPackage(ctx, entry)
		if err != nil {
			return result, err
		}
		manifest := p.Manifest()
		if manifest.Key != settings.Key {
			continue
		}
		schema, err := p.ModelUsageSchema(result.MappedModel)
		if err != nil {
			result.Reason = "mapped model is unavailable in the active task plugin"
			break
		}
		source := (pricingPluginSchema{entry: entry, key: settings.Key, executable: p.Executable(), schema: schema}).pricingContext(result.MappedModel)
		result.Schema, result.Reason, result.Profile = source.Schema, source.Reason, source.Profile
		result.PreviewOnly = source.PreviewOnly
		result.Available = source.Available && channel.Status == 1
		if _, err := manifest.ResolveChannelConfig(settings.Config, settings.Upstream); err != nil {
			result.Available = false
			result.Reason = "channel configuration does not match the active task plugin"
		} else if source.Available && channel.Status != 1 {
			result.Reason = "channel disabled or mapped-target-only; quote its advertised alias"
		}
		result.binding = struct{ Channel, Plugin any }{channel, source.binding}
		break
	}
	return result, nil
}

type pricingPluginSchema struct {
	entry      taskplugin.StoredPackage
	key        string
	executable bool
	schema     map[string]taskplugin.UsageField
}

func (a *pricingAuthority) pricingPluginPackage(ctx context.Context, entry taskplugin.StoredPackage) (*taskplugin.Package, error) {
	if pkg := a.validatedPackages[entry.Digest]; pkg != nil {
		return pkg, nil
	}
	pkg, err := entry.LoadRetainedPackage(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	} // A failed reload cannot erase a pricing source.
	if a.validatedPackages == nil {
		a.validatedPackages = make(map[string]*taskplugin.Package)
	}
	a.validatedPackages[entry.Digest] = pkg
	return pkg, nil
}

// The deletion check projects many models under the same locks. Load each
// retained manifest once and share immutable schemas across its model entries.
func (a *pricingAuthority) indexPluginSchemas(ctx context.Context) error {
	index := make(map[string][]pricingPluginSchema)
	for _, entry := range a.plugins {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.Active {
			continue
		}
		pkg, err := a.pricingPluginPackage(ctx, entry)
		if err != nil {
			return err
		}
		manifest := pkg.Manifest()
		profiles := make(map[string]map[string]taskplugin.UsageField)
		for _, profile := range manifest.UsageProfiles {
			for _, model := range profile.Models {
				if err := ctx.Err(); err != nil {
					return err
				}
				profiles[model] = profile.Schema
			}
		}
		for _, model := range manifest.Models {
			if err := ctx.Err(); err != nil {
				return err
			}
			schema := manifest.UsageSchema
			if len(manifest.UsageProfiles) != 0 {
				var found bool
				schema, found = profiles[model]
				if !found {
					continue
				}
			}
			index[model] = append(index[model], pricingPluginSchema{entry: entry, key: manifest.Key, executable: pkg.Executable(), schema: schema})
		}
	}
	a.pluginSchemas = index
	return ctx.Err()
}

func (p pricingPluginSchema) pricingContext(model string) ModelPricingContext {
	entry := p.entry
	result := ModelPricingContext{ID: "plugin:" + p.key, PluginKey: p.key, MappedModel: model, Schema: map[string]types.TaskUsageField{}, binding: struct {
		Digest           string
		Enabled, Revoked bool
	}{entry.Digest, entry.Enabled, entry.Revoked}, source: taskpricing.Source{Driver: "task_plugin", PluginKey: p.key}, Reason: "no vetted host phase contract"}
	for key, field := range p.schema {
		result.Schema[key] = types.TaskUsageField{Type: field.Type, Unit: field.Unit, Enum: slices.Clone(field.Enum)}
	}
	if p.executable && entry.Enabled && !entry.Revoked {
		result.Available = true
		result.PreviewOnly = true
		result.Profile = taskpricing.NewAPITask
		result.Reason = "plugin configuration preview only; paid task admission unavailable"
	}
	if !entry.Enabled || entry.Revoked {
		result.Reason = "plugin disabled or revoked"
	}
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
func buildModelPricingEntry(values pricingMaps, a *pricingAuthority, name string) (ModelPricingEntry, error) {
	return buildModelPricingEntryContext(context.Background(), values, a, name)
}

func buildModelPricingEntryContext(ctx context.Context, values pricingMaps, a *pricingAuthority, name string) (ModelPricingEntry, error) {
	contexts, err := a.contextsContext(ctx, name)
	if err != nil {
		return ModelPricingEntry{}, err
	}
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
			} else if context.PreviewOnly {
				context.Compatible = context.SchemaCompatible
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
		if !slices.ContainsFunc(contexts, func(context ModelPricingContext) bool { return context.PluginKey == plugin }) {
			expression, _ := raw.(string)
			entry.PluginVariants = append(entry.PluginVariants, ModelPricingContext{ID: "plugin:" + plugin, PluginKey: plugin, MappedModel: name, Configured: &expression, Reason: "source unavailable"})
		}
	}
	sort.Slice(entry.PluginVariants, func(i, j int) bool { return entry.PluginVariants[i].ID < entry.PluginVariants[j].ID })
	// Read DTOs stay typed even when historical billing rows are corrupt. The
	// error is explicit; snapshot.Options and the equality token retain raw data.
	if omitInvalidBillingValues(entry.Configured) {
		entry.ConfigurationError += "; invalid stored billing values omitted from editable projection; raw values remain in options"
	}
	omitInvalidBillingValues(entry.Effective)
	return entry, nil
}

func omitInvalidBillingValues(values types.ModelPricingValues) bool {
	omitted := false
	for _, key := range []string{"billing_setting.billing_mode", "billing_setting.billing_expr"} {
		if value, exists := values[key]; exists {
			if _, ok := value.(string); !ok {
				delete(values, key)
				omitted = true
			}
		}
	}
	if variants, ok := values[billing_setting.PluginBillingExprOption].(map[string]any); ok {
		for key, value := range variants {
			if _, ok := value.(string); !ok {
				delete(variants, key)
				omitted = true
			}
		}
		if len(variants) == 0 {
			delete(values, billing_setting.PluginBillingExprOption)
		}
	}
	return omitted
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
