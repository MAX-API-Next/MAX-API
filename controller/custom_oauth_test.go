package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/MAX-API-Next/MAX-API/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFetchCustomOAuthDiscoveryRejectsPrivateURLWhenSSRFProtectionEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	serverHit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHit = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issuer":"http://127.0.0.1","authorization_endpoint":"http://127.0.0.1/auth"}`))
	}))
	defer server.Close()

	fetchSetting := system_setting.GetFetchSetting()
	original := *fetchSetting
	t.Cleanup(func() {
		*fetchSetting = original
	})
	fetchSetting.EnableSSRFProtection = true
	fetchSetting.AllowPrivateIp = false
	fetchSetting.DomainFilterMode = false
	fetchSetting.IpFilterMode = false
	fetchSetting.DomainList = nil
	fetchSetting.IpList = nil
	fetchSetting.AllowedPorts = []string{"80", "443", "8080", "8443"}
	fetchSetting.ApplyIPFilterForDomain = true

	payload, err := common.Marshal(FetchCustomOAuthDiscoveryRequest{WellKnownURL: server.URL})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/custom-oauth-provider/discovery", bytes.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")

	FetchCustomOAuthDiscovery(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	require.False(t, serverHit)
}
