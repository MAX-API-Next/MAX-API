package common

import "time"

// EnableFirstResultTracking is called by adapters before starting workers.
// Unclassified adapters retain their existing first-response semantics.
func (info *RelayInfo) EnableFirstResultTracking() {
	if info == nil || info.channelFirstResultTracking {
		return
	}
	info.channelFirstResultTracking = true
	info.channelFirstResultSignal = make(chan struct{})
}

// SetFirstResultTime records a successfully delivered generation payload,
// independently of lifecycle/usage frames and their existing latency metric.
func (info *RelayInfo) SetFirstResultTime() {
	if info == nil {
		return
	}
	info.channelFirstResponseMu.Lock()
	defer info.channelFirstResponseMu.Unlock()
	info.setFirstResponseTimeLocked()
	if !info.channelFirstResultTracking || !info.channelFirstResultRecorded.CompareAndSwap(false, true) {
		return
	}
	if info.FirstResultTime.IsZero() {
		info.FirstResultTime = time.Now()
	}
	close(info.channelFirstResultSignal)
	if !info.IsChannelTest {
		info.recordChannelHealthSuccess()
	}
}

func (info *RelayInfo) HasRecordedChannelFirstResult() bool {
	if info == nil {
		return false
	}
	if !info.channelFirstResultTracking {
		return info.HasRecordedChannelFirstResponse()
	}
	return info.channelFirstResultRecorded.Load()
}

func (info *RelayInfo) FirstResultSignal() <-chan struct{} {
	if info == nil {
		return nil
	}
	if !info.channelFirstResultTracking {
		return info.FirstResponseSignal()
	}
	return info.channelFirstResultSignal
}

func (info *RelayInfo) FirstResultDeadlineExpired() bool {
	if info == nil {
		return true
	}
	info.channelFirstResponseMu.Lock()
	defer info.channelFirstResponseMu.Unlock()
	if info.HasRecordedChannelFirstResult() {
		return false
	}
	return !info.channelFirstResponseEvaluating || time.Since(info.channelFirstResponseEvalAt) >= firstResponseEvaluationGrace
}
