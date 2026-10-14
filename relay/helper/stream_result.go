package helper

import (
	relaycommon "github.com/MAX-API-Next/MAX-API/relay/common"
)

// StreamResult is passed to each dataHandler invocation, providing methods
// to record soft errors, signal fatal stops, or mark normal completion.
// StreamScannerHandler checks IsStopped() after each callback invocation.
type StreamResult struct {
	status            *relaycommon.StreamStatus
	stopped           bool
	done              bool
	delivered         bool
	initialErrorCount int
}

func newStreamResult(status *relaycommon.StreamStatus) *StreamResult {
	return &StreamResult{status: status}
}

// Error records a soft error. The stream continues processing.
// Can be called multiple times per chunk.
func (r *StreamResult) Error(err error) {
	if err == nil {
		return
	}
	r.status.RecordError(err.Error())
}

// Stop records a fatal error and marks the stream to stop after this chunk.
func (r *StreamResult) Stop(err error) {
	if err != nil {
		r.status.RecordError(err.Error())
		r.status.RecordFatalError(err)
	}
	r.status.SetEndReason(relaycommon.StreamEndReasonHandlerStop, err)
	r.stopped = true
}

// Done signals that the handler has finished processing normally
// (e.g., Dify "message_end"). The stream stops after this chunk.
func (r *StreamResult) Done() {
	r.status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	r.done = true
	r.stopped = true
}

// MarkDelivered explicitly marks an event whose output was delivered through
// a path that the scanner cannot observe directly. Normal handlers can rely on
// StreamScannerHandler detecting a positive response-writer byte delta.
func (r *StreamResult) MarkDelivered() {
	if r != nil {
		r.delivered = true
	}
}

func (r *StreamResult) IsDeliverable() bool {
	return r != nil && r.delivered
}

// IsStopped returns whether Stop() or Done() was called during this chunk.
func (r *StreamResult) IsStopped() bool {
	return r.stopped
}

// IsSuccessful reports whether the handler accepted the current upstream
// event without a fatal stop or a newly recorded soft conversion error.
func (r *StreamResult) IsSuccessful() bool {
	if r == nil || r.status == nil {
		return false
	}
	return (r.done || !r.stopped) && r.status.TotalErrorCount() == r.initialErrorCount
}

// reset clears the per-chunk stopped flag so the object can be reused.
func (r *StreamResult) reset() {
	r.stopped = false
	r.done = false
	r.delivered = false
	if r.status != nil {
		r.initialErrorCount = r.status.TotalErrorCount()
	}
}
