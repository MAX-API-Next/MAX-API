package service

import (
	"context"
	"time"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/model"
)

const authFlowCleanupInterval = time.Hour

func StartAuthFlowCleanup() {
	StartAuthFlowCleanupWithContext(context.Background())
}

func StartAuthFlowCleanupWithContext(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	if !common.IsMasterNode {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		runAuthFlowCleanup(ctx, authFlowCleanupInterval, cleanupAuthFlows)
	}()
	return done
}

func runAuthFlowCleanup(ctx context.Context, interval time.Duration, cleanup func()) {
	if ctx.Err() != nil {
		return
	}
	cleanup()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}

func cleanupAuthFlows() {
	if err := model.DeleteExpiredAuthFlows(time.Now()); err != nil {
		common.SysError("failed to delete expired OAuth flows: " + err.Error())
	}
}
