package middleware

import (
	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/service/authz"
	"github.com/gin-gonic/gin"
)

// AuthzShadow evaluates the fine-grained permission without changing the
// legacy route decision. This allows route coverage and policy mismatches to
// be observed before enforcement is enabled for a route.
func AuthzShadow(permission authz.Permission) func(*gin.Context) {
	return func(c *gin.Context) {
		userID := c.GetInt("id")
		role := c.GetInt("role")
		decision := authz.Evaluate(userID, role, permission)
		if decision.Err != nil {
			common.SysLog("authz shadow evaluation failed: " + decision.Err.Error())
		} else if !decision.Allowed {
			common.SysLog("authz shadow denied legacy-allowed request: user=" + c.GetString("username") + " resource=" + permission.Resource + " action=" + permission.Action + " source=" + decision.Source)
		}
		c.Set("authz_shadow_allowed", decision.Allowed)
		c.Set("authz_shadow_source", decision.Source)
		c.Next()
	}
}
