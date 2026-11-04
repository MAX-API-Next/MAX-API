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

package types

// TaskPricingMaps follows New API's billing_setting maps. It is a value passed
// to pure contract code, not another price store or a registered Option group.
type TaskPricingMaps struct {
	BillingMode       map[string]string `json:"billing_mode"`
	BillingExpr       map[string]string `json:"billing_expr"`
	PluginBillingExpr map[string]string `json:"plugin_billing_expr"`
}

// ModelPricingValues uses Option names (e.g. ModelPrice and
// billing_setting.plugin_billing_expr). In this model projection the latter is
// plugin-key -> expression; in TaskPricingMaps it is plugin-key::model -> text.
type ModelPricingValues map[string]any

// ModelPricingChange preserves the upstream PATCH shape; MAX task contexts
// additionally need contract/binding guards, checked by the model pricing writer.
type ModelPricingChange struct {
	ModelName              string             `json:"model_name"`
	ExpectedVersion        string             `json:"expected_version"`
	Pricing                ModelPricingValues `json:"pricing"`
	Reset                  *bool              `json:"reset,omitempty"`
	ExpectedContractDigest string             `json:"expected_contract_digest,omitempty"`
	ExpectedBindingVersion string             `json:"expected_binding_version,omitempty"`
}

// TaskUsageField retains New API's wire names, including omitted type on enums.
// Labels are presentation metadata and do not contribute to a semantic digest.
type TaskUsageField struct {
	Type        string                       `json:"type,omitempty"`
	Unit        string                       `json:"unit,omitempty"`
	Enum        []string                     `json:"enum,omitempty"`
	UnitLabel   map[string]string            `json:"unitLabel,omitempty"`
	Description map[string]string            `json:"description,omitempty"`
	EnumLabels  map[string]map[string]string `json:"enumLabels,omitempty"`
}

// TaskFactPolicy supplements the plugin schema. Request facts are frozen at
// submit; required actual facts must be supplied again at completion. Numeric
// facts are always finite and non-negative. No missing value defaults to zero.
type TaskFactPolicy struct {
	Phase    string   `json:"phase"`
	Required bool     `json:"required"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Integer  bool     `json:"integer,omitempty"`
}

type TaskFactContract struct {
	Version     int                       `json:"version"`
	UsageSchema map[string]TaskUsageField `json:"usage_schema"`
	FieldPolicy map[string]TaskFactPolicy `json:"field_policy"`
}

type TaskPricingFacts map[string]any
