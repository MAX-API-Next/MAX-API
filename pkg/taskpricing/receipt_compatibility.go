// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

// ReceiptRequirement must come from trusted operation provenance, never from
// receipt presence or a zero amount. The zero value is deliberately invalid.
// No production writer/consumer persists or supplies this requirement yet.
type ReceiptRequirement string

const (
	ReceiptLegacy   ReceiptRequirement = "legacy"
	ReceiptWalletV1 ReceiptRequirement = "wallet_receipt_v1"
)

// ValidateReceiptCompatibility is the pure policy shared by future creation,
// recovery, replay and reconciliation consumers. Legacy success leaves every
// old financial/ownership validator mandatory. New-profile success proves only
// equality to the separately retained original; neither result permits funds
// or dispatch. Unknown data is rejected, never rewritten or downgraded.
func ValidateReceiptCompatibility(requirement ReceiptRequirement, receipt *TaskPricingReceipt, input WalletReceiptInput) error {
	switch requirement {
	case ReceiptWalletV1:
		return ValidateWalletReservationReceipt(receipt, input)
	case ReceiptLegacy:
		if receipt != nil || input.Plan != nil && (input.Plan.Profile != "" || input.Plan.ExpressionPlan != nil || input.Plan.Source == "task_expression") ||
			input.Binding != nil && (input.Binding.Profile != "" || input.Binding.Action != "" || input.Binding.PhaseApprovalID != "") {
			return ErrTaskPricingReceiptConflict
		}
		return nil
	default:
		return ErrTaskPricingReceiptConflict
	}
}
