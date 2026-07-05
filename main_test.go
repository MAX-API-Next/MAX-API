package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWithTimeoutCompletes(t *testing.T) {
	assert.True(t, runWithTimeout(time.Second, func() {}))
}

func TestRunWithTimeoutTimesOut(t *testing.T) {
	start := time.Now()
	assert.False(t, runWithTimeout(10*time.Millisecond, func() {
		time.Sleep(500 * time.Millisecond)
	}))
	assert.Less(t, time.Since(start), 250*time.Millisecond)
}

func TestShutdownHTTPServerClosesActiveHandlersAfterTimeout(t *testing.T) {
	handlerStarted := make(chan struct{})
	handlerDone := make(chan struct{})
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(handlerStarted)
			<-r.Context().Done()
			close(handlerDone)
		}),
	}
	t.Cleanup(func() {
		_ = server.Close()
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()

	client := &http.Client{Timeout: time.Second}
	clientDone := make(chan error, 1)
	go func() {
		resp, err := client.Get("http://" + listener.Addr().String())
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		clientDone <- err
	}()

	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	shutdownHTTPServer(ctx, server)

	select {
	case <-handlerDone:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("active handler was not force-closed after shutdown timeout")
	}

	select {
	case err := <-serveDone:
		assert.ErrorIs(t, err, http.ErrServerClosed)
	case <-time.After(250 * time.Millisecond):
		t.Fatal("server did not stop after forced close")
	}

	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("client request did not finish after forced close")
	}
}
