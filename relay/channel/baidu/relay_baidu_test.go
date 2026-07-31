package baidu

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/dto"
	"github.com/stretchr/testify/require"
)

func TestParseBaiduAccessTokenResponseRejectsNonSuccessStatus(t *testing.T) {
	accessToken, err := parseBaiduAccessTokenResponse(&http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_client"}`)),
	})

	require.Nil(t, accessToken)
	require.EqualError(t, err, "baidu access token endpoint returned HTTP 401")
}

func TestParseBaiduAccessTokenResponseAcceptsSuccessStatus(t *testing.T) {
	accessToken, err := parseBaiduAccessTokenResponse(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"access_token":"token","expires_in":3600}`)),
	})

	require.NoError(t, err)
	require.Equal(t, "token", accessToken.AccessToken)
	require.Equal(t, int64(3600), accessToken.ExpiresIn)
}

func TestRequestOpenAI2BaiduPreservesExplicitZeroValues(t *testing.T) {
	zero := 0.0
	stream := false
	request := requestOpenAI2Baidu(dto.GeneralOpenAIRequest{
		TopP:             &zero,
		FrequencyPenalty: &zero,
		Stream:           &stream,
	})

	payload, err := common.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, `{"messages":null,"top_p":0,"penalty_score":0,"stream":false}`, string(payload))
}
