package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRelayRouterRegistersAlphaSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)

	found := false
	for _, route := range engine.Routes() {
		if route.Method == http.MethodPost && route.Path == "/v1/alpha/search" {
			found = true
			break
		}
	}

	require.True(t, found, "expected POST /v1/alpha/search route to be registered")
}
