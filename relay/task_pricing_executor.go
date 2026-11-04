// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
package relay

import (
	"errors"
	"strconv"
	"strings"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/pkg/taskpricing"
	"github.com/MAX-API-Next/MAX-API/relay/channel"
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
)

// TaskPricingGoExecutor identifies the actual object constructed by MAX's
// existing Go factory. It is not an initialized/sent request or paid permit.
type TaskPricingGoExecutor struct {
	Adaptor                               channel.TaskAdaptor
	Source                                taskpricing.Source
	SourceID                              string
	ChannelID, ChannelType                int
	Model, MappedModel, RequestID, Action string
}

// ResolveTaskPricingGoExecutor compares host routing metadata with the shadow
// observation, then uses the existing factory. It never initializes/submits an
// adaptor or falls back from a missing plugin. Price freshness, exposure bounds
// and durable admission remain separate gates; production does not call it yet.
func ResolveTaskPricingGoExecutor(info *relaycommon.RelayInfo, shadow *model.TaskPricingShadowContext, action string) (*TaskPricingGoExecutor, error) {
	if info == nil || info.ChannelMeta == nil || shadow == nil || info.UserId <= 0 || info.TokenId <= 0 || info.ChannelId <= 0 ||
		info.RequestId == "" || strings.TrimSpace(info.RequestId) != info.RequestId || action == "" || strings.TrimSpace(action) != action ||
		shadow.Profile != taskpricing.NewAPITask || shadow.Source != (taskpricing.Source{Driver: "go_adapter"}) ||
		shadow.ChannelID != info.ChannelId || shadow.ChannelType != info.ChannelType || shadow.Group != info.UsingGroup ||
		shadow.OriginalModel != info.OriginModelName || shadow.MappedModel != info.UpstreamModelName ||
		taskpricing.CheckGuards(shadow.Guards, shadow.Guards) != nil {
		return nil, errors.New("task pricing Go executor does not match the configured host context")
	}
	family, source := "", ""
	switch info.ChannelType {
	case constant.ChannelTypeMiniMax:
		family, source = "hailuo", "minimax"
	case constant.ChannelTypeKling:
		family, source = "kling", "kling"
	case constant.ChannelTypeDoubaoVideo, constant.ChannelTypeVolcEngine:
		family, source = "doubao", "doubao_video"
	default:
		return nil, errors.New("unsupported task pricing Go executor")
	}
	contract, ok := taskpricing.KnownContract(family, info.UpstreamModelName)
	if !ok {
		return nil, errors.New("task pricing Go model has no host contract")
	}
	digest, err := taskpricing.ContractDigest(contract)
	if err != nil || digest != shadow.Guards.ContractDigest {
		return nil, errors.New("task pricing Go contract does not match the configured host context")
	}
	adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(info.ChannelType)))
	if adaptor == nil {
		return nil, errors.New("task pricing Go executor is unavailable")
	}
	return &TaskPricingGoExecutor{Adaptor: adaptor, Source: shadow.Source, SourceID: source,
		ChannelID: info.ChannelId, ChannelType: info.ChannelType, Model: info.OriginModelName,
		MappedModel: info.UpstreamModelName, RequestID: info.RequestId, Action: action}, nil
}
