package vertex

import "github.com/MAX-API-Next/MAX-API/common"

func configuredRegion(value any) (string, bool) {
	region, ok := value.(string)
	return region, ok && region != ""
}

func GetModelRegion(other string, localModelName string) string {
	// if other is json string
	if common.IsJsonObject(other) {
		m, err := common.StrToMap(other)
		if err != nil {
			return other // return original if parsing fails
		}
		if region, ok := configuredRegion(m[localModelName]); ok {
			return region
		}
		if region, ok := configuredRegion(m["default"]); ok {
			return region
		}
		return "global"
	}
	return other
}
