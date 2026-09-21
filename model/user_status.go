package model

import (
	"context"
	"errors"

	"github.com/MAX-API-Next/MAX-API/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrUserStatusForbidden = errors.New("user status change not permitted")
	ErrUserStatusConflict  = errors.New("user status changed; refresh before retrying")
)

type AdminUserStatusChange struct {
	Username       string
	PreviousStatus int
	Status         int
	Changed        bool
}

// SetUserStatusByAdmin changes only account status, never balances, credentials,
// subscriptions or task records. Re-check both identities inside the transaction;
// a controller's cached role or a target's list-page snapshot is not authority.
func SetUserStatusByAdmin(ctx context.Context, actorID, targetID, expectedStatus, status int) (AdminUserStatusChange, error) {
	var result AdminUserStatusChange
	if actorID <= 0 || targetID <= 0 || actorID == targetID {
		return result, ErrUserStatusForbidden
	}
	if (status != common.UserStatusEnabled && status != common.UserStatusDisabled) ||
		(expectedStatus != common.UserStatusEnabled && expectedStatus != common.UserStatusDisabled) {
		return result, ErrUserStatusConflict
	}
	var cacheTask CacheInvalidationTask
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// Keep advisory-lock release independent of client cancellation. Only the
	// transaction uses the request context, so a cancelled request cannot leave
	// a session-level MySQL/PostgreSQL lock on a pooled connection.
	err := withUserOAuthIdentityMutationLock(DB, func(conn *gorm.DB) error {
		return conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var users []User
			// Deterministic lock order also protects against concurrent role changes.
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Select("id", "username", "role", "status").
				Where("id IN ?", []int{actorID, targetID}).Order("id").Find(&users).Error; err != nil {
				return err
			}
			var actor, target *User
			for i := range users {
				if users[i].Id == actorID {
					actor = &users[i]
				}
				if users[i].Id == targetID {
					target = &users[i]
				}
			}
			if actor == nil || actor.Status != common.UserStatusEnabled || actor.Role < common.RoleAdminUser {
				return ErrUserStatusForbidden
			}
			if target == nil {
				return gorm.ErrRecordNotFound
			}
			if target.Role >= common.RoleRootUser || actor.Role <= target.Role {
				return ErrUserStatusForbidden
			}
			result = AdminUserStatusChange{Username: target.Username, PreviousStatus: target.Status, Status: target.Status}
			if target.Status == status {
				return nil
			}
			if target.Status != expectedStatus {
				return ErrUserStatusConflict
			}
			update := tx.Model(&User{}).
				Where("id = ? AND status = ? AND role = ?", targetID, expectedStatus, target.Role).
				Update("status", status)
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return ErrUserStatusConflict
			}
			var err error
			cacheTask, err = stageUserCacheInvalidationTx(tx, targetID, false)
			if err != nil {
				return err
			}
			result.Status = status
			result.Changed = true
			return nil
		})
	})
	if err != nil {
		// A commit/connection-release error can have an unknown outcome. Do not
		// automatically repeat it or return a confirmed-success projection.
		return AdminUserStatusChange{}, err
	}
	// TokenAuth and session auth check the user cache. Its durable pending-task
	// fence prevents stale enabled snapshots being trusted if Redis is unavailable.
	dispatchStagedCacheInvalidation(cacheTask)
	return result, nil
}
