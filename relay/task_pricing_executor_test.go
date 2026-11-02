// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package relay

import (
	"fmt"
	"testing"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
	"github.com/stretchr/testify/require"
)

func TestTaskPricingGoExecutorUsesActualFactory(t *testing.T) {
	for _, tc := range []struct {
		family, name, source, implementation string
		kind                                 int
	}{
		{"hailuo", "MiniMax-H3", "minimax", "*hailuo.TaskAdaptor", constant.ChannelTypeMiniMax},
		{"kling", "kling-v1", "kling", "*kling.TaskAdaptor", constant.ChannelTypeKling},
		{"doubao", "doubao-seedance-1-5-pro-251215", "doubao_video", "*doubao.TaskAdaptor", constant.ChannelTypeDoubaoVideo},
		{"doubao", "doubao-seedance-1-5-pro-251215", "doubao_video", "*doubao.TaskAdaptor", constant.ChannelTypeVolcEngine},
	} {
		t.Run(tc.family, func(t *testing.T) {
			contract, ok := taskpricing.KnownContract(tc.family, tc.name)
			require.True(t, ok)
			digest, err := taskpricing.ContractDigest(contract)
			require.NoError(t, err)
			shadow := &model.TaskPricingShadowContext{Profile: taskpricing.NewAPITask, ChannelID: 902, ChannelType: tc.kind,
				OriginalModel: "alias", MappedModel: tc.name, Group: "default", Source: taskpricing.Source{Driver: "go_adapter"},
				Guards: taskpricing.Guards{PriceVersion: "synthetic-price", BindingVersion: "synthetic-binding", ContractDigest: digest}}
			info := &relaycommon.RelayInfo{UserId: 901, TokenId: 903, RequestId: "go-request", OriginModelName: "alias", UsingGroup: "default",
				ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 902, ChannelType: tc.kind, UpstreamModelName: tc.name}}
			got, err := ResolveTaskPricingGoExecutor(info, shadow, "generate")
			require.NoError(t, err)
			require.Equal(t, tc.implementation, fmt.Sprintf("%T", got.Adaptor))
			require.Equal(t, tc.source, got.SourceID)
			require.Equal(t, "alias", got.Model)
			for _, mutate := range []func(*model.TaskPricingShadowContext){
				func(s *model.TaskPricingShadowContext) { s.ChannelID++ },
				func(s *model.TaskPricingShadowContext) { s.MappedModel = "other" },
				func(s *model.TaskPricingShadowContext) {
					s.Source = taskpricing.Source{Driver: "task_plugin", PluginKey: tc.family}
				},
				func(s *model.TaskPricingShadowContext) { s.Guards.ContractDigest = "other" },
				func(s *model.TaskPricingShadowContext) { s.Group = "other" },
			} {
				bad := *shadow
				mutate(&bad)
				got, err := ResolveTaskPricingGoExecutor(info, &bad, "generate")
				require.Error(t, err)
				require.Nil(t, got)
			}
			// Matching two absent channel IDs cannot establish a routed identity.
			info.ChannelId, shadow.ChannelID = 0, 0
			got, err = ResolveTaskPricingGoExecutor(info, shadow, "generate")
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
}
