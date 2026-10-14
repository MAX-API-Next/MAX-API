package common

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type StreamEndReason string

const (
	StreamEndReasonNone        StreamEndReason = ""
	StreamEndReasonDone        StreamEndReason = "done"
	StreamEndReasonTimeout     StreamEndReason = "timeout"
	StreamEndReasonClientGone  StreamEndReason = "client_gone"
	StreamEndReasonScannerErr  StreamEndReason = "scanner_error"
	StreamEndReasonHandlerStop StreamEndReason = "handler_stop"
	StreamEndReasonEOF         StreamEndReason = "eof"
	StreamEndReasonPanic       StreamEndReason = "panic"
	StreamEndReasonPingFail    StreamEndReason = "ping_fail"
)

const maxStreamErrorEntries = 20

type StreamErrorEntry struct {
	Message   string
	Timestamp time.Time
}

type StreamStatus struct {
	EndReason StreamEndReason
	EndError  error
	endOnce   sync.Once

	mu         sync.Mutex
	Errors     []StreamErrorEntry
	ErrorCount int

	fatalMu    sync.Mutex
	fatalError error
}

func NewStreamStatus() *StreamStatus {
	return &StreamStatus{}
}

func (s *StreamStatus) SetEndReason(reason StreamEndReason, err error) {
	if s == nil {
		return
	}
	s.endOnce.Do(func() {
		s.EndReason = reason
		s.EndError = err
	})
}

func (s *StreamStatus) RecordError(msg string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ErrorCount++
	if len(s.Errors) < maxStreamErrorEntries {
		s.Errors = append(s.Errors, StreamErrorEntry{
			Message:   msg,
			Timestamp: time.Now(),
		})
	}
}

// RecordFatalError preserves a handler error even when the scanner has
// already recorded EOF. The scanner and handler run concurrently, so the
// first transport end reason is not sufficient to classify a fatal callback
// failure.
func (s *StreamStatus) RecordFatalError(err error) {
	if s == nil || err == nil {
		return
	}
	s.fatalMu.Lock()
	defer s.fatalMu.Unlock()
	if s.fatalError == nil {
		s.fatalError = err
	}
}

func (s *StreamStatus) FatalError() error {
	if s == nil {
		return nil
	}
	s.fatalMu.Lock()
	defer s.fatalMu.Unlock()
	return s.fatalError
}

func (s *StreamStatus) HasErrors() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ErrorCount > 0
}

func (s *StreamStatus) TotalErrorCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ErrorCount
}

func (s *StreamStatus) IsNormalEnd() bool {
	if s == nil {
		return true
	}
	return !s.IsAbnormalEnd() && (s.EndReason == StreamEndReasonDone ||
		s.EndReason == StreamEndReasonEOF ||
		s.EndReason == StreamEndReasonHandlerStop)
}

// IsAbnormalEnd reports transport or handler endings that must not be treated
// as a successful provider completion. Plain EOF is intentionally excluded:
// callers still need to verify a protocol terminal event when a provider
// closes the stream without sending [DONE] or an equivalent event. A fatal
// handler error remains abnormal even if the scanner won the EOF race.
func (s *StreamStatus) IsAbnormalEnd() bool {
	if s == nil {
		return false
	}
	if s.FatalError() != nil {
		return true
	}
	switch s.EndReason {
	case StreamEndReasonTimeout, StreamEndReasonClientGone,
		StreamEndReasonScannerErr, StreamEndReasonPanic, StreamEndReasonPingFail:
		return true
	case StreamEndReasonHandlerStop:
		return s.EndError != nil
	default:
		return false
	}
}

func (s *StreamStatus) Summary() string {
	if s == nil {
		return "StreamStatus<nil>"
	}
	b := &strings.Builder{}
	fmt.Fprintf(b, "reason=%s", s.EndReason)
	endError := s.EndError
	if endError == nil {
		endError = s.FatalError()
	}
	if endError != nil {
		fmt.Fprintf(b, " end_error=%q", endError.Error())
	}
	s.mu.Lock()
	if s.ErrorCount > 0 {
		fmt.Fprintf(b, " soft_errors=%d", s.ErrorCount)
	}
	s.mu.Unlock()
	return b.String()
}
