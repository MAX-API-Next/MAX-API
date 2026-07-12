package router

import (
	"testing"

	"github.com/gin-gonic/gin"
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
