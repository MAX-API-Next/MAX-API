package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountBindingRoutesRequireAuthentication(t *testing.T) {
	oldRate := common.GlobalApiRateLimitEnable
	common.GlobalApiRateLimitEnable = false
	t.Cleanup(func() { common.GlobalApiRateLimitEnable = oldRate })
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("binding-router-test"))))
	SetApiRouter(r)
	for _, path := range []string{"/api/oauth/email/bind", "/api/oauth/wechat/bind"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		require.Equal(t, http.StatusUnauthorized, res.Code, path)
	}
}

func TestAccountBindingOpenAPIUsesAuthenticatedJSONPost(t *testing.T) {
	data, err := os.ReadFile("../docs/openapi/api.json")
	require.NoError(t, err)
	var doc struct {
		Paths map[string]map[string]struct {
			Security    []map[string][]string `json:"security"`
			RequestBody struct {
				Required bool `json:"required"`
			} `json:"requestBody"`
		} `json:"paths"`
	}
	require.NoError(t, common.Unmarshal(data, &doc))
	for _, path := range []string{"/api/oauth/email/bind", "/api/oauth/wechat/bind"} {
		op, ok := doc.Paths[path]["post"]
		require.True(t, ok, path)
		_, legacy := doc.Paths[path]["get"]
		require.False(t, legacy, path)
		require.True(t, op.RequestBody.Required, path)
		require.Equal(t, []map[string][]string{{"SessionAuth1": {}}}, op.Security, path)
	}
}
