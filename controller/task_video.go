package controller

import (
	"context"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/model"
	"github.com/MAX-API-Next/MAX-API/service"
)

// UpdateVideoTaskAll is kept for compatibility with older controller callers.
// The service implementation owns polling, CAS, and billing settlement.
func UpdateVideoTaskAll(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	return service.UpdateVideoTasks(ctx, platform, taskChannelM, taskM)
}
