// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/types"
	"gorm.io/gorm"
)

// TaskPricingShadowRequest identifies a configured channel, not an executor.
// It deliberately accepts no draft prices, guards, unit, multipliers or clock.
type TaskPricingShadowRequest struct {
	ModelName string
	ChannelID int
	Group     string
}

// TaskPricingShadowContext is a detached configuration observation, never paid
// admission or a persisted TaskBillingPlan. It excludes user-specific group
// overrides, actual driver/package loading and provider exposure bounds.
// Equality tokens describe their respective inputs; they are not time order,
// a DB CAS, or assurance that configuration is unchanged after the read.
type TaskPricingShadowContext struct {
	Scope            string
	AdmissionReady   bool
	ChannelID        int
	ChannelType      int
	OriginalModel    string
	MappedModel      string
	Profile          string
	Source           taskpricing.Source
	Selection        taskpricing.Selection
	Contract         types.TaskFactContract
	Guards           taskpricing.Guards
	Group            string
	GroupRatio       float64
	QuotaPerUnit     float64
	ReservationFloor int
	TermsVersion     string
	PricingAt        time.Time
	PluginKey        string
	PluginVersion    string
	PackageDigest    string
	PhaseApprovalID  string
}

// ReadTaskPricingShadowContext uses the existing pricing authority transaction
// and source-selection core. Like New API's pricing snapshots (ea343c4a,
// model/model_pricing_config.go), it separates configured prices and sources;
// MAX additionally requires a host phase contract and explicit profile.
// It performs no HTTP, task submission, cache publication or financial writes.
func ReadTaskPricingShadowContext(ctx context.Context, request TaskPricingShadowRequest) (*TaskPricingShadowContext, error) {
	return readTaskPricingShadowContext(DB, ctx, request, time.Now().UTC())
}

func readTaskPricingShadowContext(db *gorm.DB, ctx context.Context, request TaskPricingShadowRequest, at time.Time) (*TaskPricingShadowContext, error) {
	return readTaskPricingShadowWithPlugin(db, ctx, request, at, nil, nil)
}

func readTaskPricingShadowWithPlugin(db *gorm.DB, ctx context.Context, request TaskPricingShadowRequest, at time.Time, pinned *types.TaskPluginBinding, registry *taskpricing.PhaseRegistry) (*TaskPricingShadowContext, error) {
	if db == nil || ctx == nil || at.IsZero() || !validPricingName(request.ModelName) || request.ChannelID <= 0 || request.Group == "" {
		return nil, errors.New("task pricing database, context, model, channel and group required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// QuotaPerUnit is node configuration. Freeze this one value, rather than
	// letting the result and terms token describe different reads of it.
	unit := common.QuotaPerUnit
	if unit <= 0 || math.IsNaN(unit) || math.IsInf(unit, 0) {
		return nil, errors.New("invalid node quota unit")
	}
	var result *TaskPricingShadowContext
	err := readPricingState(db.WithContext(ctx), func(tx *gorm.DB, values pricingMaps, authority *pricingAuthority) error {
		entry := buildModelPricingEntry(values, authority, request.ModelName)
		if entry.ConfigurationError != "" {
			return errors.New(entry.ConfigurationError)
		}
		selected, found := findPricingContext(entry.Contexts, fmt.Sprintf("channel:%d", request.ChannelID))
		approvalID := ""
		if pinned != nil {
			pluginContext, id, err := approvedPluginShadow(authority, request, *pinned, registry)
			if err != nil {
				return err
			}
			selected, approvalID = pluginContext, id
			found = true
		}
		if !found || !selected.Available || selected.Contract == nil || selected.Profile != taskpricing.NewAPITask ||
			(pinned == nil && selected.source.Driver != types.TaskUsageProducerKindGoAdapter) {
			return errors.New("configured channel has no explicit task shadow profile and available host contract")
		}
		if !slices.Contains(selected.Groups, request.Group) {
			return errors.New("configured channel does not serve this shadow group")
		}
		selection, err := taskpricing.SelectExpression(taskPriceMaps(values), selected.source, request.ModelName, selected.MappedModel)
		if err != nil {
			return err
		}
		if selection.Model != request.ModelName {
			mapped := buildModelPricingEntry(values, authority, selection.Model)
			if mapped.ConfigurationError != "" {
				return errors.New(mapped.ConfigurationError)
			}
		}
		contract, err := taskpricing.NormalizeContract(*selected.Contract)
		if err != nil {
			return err
		}
		if err := taskpricing.ValidateExpressionContract(selection.Expression, contract); err != nil {
			return err
		}
		digest, err := taskpricing.ContractDigest(contract)
		if err != nil {
			return err
		}
		ratio, floor, terms, err := readQuoteTermsWithUnit(tx, request.Group, unit)
		if err != nil {
			return err
		}
		_, _, after, err := readQuoteTermsWithUnit(tx, request.Group, unit)
		if err != nil {
			return err
		}
		if terms != after {
			return ErrModelPricingConflict
		}
		binding, ok := selected.binding.(pricingChannel)
		if !ok {
			return errors.New("configured shadow channel binding unavailable")
		}
		result = &TaskPricingShadowContext{
			Scope:     "configured_channel_shadow; configured group only; no executor, package or paid admission proof",
			ChannelID: request.ChannelID, ChannelType: binding.Type,
			OriginalModel: request.ModelName, MappedModel: selected.MappedModel, Profile: selected.Profile,
			Source: selected.source, Selection: selection, Contract: contract,
			Guards: taskpricing.Guards{PriceVersion: entry.Version, ContractDigest: digest, BindingVersion: entry.BindingVersion},
			Group:  request.Group, GroupRatio: ratio, QuotaPerUnit: unit, ReservationFloor: floor,
			TermsVersion: terms, PricingAt: at.UTC(),
		}
		if pinned != nil {
			result.Scope = "approved_package_configured_channel_shadow; no JS, provider-cost, funding or paid-admission proof"
			result.PluginKey, result.PluginVersion, result.PackageDigest, result.PhaseApprovalID = pinned.PluginKey, pinned.Version, pinned.PackageDigest, approvalID
			result.Guards.BindingVersion = pricingDigest(struct {
				Configured string
				Binding    types.TaskPluginBinding
				ApprovalID string
			}{entry.BindingVersion, *pinned, approvalID})
		}
		return nil
	})
	if err != nil {
		return nil, err // Never return a partially built observation on failed recheck/commit.
	}
	return result, nil
}
