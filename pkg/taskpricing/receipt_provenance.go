// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Lifecycle reference: QuantumNous/new-api 706144769e relay/relay_task.go.
// Independent MAX provenance contract; no upstream funding implementation.
package taskpricing

import "strings"

// ReceiptProvenance records a host-selected policy for one task reservation.
// It must be retained independently of the mutable task snapshot, atomically
// with the original intent/receipt, and never inferred from a missing receipt.
// Its version is separate from the receipt hash version. This pure type has
// no production writer/migration yet; it is not a signature or a funds permit.
type ReceiptProvenance struct {
	Version      int                `json:"version"`
	Requirement  ReceiptRequirement `json:"requirement"`
	TaskID       int64              `json:"task_id"`
	OperationKey string             `json:"operation_key"`
}

// PrepareReceiptProvenance validates an explicit host decision at creation or
// an audited legacy migration. Recovery must read the retained original, not
// call this factory to regenerate evidence from the current task. Legacy
// compatibility leaves the caller's existing ownership/funding checks intact.
func PrepareReceiptProvenance(requirement ReceiptRequirement, input WalletReceiptInput) (*ReceiptProvenance, error) {
	p := &ReceiptProvenance{Version: 1, Requirement: requirement, TaskID: input.TaskID, OperationKey: input.OperationKey}
	if !receiptProvenanceIdentityValid(p) {
		return nil, ErrTaskPricingReceiptConflict
	}
	switch requirement {
	case ReceiptWalletV1:
		if _, err := PrepareWalletReservationReceipt(input); err != nil {
			return nil, err
		}
	case ReceiptLegacy:
		if err := ValidateReceiptCompatibility(requirement, nil, input); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// ValidateReceiptProvenance checks an original host-owned record against the
// independently loaded intent identity before applying its compatibility rule.
// A nil/unknown record is unproved, never legacy. Loading/decoding errors must
// be propagated by the consumer; this function neither repairs nor persists it.
func ValidateReceiptProvenance(provenance *ReceiptProvenance, receipt *TaskPricingReceipt, input WalletReceiptInput) error {
	if !receiptProvenanceIdentityValid(provenance) || provenance.TaskID != input.TaskID || provenance.OperationKey != input.OperationKey {
		return ErrTaskPricingReceiptConflict
	}
	return ValidateReceiptCompatibility(provenance.Requirement, receipt, input)
}

func receiptProvenanceIdentityValid(p *ReceiptProvenance) bool {
	if p == nil || p.Version != 1 || p.TaskID <= 0 || p.Requirement != ReceiptLegacy && p.Requirement != ReceiptWalletV1 {
		return false
	}
	request, ok := strings.CutPrefix(p.OperationKey, "request:")
	if !ok {
		return false
	}
	request, ok = strings.CutSuffix(request, ":pre-consume")
	return ok && len(request) > 0 && len(request) <= 64 && request == strings.TrimSpace(request)
}
