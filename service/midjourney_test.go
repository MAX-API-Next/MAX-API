package service

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"testing"
	"time"

	"github.com/MAX-API-Next/MAX-API/constant"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type midjourneyRoundTripFunc func(*http.Request) (*http.Response, error)

func (f midjourneyRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoMidjourneyHttpRequestTracksWhetherTransportWroteRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalClient := httpClient
	t.Cleanup(func() { httpClient = originalClient })

	tests := []struct {
		name        string
		wroteErr    error
		invokeTrace bool
		wantSent    bool
	}{
		{name: "transport failure before write", wantSent: false},
		{name: "request fully written before response failure", invokeTrace: true, wantSent: true},
		{name: "partial write remains ambiguous", invokeTrace: true, wroteErr: errors.New("partial write"), wantSent: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpClient = &http.Client{Transport: midjourneyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if tt.invokeTrace {
					trace := httptrace.ContextClientTrace(req.Context())
					require.NotNil(t, trace)
					require.NotNil(t, trace.WroteRequest)
					trace.WroteRequest(httptrace.WroteRequestInfo{Err: tt.wroteErr})
				}
				return nil, errors.New("upstream transport failed")
			})}

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit", bytes.NewBufferString(`{"prompt":"test"}`))
			ctx.Request.Header.Set("Content-Type", "application/json")

			_, _, requestSent, err := DoMidjourneyHttpRequest(ctx, time.Second, "https://midjourney.invalid/submit")
			require.Error(t, err)
			require.Equal(t, tt.wantSent, requestSent)
		})
	}
}

func TestDoMidjourneyHttpRequestTreatsReceivedResponseAsSent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalClient := httpClient
	httpClient = &http.Client{Transport: midjourneyRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(midjourneyFailingReader{}),
		}, nil
	})}
	t.Cleanup(func() { httpClient = originalClient })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit", bytes.NewBufferString(`{"prompt":"test"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	_, _, requestSent, err := DoMidjourneyHttpRequest(ctx, time.Second, "https://midjourney.invalid/submit")
	require.Error(t, err)
	require.True(t, requestSent)
}

func TestCoverPlusActionToNormalActionRejectsMalformedCustomID(t *testing.T) {
	tests := []string{
		"plain",
		"MJ",
		"MJ::JOB",
		"MJ::JOB::upsample",
		"MJ::JOB::variation",
	}

	for _, customID := range tests {
		t.Run(customID, func(t *testing.T) {
			req := &dto.MidjourneyRequest{CustomId: customID}

			var resp *dto.MidjourneyResponse
			require.NotPanics(t, func() {
				resp = CoverPlusActionToNormalAction(req)
			})

			require.NotNil(t, resp)
			require.Equal(t, constant.MjRequestError, resp.Code)
		})
	}
}

func TestCoverPlusActionToNormalActionAcceptsJobUpsampleCustomID(t *testing.T) {
	req := &dto.MidjourneyRequest{CustomId: "MJ::JOB::upsample::2::task-id"}

	resp := CoverPlusActionToNormalAction(req)

	require.Nil(t, resp)
	require.Equal(t, constant.MjActionUpscale, req.Action)
	require.Equal(t, 2, req.Index)
}

type midjourneyFailingReader struct{}

func (midjourneyFailingReader) Read([]byte) (int, error) {
	return 0, errors.New("response body failed")
}
