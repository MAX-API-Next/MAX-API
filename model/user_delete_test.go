package model

import (
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/stretchr/testify/require"
)

func TestUserDeleteRejectsRootUserInModelLayer(t *testing.T) {
	truncateTables(t)
	root := User{Id: 9101, Username: "root-delete-guard", Password: "password123", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&root).Error)

	err := (&User{Id: root.Id}).Delete()
	require.Error(t, err)

	var count int64
	require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", root.Id).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestUserHardDeleteRejectsRootUserInModelLayer(t *testing.T) {
	truncateTables(t)
	root := User{Id: 9102, Username: "root-hard-delete-guard", Password: "password123", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&root).Error)

	err := HardDeleteUserById(root.Id)
	require.Error(t, err)

	var count int64
	require.NoError(t, DB.Unscoped().Model(&User{}).Where("id = ?", root.Id).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
