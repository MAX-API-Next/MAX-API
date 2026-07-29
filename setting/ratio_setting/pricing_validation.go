package ratio_setting

import (
	"fmt"
	"math"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

func ValidatePricingMapJSONString(jsonStr string) error {
	var values map[string]*float64
	if err := common.UnmarshalJsonStr(jsonStr, &values); err != nil {
		return err
	}
	if values == nil {
		return fmt.Errorf("pricing configuration must be a JSON object")
	}
	for modelName, value := range values {
		if value == nil {
			return fmt.Errorf("pricing value for model %q must not be null", modelName)
		}
		if *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
			return fmt.Errorf("pricing value for model %q must be finite and non-negative", modelName)
		}
	}
	return nil
}

func loadPricingMap(m *types.RWMap[string, float64], jsonStr string) error {
	if err := ValidatePricingMapJSONString(jsonStr); err != nil {
		return err
	}
	return types.LoadFromJsonStringWithCallback(m, jsonStr, InvalidateExposedDataCache)
}
