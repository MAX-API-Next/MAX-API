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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/types"
)

// NormalizeContract copies the semantic portion. Cosmetic labels and enum
// ordering are excluded; units, bounds, presence and phase are included. This
// digest proves neither package identity nor channel binding/version.
func NormalizeContract(c types.TaskFactContract) (types.TaskFactContract, error) {
	out := types.TaskFactContract{Version: c.Version, UsageSchema: map[string]types.TaskUsageField{}, FieldPolicy: map[string]types.TaskFactPolicy{}}
	if c.Version != 1 || len(c.UsageSchema) == 0 || len(c.UsageSchema) != len(c.FieldPolicy) {
		return out, fmt.Errorf("invalid task fact contract version or fields")
	}
	schema := make(map[string]taskplugin.UsageField, len(c.UsageSchema))
	for name, field := range c.UsageSchema {
		if field.Type == "" && len(field.Enum) > 0 {
			field.Type = "enum"
		}
		enum := slices.Clone(field.Enum)
		slices.Sort(enum)
		schema[name] = taskplugin.UsageField{Type: field.Type, Unit: field.Unit, Enum: enum}
		out.UsageSchema[name] = types.TaskUsageField{Type: field.Type, Unit: field.Unit, Enum: enum}
		p, ok := c.FieldPolicy[name]
		if !ok || (p.Phase != "request" && p.Phase != "actual") {
			return out, fmt.Errorf("missing or invalid phase for %q", name)
		}
		if field.Type != "number" && (p.Min != nil || p.Max != nil || p.Integer) {
			return out, fmt.Errorf("non-numeric bounds for %q", name)
		}
		for _, bound := range []*float64{p.Min, p.Max} {
			if bound != nil && (math.IsNaN(*bound) || math.IsInf(*bound, 0) || *bound < 0) {
				return out, fmt.Errorf("invalid bound for %q", name)
			}
		}
		if p.Min != nil && p.Max != nil && *p.Min > *p.Max {
			return out, fmt.Errorf("reversed bounds for %q", name)
		}
		if p.Min != nil {
			v := *p.Min
			p.Min = &v
		}
		if p.Max != nil {
			v := *p.Max
			p.Max = &v
		}
		out.FieldPolicy[name] = p
	}
	if err := taskplugin.ValidateUsageSchema(schema); err != nil {
		return out, err
	}
	return out, nil
}

func ContractDigest(c types.TaskFactContract) (string, error) {
	c, err := NormalizeContract(c)
	if err != nil {
		return "", err
	}
	b, err := common.Marshal(c)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// SubmitFacts returns a new scalar-only map, suitable for freezing. Actual
// fields here are estimates; CompleteFacts must not treat them as final usage.
func SubmitFacts(c types.TaskFactContract, facts types.TaskPricingFacts) (types.TaskPricingFacts, error) {
	c, err := NormalizeContract(c)
	if err != nil {
		return nil, err
	}
	return validateFacts(c, facts, true)
}

// CompleteFacts inherits only frozen request facts. Missing required actual
// usage is an error, even when an estimate was present at submit. Undeclared
// facts are rejected: plugin legacy extras require explicit host adaptation.
func CompleteFacts(c types.TaskFactContract, frozen, actual types.TaskPricingFacts) (types.TaskPricingFacts, error) {
	c, err := NormalizeContract(c)
	if err != nil {
		return nil, err
	}
	frozen, err = validateFacts(c, frozen, true)
	if err != nil {
		return nil, err
	}
	actual, err = validateFacts(c, actual, false)
	if err != nil {
		return nil, err
	}
	out := types.TaskPricingFacts{}
	for name, p := range c.FieldPolicy {
		if p.Phase != "request" {
			continue
		}
		v, exists := frozen[name]
		if replacement, present := actual[name]; present && (!exists || replacement != v) {
			return nil, fmt.Errorf("frozen request fact changed: %q", name)
		}
		if exists {
			out[name] = v
		}
	}
	for name, value := range actual {
		out[name] = value
	}
	return out, nil
}

func validateFacts(c types.TaskFactContract, facts types.TaskPricingFacts, submit bool) (types.TaskPricingFacts, error) {
	out := types.TaskPricingFacts{}
	for name, value := range facts {
		field, exists := c.UsageSchema[name]
		if !exists {
			return nil, fmt.Errorf("undeclared task fact %q", name)
		}
		p := c.FieldPolicy[name]
		switch field.Type {
		case "number":
			n, ok := number(value)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1<<53-1 || (p.Min != nil && n < *p.Min) || (p.Max != nil && n > *p.Max) || (p.Integer && math.Trunc(n) != n) {
				return nil, fmt.Errorf("invalid numeric task fact %q", name)
			}
			out[name] = n
		case "boolean":
			v, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("invalid boolean task fact %q", name)
			}
			out[name] = v
		case "enum":
			v, ok := value.(string)
			if !ok || !slices.Contains(field.Enum, v) {
				return nil, fmt.Errorf("invalid enum task fact %q", name)
			}
			out[name] = v
		}
	}
	for name, p := range c.FieldPolicy {
		if p.Required && ((submit && p.Phase == "request") || (!submit && p.Phase == "actual")) {
			if _, present := out[name]; !present {
				return nil, fmt.Errorf("missing required task fact %q", name)
			}
		}
	}
	return out, nil
}

func number(value any) (float64, bool) {
	if n, ok := value.(json.Number); ok {
		v, err := n.Float64()
		return v, err == nil
	}
	if value == nil {
		return 0, false
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		return v.Float(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := v.Int()
		if n < -(1<<53-1) || n > 1<<53-1 {
			return 0, false
		}
		return float64(n), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n := v.Uint()
		if n > 1<<53-1 {
			return 0, false
		}
		return float64(n), true
	}
	return 0, false
}
