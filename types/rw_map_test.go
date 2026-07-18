package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadFromJsonStringWithCallbackAllowsCallbackToReadMap(t *testing.T) {
	m := NewRWMap[string, int]()
	done := make(chan error, 1)
	read := make(chan map[string]int, 1)

	go func() {
		done <- LoadFromJsonStringWithCallback(m, `{"answer":42}`, func() {
			read <- m.ReadAll()
		})
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("LoadFromJsonStringWithCallback deadlocked while callback read the map")
	}

	select {
	case got := <-read:
		require.Equal(t, map[string]int{"answer": 42}, got)
	default:
		t.Fatal("callback did not read the map")
	}
}
