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

package service

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
	"github.com/stretchr/testify/require"
)

func TestTaskPluginHostRequiresPersistedOwnerAndFrozenUsageContract(t *testing.T) {
	manifest := taskplugin.Manifest{APIVersion: 1, Key: "host-test", Name: "Host test", Version: "1.0.0", Models: []string{"original-model"}, Routes: []taskplugin.Route{{Method: "POST", Path: "/plugin/jobs", Type: "submit", Decode: "decode", Render: "render"}}}
	data, err := common.Marshal(taskplugin.PackageDocument{FormatVersion: 1, Manifest: manifest, Source: "not executed"})
	require.NoError(t, err)
	hash := sha256.Sum256(data)
	p, err := taskplugin.LoadPackage(bytes.NewReader(data), fmt.Sprintf("%x", hash))
	require.NoError(t, err)
	host := taskplugin.NewHost()
	_, err = host.Install(p)
	require.NoError(t, err)
	actor := taskplugin.Actor{UserID: 1, TokenID: 2, ChannelID: 3, RequestID: "submission-one"}
	contract := taskusage.DoubaoVideoContract()
	binding, err := host.BindMapped(actor, "POST", "/plugin/jobs", "client-alias", "original-model", contract)
	require.NoError(t, err)
	task := &model.Task{ID: 99, UserId: 1, ChannelId: 3, Properties: model.Properties{OriginModelName: "client-alias", UpstreamModelName: "original-model"}, PrivateData: model.TaskPrivateData{TokenId: 2, BillingRequestId: "submission-one", BillingContext: &model.TaskBillingContext{TaskBillingPlan: &types.TaskBillingPlan{UsageSourceID: binding.SourceID, UsageSchemaVersion: binding.SchemaVersion, UsageContractDigest: binding.ContractDigest}}}}
	require.NoError(t, AttachTaskPluginBinding(host, task, binding))
	// Roundtrip exactly the JSON value that model.Task stores in private_data.
	persisted, err := common.Marshal(task.PrivateData)
	require.NoError(t, err)
	task.PrivateData = model.TaskPrivateData{}
	require.NoError(t, common.Unmarshal(persisted, &task.PrivateData))
	zero := int64(0)
	usage := &types.TaskUsage{CompletionTokens: &zero, TotalTokens: &zero, Source: types.TaskUsageSourceProviderResponse, Completeness: types.TaskUsageCompletenessComplete}
	result := &relaycommon.TaskInfo{Status: string(model.TaskStatusSuccess)}
	require.NoError(t, ApplyTaskPluginUsage(host, task, result, usage))
	require.Equal(t, types.TaskUsagePresencePresentZero, result.UsageEnvelope.Presence)
	require.Equal(t, types.TaskUsageProducerKindTaskPlugin, result.UsageEnvelope.ProducerKind)
	task.PrivateData.TokenId = 100
	before := *result
	require.ErrorIs(t, ApplyTaskPluginUsage(host, task, result, usage), taskplugin.ErrBindingRejected)
	require.Equal(t, before, *result, "invalid ownership must not mutate polling evidence")
	task.PrivateData.TokenId = 2
	task.PrivateData.BillingContext.TaskBillingPlan.UsageContractDigest = "tampered"
	require.ErrorIs(t, ApplyTaskPluginUsage(host, task, result, usage), taskplugin.ErrBindingRejected)
	public, err := common.Marshal(task)
	require.NoError(t, err)
	require.NotContains(t, string(public), "plugin_binding")
	require.NotContains(t, string(public), binding.PackageDigest)
}
