package service

import (
	"strings"

	"github.com/MAX-API-Next/MAX-API/constant"
)

func CoverTaskActionToModelName(platform constant.TaskPlatform, action string) string {
	return strings.ToLower(string(platform)) + "_" + strings.ToLower(action)
}
