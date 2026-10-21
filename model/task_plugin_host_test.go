/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package model

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskPluginHostBindingSurvivesSQLiteFundingAndEffectReplay(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "plugin.db")), taskBillingTestGORMConfig())
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	prepareTaskBillingContractDB(t, db, "sqlite")
	f := newTaskBillingContractFixture(t, db, 9992, BillingSettlementSourceWallet, -20)
	manifest := taskplugin.Manifest{APIVersion: 1, Key: "ledger-test", Name: "Ledger test", Version: "1.0.0", Models: []string{"contract-model"}, Routes: []taskplugin.Route{{Method: "GET", Path: "/plugin/:id", Type: "query", Render: "render"}}}
	data, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Manifest: manifest, Source: "original fixture, never executed"})
	require.NoError(t, err)
	hash := sha256.Sum256(data)
	p, err := taskplugin.LoadPackage(bytes.NewReader(data), fmt.Sprintf("%x", hash))
	require.NoError(t, err)
	host := taskplugin.NewHost()
	_, err = host.Install(p)
	require.NoError(t, err)
	actor := taskplugin.Actor{UserID: f.user.Id, TokenID: f.token.Id, ChannelID: f.channel.Id, RequestID: "plugin-submission"}
	binding, err := host.Bind(actor, "GET", "/plugin/one", "contract-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	f.task.PrivateData.PluginBinding = &binding
	f.task.PrivateData.TokenId = f.token.Id
	f.task.PrivateData.BillingRequestId = actor.RequestID
	require.NoError(t, db.Save(&f.task).Error)
	var stored Task
	require.NoError(t, db.First(&stored, f.task.ID).Error)
	require.NotNil(t, stored.PrivateData.PluginBinding)
	restarted := taskplugin.NewHost()
	currentManifest := manifest
	currentManifest.Version = "2.0.0"
	currentData, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Manifest: currentManifest, Source: "current fixture, never executed"})
	require.NoError(t, err)
	currentHash := sha256.Sum256(currentData)
	currentPackage, err := taskplugin.LoadPackage(bytes.NewReader(currentData), fmt.Sprintf("%x", currentHash))
	require.NoError(t, err)
	_, err = restarted.Install(currentPackage)
	require.NoError(t, err)
	_, err = restarted.Retain(p)
	require.NoError(t, err)
	session, err := restarted.Resolve(*stored.PrivateData.PluginBinding, actor)
	require.NoError(t, err)
	require.Equal(t, "1.0.0", session.Binding().Version)
	require.Equal(t, p.Digest(), session.Binding().PackageDigest)
	newBinding, err := restarted.Bind(actor, "GET", "/plugin/one", "contract-model", taskusage.MiniMaxH3Contract())
	require.NoError(t, err)
	require.Equal(t, "2.0.0", newBinding.Version, "restoring a persisted task must not switch the active version")
	duration, images := int64(5_000), int64(0)
	envelope, err := session.UsageEnvelope(&types.TaskUsage{OutputDurationMs: &duration, InputImageCount: &images, Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete})
	require.NoError(t, err)
	stored.PrivateData.BillingContext.TaskUsageEnvelope = envelope
	require.NoError(t, db.Save(&stored).Error)
	for attempt := 0; attempt < 2; attempt++ {
		applied, replay, err := ApplyBillingSettlementOnce(f.input)
		require.NoError(t, err)
		require.EqualValues(t, -20, applied)
		require.Equal(t, attempt > 0, replay)
		require.NoError(t, ProcessBillingSettlementEffect(f.input.OperationKey))
	}
	f.assertProjection(t, db, 80, 1)
	require.NoError(t, db.First(&stored, f.task.ID).Error)
	require.Equal(t, binding, *stored.PrivateData.PluginBinding)
	require.Equal(t, envelope.EvidenceDigest, stored.PrivateData.BillingContext.TaskUsageEnvelope.EvidenceDigest)
}
