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
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/MAX-API-Next/MAX-API/types"
)

// AttachTaskPluginBinding prepares private task metadata for the caller's
// existing persistence transaction, before upstream acceptance. It does not
// reserve, settle, refund, submit or retry anything.
func AttachTaskPluginBinding(host *taskplugin.Host, task *model.Task, binding types.TaskPluginBinding) error {
	if !taskPluginBindingMatches(task, binding) {
		return taskplugin.ErrBindingRejected
	}
	if _, err := host.Resolve(binding, taskplugin.Actor{UserID: task.UserId, TokenID: task.PrivateData.TokenId, ChannelID: task.ChannelId, RequestID: task.PrivateData.BillingRequestId}); err != nil {
		return err
	}
	if task.PrivateData.PluginBinding != nil && *task.PrivateData.PluginBinding != binding {
		return taskplugin.ErrBindingRejected
	}
	copy := binding
	task.PrivateData.PluginBinding = &copy
	task.IncludePrivateDataInUpdate()
	return nil
}

func taskPluginBindingMatches(task *model.Task, binding types.TaskPluginBinding) bool {
	if task == nil || task.PrivateData.BillingContext == nil {
		return false
	}
	plan := task.PrivateData.BillingContext.TaskBillingPlan
	upstream := task.Properties.UpstreamModelName
	if upstream == "" {
		upstream = task.Properties.OriginModelName
	}
	return plan != nil && binding.UserID > 0 && binding.UserID == task.UserId && binding.TokenID > 0 && binding.TokenID == task.PrivateData.TokenId && binding.ChannelID > 0 && binding.ChannelID == task.ChannelId && binding.RequestID != "" && binding.RequestID == task.PrivateData.BillingRequestId && binding.Model != "" && binding.Model == task.Properties.OriginModelName && binding.UpstreamModel == upstream && binding.SourceID == plan.UsageSourceID && binding.SchemaVersion == plan.UsageSchemaVersion && binding.ContractDigest == plan.UsageContractDigest
}

// ResolveTaskPluginSession validates persisted ownership and frozen billing
// identity. Restoring a binding never falls back to the latest plugin version.
func ResolveTaskPluginSession(host *taskplugin.Host, task *model.Task) (*taskplugin.Session, error) {
	if task == nil || task.ID <= 0 || task.PrivateData.PluginBinding == nil || !taskPluginBindingMatches(task, *task.PrivateData.PluginBinding) {
		return nil, taskplugin.ErrBindingRejected
	}
	return host.Resolve(*task.PrivateData.PluginBinding, taskplugin.Actor{UserID: task.UserId, TokenID: task.PrivateData.TokenId, ChannelID: task.ChannelId, RequestID: task.PrivateData.BillingRequestId})
}

// ApplyTaskPluginUsage only attaches validated provider facts. All terminal
// decisions and money mutations remain in the existing polling/settlement path.
func ApplyTaskPluginUsage(host *taskplugin.Host, task *model.Task, result *relaycommon.TaskInfo, usage *types.TaskUsage) error {
	if result == nil {
		return taskplugin.ErrBindingRejected
	}
	session, err := ResolveTaskPluginSession(host, task)
	if err != nil {
		return err
	}
	envelope, err := session.UsageEnvelope(usage)
	if err != nil {
		return err
	}
	candidate := *result
	if envelope.SourceID != taskusage.SourceIDDoubaoVideo || envelope.Completeness == types.TaskUsageCompletenessComplete {
		if err := applyLegacyTaskUsageFields(&candidate, envelope); err != nil {
			return err
		}
	}
	candidate.UsageEnvelope = types.CloneTaskUsageEnvelope(envelope)
	candidate.Usage = types.CloneTaskUsage(envelope.Usage)
	*result = candidate
	return nil
}
