package relay

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

type taskResponseSnapshot struct {
	header http.Header
	status int
	body   []byte
}

func (s *taskResponseSnapshot) writeTo(c *gin.Context) error {
	if s == nil || c == nil {
		return errors.New("task response snapshot is unavailable")
	}
	for key, values := range s.header {
		c.Writer.Header()[key] = append([]string(nil), values...)
	}
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	c.Writer.WriteHeader(status)
	_, err := c.Writer.Write(s.body)
	return err
}

type taskResponseBuffer struct {
	original gin.ResponseWriter
	header   http.Header
	body     bytes.Buffer
	status   int
	written  bool
}

func newTaskResponseBuffer(original gin.ResponseWriter) *taskResponseBuffer {
	header := make(http.Header, len(original.Header()))
	for key, values := range original.Header() {
		header[key] = append([]string(nil), values...)
	}
	return &taskResponseBuffer{original: original, header: header, status: http.StatusOK}
}

func (w *taskResponseBuffer) Header() http.Header { return w.header }

func (w *taskResponseBuffer) Write(data []byte) (int, error) {
	if !w.written {
		w.WriteHeaderNow()
	}
	return w.body.Write(data)
}

func (w *taskResponseBuffer) WriteString(data string) (int, error) {
	if !w.written {
		w.WriteHeaderNow()
	}
	return w.body.WriteString(data)
}

func (w *taskResponseBuffer) WriteHeader(status int) {
	if w.written {
		return
	}
	w.status = status
}

func (w *taskResponseBuffer) WriteHeaderNow() { w.written = true }
func (w *taskResponseBuffer) Status() int     { return w.status }
func (w *taskResponseBuffer) Size() int       { return w.body.Len() }
func (w *taskResponseBuffer) Written() bool   { return w.written }
func (w *taskResponseBuffer) Flush()          {}
func (w *taskResponseBuffer) Pusher() http.Pusher {
	return w.original.Pusher()
}
func (w *taskResponseBuffer) CloseNotify() <-chan bool { return w.original.CloseNotify() }
func (w *taskResponseBuffer) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("task submission responses cannot hijack the connection")
}

func (w *taskResponseBuffer) snapshot() *taskResponseSnapshot {
	header := make(http.Header, len(w.header))
	for key, values := range w.header {
		header[key] = append([]string(nil), values...)
	}
	return &taskResponseSnapshot{
		header: header,
		status: w.status,
		body:   append([]byte(nil), w.body.Bytes()...),
	}
}
