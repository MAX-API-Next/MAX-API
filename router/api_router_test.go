package router

import (
	"net/http"
	"os"
	"testing"

	"github.com/MAX-API-Next/MAX-API/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserTokenRouteUsesPostOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	SetApiRouter(engine)

	var hasPost bool
	var hasGet bool
	for _, route := range engine.Routes() {
		if route.Path != "/api/user/token" {
			continue
		}
		switch route.Method {
		case "POST":
			hasPost = true
		case "GET":
			hasGet = true
		}
	}

	if !hasPost {
		t.Fatal("expected POST /api/user/token route to be registered")
	}
	if hasGet {
		t.Fatal("did not expect GET /api/user/token route to be registered")
	}
}

func TestUserTokenOpenAPIUsesPostOnly(t *testing.T) {
	documentBytes, err := os.ReadFile("../docs/openapi/api.json")
	require.NoError(t, err)

	var document struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	require.NoError(t, common.Unmarshal(documentBytes, &document))

	tokenPath, ok := document.Paths["/api/user/token"]
	require.True(t, ok, "expected /api/user/token in OpenAPI document")
	require.Contains(t, tokenPath, "post")
	require.NotContains(t, tokenPath, "get")
}

func TestVerificationMethodsRouteIsRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	SetApiRouter(engine)

	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/verify/methods" {
			return
		}
	}
	t.Fatal("expected GET /api/verify/methods route to be registered")
}
