package common

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireIndependentBodyReaders(t *testing.T, storage BodyStorage, payload []byte) {
	t.Helper()
	first, err := storage.NewReader()
	require.NoError(t, err)
	defer first.Close()
	second, err := storage.NewReader()
	require.NoError(t, err)
	defer second.Close()

	prefix := make([]byte, 4)
	_, err = io.ReadFull(first, prefix)
	require.NoError(t, err)
	secondBody, err := io.ReadAll(second)
	require.NoError(t, err)
	require.Equal(t, payload, secondBody)
	firstRest, err := io.ReadAll(first)
	require.NoError(t, err)
	require.Equal(t, payload[4:], firstRest)
}

func TestBodyStorageNewReaderUsesIndependentCursors(t *testing.T) {
	payload := []byte("independent replay payload")

	t.Run("memory", func(t *testing.T) {
		storage := newMemoryStorage(payload)
		defer storage.Close()
		requireIndependentBodyReaders(t, storage, payload)
	})

	t.Run("disk", func(t *testing.T) {
		storage, err := newDiskStorage(payload, "")
		require.NoError(t, err)
		defer storage.Close()
		requireIndependentBodyReaders(t, storage, payload)
	})
}

func TestBodyStorageNewReaderRejectsClosedStorage(t *testing.T) {
	storage := newMemoryStorage([]byte("closed"))
	require.NoError(t, storage.Close())
	reader, err := storage.NewReader()
	require.Nil(t, reader)
	require.ErrorIs(t, err, ErrStorageClosed)
}
