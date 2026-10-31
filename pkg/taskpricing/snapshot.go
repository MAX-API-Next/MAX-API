// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

const ExpressionPlanRounding = "half_away_from_zero@1"

// FreezeInput is host-owned. The pure builder cannot prove authoritative reads,
// executor readiness or a provider exposure bound. No production admission
// caller is enabled by this API. Channel/request identity must be persisted
// with the actual reservation before any provider I/O in a future caller.
type FreezeInput struct {
	PreviewInput
	ChannelID     int
	Action        string
	RequestID     string
	SourceID      string
	PluginVersion string
	PackageDigest string
}

func Freeze(input FreezeInput) (*types.TaskBillingPlan, error) {
	if input.Actual != nil {
		return nil, fmt.Errorf("cannot freeze a task from completion facts")
	}
	if strings.TrimSpace(input.MappedModel) == "" {
		return nil, fmt.Errorf("actual upstream model is required for a frozen task plan")
	}
	preview, err := Preview(input.PreviewInput)
	if err != nil {
		return nil, err
	}
	contract, err := NormalizeContract(input.Contract)
	if err != nil {
		return nil, err
	}
	facts, err := SubmitFacts(contract, input.Submit)
	if err != nil {
		return nil, err
	}
	plan := &types.TaskBillingPlan{
		Profile: NewAPITask, Source: "task_expression", RuleKey: expressionRuleKey(preview.Selection),
		SchemaVersion: 1, Mode: types.TaskBillingPlanModeBoundedActual, ConfigHash: input.Current.PriceVersion,
		Currency: "USD", GroupRatio: input.GroupRatio, QuotaPerUnit: input.QuotaPerUnit,
		UsageProducerKind: input.Source.Driver, UsageSourceID: input.SourceID,
		UsageSchemaVersion: contract.Version, UsageContractDigest: preview.ContractDigest,
		EstimateQuota: preview.Estimate.QuotaAfterGroup, ReserveQuota: preview.ReservedQuota,
		ExpressionPlan: &types.TaskExpressionPlan{
			Version: 1, OriginalModel: input.OriginalModel, MappedModel: input.MappedModel,
			SelectedModel: preview.Selection.Model, SelectedPluginKey: preview.Selection.PluginKey, Driver: input.Source.Driver, PluginKey: input.Source.PluginKey,
			PluginVersion: input.PluginVersion, PackageDigest: input.PackageDigest, ChannelID: input.ChannelID,
			Action: input.Action, RequestID: input.RequestID, BindingVersion: input.Current.BindingVersion,
			Expression: preview.Selection.Expression, ExpressionHash: billingexpr.ExprHashString(preview.Selection.Expression),
			PricingAt: input.PricingAt.UTC(), Rounding: ExpressionPlanRounding, ReservationFloor: input.ReservationFloor,
			Contract: contract, SubmitFacts: facts,
		},
	}
	if err := ValidateSnapshot(plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func expressionRuleKey(selected Selection) string {
	if selected.PluginKey != "" {
		return selected.PluginKey + "::" + selected.Model
	}
	return selected.Model
}

// Request probes are deliberately rejected, even in dead branches. Replaying
// them with an empty or live request would change the price. This restriction
// applies to frozen task plans, not the existing read-only preview API.
func validateFrozenDependencies(expression string) error {
	// Match the shared compiler's syntax view without rewriting the snapshot
	// expression or its hash, both of which retain the original version tag.
	_, body := billingexpr.ParseExprVersion(expression)
	tree, err := parser.Parse(body)
	if err != nil {
		return err
	}
	var invalid bool
	ast.Find(tree.Node, func(node ast.Node) bool {
		if id, ok := node.(*ast.IdentifierNode); ok && (id.Value == "param" || id.Value == "header") {
			invalid = true
		}
		return false
	})
	if invalid {
		return fmt.Errorf("request probes are not supported in frozen task plans; use declared request facts")
	}
	return nil
}

// ValidateSnapshot uses only the persisted snapshot, never live price maps or
// the current plugin registry. A digest proves integrity/equality, not trust or
// ordering; the host must supply trusted producer and binding identity.
func ValidateSnapshot(plan *types.TaskBillingPlan) error {
	if plan == nil || plan.Profile != NewAPITask || plan.ExpressionPlan == nil {
		return fmt.Errorf("explicit task expression snapshot required")
	}
	p := plan.ExpressionPlan
	if p.Version != 1 || plan.SchemaVersion != 1 || plan.Mode != types.TaskBillingPlanModeBoundedActual ||
		plan.Source != "task_expression" || plan.Currency != "USD" || plan.ConfigHash == "" ||
		p.BindingVersion == "" || p.ChannelID <= 0 || strings.TrimSpace(p.Action) == "" ||
		strings.TrimSpace(p.RequestID) == "" || p.PricingAt.IsZero() || p.Rounding != ExpressionPlanRounding ||
		p.ReservationFloor < 0 || p.ReservationFloor > common.MaxQuota {
		return fmt.Errorf("invalid task expression snapshot metadata")
	}
	if len(plan.Components) != 0 || plan.Resolution != "" || plan.RequestedOutputDurationSeconds != 0 ||
		plan.InputVideoCount != 0 || plan.InputAudioCount != 0 || plan.InputImageCount != 0 {
		return fmt.Errorf("task expression snapshot contains legacy components")
	}
	source := Source{Driver: p.Driver, PluginKey: p.PluginKey}
	if err := source.validate(); err != nil {
		return err
	}
	if plan.UsageProducerKind != p.Driver || strings.TrimSpace(plan.UsageSourceID) == "" {
		return fmt.Errorf("invalid task expression producer identity")
	}
	if p.Driver == types.TaskUsageProducerKindTaskPlugin {
		digest, err := hex.DecodeString(p.PackageDigest)
		if p.PluginVersion == "" || err != nil || len(digest) != 32 || plan.UsageSourceID != p.PluginKey {
			return fmt.Errorf("invalid frozen plugin package identity")
		}
	} else if p.PluginVersion != "" || p.PackageDigest != "" {
		return fmt.Errorf("Go adapter cannot claim a plugin package")
	}
	if strings.TrimSpace(p.OriginalModel) == "" || strings.TrimSpace(p.MappedModel) == "" || (p.SelectedModel != p.OriginalModel && p.SelectedModel != p.MappedModel) ||
		(p.SelectedPluginKey != "" && p.SelectedPluginKey != p.PluginKey) ||
		plan.RuleKey != expressionRuleKey(Selection{Model: p.SelectedModel, PluginKey: p.SelectedPluginKey}) {
		return fmt.Errorf("invalid frozen model selection")
	}
	if p.ExpressionHash != billingexpr.ExprHashString(p.Expression) {
		return fmt.Errorf("frozen task expression hash mismatch")
	}
	if err := validateFrozenDependencies(p.Expression); err != nil {
		return err
	}
	if err := ValidateExpressionContract(p.Expression, p.Contract); err != nil {
		return err
	}
	digest, err := ContractDigest(p.Contract)
	if err != nil {
		return err
	}
	if plan.UsageContractDigest != digest || plan.UsageSchemaVersion != p.Contract.Version {
		return fmt.Errorf("frozen task contract identity mismatch")
	}
	facts, err := SubmitFacts(p.Contract, p.SubmitFacts)
	if err != nil {
		return err
	}
	quote, err := quoteSnapshot(plan, facts)
	if err != nil {
		return err
	}
	if plan.EstimateQuota != quote.QuotaAfterGroup || plan.ReserveQuota != max(p.ReservationFloor, quote.QuotaAfterGroup) {
		return fmt.Errorf("frozen task totals do not match the expression")
	}
	return nil
}

func quoteSnapshot(plan *types.TaskBillingPlan, facts types.TaskPricingFacts) (billingexpr.TaskQuote, error) {
	p := plan.ExpressionPlan
	return billingexpr.QuoteTaskExpression(billingexpr.TaskQuoteInput{
		Profile: plan.Profile, Expression: p.Expression, Usage: facts, PricingAt: p.PricingAt,
		QuotaPerUnit: plan.QuotaPerUnit, GroupRatio: plan.GroupRatio,
	})
}

func QuoteFinal(plan *types.TaskBillingPlan, envelope *types.TaskPricingUsageEnvelope) (billingexpr.TaskQuote, error) {
	if err := ValidateSnapshot(plan); err != nil {
		return billingexpr.TaskQuote{}, err
	}
	p := plan.ExpressionPlan
	if envelope == nil || envelope.ProducerKind != plan.UsageProducerKind || envelope.SourceID != plan.UsageSourceID ||
		envelope.PluginVersion != p.PluginVersion || envelope.PackageDigest != p.PackageDigest ||
		envelope.ChannelID != p.ChannelID || envelope.RequestID != p.RequestID || envelope.Action != p.Action ||
		envelope.SchemaVersion != plan.UsageSchemaVersion || envelope.ContractDigest != plan.UsageContractDigest ||
		envelope.Stage != types.TaskUsageSourceProviderResponse || envelope.Completeness != types.TaskUsageCompletenessComplete {
		return billingexpr.TaskQuote{}, fmt.Errorf("missing, incomplete or mismatched final task pricing evidence")
	}
	facts, err := CompleteFacts(plan.ExpressionPlan.Contract, plan.ExpressionPlan.SubmitFacts, envelope.Facts)
	if err != nil {
		return billingexpr.TaskQuote{}, err
	}
	return quoteSnapshot(plan, facts)
}
