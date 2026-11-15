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

func (info *ClaudeResponseInfo) recordDeliveredStreamOutput(data string) {
	var text strings.Builder
	for _, path := range []string{"delta.text", "delta.thinking", "delta.partial_json", "content_block.text"} {
		text.WriteString(gjson.Get(data, path).String())
	}
	if gjson.Get(data, "content_block.type").String() == "tool_use" {
		text.WriteString(gjson.Get(data, "content_block.name").String())
		text.WriteString(gjson.Get(data, "content_block.input").Raw)
	}
	if text.Len() > 0 {
		info.streamOutputDelivered = true
		info.streamDeliveredText.WriteString(text.String())
	}
}
