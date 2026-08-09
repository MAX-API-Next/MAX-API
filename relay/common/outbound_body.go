package common

import (
	"io"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
)

// NewOutboundJSONBody wraps the already-marshaled upstream request body into a
// BodyStorage. When disk cache is enabled and the payload exceeds the configured
// threshold, the data is written to a temp file and the original []byte can be
// GC'd, significantly reducing the heap residency while waiting for the
// upstream provider to respond (the dominant cost for large base64 payloads).
//
// In memory mode the underlying memoryStorage reuses the same backing array,
// so this is equivalent to bytes.NewReader(data) in terms of memory usage.
//
// The caller MUST invoke closer.Close() once the upstream call has finished
// (typically via defer) to release the disk file / memory accounting.
//
// The returned replayable reader hides storage ownership from net/http while
// exposing independent readers for transport-level retries.
func NewOutboundJSONBody(data []byte) (body io.Reader, size int64, closer io.Closer, err error) {
	storage, err := common.CreateBodyStorage(data)
	if err != nil {
		return nil, 0, nil, err
	}
	return common.NewReplayableBodyReader(storage), storage.Size(), storage, nil
}

// PreparePassThroughJSONBody preserves raw pass-through semantics while still
// enforcing channel permissions for fields that can change cost or privacy.
func PreparePassThroughJSONBody(storage common.BodyStorage, settings dto.ChannelOtherSettings) (body io.Reader, size int64, closer io.Closer, err error) {
	filtered, err := newPassThroughFilteredBody(storage, settings)
	if err != nil {
		return nil, 0, nil, err
	}
	if filtered == nil {
		return common.NewReplayableBodyReader(storage), storage.Size(), nil, nil
	}
	return filtered, filtered.Size(), filtered, nil
}
