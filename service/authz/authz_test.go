package authz

import (
	"errors"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAuthzTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previous := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.AuthzUserOverride{}))
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	return db
}

func TestCatalogAndRoleBaselinesAreStable(t *testing.T) {
	catalog := Catalog()
	require.NotEmpty(t, catalog)
	var channel ResourceDefinition
	for _, item := range catalog {
		if item.Resource == ResourceChannel {
			channel = item
			break
		}
	}
	assert.Equal(t, ResourceChannel, channel.Resource)
	assert.True(t, roleGrants(common.RoleAdminUser)[ResourceChannel][ActionRead])
	assert.False(t, roleGrants(common.RoleAdminUser)[ResourceChannel][ActionSensitiveWrite])
	assert.True(t, roleGrants(common.RoleRootUser)[ResourceChannel][ActionSensitiveWrite])
	assert.False(t, roleGrants(common.RoleCommonUser)[ResourceChannel][ActionRead])
	roles := Roles()
	require.Len(t, roles, 3)
	assert.True(t, roles[0].Superuser)
}

func TestEvaluateUsesUserOverrideAndRootGuard(t *testing.T) {
	newAuthzTestDB(t)
	assert.True(t, Can(1, common.RoleRootUser, Permission{Resource: ResourceChannel, Action: ActionSecretView}))
	assert.False(t, Can(2, common.RoleAdminUser, Permission{Resource: ResourceChannel, Action: ActionSecretView}))
	require.NoError(t, ReplaceUserOverrides(2, PermissionsMap{ResourceChannel: {ActionSecretView: true, ActionRead: false}}))
	assert.True(t, Can(2, common.RoleAdminUser, Permission{Resource: ResourceChannel, Action: ActionSecretView}))
	assert.False(t, Can(2, common.RoleAdminUser, Permission{Resource: ResourceChannel, Action: ActionRead}))
	assert.False(t, Can(3, common.RoleCommonUser, Permission{Resource: ResourceChannel, Action: ActionSecretView}))
	decision := Evaluate(2, common.RoleAdminUser, Permission{Resource: "unknown", Action: ActionRead})
	assert.ErrorIs(t, decision.Err, ErrUnknownPermission)
	assert.False(t, decision.Allowed)
}

func TestReplaceUserOverridesRejectsUnknownAndPreservesExisting(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, ReplaceUserOverrides(7, PermissionsMap{ResourceChannel: {ActionWrite: false}}))
	err := ReplaceUserOverrides(7, PermissionsMap{ResourceChannel: {"unknown": true}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownPermission))
	assert.False(t, Can(7, common.RoleAdminUser, Permission{Resource: ResourceChannel, Action: ActionWrite}))
	var count int64
	require.NoError(t, db.Model(&model.AuthzUserOverride{}).Where("user_id = ?", 7).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	require.NoError(t, ClearUserOverrides(7))
	assert.True(t, Can(7, common.RoleAdminUser, Permission{Resource: ResourceChannel, Action: ActionWrite}))
}

func TestEvaluateFailsClosedWhenDatabaseUnavailable(t *testing.T) {
	previous := model.DB
	model.DB = nil
	t.Cleanup(func() { model.DB = previous })
	decision := Evaluate(1, common.RoleAdminUser, Permission{Resource: ResourceChannel, Action: ActionRead})
	assert.False(t, decision.Allowed)
	assert.Equal(t, "error", decision.Source)
	assert.Error(t, decision.Err)
}
