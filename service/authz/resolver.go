package authz

import (
	"errors"
	"fmt"
	"sync"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"gorm.io/gorm"
)

var ErrUnknownPermission = errors.New("unknown authorization permission")

// Decision is intentionally richer than a bool so shadow enforcement can
// distinguish a normal deny from a fail-closed database or catalog error.
type Decision struct {
	Allowed bool
	Source  string
	Err     error
}

var invalidationMu sync.RWMutex
var invalidationHook func(int)

// SetInvalidationHookForTest installs a test-only observation seam. The
// initial shadow phase does not cache authorization decisions, so database
// reads remain authoritative and cannot serve stale revocations.
func SetInvalidationHookForTest(hook func(int)) func() {
	invalidationMu.Lock()
	previous := invalidationHook
	invalidationHook = hook
	invalidationMu.Unlock()
	return func() {
		invalidationMu.Lock()
		invalidationHook = previous
		invalidationMu.Unlock()
	}
}

// InvalidateUser is the cache invalidation seam for future local/distributed
// authorization caches. The first rollout deliberately reads policy rows
// directly, which is safer than introducing a stale permission cache.
func InvalidateUser(userID int) {
	if userID <= 0 {
		return
	}
	invalidationMu.RLock()
	hook := invalidationHook
	invalidationMu.RUnlock()
	if hook != nil {
		hook(userID)
	}
}

func Can(userID int, systemRole int, permission Permission) bool {
	return Evaluate(userID, systemRole, permission).Allowed
}

func Evaluate(userID int, systemRole int, permission Permission) Decision {
	if !isKnownPermission(permission) {
		return Decision{Source: "unknown", Err: fmt.Errorf("%w: %s.%s", ErrUnknownPermission, permission.Resource, permission.Action)}
	}
	if systemRole >= common.RoleRootUser {
		return Decision{Allowed: true, Source: "root"}
	}
	if systemRole < common.RoleAdminUser {
		return Decision{Allowed: false, Source: "role"}
	}

	entry, _ := actionDefinition(permission)
	if model.DB == nil {
		return Decision{Source: "error", Err: errors.New("authorization database is not initialized")}
	}

	var override model.AuthzUserOverride
	result := model.DB.Where("user_id = ? AND resource = ? AND action = ?", userID, permission.Resource, permission.Action).Take(&override)
	if result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return Decision{Source: "error", Err: result.Error}
	}
	if result.RowsAffected > 0 {
		if override.Allowed {
			return Decision{Allowed: true, Source: "user_override"}
		}
		return Decision{Allowed: false, Source: "user_override"}
	}
	return Decision{Allowed: entry.DefaultAdmin, Source: "role"}
}

func Capabilities(userID int, systemRole int) PermissionsMap {
	result := roleGrants(systemRole)
	if systemRole >= common.RoleRootUser || systemRole < common.RoleAdminUser || userID <= 0 {
		return result
	}
	if model.DB == nil {
		return make(PermissionsMap)
	}

	var overrides []model.AuthzUserOverride
	if err := model.DB.Where("user_id = ?", userID).Find(&overrides).Error; err != nil {
		// A capability response must fail closed when policy storage is not
		// readable; callers should not mistake a partial matrix for authority.
		return make(PermissionsMap)
	}
	for _, override := range overrides {
		if _, ok := actionDefinition(Permission{Resource: override.Resource, Action: override.Action}); !ok {
			continue
		}
		if _, ok := result[override.Resource]; !ok {
			result[override.Resource] = map[string]bool{}
		}
		result[override.Resource][override.Action] = override.Allowed
	}
	return result
}
