// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package taskpricing

import "github.com/MAX-API-Next/MAX-API/types"

// KnownContract is the host's small, explicit configuration-sample contract.
// Schema/phase references: New API 4193734a, plugins/tasks/{hailuo,kling,doubao}/plugin.js.
// It does not prove driver activation, a reservation upper bound or paid readiness.
// No fixture prices or synthetic credit/token caps are used in this registry.
func KnownContract(family, model string) (types.TaskFactContract, bool) {
	c := types.TaskFactContract{Version: 1, UsageSchema: map[string]types.TaskUsageField{}, FieldPolicy: map[string]types.TaskFactPolicy{}}
	number := func(name, unit, phase string, integer bool) {
		c.UsageSchema[name] = types.TaskUsageField{Type: "number", Unit: unit}
		c.FieldPolicy[name] = types.TaskFactPolicy{Phase: phase, Required: true, Integer: integer}
	}
	enum := func(name string, values ...string) {
		c.UsageSchema[name] = types.TaskUsageField{Enum: values}
		c.FieldPolicy[name] = types.TaskFactPolicy{Phase: "request", Required: true}
	}
	switch {
	case family == "hailuo" && model == "MiniMax-H3":
		number("seconds", "second", "actual", false)
		// Existing MAX H3 contract: output 4..15s, input video total <=15s.
		min, max := 4.0, 15.0
		p := c.FieldPolicy["seconds"]
		p.Min = &min
		p.Max = &max
		c.FieldPolicy["seconds"] = p
		enum("resolution", "768P", "2K")
		number("input_images", "count", "actual", true)
		number("input_video_seconds", "second", "actual", false)
		p = c.FieldPolicy["input_video_seconds"]
		p.Max = &max
		c.FieldPolicy["input_video_seconds"] = p
	case family == "kling" && (model == "kling-v1" || model == "kling-v1-6" || model == "kling-v2-master"):
		number("units", "credit", "actual", false)
	case family == "doubao" && model == "doubao-seedance-1-5-pro-251215":
		number("tokens", "token", "actual", true)
		min := 1.0
		p := c.FieldPolicy["tokens"]
		p.Min = &min
		c.FieldPolicy["tokens"] = p
		enum("resolution", "480p", "720p", "1080p")
		c.UsageSchema["generate_audio"] = types.TaskUsageField{Type: "boolean"}
		c.FieldPolicy["generate_audio"] = types.TaskFactPolicy{Phase: "request", Required: true}
	case family == "doubao" && model == "doubao-seedance-2-0-260128":
		number("tokens", "token", "actual", true)
		min := 1.0
		p := c.FieldPolicy["tokens"]
		p.Min = &min
		c.FieldPolicy["tokens"] = p
		enum("resolution", "480p", "720p", "1080p", "4k")
		enum("video_input", "none", "video")
	default:
		return types.TaskFactContract{}, false
	}
	return c, true
}
