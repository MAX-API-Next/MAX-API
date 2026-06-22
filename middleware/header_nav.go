package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/gin-gonic/gin"
)

type headerNavAccess struct {
	Enabled     bool
	RequireAuth bool
}

func getHeaderNavAccess(module string) headerNavAccess {
	defaultAccess := headerNavAccess{
		Enabled:     true,
		RequireAuth: false,
	}

	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap["HeaderNavModules"]
	rankingsRaw := common.OptionMap["RankingsModule"]
	common.OptionMapRWMutex.RUnlock()

	legacyAccess := defaultAccess

	if strings.TrimSpace(raw) != "" {
		var parsed map[string]any
		if err := common.Unmarshal([]byte(raw), &parsed); err == nil {
			legacyAccess = parseHeaderNavAccess(parsed[module], defaultAccess)
		}
	}

	if module == "rankings" && strings.TrimSpace(rankingsRaw) != "" {
		return parseHeaderNavAccessOption(rankingsRaw, legacyAccess)
	}

	return legacyAccess
}

func parseHeaderNavAccessOption(raw string, fallback headerNavAccess) headerNavAccess {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}

	var parsed any
	if err := common.Unmarshal([]byte(trimmed), &parsed); err == nil {
		return parseHeaderNavAccess(parsed, fallback)
	}

	return parseHeaderNavAccess(trimmed, fallback)
}

func parseHeaderNavAccess(raw any, fallback headerNavAccess) headerNavAccess {
	switch value := raw.(type) {
	case bool:
		return headerNavAccess{
			Enabled:     value,
			RequireAuth: fallback.RequireAuth,
		}
	case string:
		return headerNavAccess{
			Enabled:     parseHeaderNavBool(value, fallback.Enabled),
			RequireAuth: fallback.RequireAuth,
		}
	case float64:
		return headerNavAccess{
			Enabled:     parseHeaderNavBool(value, fallback.Enabled),
			RequireAuth: fallback.RequireAuth,
		}
	case map[string]any:
		access := fallback
		if enabled, ok := value["enabled"]; ok {
			access.Enabled = parseHeaderNavBool(enabled, fallback.Enabled)
		}
		if requireAuth, ok := value["requireAuth"]; ok {
			access.RequireAuth = parseHeaderNavBool(requireAuth, fallback.RequireAuth)
		}
		return access
	default:
		return fallback
	}
}

func parseHeaderNavBool(value any, fallback bool) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1":
			return true
		case "false", "0":
			return false
		default:
			return fallback
		}
	case float64:
		if v == 1 {
			return true
		}
		if v == 0 {
			return false
		}
		return fallback
	case int:
		if v == 1 {
			return true
		}
		if v == 0 {
			return false
		}
		return fallback
	default:
		return fallback
	}
}

func HeaderNavModuleAuth(module string) gin.HandlerFunc {
	return func(c *gin.Context) {
		access := getHeaderNavAccess(module)
		if !access.Enabled {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": fmt.Sprintf("%s is disabled", module),
			})
			c.Abort()
			return
		}

		if access.RequireAuth {
			UserAuth()(c)
			return
		}

		TryUserAuth()(c)
	}
}

func HeaderNavModulePublicOrUserAuth(module string) gin.HandlerFunc {
	return func(c *gin.Context) {
		access := getHeaderNavAccess(module)
		if !access.Enabled || access.RequireAuth {
			UserAuth()(c)
			return
		}

		TryUserAuth()(c)
	}
}
