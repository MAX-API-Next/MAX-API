// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskplugin

import (
	"math"
	"slices"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/MAX-API-Next/MAX-API/types"
)

// NormalizeDriverFacts preserves API v1 scalar extension facts as well as the
// selected model schema. Official Hailuo/Seedance hooks deliberately retain
// legacy fields absent from a narrower profile. A profile is not a whitelist
// that may discard these facts or turn the whole observation into missing.
// This is not the separate phase-vetted pricing contract or paid admission.
func NormalizeDriverFacts(schema map[string]UsageField, facts types.TaskPricingFacts, completion bool) (types.TaskPricingFacts, error) {
	if err := ValidateUsageSchema(schema); err != nil {
		return nil, ErrPluginHook
	}
	if facts == nil {
		return nil, nil
	}
	out := make(types.TaskPricingFacts, len(facts))
	for key, value := range facts {
		field, declared := schema[key]
		if declared && (field.Type == "enum" || len(field.Enum) > 0) {
			v, ok := value.(string)
			if !ok || !slices.Contains(field.Enum, v) {
				return nil, ErrPluginHook
			}
			out[key] = v
			continue
		}
		if declared && field.Type == "boolean" {
			v, ok := value.(bool)
			if !ok {
				return nil, ErrPluginHook
			}
			out[key] = v
			continue
		}
		var numeric bool
		var number float64
		switch v := value.(type) {
		case float64:
			number, numeric = v, true
		case int:
			number, numeric = float64(v), true
		case int64:
			number, numeric = float64(v), true
		}
		limit, canonical := canonicalFactLimit(key)
		if declared || canonical {
			if !numeric {
				return nil, ErrPluginHook
			}
		} else if !numeric {
			// The task expression core consumes scalars only. Unknown structured
			// extension data is not a billable fact in this SDK subset.
			switch value.(type) {
			case string, bool:
				out[key] = value
				continue
			default:
				return nil, ErrPluginSDK
			}
		}
		if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
			return nil, ErrPluginHook
		}
		if declared && (field.Unit == "token" || field.Unit == "credit") {
			out[key] = math.Min(number, float64(common.MaxQuota))
			continue
		}
		if declared {
			limit = 3600
			if field.Unit == "count" {
				limit = dto.MaxImageN
			}
		}
		if !declared && !canonical {
			if completion {
				switch key {
				case "upstreamUnits", "completionTokens", "totalTokens":
					quota, _ := common.QuotaFromFloatChecked(number)
					number = float64(quota)
				}
				out[key] = number
				continue
			}
			limit = 3600
		}
		if number > float64(limit) {
			return nil, ErrPluginHook
		}
		out[key] = number
	}
	return out, nil
}

func canonicalFactLimit(key string) (int, bool) {
	key = strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
	switch key {
	case "duration", "durationseconds", "second", "seconds":
		return 3600, true
	case "n", "count", "imagecount", "samplecount", "batchcount", "numimages":
		return dto.MaxImageN, true
	}
	return 0, false
}
