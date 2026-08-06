package common

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

// DiskSpaceInfo 磁盘空间信息
type DiskSpaceInfo struct {
	// 总空间（字节）
	Total uint64 `json:"total"`
	// 可用空间（字节）
	Free uint64 `json:"free"`
	// 已用空间（字节）
	Used uint64 `json:"used"`
	// 使用百分比
	UsedPercent float64 `json:"used_percent"`
}

// SystemStatus 系统状态信息
type SystemStatus struct {
	CPUUsage    float64
	MemoryUsage float64
	DiskUsage   float64
}

var latestSystemStatus atomic.Value
var systemMonitorState struct {
	sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func init() {
	latestSystemStatus.Store(SystemStatus{})
}

// StartSystemMonitor 启动系统监控
func StartSystemMonitor() {
	systemMonitorState.Lock()
	defer systemMonitorState.Unlock()
	if systemMonitorState.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	systemMonitorState.cancel = cancel
	systemMonitorState.done = done
	go func() {
		defer close(done)
		runSystemMonitor(ctx)
	}()
}

func runSystemMonitor(ctx context.Context) {
	for {
		interval := 30 * time.Second
		config := GetPerformanceMonitorConfig()
		if config.Enabled {
			updateSystemStatus()
			interval = 5 * time.Second
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
}

func StopSystemMonitor(ctx context.Context) error {
	systemMonitorState.Lock()
	cancel := systemMonitorState.cancel
	done := systemMonitorState.done
	systemMonitorState.Unlock()
	if cancel == nil || done == nil {
		return nil
	}

	cancel()
	select {
	case <-done:
		systemMonitorState.Lock()
		if systemMonitorState.done == done {
			systemMonitorState.cancel = nil
			systemMonitorState.done = nil
		}
		systemMonitorState.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func updateSystemStatus() {
	var status SystemStatus

	// CPU
	// 注意：cpu.Percent(0, false) 返回自上次调用以来的 CPU 使用率
	// 如果是第一次调用，可能会返回错误或不准确的值，但在循环中会逐渐正常
	percents, err := cpu.Percent(0, false)
	if err == nil && len(percents) > 0 {
		status.CPUUsage = percents[0]
	}

	// Memory
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		status.MemoryUsage = memInfo.UsedPercent
	}

	// Disk
	diskInfo := GetDiskSpaceInfo()
	if diskInfo.Total > 0 {
		status.DiskUsage = diskInfo.UsedPercent
	}

	latestSystemStatus.Store(status)
}

// GetSystemStatus 获取当前系统状态
func GetSystemStatus() SystemStatus {
	return latestSystemStatus.Load().(SystemStatus)
}
