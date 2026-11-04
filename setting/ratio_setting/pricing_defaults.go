// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package ratio_setting

import (
	"maps"
	"strings"
)

// GetDefaultPricingMaps returns compiled defaults, never a node's live cache.
func GetDefaultPricingMaps() map[string]map[string]float64 {
	return map[string]map[string]float64{
		"ModelPrice": maps.Clone(defaultModelPrice), "ModelRatio": maps.Clone(defaultModelRatio),
		"CompletionRatio": maps.Clone(defaultCompletionRatio), "CacheRatio": maps.Clone(defaultCacheRatio),
		"CreateCacheRatio": maps.Clone(defaultCreateCacheRatio), "ImageRatio": maps.Clone(defaultImageRatio),
		"AudioRatio": maps.Clone(defaultAudioRatio), "AudioCompletionRatio": maps.Clone(defaultAudioCompletionRatio),
	}
}

// ResolveConfiguredCompletionRatio shares the runtime's precedence without
// reading its mutable maps. The caller supplies an authoritative snapshot.
func ResolveConfiguredCompletionRatio(name string, configured map[string]float64) float64 {
	name = FormatMatchingModelName(name)
	if value, ok := configured[name]; ok && strings.Contains(name, "/") {
		return value
	}
	fallback, locked := getHardcodedCompletionModelRatio(name)
	if locked {
		return fallback
	}
	if value, ok := configured[name]; ok {
		return value
	}
	return fallback
}

func GetDefaultGroupRatioCopy() map[string]float64 { return maps.Clone(defaultGroupRatio) }
