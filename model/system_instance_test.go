package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeleteStaleSystemInstanceDeletesOnlyStaleNodes(t *testing.T) {
	truncateTables(t)

	now := int64(10_000)
	require.NoError(t, DB.Create(&SystemInstance{
		NodeName:   "stale-node",
		LastSeenAt: now - SystemInstanceStaleAfterSeconds - 1,
	}).Error)
	require.NoError(t, DB.Create(&SystemInstance{
		NodeName:   "online-node",
		LastSeenAt: now,
	}).Error)

	require.NoError(t, DeleteStaleSystemInstance("stale-node", now))

	var count int64
	require.NoError(t, DB.Model(&SystemInstance{}).Where("node_name = ?", "stale-node").Count(&count).Error)
	require.Zero(t, count)

	err := DeleteStaleSystemInstance("online-node", now)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSystemInstanceOnline))

	require.NoError(t, DB.Model(&SystemInstance{}).Where("node_name = ?", "online-node").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestDeleteStaleSystemInstanceMissingNode(t *testing.T) {
	truncateTables(t)

	err := DeleteStaleSystemInstance("missing-node", 10_000)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSystemInstanceNotFound))
}
