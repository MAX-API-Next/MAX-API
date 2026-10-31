/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package taskpricing

import (
	"fmt"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/billingexpr"
	"github.com/MAX-API-Next/MAX-API/types"
)

// PreviewInput is a host-owned pure-call context, not an HTTP request DTO.
// Current must come from the authority at the eventual caller boundary; this
// function cannot prove a DB/cache read is fresh. No provider/task/fund writes
// occur here. Inputs must be immutable while the call is running.
type PreviewInput struct {
	Profile          string
	Prices           types.TaskPricingMaps
	Source           Source
	OriginalModel    string
	MappedModel      string
	Contract         types.TaskFactContract
	Expected         Guards
	Current          Guards
	Submit           types.TaskPricingFacts
	Actual           types.TaskPricingFacts // nil: final usage not yet available
	Request          billingexpr.RequestInput
	PricingAt        time.Time
	QuotaPerUnit     float64
	GroupRatio       float64
	ReservationFloor int
}

// PreviewResult describes prices, never actual accounting effects. A positive
// FinalMinusReserved is an uncovered funding gap, not permission to debit it.
// A nil Final distinguishes unavailable usage from an explicit zero target.
type PreviewResult struct {
	Profile            string                 `json:"profile"`
	Source             Source                 `json:"source"`
	Selection          Selection              `json:"selection"`
	ContractDigest     string                 `json:"contract_digest"`
	Estimate           billingexpr.TaskQuote  `json:"estimate"`
	ReservedQuota      int                    `json:"reserved_quota"`
	Final              *billingexpr.TaskQuote `json:"final"`
	FinalMinusReserved *int                   `json:"final_minus_reserved"`
}

// ValidateExpressionContract checks all static usage references, including
// branches the compiler may optimize away. It does not smoke-test every possible
// fact combination; the shared core also validates the actual computed price.
func ValidateExpressionContract(expression string, contract types.TaskFactContract) error {
	contract, err := NormalizeContract(contract)
	if err != nil {
		return err
	}
	keys, err := billingexpr.TaskUsageKeys(expression)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, declared := contract.UsageSchema[key]; !declared {
			return fmt.Errorf("task price references undeclared fact %q", key)
		}
	}
	return nil
}

// Preview composes the existing source resolver, contract validator and one
// billing expression core. It applies the floor only to the complete estimate;
// the final target uses actual facts with frozen request dimensions and terms.
func Preview(input PreviewInput) (PreviewResult, error) {
	if input.Profile != NewAPITask {
		return PreviewResult{}, fmt.Errorf("explicit task expression profile required")
	}
	if input.ReservationFloor < 0 || input.ReservationFloor > common.MaxQuota {
		return PreviewResult{}, fmt.Errorf("invalid task reservation floor")
	}
	digest, err := ContractDigest(input.Contract)
	if err != nil {
		return PreviewResult{}, err
	}
	if digest != input.Current.ContractDigest {
		return PreviewResult{}, fmt.Errorf("task contract does not match current quote context")
	}
	if err := CheckGuards(input.Expected, input.Current); err != nil {
		return PreviewResult{}, err
	}
	selected, err := SelectExpression(input.Prices, input.Source, input.OriginalModel, input.MappedModel)
	if err != nil {
		return PreviewResult{}, err
	}
	if err := ValidateExpressionContract(selected.Expression, input.Contract); err != nil {
		return PreviewResult{}, err
	}
	frozen, err := SubmitFacts(input.Contract, input.Submit)
	if err != nil {
		return PreviewResult{}, err
	}
	quote := func(facts types.TaskPricingFacts) (billingexpr.TaskQuote, error) {
		return billingexpr.QuoteTaskExpression(billingexpr.TaskQuoteInput{Profile: input.Profile,
			Expression: selected.Expression, Usage: facts, Request: input.Request, PricingAt: input.PricingAt,
			QuotaPerUnit: input.QuotaPerUnit, GroupRatio: input.GroupRatio})
	}
	estimate, err := quote(frozen)
	if err != nil {
		return PreviewResult{}, err
	}
	result := PreviewResult{Profile: NewAPITask, Source: input.Source, Selection: selected, ContractDigest: digest,
		Estimate: estimate, ReservedQuota: max(input.ReservationFloor, estimate.QuotaAfterGroup)}
	if input.Actual == nil {
		return result, nil
	}
	actual, err := CompleteFacts(input.Contract, frozen, input.Actual)
	if err != nil {
		return PreviewResult{}, err
	}
	final, err := quote(actual)
	if err != nil {
		return PreviewResult{}, err
	}
	delta := final.QuotaAfterGroup - result.ReservedQuota
	result.Final = &final
	result.FinalMinusReserved = &delta
	return result, nil
}
