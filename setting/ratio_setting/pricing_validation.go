package ratio_setting

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

const (
	maxPricingMapJSONBytes = 2 * 1024 * 1024
	maxPricingMapEntries   = 20000
	maxPricingMapKeyRunes  = 256
)

type pricingMapValidationOptions struct {
	name         string
	normalizeKey func(string) string
}

func ValidatePricingMapJSONString(jsonStr string) error {
	return validatePricingMapJSONString(jsonStr, pricingMapValidationOptions{
		name: "pricing configuration",
	})
}

func ValidateModelRatioJSONString(jsonStr string) error {
	return validatePricingMapJSONString(jsonStr, pricingMapValidationOptions{
		name:         "model ratio configuration",
		normalizeKey: FormatMatchingModelName,
	})
}

func ValidateCompletionRatioJSONString(jsonStr string) error {
	return validatePricingMapJSONString(jsonStr, pricingMapValidationOptions{
		name:         "completion ratio configuration",
		normalizeKey: FormatMatchingModelName,
	})
}

func validatePricingMapJSONString(jsonStr string, opts pricingMapValidationOptions) error {
	_, err := normalizePricingMapJSONString(jsonStr, opts)
	return err
}

func normalizePricingMapJSONString(jsonStr string, opts pricingMapValidationOptions) (string, error) {
	if opts.name == "" {
		opts.name = "pricing configuration"
	}
	if len(jsonStr) > maxPricingMapJSONBytes {
		return "", fmt.Errorf("%s is too large: %d bytes exceeds limit %d", opts.name, len(jsonStr), maxPricingMapJSONBytes)
	}
	var values map[string]*float64
	if err := common.UnmarshalJsonStr(jsonStr, &values); err != nil {
		return "", err
	}
	if values == nil {
		return "", fmt.Errorf("%s must be a JSON object", opts.name)
	}
	if len(values) > maxPricingMapEntries {
		return "", fmt.Errorf("%s has too many entries: %d exceeds limit %d", opts.name, len(values), maxPricingMapEntries)
	}
	var normalizedValues map[string]float64
	var normalizedKeys map[string]string
	if opts.normalizeKey != nil {
		normalizedValues = make(map[string]float64, len(values))
		normalizedKeys = make(map[string]string, len(values))
	}
	for modelName, value := range values {
		trimmedName := strings.TrimSpace(modelName)
		if trimmedName == "" {
			return "", fmt.Errorf("%s contains an empty model name", opts.name)
		}
		if utf8.RuneCountInString(modelName) > maxPricingMapKeyRunes {
			return "", fmt.Errorf("%s model name %q exceeds %d characters", opts.name, modelName, maxPricingMapKeyRunes)
		}
		normalizedName := trimmedName
		if opts.normalizeKey != nil {
			normalizedName = opts.normalizeKey(trimmedName)
			if existing, ok := normalizedKeys[normalizedName]; ok && existing != modelName {
				return "", fmt.Errorf("%s contains duplicate normalized model names %q and %q", opts.name, existing, modelName)
			}
			normalizedKeys[normalizedName] = modelName
		}
		if value == nil {
			return "", fmt.Errorf("%s value for model %q must not be null", opts.name, modelName)
		}
		if *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
			return "", fmt.Errorf("%s value for model %q must be finite and non-negative", opts.name, modelName)
		}
		if normalizedValues != nil {
			normalizedValues[normalizedName] = *value
		}
	}
	if normalizedValues == nil {
		return jsonStr, nil
	}
	normalizedJSON, err := common.Marshal(normalizedValues)
	if err != nil {
		return "", err
	}
	return string(normalizedJSON), nil
}

func loadPricingMap(m *types.RWMap[string, float64], jsonStr string) error {
	return loadPricingMapWithOptions(m, jsonStr, pricingMapValidationOptions{
		name: "pricing configuration",
	})
}

func loadPricingMapWithOptions(m *types.RWMap[string, float64], jsonStr string, opts pricingMapValidationOptions) error {
	normalizedJSON, err := normalizePricingMapJSONString(jsonStr, opts)
	if err != nil {
		return err
	}
	return types.LoadFromJsonStringWithCallback(m, normalizedJSON, InvalidateExposedDataCache)
}
