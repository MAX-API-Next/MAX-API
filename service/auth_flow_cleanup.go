package service

import (
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
)

const authFlowCleanupInterval = time.Hour

func StartAuthFlowCleanup() {
	if !common.IsMasterNode {
		return
	}
	go func() {
		cleanupAuthFlows()
		ticker := time.NewTicker(authFlowCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			cleanupAuthFlows()
		}
	}()
}

func cleanupAuthFlows() {
	if err := model.DeleteExpiredAuthFlows(time.Now()); err != nil {
		common.SysError("failed to delete expired OAuth flows: " + err.Error())
	}
}
