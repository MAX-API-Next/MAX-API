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
	"encoding/json"
	"fmt"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

// DecodePricingChange checks transport shape only. Price keys, expression
// compilation, authorization and transactional guards belong to the P1 writer.
// In particular null reset must not silently become an omitted/false reset.
func DecodePricingChange(data []byte) (types.ModelPricingChange, error) {
	var out types.ModelPricingChange
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return out, err
	}
	allowed := map[string]string{"model_name": "string", "expected_version": "string", "pricing": "object", "reset": "boolean", "expected_contract_digest": "string", "expected_binding_version": "string"}
	for name, raw := range fields {
		kind, ok := allowed[name]
		if !ok || common.GetJsonType(raw) != kind {
			return out, fmt.Errorf("invalid pricing change field %q", name)
		}
	}
	for _, name := range []string{"model_name", "expected_version", "pricing"} {
		if _, ok := fields[name]; !ok {
			return out, fmt.Errorf("missing pricing change field %q", name)
		}
	}
	if err := common.Unmarshal(data, &out); err != nil {
		return out, err
	}
	if out.ModelName == "" || out.ExpectedVersion == "" {
		return out, fmt.Errorf("empty model or expected price version")
	}
	for _, guard := range []string{"expected_contract_digest", "expected_binding_version"} {
		if raw, exists := fields[guard]; exists {
			var value string
			if err := common.Unmarshal(raw, &value); err != nil || value == "" {
				return out, fmt.Errorf("empty pricing guard %q", guard)
			}
		}
	}
	return out, nil
}
