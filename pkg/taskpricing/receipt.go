// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Lifecycle reference: QuantumNous/new-api ba5f7367 relay/relay_task.go.
// Independent equality contract; MAX retains funding and settlement authority.
package taskpricing

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

var ErrTaskPricingReceiptConflict = errors.New("task pricing receipt evidence is inconsistent or unsupported")

// TaskPricingReceipt is a versioned equality anchor, not an authenticated
// signature, proof of applied funds, or an admission permit. It has no GORM
// field/writer yet: a future host must store it atomically with its original
// plan and funding intent, then upgrade every recovery consumer before use.
type TaskPricingReceipt struct {
	Version int    `json:"version"`
	Digest  string `json:"digest"`
}

// WalletReceiptInput is host-owned frozen identity. ReserveQuota inside Plan
// supplies the funding/token/target amount; model adapters must check those
// amounts and the wallet source against their actual funding intent first.
type WalletReceiptInput struct {
	TaskID       int64
	OperationKey string
	Group        string
	Plan         *types.TaskBillingPlan
	Binding      *types.TaskPluginBinding
}

// PrepareWalletReservationReceipt is pure. No live configuration, database,
// registry, credentials, or mutable funding status participates in this digest.
func PrepareWalletReservationReceipt(input WalletReceiptInput) (*TaskPricingReceipt, error) {
	b, plan := input.Binding, input.Plan
	if input.TaskID <= 0 || strings.TrimSpace(input.Group) == "" || input.Group != strings.TrimSpace(input.Group) ||
		b == nil || b.UserID <= 0 || b.TokenID <= 0 || b.ChannelID <= 0 ||
		b.RequestID == "" || len(b.RequestID) > 64 || b.RequestID != strings.TrimSpace(b.RequestID) ||
		input.OperationKey != "request:"+b.RequestID+":pre-consume" || !receiptDigestValid(b.PhaseApprovalID) ||
		ValidateSnapshot(plan) != nil || plan.ExpressionPlan.Version != 2 {
		return nil, ErrTaskPricingReceiptConflict
	}
	p := plan.ExpressionPlan
	if p.Driver != types.TaskUsageProducerKindTaskPlugin || b.Profile != plan.Profile ||
		b.PluginKey != p.PluginKey || b.Version != p.PluginVersion || b.PackageDigest != p.PackageDigest ||
		b.SourceID != plan.UsageSourceID || b.SchemaVersion != plan.UsageSchemaVersion || b.ContractDigest != plan.UsageContractDigest ||
		b.ChannelID != p.ChannelID || b.RequestID != p.RequestID || b.Action != p.Action ||
		b.Model != p.OriginalModel || b.UpstreamModel != p.MappedModel {
		return nil, ErrTaskPricingReceiptConflict
	}
	// Normalize only documented representation differences, without mutating
	// the caller. Request fact presence (including 0/false) remains significant.
	canonical := types.CloneTaskBillingPlan(plan)
	canonical.ExpressionPlan.PricingAt = p.PricingAt.UTC()
	facts, err := SubmitFacts(p.Contract, p.SubmitFacts)
	if err != nil {
		return nil, ErrTaskPricingReceiptConflict
	}
	canonical.ExpressionPlan.SubmitFacts = facts
	preimage := struct {
		Version      int                      `json:"version"`
		TaskID       int64                    `json:"task_id"`
		OperationKey string                   `json:"operation_key"`
		Source       string                   `json:"source"`
		Group        string                   `json:"group"`
		Plan         *types.TaskBillingPlan   `json:"plan"`
		Binding      *types.TaskPluginBinding `json:"binding"`
	}{1, input.TaskID, input.OperationKey, "wallet", input.Group, canonical, b}
	wire, err := common.Marshal(preimage)
	if err != nil {
		return nil, ErrTaskPricingReceiptConflict
	}
	digest := sha256.Sum256(append([]byte("max-api/task-pricing-wallet-receipt@1\n"), wire...))
	return &TaskPricingReceipt{Version: 1, Digest: hex.EncodeToString(digest[:])}, nil
}

// ValidateWalletReservationReceipt rejects missing/unknown receipts. Legacy
// funding is intentionally not upgraded or accepted via a zero-value fallback.
func ValidateWalletReservationReceipt(receipt *TaskPricingReceipt, input WalletReceiptInput) error {
	if receipt == nil || receipt.Version != 1 || !receiptDigestValid(receipt.Digest) {
		return ErrTaskPricingReceiptConflict
	}
	expected, err := PrepareWalletReservationReceipt(input)
	if err != nil || receipt.Digest != expected.Digest {
		return ErrTaskPricingReceiptConflict
	}
	return nil
}

func receiptDigestValid(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && hex.EncodeToString(decoded) == value
}
