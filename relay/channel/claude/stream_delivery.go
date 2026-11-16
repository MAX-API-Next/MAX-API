package claude

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Legacy SSE rendering discards write errors. Latch them so a failed or short
// frame cannot be counted as delivered or followed by a controller error frame.
type claudeStreamWriteObserver struct {
	gin.ResponseWriter
	err error
}

func (w *claudeStreamWriteObserver) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.ResponseWriter.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func (w *claudeStreamWriteObserver) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *claudeStreamWriteObserver) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (info *ClaudeResponseInfo) recordDeliveredStreamOutput(data string, nativeOutput bool) {
	var text strings.Builder
	if nativeOutput {
		text.WriteString(gjson.Get(data, "content_block.thinking").String())
	}
	for _, path := range []string{"delta.text", "delta.thinking", "delta.partial_json", "content_block.text"} {
		text.WriteString(gjson.Get(data, path).String())
	}
	if gjson.Get(data, "content_block.type").String() == "tool_use" {
		text.WriteString(gjson.Get(data, "content_block.name").String())
		// The Chat converter sends an empty arguments string at tool start.
		// Only native Claude clients receive the initial input object.
		if nativeOutput {
			text.WriteString(gjson.Get(data, "content_block.input").Raw)
		}
	}
	// Native clients also receive opaque thinking payloads. Mark delivery
	// without treating encrypted data/signatures as estimated plaintext.
	if nativeOutput {
		eventType := gjson.Get(data, "type").String()
		blockType := gjson.Get(data, "content_block.type").String()
		if (eventType == "content_block_start" &&
			((blockType == "redacted_thinking" && gjson.Get(data, "content_block.data").String() != "") ||
				(blockType == "thinking" && gjson.Get(data, "content_block.signature").String() != ""))) ||
			(eventType == "content_block_delta" && gjson.Get(data, "delta.type").String() == "signature_delta" && gjson.Get(data, "delta.signature").String() != "") {
			info.streamOutputDelivered = true
		}
	}
	if text.Len() > 0 {
		info.streamOutputDelivered = true
		info.streamDeliveredText.WriteString(text.String())
	}
}
