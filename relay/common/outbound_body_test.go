package common

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	basecommon "github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/stretchr/testify/require"
)

type noBytesBodyStorage struct {
	payload        []byte
	reader         *bytes.Reader
	newReaderCalls int
}

func newNoBytesBodyStorage(payload []byte) *noBytesBodyStorage {
	return &noBytesBodyStorage{payload: payload, reader: bytes.NewReader(payload)}
}

func (s *noBytesBodyStorage) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s *noBytesBodyStorage) Seek(offset int64, whence int) (int64, error) {
	return s.reader.Seek(offset, whence)
}
func (s *noBytesBodyStorage) Close() error { return nil }
func (s *noBytesBodyStorage) Bytes() ([]byte, error) {
	return nil, errors.New("full body read is forbidden")
}
func (s *noBytesBodyStorage) Size() int64  { return int64(len(s.payload)) }
func (s *noBytesBodyStorage) IsDisk() bool { return true }
func (s *noBytesBodyStorage) NewReader() (io.ReadCloser, error) {
	s.newReaderCalls++
	return io.NopCloser(bytes.NewReader(s.payload)), nil
}

func TestPreparePassThroughJSONBodyFiltersCostFields(t *testing.T) {
	storage, err := basecommon.CreateBodyStorage([]byte(`{"model":"gpt-5","service_tier":"flex","speed":"fast"}`))
	require.NoError(t, err)
	defer storage.Close()

	body, size, closer, err := PreparePassThroughJSONBody(storage, dto.ChannelOtherSettings{})
	require.NoError(t, err)
	if closer != nil {
		defer closer.Close()
	}
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-5"}`, string(data))
	require.Equal(t, int64(len(data)), size)
}

func TestPreparePassThroughJSONBodyReusesStorageWhenUnchanged(t *testing.T) {
	payload := []byte(`{"model":"gpt-5","service_tier":"flex"}`)
	storage, err := basecommon.CreateBodyStorage(payload)
	require.NoError(t, err)
	defer storage.Close()

	body, size, closer, err := PreparePassThroughJSONBody(storage, dto.ChannelOtherSettings{AllowServiceTier: true})
	require.NoError(t, err)
	require.Nil(t, closer)
	require.Equal(t, int64(len(payload)), size)
	_, ok := body.(basecommon.ReplayableBody)
	require.True(t, ok)
}

func TestPreparePassThroughJSONBodyDoesNotMaterializeUnchangedStorage(t *testing.T) {
	payload := []byte(`{"model":"gpt-5","input":"` + strings.Repeat("x", 1<<20) + `"}`)
	storage := newNoBytesBodyStorage(payload)

	body, size, closer, err := PreparePassThroughJSONBody(storage, dto.ChannelOtherSettings{})
	require.NoError(t, err)
	require.Nil(t, closer)
	require.Equal(t, int64(len(payload)), size)

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, payload, data)
}

func TestPreparePassThroughJSONBodySkipsScanWhenAllControlledFieldsAllowed(t *testing.T) {
	payload := []byte(`{"model":"gpt-5","service_tier":"flex"}`)
	storage := newNoBytesBodyStorage(payload)
	settings := dto.ChannelOtherSettings{
		AllowServiceTier:        true,
		AllowInferenceGeo:       true,
		AllowSpeed:              true,
		AllowSafetyIdentifier:   true,
		AllowIncludeObfuscation: true,
	}

	body, size, closer, err := PreparePassThroughJSONBody(storage, settings)
	require.NoError(t, err)
	require.Nil(t, closer)
	require.Equal(t, int64(len(payload)), size)
	require.Zero(t, storage.newReaderCalls)

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, payload, data)
}

func TestPreparePassThroughJSONBodyFiltersWithoutMaterializingStorage(t *testing.T) {
	payload := []byte(`{"service_tier":"flex","model":"gpt-5","input":"` + strings.Repeat("x", 1<<20) + `"}`)
	storage := newNoBytesBodyStorage(payload)

	body, size, closer, err := PreparePassThroughJSONBody(storage, dto.ChannelOtherSettings{})
	require.NoError(t, err)
	require.NotNil(t, closer)
	defer closer.Close()

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NotContains(t, string(data), "service_tier")
	require.JSONEq(t, `{"model":"gpt-5","input":"`+strings.Repeat("x", 1<<20)+`"}`, string(data))
	require.Equal(t, int64(len(data)), size)
}

func TestPreparePassThroughJSONBodyPreservesValidStructureAcrossFieldPositions(t *testing.T) {
	tests := []struct {
		name     string
		payload  string
		expected string
	}{
		{name: "first", payload: `{"service_tier":"flex","model":"gpt-5"}`, expected: `{"model":"gpt-5"}`},
		{name: "middle", payload: `{"model":"gpt-5","speed":"fast","input":"hi"}`, expected: `{"model":"gpt-5","input":"hi"}`},
		{name: "last", payload: `{"model":"gpt-5","speed":"fast"}`, expected: `{"model":"gpt-5"}`},
		{name: "all", payload: `{"service_tier":"flex","speed":"fast"}`, expected: `{}`},
		{name: "escaped key", payload: `{"service\u005ftier":"flex","model":"gpt-5"}`, expected: `{"model":"gpt-5"}`},
		{name: "nested root field is preserved", payload: `{"metadata":{"service_tier":"flex"},"model":"gpt-5"}`, expected: `{"metadata":{"service_tier":"flex"},"model":"gpt-5"}`},
		{name: "empty stream options removed", payload: `{"stream_options":{"include_obfuscation":false},"model":"gpt-5"}`, expected: `{"model":"gpt-5"}`},
		{name: "other stream options preserved", payload: `{"stream_options":{"include_usage":true,"include_obfuscation":false},"model":"gpt-5"}`, expected: `{"stream_options":{"include_usage":true},"model":"gpt-5"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := newNoBytesBodyStorage([]byte(tt.payload))
			body, size, closer, err := PreparePassThroughJSONBody(storage, dto.ChannelOtherSettings{})
			require.NoError(t, err)
			if closer != nil {
				defer closer.Close()
			}
			data, err := io.ReadAll(body)
			require.NoError(t, err)
			require.JSONEq(t, tt.expected, string(data))
			require.Equal(t, int64(len(data)), size)
		})
	}
}

func TestPreparePassThroughJSONBodyProvidesIndependentFilteredReplayReaders(t *testing.T) {
	storage := newNoBytesBodyStorage([]byte(`{"model":"gpt-5","service_tier":"flex","input":"abcdef"}`))
	body, _, closer, err := PreparePassThroughJSONBody(storage, dto.ChannelOtherSettings{})
	require.NoError(t, err)
	require.NotNil(t, closer)

	replayable, ok := body.(basecommon.ReplayableBody)
	require.True(t, ok)
	first, err := replayable.NewReader()
	require.NoError(t, err)
	defer first.Close()
	second, err := replayable.NewReader()
	require.NoError(t, err)
	defer second.Close()

	prefix := make([]byte, 8)
	_, err = io.ReadFull(first, prefix)
	require.NoError(t, err)
	secondData, err := io.ReadAll(second)
	require.NoError(t, err)
	firstRest, err := io.ReadAll(first)
	require.NoError(t, err)

	expected := []byte(`{"model":"gpt-5","input":"abcdef"}`)
	require.Equal(t, expected, secondData)
	require.Equal(t, expected, append(prefix, firstRest...))
	require.NoError(t, closer.Close())
	_, err = body.Read(make([]byte, 1))
	require.ErrorIs(t, err, io.ErrClosedPipe)
	third, err := replayable.NewReader()
	require.NoError(t, err)
	defer third.Close()
	thirdData, err := io.ReadAll(third)
	require.NoError(t, err)
	require.Equal(t, expected, thirdData)
}
