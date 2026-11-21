// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package service

import (
	"context"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskplugin"
)

// BindConfiguredTaskPlugin supplies the SDK with host-selected channel data.
// The binding identifies code. Submit still requires caller authorization.
func BindConfiguredTaskPlugin(ctx context.Context, catalog *taskplugin.Catalog, channel *model.Channel, actor taskplugin.Actor, original, action string) (*taskplugin.Executor, taskplugin.ChannelInput, error) {
	input := taskplugin.ChannelInput{}
	if channel == nil || channel.Type != constant.ChannelTypeTaskPlugin || channel.Status != common.ChannelStatusEnabled ||
		channel.Id != actor.ChannelID || channel.ChannelInfo.IsMultiKey || channel.Key == "" || channel.BaseURL == nil {
		return nil, input, taskplugin.ErrBindingRejected
	}
	settings, err := model.ReadTaskPluginChannelSettings(channel.Setting)
	if err != nil {
		return nil, input, err
	}
	models, err := channel.TaskPluginModels()
	mapped, advertised := models[original]
	if err != nil || !advertised {
		return nil, input, taskplugin.ErrBindingRejected
	}
	if err := refreshTaskPricingCatalog(ctx, catalog); err != nil {
		return nil, input, err
	}
	executor, err := catalog.Host().BindExecutor(actor, settings.Key, original, mapped, action)
	if err != nil || executor.Binding().DeclaredModel != mapped {
		return nil, input, taskplugin.ErrBindingRejected
	}
	config, err := executor.ConfiguredChannelParams(settings.Config, settings.Upstream)
	if err != nil {
		return nil, input, err
	}
	if taskplugin.ValidateChannelBaseURL(*channel.BaseURL) != nil {
		return nil, input, taskplugin.ErrRequestPolicy
	}
	return executor, taskplugin.ChannelInput{
		ID: channel.Id, BaseURL: *channel.BaseURL, APIKey: channel.Key,
		UpstreamKind: settings.Upstream, Config: config, Other: channel.Other,
	}, nil
}
